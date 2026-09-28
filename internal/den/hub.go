package den

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Audience says which members an event is for.
type Audience struct {
	kind audienceKind
	a, b int64
}

type audienceKind uint8

const (
	toEveryone audienceKind = iota
	toStaff
	toNonStaff
	toMembers
	toNobody
)

// Audiences.
var (
	Everyone = Audience{kind: toEveryone}
	Staff    = Audience{kind: toStaff}    // moderators and the owner
	NonStaff = Audience{kind: toNonStaff} // everyone else
	nobody   = Audience{kind: toNobody}
)

// OnlyMember is an audience of one.
func OnlyMember(id int64) Audience { return Audience{kind: toMembers, a: id, b: id} }

// Pair is the audience of a DM: its two members.
func Pair(a, b int64) Audience { return Audience{kind: toMembers, a: a, b: b} }

func (a Audience) includes(member int64, staff bool) bool {
	switch a.kind {
	case toStaff:
		return staff
	case toNonStaff:
		return !staff
	case toMembers:
		return member == a.a || member == a.b
	case toNobody:
		return false
	}
	return true
}

// eventRefresh is an internal event telling a socket to send a fresh
// ready: what its member can see has changed. It never reaches clients.
const eventRefresh = "_refresh"

// Hub numbers durable events and fans them out to connected sockets. It
// keeps recent events in a ring so a client that reconnects gets only what
// it missed. Sequence numbers restart with each process, so the epoch tells
// a resuming client whether its number still means anything here.
type Hub struct {
	epoch   string
	maxLen  int
	maxAge  time.Duration
	subSize int
	now     func() time.Time

	mu    sync.Mutex
	seq   uint64
	first uint64 // seq of ring[0]
	ring  []ringEntry
	subs  map[*Sub]struct{}
	// Resume points before these need a fresh snapshot instead of a
	// replay: what the member could see changed after them.
	nonStaffRefresh uint64
	memberRefresh   map[int64]uint64
}

type ringEntry struct {
	at       time.Time
	audience Audience
	event    denproto.Event
}

// Sub is one socket's subscription. Events arrive in order; the channel is
// closed when the socket falls too far behind (Slow) or unsubscribes.
type Sub struct {
	member int64
	Events chan denproto.Event
	slow   bool

	// Guarded by Hub.mu.
	staff bool
	focus map[string]bool // channels the connection shows
}

// Slow reports whether the subscription was dropped for falling behind.
// Read it only after Events is closed.
func (s *Sub) Slow() bool { return s.slow }

// NewHub returns a hub with a fresh epoch.
func NewHub() *Hub {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return &Hub{
		epoch:         hex.EncodeToString(b),
		maxLen:        50_000,
		maxAge:        10 * time.Minute,
		subSize:       1024,
		now:           time.Now,
		first:         1,
		subs:          map[*Sub]struct{}{},
		memberRefresh: map[int64]uint64{},
	}
}

// Epoch identifies this process's sequence numbers.
func (h *Hub) Epoch() string { return h.epoch }

// Publish records a durable event for an audience and sends it to their
// sockets.
func (h *Hub) Publish(t string, data any, audience Audience) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	event, err := denproto.NewEvent(t, h.seq+1, data)
	if err != nil {
		return err
	}
	h.record(event, audience)
	for sub := range h.subs {
		if audience.includes(sub.member, sub.staff) {
			h.send(sub, event)
		}
	}
	return nil
}

// record appends an event to the ring under the next seq; the caller holds
// h.mu.
func (h *Hub) record(event denproto.Event, audience Audience) {
	h.seq++
	event.Seq = h.seq
	now := h.now()
	h.ring = append(h.ring, ringEntry{at: now, audience: audience, event: event})
	h.trim(now)
}

// Ephemeral sends an event that isn't numbered or kept for replay, such as
// presence or typing, to every socket that to accepts. to runs with the
// hub locked, so it may read a subscription's staff flag and focus.
func (h *Hub) Ephemeral(t string, data any, to func(*Sub) bool) error {
	event, err := denproto.NewEvent(t, 0, data)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if to(sub) {
			h.send(sub, event)
		}
	}
	return nil
}

