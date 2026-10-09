package den

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"errors"
	"net"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
)

// A call joins one member's browser to the den's SFU, in a voice channel
// (M2). The den decides who may be in which call, and who may share their
// screen and watch others' (M4.2), and tells members who is; the SFU
// carries the media and makes the offers. A call belongs to the socket
// that joined it. When that socket closes, the den holds the call for its
// device to take back on another (M3), unless the den closed the socket
// for good.

type call struct {
	member  int64
	channel int64
	key     []byte // the device that holds the call
	// sock is the socket the call's signaling goes to, or nil while the
	// call is held. The SFU's signal reads it without the registry's lock.
	sock  atomic.Pointer[socket]
	staff bool // whether the member is staff, which a held call can't ask its socket
	peer  *sfu.Peer
	// The member's own marks: muted, and deafened, hearing nothing (M3.3).
	muted, deafened bool
	hold            *time.Timer // ends a held call that isn't taken back
	// sharing is whether the member shares their screen, with sound if
	// sound, and watching whose shares they watch, in the order they
	// started (M4.2).
	sharing, sound bool
	watching       []int64
}

// callRoom is a voice channel's call.
type callRoom struct {
	staffOnly bool
	members   []*call // in the order they joined
}

// endedCall is why a held call ended, for its device's resume.
type endedCall struct {
	channel int64
	reason  string
	at      time.Time
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
	// staffMuted holds the members staff muted, in calls or not, until staff
	// lift it, the member leaves, or the den restarts.
	staffMuted map[int64]bool
	// ended remembers, by device, why a held call ended.
	ended map[string]endedCall
}

func newCallRegistry() callRegistry {
	return callRegistry{byMember: map[int64]*call{}, rooms: map[int64]*callRoom{},
		staffMuted: map[int64]bool{}, ended: map[string]endedCall{}}
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
	for _, c := range r.byMember {
		if c.hold != nil {
			c.hold.Stop()
		}
	}
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
	// A held call makes no offers, so there's always a socket; this guards
	// the moment it's taken back.
	if sock := s.c.sock.Load(); sock != nil {
		sock.voice(denproto.EventVoiceOffer, denproto.VoiceOffer{ChannelID: denproto.FormatID(s.c.channel),
			Version: version, SDP: sdp, UDPPort: s.udpPort, TCPPort: s.tcpPort})
	}
}

func (s callSignal) Failed() { s.d.endCall(s.c, denproto.VoiceFailed) }

