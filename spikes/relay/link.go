package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// The link: one Pion peer connection to the other probe, with nothing
// passed between them first. In Dens the den would carry each side's
// fingerprint and ICE credentials, sealed; here both come from a secret the
// two probes share, so each knows the other's without asking. Each side
// binds one UDP port and sends to the other's at once.

// LinkConfig is what a probe is told of its link.
type LinkConfig struct {
	Role   string // "a" offers, "b" answers
	Peer   string // the other side's address
	Port   int    // the UDP port this side binds
	Remote int    // the UDP port the other side binds
	Secret string
	Local  string // gather on this address alone, if set
	// ICE's timeouts: how long without an answer before the link is
	// disconnected, then failed, and how often it asks.
	Disconnected, Failed, KeepAlive time.Duration
	// RTOMax caps SCTP's retransmission timeout, which is also how long a
	// channel stalls once a cut has outlasted what it may have in flight.
	RTOMax time.Duration
	// RTP sends the sound as RTP beside the channels, in place of the pcm
	// channel: SRTP under the same DTLS, with nothing acknowledged, sent
	// again or held back.
	RTP bool
}

// Link is one attempt's connection and its three channels.
type Link struct {
	pc *webrtc.PeerConnection
	// ctl is reliable and ordered; now and pcm are neither, so a lost
	// packet never holds the next one back.
	ctl, now, pcm *webrtc.DataChannel
	open          chan struct{}
	gone          chan struct{}
	goneOnce      sync.Once
	opened        atomic.Int32
	started       time.Duration
	// With -sound rtp: the track this side sends its sound on, and what
	// takes the other side's as it arrives.
	track   *webrtc.TrackLocalStaticRTP
	rtpSeq  uint16
	onSound atomic.Pointer[func([]byte)]
}

// The sound's RTP: 16-bit PCM under a payload type of the link's own, and
// each side's SSRC fixed, so the description written for the other side
// can name it.
const soundPayload = 96

var soundCodec = webrtc.RTPCodecCapability{MimeType: "audio/L16", ClockRate: 48000, Channels: 2}

func soundSSRC(role string) uint32 {
	if role == "a" {
		return 0x52454c01
	}
	return 0x52454c02
}

// sendSound sends one packet of sound as RTP. Nothing queues: it goes to
// the socket or it's lost.
func (l *Link) sendSound(pkt []byte, pos uint64) error {
	l.rtpSeq++
	return l.track.WriteRTP(&rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: soundPayload, SequenceNumber: l.rtpSeq, Timestamp: uint32(pos)},
		Payload: pkt,
	})
}

// identity is one side's certificate and ICE credentials, made from the
// secret: the same on both probes.
type identity struct {
	cert        webrtc.Certificate
	fingerprint string
	ufrag, pwd  string
}

