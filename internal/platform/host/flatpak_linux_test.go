package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeFlatpakPortals answers the desktop's Secret, Background and
// Notification portals and Flatpak's own, as the test sets them.
type fakeFlatpakPortals struct {
	conn *dbus.Conn

	mu sync.Mutex
	// secretUp is whether the Secret interface is there yet, secrets the
	// answers it gives in turn, and secretCode its response.
	secretUp   bool
	secrets    [][]byte
	secretCode uint32
	secretHold time.Duration
	asked      int

	backgroundOpts map[string]dbus.Variant
	status         string
	notified       []string

	spawned [][]string
	flags   uint32
	monitor dbus.ObjectPath
	// updateAnswer is the monitor's Update: a D-Bus error's name, or the
	// Progress statuses it sends.
	updateError  string
	updateStatus []uint32
	updateFailed string
}

func startFlatpakPortals(t *testing.T, conn *dbus.Conn) *fakeFlatpakPortals {
	t.Helper()
	f := &fakeFlatpakPortals{conn: conn, secretUp: true}
	for iface, v := range map[string]any{
		portalSecret:                      (*fakeSecret)(f),
		portalBackground:                  (*fakeBackground)(f),
		portalNotification:                (*fakeNotification)(f),
		"org.freedesktop.DBus.Properties": (*fakeFlatpakProperties)(f),
	} {
		if err := conn.Export(v, portalPath, iface); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Export((*fakeFlatpak)(f), flatpakPortalPath, flatpakPortal); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{portalName, flatpakPortalName} {
		if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("own %s: %v %v", name, reply, err)
		}
	}
	return f
}

func (f *fakeFlatpakPortals) respond(sender dbus.Sender, opts map[string]dbus.Variant, after time.Duration, code uint32, results map[string]dbus.Variant) dbus.ObjectPath {
	token, _ := opts["handle_token"].Value().(string)
	path := dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", portalPath, strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_"), token))
	go func() {
		time.Sleep(20*time.Millisecond + after)
		_ = f.conn.Emit(path, portalRequest+".Response", code, results)
	}()
	return path
}

type fakeFlatpakProperties fakeFlatpakPortals

func (f *fakeFlatpakProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	f.mu.Lock()
	up := f.secretUp
	f.mu.Unlock()
	switch {
	case iface == portalSecret && up, iface == portalBackground:
		return dbus.MakeVariant(uint32(2)), nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{"No such interface " + iface})
}

type fakeSecret fakeFlatpakPortals

func (f *fakeSecret) RetrieveSecret(sender dbus.Sender, fd dbus.UnixFD, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p := (*fakeFlatpakPortals)(f)
	p.mu.Lock()
	secret := []byte{}
	if len(p.secrets) > 0 {
		secret = p.secrets[p.asked%len(p.secrets)]
	}
	p.asked++
	code, hold := p.secretCode, p.secretHold
	p.mu.Unlock()
	pipe := os.NewFile(uintptr(fd), "secret")
	go func() {
		defer pipe.Close()
		time.Sleep(hold)
		if code == 0 {
			_, _ = pipe.Write(secret)
		}
	}()
	return p.respond(sender, opts, hold, code, map[string]dbus.Variant{}), nil
}

type fakeBackground fakeFlatpakPortals

func (f *fakeBackground) RequestBackground(sender dbus.Sender, parent string, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p := (*fakeFlatpakPortals)(f)
	p.mu.Lock()
	p.backgroundOpts = opts
	p.mu.Unlock()
	autostart, _ := opts["autostart"].Value().(bool)
	return p.respond(sender, opts, 0, 0, map[string]dbus.Variant{"background": dbus.MakeVariant(true), "autostart": dbus.MakeVariant(autostart)}), nil
}

func (f *fakeBackground) SetStatus(opts map[string]dbus.Variant) *dbus.Error {
	p := (*fakeFlatpakPortals)(f)
	p.mu.Lock()
	p.status, _ = opts["message"].Value().(string)
	p.mu.Unlock()
	return nil
}

type fakeNotification fakeFlatpakPortals

func (f *fakeNotification) AddNotification(id string, n map[string]dbus.Variant) *dbus.Error {
	p := (*fakeFlatpakPortals)(f)
	title, _ := n["title"].Value().(string)
	body, _ := n["body"].Value().(string)
	p.mu.Lock()
	p.notified = append(p.notified, id+": "+title+": "+body)
	p.mu.Unlock()
	return nil
}

type fakeFlatpak fakeFlatpakPortals

