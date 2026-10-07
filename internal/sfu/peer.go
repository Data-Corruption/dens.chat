package sfu

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"golang.org/x/time/rate"
)

// What the den forwards from one member at most; the rest is dropped.
const (
	maxBytesPerSecond   = 256_000 / 8
	maxPacketsPerSecond = 500
)

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

	// muted drops the member's audio before it's forwarded, as a staff mute
	// does.
	muted atomic.Bool

	mu        sync.Mutex
	closed    bool
	version   int
	offer     string // the offer that's out, as sent
	waiting   bool   // an offer is out
	stale     bool   // the sections changed since that offer
	held      bool   // the member's signaling is away, so no offer goes out
	answered  bool   // the member has answered an offer
	restart   bool   // the next offer restarts ICE
	connected bool
	answerBy  *time.Timer
	connectBy *time.Timer
	// sends maps each other member to the section sending their audio.
	// A section whose member left retires; once the client has answered an
	// offer that retires it, another member's audio can take it, so a long
	// call's offers don't grow with every join.
	sends    map[string]*webrtc.RTPTransceiver
	retiring []*webrtc.RTPTransceiver // not in an offer yet
	offered  []*webrtc.RTPTransceiver // in the offer that's out
	spare    []*webrtc.RTPTransceiver
}

// Join adds a member to a room's call. The peer sends them its first
// offer through signal, and every other peer in the room a new one that
// adds the member's audio.
func (s *SFU) Join(room, member string, signal Signal) (*Peer, error) {
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	p := &Peer{sfu: s, room: room, member: member, signal: signal, pc: pc, sends: map[string]*webrtc.RTPTransceiver{}}
	fail := func(err error) (*Peer, error) {
		_ = pc.Close()
		return nil, err
	}
	p.mic, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		return fail(err)
	}
	if p.track, err = webrtc.NewTrackLocalStaticRTP(opus, "audio", member); err != nil {
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
		if err := p.send(o.member, o.track); err != nil {
			s.mu.Unlock()
			return fail(err)
		}
	}
	var renegotiate, broken []*Peer
	for _, o := range others {
		if o.send(member, p.track) == nil {
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
	if !p.connected && p.connectBy == nil {
		p.connectBy = time.AfterFunc(p.sfu.connect, p.fail)
	}
	again := p.stale
	p.mu.Unlock()
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
	p.offer = setOpusParams(denproto.StripCandidates(offer.SDP), opusParams)
	p.answerBy = time.AfterFunc(p.sfu.answer, p.fail)
	p.signal.Offer(p.version, p.offer)
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

// SetMuted drops the member's audio, or forwards it again.
func (p *Peer) SetMuted(muted bool) { p.muted.Store(muted) }

// send has the peer send another member's audio, on a spare section if
// there is one. The caller holds the SFU's lock.
func (p *Peer) send(member string, track *webrtc.TrackLocalStaticRTP) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	var t *webrtc.RTPTransceiver
	if n := len(p.spare); n > 0 {
		t = p.spare[n-1]
		sender, err := p.sfu.api.NewRTPSender(track, p.mic.Receiver().Transport())
		if err != nil {
			return err
		}
		if err := t.SetSender(sender, track); err != nil {
			_ = sender.Stop()
			return err
		}
		p.spare = p.spare[:n-1]
	} else {
		var err error
		t, err = p.pc.AddTransceiverFromTrack(track,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			return err
		}
	}
	p.sends[member] = t
	go drain(t.Sender())
	return nil
}

// retire stops sending a member's audio, and reports whether the peer
// needs a new offer. The caller holds the SFU's lock.
func (p *Peer) retire(member string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.sends[member]
	if p.closed || t == nil {
		return false
	}
	delete(p.sends, member)
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
	var renegotiate []*Peer
	if room := s.rooms[p.room]; room[p.member] == p {
		delete(room, p.member)
		if len(room) == 0 {
			delete(s.rooms, p.room)
		}
		for _, o := range room {
			if o.retire(p.member) && !s.closed {
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

// forward passes the member's audio to the others in the call: only Opus
// from their own section, under headers of the den's own, so nothing but
// the payload passes from one member to another, and no more than the
// member's rate.
func (p *Peer) forward(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	if receiver != p.mic.Receiver() {
		return
	}
	bytes := rate.NewLimiter(maxBytesPerSecond, maxBytesPerSecond)
	packets := rate.NewLimiter(maxPacketsPerSecond, maxPacketsPerSecond)
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		now := time.Now()
		if p.muted.Load() || pkt.PayloadType != uint8(remote.PayloadType()) ||
			!packets.AllowN(now, 1) || !bytes.AllowN(now, len(pkt.Payload)) {
			continue
		}
		pkt.Header = rtp.Header{Version: 2, Marker: pkt.Marker, PayloadType: pkt.PayloadType,
			SequenceNumber: pkt.SequenceNumber, Timestamp: pkt.Timestamp, SSRC: pkt.SSRC}
		pkt.PaddingSize = 0
		_ = p.track.WriteRTP(pkt)
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
