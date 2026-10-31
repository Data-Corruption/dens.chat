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
	"github.com/Data-Corruption/dens.chat/internal/notify"
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

// next is the next line on a stream but a ping.
func next(t *testing.T, s *control.Stream) control.Message {
	t.Helper()
	for {
		msg, err := s.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if msg.T != control.MessagePing {
			return msg
		}
	}
}

// The shell's stream carries the notifications the shell posts (M12.3):
// whether the member turned any on, each one to post and each to take
// back, and the shell's word on the app being in front and on Android
// letting it post them.
func TestTheShellPostsNotifications(t *testing.T) {
	androidHome(t, bytes.Repeat([]byte{3}, host.HostKeySize))
	a, client, _, err := runAndroid(t, androidBuild("v1.5.0"), freePort(t))
	if err != nil {
		t.Fatalf("the service didn't start: %v", err)
	}
	stream, err := client.Open(control.Request{Op: control.OpShell})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if msg := next(t, stream); msg.T != "hello" {
		t.Fatalf("first line %s, want hello", msg.T)
	}
	if msg := next(t, stream); msg.T != "notifying" || string(msg.D) != `{"on":false}` {
		t.Fatalf("second line %s %s, want notifying off", msg.T, msg.D)
	}
	if err := stream.Send("notifies", map[string]bool{"on": true}); err != nil {
		t.Fatal(err)
	}
	if err := a.Notify.SetKinds(new(true), nil, nil); err != nil {
		t.Fatal(err)
	}
	if msg := next(t, stream); msg.T != "notifying" || string(msg.D) != `{"on":true}` {
		t.Fatalf("after DMs turned on: %s %s, want notifying on", msg.T, msg.D)
	}
	for range 100 {
		if st := a.Notify.Status(); st.Shell != nil && st.Shell.Allowed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := a.Notify.Status(); st.Shell == nil || !st.Shell.Allowed {
		t.Fatalf("the status says %+v of the shell, which said Android lets it post", st.Shell)
	}

	// With the app behind, a DM posts through the shell.
	if err := stream.Send("front", map[string]bool{"on": false}); err != nil {
		t.Fatal(err)
	}
	a.Notify.Message(notify.Message{Den: "d1", DenName: "Den", Channel: "7", DM: true, ID: "10", Author: "Alice", Text: "hi"})
	msg := next(t, stream)
	var n notify.Notification
	if err := json.Unmarshal(msg.D, &n); err != nil || msg.T != "notify" || n.Key != "d1/7" || n.Title != "Alice" || n.Text != "hi" || n.Path != "/den/d1/7#message=10" {
		t.Fatalf("got %s %s, want the DM's notification", msg.T, msg.D)
	}
	a.Notify.Cleared("d1", "7", "10")
	if msg := next(t, stream); msg.T != "unnotify" || string(msg.D) != `{"keys":["d1/7"]}` {
		t.Fatalf("after the DM was read: %s %s, want it taken back", msg.T, msg.D)
	}
}

// The phone's name and its model, which the shell gives Dens, name its
// device on the dens it joins, where its hostname would say only
// localhost (M13.3).
func TestThePhonesNameAndModelAreItsDevicesLabel(t *testing.T) {
	androidHome(t, bytes.Repeat([]byte{3}, host.HostKeySize))
	t.Setenv(app.DeviceNameEnv, "Galaxy S24 Ultra")
	t.Setenv(app.DeviceModelEnv, "SM-S928U1")
	a, _, _, err := runAndroid(t, androidBuild("v1.6.0"), freePort(t))
	if err != nil {
		t.Fatalf("the service didn't start: %v", err)
	}
	if a.Dens.Label != "Galaxy S24 Ultra (SM-S928U1)" {
		t.Fatalf("the device's label is %q, want the phone's name and model", a.Dens.Label)
	}
}

// The shell paints its bars with the page's background, which the page
// says over its stream as its theme changes, and the shell hears over its
// own: at each change, in the order the page said them, and as it
// connects, where the page has said one. A color that isn't one goes
// nowhere (M13.3).
func TestTheShellPaintsTheBarsWithThePagesTheme(t *testing.T) {
	androidHome(t, bytes.Repeat([]byte{3}, host.HostKeySize))
	a, client, _, err := runAndroid(t, androidBuild("v1.6.0"), freePort(t))
	if err != nil {
		t.Fatalf("the service didn't start: %v", err)
	}
	page := pair(t, a, client).stream()
	page.next("dens")
	shell := func() *control.Stream {
		s, err := client.Open(control.Request{Op: control.OpShell})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"hello", "notifying"} {
			if msg := next(t, s); msg.T != want {
				t.Fatalf("got %s, want %s", msg.T, want)
			}
		}
		return s
	}
	bars := func(s *control.Stream, want string) {
		t.Helper()
		if msg := next(t, s); msg.T != "bars" || string(msg.D) != `{"color":"`+want+`"}` {
			t.Fatalf("got %s %s, want bars %s", msg.T, msg.D, want)
		}
	}

	first := shell()
	page.send("bars", map[string]string{"color": "#1D232A"})
	bars(first, "#1d232a")
	_ = first.Close()

	again := shell()
	defer again.Close()
	bars(again, "#1d232a")
	for _, color := range []string{"#FFFFFF", "#ffffff", "red", "#fff", "#12345g", "", "#00FF7F"} {
		page.send("bars", map[string]string{"color": color})
	}
	bars(again, "#ffffff")
	bars(again, "#00ff7f")
	if err := a.SetBars("blue"); !errors.Is(err, app.ErrBarsColor) {
		t.Fatalf("a color that isn't one: %v", err)
	}
	if err := a.SetBars("#123456"); err != nil {
		t.Fatal(err)
	}
	bars(again, "#123456")
}
