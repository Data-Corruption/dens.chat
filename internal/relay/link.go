package relay

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// The link's timings.
const (
	// introduction is how long a link's halves are good for: each side
	// sends checks for as long, since each side's sending is what lets the
	// other's packets through its firewall.
	introduction = 30 * time.Second
	// ICE says a link is disconnected once nothing has come for a second,
	// checks every half second once connected, and every 200 ms, Pion's
	// own, before; a link disconnected for 3 seconds more has failed.
	disconnectedAfter = time.Second
	keepAlive         = 500 * time.Millisecond
	checkEvery        = 200 * time.Millisecond
	failedAfter       = 3 * time.Second
	// rtoMax caps SCTP's retransmission timer, whose floor of a second
	// otherwise stops every channel for a second after any cut of a fifth
	// of one.
	rtoMax = 100 * time.Millisecond
)

// The sound crosses as RTP beside the channels (M20.2), under the link's
// own DTLS, with no interceptors, so nothing is reported, acknowledged or
// sent again: a packet the path loses is 5 ms the player fades across.
// Its payload is the helper's piece as it came, the rate and then the
// frames, little-endian, whatever the codec's name says, and its
// timestamp the piece's place in the stream, in frames.
// Each side sends from an SSRC its role fixes, which the description
// written for it names.
const soundPayloadType = 96

var soundCodec = webrtc.RTPCodecCapability{MimeType: "audio/L16", ClockRate: 48000, Channels: 2}

// soundSSRC is the SSRC of the side that offers, or of the one that
// answers.
func soundSSRC(offers bool) uint32 {
	if offers {
		return 0x44454e01
	}
	return 0x44454e02
}

// local is this side of an attempt at a link: the UDP port it holds, its
// certificate and the ICE credentials made for it.
type local struct {
	conn  *net.UDPConn
	mux   *ice.UDPMuxDefault
	cert  webrtc.Certificate
	fp    []byte
	ufrag string
	pwd   string
	addrs []netip.Addr
}

// newLocal holds an ephemeral port on every interface, and makes a
// certificate and credentials, for a link through addrs.
func newLocal(addrs []netip.Addr) (*local, error) {
	if len(addrs) == 0 {
		return nil, errors.New("this computer has no address on a local network")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	cert, err := webrtc.GenerateCertificate(key)
	if err != nil {
		return nil, err
	}
	fps, err := cert.GetFingerprints()
	if err != nil {
		return nil, err
	}
	var fp []byte
	for _, f := range fps {
		if f.Algorithm == "sha-256" {
			fp, err = hex.DecodeString(strings.ReplaceAll(f.Value, ":", ""))
		}
	}
	if err != nil || len(fp) != 32 {
		return nil, errors.New("the certificate has no SHA-256 fingerprint")
	}
	// The error from binding names the address, which stays out of the
	// log, so it's not passed on.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, errors.New("couldn't hold a UDP port")
	}
	l := &local{conn: conn, cert: *cert, fp: fp, ufrag: iceRandom(16), pwd: iceRandom(32), addrs: addrs}
	l.mux = ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: conn, Logger: quiet{}.NewLogger("udpmux")})
	return l, nil
}

func (l *local) half() *half {
	h := &half{Fingerprint: l.fp, Ufrag: l.ufrag, Pwd: l.pwd, Port: l.conn.LocalAddr().(*net.UDPAddr).Port}
	for _, a := range l.addrs {
		h.Addrs = append(h.Addrs, a.String())
	}
	return h
}

// close lets go of the port, which ends a link made on it.
func (l *local) close() { _ = l.mux.Close() }

// iceRandom makes ICE credentials of n letters and digits.
func iceRandom(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// muxAt is the port held on every interface, naming to ICE the addresses
// the half does and no others, which it would otherwise gather from every
// interface, public ones among them.
type muxAt struct {
	*ice.UDPMuxDefault
	addrs []net.Addr
}

func (m muxAt) GetListenAddresses() []net.Addr { return m.addrs }

// link is one peer connection between two of the member's installs, with
// its two channels: ctl, reliable and in order, and now, neither, so a lost
// message never holds the next one back; and the track this side's sound
// goes on (M20.2).
type link struct {
	pc       *webrtc.PeerConnection
	ctl, now *webrtc.DataChannel
	track    *webrtc.TrackLocalStaticRTP
	// up is closed once both channels are open, and done once the link has
	// failed or closed.
	up, done chan struct{}
	opened   atomic.Int32
	upOnce   sync.Once
	doneOnce sync.Once
	mu       sync.Mutex
	lost     *time.Timer
	// The sound's sequence number and place in the stream, as sent.
	soundMu  sync.Mutex
	seq      uint16
	position uint32
}

// sendSound sends a piece of this computer's sound as RTP. Nothing waits:
// it goes to the socket, or it's lost.
func (k *link) sendSound(piece []byte) {
	frames := uint32((len(piece) - 4) / 4)
	k.soundMu.Lock()
	defer k.soundMu.Unlock()
	k.seq++
	_ = k.track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: soundPayloadType, SequenceNumber: k.seq,
		Timestamp: k.position}, Payload: piece})
	k.position += frames
}

