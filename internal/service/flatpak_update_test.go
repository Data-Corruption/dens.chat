package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// fakeMonitor is Flatpak's update monitor as the test sets it.
type fakeMonitor struct {
	updates chan host.FlatpakUpdate
	mu      sync.Mutex
	answer  error
}

func (m *fakeMonitor) Updates() <-chan host.FlatpakUpdate { return m.updates }
func (m *fakeMonitor) Close() error                       { return nil }
func (m *fakeMonitor) Update(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.answer
}

// The Flatpak's update notice follows Flatpak's update monitor (M9.3): it
// offers Update where Dens may install the update, which GNOME never lets
// an app with no window do, and the software center where Flatpak refused
// it, until the remote has another; once one is installed, by anything,
// it offers Restart, which starts the newest build; and an update that
// had nothing to install, since something else had, restarts too.
func TestFlatpakUpdateNotice(t *testing.T) {
	a := app.New(build.BuildInfo{Name: "dens", Version: "v1.1.0"})
	var err error
	if a.Log, err = xlog.New(t.TempDir(), "error"); err != nil {
		t.Fatal(err)
	}
	defer a.Log.Close()
	a.Layout.Instance = "second"
	m := &fakeMonitor{updates: make(chan host.FlatpakUpdate, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newFlatpak(ctx, a, func(string) {})
	f.gnome = false
	f.openMonitor = func() (updateMonitor, error) { return m, nil }
	var spawned []string
	f.spawn = func(argv []string, _ ...*os.File) error {
		spawned = argv
		return nil
	}
	f.start(f.watchUpdates)
	notice := func(u host.FlatpakUpdate) app.FlatpakUpdate {
		t.Helper()
		m.updates <- u
		deadline := time.Now().Add(5 * time.Second)
		for {
			f.mu.Lock()
			got := f.found == u
			f.mu.Unlock()
			if got {
				return f.Update()
			}
			if time.Now().After(deadline) {
				t.Fatalf("the monitor's %+v didn't arrive", u)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	if u := f.Update(); u.Available || u.Installed {
		t.Fatalf("before the monitor finds anything: %+v", u)
	}
	if u := notice(host.FlatpakUpdate{Running: "a", Local: "a", Remote: "b"}); !u.Available || u.Installed || !u.CanInstall {
		t.Fatalf("an update on the remote: %+v", u)
	}
	f.gnome = true
	if u := f.Update(); u.CanInstall {
		t.Fatalf("on GNOME: %+v", u)
	}
	f.gnome = false

	m.mu.Lock()
	m.answer = fmt.Errorf("%w: Self update not supported, new version requires new permissions", host.ErrUpdateRefused)
	m.mu.Unlock()
	if err := f.InstallUpdate(ctx); !errors.Is(err, host.ErrUpdateRefused) {
		t.Fatalf("a refused update = %v", err)
	}
	if u := f.Update(); u.CanInstall || !strings.Contains(u.Refused, "new permissions") {
		t.Fatalf("after Flatpak refused it: %+v", u)
	}
	if u := notice(host.FlatpakUpdate{Running: "a", Local: "a", Remote: "c"}); !u.CanInstall || u.Refused != "" {
		t.Fatalf("another update after a refused one: %+v", u)
	}

	m.mu.Lock()
	m.answer = host.ErrNothingToUpdate
	m.mu.Unlock()
	if err := f.InstallUpdate(ctx); err != nil {
		t.Fatalf("an update something else installed first = %v", err)
	}
	if u := notice(host.FlatpakUpdate{Running: "a", Local: "c", Remote: "c"}); u.Available || !u.Installed {
		t.Fatalf("an update installed: %+v", u)
	}
	if err := f.StartNewest(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(spawned, " ") != "dens --instance second service run" {
		t.Fatalf("restarted with %v", spawned)
	}
}
