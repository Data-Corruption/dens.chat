package shortcuts

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"golang.org/x/mod/semver"
)

// PollEvery is how often the helper reads the keys while an instance holds
// a call.
const PollEvery = 10 * time.Millisecond

// rescanEvery is how often the helper looks for instances an install added,
// and retryMax the longest it waits to reach one again.
const (
	rescanEvery = time.Minute
	retryMax    = 30 * time.Second
)

// KeyReader reads whether keys are down: host.KeyState, or a test's.
type KeyReader interface {
	Key(code string) (host.Key, bool)
	Down(k host.Key) bool
	Modifiers() (ctrl, alt, shift, meta bool)
}

// Helper is the desktop user's side, which dens shortcuts runs at login.
// It keeps a stream to every instance that answers it, reads the keys
// bound there while that instance holds a call, and sends it each press
// and release. Keys reach other programs only as far as the instances'
// pages act on them.
type Helper struct {
	Version string
	// Instances lists the instances to try, and Dial gives each one's
	// control client.
	Instances func() ([]string, error)
	Dial      func(instance string) (control.Client, error)
	// Keys reads the keys; where the system offers no way to, it's nil and
	// Problem says why, which the services log. Note says what the system
	// keeps from the reader, for the settings.
	Keys    KeyReader
	Problem string
	Note    string
	// Restart takes up the installed binary when a service runs a newer
	// version than this helper; Run returns what it returns.
	Restart func() error
	Logf    func(format string, args ...any)

	mu      sync.Mutex
	remotes map[*remote]struct{}
	wake    chan struct{}
}

// remote is one instance's stream and what it asks the helper to watch.
type remote struct {
	name   string
	stream *control.Stream
	call   bool
	bound  map[string]bound
	held   map[string]bool
}

// bound is a key resolved to what the reader reads.
type bound struct {
	key                    host.Key
	ctrl, alt, shift, meta bool
}

// mods counts the modifiers a key is bound with.
func (b bound) mods() int {
	n := 0
	for _, m := range []bool{b.ctrl, b.alt, b.shift, b.meta} {
		if m {
			n++
		}
	}
	return n
}

var (
	errReplaced = errors.New("a newer helper took over")
	errRestart  = errors.New("a service runs a newer version")
)

// Run serves the instances until ctx ends, a newer helper takes over, no
// instance is left that answers this account, or a service runs a newer
// version, when it restarts into the installed binary.
func (h *Helper) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	h.remotes = make(map[*remote]struct{})
	h.wake = make(chan struct{}, 1)
	go h.poll(ctx)

	started := make(map[string]bool)
	refused := make(chan string, 16)
	gone := make(map[string]bool)
	ticker := time.NewTicker(rescanEvery)
	defer ticker.Stop()
	for scan := true; ; {
		if scan {
			names, err := h.Instances()
			if err != nil {
				h.Logf("Couldn't list the instances: %v", err)
			} else if len(names) == 0 {
				h.Logf("No instance is installed; stopping.")
				return nil
			}
			for _, name := range names {
				if !started[name] {
					started[name] = true
					go h.keep(ctx, cancel, name, refused)
				}
			}
		}
		scan = false
		select {
		case <-ctx.Done():
			switch cause := context.Cause(ctx); {
			case errors.Is(cause, errRestart):
				h.Logf("A service runs a newer version; restarting into it.")
				return h.Restart()
			case errors.Is(cause, errReplaced):
				h.Logf("A newer helper took over; stopping.")
				return nil
			default:
				return ctx.Err()
			}
		case name := <-refused:
			gone[name] = true
			if len(gone) == len(started) {
				h.Logf("No instance answers this account; stopping.")
				return nil
			}
		case <-ticker.C:
			scan = true
		}
	}
}

