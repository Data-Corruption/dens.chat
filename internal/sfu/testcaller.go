package sfu

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
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
	// ScreenRate is how many packets a second the caller's screen sends
	// while it shares, 90 when zero, in frames of three, and ScreenBytes
	// how large each one is, 1,000 bytes when zero: about 720 kbps.
	ScreenRate, ScreenBytes int
}

// TestCaller stands in for a member's browser in tests and in the den
// e2e's voice probe: a Pion peer with no STUN or TURN servers that
// answers a den's offers as the page does, sends numbered packets as its
// microphone, and counts the packets that reach it from each member. From
// M4.2 it shares a screen, with sound, when asked, counts what reaches it
// of the shares it watches, and asks for keyframes as a viewer's browser
// does.
type TestCaller struct {
	pc     *webrtc.PeerConnection
	mic    *webrtc.TrackLocalStaticRTP
	screen *webrtc.TrackLocalStaticRTP
	sound  *webrtc.TrackLocalStaticRTP
	rate   int
	// screenRate and screenBytes shape the screen's packets.
	screenRate, screenBytes int
	stop                    chan struct{}
	once                    sync.Once

	// checking closes once the caller's connection has started, which
	// Pion does in the background after the first answer.
	checking chan struct{}

	mu      sync.Mutex
	started bool
	heard   map[string]int // kind and stream → packets received
	leaked  int            // packets that arrived with the caller's extension
	changed chan struct{}
	// The caller's share: whether it's sharing, with sound, and whether its
	// screen and sound have their sections yet.
	sharing, withSound          bool
	screenSending, soundSending bool
	keyframeRequests            int
}

// browserOpus is Opus as Chromium describes it in its answers. The caller
// answers with its parameters, as a browser does, where Pion would echo the
// den's.
var browserOpus = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
	SDPFmtpLine: "minptime=10;useinbandfec=1"}

// NewTestCaller makes a caller.
func NewTestCaller(opts CallerOptions) (*TestCaller, error) {
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: browserOpus, PayloadType: opusType}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: vp9, PayloadType: vp9Type}, webrtc.RTPCodecTypeVideo); err != nil {
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
	c := &TestCaller{pc: pc, rate: opts.Rate, screenRate: opts.ScreenRate, screenBytes: opts.ScreenBytes,
		stop: make(chan struct{}), checking: make(chan struct{}), heard: map[string]int{}, changed: make(chan struct{})}
	fail := func(err error) (*TestCaller, error) {
		_ = pc.Close()
		return nil, err
	}
	if c.mic, err = webrtc.NewTrackLocalStaticRTP(browserOpus, "mic", "self"); err != nil {
		return fail(err)
	}
	if c.screen, err = webrtc.NewTrackLocalStaticRTP(vp9, "screen", "self-screen"); err != nil {
		return fail(err)
	}
	if c.sound, err = webrtc.NewTrackLocalStaticRTP(browserOpus, "sound", "self-screen"); err != nil {
		return fail(err)
	}
	if c.rate == 0 {
		c.rate = 50
	}
	if c.screenRate == 0 {
		c.screenRate = 90
	}
	if c.screenBytes == 0 {
		c.screenBytes = 1000
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
// it. The caller starts speaking with its first answer, and, while it
// shares, sends its screen and sound once the den offers to receive them.
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
	if err := c.attachShare(offer); err != nil {
		return "", err
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
	return setOpusParams(denproto.StripCandidates(answer.SDP), browserOpus.SDPFmtpLine), nil
}

// attachShare puts the caller's screen, and its sound, on the sections the
// den receives them on, once an offer has them, while the caller shares.
// Pion's AddTrack takes the first section of the track's kind that the den
// receives on and the caller doesn't send on yet, which is the den's.
func (c *TestCaller) attachShare(offer string) error {
	c.mu.Lock()
	screen := c.sharing && !c.screenSending && receivesOn(offer, "video") > 0
	sound := c.sharing && c.withSound && !c.soundSending && receivesOn(offer, "audio") > 1
	c.mu.Unlock()
	if screen {
		sender, err := c.pc.AddTrack(c.screen)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.screenSending = true
		c.mu.Unlock()
		go c.show()
		go c.countKeyframeRequests(sender)
	}
	if sound {
		if _, err := c.pc.AddTrack(c.sound); err != nil {
			return err
		}
		c.mu.Lock()
		c.soundSending = true
		c.mu.Unlock()
		go c.play()
	}
	return nil
}

// receivesOn counts the sections of a kind an offer receives on.
func receivesOn(offer, kind string) int {
	n := 0
	for _, section := range strings.Split(offer, "\r\nm=")[1:] {
		if strings.HasPrefix(section, kind+" ") && strings.Contains(section, "\r\na=recvonly") {
			n++
		}
	}
	return n
}

// Share has the caller share its screen, with its sound if sound, from the
// next offer that has the den's sections for them, or at once if earlier
// ones had them already.
func (c *TestCaller) Share(sound bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sharing, c.withSound = true, sound
}

// StopSharing has the caller send nothing of its share, as a browser whose
// member stopped it.
func (c *TestCaller) StopSharing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sharing = false
}

func (c *TestCaller) isSharing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sharing
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
		pkt := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: opusType, SequenceNumber: seq, Timestamp: ts},
			Payload: []byte{0xf8, 0xff, 0xfe, byte(seq >> 8), byte(seq)}}
		_ = pkt.Header.SetExtension(testExtensionID, []byte(testExtension))
		_ = c.mic.WriteRTP(pkt)
	}
}