// joinCall starts the member's call on this socket, in a voice channel
// they can see, and ends any other call of theirs. With resume, it takes
// back the call the den holds for this device instead.
func (d *Den) joinCall(ctx context.Context, s *Session, sock *socket, sub *Sub, req denproto.VoiceJoin) {
	refuse := func(reason string) {
		sock.voice(denproto.EventVoiceEnded, denproto.VoiceEnded{ChannelID: req.ChannelID, Reason: reason})
	}
	// A resume makes no peer, so only joins count against the limit.
	if !req.Resume && d.Allow(LimitCall, strconv.FormatInt(s.MemberID, 10)) != nil {
		refuse(denproto.VoiceRateLimited)
		return
	}
	cid, err := denproto.ParseID(req.ChannelID)
	if err != nil {
		refuse(denproto.VoiceNotFound)
		return
	}
	if req.Resume {
		d.resumeCall(sock, cid, req.ChannelID)
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
	info, _ := d.Info()
	if in >= info.CallLimits.Members || total >= info.CallLimits.Callers {
		refuse(denproto.VoiceFull)
		return
	}
	changed := []int64{cid}
	if old != nil {
		d.dropCallLocked(old)
		// The socket that asked knows its old call is over, and a held one
		// has no socket to tell.
		if os := old.sock.Load(); os != nil && os != sock {
			os.voice(denproto.EventVoiceEnded, denproto.VoiceEnded{ChannelID: denproto.FormatID(old.channel), Reason: denproto.VoiceMoved})
		}
		changed = append(changed, old.channel)
	}
	nc := &call{member: s.MemberID, channel: cid, key: sock.keyID, staff: d.Hub.Staff(sub), muted: req.Muted, deafened: req.Deafened}
	nc.sock.Store(sock)
	bitrate := c.Bitrate
	if bitrate == 0 {
		bitrate = denproto.DefaultVoiceBitrate
	}
	peer, err := r.sfu.Join(denproto.FormatID(cid), denproto.FormatID(s.MemberID), bitrate, callSignal{d, nc, r.udpPort, r.tcpPort})
	if err != nil {
		d.log.Errorf("join call: %v", err)
		refuse(denproto.VoiceFailed)
		d.publishCallsLocked(changed...)
		return
	}
	peer.SetMuted(r.staffMuted[s.MemberID])
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

// resumeCall moves the call the den holds for this socket's device, in
// that channel, to this socket, or tells the socket why there's none. A
// resume never starts a call.
func (d *Den) resumeCall(sock *socket, cid int64, channel string) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c != nil && c.channel == cid && bytes.Equal(c.key, sock.keyID) && c.sock.Load() != sock {
		if c.hold != nil {
			c.hold.Stop()
			c.hold = nil
		}
		c.sock.Store(sock)
		// Queued before the offer the peer may send again, which follows.
		sock.voice(denproto.EventVoiceResumed, denproto.VoiceResumed{ChannelID: channel})
		c.peer.Resume()
		d.log.Infof("The call of member %d in channel %d resumed", c.member, c.channel)
		return
	}
	reason := r.recall(sock.keyID, cid, d.now())
	if reason == "" {
		reason = denproto.VoiceFailed
	}
	sock.voice(denproto.EventVoiceEnded, denproto.VoiceEnded{ChannelID: channel, Reason: reason})
}

// answerCall passes the client's answer to the call this socket holds.
// One that doesn't fit its offer fails the call.
func (d *Den) answerCall(sock *socket, req denproto.VoiceAnswer) {
	if c := d.socketCall(sock); c != nil {
		c.peer.Answer(req.Version, req.SDP)
	}
}

// restartCall has the next offer of the call this socket holds restart
// ICE, as when the client's media connection broke.
func (d *Den) restartCall(sock *socket) {
	if c := d.socketCall(sock); c != nil {
		c.peer.Restart()
	}
}

// socketCall returns the call this socket holds, if any.
func (d *Den) socketCall(sock *socket) *call {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock {
		return nil
	}
	return c
}

// muteCall sets the mark of the member whose call this socket holds.
func (d *Den) muteCall(sock *socket, marks denproto.VoiceMute) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock || (c.muted == marks.Muted && c.deafened == marks.Deafened) {
		return
	}
	c.muted, c.deafened = marks.Muted, marks.Deafened
	d.publishCallsLocked(c.channel)
}

// leaveCall ends the call this socket holds, if any: the member left it.
func (d *Den) leaveCall(sock *socket) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock {
		return
	}
	d.dropCallLocked(c)
	d.publishCallsLocked(c.channel)
	d.log.Infof("Member %d left the call in channel %d", c.member, c.channel)
}

// socketClosed holds the call a closed socket carried, for its device to
// take back within the hold, and ends it if the den closed the socket for
// good, as when the member left, was removed or banned, or the device
// signed out. The call's media goes on while it's held.
func (d *Den) socketClosed(sock *socket) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock {
		return
	}
	if sock.forGood.Load() {
		d.dropCallLocked(c)
		d.publishCallsLocked(c.channel)
		d.log.Infof("The call of member %d in channel %d ended with its socket", c.member, c.channel)
		return
	}
	c.sock.Store(nil)
	c.peer.Hold()
	c.hold = time.AfterFunc(d.CallHold, func() { d.endHeld(c) })
	d.log.Infof("The call of member %d in channel %d is held", c.member, c.channel)
}

// endHeld ends a held call its device didn't take back in time.
func (d *Den) endHeld(c *call) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byMember[c.member] != c || c.sock.Load() != nil {
		return
	}
	d.endCallLocked(c, denproto.VoiceFailed)
	d.publishCallsLocked(c.channel)
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

