package relay

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
)

// fakeHub records what Relay hands the shortcut hub: each device's holds
// as they change, the toggles fired, and the hand-over of this install's
// own presses.
type fakeHub struct {
	mu      sync.Mutex
	holds   map[string][]string
	toggles []string
	take    func(shortcuts.Press)
	calls   []string
}

func newFakeHub() *fakeHub { return &fakeHub{holds: map[string][]string{}} }

func (h *fakeHub) SetRelay(take func(shortcuts.Press)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.take = take
}

func (h *fakeHub) RelayHolds(device string, held []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, device+":"+strings.Join(held, ","))
	h.holds[device] = slices.Clone(held)
}

func (h *fakeHub) RelayToggle(device, action string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toggles = append(h.toggles, device+":"+action)
}

func (h *fakeHub) held(device string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.holds[device])
}

func (h *fakeHub) fired() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.toggles)
}

func (h *fakeHub) taker() func(shortcuts.Press) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.take
}

// clock is a fake clock that moves only when told.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Presses read on this install become states: every hold held, and each
// toggle's presses counted, busy while a key is held and for a moment
// after any change.
func TestKeysOut(t *testing.T) {
	c := &clock{t: time.Unix(1_000_000, 0)}
	out := newKeysOut(c.now)
	c.add(time.Second)
	if out.busy() {
		t.Error("busy before any press")
	}
	out.take(shortcuts.Press{Action: "ptt", Down: true})
	out.take(shortcuts.Press{Action: "toggleMute", Down: true})
	out.take(shortcuts.Press{Action: "toggleMute", Down: false})
	out.take(shortcuts.Press{Action: "toggleMute", Down: true})
	out.take(shortcuts.Press{Action: "nothing", Down: true})
	s := out.state()
	if !slices.Equal(s.Held, []string{"ptt"}) || s.Counts["toggleMute"] != 2 || s.Counts["toggleDeafen"] != 0 || s.N != 1 {
		t.Errorf("the state is %+v", s)
	}
	if !out.busy() {
		t.Error("not busy while push to talk is held")
	}
	out.take(shortcuts.Press{Action: "ptt", Down: false})
	c.add(busyFor - time.Millisecond)
	if !out.busy() {
		t.Error("not busy just after a change")
	}
	c.add(time.Millisecond)
	if out.busy() {
		t.Error("busy once nothing has changed for a while")
	}
	if s := out.state(); len(s.Held) != 0 || s.N != 2 {
		t.Errorf("the state after the release is %+v", s)
	}
}

// Another device's states reach the hub as a second helper's would: a
// hold as held and let go, a toggle fired once as its count moves and
// never by a state repeated or one late, the first state's counts fired
// not at all, and everything let go once the device is quiet for 300 ms.
func TestKeysIn(t *testing.T) {
	c := &clock{t: time.Unix(1_000_000, 0)}
	hub := newFakeHub()
	in := newKeysIn(hub, "laptop", c.now)
	in.apply(keysMsg{N: 1, Counts: map[string]uint32{"toggleMute": 7, "toggleDeafen": 2}})
	if len(hub.fired()) != 0 {
		t.Errorf("the first state fired %v", hub.fired())
	}
	in.apply(keysMsg{N: 2, Held: []string{"ptt"}, Counts: map[string]uint32{"toggleMute": 7, "toggleDeafen": 2}})
	if !slices.Equal(hub.held("laptop"), []string{"ptt"}) {
		t.Errorf("held %v", hub.held("laptop"))
	}
	in.apply(keysMsg{N: 3, Held: []string{"ptt"}, Counts: map[string]uint32{"toggleMute": 8, "toggleDeafen": 2}})
	in.apply(keysMsg{N: 4, Held: []string{"ptt"}, Counts: map[string]uint32{"toggleMute": 8, "toggleDeafen": 2}})
	in.apply(keysMsg{N: 3, Held: []string{"ptt"}, Counts: map[string]uint32{"toggleMute": 8, "toggleDeafen": 2}})
	if got := hub.fired(); !slices.Equal(got, []string{"laptop:toggleMute"}) {
		t.Errorf("one press of toggle mute fired %v", got)
	}
	// Two presses between states, as when one is lost, fire twice.
	in.apply(keysMsg{N: 6, Held: []string{"ptt", "toggleDeafen"}, Counts: map[string]uint32{"toggleMute": 8, "toggleDeafen": 4}})
	if got := hub.fired(); len(got) != 3 || got[2] != "laptop:toggleDeafen" {
		t.Errorf("two presses of toggle deafen fired %v", got)
	}
	if !slices.Equal(hub.held("laptop"), []string{"ptt"}) {
		t.Errorf("a toggle among what's held reached the hub as held: %v", hub.held("laptop"))
	}

	// Quiet for just under 300 ms holds on; at 300 ms it lets go.
	c.add(quietAfter - time.Millisecond)
	if in.check() || !slices.Equal(hub.held("laptop"), []string{"ptt"}) {
		t.Errorf("let go %s after the last word", quietAfter-time.Millisecond)
	}
	c.add(time.Millisecond)
	if !in.check() || len(hub.held("laptop")) != 0 {
		t.Errorf("held %v once quiet for %s", hub.held("laptop"), quietAfter)
	}
	// A word again holds it again; a ping keeps it held.
	in.apply(keysMsg{N: 7, Held: []string{"ptt"}, Counts: map[string]uint32{"toggleMute": 8, "toggleDeafen": 4}})
	c.add(quietAfter / 2)
	in.hear()
	c.add(quietAfter / 2)
	if in.check() || !slices.Equal(hub.held("laptop"), []string{"ptt"}) {
		t.Error("a ping didn't keep the hold")
	}
	in.letGo()
	if len(hub.held("laptop")) != 0 {
		t.Error("the link's end left a hold")
	}
}
