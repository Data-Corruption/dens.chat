package service_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/service"
	"github.com/Data-Corruption/dens.chat/internal/ui"
)

// flatpakHome gives the Flatpak's layout the directories Flatpak gives its
// sandbox, below the test's own, and a session bus that isn't there, so
// the service reaches no desktop's portals: none takes Dens's name on the
// developer's own bus, or has their desktop start it at sign-in.
func flatpakHome(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "app")
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "no-bus"))
}

func flatpakBuild(version string) build.BuildInfo {
	return build.BuildInfo{Name: "dens", Version: version, DefaultLogLevel: "warn"}
}

// runFlatpak opens the Flatpak's instance as bi, outside a sandbox, with no
// keyring, and runs its service until the returned stop.
func runFlatpak(t *testing.T, bi build.BuildInfo, port int) (*app.App, control.Client, func()) {
	t.Helper()
	a := app.New(bi)
	a.RetrieveSecret = func(context.Context, time.Duration) ([]byte, error) { return nil, errors.ErrUnsupported }
	form := layout.Flatpak
	if err := a.Open(app.OpenOptions{Instance: "main", DevClientPort: port, Form: &form}); err != nil {
		t.Fatal(err)
	}
	pages, err := ui.New()
	if err != nil {
		t.Fatal(err)
	}
	a.UI = pages
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx, a, func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-done:
		_ = a.Close()
		t.Fatalf("the Flatpak's service stopped before ready: %v", err)
	}
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	client := control.Client{Endpoint: a.Layout.ControlEndpoint, Server: host.ControlServer{Account: me}, Timeout: 10 * time.Second}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		if err := <-done; err != nil {
			t.Errorf("service.Run: %v", err)
		}
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}
	t.Cleanup(stop)
	return a, client, stop
}

// The Flatpak updated to a new build starts locked: the page gets its
// unlock screen and nothing else until the local password opens Dens, and
// the update, migrated after a snapshot, is done once it has. It hosts no
// den (M9.3).
func TestFlatpakServiceStartsLockedAfterAnUpdate(t *testing.T) {
	flatpakHome(t)
	port := freePort(t)
	a, client, stop := runFlatpak(t, flatpakBuild("v1.0.0"), port)
	b := pair(t, a, client)
	l := a.Layout
	b.expect(http.StatusConflict, "turn hosting on in the Flatpak", "POST", "/api/hosting",
		map[string]any{"on": true, "password": localPassword})
	stop()

	a, client, _ = runFlatpak(t, flatpakBuild("v1.1.0"), port)
	var status struct {
		Locked   bool `json:"locked"`
		Remember struct {
			On bool `json:"on"`
		} `json:"remember"`
	}
	b.expect(http.StatusOK, "the status while locked", "GET", "/api/status", nil, &status)
	if !status.Locked || status.Remember.On {
		t.Fatalf("the status while locked: %+v", status)
	}
	if state, err := maintenance.ReadState(l.State); err != nil || state.Phase != maintenance.PhaseUpdating {
		t.Fatalf("the update before the unlock: %+v, %v", state, err)
	}
	for _, path := range []string{"/api/settings", "/api/dens", "/api/hosting"} {
		b.expect(http.StatusLocked, path+" while locked", "GET", path, nil)
	}
	err := client.Call(control.Request{Op: control.OpBackup, Password: localPassword}, nil)
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("a backup while locked: %v", err)
	}
	var cs control.Status
	if err := client.Call(control.Request{Op: control.OpStatus}, &cs); err != nil || !cs.Locked {
		t.Fatalf("the control status while locked: %+v, %v", cs, err)
	}

	b.expect(http.StatusForbidden, "unlock with a wrong password", "POST", "/api/unlock", map[string]any{"password": "wrong wrong wrong"})
	b.expect(http.StatusOK, "unlock", "POST", "/api/unlock", map[string]any{"password": localPassword})
	b.expect(http.StatusOK, "the dens once unlocked", "GET", "/api/dens", nil)
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, err := maintenance.ReadState(l.State)
		if err == nil && state.Phase == maintenance.PhaseReady && state.Version == "v1.1.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the update after the unlock: %+v, %v", state, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	snapshots, err := os.ReadDir(l.Snapshot)
	if err != nil || len(snapshots) != 1 || !strings.Contains(snapshots[0].Name(), "v1.0.0") {
		t.Fatalf("the update's snapshot: %v, %v", snapshots, err)
	}

	// The shortcut helper runs inside the service once it's unlocked, over
	// a pipe to the hub; with no desktop to read the keys from, it says
	// why, once it has told the hub what it covers.
	events, stopWatching := a.Shortcuts.Watch()
	defer stopWatching()
	timeout := time.After(10 * time.Second)
	var last any
	for {
		select {
		case e := <-events:
			if e.Status != nil {
				last = e.Status
				if e.Status.Helper && e.Status.Problem != "" {
					return
				}
			}
		case <-timeout:
			t.Fatalf("the helper didn't tell the hub why it reads no keys; the hub's status: %+v", last)
		}
	}
}