// endCallLocked ends a call, and tells its socket why, or for a held call
// keeps the reason for its device's resume.
func (d *Den) endCallLocked(c *call, reason string) {
	d.dropCallLocked(c)
	if sock := c.sock.Load(); sock != nil {
		sock.voice(denproto.EventVoiceEnded, denproto.VoiceEnded{ChannelID: denproto.FormatID(c.channel), Reason: reason})
	} else {
		d.calls.remember(c.key, c.channel, reason, d.now())
	}
	d.log.Infof("The call of member %d in channel %d ended: %s", c.member, c.channel, reason)
}

// dropCallLocked takes a call out and hangs up its peer, which retires
// its sections in the others' calls, its share's among them. The caller
// holds the registry's lock, and publishes the change.
func (d *Den) dropCallLocked(c *call) {
	r := &d.calls
	delete(r.byMember, c.member)
	if room := r.rooms[c.channel]; room != nil {
		room.members = slices.DeleteFunc(room.members, func(x *call) bool { return x == c })
		if c.sharing {
			unwatchedLocked(room, c.member)
		}
	}
	if c.hold != nil {
		c.hold.Stop()
		c.hold = nil
	}
	c.peer.Close()
}

// shareCall starts or ends the share of the member whose call this socket
// holds (M4.2), within the den's limits, or tells the socket why not.
func (d *Den) shareCall(s *Session, sock *socket, req denproto.VoiceShare) {
	limited := d.Allow(LimitWrite, strconv.FormatInt(s.MemberID, 10)) != nil
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock {
		return
	}
	refuse := func(reason string) {
		sock.voice(denproto.EventVoiceRefused, denproto.VoiceRefused{ChannelID: denproto.FormatID(c.channel),
			What: denproto.RefusedShare, Reason: reason})
	}
	if limited {
		refuse(denproto.VoiceRateLimited)
		return
	}
	if !req.On {
		if c.sharing {
			d.endShareLocked(c)
			d.publishCallsLocked(c.channel)
		}
		return
	}
	info, _ := d.Info()
	limits := info.CallLimits
	switch {
	case limits.Shares == 0:
		refuse(denproto.RefusedOff)
	case r.staffMuted[c.member]:
		refuse(denproto.RefusedStaffMuted)
	case !c.sharing && r.sharingLocked() >= limits.Shares:
		refuse(denproto.RefusedFull)
	default:
		if err := c.peer.Share(req.Sound, limits.ShareBitrate); err != nil {
			d.log.Errorf("share: %v", err)
			return
		}
		started := !c.sharing
		c.sharing, c.sound = true, req.Sound
		d.publishCallsLocked(c.channel)
		if started {
			d.log.Infof("Member %d started sharing in channel %d", c.member, c.channel)
		}
	}
}

// watchCall starts or stops this socket's member watching another
// member's share in their call (M4.2), within the den's limits, or tells
// the socket why not.
func (d *Den) watchCall(s *Session, sock *socket, req denproto.VoiceWatch) {
	limited := d.Allow(LimitWrite, strconv.FormatInt(s.MemberID, 10)) != nil
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock {
		return
	}
	refuse := func(reason string) {
		sock.voice(denproto.EventVoiceRefused, denproto.VoiceRefused{ChannelID: denproto.FormatID(c.channel),
			What: denproto.RefusedWatch, MemberID: req.MemberID, Reason: reason})
	}
	if limited {
		refuse(denproto.VoiceRateLimited)
		return
	}
	mid, err := denproto.ParseID(req.MemberID)
	watching := err == nil && slices.Contains(c.watching, mid)
	if !req.On {
		if watching {
			c.peer.Unwatch(denproto.FormatID(mid))
			c.watching = slices.DeleteFunc(c.watching, func(x int64) bool { return x == mid })
			d.publishCallsLocked(c.channel)
		}
		return
	}
	if watching {
		return
	}
	sharer := r.byMember[mid]
	if err != nil || sharer == nil || sharer == c || sharer.channel != c.channel || !sharer.sharing {
		refuse(denproto.RefusedNotSharing)
		return
	}
	info, _ := d.Info()
	if len(c.watching) >= denproto.MaxWatching || r.viewersLocked(sharer) >= info.CallLimits.ShareViewers {
		refuse(denproto.RefusedFull)
		return
	}
	if err := c.peer.Watch(sharer.peer); err != nil {
		d.log.Warnf("watch: %v", err)
		refuse(denproto.RefusedNotSharing)
		return
	}
	c.watching = append(c.watching, mid)
	d.publishCallsLocked(c.channel)
}

