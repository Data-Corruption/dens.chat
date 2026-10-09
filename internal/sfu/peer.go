package sfu

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"golang.org/x/time/rate"
)

// What the den forwards from one member's audio at most, their voice or a
// share's sound; the rest is dropped.
const (
	maxBytesPerSecond   = 256_000 / 8
	maxPacketsPerSecond = 500
)

// A share's video goes at most a quarter past its bitrate, in bursts of up
// to a second of it or shareBurst, whichever is more, so a keyframe gets
// through whole (M4.2). Its packets are held to one for each
// bytesPerPacket of the bitrate, and at least maxPacketsPerSecond.
const (
	shareHeadroom  = 1.25
	shareBurst     = 512 << 10
	bytesPerPacket = 500
)

// keyframeGap is the least time between two keyframe requests to a
// sharer, since each keyframe goes to every viewer and is many times the
// size of other frames. A share whose packets stop for as long counts as
// paused.
const keyframeGap = 500 * time.Millisecond

// The IDs of the tracks the den sends: a member's voice, in a stream named
// for the member, and their share's screen and sound, in a stream
// screenStream names (M4.2).
const (
	voiceTrack  = "audio"
	screenTrack = "screen"
	soundTrack  = "sound"
)

// screenStream names the stream a member's share goes out in.
func screenStream(member string) string { return "screen-" + member }

// What a peer sends its member goes by another member's ID for their
// voice, and by these for the screen and sound of a share they watch.
func screenKey(member string) string { return "screen:" + member }
func soundKey(member string) string  { return "sound:" + member }

// ErrNotSharing is returned for a watch of a member who isn't sharing.
var ErrNotSharing = errors.New("the member isn't sharing")

// Signal is how a peer reaches its member's client, through the den.
// Offer is called with the peer's lock held, so it must not call back into
// the SFU; Failed is called with no lock held, once the peer has closed.
type Signal interface {
	Offer(version int, sdp string)
	Failed()
}

// Peer is one member's side of a call.
type Peer struct {
	sfu    *SFU
	room   string
	member string
	signal Signal
	pc     *webrtc.PeerConnection
	mic    *webrtc.RTPTransceiver      // receives the member's audio
	track  *webrtc.TrackLocalStaticRTP // that audio, as the others receive it
	// The member's share's screen and sound, as viewers receive them.
	screenOut, soundOut *webrtc.TrackLocalStaticRTP

	// muted drops the member's audio before it's forwarded, as a staff mute
	// does, and their share's too.
	muted atomic.Bool
	// share is the member's screen share while it goes on (M4.2), and
	// screenSSRC what their browser sends the screen under, which keyframe
	// requests name.
	share      atomic.Pointer[share]
	screenSSRC atomic.Uint32
	keyframes  keyframes

	mu        sync.Mutex
	closed    bool
	version   int
	voice     int    // the Opus bitrate voice sections ask for (M4.2)
	offer     string // the offer that's out, as sent
	waiting   bool   // an offer is out
	stale     bool   // the sections changed since that offer
	held      bool   // the member's signaling is away, so no offer goes out
	answered  bool   // the member has answered an offer
	restart   bool   // the next offer restarts ICE
	connected bool
	answerBy  *time.Timer
	connectBy *time.Timer
	// screen and sound receive the member's share, from their first share
	// in the call (M4.2), and shareBitrate is the most the screen's section
	// asks their browser for.
	screen, sound *webrtc.RTPTransceiver
	shareBitrate  int
	// sends maps what the peer sends the member to its section: another
	// member's voice by their ID, and a watched share's screen and sound by
	// screenKey and soundKey. A section whose sender left retires; once the
	// client has answered an offer that retires it, something else of its
	// kind can take it, so a long call's offers don't grow with every join.
	sends    map[string]*webrtc.RTPTransceiver
	retiring []*webrtc.RTPTransceiver // not in an offer yet
	offered  []*webrtc.RTPTransceiver // in the offer that's out
	spare    []*webrtc.RTPTransceiver
	// opening are the sharers whose screens the member's next offer opens,
	// and opened those the offer that's out opens: once the member answers
	// it, each is asked for a keyframe, so their first picture is whole.
	opening, opened []*Peer
}

