package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// sessionBus runs a private session bus for the test, and points the
// portal client at it.
func sessionBus(t *testing.T) *dbus.Conn {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("no dbus-daemon to run a session bus on")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address", "--address=unix:path="+filepath.Join(t.TempDir(), "bus"))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	address, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("read the bus's address: %v", err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(address))
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// fakePortal answers the portal's calls as a desktop would, with the
// answers the test sets.
type fakePortal struct {
	conn    *dbus.Conn
	version uint32

	mu         sync.Mutex
	registered string
	session    dbus.ObjectPath
	bindAnswer uint32
	configured bool
}

type fakeShortcut struct {
	ID   string
	Opts map[string]dbus.Variant
}

func startPortal(t *testing.T, conn *dbus.Conn, version uint32) *fakePortal {
	t.Helper()
	p := &fakePortal{conn: conn, version: version}
	for iface, v := range map[string]any{
		portalShortcuts:                   (*fakeShortcuts)(p),
		portalRegistry:                    (*fakeRegistry)(p),
		"org.freedesktop.DBus.Properties": (*fakeProperties)(p),
	} {
		if err := conn.Export(v, portalPath, iface); err != nil {
			t.Fatal(err)
		}
	}
	if reply, err := conn.RequestName(portalName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own the portal's name: %v %v", reply, err)
	}
	return p
}

func (p *fakePortal) respond(sender dbus.Sender, opts map[string]dbus.Variant, code uint32, results map[string]dbus.Variant) dbus.ObjectPath {
	token, _ := opts["handle_token"].Value().(string)
	path := dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", portalPath, strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_"), token))
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = p.conn.Emit(path, portalRequest+".Response", code, results)
	}()
	return path
}

type fakeShortcuts fakePortal

func (f *fakeShortcuts) CreateSession(sender dbus.Sender, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p := (*fakePortal)(f)
	token, _ := opts["session_handle_token"].Value().(string)
	p.mu.Lock()
	p.session = dbus.ObjectPath(fmt.Sprintf("%s/session/%s/%s", portalPath, strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_"), token))
	session := p.session
	p.mu.Unlock()
	return p.respond(sender, opts, 0, map[string]dbus.Variant{"session_handle": dbus.MakeVariant(string(session))}), nil
}

func (f *fakeShortcuts) BindShortcuts(sender dbus.Sender, session dbus.ObjectPath, shortcuts []fakeShortcut, parent string, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p := (*fakePortal)(f)
	p.mu.Lock()
	code := p.bindAnswer
	p.mu.Unlock()
	var bound []fakeShortcut
	for _, s := range shortcuts {
		trigger, _ := s.Opts["preferred_trigger"].Value().(string)
		bound = append(bound, fakeShortcut{ID: s.ID, Opts: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Press " + trigger)}})
	}
	return p.respond(sender, opts, code, map[string]dbus.Variant{"shortcuts": dbus.MakeVariant(bound)}), nil
}

func (f *fakeShortcuts) ConfigureShortcuts(session dbus.ObjectPath, parent string, opts map[string]dbus.Variant) *dbus.Error {
	p := (*fakePortal)(f)
	p.mu.Lock()
	p.configured = true
	p.mu.Unlock()
	return nil
}

type fakeRegistry fakePortal

func (f *fakeRegistry) Register(appID string, opts map[string]dbus.Variant) *dbus.Error {
	p := (*fakePortal)(f)
	p.mu.Lock()
	p.registered = appID
	p.mu.Unlock()
	return nil
}

type fakeProperties fakePortal

func (f *fakeProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface == portalShortcuts && name == "version" {
		return dbus.MakeVariant(f.version), nil
	}
	return dbus.Variant{}, dbus.MakeFailedError(errors.New("no such property"))
}

func (p *fakePortal) emit(member string, args ...any) {
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	_ = p.conn.Emit(portalPath, portalShortcuts+"."+member, append([]any{session}, args...)...)
}

func TestDesktopKeysBindAndReportPresses(t *testing.T) {
	conn := sessionBus(t)
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	p := startPortal(t, conn, 2)
	d, err := OpenDesktopKeys("chat.dens.Dens")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p.mu.Lock()
	registered := p.registered
	p.mu.Unlock()
	if registered != "chat.dens.Dens" {
		t.Fatalf("registered %q", registered)
	}
	if !d.CanConfigure() || d.Note() != "" {
		t.Fatalf("a version 2 portal: configure %v, note %q", d.CanConfigure(), d.Note())
	}

	names, err := d.Bind(context.Background(), []DesktopAction{{ID: "ptt", Description: "Push to talk", Trigger: "F8"}, {ID: "toggleMute", Description: "Toggle mute"}})
	if err != nil {
		t.Fatal(err)
	}
	if names["ptt"] != "Press F8" || names["toggleMute"] != "Press " {
		t.Fatalf("names %v", names)
	}

	p.emit("Activated", "ptt", uint64(1), map[string]dbus.Variant{})
	p.emit("Deactivated", "ptt", uint64(2), map[string]dbus.Variant{})
	for _, want := range []DesktopPress{{ID: "ptt", Down: true}, {ID: "ptt", Down: false}} {
		select {
		case got := <-d.Presses():
			if got != want {
				t.Fatalf("press %+v, want %+v", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no press %+v", want)
		}
	}
	// Another session's presses aren't this one's.
	_ = conn.Emit(portalPath, portalShortcuts+".Activated", dbus.ObjectPath("/org/freedesktop/portal/desktop/session/1_9/other"), "ptt", uint64(3), map[string]dbus.Variant{})

	p.emit("ShortcutsChanged", []fakeShortcut{{ID: "ptt", Opts: map[string]dbus.Variant{"trigger_description": dbus.MakeVariant("Press F9")}}})
	select {
	case names := <-d.Changes():
		if names["ptt"] != "Press F9" {
			t.Fatalf("changed to %v", names)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change")
	}
	select {
	case got := <-d.Presses():
		t.Fatalf("another session's press reached this one: %+v", got)
	default:
	}

	if err := d.Configure(); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	configured := p.configured
	session := p.session
	p.mu.Unlock()
	if !configured {
		t.Fatal("the portal wasn't asked to show its settings")
	}

	// The desktop ending the session ends it here.
	_ = conn.Emit(session, portalSession+".Closed", map[string]dbus.Variant{})
	select {
	case <-d.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("the session didn't end with the desktop's")
	}
}

func TestDesktopKeysOnGNOMEAndATurnedDownDialog(t *testing.T) {
	conn := sessionBus(t)
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	p := startPortal(t, conn, 1)
	p.mu.Lock()
	p.bindAnswer = 1
	p.mu.Unlock()
	d, err := OpenDesktopKeys("chat.dens.Dens")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.CanConfigure() || !strings.Contains(d.Note(), "GNOME") {
		t.Fatalf("a version 1 portal on GNOME: configure %v, note %q", d.CanConfigure(), d.Note())
	}
	if err := d.Configure(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Configure on version 1 = %v", err)
	}
	if _, err := d.Bind(context.Background(), []DesktopAction{{ID: "ptt", Trigger: "F8"}}); !errors.Is(err, ErrDesktopCancelled) {
		t.Fatalf("Bind turned down = %v", err)
	}
}

func TestDesktopKeysNeedWaylandAndThePortal(t *testing.T) {
	sessionBus(t)
	if _, err := OpenDesktopKeys("chat.dens.Dens"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("with no portal on the bus = %v", err)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	if _, err := OpenDesktopKeys("chat.dens.Dens"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("outside Wayland = %v", err)
	}
}