// Spawn records what it starts. godbus can't take a call that passes
// descriptors inside a map, as Spawn's do, so the fake is never given any;
// the Flatpak e2e passes them to Flatpak's own portal.
func (f *fakeFlatpak) Spawn(cwd []byte, argv [][]byte, fds map[uint32]dbus.UnixFDIndex, envs map[string]string, flags uint32, opts map[string]dbus.Variant) (uint32, *dbus.Error) {
	p := (*fakeFlatpakPortals)(f)
	args := []string{}
	for _, a := range argv {
		args = append(args, strings.TrimSuffix(string(a), "\x00"))
	}
	p.mu.Lock()
	p.spawned = append(p.spawned, args)
	p.flags = flags
	p.mu.Unlock()
	return 42, nil
}

func (f *fakeFlatpak) CreateUpdateMonitor(sender dbus.Sender, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p := (*fakeFlatpakPortals)(f)
	token, _ := opts["handle_token"].Value().(string)
	path := dbus.ObjectPath(fmt.Sprintf("%s/update_monitor/%s/%s", flatpakPortalPath, strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_"), token))
	if err := p.conn.Export((*fakeMonitor)(p), path, updateMonitor); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	p.mu.Lock()
	p.monitor = path
	p.mu.Unlock()
	return path, nil
}

type fakeMonitor fakeFlatpakPortals

func (m *fakeMonitor) Update(parent string, opts map[string]dbus.Variant) *dbus.Error {
	p := (*fakeFlatpakPortals)(m)
	p.mu.Lock()
	failure, statuses, message, path := p.updateError, p.updateStatus, p.updateFailed, p.monitor
	p.mu.Unlock()
	if failure != "" {
		return dbus.NewError(failure, []any{"refused"})
	}
	go func() {
		for _, status := range statuses {
			time.Sleep(20 * time.Millisecond)
			info := map[string]dbus.Variant{"status": dbus.MakeVariant(status)}
			if status == 3 {
				info["error"] = dbus.MakeVariant("org.freedesktop.portal.Error.NotAllowed")
				info["error_message"] = dbus.MakeVariant(message)
			}
			_ = p.conn.Emit(path, updateMonitor+".Progress", info)
		}
	}()
	return nil
}

func (m *fakeMonitor) Close() *dbus.Error { return nil }

// The Secret portal's answer comes through a pipe, once its interface is
// there, which at sign-in may be some seconds in; a prompt turned down is
// a cancel; and with no desktop portal on the bus there's no keyring at
// once, rather than after the wait (M9.3).
func TestRetrieveSecret(t *testing.T) {
	conn := sessionBus(t)
	ctx := context.Background()
	if _, err := RetrieveSecret(ctx, time.Minute); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("with no portal on the bus = %v", err)
	}

	f := startFlatpakPortals(t, conn)
	f.mu.Lock()
	f.secretUp, f.secrets = false, [][]byte{[]byte("the app's secret")}
	f.mu.Unlock()
	time.AfterFunc(600*time.Millisecond, func() {
		f.mu.Lock()
		f.secretUp = true
		f.mu.Unlock()
	})
	secret, err := RetrieveSecret(ctx, 10*time.Second)
	if err != nil || string(secret) != "the app's secret" {
		t.Fatalf("a secret once the interface came: %q, %v", secret, err)
	}

	f.mu.Lock()
	f.secretUp = false
	f.mu.Unlock()
	if _, err := RetrieveSecret(ctx, 300*time.Millisecond); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("an interface that never comes = %v", err)
	}

	f.mu.Lock()
	f.secretUp, f.secretCode = true, 1
	f.mu.Unlock()
	if _, err := RetrieveSecret(ctx, time.Second); !errors.Is(err, ErrDesktopCancelled) {
		t.Fatalf("a prompt turned down = %v", err)
	}

	// A prompt holds the request for as long as the member takes.
	f.mu.Lock()
	f.secretCode, f.secretHold = 0, time.Second
	f.mu.Unlock()
	if secret, err := RetrieveSecret(ctx, time.Second); err != nil || string(secret) != "the app's secret" {
		t.Fatalf("a held request: %q, %v", secret, err)
	}
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := RetrieveSecret(short, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a held request whose caller gave up = %v", err)
	}
}

