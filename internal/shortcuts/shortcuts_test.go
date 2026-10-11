package shortcuts

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func testLog(t *testing.T) *xlog.Logger {
	t.Helper()
	log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	return log
}

// serve puts hub behind a real control endpoint answering allowed, and
// returns a client for it.
func serve(t *testing.T, hub *Hub, allowed host.Identity) control.Client {
	t.Helper()
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(t.TempDir(), "control.sock")
	if runtime.GOOS == "windows" {
		endpoint = `\\.\pipe\dens-shortcuts-test-` + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	}
	ln, err := host.ListenControl(endpoint, me)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &control.Server{Allowed: allowed, Log: testLog(t), Handlers: map[string]control.Handler{
		control.OpShortcuts: func(context.Context, control.Request) (control.Reply, error) {
			return control.Reply{Stream: hub.Serve}, nil
		},
	}}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return control.Client{Endpoint: endpoint, Server: host.ControlServer{Account: me}, Timeout: 5 * time.Second}
}

// fakeKeys is a keyboard the test presses.
type fakeKeys struct {
	mu                     sync.Mutex
	codes                  map[string]host.Key
	down                   map[host.Key]bool
	ctrl, alt, shift, meta bool
}

func newFakeKeys() *fakeKeys {
	return &fakeKeys{codes: map[string]host.Key{"KeyV": 1, "Mouse4": 2, "F8": 3}, down: map[host.Key]bool{}}
}

func (f *fakeKeys) Key(code string) (host.Key, bool) {
	k, ok := f.codes[code]
	return k, ok
}

func (f *fakeKeys) Down(k host.Key) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.down[k]
}

func (f *fakeKeys) Modifiers() (ctrl, alt, shift, meta bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ctrl, f.alt, f.shift, f.meta
}

func (f *fakeKeys) set(k host.Key, down bool) {
	f.mu.Lock()
	f.down[k] = down
	f.mu.Unlock()
}

func (f *fakeKeys) mods(ctrl, shift bool) {
	f.mu.Lock()
	f.ctrl, f.shift = ctrl, shift
	f.mu.Unlock()
}

func helperFor(t *testing.T, version string, client control.Client, keys KeyReader) *Helper {
	return &Helper{
		Version:   version,
		Instances: func() ([]string, error) { return []string{"main"}, nil },
		Dial:      func(string) (control.Client, error) { return client, nil },
		Keys:      keys,
		Logf:      t.Logf,
	}
}

// running is a helper's Run: done closes as it returns err.
type running struct {
	done chan struct{}
	err  error
}

// stopped waits for Run to return, and reports whether it did.
func (r *running) stopped(within time.Duration) bool {
	select {
	case <-r.done:
		return true
	case <-time.After(within):
		return false
	}
}

// run runs h until it returns or the test ends.
func run(t *testing.T, h *Helper) *running {
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{done: make(chan struct{})}
	go func() {
		r.err = h.Run(ctx)
		close(r.done)
	}()
	t.Cleanup(func() {
		cancel()
		if !r.stopped(5 * time.Second) {
			t.Error("the helper didn't stop")
		}
	})
	return r
}

func waitStatus(t *testing.T, events <-chan Event, ok func(*Status) bool) *Status {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e, open := <-events:
			if !open {
				t.Fatal("the page's events closed")
			}
			if e.Status != nil && ok(e.Status) {
				return e.Status
			}
		case <-deadline:
			t.Fatal("the status never came")
		}
	}
}

func waitPress(t *testing.T, events <-chan Event, want Press) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Press == nil {
				continue
			}
			if *e.Press != want {
				t.Fatalf("press = %+v, want %+v", *e.Press, want)
			}
			return
		case <-deadline:
			t.Fatalf("no press %+v", want)
		}
	}
}

func noPress(t *testing.T, events <-chan Event, why string) {
	t.Helper()
	deadline := time.After(10 * PollEvery)
	for {
		select {
		case e := <-events:
			if e.Press != nil {
				t.Fatalf("%s, yet the page got %+v", why, *e.Press)
			}
		case <-deadline:
			return
		}
	}
}

var keysV = map[string]types.VoiceKey{
	"ptt":        {Code: "KeyV", Label: "V"},
	"toggleMute": {Code: "Mouse4", Label: "Mouse 4", Ctrl: true},
	"pushMute":   {Code: "Unmapped", Label: "?"},
}

