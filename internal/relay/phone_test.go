package relay

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
)

// wanted records what a phone's Relay asks of the system, as it changes.
type wanted struct {
	mu   sync.Mutex
	seen []Wants
}

func (w *wanted) take(x Wants) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen = append(w.seen, x)
}

func (w *wanted) last() Wants {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.seen) == 0 {
		return Wants{}
	}
	return w.seen[len(w.seen)-1]
}

// statusOf waits for a status that ok takes, and returns it.
func statusOf(t *testing.T, what string, ch <-chan Status, ok func(Status) bool) Status {
	t.Helper()
	end := time.After(10 * time.Second)
	for {
		select {
		case st, open := <-ch:
			if !open {
				t.Fatalf("the statuses stopped before %s", what)
			}
			if ok(st) {
				return st
			}
		case <-end:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// The Android app (M20.3), with Keybind relay on, asks the system to let it
// reach its network, and links with nothing until it may: it says who it
// is through the den, but sends no hello. Allowed, it links to the desk
// holding the member's call, and its page hears which device that is, and
// whether the member is muted there, for the call's button, whose presses
// reach the desk as a helper's do.
func TestThePhoneLinksOnceAllowed(t *testing.T) {
	s := newStandIn(t)
	w := &wanted{}
	desk, phone := s.install("Desk"), s.phone("Phone", w.take)
	desk.call = "den"
	s.calls = []string{"den"}
	desk.run()
	phone.run()
	status, stop := phone.relay.Watch()
	defer stop()

	waitFor(t, "the phone asking for its network", func() bool { return w.last() == Wants{On: true} })
	lists(t, desk, phone, false)
	time.Sleep(500 * time.Millisecond)
	if slices.Contains(phone.sentKinds(), kindHello) || phone.hub.taker() != nil {
		t.Fatalf("the phone asked for a link before it may reach its network: %v", phone.sentKinds())
	}
	if !slices.Equal(phone.relay.Can(), []string{offerKeys}) {
		t.Errorf("the phone relays %v", phone.relay.Can())
	}

	phone.relay.SetLocal(true, false)
	waitFor(t, "the phone linked", func() bool { return phone.hub.taker() != nil })
	st := statusOf(t, "the desk holding the call", status, func(st Status) bool { return st.Call != nil })
	if *st.Call != (Holding{ID: desk.relay.cfg.ID.String(), Name: "Desk"}) {
		t.Errorf("the call's holder: %+v", *st.Call)
	}
	s.mu.Lock()
	s.muted = true
	s.mu.Unlock()
	phone.relay.Changed()
	statusOf(t, "the member muted there", status, func(st Status) bool { return st.Call != nil && st.Call.Muted })

	// The page's button, as the hub hands it over.
	device := phone.relay.cfg.ID.String()
	phone.hub.taker()(shortcuts.Press{Action: "ptt", Down: true})
	waitFor(t, "push to talk held on the desk", func() bool { return slices.Equal(desk.hub.held(device), []string{"ptt"}) })
	phone.hub.taker()(shortcuts.Press{Action: "ptt", Down: false})
	waitFor(t, "push to talk let go on the desk", func() bool { return len(desk.hub.held(device)) == 0 })

	// The call gone, the button goes.
	desk.mu.Lock()
	desk.call = ""
	desk.mu.Unlock()
	s.mu.Lock()
	s.calls = nil
	s.mu.Unlock()
	desk.relay.Changed()
	phone.relay.Changed()
	statusOf(t, "no call held", status, func(st Status) bool { return st.Call == nil })
}

// A phone listens to a computer's sound without sharing its own (M20.3).
// Listening, it asks the system to play while it does; the member refusing
// it its network ends the listening, saying so, and allowed, the next one
// plays.
func TestThePhoneListens(t *testing.T) {
	s := newStandIn(t)
	w := &wanted{}
	desk, phone := s.install("Desk"), s.phone("Phone", w.take)
	phone.relay.SetKeys(false)
	desk.relay.SetSound(true)
	desk.run()
	phone.run()
	lists(t, phone, desk, true)

	l, err := phone.relay.Listen(desk.relay.cfg.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the phone asking to play", func() bool { return w.last() == Wants{On: true, Listening: true} })
	time.Sleep(300 * time.Millisecond)
	if slices.Contains(phone.sentKinds(), kindHello) {
		t.Fatal("the phone asked for a link before it may reach its network")
	}
	phone.relay.SetLocal(false, true)
	ended(t, l, WhyLocal)
	waitFor(t, "the phone no longer asking", func() bool { return w.last() == Wants{} })
	if !phone.relay.Refused() {
		t.Error("the settings don't hear the member refused it")
	}

	phone.relay.SetLocal(true, false)
	l, err = phone.relay.Listen(desk.relay.cfg.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	sends(t, desk, 50, -1)
	if places, _ := heard(l, time.Second); len(places) == 0 {
		t.Fatal("no sound reached the phone")
	}
	l.Stop()
	waitFor(t, "the phone done playing", func() bool { return w.last() == Wants{} })
}
