package relay

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// standIn stands in for a den that introduces a member's installs: it
// seals each note in a box under the member's seal, as their Dens does,
// passes it to their other devices, or the one it names, and opens it
// there as each one's Dens would.
type standIn struct {
	t      *testing.T
	key    []byte
	denID  []byte
	member int64

	mu       sync.Mutex
	installs []*install
	calls    []string
	// holding keeps the boxes passed until let go, held, as a slow den.
	holding bool
	held    []func()
}

// hold keeps the boxes passed from now until letGo passes them, in order.
func (s *standIn) hold() {
	s.mu.Lock()
	s.holding = true
	s.mu.Unlock()
}

func (s *standIn) letGo() {
	s.mu.Lock()
	held := s.held
	s.holding, s.held = false, nil
	s.mu.Unlock()
	for _, f := range held {
		f()
	}
}

func newStandIn(t *testing.T) *standIn {
	denID := denproto.Random(denproto.IDSize)
	key, err := denproto.RelayKey(denproto.NewSeal(), denID)
	if err != nil {
		t.Fatal(err)
	}
	return &standIn{t: t, key: key, denID: denID, member: 7}
}

// install is one of the member's installs on the stand-in den: its device
// key's ID there, the call it holds, its hub, and the notes it sent.
type install struct {
	s     *standIn
	keyID denproto.Bytes
	hub   *fakeHub
	relay *Relay
	stop  context.CancelFunc
	done  chan struct{}

	mu   sync.Mutex
	call string
	sent []string
}

func (s *standIn) install(name string) *install {
	return s.installAs(name, NewID(), denproto.Random(denproto.IDSize))
}

// installAs is an install with its Relay ID and device key, as one started
// again has.
func (s *standIn) installAs(name string, id ID, keyID denproto.Bytes) *install {
	i := &install{s: s, keyID: keyID, hub: newFakeHub()}
	dir := s.t.TempDir()
	// A failed test shows what each install logged.
	s.t.Cleanup(func() {
		if !s.t.Failed() {
			return
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*"))
		for _, f := range files {
			if b, err := os.ReadFile(f); err == nil {
				s.t.Logf("%s's log:\n%s", name, b)
			}
		}
	})
	log, err := xlog.New(dir, "debug")
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { log.Close() })
	i.relay = New(Config{ID: id, Name: name, Keys: true, CanKeys: true, CanSound: true, Loopback: true, Dens: i, Hub: i.hub, Log: log,
		Addrs: func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} }})
	s.mu.Lock()
	s.installs = append(s.installs, i)
	s.mu.Unlock()
	return i
}

func (i *install) run() {
	ctx, stop := context.WithCancel(context.Background())
	i.stop, i.done = stop, make(chan struct{})
	go func() {
		defer close(i.done)
		_ = i.relay.Run(ctx)
	}()
	i.s.t.Cleanup(i.halt)
}

func (i *install) halt() {
	i.stop()
	<-i.done
}

func (i *install) RelayDens() []string { return []string{"den"} }

func (i *install) MemberCalls() []string {
	i.s.mu.Lock()
	defer i.s.mu.Unlock()
	return slices.Clone(i.s.calls)
}

func (i *install) CallDen() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.call
}

func (i *install) SendRelay(den string, to denproto.Bytes, b []byte) error {
	box, err := denproto.SealRelayBox(i.s.key, denproto.RelayBoxAD(i.s.denID, i.s.member, i.keyID, to), b)
	if err != nil {
		return err
	}
	if n, err := decodeNote(b, time.Now(), func(netip.Addr) bool { return true }); err == nil {
		i.mu.Lock()
		i.sent = append(i.sent, n.Kind)
		i.mu.Unlock()
	}
	i.s.pass(i.keyID, to, box)
	return nil
}

// remove takes an install off the den, as one that's gone.
func (s *standIn) remove(i *install) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installs = slices.DeleteFunc(s.installs, func(x *install) bool { return x == i })
}

