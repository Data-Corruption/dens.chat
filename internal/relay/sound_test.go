package relay

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
)

// piece is a piece of sound as the helper cuts it: 48 kHz, and 240 frames
// that all hold n.
func piece(n int) []byte {
	p := binary.LittleEndian.AppendUint32(nil, 48000)
	for range 240 * 2 {
		p = binary.LittleEndian.AppendUint16(p, uint16(n))
	}
	return p
}

// lists waits until one install lists another, sharing its sound or not.
func lists(t *testing.T, i, other *install, sharing bool) {
	t.Helper()
	id := other.relay.cfg.ID.String()
	waitFor(t, "the device listed", func() bool {
		for _, d := range i.relay.Devices() {
			if d.ID == id && slices.Contains(d.Offers, offerSound) == sharing {
				return true
			}
		}
		// Asked again at most every ten seconds: the test asks at once.
		i.relay.mu.Lock()
		i.relay.asked = time.Time{}
		i.relay.mu.Unlock()
		return false
	})
}

// sends waits until the desk's helper is asked for its sound, and sends n
// pieces of it, skipping the piece skip, as the path would lose it.
func sends(t *testing.T, desk *install, n, skip int) {
	t.Helper()
	waitFor(t, "the desk's helper asked for its sound", func() bool { return desk.hub.sounder() != nil })
	take := desk.hub.sounder()
	for i := range n {
		if i == skip {
			desk.relay.mu.Lock()
			k := desk.relay.listener.link
			desk.relay.mu.Unlock()
			k.soundMu.Lock()
			k.seq++
			k.position += 240
			k.soundMu.Unlock()
			continue
		}
		take(piece(i))
		time.Sleep(time.Millisecond)
	}
}

// heard reads the packets a listening got within a while.
func heard(l *Listening, within time.Duration) (places []uint32, values []int) {
	end := time.After(within)
	for {
		select {
		case p := <-l.Packets():
			places = append(places, binary.LittleEndian.Uint32(p))
			values = append(values, int(int16(binary.LittleEndian.Uint16(p[8:]))))
		case <-end:
			return
		}
	}
}

