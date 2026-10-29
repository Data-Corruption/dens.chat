package shortcuts

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/notify"
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

// DesktopKeys is a session with a desktop that owns the keys (M7.2):
// host.DesktopKeys, the portal under Wayland, or a test's. A session binds
// once.
type DesktopKeys interface {
	Bind(ctx context.Context, actions []host.DesktopAction) (map[string]string, error)
	Presses() <-chan host.DesktopPress
	Changes() <-chan map[string]string
	Closed() <-chan struct{}
	CanConfigure() bool
	Configure() error
	Note() string
	Close() error
}

// Notifier shows notifications as the desktop's own (M12.3): host.Notifier,
// through the freedesktop notification service or Windows' toasts, or the
// Flatpak's portal, or a test's. Each shows under an ID of the helper's,
// which a newer one replaces, and Clicks delivers the ID of each the member
// clicks, where the desktop doesn't open its address itself.
type Notifier interface {
	Show(id string, n host.DesktopNotification) error
	Withdraw(id string) error
	Clicks() <-chan string
}

// Helper is the desktop user's side, which dens shortcuts runs at login.
// It keeps a stream to every instance that answers it, reads the keys
// bound there while that instance holds a call, and sends it each press
// and release. Keys reach other programs only as far as the instances'
// pages act on them. It shows their notifications where no page can.
type Helper struct {
	Version string
	// Instances lists the instances to try, and Open opens the stream to
	// each one's hub: over its control endpoint, or a pipe inside the
	// Flatpak's service (M9.3).
	Instances func() ([]string, error)
	Open      func(instance string) (*control.Stream, error)
	// Keys reads the keys; where the system offers no way to, it's nil and
	// Problem says why, which the services log. Note says what the system
	// keeps from the reader, for the settings.
	Keys    KeyReader
	Problem string
	Note    string
	// OpenDesktop opens a session with a desktop that owns the keys; nil,
	// or failing, where there's none. The helper binds the actions with it
	// once the member asks, or as it starts once they have, and opens
	// another when the desktop ends one or the member turns its dialog
	// down, since a session binds once.
	OpenDesktop func() (DesktopKeys, error)
	// Restart takes up the installed binary when a service runs a newer
	// version than this helper; Run returns what it returns.
	Restart func() error
	Logf    func(format string, args ...any)
	// Notifier shows the instances' notifications (M12.3), nil where the
	// desktop has nothing to show them with; OpenPage opens an instance's
	// page at a notification's message, as a click on it asks, paired as
	// dens open pairs it.
	Notifier Notifier
	OpenPage func(instance string, n notify.Notification) error

	mu      sync.Mutex
	remotes map[*remote]struct{}
	wake    chan struct{}
	ctx     context.Context
	// notes are the notifications showing, by the ID each shows under.
	notes map[string]note

	// desk is the desktop's session, deskNames its names for the actions
	// it bound (nil until it binds), deskBusy whether its dialog is up, and
	// deskDeclined whether the member turned it down this run, after which
	// only their asking binds again. deskHeld holds what the desktop
	// reported down, since it may repeat a held key's press.
	desk         DesktopKeys
	deskNames    map[string]string
	deskBusy     bool
	deskDeclined bool
	deskHeld     map[string]bool
	deskWake     chan struct{}
}

// UseSystem has the helper read the keys as the system lets it: through
// the desktop's global shortcuts portal where there's one, as under Wayland
// and from the Flatpak's sandbox, where the portal knows Dens by its app ID
// whatever the session (M9.3), and otherwise from the X11 session's
// keyboard. Where neither works, Problem says why. It returns what closes
// the reader.
func (h *Helper) UseSystem(sandboxed bool) (close func()) {
	desk, deskErr := host.OpenDesktopKeys(AppID, sandboxed)
	if deskErr == nil {
		first := desk
		h.OpenDesktop = func() (DesktopKeys, error) {
			if first != nil {
				d := first
				first = nil
				return d, nil
			}
			d, err := host.OpenDesktopKeys(AppID, sandboxed)
			if err != nil {
				return nil, err
			}
			return d, nil
		}
		return func() {}
	}
	keys, err := host.OpenKeyState()
	if err != nil {
		h.Problem = errors.Join(deskErr, err).Error()
		return func() {}
	}
	h.Keys, h.Note = keys, keys.Note()
	return func() { _ = keys.Close() }
}

// remote is one instance's stream and what it asks the helper to watch,
// with its page's origin.
type remote struct {
	name   string
	page   string
	stream *control.Stream
	keys   map[string]types.VoiceKey
	call   bool
	bound  map[string]bound
	held   map[string]bool
}