// dial starts a link from this side's half to the other's, the side with
// the lower Relay ID offering, each writing the other's description from
// its half. From then each sends checks to the other's addresses until one
// answers, or the introduction runs out. onCtl and onNow take what each
// channel carries, from its first message, and onSound each piece of the
// other side's sound, with its place in the stream.
func (l *local) dial(theirs *half, offer, loopback bool, onCtl, onNow func([]byte), onSound func(at uint32, piece []byte)) (*link, error) {
	var se webrtc.SettingEngine
	se.LoggerFactory = quiet{}
	se.SetICECredentials(l.ufrag, l.pwd)
	listen := make([]net.Addr, 0, len(l.addrs))
	port := l.conn.LocalAddr().(*net.UDPAddr).Port
	for _, a := range l.addrs {
		listen = append(listen, &net.UDPAddr{IP: a.AsSlice(), Port: port})
	}
	se.SetICEUDPMux(muxAt{l.mux, listen})
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetIncludeLoopbackCandidate(loopback)
	// Pion's failed timeout would also end the first checks after
	// disconnected and failed together, 4 seconds; the link fails itself
	// instead, 3 seconds after it's disconnected, and an attempt once its
	// introduction runs out.
	se.SetICETimeouts(disconnectedAfter, 0, keepAlive)
	se.SetICEMaxBindingRequests(uint16(introduction / checkEvery))
	se.SetSCTPRTOMax(rtoMax)
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: soundCodec, PayloadType: soundPayloadType},
		webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(media),
		webrtc.WithInterceptorRegistry(&interceptor.Registry{}))
	pc, err := api.NewPeerConnection(webrtc.Configuration{Certificates: []webrtc.Certificate{l.cert}})
	if err != nil {
		return nil, err
	}
	k := &link{pc: pc, up: make(chan struct{}), done: make(chan struct{})}
	if k.track, err = webrtc.NewTrackLocalStaticRTP(soundCodec, "sound", "relay"); err == nil {
		_, err = pc.AddTransceiverFromTrack(k.track, webrtc.RTPTransceiverInit{
			Direction:     webrtc.RTPTransceiverDirectionSendrecv,
			SendEncodings: []webrtc.RTPEncodingParameters{{RTPCodingParameters: webrtc.RTPCodingParameters{SSRC: webrtc.SSRC(soundSSRC(offer))}}},
		})
	}
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			p, _, err := t.ReadRTP()
			if err != nil {
				return
			}
			if p.PayloadType == soundPayloadType {
				onSound(p.Timestamp, p.Payload)
			}
		}
	})
	yes, no, zero := true, false, uint16(0)
	channel := func(label string, id uint16, reliable bool) (*webrtc.DataChannel, error) {
		init := &webrtc.DataChannelInit{Negotiated: &yes, ID: &id, Ordered: &yes}
		if !reliable {
			init.Ordered, init.MaxRetransmits = &no, &zero
		}
		dc, err := pc.CreateDataChannel(label, init)
		if err != nil {
			return nil, err
		}
		dc.OnOpen(func() {
			if k.opened.Add(1) == 2 {
				k.upOnce.Do(func() { close(k.up) })
			}
		})
		dc.OnClose(k.end)
		return dc, nil
	}
	if k.ctl, err = channel("ctl", 0, true); err == nil {
		k.now, err = channel("now", 1, false)
	}
	if err == nil {
		k.ctl.OnMessage(func(m webrtc.DataChannelMessage) { onCtl(m.Data) })
		k.now.OnMessage(func(m webrtc.DataChannelMessage) { onNow(m.Data) })
	}
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		switch s {
		case webrtc.ICEConnectionStateDisconnected:
			k.mu.Lock()
			if k.lost == nil {
				k.lost = time.AfterFunc(failedAfter, k.end)
			}
			k.mu.Unlock()
		case webrtc.ICEConnectionStateConnected, webrtc.ICEConnectionStateCompleted:
			k.mu.Lock()
			if k.lost != nil {
				k.lost.Stop()
				k.lost = nil
			}
			k.mu.Unlock()
		case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateClosed:
			k.end()
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			k.end()
		}
	})
	if offer {
		o, err := pc.CreateOffer(nil)
		if err == nil {
			err = pc.SetLocalDescription(o)
		}
		if err == nil {
			err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: description(theirs, "active", soundSSRC(false))})
		}
		// Pion's errors can name addresses, which stay out of the log.
		if err != nil {
			_ = pc.Close()
			return nil, errors.New("couldn't offer a link")
		}
		return k, nil
	}
	err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: description(theirs, "actpass", soundSSRC(true))})
	var a webrtc.SessionDescription
	if err == nil {
		a, err = pc.CreateAnswer(nil)
	}
	if err == nil {
		err = pc.SetLocalDescription(a)
	}
	if err != nil {
		_ = pc.Close()
		return nil, errors.New("couldn't answer a link")
	}
	return k, nil
}