// share is a member's screen share going on: whether it has sound, and
// the limits its video is held to.
type share struct {
	sound          bool
	bytes, packets *rate.Limiter
}

func newShare(sound bool, bitrate int) *share {
	perSecond := float64(bitrate) / 8
	packets := max(bitrate/8/bytesPerPacket, maxPacketsPerSecond)
	return &share{
		sound:   sound,
		bytes:   rate.NewLimiter(rate.Limit(perSecond*shareHeadroom), max(int(perSecond), shareBurst)),
		packets: rate.NewLimiter(rate.Limit(packets), packets),
	}
}

// Join adds a member to a room's call, whose voice goes at bitrate. The
// peer sends them its first offer through signal, and every other peer in
// the room a new one that adds the member's audio.
func (s *SFU) Join(room, member string, bitrate int, signal Signal) (*Peer, error) {
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	p := &Peer{sfu: s, room: room, member: member, signal: signal, pc: pc, voice: bitrate,
		sends: map[string]*webrtc.RTPTransceiver{}}
	fail := func(err error) (*Peer, error) {
		_ = pc.Close()
		return nil, err
	}
	p.mic, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		return fail(err)
	}
	if p.track, err = webrtc.NewTrackLocalStaticRTP(opus, voiceTrack, member); err != nil {
		return fail(err)
	}
	if p.screenOut, err = webrtc.NewTrackLocalStaticRTP(vp9, screenTrack, screenStream(member)); err != nil {
		return fail(err)
	}
	if p.soundOut, err = webrtc.NewTrackLocalStaticRTP(opus, soundTrack, screenStream(member)); err != nil {
		return fail(err)
	}
	pc.OnTrack(p.forward)
	pc.OnConnectionStateChange(p.stateChanged)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fail(ErrClosed)
	}
	others := s.rooms[room]
	if others[member] != nil {
		s.mu.Unlock()
		return fail(errors.New("the member is in this call already"))
	}
	for _, o := range others {
		if err := p.send(o.member, o.track, o); err != nil {
			s.mu.Unlock()
			return fail(err)
		}
	}
	var renegotiate, broken []*Peer
	for _, o := range others {
		if o.send(member, p.track, p) == nil {
			renegotiate = append(renegotiate, o)
		} else {
			broken = append(broken, o)
		}
	}
	if others == nil {
		others = map[string]*Peer{}
		s.rooms[room] = others
	}
	others[member] = p
	s.mu.Unlock()
	for _, o := range broken {
		go o.fail()
	}
	for _, o := range renegotiate {
		o.negotiate()
	}
	p.negotiate()
	return p, nil
}

// Close takes the member out of the call. It's safe to call more than once.
func (p *Peer) Close() { p.close() }

// Answer applies the client's answer to the offer with that version, and
// ignores an answer to any other. An answer that doesn't fit its offer
// fails the call.
func (p *Peer) Answer(version int, sdp string) {
	p.mu.Lock()
	if p.closed || !p.waiting || version != p.version {
		p.mu.Unlock()
		return
	}
	if len(sdp) > denproto.MaxSDP ||
		p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}) != nil {
		p.mu.Unlock()
		p.fail()
		return
	}
	p.waiting, p.answered = false, true
	p.answerBy.Stop()
	p.spare = append(p.spare, p.offered...)
	p.offered = nil
	opened := p.opened
	p.opened = nil
	if !p.connected && p.connectBy == nil {
		p.connectBy = time.AfterFunc(p.sfu.connect, p.fail)
	}
	again := p.stale
	p.mu.Unlock()
	// The member's sections for these shares send from now on, so each
	// sharer's next keyframe reaches them.
	for _, sharer := range opened {
		sharer.requestKeyframe()
	}
	if again {
		p.negotiate()
	}
}