// note is a notification showing: its instance's, and what it shows.
type note struct {
	instance string
	n        notify.Notification
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
	h.deskWake = make(chan struct{}, 1)
	h.notes = make(map[string]note)
	h.ctx = ctx
	go h.poll(ctx)
	if h.Notifier != nil {
		go h.clicks(ctx)
	}
	if h.OpenDesktop != nil {
		h.reopenDesktop(nil)
		go h.desktopLoop(ctx)
	}

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
		s, err := h.Open(name)
		if err == nil {
			wait = time.Second
			err = h.serve(ctx, cancel, name, s)
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
			if json.Unmarshal(msg.D, &hi) != nil {
				continue
			}
			if newer(hi.Version, h.Version) && h.Restart != nil {
				cancel(errRestart)
				return nil
			}
			h.mu.Lock()
			r.page = hi.Page
			h.mu.Unlock()
		case msgState:
			var st state
			if json.Unmarshal(msg.D, &st) != nil {
				continue
			}
			if err := s.Send(msgSource, h.setState(r, st)); err != nil {
				return err
			}
			if st.Desktop {
				go h.bindDesktop(st.Keys, false)
			}
		case msgDesktop:
			var req desktopRequest
			if json.Unmarshal(msg.D, &req) != nil {
				continue
			}
			h.desktopRequest(r, req.Do)
		case msgNotify:
			var n notify.Notification
			if json.Unmarshal(msg.D, &n) == nil && n.Key != "" {
				h.notify(r, n)
			}
		case msgUnnotify:
			var u unnotify
			if json.Unmarshal(msg.D, &u) == nil {
				h.unnotify(r, u.Keys)
			}
		case msgReplaced:
			cancel(errReplaced)
			return nil
		}
	}
}

// notify shows an instance's notification, replacing its channel's. A click
// on it opens the page at its message: the helper does, paired, where the
// desktop hands the click back, and the desktop itself where it opens the
// address, as Windows does.
func (h *Helper) notify(r *remote, n notify.Notification) {
	if h.Notifier == nil {
		return
	}
	id := r.name + "/" + n.Key
	h.mu.Lock()
	h.notes[id] = note{instance: r.name, n: n}
	open := ""
	if r.page != "" {
		open = n.URL(r.page, "")
	}
	h.mu.Unlock()
	if err := h.Notifier.Show(id, host.DesktopNotification{Title: n.Heading(), Body: n.Text, Open: open}); err != nil {
		h.Logf("The desktop didn't show a notification: %v", err)
	}
}

// unnotify takes an instance's notifications back.
func (h *Helper) unnotify(r *remote, keys []string) {
	if h.Notifier == nil {
		return
	}
	for _, key := range keys {
		id := r.name + "/" + key
		h.mu.Lock()
		_, ok := h.notes[id]
		delete(h.notes, id)
		h.mu.Unlock()
		if ok {
			if err := h.Notifier.Withdraw(id); err != nil {
				h.Logf("The desktop didn't take a notification back: %v", err)
			}
		}
	}
}

// clicks opens the page at the message of each notification the member
// clicks, and takes the notification back, which most desktops do for a
// click by themselves.
func (h *Helper) clicks(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id, ok := <-h.Notifier.Clicks():
			if !ok {
				return
			}
			h.mu.Lock()
			nt, found := h.notes[id]
			delete(h.notes, id)
			h.mu.Unlock()
			if !found {
				continue
			}
			_ = h.Notifier.Withdraw(id)
			if h.OpenPage == nil {
				continue
			}
			if err := h.OpenPage(nt.instance, nt.n); err != nil {
				h.Logf("Couldn't open the page at a notification's message: %v", err)
			}
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
	h.mu.Lock()
	r.keys = st.Keys
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
	}
	for action := range r.held {
		if _, ok := r.bound[action]; !ok {
			delete(r.held, action)
		}
	}
	src := h.sourceLocked(r)
	h.mu.Unlock()
	h.nudge()
	return src
}

// sourceLocked is what the helper covers for an instance: the actions the
// desktop bound where one owns the keys, and otherwise those whose keys it
// reads itself.
func (h *Helper) sourceLocked(r *remote) Source {
	if h.desk != nil {
		src := Source{
			Covers:       []string{},
			Confirms:     true,
			Bindable:     h.deskNames == nil && !h.deskBusy,
			Binding:      h.deskBusy,
			Configurable: h.desk.CanConfigure(),
			Note:         h.desk.Note(),
			Notifies:     h.Notifier != nil,
		}
		if h.deskNames != nil {
			src.Names = map[string]string{}
			for _, action := range types.VoiceActions {
				if name := h.deskNames[action]; name != "" {
					src.Covers = append(src.Covers, action)
					src.Names[action] = name
				}
			}
		}
		return src
	}
	src := Source{Covers: []string{}, Problem: h.Problem, Note: h.Note, Notifies: h.Notifier != nil}
	for _, action := range types.VoiceActions {
		if _, ok := r.bound[action]; ok {
			src.Covers = append(src.Covers, action)
		}
	}
	return src
}

// sendSources tells every instance what the helper covers now.
func (h *Helper) sendSources() {
	type sourceFor struct {
		r   *remote
		src Source
	}
	h.mu.Lock()
	var out []sourceFor
	for r := range h.remotes {
		out = append(out, sourceFor{r, h.sourceLocked(r)})
	}
	h.mu.Unlock()
	for _, x := range out {
		if x.r.stream.Send(msgSource, x.src) != nil {
			_ = x.r.stream.Close()
		}
	}
}