func ended(t *testing.T, l *Listening, why string) {
	t.Helper()
	select {
	case <-l.Done():
		if l.Why() != why {
			t.Errorf("the listening ended %q, not %q", l.Why(), why)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the listening never ended %q", why)
	}
}

// A device that shares its sound says so, and one that listens gets it:
// the laptop asks the desk for a link, through the stand-in den, and the
// desk's helper reads the sound, which reaches the laptop's listening in
// order, its places 240 frames apart, a piece lost on the way counted.
// The desk's pages hear the laptop listens. Stopped on the desk, the
// laptop's listening ends saying so, and the helper reads no more.
func TestSoundAcross(t *testing.T) {
	s := newStandIn(t)
	desk, laptop := s.install("Desk"), s.install("Laptop")
	desk.run()
	laptop.run()
	if _, err := laptop.relay.Listen(desk.relay.cfg.ID.String()); !errors.Is(err, ErrNotSharing) {
		t.Errorf("listening to a device not sharing: %v", err)
	}
	status, stopStatus := desk.relay.Watch()
	defer stopStatus()
	desk.relay.SetSound(true)
	lists(t, laptop, desk, true)

	l, err := laptop.relay.Listen(desk.relay.cfg.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "Desk" {
		t.Errorf("listening to %q", l.Name)
	}
	sends(t, desk, 100, 40)
	places, values := heard(l, time.Second)
	if len(places) != 99 {
		t.Fatalf("%d packets of 99 came", len(places))
	}
	for i := 1; i < len(places); i++ {
		if want := uint32(240); i != 40 && places[i]-places[i-1] != want || i == 40 && places[i]-places[i-1] != 2*want {
			t.Fatalf("packet %d at %d, after %d", i, places[i], places[i-1])
		}
	}
	if values[0] != 0 || values[98] != 99 {
		t.Errorf("the sound came as %d to %d", values[0], values[98])
	}
	if got, lost := l.got.Load(), l.lost.Load(); got != 99 || lost != 1 {
		t.Errorf("the listening counted %d packets and %d lost", got, lost)
	}
	waitFor(t, "the desk's pages hearing the laptop listens", func() bool {
		select {
		case st := <-status:
			return st.Sharing != nil && st.Sharing.Name == "Laptop"
		default:
			return false
		}
	})

	desk.relay.StopSharing()
	ended(t, l, WhyStopped)
	waitFor(t, "the desk's helper to stop", func() bool { return desk.hub.sounder() == nil })
}

// One listener to a source: the phone listening takes the desk's sound
// from the laptop, which is told; and a second page on the phone takes it
// from the first. Listening pauses sharing: the desk, listening to the
// laptop's sound in turn, stops sharing its own, and the phone hears so.
func TestOneListenerAtATime(t *testing.T) {
	s := newStandIn(t)
	desk, laptop, phone := s.install("Desk"), s.install("Laptop"), s.install("Phone")
	for _, i := range []*install{desk, laptop, phone} {
		i.run()
	}
	desk.relay.SetSound(true)
	lists(t, laptop, desk, true)
	lists(t, phone, desk, true)
	first, err := laptop.relay.Listen(desk.relay.cfg.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	sends(t, desk, 20, -1)
	if places, _ := heard(first, 300*time.Millisecond); len(places) == 0 {
		t.Fatal("the laptop heard nothing")
	}

	second, err := phone.relay.Listen(desk.relay.cfg.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	ended(t, first, WhyTaken)
	waitFor(t, "the phone taking over", func() bool {
		desk.relay.mu.Lock()
		defer desk.relay.mu.Unlock()
		return desk.relay.listener != nil && desk.relay.listener.id == phone.relay.cfg.ID
	})
	sends(t, desk, 20, -1)
	if places, _ := heard(second, 300*time.Millisecond); len(places) == 0 {
		t.Fatal("the phone heard nothing")
	}

	third, err := phone.relay.Listen(desk.relay.cfg.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	ended(t, second, WhyPage)
	sends(t, desk, 20, -1)
	if places, _ := heard(third, 300*time.Millisecond); len(places) == 0 {
		t.Fatal("the phone's second page heard nothing")
	}

	laptop.relay.SetSound(true)
	lists(t, desk, laptop, true)
	if _, err := desk.relay.Listen(laptop.relay.cfg.ID.String()); err != nil {
		t.Fatal(err)
	}
	ended(t, third, WhyUnshared)
	lists(t, phone, desk, false)
}

// Two hellos that cross, each install asking the other at once, as the
// laptop asks for keys while the desk, holding the call, asks for the
// laptop's sound: each answers the other's, and both keep the link of the
// greater attempt, which carries the keys one way and the sound the other.
func TestHellosThatCross(t *testing.T) {
	s := newStandIn(t)
	desk, laptop := s.install("Desk"), s.install("Laptop")
	desk.call = "den"
	s.calls = []string{"den"}
	laptop.relay.SetKeys(false)
	laptop.relay.SetSound(true)
	desk.run()
	laptop.run()
	lists(t, desk, laptop, true)
	s.hold()
	laptop.relay.SetKeys(true)
	waitFor(t, "the laptop's hello", func() bool { return slices.Contains(laptop.sentKinds(), kindHello) })
	l, err := desk.relay.Listen(laptop.relay.cfg.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the desk's hello", func() bool { return slices.Contains(desk.sentKinds(), kindHello) })
	s.letGo()

	waitFor(t, "the laptop handing its keys over", func() bool { return laptop.hub.taker() != nil })
	sends(t, laptop, 20, -1)
	if places, _ := heard(l, 300*time.Millisecond); len(places) == 0 {
		t.Fatal("the desk heard nothing")
	}
	device := laptop.relay.cfg.ID.String()
	laptop.hub.taker()(shortcuts.Press{Action: "ptt", Down: true})
	waitFor(t, "push to talk on the desk", func() bool { return slices.Equal(desk.hub.held(device), []string{"ptt"}) })
	desk.relay.mu.Lock()
	dp := desk.relay.peers[laptop.relay.cfg.ID]
	desk.relay.mu.Unlock()
	laptop.relay.mu.Lock()
	lp := laptop.relay.peers[desk.relay.cfg.ID]
	laptop.relay.mu.Unlock()
	if dp == nil || lp == nil || !slices.Equal(dp.attempt, lp.attempt) {
		t.Errorf("the two kept different links")
	}
}
