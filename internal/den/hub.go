package den

import (
	"crypto/rand"
	"encoding/hex"
	"math/bits"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// maxRoleSlots is the width of the Hub's dense role bitmask. A den allows at
// most denproto.MaxRoles (50) live roles, leaving 14 spare slots to quarantine
// recently deleted roles until they fall out of the replay ring.
const maxRoleSlots = 64

// Audience says which members an event is for. Packing staff beside kind keeps
// Audience at 48 bytes with zero heap allocations for role-gated channels.
type Audience struct {
	kind  audienceKind
	staff bool
	a, b  int64
	roles uint64
	// set holds a poll's voters (or an event's interested members), for an
	// audience of them or of the rest of its channel, and staff/roles who
	// sees the channel. Nothing changes set once its audience is made, since
	// the ring keeps the audience for replays.
	set     map[int64]bool
	version int
}

type audienceKind uint8

const (
	toEveryone audienceKind = iota
	toStaff
	toNonStaff
	toMembers
	toNobody
	toRoles
	toVoters
	toNonVoters
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

// Roles is the audience of a role-gated channel: staff and any member holding
// at least one role bit in mask.
func Roles(mask uint64) Audience { return Audience{kind: toRoles, roles: mask} }

// Voters is the audience of a poll's voters (or an event's interested members)
// among those who see its channel, and NonVoters the rest of them. voters
// mustn't change once it's passed.
func Voters(voters map[int64]bool, staffOnly bool, roles uint64) Audience {
	return Audience{kind: toVoters, set: voters, staff: staffOnly, roles: roles}
}

func NonVoters(voters map[int64]bool, staffOnly bool, roles uint64) Audience {
	return Audience{kind: toNonVoters, set: voters, staff: staffOnly, roles: roles}
}

// notesOf is the audience of a member's notes: the member, on sockets
// speaking version 3 of the protocol.
func notesOf(member int64) Audience {
	return Audience{kind: toMembers, a: member, b: member, version: denproto.NotesVersion}
}

// needs is the audience on sockets speaking a version of the protocol at
// least, as an event about a channel of a kind newer than the protocol's
// first needs (see kindVersion).
func (a Audience) needs(version int) Audience {
	a.version = max(a.version, version)
	return a
}

// includes reports whether the audience holds a member, staff or not, with
// custom role mask roles, on a socket speaking a version of the protocol.
func (a Audience) includes(member int64, staff bool, roles uint64, version int) bool {
	if version < a.version {
		return false
	}
	switch a.kind {
	case toStaff:
		return staff
	case toNonStaff:
		return !staff
	case toMembers:
		return member == a.a || member == a.b
	case toNobody:
		return false
	case toRoles:
		return staff || (a.roles&roles) != 0
	case toVoters:
		return (staff || (!a.staff && (a.roles == 0 || (a.roles&roles) != 0))) && a.set[member]
	case toNonVoters:
		return (staff || (!a.staff && (a.roles == 0 || (a.roles&roles) != 0))) && !a.set[member]
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
	epoch  string
	maxLen int
	maxAge time.Duration
	// maxBytes bounds what the ring's events hold, so members sending
	// large messages fast can't grow the den's memory as far as maxLen
	// would let them.
	maxBytes int
	subSize  int
	now      func() time.Time

	mu    sync.Mutex
	seq   uint64
	first uint64 // seq of ring[head]
	// ring[head:] holds the recent events; trim moves head past the ones
	// it drops and clears them, and record moves the rest down only once
	// they take less than half the slice, so publishing never copies the
	// whole ring.
	ring  []ringEntry
	head  int
	bytes int // the size of the ring's events
	subs  map[*Sub]struct{}
	// Resume points before these need a fresh snapshot instead of a
	// replay: what the member could see changed after them.
	nonStaffRefresh uint64
	memberRefresh   map[int64]uint64
	roleRefresh     [maxRoleSlots]uint64

	// Dense slot allocation: each active role holds one bit in [0, 64).
	// Deleted roles keep their slot quarantined in retiredAt until that seq
	// falls behind first (trimmed from the ring); if all 64 slots are active
	// or still in the ring, recycling refreshes non-staff first so no
	// resuming non-staff socket can replay across a reused bit.
	slotOf    map[int64]uint8
	active    uint64
	retiredAt [maxRoleSlots]uint64
}

type ringEntry struct {
	at       time.Time
	audience Audience
	event    denproto.Event
	size     int
}

// Sub is one socket's subscription. Events arrive in order; the channel is
// closed when the socket falls too far behind (Slow) or unsubscribes.
// version is the protocol's version its socket speaks, which decides
// whether it gets notes.
type Sub struct {
	member  int64
	version int
	Events  chan denproto.Event
	slow    bool

	// Guarded by Hub.mu.
	staff bool
	roles uint64
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
		maxBytes:      64 << 20,
		subSize:       1024,
		now:           time.Now,
		first:         1,
		subs:          map[*Sub]struct{}{},
		memberRefresh: map[int64]uint64{},
		slotOf:        map[int64]uint8{},
	}
}

// Epoch identifies this process's sequence numbers.
func (h *Hub) Epoch() string { return h.epoch }

// EnsureRole assigns a bit slot for roleID if it does not already have one,
// returning its single-bit mask and whether recycling an in-ring retired slot
// forced a non-staff refresh.
func (h *Hub) EnsureRole(roleID int64) (uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.slotOf[roleID]; ok {
		return uint64(1) << s, false
	}
	h.trim(h.now())
	var oldestSlot uint8 = 255
	var oldestSeq uint64 = ^uint64(0)
	for s := uint8(0); s < maxRoleSlots; s++ {
		if h.active&(uint64(1)<<s) != 0 {
			continue
		}
		if h.retiredAt[s] == 0 || h.retiredAt[s] < h.first {
			h.slotOf[roleID] = s
			h.active |= uint64(1) << s
			h.retiredAt[s] = 0
			h.roleRefresh[s] = 0
			return uint64(1) << s, false
		}
		if h.retiredAt[s] < oldestSeq {
			oldestSeq = h.retiredAt[s]
			oldestSlot = s
		}
	}
	if oldestSlot == 255 {
		return 0, false
	}
	h.nonStaffRefresh = h.refresh(func(sub *Sub) bool { return !sub.staff })
	h.slotOf[roleID] = oldestSlot
	h.active |= uint64(1) << oldestSlot
	h.retiredAt[oldestSlot] = 0
	h.roleRefresh[oldestSlot] = 0
	return uint64(1) << oldestSlot, true
}

// Mask returns the union of bit slots assigned to roleIDs.
func (h *Hub) Mask(roleIDs []int64) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	var m uint64
	for _, id := range roleIDs {
		if s, ok := h.slotOf[id]; ok {
			m |= uint64(1) << s
		}
	}
	return m
}

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
		if audience.includes(sub.member, sub.staff, sub.roles, sub.version) {
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
	if h.head > 0 && h.head >= len(h.ring)-h.head {
		n := copy(h.ring, h.ring[h.head:])
		clear(h.ring[n:])
		h.ring = h.ring[:n]
		h.head = 0
	}
	size := eventSize(event)
	h.ring = append(h.ring, ringEntry{at: now, audience: audience, event: event, size: size})
	h.bytes += size
	h.trim(now)
}

// Ephemeral sends an event that isn't numbered or kept for replay, such as
// presence or typing, to every socket that to accepts. to runs with the
// hub locked, so it may read a subscription's staff flag, roles and focus.
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

// pruneMemberRefresh removes member refresh watermarks that are already older
// than the ring. The caller holds h.mu.
func (h *Hub) pruneMemberRefresh() {
	for id, seq := range h.memberRefresh {
		if seq < h.first {
			delete(h.memberRefresh, id)
		}
	}
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

// RefreshChannelVisibility refreshes only non-staff members whose visibility
// may have changed. When either old or new mode is Everyone, all non-staff
// refresh; between StaffOnly and SelectedRoles (or two role sets), only
// non-staff holding any bit in oldMask|newMask refresh and have their
// roleRefresh watermarks bumped.
func (h *Hub) RefreshChannelVisibility(oldEveryone, newEveryone bool, oldMask, newMask uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if oldEveryone || newEveryone {
		h.nonStaffRefresh = h.refresh(func(sub *Sub) bool { return !sub.staff })
		return
	}
	affected := oldMask | newMask
	if affected == 0 {
		return
	}
	seq := h.refresh(func(sub *Sub) bool {
		return !sub.staff && (sub.roles&affected) != 0
	})
	for m := affected; m != 0; {
		slot := uint8(bits.TrailingZeros64(m))
		if seq > h.roleRefresh[slot] {
			h.roleRefresh[slot] = seq
		}
		m &= m - 1
	}
}

// SetStaff changes whether a member's sockets see staff-only channels, after
// a role change, and starts them over from a fresh snapshot.
func (h *Hub) SetStaff(member int64, staff bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneMemberRefresh()
	h.memberRefresh[member] = h.refresh(func(sub *Sub) bool {
		if sub.member != member {
			return false
		}
		sub.staff = staff
		return true
	})
}

// SetMemberRoles updates a member's custom role mask; non-staff sockets
// refresh and older resume points for the member force a fresh snapshot.
func (h *Hub) SetMemberRoles(member int64, roles uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneMemberRefresh()
	seq := h.refresh(func(sub *Sub) bool {
		if sub.member != member {
			return false
		}
		sub.roles = roles
		return !sub.staff
	})
	h.memberRefresh[member] = seq
}

// DeleteRole retires roleID's bit slot, clears it from live sockets, refreshes
// non-staff in affectedMembers (loaded before delete) or holding any role in
// otherChannelRoles (roles sharing a channel with roleID), and marks them
// stale for resume.
func (h *Hub) DeleteRole(roleID int64, affectedMembers []int64, otherChannelRoles uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	slot, ok := h.slotOf[roleID]
	if !ok {
		return
	}
	bit := uint64(1) << slot
	otherChannelRoles &^= bit

	memberSet := make(map[int64]struct{}, len(affectedMembers))
	for _, id := range affectedMembers {
		memberSet[id] = struct{}{}
	}
	h.pruneMemberRefresh()
	seq := h.refresh(func(sub *Sub) bool {
		sub.roles &^= bit
		if sub.staff {
			return false
		}
		if _, had := memberSet[sub.member]; had {
			return true
		}
		return (sub.roles & otherChannelRoles) != 0
	})
	for _, id := range affectedMembers {
		h.memberRefresh[id] = seq
	}
	for m := otherChannelRoles; m != 0; {
		s := uint8(bits.TrailingZeros64(m))
		if seq > h.roleRefresh[s] {
			h.roleRefresh[s] = seq
		}
		m &= m - 1
	}
	delete(h.slotOf, roleID)
	h.active &^= bit
	h.retiredAt[slot] = seq
}

// Staff reports whether a subscription currently sees staff-only channels.
func (h *Hub) Staff(sub *Sub) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return sub.staff
}

// Roles returns a subscription's current custom role mask.
func (h *Hub) Roles(sub *Sub) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return sub.roles
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

// trim drops the oldest events past the ring's length, size or age. The
// caller holds h.mu.
func (h *Hub) trim(now time.Time) {
	for h.head < len(h.ring) {
		e := &h.ring[h.head]
		if len(h.ring)-h.head <= h.maxLen && h.bytes <= h.maxBytes && now.Sub(e.at) <= h.maxAge {
			break
		}
		h.bytes -= e.size
		*e = ringEntry{} // so the event's memory goes now
		h.head++
		h.first++
	}
}

// forget drops the events the ring holds, as erasing the den does,
// so no client resumes into what the den was.
func (h *Hub) forget() {
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.ring)
	h.ring, h.head, h.bytes = h.ring[:0], 0, 0
	h.first = h.seq + 1
	h.nonStaffRefresh = 0
	clear(h.memberRefresh)
	clear(h.slotOf)
	h.active = 0
	h.retiredAt = [maxRoleSlots]uint64{}
	h.roleRefresh = [maxRoleSlots]uint64{}
}