// desktopRequest does what the member asked of the desktop.
func (h *Helper) desktopRequest(r *remote, do string) {
	h.mu.Lock()
	d, keys := h.desk, r.keys
	h.mu.Unlock()
	if d == nil {
		return
	}
	switch do {
	case DesktopBind:
		go h.bindDesktop(keys, true)
	case DesktopConfigure:
		if err := d.Configure(); err != nil {
			h.Logf("The desktop didn't show its settings for the keys: %v", err)
		}
	case DesktopStop:
		h.reopenDesktop(d)
		h.sendSources()
	}
}

// bindDesktop binds the actions with the desktop, preferring the member's
// keys, unless it has them already, its dialog is up, or the member turned
// it down this run and didn't ask again. A bind the member asked for that
// succeeds tells the instances, which then bind as the helper starts.
func (h *Helper) bindDesktop(keys map[string]types.VoiceKey, asked bool) {
	h.mu.Lock()
	d := h.desk
	if d == nil || h.deskBusy || h.deskNames != nil || (!asked && h.deskDeclined) {
		h.mu.Unlock()
		return
	}
	h.deskBusy = true
	h.mu.Unlock()
	h.sendSources()

	actions := make([]host.DesktopAction, 0, len(types.VoiceActions))
	for _, a := range types.VoiceActions {
		k := keys[a]
		actions = append(actions, host.DesktopAction{ID: a, Description: actionNames[a],
			Trigger: desktopTrigger(k)})
	}
	names, err := d.Bind(h.ctx, actions)
	h.mu.Lock()
	h.deskBusy = false
	current := h.desk == d
	if err == nil && current {
		h.deskNames, h.deskHeld, h.deskDeclined = names, map[string]bool{}, false
	} else if err != nil {
		h.deskDeclined = true
	}
	h.mu.Unlock()
	if err != nil {
		if errors.Is(err, host.ErrDesktopCancelled) {
			h.Logf("The member turned the desktop's dialog down.")
		} else if h.ctx.Err() == nil {
			h.Logf("The desktop didn't bind the keys: %v", err)
		}
		// A session binds once; another try needs another session.
		h.reopenDesktop(d)
	} else if asked && current {
		h.mu.Lock()
		var all []*remote
		for r := range h.remotes {
			all = append(all, r)
		}
		h.mu.Unlock()
		for _, r := range all {
			_ = r.stream.Send(msgBound, nil)
		}
	}
	h.sendSources()
}

// reopenDesktop replaces the desktop's session old, if it's still the
// helper's, with a new one, which binds nothing yet. With no old one, it
// opens the first.
func (h *Helper) reopenDesktop(old DesktopKeys) {
	h.mu.Lock()
	if h.desk != old {
		h.mu.Unlock()
		return
	}
	h.desk, h.deskNames, h.deskHeld = nil, nil, nil
	h.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	if h.ctx.Err() != nil {
		return
	}
	d, err := h.OpenDesktop()
	h.mu.Lock()
	if err == nil {
		h.desk = d
	} else if old == nil {
		h.Logf("No desktop owns the keys here: %v", err)
	} else {
		h.Logf("The desktop's session couldn't open again: %v", err)
	}
	h.mu.Unlock()
	select {
	case h.deskWake <- struct{}{}:
	default:
	}
}

// desktopLoop passes on what the desktop's session reports: presses,
// changed keys and its end.
func (h *Helper) desktopLoop(ctx context.Context) {
	for {
		h.mu.Lock()
		d := h.desk
		h.mu.Unlock()
		if d == nil {
			select {
			case <-ctx.Done():
				return
			case <-h.deskWake:
				continue
			}
		}
		select {
		case <-ctx.Done():
			_ = d.Close()
			return
		case <-h.deskWake:
		case p := <-d.Presses():
			h.desktopPress(d, p)
		case names := <-d.Changes():
			h.mu.Lock()
			if h.desk == d && h.deskNames != nil {
				h.deskNames = names
			}
			h.mu.Unlock()
			h.sendSources()
		case <-d.Closed():
			h.reopenDesktop(d)
			h.sendSources()
		}
	}
}

// desktopPress sends a press the desktop reported to every instance in a
// call: its keys are the desktop's, not an instance's. A press of a key
// already down is the desktop repeating it, and a release of one that
// isn't, late.
func (h *Helper) desktopPress(d DesktopKeys, p host.DesktopPress) {
	h.mu.Lock()
	if h.desk != d || h.deskNames == nil || h.deskNames[p.ID] == "" || p.Down == h.deskHeld[p.ID] {
		h.mu.Unlock()
		return
	}
	h.deskHeld[p.ID] = p.Down
	var targets []*remote
	for r := range h.remotes {
		if r.call {
			targets = append(targets, r)
		}
	}
	h.mu.Unlock()
	for _, r := range targets {
		if r.stream.Send(msgPress, Press{Action: p.ID, Down: p.Down}) != nil {
			_ = r.stream.Close()
		}
	}
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