// negotiate sends the client a new offer, or, while one is out or the
// member's signaling is away, marks it stale, so the next goes once it's
// answered or the call resumes.
func (p *Peer) negotiate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	if p.waiting || p.held {
		p.stale = true
		return
	}
	offer, err := p.pc.CreateOffer(&webrtc.OfferOptions{ICERestart: p.restart})
	if err == nil {
		err = p.pc.SetLocalDescription(offer)
	}
	if err != nil {
		go p.fail()
		return
	}
	p.version++
	p.waiting, p.stale, p.restart = true, false, false
	p.offered = append(p.offered, p.retiring...)
	p.retiring = nil
	p.opened = append(p.opened, p.opening...)
	p.opening = nil
	p.offer = shapeSections(denproto.StripCandidates(offer.SDP), p.shapes())
	p.answerBy = time.AfterFunc(p.sfu.answer, p.fail)
	p.signal.Offer(p.version, p.offer)
}

// shapes gives, by mid, what each of the member's sections asks of their
// browser: voice at the channel's bitrate, a share's sound in stereo, and
// the screen they share within its bitrate. The caller holds the peer's
// lock, after the offer has given every section its mid.
func (p *Peer) shapes() func(mid string) shape {
	byMid := map[string]shape{}
	for _, t := range p.pc.GetTransceivers() {
		mid := t.Mid()
		var out webrtc.TrackLocal
		if s := t.Sender(); s != nil {
			out = s.Track()
		}
		switch {
		case mid == "":
		case t == p.screen:
			byMid[mid] = shape{bitrate: p.shareBitrate}
		case t == p.sound || (out != nil && out.ID() == soundTrack):
			byMid[mid] = shape{opus: soundParams}
		case t.Kind() == webrtc.RTPCodecTypeAudio:
			byMid[mid] = shape{opus: voiceParams(p.voice)}
		}
	}
	return func(mid string) shape { return byMid[mid] }
}

// Hold keeps the member in the call while their signaling is away (M3):
// their media flows as before, but no offer goes out, and the one that's
// out waits without its deadline. Pion can't take an offer back, so
// Resume sends it again.
func (p *Peer) Hold() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.held {
		return
	}
	p.held = true
	if p.waiting {
		p.answerBy.Stop()
	}
}

// Resume brings the member's signaling back: the offer that was out goes
// again, under its version, which a client that answered it already
// answers again, or the offer the call's changes need goes now.
func (p *Peer) Resume() {
	p.mu.Lock()
	if p.closed || !p.held {
		p.mu.Unlock()
		return
	}
	p.held = false
	if p.waiting {
		p.answerBy = time.AfterFunc(p.sfu.answer, p.fail)
		p.signal.Offer(p.version, p.offer)
		p.mu.Unlock()
		return
	}
	again := p.stale
	p.mu.Unlock()
	if again {
		p.negotiate()
	}
}

// Restart has the peer's next offer restart ICE, with new credentials, as
// when the member's connection to the media ports broke: at once, or once
// the offer that's out is answered. DTLS carries on, so the call keeps its
// keys and sections. Before the first answer there's nothing to restart.
func (p *Peer) Restart() {
	p.mu.Lock()
	if p.closed || !p.answered {
		p.mu.Unlock()
		return
	}
	p.restart = true
	p.mu.Unlock()
	p.negotiate()
}

// SetMuted drops the member's audio and share, or forwards them again.
func (p *Peer) SetMuted(muted bool) { p.muted.Store(muted) }

// SetVoiceBitrate has the member's offers ask for another Opus bitrate for
// voice, from the next one, which goes now (M4.2).
func (p *Peer) SetVoiceBitrate(bitrate int) {
	p.mu.Lock()
	if p.closed || p.voice == bitrate {
		p.mu.Unlock()
		return
	}
	p.voice = bitrate
	p.mu.Unlock()
	p.negotiate()
}