// keep holds a stream to one instance, reconnecting as its service stops
// and starts, until the instance refuses this account.
func (h *Helper) keep(ctx context.Context, cancel context.CancelCauseFunc, name string, refused chan<- string) {
	wait := time.Second
	for ctx.Err() == nil {
		client, err := h.Dial(name)
		if err == nil {
			var s *control.Stream
			if s, err = client.Open(control.Request{Op: control.OpShortcuts}); err == nil {
				wait = time.Second
				err = h.serve(ctx, cancel, name, s)
			}
		}
		var userErr *control.UserError
		if errors.As(err, &userErr) || errors.Is(err, fs.ErrPermission) {
			// The instance answers another account, or a version without
			// shortcuts.
			select {
			case refused <- name:
			case <-ctx.Done():
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(2*wait, retryMax)
	}
}

// serve follows one stream until it ends.
func (h *Helper) serve(ctx context.Context, cancel context.CancelCauseFunc, name string, s *control.Stream) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	// Closing the stream as serve returns, or as the helper stops, ends
	// its Recv and its pings.
	context.AfterFunc(ctx, func() { _ = s.Close() })
	go s.KeepAlive(ctx)
	r := &remote{name: name, stream: s, bound: map[string]bound{}, held: map[string]bool{}}
	h.mu.Lock()
	h.remotes[r] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.remotes, r)
		h.mu.Unlock()
		h.nudge()
	}()
	for {
		msg, err := s.Recv()
		if err != nil {
			return err
		}
		switch msg.T {
		case msgHello:
			var hi hello
			if json.Unmarshal(msg.D, &hi) == nil && newer(hi.Version, h.Version) && h.Restart != nil {
				cancel(errRestart)
				return nil
			}
		case msgState:
			var st state
			if json.Unmarshal(msg.D, &st) != nil {
				continue
			}
			if err := s.Send(msgSource, h.setState(r, st)); err != nil {
				return err
			}
		case msgReplaced:
			cancel(errReplaced)
			return nil
		}
	}
}

// newer says whether a service's version is past the helper's. Versions
// that aren't semantic, as a development build's, are never newer.
func newer(service, helper string) bool {
	return semver.IsValid(service) && semver.IsValid(helper) && semver.Compare(service, helper) > 0
}

// setState resolves an instance's keys and reports what the helper covers.
func (h *Helper) setState(r *remote, st state) Source {
	src := Source{Covers: []string{}, Problem: h.Problem, Note: h.Note}
	h.mu.Lock()
	r.call = st.Call
	r.bound = map[string]bound{}
	if !r.call {
		// The page lets go of what's held when its call ends.
		clear(r.held)
	}
	for _, action := range types.VoiceActions {
		k, ok := st.Keys[action]
		if !ok || h.Keys == nil {
			continue
		}
		key, ok := h.Keys.Key(k.Code)
		if !ok {
			continue
		}
		r.bound[action] = bound{key: key, ctrl: k.Ctrl, alt: k.Alt, shift: k.Shift, meta: k.Meta}
		src.Covers = append(src.Covers, action)
	}
	for action := range r.held {
		if _, ok := r.bound[action]; !ok {
			delete(r.held, action)
		}
	}
	h.mu.Unlock()
	h.nudge()
	return src
}

func (h *Helper) nudge() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// watching says whether an instance holds a call with keys to read.
func (h *Helper) watching() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for r := range h.remotes {
		if r.call && len(r.bound) > 0 {
			return true
		}
	}
	return false
}

// poll reads the keys every PollEvery while an instance holds a call, and
// sleeps otherwise.
func (h *Helper) poll(ctx context.Context) {
	for {
		if !h.watching() {
			select {
			case <-ctx.Done():
				return
			case <-h.wake:
				continue
			}
		}
		ticker := time.NewTicker(PollEvery)
		for h.watching() {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				h.read()
			case <-h.wake:
			}
		}
		ticker.Stop()
	}
}

type pressFor struct {
	r *remote
	p Press
}

// read reads the bound keys once and sends what changed. A key goes down
// while its own modifiers are held, whatever else is, since a game's
// player holds Shift to run and still pushes to talk, and up as it's let
// go, whichever modifiers went first. Of two actions on one key, the one
// with more modifiers, all held, takes the press, as in the page.
func (h *Helper) read() {
	if h.Keys == nil {
		return
	}
	ctrl, alt, shift, meta := h.Keys.Modifiers()
	held := func(b bound) bool {
		return (!b.ctrl || ctrl) && (!b.alt || alt) && (!b.shift || shift) && (!b.meta || meta)
	}
	var presses []pressFor
	h.mu.Lock()
	for r := range h.remotes {
		if !r.call {
			continue
		}
		for _, action := range types.VoiceActions {
			b, ok := r.bound[action]
			if !ok {
				continue
			}
			down := h.Keys.Down(b.key)
			pressed := down && held(b)
			for other, o := range r.bound {
				if pressed && other != action && o.key == b.key && held(o) && o.mods() > b.mods() {
					pressed = false
				}
			}
			switch {
			case pressed && !r.held[action]:
				r.held[action] = true
				presses = append(presses, pressFor{r, Press{Action: action, Down: true}})
			case !down && r.held[action]:
				r.held[action] = false
				presses = append(presses, pressFor{r, Press{Action: action, Down: false}})
			}
		}
	}
	h.mu.Unlock()
	for _, x := range presses {
		if x.r.stream.Send(msgPress, x.p) != nil {
			_ = x.r.stream.Close()
		}
	}
}
