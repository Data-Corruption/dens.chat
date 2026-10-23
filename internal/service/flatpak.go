package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
	"github.com/Data-Corruption/dens.chat/internal/types"

	"golang.org/x/mod/semver"
)

// flatpak is what the Flatpak's service does in the member's session
// (M9.3), the App's Flatpak. It holds Dens's name on the session bus, whose
// quit action GNOME's Background Apps sends; watches Flatpak's update
// monitor for the page's notice, and installs an update where Flatpak lets
// it; starts the newest build installed, as a sandbox of its own, to
// restart into it; runs the shortcut helper inside the service, since it's
// in the session already; says how many dens it's connected to under its
// name among the desktop's background apps; and has the desktop start it at
// sign-in once its key is remembered.
type flatpak struct {
	a    *app.App
	ctx  context.Context
	quit func(why string)
	// gnome is a GNOME session, which won't ask the member to let an app
	// with no window update itself.
	gnome bool
	// openMonitor, spawn and output are Flatpak's portal's, or a test's.
	openMonitor func() (updateMonitor, error)
	spawn       func(argv []string, files ...*os.File) error
	output      func(ctx context.Context, argv []string) ([]byte, error)

	// wg is what run waits for as it ends; once closed, nothing more
	// starts.
	wg      sync.WaitGroup
	mu      sync.Mutex
	closed  bool
	monitor updateMonitor
	found   host.FlatpakUpdate
	// refused is why Flatpak wouldn't install the update from Dens, for the
	// remote's commit refusedFor.
	refused, refusedFor string
	// manual is a desktop with no Background portal, where the member has
	// Dens start at sign-in by hand.
	manual bool
	// checked is when CheckInstalled last asked, and newest what the
	// newest build installed answered.
	checked time.Time
	newest  string
}

// updateMonitor is Flatpak's update monitor, host.UpdateMonitor, or a
// test's.
type updateMonitor interface {
	Updates() <-chan host.FlatpakUpdate
	Update(ctx context.Context) error
	Close() error
}

func newFlatpak(ctx context.Context, a *app.App, quit func(why string)) *flatpak {
	return &flatpak{
		a: a, ctx: ctx, quit: quit,
		gnome:       strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "GNOME"),
		openMonitor: func() (updateMonitor, error) { return host.OpenUpdateMonitor() },
		spawn:       host.SpawnLatest,
		output:      host.SpawnLatestOutput,
	}
}

// start runs f until the service stops, unless it's stopping already.
func (f *flatpak) start(do func(ctx context.Context)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		do(f.ctx)
	}()
}

// run does the Flatpak's part until the service stops: what needs no key
// at once, and the rest once the instance has its key.
func (f *flatpak) run() {
	a, ctx := f.a, f.ctx
	defer func() {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
		f.wg.Wait()
	}()
	name, err := host.OwnAppName(layout.DesktopID, func() { f.quit("asked to by the desktop") })
	if err != nil {
		a.Log.Warnf("The desktop can't quit Dens from its background apps: %v", err)
	} else {
		defer name.Close()
	}
	f.start(f.watchUpdates)
	if a.Locked() {
		f.status("Locked: open Dens to unlock it")
	}
	select {
	case <-a.Unlocked():
	case <-ctx.Done():
		return
	}
	f.resync()
	f.start(f.showConnected)
	f.start(f.runHelper)
	<-ctx.Done()
}

// resync has the desktop start Dens at sign-in, or not, as wanted now: as
// the instance unlocks, and as its remembered key comes or goes.
func (f *flatpak) resync() { f.start(f.syncStartAtSignIn) }

// status is the line under Dens among the desktop's background apps, which
// needs version 2 of the Background portal.
func (f *flatpak) status(message string) {
	if err := host.SetBackgroundStatus(message); err != nil {
		f.a.Log.Debugf("The desktop shows no line under Dens: %v", err)
	}
}

