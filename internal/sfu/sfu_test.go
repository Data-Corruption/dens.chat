package sfu

import (
	"net"
	"net/netip"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// testBitrate is the voice bitrate test calls ask for.
const testBitrate = 96000

// testSFU runs an SFU on loopback ports.
func testSFU(t *testing.T, cfg Config) (*SFU, int, int) {
	t.Helper()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Loopback = true
	s, err := New(udp, tcp, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, udp.LocalAddr().(*net.UDPAddr).Port, tcp.Addr().(*net.TCPAddr).Port
}

type offer struct {
	version int
	sdp     string
}

// member is one test member in a call: their caller, standing in for
// the browser, and the den's peer for them, with the client's part in
// between, writing the den's candidates into each offer.
type member struct {
	t      *testing.T
	id     string
	peer   *Peer
	caller *TestCaller
	offers chan offer

	mu      sync.Mutex
	got     []offer
	failed  chan struct{}
	closing sync.Once
}

func (m *member) Offer(version int, sdp string) { m.offers <- offer{version, sdp} }
func (m *member) Failed()                       { m.closing.Do(func() { close(m.failed) }) }

// join adds a member to a room. Unless answer is false, a goroutine
// answers each offer as the member's client and browser would.
func join(t *testing.T, s *SFU, udpPort, tcpPort int, room, id string, opts CallerOptions, answer bool) *member {
	t.Helper()
	opts.Loopback = true
	caller, err := NewTestCaller(opts)
	if err != nil {
		t.Fatal(err)
	}
	m := &member{t: t, id: id, caller: caller, offers: make(chan offer, 16), failed: make(chan struct{})}
	t.Cleanup(func() { _ = caller.Close() })
	if m.peer, err = s.Join(room, id, testBitrate, m); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.peer.Close)
	go func() {
		for o := range m.offers {
			m.mu.Lock()
			m.got = append(m.got, o)
			m.mu.Unlock()
			if !answer {
				continue
			}
			sdp, err := denproto.AddCandidates(o.sdp, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, udpPort, tcpPort)
			if err == nil {
				var reply string
				if reply, err = caller.Answer(sdp); err == nil {
					m.peer.Answer(o.version, reply)
					continue
				}
			}
			// A test that has ended closes its callers first.
			select {
			case <-caller.stop:
			default:
				t.Error(err)
			}
			return
		}
	}()
	return m
}

// offersSeen returns the offers the member has had so far.
func (m *member) offersSeen() []offer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]offer(nil), m.got...)
}

func (m *member) waitOffers(n int) []offer {
	m.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := m.offersSeen(); len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.t.Fatalf("member %s had %d offers, not %d", m.id, len(m.offersSeen()), n)
	return nil
}

func (m *member) hears(other *member) {
	m.t.Helper()
	if err := m.caller.WaitHeard(other.id, 20, 10*time.Second); err != nil {
		m.t.Fatal(err)
	}
}