// pass opens a box on each of the member's devices it's for.
func (s *standIn) pass(from, to denproto.Bytes, box []byte) {
	s.mu.Lock()
	if s.holding {
		s.held = append(s.held, func() { s.pass(from, to, box) })
		s.mu.Unlock()
		return
	}
	installs := slices.Clone(s.installs)
	s.mu.Unlock()
	for _, j := range installs {
		if bytes.Equal(j.keyID, from) || (len(to) > 0 && !bytes.Equal(j.keyID, to)) {
			continue
		}
		note, err := denproto.OpenRelayBox(s.key, denproto.RelayBoxAD(s.denID, s.member, from, to), box)
		if err != nil {
			s.t.Errorf("a box didn't open on the member's other device: %v", err)
			continue
		}
		j.relay.Box("den", from, note)
	}
}

func (i *install) sentKinds() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return slices.Clone(i.sent)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Two installs of one member link over loopback, as development instances
// do, from boxes a stand-in den passes: the laptop, which holds no call,
// says hello, and the desk, holding the member's call, answers. The
// laptop's keys reach the desk's hub as states, a hold held and let go, a
// toggle fired once; each lists the other; and once the laptop stops, the
// desk lets go of what it held within half a second.
func TestTwoInstallsLink(t *testing.T) {
	s := newStandIn(t)
	desk, laptop := s.install("Desk"), s.install("Laptop")
	desk.call = "den"
	s.calls = []string{"den"}
	desk.run()
	laptop.run()

	waitFor(t, "the laptop handing its keys over", func() bool { return laptop.hub.taker() != nil })
	if desk.hub.taker() != nil {
		t.Error("the desk, which holds the call, hands its keys over")
	}
	take := laptop.hub.taker()
	device := laptop.relay.cfg.ID.String()
	take(shortcuts.Press{Action: "ptt", Down: true})
	waitFor(t, "push to talk held on the desk", func() bool { return slices.Equal(desk.hub.held(device), []string{"ptt"}) })
	take(shortcuts.Press{Action: "toggleMute", Down: true})
	take(shortcuts.Press{Action: "toggleMute", Down: false})
	waitFor(t, "toggle mute on the desk", func() bool { return len(desk.hub.fired()) > 0 })
	time.Sleep(300 * time.Millisecond)
	if got := desk.hub.fired(); !slices.Equal(got, []string{device + ":toggleMute"}) {
		t.Errorf("one press of toggle mute fired %v on the desk", got)
	}
	take(shortcuts.Press{Action: "ptt", Down: false})
	waitFor(t, "push to talk let go on the desk", func() bool { return len(desk.hub.held(device)) == 0 })

	laptop.relay.Devices()
	waitFor(t, "each listing the other", func() bool {
		l, d := laptop.relay.Devices(), desk.relay.Devices()
		return len(l) == 1 && l[0].Name == "Desk" && l[0].Linked && slices.Equal(l[0].Offers, []string{offerKeys}) &&
			len(d) == 1 && d[0].Name == "Laptop" && d[0].Linked
	})

	take(shortcuts.Press{Action: "ptt", Down: true})
	waitFor(t, "push to talk held again", func() bool { return slices.Equal(desk.hub.held(device), []string{"ptt"}) })
	stopped := time.Now()
	laptop.halt()
	waitFor(t, "the desk letting go", func() bool { return len(desk.hub.held(device)) == 0 })
	if d := time.Since(stopped); d > 500*time.Millisecond {
		t.Errorf("the desk let go %s after the laptop stopped", d)
	}
	waitFor(t, "the desk's link closing", func() bool {
		d := desk.relay.Devices()
		return len(d) == 1 && !d[0].Linked
	})
}