// showConnected keeps the line under Dens saying how many dens it's
// connected to.
func (f *flatpak) showConnected(ctx context.Context) {
	changed, stop := f.a.Dens.Watch()
	defer stop()
	last := ""
	for {
		n := 0
		for _, s := range f.a.Dens.Statuses() {
			if s.State == denclient.StateConnected {
				n++
			}
		}
		line := fmt.Sprintf("Connected to %d dens", n)
		if n == 1 {
			line = "Connected to 1 den"
		}
		if line != last {
			f.status(line)
			last = line
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
		// A den's state can change several times as it connects.
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return
		}
	}
}

// runHelper runs the shortcut helper inside the service, its stream a pipe
// to the hub, and its keys the desktop's shortcuts portal, which knows the
// sandboxed app by its Flatpak ID in every session, or X11's.
func (f *flatpak) runHelper(ctx context.Context) {
	a := f.a
	h := &shortcuts.Helper{
		Version:   a.BuildInfo().Version,
		Instances: func() ([]string, error) { return []string{a.Layout.Instance}, nil },
		Open: func(string) (*control.Stream, error) {
			mine, hubs := net.Pipe()
			go func() { _ = a.Shortcuts.Serve(ctx, control.NewStream(hubs)) }()
			return control.NewStream(mine), nil
		},
		Logf: func(format string, args ...any) { a.Log.Infof("Voice keys: "+format, args...) },
	}
	defer h.UseSystem(true)()
	if err := h.Run(ctx); err != nil && ctx.Err() == nil {
		a.Log.Warnf("The voice keys' helper stopped: %v", err)
	}
}

// watchUpdates follows Flatpak's update monitor, which checks the app's
// remote every 30 minutes.
func (f *flatpak) watchUpdates(ctx context.Context) {
	m, err := f.openMonitor()
	if err != nil {
		f.a.Log.Infof("Flatpak's update monitor isn't there: %v", err)
		return
	}
	defer m.Close()
	f.mu.Lock()
	f.monitor = m
	f.mu.Unlock()
	for {
		select {
		case u := <-m.Updates():
			f.mu.Lock()
			f.found = u
			f.mu.Unlock()
			f.a.Log.Infof("Flatpak's update monitor: running %.12s, installed %.12s, on the remote %.12s", u.Running, u.Local, u.Remote)
		case <-ctx.Done():
			return
		}
	}
}

func (f *flatpak) Update() app.FlatpakUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.found
	var v app.FlatpakUpdate
	v.Available = u.Remote != "" && u.Remote != u.Local
	v.Installed = u.Local != "" && u.Local != u.Running
	if semver.Compare(f.newest, f.a.BuildInfo().Version) > 0 {
		v.Installed, v.Version = true, f.newest
	}
	if v.Available && f.refusedFor == u.Remote {
		v.Refused = f.refused
	}
	v.CanInstall = v.Available && !f.gnome && v.Refused == "" && f.monitor != nil
	return v
}

// checkEvery is how often CheckInstalled asks at most, which the page has
// it do as the member comes back to it, and each minute while they stay.
const checkEvery = 30 * time.Second

// CheckInstalled asks the newest build installed its version, which it
// prints, from a sandbox of its own that ends at once. Flatpak's update
// monitor learns of a build something else installed, as the software
// center does, only at its next check, every 30 minutes; GNOME Software
// installs updates by itself.
func (f *flatpak) CheckInstalled(ctx context.Context) {
	f.mu.Lock()
	if time.Since(f.checked) < checkEvery {
		f.mu.Unlock()
		return
	}
	f.checked = time.Now()
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := f.output(ctx, []string{f.a.BuildInfo().Name, "--build-vars"})
	if err != nil {
		f.a.Log.Infof("Asking the newest build of Dens installed its version: %v", err)
		return
	}
	var bi build.BuildInfo
	if err := json.Unmarshal(out, &bi); err != nil || !semver.IsValid(bi.Version) {
		f.a.Log.Infof("The newest build of Dens installed answered no version: %q", out)
		return
	}
	f.mu.Lock()
	f.newest = bi.Version
	f.mu.Unlock()
}