func derive(secret, role string) (identity, error) {
	var id identity
	sum := func(what string) []byte {
		h := sha256.Sum256([]byte("relay-spike/" + secret + "/" + role + "/" + what))
		return h[:]
	}
	// The key DTLS proves is ECDSA, which Pion takes. Its certificate is
	// signed by an Ed25519 key, whose signatures hold no randomness, so
	// both probes make the same bytes and so the same fingerprint. Nothing
	// checks the signature: WebRTC trusts the fingerprint alone.
	d := new(big.Int).SetBytes(sum("key"))
	curve := elliptic.P256()
	d.Mod(d, new(big.Int).Sub(curve.Params().N, big.NewInt(1)))
	d.Add(d, big.NewInt(1))
	key, err := ecdsa.ParseRawPrivateKey(curve, d.FillBytes(make([]byte, 32)))
	if err != nil {
		return id, err
	}
	signer := ed25519.NewKeyFromSeed(sum("signer"))
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay spike " + role},
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(zeroes{}, tpl, tpl, key.Public(), signer)
	if err != nil {
		return id, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return id, err
	}
	id.cert = webrtc.CertificateFromX509(key, cert)
	fp := sha256.Sum256(der)
	parts := make([]string, len(fp))
	for i, b := range fp {
		parts[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	id.fingerprint = strings.Join(parts, ":")
	id.ufrag = hex.EncodeToString(sum("ufrag"))[:16]
	id.pwd = hex.EncodeToString(sum("pwd"))
	return id, nil
}

type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// description is the other side's, as it would have sent it: one section
// for data channels, with its credentials, its fingerprint and the one
// address it was told to be at.
//
// With ssrc set it has a section for the sound before that, as Pion orders
// its own: RTP of one payload type, from the one SSRC the other side sends.
func description(id identity, setup, addr string, port int, ssrc uint32) string {
	transport := []string{
		"a=ice-ufrag:" + id.ufrag,
		"a=ice-pwd:" + id.pwd,
		"a=fingerprint:sha-256 " + id.fingerprint,
		"a=setup:" + setup,
	}
	candidate := []string{
		fmt.Sprintf("a=candidate:1 1 udp 2130706431 %s %d typ host", addr, port),
		"a=end-of-candidates",
	}
	lines := []string{"v=0", "o=- 1 1 IN IP4 0.0.0.0", "s=-", "t=0 0"}
	data := "0"
	if ssrc != 0 {
		data = "1"
		lines = append(lines, "a=group:BUNDLE 0 1",
			fmt.Sprintf("m=audio 9 UDP/TLS/RTP/SAVPF %d", soundPayload),
			"c=IN IP4 0.0.0.0",
			"a=mid:0")
		lines = append(lines, transport...)
		lines = append(lines,
			"a=rtcp-mux",
			"a=sendrecv",
			fmt.Sprintf("a=rtpmap:%d L16/48000/2", soundPayload),
			"a=msid:relay sound",
			fmt.Sprintf("a=ssrc:%d cname:relay", ssrc),
			fmt.Sprintf("a=ssrc:%d msid:relay sound", ssrc))
		lines = append(lines, candidate...)
	} else {
		lines = append(lines, "a=group:BUNDLE 0")
	}
	lines = append(lines,
		"m=application 9 UDP/DTLS/SCTP webrtc-datachannel",
		"c=IN IP4 0.0.0.0",
		"a=mid:"+data)
	lines = append(lines, transport...)
	lines = append(lines, "a=sctp-port:5000")
	lines = append(lines, candidate...)
	return strings.Join(lines, "\r\n") + "\r\n"
}

// dial starts one attempt. It returns once both descriptions are set, and
// from then this side sends to the other's port until one answers.
func dial(cfg LinkConfig) (*Link, error) {
	other := "b"
	if cfg.Role == "b" {
		other = "a"
	}
	mine, err := derive(cfg.Secret, cfg.Role)
	if err != nil {
		return nil, err
	}
	theirs, err := derive(cfg.Secret, other)
	if err != nil {
		return nil, err
	}

	var se webrtc.SettingEngine
	se.SetICECredentials(mine.ufrag, mine.pwd)
	if err := se.SetEphemeralUDPPortRange(uint16(cfg.Port), uint16(cfg.Port)); err != nil {
		return nil, err
	}
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetICETimeouts(cfg.Disconnected, cfg.Failed, cfg.KeepAlive)
	// An attempt keeps asking for as long as it lasts: the other side may
	// start seconds later, and only this side's sending opens its firewall
	// to the answers.
	se.SetICEMaxBindingRequests(600)
	if cfg.RTOMax > 0 {
		se.SetSCTPRTOMax(cfg.RTOMax)
	}
	if cfg.Local != "" {
		local := net.ParseIP(cfg.Local)
		se.SetIPFilter(func(ip net.IP) bool { return ip.Equal(local) })
		se.SetIncludeLoopbackCandidate(local.IsLoopback())
	}
	// No interceptors: no reports, no asking again, nothing but the sound.
	media := &webrtc.MediaEngine{}
	if cfg.RTP {
		if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: soundCodec, PayloadType: soundPayload}, webrtc.RTPCodecTypeAudio); err != nil {
			return nil, err
		}
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(&interceptor.Registry{}))
	pc, err := api.NewPeerConnection(webrtc.Configuration{Certificates: []webrtc.Certificate{mine.cert}})
	if err != nil {
		return nil, err
	}
	l := &Link{pc: pc, open: make(chan struct{}), gone: make(chan struct{}), started: now()}
	var theirSSRC uint32
	if cfg.RTP {
		theirSSRC = soundSSRC(other)
		if l.track, err = webrtc.NewTrackLocalStaticRTP(soundCodec, "sound", "relay"); err != nil {
			return nil, err
		}
		if _, err = pc.AddTransceiverFromTrack(l.track, webrtc.RTPTransceiverInit{
			Direction:     webrtc.RTPTransceiverDirectionSendrecv,
			SendEncodings: []webrtc.RTPEncodingParameters{{RTPCodingParameters: webrtc.RTPCodingParameters{SSRC: webrtc.SSRC(soundSSRC(cfg.Role))}}},
		}); err != nil {
			return nil, err
		}
		pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			logf("link", "track ssrc=%x codec=%s after=%s", uint32(t.SSRC()), t.Codec().MimeType, since(l.started))
			for {
				p, _, err := t.ReadRTP()
				if err != nil {
					return
				}
				if f := l.onSound.Load(); f != nil {
					(*f)(p.Payload)
				}
			}
		})
	}

	var want atomic.Int32
	want.Store(3)
	if cfg.RTP {
		want.Store(2)
	}
	yes, no, zero := true, false, uint16(0)
	channel := func(label string, id uint16, reliable bool) (*webrtc.DataChannel, error) {
		init := &webrtc.DataChannelInit{Negotiated: &yes, ID: &id, Ordered: &yes}
		if !reliable {
			init.Ordered = &no
			init.MaxRetransmits = &zero
		}
		dc, err := pc.CreateDataChannel(label, init)
		if err != nil {
			return nil, err
		}
		dc.OnOpen(func() {
			logf("link", "channel=%s open after=%s", label, since(l.started))
			if l.opened.Add(1) == want.Load() {
				close(l.open)
			}
		})
		return dc, nil
	}
	if l.ctl, err = channel("ctl", 0, true); err != nil {
		return nil, err
	}
	if l.now, err = channel("now", 1, false); err != nil {
		return nil, err
	}
	if !cfg.RTP {
		if l.pcm, err = channel("pcm", 2, false); err != nil {
			return nil, err
		}
	}

	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		logf("link", "ice=%s after=%s", s, since(l.started))
		if s == webrtc.ICEConnectionStateConnected {
			if pair, err := pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair(); err == nil && pair != nil {
				logf("link", "pair local=%s:%d(%s) remote=%s:%d(%s)", pair.Local.Address, pair.Local.Port, pair.Local.Typ,
					pair.Remote.Address, pair.Remote.Port, pair.Remote.Typ)
			}
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		logf("link", "state=%s after=%s", s, since(l.started))
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			l.goneOnce.Do(func() { close(l.gone) })
		}
	})

	if cfg.Role == "a" {
		offer, err := pc.CreateOffer(nil)
		if err != nil {
			return nil, err
		}
		if err := pc.SetLocalDescription(offer); err != nil {
			return nil, err
		}
		answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: description(theirs, "active", cfg.Peer, cfg.Remote, theirSSRC)}
		if err := pc.SetRemoteDescription(answer); err != nil {
			return nil, fmt.Errorf("the answer made for the other side: %w", err)
		}
	} else {
		offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: description(theirs, "actpass", cfg.Peer, cfg.Remote, theirSSRC)}
		if err := pc.SetRemoteDescription(offer); err != nil {
			return nil, fmt.Errorf("the offer made for the other side: %w", err)
		}
		answer, err := pc.CreateAnswer(nil)
		if err != nil {
			return nil, err
		}
		if err := pc.SetLocalDescription(answer); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *Link) Close() {
	_ = l.pc.Close()
	l.goneOnce.Do(func() { close(l.gone) })
}

