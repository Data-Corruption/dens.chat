package den

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Everyone is the audience of an event every member sees.
const Everyone int64 = 0

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
}

type ringEntry struct {
	at       time.Time
	audience int64
	event    denproto.Event
}

// Sub is one socket's subscription. Events arrives in order; it is closed
// when the socket falls too far behind (Slow) or unsubscribes.
type Sub struct {
	member int64
	Events chan denproto.Event
	slow   bool
}

// Slow reports whether the subscription was dropped for falling behind.
// Read it only after Events is closed.
func (s *Sub) Slow() bool { return s.slow }

// NewHub returns a hub with a fresh epoch.
func NewHub() *Hub {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return &Hub{
		epoch:   hex.EncodeToString(b),
		maxLen:  50_000,
		maxAge:  10 * time.Minute,
		subSize: 1024,
		now:     time.Now,
		first:   1,
		subs:    map[*Sub]struct{}{},
	}
}

// Epoch identifies this process's sequence numbers.
func (h *Hub) Epoch() string { return h.epoch }

// Publish records a durable event for audience (Everyone, or one member)
// and sends it to their sockets.
func (h *Hub) Publish(t string, data any, audience int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	event, err := denproto.NewEvent(t, h.seq+1, data)
	if err != nil {
		return err
	}
	h.seq++
	now := h.now()
	h.ring = append(h.ring, ringEntry{at: now, audience: audience, event: event})
	h.trim(now)
	for sub := range h.subs {
		if audience != Everyone && audience != sub.member {
			continue
		}
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
	return nil
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

// Subscribe registers a socket for member. When the client's resume point
// (epoch and seq) is still covered by the ring, it returns the events the
// member missed and resumed is true; otherwise the client needs a fresh
// snapshot, taken after this call, and seq is where that snapshot starts.
// Events published after Subscribe arrive on the subscription either way.
func (h *Hub) Subscribe(member int64, epoch string, after uint64) (sub *Sub, missed []denproto.Event, resumed bool, seq uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trim(h.now())
	sub = &Sub{member: member, Events: make(chan denproto.Event, h.subSize)}
	h.subs[sub] = struct{}{}
	if epoch == h.epoch && after <= h.seq && after+1 >= h.first {
		for _, e := range h.ring[after+1-h.first:] {
			if e.audience == Everyone || e.audience == member {
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