// end closes the link, once, as it failed or closed. Pion's callbacks call
// it, and a peer connection closed from inside one can wait on itself, so
// it closes in the background.
func (k *link) end() {
	if k.ending() {
		go func() { _ = k.pc.Close() }()
	}
}

// close closes the link, once, from outside Pion's callbacks, waiting for
// the other side to be told, which lets it know at once.
func (k *link) close() {
	if k.ending() {
		_ = k.pc.Close()
	}
}

// ending marks the link done, and reports whether this is the first time.
func (k *link) ending() bool {
	first := false
	k.doneOnce.Do(func() {
		first = true
		close(k.done)
		k.mu.Lock()
		if k.lost != nil {
			k.lost.Stop()
		}
		k.mu.Unlock()
	})
	return first
}

// description is the other side's, as it would have written it, from its
// half: a section for its sound, from the SSRC its role fixes, then one
// for the data channels, as Pion orders its own, each with its
// credentials, its fingerprint, and a host candidate at each of its
// addresses.
func description(h *half, setup string, ssrc uint32) string {
	fp := make([]string, len(h.Fingerprint))
	for i, b := range h.Fingerprint {
		fp[i] = fmt.Sprintf("%02X", b)
	}
	transport := []string{"a=ice-ufrag:" + h.Ufrag, "a=ice-pwd:" + h.Pwd, "a=fingerprint:sha-256 " + strings.Join(fp, ":"),
		"a=setup:" + setup}
	for i, a := range h.Addrs {
		transport = append(transport, fmt.Sprintf("a=candidate:%d 1 udp 2130706431 %s %d typ host", i+1, a, h.Port))
	}
	transport = append(transport, "a=end-of-candidates")
	lines := []string{"v=0", "o=- 1 1 IN IP4 0.0.0.0", "s=-", "t=0 0", "a=group:BUNDLE 0 1",
		fmt.Sprintf("m=audio 9 UDP/TLS/RTP/SAVPF %d", soundPayloadType), "c=IN IP4 0.0.0.0", "a=mid:0"}
	lines = append(lines, transport...)
	lines = append(lines, "a=rtcp-mux", "a=sendrecv", fmt.Sprintf("a=rtpmap:%d L16/48000/2", soundPayloadType), "a=msid:relay sound",
		fmt.Sprintf("a=ssrc:%d cname:relay", ssrc), fmt.Sprintf("a=ssrc:%d msid:relay sound", ssrc),
		"m=application 9 UDP/DTLS/SCTP webrtc-datachannel", "c=IN IP4 0.0.0.0", "a=mid:1")
	lines = append(lines, transport...)
	lines = append(lines, "a=sctp-port:5000")
	return strings.Join(lines, "\r\n") + "\r\n"
}

// quiet drops Pion's messages, which name addresses: none reaches the log,
// in any form.
type quiet struct{}

func (quiet) NewLogger(string) logging.LeveledLogger { return quietLogger{} }

type quietLogger struct{}

func (quietLogger) Trace(string)          {}
func (quietLogger) Tracef(string, ...any) {}
func (quietLogger) Debug(string)          {}
func (quietLogger) Debugf(string, ...any) {}
func (quietLogger) Info(string)           {}
func (quietLogger) Infof(string, ...any)  {}
func (quietLogger) Warn(string)           {}
func (quietLogger) Warnf(string, ...any)  {}
func (quietLogger) Error(string)          {}
func (quietLogger) Errorf(string, ...any) {}