// refresh makes the matching sockets start over from a fresh snapshot, and
// returns the seq it took. The refresh occupies a seq of its own, recorded
// for nobody, so a client that saw every event before it still resyncs if
// it disconnected before the snapshot arrived. The caller holds h.mu.
func (h *Hub) refresh(match func(*Sub) bool) uint64 {
	h.record(denproto.Event{T: eventRefresh}, nobody)
	refresh := denproto.Event{T: eventRefresh, Seq: h.seq}
	for sub := range h.subs {
		if match(sub) {
			// Focus is checked against visibility when it's set, so it
			// starts over too; clients send it again after the snapshot.
			sub.focus = nil
			h.send(sub, refresh)
		}
	}
	return h.seq
}

// RefreshNonStaff tells every non-staff socket to replace its state with a
// fresh snapshot, and makes older resume points for non-staff members
// resync. Visibility changes use it: the events that led up to one can't
// be replayed as filtered history.
func (h *Hub) RefreshNonStaff() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nonStaffRefresh = h.refresh(func(sub *Sub) bool { return !sub.staff })
}

// SetStaff changes whether a member's sockets see staff-only channels, after
// a role change, and starts them over from a fresh snapshot.
func (h *Hub) SetStaff(member int64, staff bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, seq := range h.memberRefresh {
		if seq < h.first {
			delete(h.memberRefresh, id) // older than the ring; resyncs anyway
		}
	}
	h.memberRefresh[member] = h.refresh(func(sub *Sub) bool {
		if sub.member != member {
			return false
		}
		sub.staff = staff
		return true
	})
}

// Staff reports whether a subscription currently sees staff-only channels.
func (h *Hub) Staff(sub *Sub) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return sub.staff
}

// SetFocus records the channels a subscription's connection shows.
func (h *Hub) SetFocus(sub *Sub, channels map[string]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub.focus = channels
}

// Focused reports whether a subscription's connection shows a channel.
func (h *Hub) Focused(sub *Sub, channel string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return sub.focus[channel]
}

// send queues an event for a subscriber; the caller holds h.mu.
func (h *Hub) send(sub *Sub, event denproto.Event) {
	select {
	case sub.Events <- event:
	default:
		// Dropping a slow socket bounds the den's memory; the client
		// resumes from the ring when it reconnects.
		sub.slow = true
		delete(h.subs, sub)
		close(sub.Events)
	}
}

func (h *Hub) trim(now time.Time) {
	drop := 0
	for drop < len(h.ring) && (len(h.ring)-drop > h.maxLen || now.Sub(h.ring[drop].at) > h.maxAge) {
		drop++
	}
	if drop > 0 {
		h.ring = append(h.ring[:0:0], h.ring[drop:]...)
		h.first += uint64(drop)
	}
}

// Subscribe registers a socket for a member. When the client's resume
// point (epoch and seq) is still covered by the ring, it returns the events
// the member missed and resumed is true; otherwise the client needs a fresh
// snapshot, taken after this call, and seq is where that snapshot starts.
// Events published after Subscribe arrive on the subscription either way.
func (h *Hub) Subscribe(member int64, staff bool, epoch string, after uint64) (sub *Sub, missed []denproto.Event, resumed bool, seq uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trim(h.now())
	sub = &Sub{member: member, staff: staff, Events: make(chan denproto.Event, h.subSize)}
	h.subs[sub] = struct{}{}
	stale := (!staff && after < h.nonStaffRefresh) || after < h.memberRefresh[member]
	if epoch == h.epoch && !stale && after <= h.seq && after+1 >= h.first {
		for _, e := range h.ring[after+1-h.first:] {
			if e.audience.includes(member, staff) {
				missed = append(missed, e.event)
			}
		}
		return sub, missed, true, h.seq
	}
	return sub, nil, false, h.seq
}

// Unsubscribe removes a subscription and closes its channel.
func (h *Hub) Unsubscribe(sub *Sub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[sub]; ok {
		delete(h.subs, sub)
		close(sub.Events)
	}
}
