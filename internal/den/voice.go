package den

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
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
// screen and watch others' (M4.2), and what sound of its soundboard plays
// in a call (M14.3), and tells members who is; the SFU carries the media
// and makes the offers. A call belongs to the socket that joined it. When
// that socket closes, the den holds the call for its device to take back
// on another (M3), unless the den closed the socket for good.
//
// From M16.2 every call is sealed end to end: each member's page seals
// what it sends under a key their Dens makes, which the den passes to each
// other member of the call sealed to their identity, and keeps the latest
// of while the call lasts. The SFU forwards what it can't open, as it
// always did, and the den takes no join that doesn't say it seals.

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
	// talking is whether the member holds push to talk, as the call's
	// members hear (M11).
	talking bool
	// sealKey is the member's latest key for the call, as their Dens sent
	// it (M16.2), which each member it holds a copy for gets as their call
	// starts or is taken back. It goes with the call.
	sealKey *denproto.VoiceKey
	// joined is when the call joined its channel, which a stretch toward a
	// live event's badge counts from (M17.5); taking a held call back keeps
	// it. It goes with the call.
	joined time.Time
}

// callRoom is a voice channel's call, and the sound that plays in it, if
// any (M14.3).
type callRoom struct {
	staffOnly bool
	members   []*call // in the order they joined
	sound     *sounding
}

// sounding is a sound of the soundboard's playing in a call: the member
// who played it, and what ends it early.
type sounding struct {
	member int64
	stop   func()
}

// pendingMove is a move staff made of a member's call (M17.2): the
// channel their page was told to join, and until when its join doesn't
// count toward their limit on joining.
type pendingMove struct {
	channel int64
	until   time.Time
}

// moveWindow is how long the den waits for the page of a member staff
// moved to join the channel it was told of.
const moveWindow = 30 * time.Second

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
	// waits holds, by member, when they may next play a sound, once their
	// last ended (M14.3), across calls.
	waits map[int64]time.Time
	// moves holds, by member, the move staff made of their call whose join
	// hasn't come yet (M17.2).
	moves map[int64]pendingMove
}

func newCallRegistry() callRegistry {
	return callRegistry{byMember: map[int64]*call{}, rooms: map[int64]*callRoom{},
		staffMuted: map[int64]bool{}, ended: map[string]endedCall{}, waits: map[int64]time.Time{}, moves: map[int64]pendingMove{}}
}

// StartCalls runs calls on the den's media ports. The SFU's notes on each
// call go to the den's log at debug, unless cfg sends them elsewhere.
func (d *Den) StartCalls(udp *net.UDPConn, tcp net.Listener, cfg sfu.Config) error {
	if cfg.Debugf == nil {
		cfg.Debugf = d.log.Debugf
	}
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
// stopping, so nobody is told, and staff mutes go as they do when it
// restarts.
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
	clear(r.staffMuted)
	clear(r.ended)
	clear(r.waits)
	clear(r.moves)
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
	// A page that doesn't seal would send in the clear what every other
	// page drops, and hear nothing (M16.2). A resume takes back only a call
	// the den holds, which was joined sealed.
	if !req.Sealed && !req.Resume {
		refuse(denproto.VoiceUpdateNeeded)
		return
	}
	// A resume makes no peer, so only joins count against the limit, and
	// not the join a move asked for (M17.2).
	if !req.Resume && !d.takeMove(s.MemberID, req.ChannelID) && d.Allow(LimitCall, strconv.FormatInt(s.MemberID, 10)) != nil {
		refuse(denproto.VoiceRateLimited)
		return
	}
	// A call that starts may earn a live event's badge once it's lasted a
	// stretch (M17.5): the den's timer looks then, its lock taken once the
	// registry's is let go.
	var awardBy time.Time
	defer func() {
		if !awardBy.IsZero() {
			d.dueAt(awardBy.Add(d.badgeStretch()).UnixMilli())
		}
	}()
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
	nc := &call{member: s.MemberID, channel: cid, key: sock.keyID, staff: d.Hub.Staff(sub), muted: req.Muted, deafened: req.Deafened,
		joined: d.now()}
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
	d.publishTalkingLocked(cid, sock)
	d.publishSoundingLocked(cid, sock)
	// As when they move their call to another device, the others' keys may
	// hold a copy for them already (M16.2).
	d.passKeysLocked(nc)
	awardBy = nc.joined
	d.log.Infof("Member %d joined the call in channel %d", s.MemberID, cid)
}