func TestTwoMembersHearEachOther(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	alice.hears(bob)
	bob.hears(alice)
	if n := alice.caller.Leaked() + bob.caller.Leaked(); n != 0 {
		t.Errorf("%d packets kept the sender's header extension", n)
	}
	first := alice.offersSeen()[0].sdp
	for _, want := range []string{"a=ice-lite", "a=recvonly", "opus/48000/2"} {
		if !strings.Contains(first, want) {
			t.Errorf("the first offer lacks %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "a=candidate") {
		t.Error("an offer carries the den's candidates")
	}
}

// TestOnOneCPU runs a call as on a small machine, where Pion's background
// work comes late: two members joining at once must still hear each other.
func TestOnOneCPU(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	alice.hears(bob)
	bob.hears(alice)
}

func TestOverTCPAlone(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{TCP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	alice.hears(bob)
	bob.hears(alice)
}

// TestOffersAskForTheBitrate checks that every offer asks the browser for
// the den's Opus parameters: the re-offer an earlier member gets when
// another joins too, which Pion would build from the browser's answer.
func TestOffersAskForTheBitrate(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	alice.hears(bob)
	for who, offers := range map[string][]offer{"alice": alice.waitOffers(2), "bob": bob.waitOffers(1)} {
		for _, o := range offers {
			var params []string
			for _, line := range strings.Split(o.sdp, "\r\n") {
				if p, ok := strings.CutPrefix(line, "a=fmtp:111 "); ok {
					params = append(params, p)
				}
			}
			want := voiceParams(testBitrate)
			if len(params) != strings.Count(o.sdp, "m=audio") || slices.ContainsFunc(params, func(p string) bool { return p != want }) {
				t.Errorf("%s's offer %d asks for %q, not %q in every section", who, o.version, params, want)
			}
		}
	}
}

func TestOpusParamsInAnOffer(t *testing.T) {
	params := voiceParams(testBitrate)
	in := strings.Join([]string{
		"v=0",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111 0",
		"a=rtpmap:111 opus/48000/2",
		"a=fmtp:111 minptime=10;useinbandfec=1;stereo=1",
		"a=rtpmap:0 PCMU/8000",
		"a=fmtp:0 something=1",
		"m=audio 9 UDP/TLS/RTP/SAVPF 96",
		"a=rtpmap:96 OPUS/48000/2",
		"a=sendonly",
		"",
	}, "\r\n")
	want := strings.Join([]string{
		"v=0",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111 0",
		"a=rtpmap:111 opus/48000/2",
		"a=fmtp:111 " + params,
		"a=rtpmap:0 PCMU/8000",
		"a=fmtp:0 something=1",
		"m=audio 9 UDP/TLS/RTP/SAVPF 96",
		"a=rtpmap:96 OPUS/48000/2",
		"a=fmtp:96 " + params,
		"a=sendonly",
		"",
	}, "\r\n")
	if got := setOpusParams(in, params); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestShapeSections checks that each section of an offer gets its own
// shape: voice's Opus parameters, a share's sound's, and the screen's
// bitrate as b=AS and b=TIAS after its connection line, in place of any
// bandwidth lines it had.
func TestShapeSections(t *testing.T) {
	in := strings.Join([]string{
		"v=0",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111",
		"c=IN IP4 0.0.0.0",
		"a=mid:0",
		"a=rtpmap:111 opus/48000/2",
		"a=fmtp:111 minptime=10;useinbandfec=1",
		"m=video 9 UDP/TLS/RTP/SAVPF 98",
		"c=IN IP4 0.0.0.0",
		"b=AS:99999",
		"a=mid:1",
		"a=rtpmap:98 VP9/90000",
		"a=fmtp:98 profile-id=0",
		"a=recvonly",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111",
		"c=IN IP4 0.0.0.0",
		"a=mid:2",
		"a=rtpmap:111 opus/48000/2",
		"m=video 9 UDP/TLS/RTP/SAVPF 98",
		"a=mid:3",
		"a=rtpmap:98 VP9/90000",
		"",
	}, "\r\n")
	want := strings.Join([]string{
		"v=0",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111",
		"c=IN IP4 0.0.0.0",
		"a=mid:0",
		"a=rtpmap:111 opus/48000/2",
		"a=fmtp:111 " + voiceParams(64000),
		"m=video 9 UDP/TLS/RTP/SAVPF 98",
		"c=IN IP4 0.0.0.0",
		"b=AS:2500",
		"b=TIAS:2500000",
		"a=mid:1",
		"a=rtpmap:98 VP9/90000",
		"a=fmtp:98 profile-id=0",
		"a=recvonly",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111",
		"c=IN IP4 0.0.0.0",
		"a=mid:2",
		"a=rtpmap:111 opus/48000/2",
		"a=fmtp:111 " + soundParams,
		"m=video 9 UDP/TLS/RTP/SAVPF 98",
		"b=AS:1",
		"b=TIAS:250",
		"a=mid:3",
		"a=rtpmap:98 VP9/90000",
		"",
	}, "\r\n")
	shapes := map[string]shape{"0": {opus: voiceParams(64000)}, "1": {bitrate: 2_500_000}, "2": {opus: soundParams}, "3": {bitrate: 250}}
	if got := shapeSections(in, func(mid string) shape { return shapes[mid] }); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestOffersFollowJoinsAndLeaves(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	alice.hears(bob)
	carol := join(t, s, udpPort, tcpPort, "41", "1003", CallerOptions{}, true)
	carol.hears(alice)
	carol.hears(bob)
	alice.hears(carol)
	sections := func(sdp string) int { return strings.Count(sdp, "m=audio") }
	got := alice.waitOffers(3)
	if n := sections(got[2].sdp); n != 3 {
		t.Fatalf("alice's offer after carol joined has %d sections, not 3", n)
	}

	carol.peer.Close()
	got = alice.waitOffers(4)
	if !strings.Contains(got[3].sdp, "a=inactive") {
		t.Errorf("carol's section didn't retire:\n%s", got[3].sdp)
	}
	// Bob is still heard once the offer that retires carol is answered.
	alice.hears(bob)

	dave := join(t, s, udpPort, tcpPort, "41", "1004", CallerOptions{}, true)
	dave.hears(alice)
	alice.hears(dave)
	got = alice.waitOffers(5)
	if n := sections(got[4].sdp); n != 3 {
		t.Errorf("dave took a new section in alice's offer rather than carol's: %d sections", n)
	}
	for i, o := range got {
		if o.version != i+1 {
			t.Errorf("offer %d has version %d", i+1, o.version)
		}
	}
}

// TestHangingUpEndsTheCall checks that a browser closing its connection
// ends its member's call, and the others' offers retire their section.
func TestHangingUpEndsTheCall(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, true)
	// Over UDP, so the den hears bob hang up; see TestCaller.Close.
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	alice.hears(bob)
	_ = bob.caller.Close()
	select {
	case <-bob.failed:
	case <-time.After(10 * time.Second):
		t.Fatal("hanging up didn't end the call")
	}
	got := alice.waitOffers(3)
	if !strings.Contains(got[2].sdp, "a=inactive") {
		t.Errorf("bob's section didn't retire:\n%s", got[2].sdp)
	}
}

func TestUnansweredOfferFails(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{AnswerTimeout: 200 * time.Millisecond})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, false)
	select {
	case <-alice.failed:
	case <-time.After(5 * time.Second):
		t.Fatal("an offer that went unanswered didn't fail the call")
	}
	// A failed peer is out of the room: a newcomer gets no section for it.
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	if n := strings.Count(bob.waitOffers(1)[0].sdp, "m=audio"); n != 1 {
		t.Errorf("a newcomer's offer has %d sections, not 1", n)
	}
}

func TestAnswers(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, false)
	o := alice.waitOffers(1)[0]
	sdp, err := denproto.AddCandidates(o.sdp, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, udpPort, tcpPort)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := alice.caller.Answer(sdp)
	if err != nil {
		t.Fatal(err)
	}
	// An answer to another version is ignored, and the call goes on.
	alice.peer.Answer(o.version+1, reply)
	select {
	case <-alice.failed:
		t.Fatal("an answer to another version failed the call")
	case <-time.After(100 * time.Millisecond):
	}
	alice.peer.Answer(o.version, "v=0\r\nnot an answer\r\n")
	select {
	case <-alice.failed:
	case <-time.After(5 * time.Second):
		t.Fatal("an answer that doesn't fit its offer didn't fail the call")
	}
}

func TestRateLimit(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	loud := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{Rate: 2000}, true)
	ear := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	ear.hears(loud)
	start, from := time.Now(), ear.caller.Heard(loud.id)
	time.Sleep(time.Second)
	got := ear.caller.Heard(loud.id) - from
	// The bucket may start full, so allow its burst on top of the rate.
	if most := maxPacketsPerSecond*(1+time.Since(start).Seconds()) + 50; float64(got) > most {
		t.Errorf("forwarded %d packets in about a second, more than %.0f", got, most)
	}
	if got < maxPacketsPerSecond/2 {
		t.Errorf("forwarded only %d packets in about a second", got)
	}
}

