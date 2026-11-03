package denclient

import (
	"slices"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A message's tallies, its reactions (M15.3) and its poll's votes (M15.4),
// change it without changing its revision. The cache applies their events
// to the copy it holds, and takes what the den sends of a message at its
// own revision too, so an install that was away fetches the message again
// as it now stands; and since the den may read a message before such an
// event and send it after, what it sends after a request that was out
// while one came takes it again from a ring of the latest.

// applyTally applies an event that changes a message's tallies to a
// message as the den sent it, and reports whether that changed it.
func applyTally(m *denproto.Message, e denproto.Event) bool {
	switch e.T {
	case denproto.EventPollUpdated, denproto.EventPollEnded:
		return applyPoll(m, e)
	}
	return applyReaction(m, e)
}

// tallyRing keeps the latest events a connection applied that change a
// message's tallies, numbered, so what the den sends of a message after a
// request that was out while they came takes them again: the den may have
// read the message before them. Applying a reaction's event twice changes
// nothing, and a poll's carry how it stands, in order, the last of them
// the newest.
type tallyRing struct {
	mu     sync.Mutex
	n      uint64 // events seen
	events []denproto.Event
}

// ringSize is how many of the latest events the ring keeps.
const ringSize = 1024

func (r *tallyRing) add(e denproto.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == ringSize {
		r.events = append(r.events[:0], r.events[1:]...)
	}
	r.events = append(r.events, e)
	r.n++
}

// mark is where the ring stands, to replay from once a request returns.
func (r *tallyRing) mark() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// since lists the events after a mark that the ring still holds.
func (r *tallyRing) since(mark uint64) []denproto.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	first := r.n - uint64(len(r.events))
	if mark < first {
		mark = first
	}
	return slices.Clone(r.events[mark-first:])
}

// replayTallies applies the events that came since a mark to messages the
// den sent meanwhile.
func (c *conn) replayTallies(mark uint64, ms []denproto.Message) {
	events := c.tallies.since(mark)
	if len(events) == 0 {
		return
	}
	for i := range ms {
		for _, e := range events {
			applyTally(&ms[i], e)
		}
	}
}

// cacheTally keeps what an event changes of the tallies of the copy of its
// message the cache holds.
func (c *conn) cacheTally(e denproto.Event, messageID string) {
	if !c.cached() {
		return
	}
	id, err := denproto.ParseID(messageID)
	if err != nil {
		return
	}
	c.cacheErr(c.m.Cache.ChangeMessage(c.j.denID, id, func(m *denproto.Message) bool { return applyTally(m, e) }))
}
