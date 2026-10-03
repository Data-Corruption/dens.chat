package den

import (
	"cmp"
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
)

// A call joins one member's browser to the den's SFU, in a voice channel
// (M2). The den decides who may be in which call and tells members who
// is; the SFU carries the media and makes the offers. A call belongs to
// the socket that joined it, and ends with it.

type call struct {
	member  int64
	channel int64
	sock    *socket
	sub     *Sub
	peer    *sfu.Peer
	muted   bool
}

// callRoom is a voice channel's call.
type callRoom struct {
	staffOnly bool
	members   []*call // in the order they joined
}

type callRegistry struct {
	// mu orders changes to calls, and the voice.state events that tell of
	// them. It's taken before the SFU's locks and the hub's.
	mu       sync.Mutex
	sfu      *sfu.SFU // nil until StartCalls
	udpPort  int
	tcpPort  int
	byMember map[int64]*call
	rooms    map[int64]*callRoom
}

func newCallRegistry() callRegistry {
	return callRegistry{byMember: map[int64]*call{}, rooms: map[int64]*callRoom{}}
}

// StartCalls runs calls on the den's media ports.
func (d *Den) StartCalls(udp *net.UDPConn, tcp net.Listener, cfg sfu.Config) error {
	s, err := sfu.New(udp, tcp, cfg)
	if err != nil {
		return err
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sfu = s
	r.udpPort = udp.LocalAddr().(*net.UDPAddr).Port
	r.tcpPort = tcp.Addr().(*net.TCPAddr).Port
	return nil
}

// StopCalls ends every call and closes the media ports. The den is
// stopping, so nobody is told.
func (d *Den) StopCalls() error {
	r := &d.calls
	r.mu.Lock()
	s := r.sfu
	r.sfu = nil
	clear(r.byMember)
	clear(r.rooms)
	r.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Close()
}

// callSignal carries a call's offers to its socket, and its failure to
// the den.
type callSignal struct {
	d                *Den
	c                *call
	udpPort, tcpPort int
}

func (s callSignal) Offer(version int, sdp string) {
	s.c.sock.voice(denproto.EventVoiceOffer, denproto.VoiceOffer{ChannelID: denproto.FormatID(s.c.channel),
		Version: version, SDP: sdp, UDPPort: s.udpPort, TCPPort: s.tcpPort})
}

func (s callSignal) Failed() { s.d.endCall(s.c, denproto.VoiceFailed) }

// joinCall starts the member's call on this socket, in a voice channel
// they can see, and ends any other call of theirs.
func (d *Den) joinCall(ctx context.Context, s *Session, sock *socket, sub *Sub, req denproto.VoiceJoin) {
	refuse := func(reason string) {
		sock.voice(denproto.EventVoiceEnded, denproto.VoiceEnded{ChannelID: req.ChannelID, Reason: reason})
	}
	if d.Allow(LimitCall, strconv.FormatInt(s.MemberID, 10)) != nil {
		refuse(denproto.VoiceRateLimited)
		return
	}
	cid, err := denproto.ParseID(req.ChannelID)
	if err != nil {
		refuse(denproto.VoiceNotFound)
		return
	}
	c, err := d.channel(ctx, d.db, cid)
	if err != nil && !errors.Is(err, errChannelNotFound) {
		d.log.Errorf("join call: %v", err)
		refuse(denproto.VoiceFailed)
		return
	}
	if err != nil || c.Kind != denproto.KindVoice || !visible(c, s.MemberID, d.Hub.Staff(sub)) {
		refuse(denproto.VoiceNotFound)
		return
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sfu == nil {
		refuse(denproto.VoiceFailed)
		return
	}
	old := r.byMember[s.MemberID]
	in, total := 0, len(r.byMember)
	if room := r.rooms[cid]; room != nil {
		in = len(room.members)
	}
	if old != nil {
		total--
		if old.channel == cid {
			in--
		}
	}
	if in >= denproto.MaxCallMembers || total >= denproto.MaxDenCallers {
		refuse(denproto.VoiceFull)
		return
	}
	changed := []int64{cid}
	if old != nil {
		d.dropCallLocked(old)
		// The socket that asked knows its old call is over.
		if old.sock != sock {
			old.sock.voice(denproto.EventVoiceEnded, denproto.VoiceEnded{ChannelID: denproto.FormatID(old.channel), Reason: denproto.VoiceMoved})
		}
		changed = append(changed, old.channel)
	}
	nc := &call{member: s.MemberID, channel: cid, sock: sock, sub: sub, muted: req.Muted}
	peer, err := r.sfu.Join(denproto.FormatID(cid), denproto.FormatID(s.MemberID), callSignal{d, nc, r.udpPort, r.tcpPort})
	if err != nil {
		d.log.Errorf("join call: %v", err)
		refuse(denproto.VoiceFailed)
		d.publishCallsLocked(changed...)
		return
	}
	nc.peer = peer
	r.byMember[s.MemberID] = nc
	room := r.rooms[cid]
	if room == nil {
		room = &callRoom{staffOnly: c.StaffOnly}
		r.rooms[cid] = room
	}
	room.members = append(room.members, nc)
	d.publishCallsLocked(changed...)
	d.log.Infof("Member %d joined the call in channel %d", s.MemberID, cid)
}

// answerCall passes the client's answer to the call this socket holds.
// One that doesn't fit its offer fails the call.
func (d *Den) answerCall(sock *socket, req denproto.VoiceAnswer) {
	r := &d.calls
	r.mu.Lock()
	c := r.byMember[sock.member]
	r.mu.Unlock()
	if c != nil && c.sock == sock {
		c.peer.Answer(req.Version, req.SDP)
	}
}

// muteCall sets the mark of the member whose call this socket holds.
func (d *Den) muteCall(sock *socket, muted bool) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock != sock || c.muted == muted {
		return
	}
	c.muted = muted
	d.publishCallsLocked(c.channel)
}

// leaveCall ends the call this socket holds, if any: the member left it,
// or the socket closed.
func (d *Den) leaveCall(sock *socket) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock != sock {
		return
	}
	d.dropCallLocked(c)
	d.publishCallsLocked(c.channel)
	d.log.Infof("Member %d left the call in channel %d", c.member, c.channel)
}

