package relay

import (
	"slices"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
	"github.com/Data-Corruption/dens.chat/internal/types"
)

// Keys cross a link as states, not presses: every action held, and for
// each toggle a count of its presses, so a lost state costs the wait for
// the next, never a key stuck down or a toggle fired twice.
const (
	// A state goes every 20 ms while a key is held or just changed, and
	// every 100 ms otherwise.
	keysBusy = 20 * time.Millisecond
	keysIdle = 100 * time.Millisecond
	busyFor  = 200 * time.Millisecond
	// A device heard from for this long has let go of everything.
	quietAfter = 300 * time.Millisecond
	// mostToggles bounds the presses one state fires of a toggle.
	mostToggles = 8
)

// keysMsg is a state on the link's now channel.
type keysMsg struct {
	T      string            `json:"t"`
	N      uint64            `json:"n"`
	Held   []string          `json:"held,omitempty"`
	Counts map[string]uint32 `json:"counts,omitempty"`
}

// keysOut is what this install's helper reads while another of the
// member's devices holds the call, for the links to send as states.
type keysOut struct {
	now func() time.Time

	mu      sync.Mutex
	held    map[string]bool
	counts  map[string]uint32
	changed time.Time
	seq     uint64
}

func newKeysOut(now func() time.Time) *keysOut {
	return &keysOut{now: now, held: map[string]bool{}, counts: map[string]uint32{}}
}

// take is a press the helper read.
func (k *keysOut) take(p shortcuts.Press) {
	if !slices.Contains(types.VoiceActions, p.Action) {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if hold(p.Action) {
		if k.held[p.Action] == p.Down {
			return
		}
		k.held[p.Action] = p.Down
	} else if p.Down {
		k.counts[p.Action]++
	} else {
		return
	}
	k.changed = k.now()
}

// state is the next state to send.
func (k *keysOut) state() keysMsg {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.seq++
	m := keysMsg{T: "keys", N: k.seq, Counts: map[string]uint32{}}
	for _, a := range types.VoiceActions {
		if hold(a) {
			if k.held[a] {
				m.Held = append(m.Held, a)
			}
		} else {
			m.Counts[a] = k.counts[a]
		}
	}
	return m
}

func hold(action string) bool { return action == "ptt" || action == "pushMute" }

// keysIn is another device's keys, as its states say, which it hands the
// hub as a second helper's: what it holds, kept by its name for the hub,
// and its toggles fired as their counts move. A device heard from for
// 300 ms has let go of everything.
type keysIn struct {
	hub    Hub
	device string
	now    func() time.Time

	mu     sync.Mutex
	seq    uint64
	counts map[string]uint32 // as last heard on this link; nil before the first state
	heard  time.Time
	holds  bool // whether the hub holds anything for it
}

func newKeysIn(hub Hub, device string, now func() time.Time) *keysIn {
	return &keysIn{hub: hub, device: device, now: now, heard: now()}
}

// hear notes a word from the device, of any kind.
func (k *keysIn) hear() {
	k.mu.Lock()
	k.heard = k.now()
	k.mu.Unlock()
}

// apply takes a state. One older than the last, as the now channel may
// deliver, goes no further; the first on a link sets the toggles' counts
// without firing them.
func (k *keysIn) apply(m keysMsg) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.heard = k.now()
	if m.N <= k.seq {
		return
	}
	k.seq = m.N
	held := slices.DeleteFunc(slices.Clone(m.Held), func(a string) bool { return !hold(a) })
	k.hub.RelayHolds(k.device, held)
	k.holds = len(held) > 0
	first := k.counts == nil
	if first {
		k.counts = map[string]uint32{}
	}
	for _, a := range types.VoiceActions {
		n, ok := m.Counts[a]
		if hold(a) || !ok {
			continue
		}
		moved := n - k.counts[a]
		k.counts[a] = n
		if first {
			continue
		}
		for range min(moved, mostToggles) {
			k.hub.RelayToggle(k.device, a)
		}
	}
}

// check lets go of what the device held once it's been quiet too long,
// and reports whether it has.
func (k *keysIn) check() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	quiet := k.now().Sub(k.heard) >= quietAfter
	if quiet && k.holds {
		k.hub.RelayHolds(k.device, nil)
		k.holds = false
	}
	return quiet
}

// letGo lets go of everything the device held, as its link ends.
func (k *keysIn) letGo() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.hub.RelayHolds(k.device, nil)
	k.holds = false
}

// busy says whether keys are held or just changed, which sends states
// more often.
func (k *keysOut) busy() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, down := range k.held {
		if down {
			return true
		}
	}
	return k.now().Sub(k.changed) < busyFor
}