func (h *Hub) roleStale(roles uint64, after uint64) bool {
	for m := roles; m != 0; {
		slot := uint8(bits.TrailingZeros64(m))
		if after < h.roleRefresh[slot] {
			return true
		}
		m &= m - 1
	}
	return false
}

// Subscribe registers a socket for a member, speaking this build's version
// of the protocol with no custom roles. When the client's resume point (epoch
// and seq) is still covered by the ring, it returns the events the member
// missed and resumed is true; otherwise the client needs a fresh snapshot,
// taken after this call, and seq is where that snapshot starts. Events
// published after Subscribe arrive on the subscription either way.
func (h *Hub) Subscribe(member int64, staff bool, epoch string, after uint64) (sub *Sub, missed []denproto.Event, resumed bool, seq uint64) {
	return h.SubscribeAt(member, staff, 0, denproto.Version, epoch, after)
}

// SubscribeAt subscribes as Subscribe does, for a socket with custom role mask
// roles speaking a version of the protocol, which gets nothing of notes before
// 3.
func (h *Hub) SubscribeAt(member int64, staff bool, roles uint64, version int, epoch string, after uint64) (sub *Sub, missed []denproto.Event, resumed bool, seq uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trim(h.now())
	sub = &Sub{member: member, version: version, staff: staff, roles: roles, Events: make(chan denproto.Event, h.subSize)}
	h.subs[sub] = struct{}{}
	stale := after < h.memberRefresh[member] || (!staff && (after < h.nonStaffRefresh || h.roleStale(roles, after)))
	if epoch == h.epoch && !stale && after <= h.seq && after+1 >= h.first {
		for _, e := range h.ring[h.head+int(after+1-h.first):] {
			if e.audience.includes(member, staff, roles, version) {
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