// Share starts the member's screen share, with its sound if sound, held to
// bitrate (M4.2). Their first share in a call adds the sections the den
// receives the screen and its sound on, which stay for later shares, and
// whenever those change, or the bitrate does, an offer goes now.
func (p *Peer) Share(sound bool, bitrate int) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	changed := p.shareBitrate != bitrate
	if p.screen == nil {
		t, err := p.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
		if err != nil {
			p.mu.Unlock()
			return err
		}
		p.screen, changed = t, true
	}
	if sound && p.sound == nil {
		t, err := p.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
		if err != nil {
			p.mu.Unlock()
			return err
		}
		p.sound, changed = t, true
	}
	p.shareBitrate = bitrate
	p.share.Store(newShare(sound, bitrate))
	p.mu.Unlock()
	if changed {
		p.negotiate()
	}
	return nil
}

// Unshare ends the member's screen share: the den forwards nothing more of
// it, and the next offer of each member watching it retires its sections.
func (p *Peer) Unshare() {
	if p.share.Swap(nil) == nil {
		return
	}
	s := p.sfu
	s.mu.Lock()
	var renegotiate []*Peer
	for _, o := range s.rooms[p.room] {
		if o != p && o.unwatchLocked(p.member) {
			renegotiate = append(renegotiate, o)
		}
	}
	s.mu.Unlock()
	for _, o := range renegotiate {
		o.negotiate()
	}
}

// Watch sends the member another member's share, its screen and any
// sound, in sections their next offer opens, which goes now (M4.2). Once
// they answer it, the sharer is asked for a keyframe.
func (p *Peer) Watch(sharer *Peer) error {
	s := p.sfu
	s.mu.Lock()
	room := s.rooms[p.room]
	if p == sharer || p.room != sharer.room || room[p.member] != p || room[sharer.member] != sharer {
		s.mu.Unlock()
		return errors.New("the members aren't in one call")
	}
	sh := sharer.share.Load()
	if sh == nil {
		s.mu.Unlock()
		return ErrNotSharing
	}
	err := p.send(screenKey(sharer.member), sharer.screenOut, sharer)
	if err == nil && sh.sound {
		err = p.send(soundKey(sharer.member), sharer.soundOut, sharer)
	}
	if err == nil {
		p.mu.Lock()
		p.opening = append(p.opening, sharer)
		p.mu.Unlock()
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	p.negotiate()
	return nil
}

// Unwatch stops sending the member another member's share.
func (p *Peer) Unwatch(sharer string) {
	s := p.sfu
	s.mu.Lock()
	changed := p.unwatchLocked(sharer)
	s.mu.Unlock()
	if changed {
		p.negotiate()
	}
}

// unwatchLocked retires the sections of a member's share, and reports
// whether the peer needs a new offer. The caller holds the SFU's lock.
func (p *Peer) unwatchLocked(sharer string) bool {
	screen := p.retire(screenKey(sharer))
	sound := p.retire(soundKey(sharer))
	return screen || sound
}

// send has the peer send the member something from the peer from:
// another member's voice, or a share's screen or sound, on a spare section
// of its kind if there is one. Sending what's sent already changes
// nothing. The caller holds the SFU's lock.
func (p *Peer) send(key string, track *webrtc.TrackLocalStaticRTP, from *Peer) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if p.sends[key] != nil {
		return nil
	}
	var t *webrtc.RTPTransceiver
	if i := slices.IndexFunc(p.spare, func(x *webrtc.RTPTransceiver) bool { return x.Kind() == track.Kind() }); i >= 0 {
		t = p.spare[i]
		sender, err := p.sfu.api.NewRTPSender(track, p.mic.Receiver().Transport())
		if err != nil {
			return err
		}
		if err := t.SetSender(sender, track); err != nil {
			_ = sender.Stop()
			return err
		}
		p.spare = slices.Delete(p.spare, i, i+1)
	} else {
		var err error
		t, err = p.pc.AddTransceiverFromTrack(track,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			return err
		}
	}
	p.sends[key] = t
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		go feedback(t.Sender(), from)
	} else {
		go drain(t.Sender())
	}
	return nil
}