// endShareLocked ends a member's share: the SFU stops forwarding it and
// retires its viewers' sections, and nobody watches it any more. The
// caller holds the registry's lock, and publishes the change.
func (d *Den) endShareLocked(c *call) {
	if !c.sharing {
		return
	}
	c.sharing, c.sound = false, false
	c.peer.Unshare()
	if room := d.calls.rooms[c.channel]; room != nil {
		unwatchedLocked(room, c.member)
	}
	d.log.Infof("Member %d stopped sharing in channel %d", c.member, c.channel)
}

// unwatchedLocked takes a member whose share ended out of everyone's
// watching in their call.
func unwatchedLocked(room *callRoom, sharer int64) {
	for _, x := range room.members {
		x.watching = slices.DeleteFunc(x.watching, func(m int64) bool { return m == sharer })
	}
}

// sharingLocked counts the members sharing across the den.
func (r *callRegistry) sharingLocked() int {
	n := 0
	for _, c := range r.byMember {
		if c.sharing {
			n++
		}
	}
	return n
}

// viewersLocked counts the members watching a member's share.
func (r *callRegistry) viewersLocked(sharer *call) int {
	n := 0
	if room := r.rooms[sharer.channel]; room != nil {
		for _, x := range room.members {
			if slices.Contains(x.watching, sharer.member) {
				n++
			}
		}
	}
	return n
}

// callBitrateChanged has the call in a voice channel go at the channel's
// new bitrate, from each member's next offer, which goes now (M4.2).
func (d *Den) callBitrateChanged(c denproto.Channel) {
	cid, err := denproto.ParseID(c.ID)
	if err != nil || c.Bitrate == 0 {
		return
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if room := r.rooms[cid]; room != nil {
		for _, x := range room.members {
			x.peer.SetVoiceBitrate(c.Bitrate)
		}
	}
}

// remember keeps why a held call ended, for its device's resume, and lets
// go of what's past remembering.
func (r *callRegistry) remember(key []byte, channel int64, reason string, now time.Time) {
	for k, e := range r.ended {
		if now.Sub(e.at) > denproto.CallEndRemembered {
			delete(r.ended, k)
		}
	}
	r.ended[string(key)] = endedCall{channel: channel, reason: reason, at: now}
}

// recall returns, once, why a device's held call in a channel ended, or ""
// if that's past remembering or never happened.
func (r *callRegistry) recall(key []byte, channel int64, now time.Time) string {
	e, ok := r.ended[string(key)]
	delete(r.ended, string(key))
	if !ok || e.channel != channel || now.Sub(e.at) > denproto.CallEndRemembered {
		return ""
	}
	return e.reason
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
		case c.StaffOnly && !x.staff:
			d.endCallLocked(x, denproto.VoiceForbidden)
		}
	}
	d.publishCallsLocked(cid)
}

// roleChanged ends a member's call in a staff-only channel once they're no
// longer staff. A held call has no socket whose subscription knows the new
// role, so the call keeps its own.
func (d *Den) roleChanged(member int64, staff bool) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[member]
	if c == nil {
		return
	}
	c.staff = staff
	if room := r.rooms[c.channel]; room != nil && room.staffOnly && !staff {
		d.endCallLocked(c, denproto.VoiceForbidden)
		d.publishCallsLocked(c.channel)
	}
}

// memberGone ends the call of a member who left or was removed, held or
// not, and forgets their staff mute.
func (d *Den) memberGone(member int64) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.staffMuted, member)
	if c := r.byMember[member]; c != nil {
		d.endCallLocked(c, denproto.VoiceForbidden)
		d.publishCallsLocked(c.channel)
	}
}