// Starting at sign-in, the line under the app, notifications and starting
// a sandbox of the newest build each go through their portal (M9.3).
func TestBackgroundSpawnAndNotify(t *testing.T) {
	conn := sessionBus(t)
	f := startFlatpakPortals(t, conn)
	ctx := context.Background()

	background, autostart, err := RequestBackground(ctx, "To keep you signed in to your dens", true, []string{"dens", "service", "run"})
	if err != nil || !background || !autostart {
		t.Fatalf("RequestBackground: %v, %v, %v", background, autostart, err)
	}
	f.mu.Lock()
	commandline, _ := f.backgroundOpts["commandline"].Value().([]string)
	f.mu.Unlock()
	if strings.Join(commandline, " ") != "dens service run" {
		t.Fatalf("the autostart command line: %v", commandline)
	}
	if _, autostart, err := RequestBackground(ctx, "Stop", false, nil); err != nil || autostart {
		t.Fatalf("turning autostart off: %v, %v", autostart, err)
	}

	if err := SetBackgroundStatus("Connected to 2 dens"); err != nil {
		t.Fatal(err)
	}
	if err := Notify("start", "Dens didn't start", "Because."); err != nil {
		t.Fatal(err)
	}

	if err := SpawnLatest([]string{"dens", "service", "run"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status != "Connected to 2 dens" || len(f.notified) != 1 || !strings.Contains(f.notified[0], "Dens didn't start") {
		t.Fatalf("status %q, notified %v", f.status, f.notified)
	}
	if len(f.spawned) != 1 || strings.Join(f.spawned[0], " ") != "dens service run" || f.flags != spawnLatestVersion {
		t.Fatalf("spawned %v, flags %d", f.spawned, f.flags)
	}
}

// The update monitor reports what it finds, and installs an update: done,
// nothing to update, or refused, by the desktop or for a new permission
// (M9.3).
func TestUpdateMonitor(t *testing.T) {
	conn := sessionBus(t)
	f := startFlatpakPortals(t, conn)
	m, err := OpenUpdateMonitor()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	f.mu.Lock()
	monitor := f.monitor
	f.mu.Unlock()
	_ = conn.Emit(monitor, updateMonitor+".UpdateAvailable", map[string]dbus.Variant{
		"running-commit": dbus.MakeVariant("aaa"), "local-commit": dbus.MakeVariant("aaa"), "remote-commit": dbus.MakeVariant("bbb"),
	})
	select {
	case u := <-m.Updates():
		if u != (FlatpakUpdate{Running: "aaa", Local: "aaa", Remote: "bbb"}) {
			t.Fatalf("update %+v", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no update reported")
	}

	ctx := context.Background()
	for _, c := range []struct {
		failure  string
		statuses []uint32
		want     error
	}{
		{"", []uint32{0, 0, 2}, nil},
		{"", []uint32{1}, ErrNothingToUpdate},
		{"", []uint32{0, 3}, ErrUpdateRefused},
		{"org.freedesktop.DBus.Error.AccessDenied", nil, ErrUpdateRefused},
	} {
		f.mu.Lock()
		f.updateError, f.updateStatus, f.updateFailed = c.failure, c.statuses, "Self update not supported, new version requires new permissions"
		f.mu.Unlock()
		if err := m.Update(ctx); !errors.Is(err, c.want) || c.want == nil && err != nil {
			t.Errorf("Update with %q %v = %v, want %v", c.failure, c.statuses, err, c.want)
		}
	}
}

// The app's name on the session bus takes Background Apps' quit, and only
// one process holds it (M9.3).
func TestAppNameQuits(t *testing.T) {
	conn := sessionBus(t)
	quit := make(chan struct{}, 1)
	name, err := OwnAppName("chat.dens.Test", func() { quit <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer name.Close()
	if _, err := OwnAppName("chat.dens.Test", func() {}); err == nil {
		t.Fatal("a second process took the app's name")
	}
	app := conn.Object("chat.dens.Test", "/chat/dens/Test")
	if err := app.Call("org.freedesktop.Application.ActivateAction", 0, "quit", []dbus.Variant{}, map[string]dbus.Variant{}).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	case <-time.After(5 * time.Second):
		t.Fatal("the quit action didn't quit")
	}
	if err := app.Call("org.freedesktop.Application.ActivateAction", 0, "nap", []dbus.Variant{}, map[string]dbus.Variant{}).Err; err == nil {
		t.Fatal("an action the app hasn't was taken")
	}
}

// From a sandbox the shortcuts portal is tried under X11 too, and the app
// doesn't register, which a sandboxed one can't (M9.3).
func TestDesktopKeysFromASandbox(t *testing.T) {
	conn := sessionBus(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	p := startPortal(t, conn, 2)
	d, err := OpenDesktopKeys("chat.dens.Dens", true)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.registered != "" {
		t.Fatalf("a sandboxed app registered as %q", p.registered)
	}
}

// A descriptor is taken for a launch's pipe only if it's a pipe: one that
// nothing passed may be the process's own (M9.3).
func TestInheritedPipeIsOnlyAPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd, err := syscall.Dup(int(w.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	pipe := InheritedPipe(fd)
	if pipe == nil {
		t.Fatal("a pipe wasn't taken")
	}
	pipe.Close()
	file, err := os.CreateTemp(t.TempDir(), "file")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, fd := range []int{int(file.Fd()), 2, 9999} {
		if InheritedPipe(fd) != nil {
			t.Errorf("descriptor %d was taken for a pipe", fd)
		}
	}
}