// retire stops sending what key names, and reports whether the peer needs
// a new offer. The caller holds the SFU's lock.
func (p *Peer) retire(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.sends[key]
	if p.closed || t == nil {
		return false
	}
	delete(p.sends, key)
	if p.pc.RemoveTrack(t.Sender()) != nil {
		return false
	}
	p.retiring = append(p.retiring, t)
	return true
}

// close takes the peer out of its call, and reports whether it was the
// one to: a peer closes once, whether the den or a failure closes it.
func (p *Peer) close() bool {
	s := p.sfu
	s.mu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		s.mu.Unlock()
		return false
	}
	p.closed = true
	for _, t := range []*time.Timer{p.answerBy, p.connectBy} {
		if t != nil {
			t.Stop()
		}
	}
	p.mu.Unlock()
	p.share.Store(nil)
	p.keyframes.stop()
	var renegotiate []*Peer
	if room := s.rooms[p.room]; room[p.member] == p {
		delete(room, p.member)
		if len(room) == 0 {
			delete(s.rooms, p.room)
		}
		for _, o := range room {
			voice := o.retire(p.member)
			watched := o.unwatchLocked(p.member)
			if (voice || watched) && !s.closed {
				renegotiate = append(renegotiate, o)
			}
		}
	}
	s.mu.Unlock()
	for _, o := range renegotiate {
		o.negotiate()
	}
	_ = p.pc.Close()
	return true
}

// fail closes the peer and tells the den, unless the peer had closed.
func (p *Peer) fail() {
	if p.close() {
		p.signal.Failed()
	}
}

func (p *Peer) stateChanged(state webrtc.PeerConnectionState) {
	switch state {
	case webrtc.PeerConnectionStateConnected:
		p.mu.Lock()
		p.connected = true
		if p.connectBy != nil {
			p.connectBy.Stop()
		}
		p.mu.Unlock()
	case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
		// Pion closes a connection itself when the browser closes its end,
		// which ends the member's call as surely as a failure. A close the
		// den made comes back here too, and fails nothing twice.
		p.fail()
	}
}

// forward passes on what the member sends on each section the den
// receives on: their voice, and their share's screen and sound.
func (p *Peer) forward(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	p.mu.Lock()
	screen, sound := p.screen, p.sound
	p.mu.Unlock()
	switch {
	case receiver == p.mic.Receiver():
		p.forwardAudio(remote, p.track, func() bool { return true })
	case screen != nil && receiver == screen.Receiver():
		p.forwardScreen(remote)
	case sound != nil && receiver == sound.Receiver():
		p.forwardAudio(remote, p.soundOut, func() bool {
			sh := p.share.Load()
			return sh != nil && sh.sound
		})
	}
}

// forwardAudio passes the member's voice, or their share's sound, to the
// others while live says it goes: only Opus from its own section, under
// headers of the den's own, so nothing but the payload passes from one
// member to another, and no more than the member's rate.
func (p *Peer) forwardAudio(remote *webrtc.TrackRemote, out *webrtc.TrackLocalStaticRTP, live func() bool) {
	bytes := rate.NewLimiter(maxBytesPerSecond, maxBytesPerSecond)
	packets := rate.NewLimiter(maxPacketsPerSecond, maxPacketsPerSecond)
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		now := time.Now()
		if p.muted.Load() || !live() || pkt.PayloadType != uint8(remote.PayloadType()) ||
			!packets.AllowN(now, 1) || !bytes.AllowN(now, len(pkt.Payload)) {
			continue
		}
		write(out, pkt)
	}
}