// takeMove reports whether staff moved the member to a channel whose
// join hasn't come yet, and forgets the move, which the join answers.
func (d *Den) takeMove(member int64, channel string) bool {
	cid, err := denproto.ParseID(channel)
	if err != nil {
		return false
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	mv, ok := r.moves[member]
	if !ok || mv.channel != cid {
		return false
	}
	delete(r.moves, member)
	return d.now().Before(mv.until)
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
		// Who held push to talk, and what sound plays, may have changed
		// while the socket was away, and the others' keys (M16.2).
		d.publishTalkingLocked(c.channel, sock)
		d.publishSoundingLocked(c.channel, sock)
		d.passKeysLocked(c)
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
	if c.muted {
		d.stopTalkingLocked(c)
	}
	d.publishCallsLocked(c.channel)
}

// talkCall lights the push-to-talk ring of the member whose call this
// socket holds, for the call's members, or puts it out (M11). A press while
// the member is muted or staff muted, or past TalksPerSecond, lights
// nothing; a release always puts a lit ring out.
func (d *Den) talkCall(sock *socket, req denproto.VoiceTalk) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock || c.talking == req.On {
		return
	}
	if !req.On {
		d.stopTalkingLocked(c)
		return
	}
	if c.muted || r.staffMuted[c.member] || d.Allow(LimitTalk, strconv.FormatInt(c.member, 10)) != nil {
		return
	}
	c.talking = true
	d.publishTalkingLocked(c.channel, nil)
}

// stopTalkingLocked puts a member's push-to-talk ring out, if it's lit, and
// tells their call. The caller holds the registry's lock.
func (d *Den) stopTalkingLocked(c *call) {
	if !c.talking {
		return
	}
	c.talking = false
	d.publishTalkingLocked(c.channel, nil)
}

// publishTalkingLocked tells the members of a channel's call who holds push
// to talk in it (M11): on every socket that holds a call in it, or only on
// to. Only the call's members hear, as only their pages ring for speech.
// The caller holds the registry's lock.
func (d *Den) publishTalkingLocked(cid int64, to *socket) {
	room := d.calls.rooms[cid]
	if room == nil {
		return
	}
	t := denproto.VoiceTalking{ChannelID: denproto.FormatID(cid), Members: []string{}}
	subs := map[*Sub]bool{}
	for _, x := range room.members {
		if x.talking {
			t.Members = append(t.Members, denproto.FormatID(x.member))
		}
		if sock := x.sock.Load(); sock != nil && (to == nil || sock == to) {
			subs[sock.sub] = true
		}
	}
	if len(subs) == 0 {
		return
	}
	if err := d.Hub.Ephemeral(denproto.EventVoiceTalking, t, func(sub *Sub) bool { return subs[sub] }); err != nil {
		d.log.Errorf("announce push to talk: %v", err)
	}
}