// show sends the caller's screen while it shares: frames of three packets
// of screenBytes, the last of each marked, each packet carrying the
// extension the den must drop.
func (c *TestCaller) show() {
	t := time.NewTicker(time.Second / time.Duration(c.screenRate))
	defer t.Stop()
	var seq uint16
	var ts uint32
	for n := 1; ; n++ {
		select {
		case <-c.stop:
			return
		case <-t.C:
		}
		if !c.isSharing() {
			continue
		}
		seq++
		end := n%3 == 0
		pkt := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: vp9Type, SequenceNumber: seq, Timestamp: ts, Marker: end},
			Payload: make([]byte, c.screenBytes)}
		if end {
			ts += 3000
		}
		_ = pkt.Header.SetExtension(testExtensionID, []byte(testExtension))
		_ = c.screen.WriteRTP(pkt)
	}
}

// play sends the share's sound while the caller shares, as speak does.
func (c *TestCaller) play() {
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	var seq uint16
	var ts uint32
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
		}
		if !c.isSharing() {
			continue
		}
		seq++
		ts += 960
		pkt := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: opusType, SequenceNumber: seq, Timestamp: ts},
			Payload: []byte{0xfc, 0xff, 0xfe, byte(seq >> 8), byte(seq)}}
		_ = c.sound.WriteRTP(pkt)
	}
}

// countKeyframeRequests counts the den's requests for keyframes of the
// caller's screen.
func (c *TestCaller) countKeyframeRequests(sender *webrtc.RTPSender) {
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
			if _, ok := pkt.(*rtcp.PictureLossIndication); ok {
				c.mu.Lock()
				c.keyframeRequests++
				c.signal()
				c.mu.Unlock()
			}
		}
	}
}

// listen counts each stream's packets by kind: the den names the stream a
// member's voice comes in for them, and the one their share comes in with
// screenStream.
func (c *TestCaller) listen(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	key := track.Kind().String() + " " + track.StreamID()
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		c.mu.Lock()
		c.heard[key]++
		if bytes.Equal(pkt.Header.GetExtension(testExtensionID), []byte(testExtension)) {
			c.leaked++
		}
		c.signal()
		c.mu.Unlock()
	}
}

// signal wakes whoever waits on a count. The caller holds c.mu.
func (c *TestCaller) signal() {
	close(c.changed)
	c.changed = make(chan struct{})
}

// The keys listen counts under: a member's voice, and their share's screen
// and sound.
func voiceKey(member string) string     { return "audio " + member }
func sharedScreen(member string) string { return "video " + screenStream(member) }
func sharedSound(member string) string  { return "audio " + screenStream(member) }

// Heard reports how many packets have reached the caller from a member.
func (c *TestCaller) Heard(member string) int { return c.count(voiceKey(member)) }

// Saw reports how many packets of a member's screen have reached the
// caller, and HeardShare how many of its sound.
func (c *TestCaller) Saw(member string) int        { return c.count(sharedScreen(member)) }
func (c *TestCaller) HeardShare(member string) int { return c.count(sharedSound(member)) }

func (c *TestCaller) count(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.heard[key]
}

// Leaked reports how many packets arrived with the header extension the
// sending caller put on them.
func (c *TestCaller) Leaked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.leaked
}

// KeyframeRequests reports how many times the den has asked the caller for
// a keyframe of its screen.
func (c *TestCaller) KeyframeRequests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keyframeRequests
}

// WaitHeard waits until n more packets than now have come from a member.
func (c *TestCaller) WaitHeard(member string, n int, timeout time.Duration) error {
	return c.wait(func() int { return c.heard[voiceKey(member)] }, n, timeout, "packets from member "+member)
}

// WaitSaw waits until n more packets than now have come of a member's
// screen, and WaitHeardShare of its sound.
func (c *TestCaller) WaitSaw(member string, n int, timeout time.Duration) error {
	return c.wait(func() int { return c.heard[sharedScreen(member)] }, n, timeout, "packets of member "+member+"'s screen")
}

func (c *TestCaller) WaitHeardShare(member string, n int, timeout time.Duration) error {
	return c.wait(func() int { return c.heard[sharedSound(member)] }, n, timeout, "packets of member "+member+"'s share's sound")
}

// WaitKeyframeRequests waits until the den has asked for n more keyframes
// than now.
func (c *TestCaller) WaitKeyframeRequests(n int, timeout time.Duration) error {
	return c.wait(func() int { return c.keyframeRequests }, n, timeout, "keyframe requests")
}

// wait waits until a count, read with c.mu held, has grown by n.
func (c *TestCaller) wait(count func() int, n int, timeout time.Duration, what string) error {
	deadline := time.After(timeout)
	c.mu.Lock()
	want := count() + n
	for count() < want {
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-deadline:
			c.mu.Lock()
			got := count()
			c.mu.Unlock()
			return fmt.Errorf("had %d %s, not %d, within %s", got, what, want, timeout)
		case <-c.stop:
			return errors.New("the caller closed")
		}
		c.mu.Lock()
	}
	c.mu.Unlock()
	return nil
}

// RequestKeyframe asks the den for a keyframe of a member's screen, as a
// viewer's browser does when it lost packets it can't decode without.
func (c *TestCaller) RequestKeyframe(member string) error {
	for _, r := range c.pc.GetReceivers() {
		for _, t := range r.Tracks() {
			if t.Kind() == webrtc.RTPCodecTypeVideo && t.StreamID() == screenStream(member) {
				return c.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(t.SSRC())}})
			}
		}
	}
	return fmt.Errorf("no screen of member %s reaches the caller", member)
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
