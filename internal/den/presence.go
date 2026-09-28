package den

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A member is online while they have an open socket. Changes are announced
// in batches, so a burst of reconnects, as after a den restart, costs one
// event instead of one per member per change.
const defaultPresenceDelay = 1500 * time.Millisecond

type presence struct {
	mu        sync.Mutex
	sockets   map[int64]int  // open sockets per member
	announced map[int64]bool // online as clients were last told
	pending   bool
}

func newPresence() *presence {
	return &presence{sockets: map[int64]int{}, announced: map[int64]bool{}}
}

// socketsChanged counts a member's socket opening (delta 1) or closing
// (-1), and schedules an announcement if that changes whether they're
// online.
func (d *Den) socketsChanged(member int64, delta int) {
	p := d.presence
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.sockets[member] + delta
	if n > 0 {
		p.sockets[member] = n
	} else {
		delete(p.sockets, member)
	}
	if (n > 0) != p.announced[member] && !p.pending {
		p.pending = true
		time.AfterFunc(d.PresenceDelay, d.announcePresence)
	}
}

func (d *Den) announcePresence() {
	p := d.presence
	p.mu.Lock()
	p.pending = false
	var update denproto.Presence
	for member := range p.sockets {
		if !p.announced[member] {
			p.announced[member] = true
			update.Online = append(update.Online, denproto.FormatID(member))
		}
	}
	for member := range p.announced {
		if p.sockets[member] == 0 {
			delete(p.announced, member)
			update.Offline = append(update.Offline, denproto.FormatID(member))
		}
	}
	p.mu.Unlock()
	if len(update.Online)+len(update.Offline) == 0 {
		return
	}
	slices.Sort(update.Online)
	slices.Sort(update.Offline)
	if err := d.Hub.Ephemeral(denproto.EventPresence, update, func(*Sub) bool { return true }); err != nil {
		d.log.Errorf("announce presence: %v", err)
	}
}

// online lists the members with an open socket, for snapshots.
func (d *Den) online() []string {
	p := d.presence
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.sockets))
	for member := range p.sockets {
		out = append(out, denproto.FormatID(member))
	}
	slices.Sort(out)
	return out
}

// focus records which of the channels a connection names its member can
// see; typing in those reaches it.
func (d *Den) focus(ctx context.Context, sub *Sub, channels []string) {
	staff := d.Hub.Staff(sub)
	shown := map[string]bool{}
	for _, id := range channels {
		cid, err := denproto.ParseID(id)
		if err != nil || shown[id] {
			continue
		}
		c, err := d.channel(ctx, d.db, cid)
		if err == nil && visible(c, sub.member, staff) {
			shown[id] = true
		}
	}
	d.Hub.SetFocus(sub, shown)
}

// typing tells the connections showing a channel that a member is typing
// there. A member types only where their own connection is looking, and
// notices past the rate limit are dropped, not refused: typing is a hint.
func (d *Den) typing(sub *Sub, channel string) {
	if !d.Hub.Focused(sub, channel) {
		return
	}
	member := sub.member
	if d.Allow(LimitTyping, denproto.FormatID(member)+":"+channel) != nil {
		return
	}
	notice := denproto.Typing{ChannelID: channel, MemberID: denproto.FormatID(member)}
	if err := d.Hub.Ephemeral(denproto.EventTyping, notice, func(o *Sub) bool {
		return o.member != member && o.focus[channel]
	}); err != nil {
		d.log.Errorf("announce typing: %v", err)
	}
}
