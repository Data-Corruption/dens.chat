package sfu

import (
	"net"
	"net/netip"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

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
	if m.peer, err = s.Join(room, id, m); err != nil {
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

// TestOffersFollowJoinsAndLeaves checks that each join and leave reaches
// the others as a new offer, and that a section a leaver retires carries
// the next joiner, so offers don't grow with every join.
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