func (f *flatpak) InstallUpdate(ctx context.Context) error {
	f.mu.Lock()
	m, remote := f.monitor, f.found.Remote
	f.mu.Unlock()
	if m == nil {
		return errors.New("Flatpak's update monitor isn't running")
	}
	err := m.Update(ctx)
	switch {
	case err == nil:
		f.a.Log.Info("Flatpak installed the update")
		return nil
	case errors.Is(err, host.ErrNothingToUpdate):
		// The monitor learns of an update something else installed only at
		// its next check.
		f.a.Log.Info("Flatpak had nothing to update: the newest build is installed already")
		return nil
	case errors.Is(err, host.ErrUpdateRefused):
		f.mu.Lock()
		f.refused, f.refusedFor = err.Error(), remote
		f.mu.Unlock()
		f.a.Log.Infof("Flatpak won't install the update from Dens: %v", err)
	}
	return err
}

func (f *flatpak) StartNewest() error {
	argv := []string{f.a.BuildInfo().Name}
	if f.a.Layout.Instance != layout.DefaultInstance {
		argv = append(argv, "--instance", f.a.Layout.Instance)
	}
	if err := f.spawn(append(argv, "service", "run")); err != nil {
		return fmt.Errorf("start the newest build of Dens: %w", err)
	}
	f.a.Log.Info("Started the newest build installed, which takes over once this one stops")
	return nil
}

func (f *flatpak) Quit(why string) { f.quit(why) }

// startAtSignIn is the command the desktop runs at sign-in.
func (f *flatpak) startAtSignIn() []string {
	argv := []string{f.a.BuildInfo().Name}
	if f.a.Layout.Instance != layout.DefaultInstance {
		argv = append(argv, "--instance", f.a.Layout.Instance)
	}
	return append(argv, "service", "run")
}

func (f *flatpak) StartAtSignIn() app.StartAtSignIn {
	on := f.wantsStartAtSignIn()
	f.mu.Lock()
	manual := f.manual
	f.mu.Unlock()
	s := app.StartAtSignIn{On: on}
	if on && manual {
		s.Manual = "flatpak run --command=" + strings.Join(append([]string{f.startAtSignIn()[0], layout.DesktopID}, f.startAtSignIn()[1:]...), " ")
	}
	return s
}

// wantsStartAtSignIn is whether Dens should start at sign-in: once its key
// is remembered, as a start that waits locked reaches no den, and unless
// the member turned it off.
func (f *flatpak) wantsStartAtSignIn() bool {
	if !f.a.Remembering().On {
		return false
	}
	cfg, err := config.View(f.a.DB)
	return err == nil && (cfg.StartAtSignIn == nil || *cfg.StartAtSignIn)
}

func (f *flatpak) SetStartAtSignIn(ctx context.Context, on bool) error {
	if _, err := config.Update(f.a.DB, func(c *types.Configuration) error {
		c.StartAtSignIn = &on
		return nil
	}); err != nil {
		return err
	}
	f.syncStartAtSignIn(ctx)
	return nil
}

// syncStartAtSignIn asks the desktop's Background portal to start Dens at
// sign-in, or not to, as wanted now. No desktop asks the member about it.
func (f *flatpak) syncStartAtSignIn(ctx context.Context) {
	want := f.wantsStartAtSignIn()
	_, autostart, err := host.RequestBackground(ctx, "Dens keeps you signed in to your dens.", want, f.startAtSignIn())
	f.mu.Lock()
	f.manual = errors.Is(err, errors.ErrUnsupported)
	f.mu.Unlock()
	switch {
	case err != nil && want && ctx.Err() == nil:
		f.a.Log.Infof("The desktop won't start Dens at sign-in: %v", err)
	case err != nil:
		f.a.Log.Debugf("The desktop's Background portal: %v", err)
	case err == nil && autostart != want:
		f.a.Log.Infof("The desktop answered starting at sign-in %v, asked %v", autostart, want)
	case err == nil:
		f.a.Log.Debugf("Starting at sign-in: %v", want)
	}
}