// forwardScreen passes the member's screen to those watching it while they
// share (M4.2): only VP9 from the screen's section, under the den's own
// headers, within the share's bitrate. When its packets start again after
// a pause, as when nobody watched it, the sharer is asked for a keyframe,
// since one asked of a paused encoder may never come.
func (p *Peer) forwardScreen(remote *webrtc.TrackRemote) {
	isVP9 := strings.EqualFold(remote.Codec().MimeType, webrtc.MimeTypeVP9)
	p.screenSSRC.Store(uint32(remote.SSRC()))
	var last time.Time
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		sh := p.share.Load()
		now := time.Now()
		if sh == nil || !isVP9 || p.muted.Load() || pkt.PayloadType != uint8(remote.PayloadType()) ||
			!sh.packets.AllowN(now, 1) || !sh.bytes.AllowN(now, len(pkt.Payload)) {
			continue
		}
		if now.Sub(last) >= keyframeGap {
			p.requestKeyframe()
		}
		last = now
		write(p.screenOut, pkt)
	}
}

// write sends a packet's payload on under a header of the den's own, so
// none of the sender's header extensions or padding passes on.
func write(out *webrtc.TrackLocalStaticRTP, pkt *rtp.Packet) {
	pkt.Header = rtp.Header{Version: 2, Marker: pkt.Marker, PayloadType: pkt.PayloadType,
		SequenceNumber: pkt.SequenceNumber, Timestamp: pkt.Timestamp, SSRC: pkt.SSRC}
	pkt.PaddingSize = 0
	_ = out.WriteRTP(pkt)
}

// keyframes spaces the keyframe requests to a sharer keyframeGap apart.
type keyframes struct {
	mu      sync.Mutex
	last    time.Time
	pending *time.Timer // the request that waits for the gap to pass
	stopped bool
}

// requestKeyframe asks the member's browser for a keyframe of the screen
// they share, with a picture loss indication: at once, or, within
// keyframeGap of the last request, once the gap has passed, which serves
// every request made meanwhile.
func (p *Peer) requestKeyframe() {
	k := &p.keyframes
	k.mu.Lock()
	if k.stopped || k.pending != nil {
		k.mu.Unlock()
		return
	}
	if wait := keyframeGap - time.Since(k.last); wait > 0 {
		k.pending = time.AfterFunc(wait, p.keyframeDue)
		k.mu.Unlock()
		return
	}
	k.last = time.Now()
	k.mu.Unlock()
	p.sendPLI()
}

// keyframeDue sends the request that waited for the gap to pass.
func (p *Peer) keyframeDue() {
	k := &p.keyframes
	k.mu.Lock()
	k.pending = nil
	if k.stopped {
		k.mu.Unlock()
		return
	}
	k.last = time.Now()
	k.mu.Unlock()
	p.sendPLI()
}

func (k *keyframes) stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopped = true
	if k.pending != nil {
		k.pending.Stop()
		k.pending = nil
	}
}

// sendPLI asks the member's browser for a keyframe, while they share a
// screen it has sent.
func (p *Peer) sendPLI() {
	if ssrc := p.screenSSRC.Load(); ssrc != 0 && p.share.Load() != nil {
		_ = p.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
	}
}

// feedback reads the RTCP of a section sending a share, which its
// interceptors need read, until the section stops, and asks the sharer
// for a keyframe whenever the viewer's browser does, with a PLI or a FIR.
// What doesn't parse is skipped, so a viewer can't stop the reading.
func feedback(sender *webrtc.RTPSender, sharer *Peer) {
	buf := make([]byte, 1500)
	for {
		n, _, err := sender.Read(buf)
		if err != nil {
			return
		}
		pkts, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			continue
		}
		for _, pkt := range pkts {
			switch pkt.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				sharer.requestKeyframe()
			}
		}
	}
}

// drain reads a sender's RTCP, which its interceptors need, until it stops.
func drain(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}