// devicesSignedOut ends the calls of devices whose keys were just deleted,
// held or not.
func (d *Den) devicesSignedOut(keys [][]byte) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.byMember {
		for _, key := range keys {
			if bytes.Equal(c.key, key) {
				d.endCallLocked(c, denproto.VoiceForbidden)
				d.publishCallsLocked(c.channel)
				break
			}
		}
	}
}

// staffOver returns the member staff act on, after checking that staff
// rank above them, as removal does: moderators act on members, and the
// owner on anyone else.
func (d *Den) staffOver(ctx context.Context, s *Session, id string) (int64, error) {
	if !IsStaff(s.Role) {
		return 0, forbidden("only moderators and the owner do that")
	}
	mid, err := denproto.ParseID(id)
	if err != nil {
		return 0, errMemberNotFound
	}
	var role string
	err = d.db.QueryRowContext(ctx, `SELECT role FROM den_members WHERE id = ? AND left_at IS NULL`, mid).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errMemberNotFound
	}
	if err != nil {
		return 0, err
	}
	if denproto.Rank(role) >= denproto.Rank(s.Role) {
		return 0, forbidden("only the owner acts on moderators, and nobody on the owner")
	}
	return mid, nil
}

// DisconnectFromCall ends a member's call, held or not, at staff's word.
// A member in no call stays as they are.
func (d *Den) DisconnectFromCall(ctx context.Context, s *Session, id string) error {
	mid, err := d.staffOver(ctx, s, id)
	if err != nil {
		return err
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.byMember[mid]; c != nil {
		d.endCallLocked(c, denproto.VoiceDisconnectedByStaff)
		d.publishCallsLocked(c.channel)
		d.log.Infof("Member %d disconnected member %d from the call in channel %d", s.MemberID, mid, c.channel)
	}
	return nil
}

// SetStaffMute mutes a member in calls, the one they're in and any they
// join, or lifts it. The den forwards nothing from a muted member.
func (d *Den) SetStaffMute(ctx context.Context, s *Session, id string, muted bool) error {
	mid, err := d.staffOver(ctx, s, id)
	if err != nil {
		return err
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.staffMuted[mid] == muted {
		return nil
	}
	if muted {
		r.staffMuted[mid] = true
	} else {
		delete(r.staffMuted, mid)
	}
	if c := r.byMember[mid]; c != nil {
		c.peer.SetMuted(muted)
		if muted {
			// The den forwards nothing from a muted member, their screen
			// included, so their share ends rather than freezing (M4.2).
			d.endShareLocked(c)
		}
		d.publishCallsLocked(c.channel)
	}
	d.log.Infof("Member %d set member %d's staff mute to %t", s.MemberID, mid, muted)
	return nil
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
		state := denproto.VoiceState{Calls: []denproto.Call{r.callOf(cid, room)}}
		staffOnly := room.staffOnly
		if err := d.Hub.Ephemeral(denproto.EventVoiceState, state, func(sub *Sub) bool { return !staffOnly || sub.staff }); err != nil {
			d.log.Errorf("announce call: %v", err)
		}
		if len(room.members) == 0 {
			delete(r.rooms, cid)
		}
	}
}

func (r *callRegistry) callOf(cid int64, room *callRoom) denproto.Call {
	c := denproto.Call{ChannelID: denproto.FormatID(cid), Members: []denproto.CallMember{}}
	for _, x := range room.members {
		m := denproto.CallMember{ID: denproto.FormatID(x.member), Muted: x.muted, Deafened: x.deafened,
			StaffMuted: r.staffMuted[x.member], Sharing: x.sharing, Sound: x.sound}
		for _, w := range x.watching {
			m.Watching = append(m.Watching, denproto.FormatID(w))
		}
		c.Members = append(c.Members, m)
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
			out = append(out, r.callOf(cid, room))
		}
	}
	slices.SortFunc(out, func(a, b denproto.Call) int {
		x, _ := denproto.ParseID(a.ChannelID)
		y, _ := denproto.ParseID(b.ChannelID)
		return cmp.Compare(x, y)
	})
	return out
}