// keepLinked dials until the context ends, starting again whenever an
// attempt fails or its link is lost, and hands each link that opens to use.
func keepLinked(ctx context.Context, cfg LinkConfig, attempt time.Duration, use func(context.Context, *Link)) {
	for n := 1; ctx.Err() == nil; n++ {
		logf("link", "attempt=%d role=%s peer=%s:%d port=%d", n, cfg.Role, cfg.Peer, cfg.Remote, cfg.Port)
		l, err := dial(cfg)
		if err != nil {
			logf("link", "attempt=%d error=%q", n, err)
			sleep(ctx, 2*time.Second)
			continue
		}
		select {
		case <-l.open:
			logf("link", "up after=%s attempt=%d", since(l.started), n)
			lctx, cancel := context.WithCancel(ctx)
			go func() {
				select {
				case <-l.gone:
				case <-lctx.Done():
				}
				cancel()
			}()
			up := now()
			use(lctx, l)
			<-lctx.Done()
			cancel()
			logf("link", "down after=%s up", since(up))
		case <-l.gone:
			logf("link", "attempt=%d failed after=%s", n, since(l.started))
		case <-time.After(attempt):
			logf("link", "attempt=%d gave up after=%s", n, since(l.started))
		case <-ctx.Done():
		}
		l.Close()
		sleep(ctx, 500*time.Millisecond)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