func TestPressesReachThePagesOnlyDuringACall(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.0.0", keysV, testLog(t))
	keys := newFakeKeys()
	run(t, helperFor(t, "v1.0.0", serve(t, hub, me), keys))
	events, stop := hub.Watch()
	defer stop()

	st := waitStatus(t, events, func(s *Status) bool { return s.Helper && len(s.Covers) > 0 })
	if !slices.Equal(st.Covers, []string{"ptt", "toggleMute"}) || st.Keys["ptt"].Code != "KeyV" {
		t.Fatalf("status = %+v; the helper covers the keys it can read", st)
	}

	keys.set(1, true)
	noPress(t, events, "no call holds the install")
	keys.set(1, false)

	hub.SetCall(true)
	keys.set(1, true)
	waitPress(t, events, Press{Action: "ptt", Down: true})
	keys.set(1, false)
	waitPress(t, events, Press{Action: "ptt", Down: false})

	// Toggle mute is bound with Ctrl: the button alone does nothing, and
	// Shift held too doesn't stop it.
	keys.set(2, true)
	noPress(t, events, "toggle mute's Ctrl isn't held")
	keys.mods(true, true)
	waitPress(t, events, Press{Action: "toggleMute", Down: true})
	// Letting go of Ctrl first keeps it down until the button goes up.
	keys.mods(false, false)
	noPress(t, events, "the button is still held")
	keys.set(2, false)
	waitPress(t, events, Press{Action: "toggleMute", Down: false})

	hub.SetCall(false)
	keys.set(1, true)
	noPress(t, events, "the call ended")
}

func TestKeyHeldWithAnotherModifierStillPushes(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.0.0", keysV, testLog(t))
	keys := newFakeKeys()
	run(t, helperFor(t, "v1.0.0", serve(t, hub, me), keys))
	events, stop := hub.Watch()
	defer stop()
	waitStatus(t, events, func(s *Status) bool { return s.Helper && len(s.Covers) > 0 })
	hub.SetCall(true)
	// A player runs with Shift held and pushes to talk.
	keys.mods(false, true)
	keys.set(1, true)
	waitPress(t, events, Press{Action: "ptt", Down: true})
}

func TestNewKeysReachTheHelper(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.0.0", nil, testLog(t))
	keys := newFakeKeys()
	run(t, helperFor(t, "v1.0.0", serve(t, hub, me), keys))
	events, stop := hub.Watch()
	defer stop()
	waitStatus(t, events, func(s *Status) bool { return s.Helper })
	hub.SetCall(true)
	hub.SetKeys(map[string]types.VoiceKey{"pushMute": {Code: "F8", Label: "F8"}})
	st := waitStatus(t, events, func(s *Status) bool { return len(s.Covers) == 1 })
	if st.Covers[0] != "pushMute" || st.Keys["pushMute"].Label != "F8" {
		t.Fatalf("status = %+v", st)
	}
	keys.set(3, true)
	waitPress(t, events, Press{Action: "pushMute", Down: true})
}

func TestHelperWithoutKeysSaysWhy(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.0.0", keysV, testLog(t))
	h := helperFor(t, "v1.0.0", serve(t, hub, me), nil)
	h.Problem = "no way to read keys here"
	run(t, h)
	events, stop := hub.Watch()
	defer stop()
	st := waitStatus(t, events, func(s *Status) bool { return s.Helper && s.Problem != "" })
	if len(st.Covers) != 0 || st.Problem != "no way to read keys here" {
		t.Fatalf("status = %+v", st)
	}
}

func TestPagesLearnTheHelperWent(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.0.0", keysV, testLog(t))
	h := helperFor(t, "v1.0.0", serve(t, hub, me), newFakeKeys())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	events, stop := hub.Watch()
	defer stop()
	waitStatus(t, events, func(s *Status) bool { return s.Helper })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want it cancelled", err)
	}
	st := waitStatus(t, events, func(s *Status) bool { return !s.Helper })
	if len(st.Covers) != 0 {
		t.Fatalf("status = %+v after the helper went", st)
	}
}