// A hello that names an address off the local network, or a stale one,
// gets no answer; and nothing answers a hello where no install holding the
// call has Keybind relay on, or once the member's call has gone.
func TestHellosRefused(t *testing.T) {
	s := newStandIn(t)
	desk := s.install("Desk")
	desk.call = "den"
	s.calls = []string{"den"}
	desk.run()
	from := denproto.Random(denproto.IDSize)
	send := func(n note) {
		b, err := n.encode()
		if err != nil {
			t.Fatal(err)
		}
		desk.relay.Box("den", from, b)
	}
	for _, n := range []note{aHello(time.Now(), "127.0.0.1", "8.8.8.8"), aHello(time.Now().Add(-2*time.Minute), "127.0.0.1")} {
		send(n)
	}
	desk.relay.SetKeys(false)
	send(aHello(time.Now(), "127.0.0.1"))
	desk.relay.SetKeys(true)
	desk.mu.Lock()
	desk.call = ""
	desk.mu.Unlock()
	send(aHello(time.Now(), "127.0.0.1"))
	time.Sleep(500 * time.Millisecond)
	if slices.Contains(desk.sentKinds(), kindAnswer) {
		t.Errorf("the desk answered: %v", desk.sentKinds())
	}
	// The same hello, from the local network to an install holding the
	// call with Keybind relay on, is answered.
	desk.mu.Lock()
	desk.call = "den"
	desk.mu.Unlock()
	send(aHello(time.Now(), "127.0.0.1"))
	waitFor(t, "the desk's answer", func() bool { return slices.Contains(desk.sentKinds(), kindAnswer) })
}

// A device that starts again before the other end of its link has failed,
// as one killed and started at once, links again at once: its hello
// replaces the link the desk still holds.
func TestRestartedInstallLinksAgain(t *testing.T) {
	s := newStandIn(t)
	desk, laptop := s.install("Desk"), s.install("Laptop")
	desk.call = "den"
	s.calls = []string{"den"}
	desk.run()
	laptop.run()
	waitFor(t, "the laptop linked", func() bool { return laptop.hub.taker() != nil })

	// The laptop dies without a word: its ports close before anything
	// could say goodbye.
	laptop.relay.mu.Lock()
	var locals []*local
	for _, p := range laptop.relay.peers {
		locals = append(locals, p.local)
	}
	laptop.relay.mu.Unlock()
	for _, l := range locals {
		l.close()
	}
	s.remove(laptop)
	laptop.halt()
	if d := desk.relay.Devices(); len(d) != 1 || !d[0].Linked {
		t.Fatalf("the desk lost the link at once: %+v", d)
	}

	again := s.installAs("Laptop", laptop.relay.cfg.ID, laptop.keyID)
	started := time.Now()
	again.run()
	waitFor(t, "the laptop linked again", func() bool { return again.hub.taker() != nil })
	if d := time.Since(started); d >= disconnectedAfter+failedAfter {
		t.Errorf("the laptop linked again %s after it started, as long as the old link takes to fail", d)
	}
	take := again.hub.taker()
	device := again.relay.cfg.ID.String()
	take(shortcuts.Press{Action: "ptt", Down: true})
	waitFor(t, "push to talk on the desk", func() bool { return slices.Equal(desk.hub.held(device), []string{"ptt"}) })
}

// A hello that went unanswered, as while the den holds the member's call
// for a device whose connection dropped, gets its answer at once once that
// device takes the call back: it says it's here, and the laptop asks again.
func TestTakingTheCallBackAnswers(t *testing.T) {
	s := newStandIn(t)
	desk, laptop := s.install("Desk"), s.install("Laptop")
	s.calls = []string{"den"}
	desk.run()
	laptop.run()
	waitFor(t, "the laptop's hello", func() bool { return slices.Contains(laptop.sentKinds(), kindHello) })
	time.Sleep(200 * time.Millisecond)
	if laptop.hub.taker() != nil {
		t.Fatal("linked with no device holding the call")
	}
	desk.mu.Lock()
	desk.call = "den"
	desk.mu.Unlock()
	took := time.Now()
	desk.relay.Changed()
	waitFor(t, "the laptop linked", func() bool { return laptop.hub.taker() != nil })
	if d := time.Since(took); d > 5*time.Second {
		t.Errorf("the laptop linked %s after the desk took the call", d)
	}
}