// endCall ends a call that's still going, and tells its socket why.
func (d *Den) endCall(c *call, reason string) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byMember[c.member] != c {
		return
	}
	d.endCallLocked(c, reason)
	d.publishCallsLocked(c.channel)
}

func (d *Den) endCallLocked(c *call, reason string) {
	d.dropCallLocked(c)
	c.sock.voice(denproto.EventVoiceEnded, denproto.VoiceEnded{ChannelID: denproto.FormatID(c.channel), Reason: reason})
	d.log.Infof("The call of member %d in channel %d ended: %s", c.member, c.channel, reason)
}

// dropCallLocked takes a call out and hangs up its peer. The caller holds
// the registry's lock, and publishes the change.
func (d *Den) dropCallLocked(c *call) {
	r := &d.calls
	delete(r.byMember, c.member)
	if room := r.rooms[c.channel]; room != nil {
		room.members = slices.DeleteFunc(room.members, func(x *call) bool { return x == c })
	}
	c.peer.Close()
}

// channelChanged ends the calls a channel's change leaves out: everyone's
// when it's deleted, and non-staff members' when it becomes staff-only.
func (d *Den) channelChanged(c denproto.Channel, deleted bool) {
	cid, err := denproto.ParseID(c.ID)
	if err != nil {
		return
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	room := r.rooms[cid]
	if room == nil {
		return
	}
	// The call's audience changes with the channel's before anyone hears
	// of it, so a call that became staff-only isn't shown to members.
	room.staffOnly = c.StaffOnly
	for _, x := range slices.Clone(room.members) {
		switch {
		case deleted:
			d.endCallLocked(x, denproto.VoiceDeleted)
		case c.StaffOnly && !d.Hub.Staff(x.sub):
			d.endCallLocked(x, denproto.VoiceForbidden)
		}
	}
	d.publishCallsLocked(cid)
}

// roleChanged ends a member's call in a staff-only channel once they're no
// longer staff. The hub already knows their new role.
func (d *Den) roleChanged(member int64) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[member]
	if c == nil {
		return
	}
	if room := r.rooms[c.channel]; room != nil && room.staffOnly && !d.Hub.Staff(c.sub) {
		d.endCallLocked(c, denproto.VoiceForbidden)
		d.publishCallsLocked(c.channel)
	}
}

// publishCallsLocked tells everyone who can see each of the channels who
// is in its call now: no one, for a call that ended.
func (d *Den) publishCallsLocked(channels ...int64) {
	r := &d.calls
	slices.Sort(channels)
	for _, cid := range slices.Compact(channels) {
		room := r.rooms[cid]
		if room == nil {
			continue
		}
		state := denproto.VoiceState{Calls: []denproto.Call{callOf(cid, room)}}
		staffOnly := room.staffOnly
		if err := d.Hub.Ephemeral(denproto.EventVoiceState, state, func(sub *Sub) bool { return !staffOnly || sub.staff }); err != nil {
			d.log.Errorf("announce call: %v", err)
		}
		if len(room.members) == 0 {
			delete(r.rooms, cid)
		}
	}
}

func callOf(cid int64, room *callRoom) denproto.Call {
	c := denproto.Call{ChannelID: denproto.FormatID(cid), Members: []denproto.CallMember{}}
	for _, x := range room.members {
		c.Members = append(c.Members, denproto.CallMember{ID: denproto.FormatID(x.member), Muted: x.muted})
	}
	return c
}

// visibleCalls lists the calls a member can see that have someone in them,
// for snapshots.
func (d *Den) visibleCalls(staff bool) []denproto.Call {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []denproto.Call{}
	for cid, room := range r.rooms {
		if len(room.members) > 0 && (staff || !room.staffOnly) {
			out = append(out, callOf(cid, room))
		}
	}
	slices.SortFunc(out, func(a, b denproto.Call) int {
		x, _ := denproto.ParseID(a.ChannelID)
		y, _ := denproto.ParseID(b.ChannelID)
		return cmp.Compare(x, y)
	})
	return out
}
