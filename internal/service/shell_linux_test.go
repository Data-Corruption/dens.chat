package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

// androidHome gives the Android app's form the files directory its shell
// names, with the key the shell wrapped in its control directory, and a
// data key where this platform's LoadHostKey finds one: on Linux, as
// systemd gives an installed service its key, standing in for the shell's
// pipe (M12).
func androidHome(t *testing.T, key []byte) layout.Layout {
	t.Helper()
	files := filepath.Join(t.TempDir(), "files")
	t.Setenv("XDG_DATA_HOME", files)
	l, err := layout.New("dens", "main", layout.Android)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{files, filepath.Dir(l.Root), l.Root, l.Control} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(l.HostKey, []byte("wrapped by the shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	setAndroidKey(t, key)
	return l
}

func setAndroidKey(t *testing.T, key []byte) {
	t.Helper()
	credentials := t.TempDir()
	if err := os.WriteFile(filepath.Join(credentials, host.HostKeyCredential), key, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", credentials)
}

func androidBuild(version string) build.BuildInfo {
	return build.BuildInfo{Name: "dens", Version: version, DefaultLogLevel: "warn"}
}

// runAndroid opens the Android app's instance as bi and runs its service
// until the returned stop, or returns why it didn't start.
func runAndroid(t *testing.T, bi build.BuildInfo, port int) (*app.App, control.Client, func(), error) {
	t.Helper()
	a := app.New(bi)
	form := layout.Android
	if err := a.Open(app.OpenOptions{Instance: "main", DevClientPort: port, Form: &form}); err != nil {
		_ = a.Close()
		return nil, control.Client{}, nil, err
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
		cancel()
		_ = a.Close()
		return nil, control.Client{}, nil, err
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
	return a, client, stop, nil
}

// The Android app's service starts from the key its shell hands it, greets
// the shell's stream with where the page is, and hosts no den. A key that
// isn't the data's, and data a newer version wrote, are refused (M12).
func TestAndroidServiceStartsFromTheShellsKey(t *testing.T) {
	key := bytes.Repeat([]byte{3}, host.HostKeySize)
	l := androidHome(t, key)
	port := freePort(t)
	a, client, stop, err := runAndroid(t, androidBuild("v1.5.0"), port)
	if err != nil {
		t.Fatalf("the service didn't start: %v", err)
	}
	if a.Layout.Root != l.Root || a.Instance.ClientPort != port {
		t.Fatalf("root %s on port %d, want %s on %d", a.Layout.Root, a.Instance.ClientPort, l.Root, port)
	}

	stream, err := client.Open(control.Request{Op: control.OpShell})
	if err != nil {
		t.Fatalf("open the shell's stream: %v", err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	var hello struct{ Version, Page string }
	if err := json.Unmarshal(msg.D, &hello); err != nil || msg.T != "hello" {
		t.Fatalf("first line %s %s, %v; want hello", msg.T, msg.D, err)
	}
	if want := "http://localhost:" + strconv.Itoa(port) + "/"; hello.Version != "v1.5.0" || hello.Page != want {
		t.Fatalf("hello %+v, want v1.5.0 and %s", hello, want)
	}
	if err := stream.Send(control.MessagePing, nil); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()

	if err := a.Hosting.Set(context.Background(), true); !errors.Is(err, app.ErrCantHost) {
		t.Fatalf("turning hosting on = %v, want refused", err)
	}
	// The start publishes ready once it has its key, just after it answers.
	var state maintenance.State
	for range 100 {
		if state, err = maintenance.ReadState(l.State); err == nil && state.Phase == maintenance.PhaseReady {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || state.Phase != maintenance.PhaseReady || state.Version != "v1.5.0" {
		t.Fatalf("lifecycle state %+v, %v; want ready at v1.5.0", state, err)
	}
	stop()

	setAndroidKey(t, bytes.Repeat([]byte{4}, host.HostKeySize))
	if _, _, _, err := runAndroid(t, androidBuild("v1.5.0"), port); err == nil || !strings.Contains(err.Error(), "another installation") {
		t.Fatalf("a start with another key = %v, want refused", err)
	}
	setAndroidKey(t, key)
	_, _, _, err = runAndroid(t, androidBuild("v1.4.0"), port)
	if rolled := (*maintenance.RolledBackError)(nil); !errors.As(err, &rolled) {
		t.Fatalf("an older build over newer data = %v, want refused", err)
	}
}