// TestHeldCallKeepsItsMedia checks that a held call's media flows both
// ways while no offer reaches its member, and that the offer the call's
// changes need goes out once it resumes.
func TestHeldCallKeepsItsMedia(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	alice.hears(bob)
	bob.hears(alice)
	before := len(alice.waitOffers(2))

	alice.peer.Hold()
	bob.hears(alice)
	alice.hears(bob)
	carol := join(t, s, udpPort, tcpPort, "41", "1003", CallerOptions{}, true)
	carol.hears(bob)
	time.Sleep(300 * time.Millisecond)
	if n := len(alice.offersSeen()); n != before {
		t.Fatalf("a held call got %d offers, not %d", n, before)
	}

	alice.peer.Resume()
	got := alice.waitOffers(before + 1)
	if o := got[before]; o.version != before+1 || strings.Count(o.sdp, "m=audio") != 3 {
		t.Fatalf("the offer after resuming has version %d and %d sections", o.version, strings.Count(o.sdp, "m=audio"))
	}
	alice.hears(carol)
	carol.hears(alice)
}

// TestHoldSendsTheOfferThatWasOutAgain checks that an offer out when the
// call is held waits past its deadline, and goes again under its version
// once the call resumes, when the answer the client kept is taken.
func TestHoldSendsTheOfferThatWasOutAgain(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{AnswerTimeout: time.Second})
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, false)
	o := alice.waitOffers(1)[0]
	// The socket closes with the offer out, and the call is held at once,
	// well within the answer's timeout even under the race detector.
	alice.peer.Hold()
	sdp, err := denproto.AddCandidates(o.sdp, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, udpPort, tcpPort)
	if err != nil {
		t.Fatal(err)
	}
	// The client answers, but the answer is lost with the socket.
	reply, err := alice.caller.Answer(sdp)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-alice.failed:
		t.Fatal("a held call failed for an offer it couldn't answer")
	case <-time.After(1500 * time.Millisecond):
	}

	alice.peer.Resume()
	got := alice.waitOffers(2)
	if got[1].version != o.version || got[1].sdp != o.sdp {
		t.Fatalf("the offer sent again has version %d, and is the same: %t", got[1].version, got[1].sdp == o.sdp)
	}
	alice.peer.Answer(o.version, reply)
	alice.hears(bob)
	bob.hears(alice)
	select {
	case <-alice.failed:
		t.Fatal("the call failed after its answer")
	case <-time.After(500 * time.Millisecond):
	}
}