// relayKey passes a member's key for their call to each other member of
// it the key holds a copy for, each their copy alone, and keeps it as the
// member's latest while the call lasts (M16.2). A key for a call the
// socket doesn't hold, or past the member's limit, goes nowhere. The den
// checks its shape alone: each member's Dens checks the rest against the
// identity it kept.
func (d *Den) relayKey(s *Session, sock *socket, req denproto.VoiceKey) {
	if d.Allow(LimitVoiceKey, strconv.FormatInt(s.MemberID, 10)) != nil {
		return
	}
	info, _ := d.Info()
	if err := denproto.CheckVoiceKey(info.ID, denproto.FormatID(s.MemberID), req); err != nil {
		d.log.Debugf("A key from member %d for a call was refused: %v", s.MemberID, err)
		return
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.byMember[sock.member]
	if c == nil || c.sock.Load() != sock || denproto.FormatID(c.channel) != req.ChannelID {
		return
	}
	c.sealKey = &req
	passed := 0
	if room := r.rooms[c.channel]; room != nil {
		for _, x := range room.members {
			if x != c && d.passKeyLocked(c, x) {
				passed++
			}
		}
	}
	d.log.Debugf("Member %d's key %s for the call in channel %d went to %d of its members", c.member, req.KID, c.channel, passed)
}

// passKeyLocked sends a member of a call their copy of another member's
// latest key, if it holds one for them, on the socket that holds their
// call, and reports whether it did. A held call gets it as it's taken back.
// The caller holds the registry's lock.
func (d *Den) passKeyLocked(from, to *call) bool {
	if from.sealKey == nil {
		return false
	}
	sock := to.sock.Load()
	copied := from.sealKey.Copy(denproto.FormatID(to.member))
	if sock == nil || copied == nil {
		return false
	}
	sub := sock.sub
	e := from.sealKey.Event(denproto.FormatID(from.member), copied)
	if err := d.Hub.Ephemeral(denproto.EventVoiceKey, e, func(x *Sub) bool { return x == sub }); err != nil {
		d.log.Errorf("pass a call's key: %v", err)
		return false
	}
	return true
}

// passKeysLocked sends a member whose call starts or is taken back each
// other member's latest key that holds a copy for them. The caller holds
// the registry's lock.
func (d *Den) passKeysLocked(c *call) {
	room := d.calls.rooms[c.channel]
	if room == nil {
		return
	}
	for _, x := range room.members {
		if x != c {
			d.passKeyLocked(x, c)
		}
	}
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
	// The member's page can't let go of push to talk while it's away.
	d.stopTalkingLocked(c)
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
// its sections in the others' calls, its share's among them. A sound the
// member played ends as they leave (M14.3). The caller holds the
// registry's lock, and publishes the change.
func (d *Den) dropCallLocked(c *call) {
	r := &d.calls
	delete(r.byMember, c.member)
	if room := r.rooms[c.channel]; room != nil {
		room.members = slices.DeleteFunc(room.members, func(x *call) bool { return x == c })
		if c.sharing {
			unwatchedLocked(room, c.member)
		}
		if room.sound != nil && room.sound.member == c.member {
			d.endSoundLocked(c.channel, room)
		}
	}
	d.stopTalkingLocked(c)
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

// soundCall plays one of the den's sounds in the call this socket holds
// (M14.3), or tells the socket why not: the soundboard is off, staff muted
// the member, a sound plays in the call, or the member's wait after their
// last hasn't passed. One sound plays in a call at a time, since mixing
// two would take re-encoding, and plays to its end, unless its player
// leaves the call, or staff mute or disconnect them, so a long one can't
// be used to wear a call down.
func (d *Den) soundCall(ctx context.Context, s *Session, sock *socket, req denproto.VoiceSound) {
	limited := d.Allow(LimitWrite, strconv.FormatInt(s.MemberID, 10)) != nil
	c := d.socketCall(sock)
	if c == nil {
		return
	}
	refuse := func(reason string) {
		sock.voice(denproto.EventVoiceRefused, denproto.VoiceRefused{ChannelID: denproto.FormatID(c.channel),
			What: denproto.RefusedSound, SoundID: req.SoundID, Reason: reason})
	}
	if limited {
		refuse(denproto.VoiceRateLimited)
		return
	}
	// The file opens before the registry's lock is taken, so a slow disk
	// holds up no other call; what it checks is checked again after.
	if reason := d.soundRefusal(c); reason != "" {
		refuse(reason)
		return
	}
	sound, err := d.openSound(ctx, req.SoundID)
	if errors.Is(err, errSoundNotFound) {
		refuse(denproto.VoiceNotFound)
		return
	}
	if err != nil {
		d.log.Errorf("play a sound: %v", err)
		refuse(denproto.VoiceFailed)
		return
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	reason := d.soundRefusalLocked(c)
	if r.byMember[c.member] != c || c.sock.Load() != sock || r.sfu == nil {
		reason = denproto.VoiceFailed
	}
	if reason != "" {
		sound.Close()
		refuse(reason)
		return
	}
	room := r.rooms[c.channel]
	playing := &sounding{member: c.member}
	cid := c.channel
	stop, err := r.sfu.Play(denproto.FormatID(cid), sound, func() { d.soundEnded(cid, playing) })
	if err != nil {
		d.log.Errorf("play a sound: %v", err)
		refuse(denproto.VoiceFailed)
		return
	}
	playing.stop = stop
	room.sound = playing
	d.publishSoundingLocked(cid, nil)
	d.log.Infof("Member %d played sound %s in channel %d", c.member, req.SoundID, cid)
}

// soundRefusal says why a member's call can't play a sound now, or "".
func (d *Den) soundRefusal(c *call) string {
	d.calls.mu.Lock()
	defer d.calls.mu.Unlock()
	return d.soundRefusalLocked(c)
}

// soundRefusalLocked is soundRefusal with the registry's lock held.
func (d *Den) soundRefusalLocked(c *call) string {
	r := &d.calls
	info, _ := d.Info()
	room := r.rooms[c.channel]
	switch {
	case info.SoundLimits.Sounds == 0:
		return denproto.RefusedOff
	case r.staffMuted[c.member]:
		return denproto.RefusedStaffMuted
	case room == nil:
		return denproto.VoiceFailed
	case room.sound != nil:
		return denproto.RefusedPlaying
	case d.now().Before(r.waits[c.member]):
		return denproto.RefusedWait
	}
	return ""
}

// soundEnded hears from the SFU that a sound is over, played to its end or
// stopped, and ends it in its call, unless the den has already.
func (d *Den) soundEnded(cid int64, playing *sounding) {
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	if room := r.rooms[cid]; room != nil && room.sound == playing {
		d.endSoundLocked(cid, room)
	}
}

// endSoundLocked ends the sound that plays in a call, which starts its
// player's wait, and tells the call. The caller holds the registry's lock.
func (d *Den) endSoundLocked(cid int64, room *callRoom) {
	r := &d.calls
	playing := room.sound
	room.sound = nil
	playing.stop()
	now := d.now()
	for member, until := range r.waits {
		if !now.Before(until) {
			delete(r.waits, member)
		}
	}
	info, _ := d.Info()
	if wait := time.Duration(info.SoundLimits.Wait) * time.Second; wait > 0 {
		r.waits[playing.member] = now.Add(wait)
	}
	d.publishSoundingLocked(cid, nil)
}

// publishSoundingLocked tells the members of a channel's call whose sound
// plays in it, or that none does (M14.3): on every socket that holds a call
// in it, or only on to, as publishTalkingLocked does. The caller holds the
// registry's lock.
func (d *Den) publishSoundingLocked(cid int64, to *socket) {
	room := d.calls.rooms[cid]
	if room == nil {
		return
	}
	e := denproto.VoiceSounding{ChannelID: denproto.FormatID(cid)}
	if room.sound != nil {
		e.MemberID = denproto.FormatID(room.sound.member)
	}
	subs := map[*Sub]bool{}
	for _, x := range room.members {
		if sock := x.sock.Load(); sock != nil && (to == nil || sock == to) {
			subs[sock.sub] = true
		}
	}
	if len(subs) == 0 {
		return
	}
	if err := d.Hub.Ephemeral(denproto.EventVoiceSounding, e, func(sub *Sub) bool { return subs[sub] }); err != nil {
		d.log.Errorf("announce a sound: %v", err)
	}
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

// MoveMember moves a member's call to another voice channel at staff's
// word (M17.2). The den checks what disconnecting checks, that staff rank
// above them, and what joining checks, that the member sees the channel
// and its call has room; then it tells the socket holding their call,
// whose page joins the channel as if they'd picked it, which ends the old
// call. A call held while its socket is away has nothing to tell, so it's
// refused. A member in no call, or in that channel's already, stays as
// they are.
func (d *Den) MoveMember(ctx context.Context, s *Session, id string, req denproto.MoveRequest) error {
	mid, err := d.staffOver(ctx, s, id)
	if err != nil {
		return err
	}
	cid, err := denproto.ParseID(req.ChannelID)
	if err != nil {
		return errChannelNotFound
	}
	c, err := d.channel(ctx, d.db, cid)
	if err != nil {
		return err
	}
	if c.Kind != denproto.KindVoice || !visible(c, s.MemberID, IsStaff(s.Role)) {
		return errChannelNotFound
	}
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	mc := r.byMember[mid]
	if mc == nil || mc.channel == cid {
		return nil
	}
	if !visible(c, mid, mc.staff) {
		return errChannelNotFound
	}
	sock := mc.sock.Load()
	if sock == nil {
		return denproto.Errorf(http.StatusConflict, denproto.CodeCallHeld, "their call is held while their connection is away")
	}
	// The member leaves one call for the other, so the den's calls hold as
	// many as before.
	info, _ := d.Info()
	if room := r.rooms[cid]; room != nil && len(room.members) >= info.CallLimits.Members {
		return denproto.Errorf(http.StatusConflict, denproto.CodeCallFull, "that call is full")
	}
	now := d.now()
	for member, mv := range r.moves {
		if !now.Before(mv.until) {
			delete(r.moves, member)
		}
	}
	r.moves[mid] = pendingMove{channel: cid, until: now.Add(moveWindow)}
	sock.voice(denproto.EventVoiceMove, denproto.VoiceMove{ChannelID: denproto.FormatID(mc.channel), To: denproto.FormatID(cid)})
	d.log.Infof("Member %d moved member %d from the call in channel %d to channel %d", s.MemberID, mid, mc.channel, cid)
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
			// included, so their share ends rather than freezing (M4.2),
			// and so does a sound they played (M14.3).
			d.endShareLocked(c)
			d.stopTalkingLocked(c)
			if room := r.rooms[c.channel]; room != nil && room.sound != nil && room.sound.member == mid {
				d.endSoundLocked(c.channel, room)
			}
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