func TestNewerHelperReplacesTheLast(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.0.0", keysV, testLog(t))
	client := serve(t, hub, me)
	events, stop := hub.Watch()
	defer stop()
	first := run(t, helperFor(t, "v1.0.0", client, newFakeKeys()))
	waitStatus(t, events, func(s *Status) bool { return s.Helper })
	run(t, helperFor(t, "v1.0.0", client, newFakeKeys()))
	if !first.stopped(5 * time.Second) {
		t.Fatal("the replaced helper kept running")
	}
	if first.err != nil {
		t.Fatalf("the replaced helper stopped with %v", first.err)
	}
	hub.mu.Lock()
	connected := hub.helper != nil
	hub.mu.Unlock()
	if !connected {
		t.Fatal("the hub lost the newer helper")
	}
}

func TestHelperRestartsForANewerService(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.1.0", keysV, testLog(t))
	h := helperFor(t, "v1.0.0", serve(t, hub, me), newFakeKeys())
	restarted := errors.New("restarted")
	h.Restart = func() error { return restarted }
	r := run(t, h)
	if !r.stopped(5 * time.Second) {
		t.Fatal("the helper didn't restart for a newer service")
	}
	if !errors.Is(r.err, restarted) {
		t.Fatalf("Run = %v, want the restart's result", r.err)
	}

	// An older service, or a development build's, is no reason to.
	for _, versions := range [][2]string{{"v1.0.0", "v1.1.0"}, {"dev", "v1.0.0"}, {"v1.1.0", "dev"}} {
		if newer(versions[0], versions[1]) {
			t.Errorf("a service at %s looks newer than a helper at %s", versions[0], versions[1])
		}
	}
}

func TestHelperStopsWhereNothingAnswersIt(t *testing.T) {
	hub := NewHub("v1.0.0", keysV, testLog(t))
	client := serve(t, hub, host.Identity{ID: "an-account-that-isnt-this-one", Name: "bob"})
	r := run(t, helperFor(t, "v1.0.0", client, newFakeKeys()))
	if !r.stopped(5 * time.Second) {
		t.Fatal("the helper kept trying an instance that refuses it")
	}
	if r.err != nil {
		t.Fatalf("Run = %v", r.err)
	}

	h := helperFor(t, "v1.0.0", client, newFakeKeys())
	h.Instances = func() ([]string, error) { return nil, nil }
	r = run(t, h)
	if !r.stopped(5 * time.Second) {
		t.Fatal("the helper kept running with no instance installed")
	}
	if r.err != nil {
		t.Fatalf("Run with no instances = %v", r.err)
	}
}

func TestPageThatFallsBehindIsClosed(t *testing.T) {
	hub := NewHub("v1.0.0", nil, testLog(t))
	events, stop := hub.Watch()
	defer stop()
	for range pageBuffer + 1 {
		hub.SetKeys(keysV)
	}
	for range events {
	}
	// The page reconnects and starts again from the status.
	again, stopAgain := hub.Watch()
	defer stopAgain()
	if e := <-again; e.Status == nil || e.Status.Keys["ptt"].Code != "KeyV" {
		t.Fatalf("first event after reconnecting = %+v", e)
	}
}

func TestTheKeyWithMoreModifiersTakesThePress(t *testing.T) {
	me, _ := host.CurrentUser()
	hub := NewHub("v1.0.0", map[string]types.VoiceKey{
		"ptt":        {Code: "KeyV", Label: "V"},
		"toggleMute": {Code: "KeyV", Label: "V", Ctrl: true},
	}, testLog(t))
	keys := newFakeKeys()
	h := helperFor(t, "v1.0.0", serve(t, hub, me), keys)
	h.Note = "a system's limit"
	run(t, h)
	events, stop := hub.Watch()
	defer stop()
	st := waitStatus(t, events, func(s *Status) bool { return s.Helper && len(s.Covers) == 2 })
	if st.Note != "a system's limit" {
		t.Fatalf("note = %q", st.Note)
	}
	hub.SetCall(true)
	keys.mods(true, false)
	keys.set(1, true)
	waitPress(t, events, Press{Action: "toggleMute", Down: true})
	noPress(t, events, "Ctrl+V is toggle mute's, not push to talk's")
	keys.set(1, false)
	waitPress(t, events, Press{Action: "toggleMute", Down: false})
	keys.mods(false, false)
	keys.set(1, true)
	waitPress(t, events, Press{Action: "ptt", Down: true})
}