// TestICERestart checks that a restart's offer carries new ICE
// credentials, and that the call's audio crosses again after it.
func TestICERestart(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, false)
	// Before the first answer there's nothing to restart.
	alice.peer.Restart()
	time.Sleep(100 * time.Millisecond)
	if n := len(alice.offersSeen()); n != 1 {
		t.Fatalf("a restart before the first answer made %d offers", n)
	}
	alice.peer.Close()

	alice = join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	alice.hears(bob)
	bob.hears(alice)
	ufrag := func(sdp string) string {
		for _, line := range strings.Split(sdp, "\r\n") {
			if v, ok := strings.CutPrefix(line, "a=ice-ufrag:"); ok {
				return v
			}
		}
		return ""
	}
	before := alice.waitOffers(2)
	alice.peer.Restart()
	after := alice.waitOffers(len(before) + 1)
	if old, now := ufrag(before[len(before)-1].sdp), ufrag(after[len(before)].sdp); old == "" || old == now {
		t.Fatalf("the restart's offer kept its ICE username %q", now)
	}
	alice.hears(bob)
	bob.hears(alice)
}

// TestStaffMute checks that a muted member's audio reaches no one, and
// comes back once the mute is lifted.
func TestStaffMute(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{}, true)
	bob.hears(alice)
	alice.peer.SetMuted(true)
	// Packets already on their way may still land.
	time.Sleep(200 * time.Millisecond)
	from := bob.caller.Heard(alice.id)
	time.Sleep(500 * time.Millisecond)
	if n := bob.caller.Heard(alice.id) - from; n > 0 {
		t.Fatalf("bob heard %d packets from muted alice", n)
	}
	alice.hears(bob)
	alice.peer.SetMuted(false)
	bob.hears(alice)
}
