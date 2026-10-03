package sfu

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// A header extension a test caller's packets carry, which the den must
// not pass on: its ID is one the den never negotiates.
const (
	testExtensionID = 14
	testExtension   = "leak"
)

// CallerOptions shape a TestCaller.
type CallerOptions struct {
	// TCP limits the caller to TCP, as on a network that blocks UDP, and
	// UDP to UDP. Pion's caller nominates the first pair that answers,
	// where a browser waits for the best, so a test that wants one path
	// asks for it.
	TCP, UDP bool
	// Loopback lets the caller reach a den on loopback, as tests do.
	Loopback bool
	// Rate is how many packets a second the caller sends: 50 when zero,
	// as a browser sends 20 ms frames.
	Rate int
}

// TestCaller stands in for a member's browser in tests and in the den
// e2e's voice probe: a Pion peer with no STUN or TURN servers that
// answers a den's offers as the page does, sends numbered packets as its
// microphone, and counts the packets that reach it from each member.
type TestCaller struct {
	pc   *webrtc.PeerConnection
	mic  *webrtc.TrackLocalStaticRTP
	rate int
	stop chan struct{}
	once sync.Once

	// checking closes once the caller's connection has started, which
	// Pion does in the background after the first answer.
	checking chan struct{}

	mu      sync.Mutex
	started bool
	heard   map[string]int // member → packets received
	leaked  int            // packets that arrived with the caller's extension
	changed chan struct{}
}

// NewTestCaller makes a caller.
func NewTestCaller(opts CallerOptions) (*TestCaller, error) {
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: opus, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptorsWithOptions(media, registry, webrtc.WithInterceptorLoggerFactory(pionLogs{})); err != nil {
		return nil, err
	}
	var settings webrtc.SettingEngine
	settings.LoggerFactory = pionLogs{}
	switch {
	case opts.TCP:
		settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6})
	case opts.UDP:
		settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	default:
		settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6,
			webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6})
	}
	settings.SetIncludeLoopbackCandidate(opts.Loopback)
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	// As a browser does, the caller stays open when the den closes its end,
	// until the caller itself hangs up.
	settings.DisableCloseByDTLS(true)
	// A caller starts its checks as it answers, before the den has the
	// answer, and the den drops the checks that come first. A browser keeps
	// checking for many seconds; Pion gives a path seven checks 200 ms
	// apart, which a den applying the answer on a busy machine can outlast.
	settings.SetICEMaxBindingRequests(100)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(registry), webrtc.WithSettingEngine(settings))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	mic, err := webrtc.NewTrackLocalStaticRTP(opus, "mic", "self")
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	c := &TestCaller{pc: pc, mic: mic, rate: opts.Rate, stop: make(chan struct{}), checking: make(chan struct{}),
		heard: map[string]int{}, changed: make(chan struct{})}
	if c.rate == 0 {
		c.rate = 50
	}
	pc.OnTrack(c.listen)
	var once sync.Once
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state != webrtc.ICEConnectionStateNew {
			once.Do(func() { close(c.checking) })
		}
	})
	return c, nil
}

// Answer answers an offer that carries the den's candidates, and returns
// the answer without the caller's own, as the page's local service sends
// it. The caller starts speaking with its first answer.
func (c *TestCaller) Answer(offer string) (string, error) {
	c.mu.Lock()
	first := !c.started
	c.started = true
	c.mu.Unlock()
	if !first {
		// Pion starts the connection in the background after an answer, and
		// takes a new offer that comes first for an ICE restart, with new
		// credentials the den never hears of. A browser doesn't, so the
		// caller waits for its connection to start, as a browser's has.
		select {
		case <-c.checking:
		case <-c.stop:
			return "", errors.New("the caller closed")
		case <-time.After(10 * time.Second):
			return "", errors.New("the caller's connection didn't start")
		}
	}
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", err
	}
	if first {
		// As the page does, the microphone takes the section the den
		// offered to receive it on.
		if _, err := c.pc.AddTrack(c.mic); err != nil {
			return "", err
		}
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	if err := c.pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	if first {
		go c.speak()
	}
	return denproto.StripCandidates(answer.SDP), nil
}

// speak sends numbered packets, each carrying the extension the den must
// drop, until the caller closes.
func (c *TestCaller) speak() {
	t := time.NewTicker(time.Second / time.Duration(c.rate))
	defer t.Stop()
	var seq uint16
	var ts uint32
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
		}
		seq++
		ts += 960
		pkt := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: seq, Timestamp: ts},
			Payload: []byte{0xf8, 0xff, 0xfe, byte(seq >> 8), byte(seq)}}
		_ = pkt.Header.SetExtension(testExtensionID, []byte(testExtension))
		_ = c.mic.WriteRTP(pkt)
	}
}

// listen counts each member's packets: the den labels the stream it
// sends them on with their ID.
func (c *TestCaller) listen(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		c.mu.Lock()
		c.heard[track.StreamID()]++
		if bytes.Equal(pkt.Header.GetExtension(testExtensionID), []byte(testExtension)) {
			c.leaked++
		}
		close(c.changed)
		c.changed = make(chan struct{})
		c.mu.Unlock()
	}
}

// Heard reports how many packets have reached the caller from a member.
func (c *TestCaller) Heard(member string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.heard[member]
}

// Leaked reports how many packets arrived with the header extension the
// sending caller put on them.
func (c *TestCaller) Leaked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.leaked
}

// WaitHeard waits until n more packets than now have come from a member.
func (c *TestCaller) WaitHeard(member string, n int, timeout time.Duration) error {
	deadline := time.After(timeout)
	c.mu.Lock()
	want := c.heard[member] + n
	for c.heard[member] < want {
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-deadline:
			return fmt.Errorf("heard %d packets from member %s, not %d, within %s", c.Heard(member), member, want, timeout)
		case <-c.stop:
			return errors.New("the caller closed")
		}
		c.mu.Lock()
	}
	c.mu.Unlock()
	return nil
}

// Path reports whether the caller's media goes over UDP or TCP, once its
// connection is up.
func (c *TestCaller) Path() string {
	for _, r := range c.pc.GetReceivers() {
		if t := r.Transport(); t != nil {
			if pair, err := t.ICETransport().GetSelectedCandidatePair(); err == nil && pair != nil {
				return pair.Local.Protocol.String()
			}
		}
	}
	return ""
}

// Connected reports whether the caller's connection to the den is up.
func (c *TestCaller) Connected() bool {
	return c.pc.ConnectionState() == webrtc.PeerConnectionStateConnected
}

// Close hangs up. Over TCP the den may not hear it: Pion queues the
// caller's close_notify, and closing the connection drops the queue, so
// the den learns only from the silence.
func (c *TestCaller) Close() error {
	c.once.Do(func() { close(c.stop) })
	return c.pc.Close()
}
