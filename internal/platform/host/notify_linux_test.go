package host

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeNotifications is the desktop's freedesktop notification service.
type fakeNotifications struct {
	conn *dbus.Conn

	mu     sync.Mutex
	next   uint32
	calls  []fakeNotify
	closed []uint32
}

type fakeNotify struct {
	replaces      uint32
	icon, summary string
	body          string
	actions       []string
	entry         string
}

func startNotifications(t *testing.T, conn *dbus.Conn) *fakeNotifications {
	t.Helper()
	f := &fakeNotifications{conn: conn, next: 40}
	if err := conn.Export(f, notificationsPath, notificationsIface); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(notificationsName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own the notification service's name: %v %v", reply, err)
	}
	return f
}

func (f *fakeNotifications) GetCapabilities() ([]string, *dbus.Error) {
	return []string{"actions", "body", "body-markup", "body-hyperlinks"}, nil
}

func (f *fakeNotifications) Notify(app string, replaces uint32, icon, summary, body string, actions []string, hints map[string]dbus.Variant, timeout int32) (uint32, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, _ := hints["desktop-entry"].Value().(string)
	f.calls = append(f.calls, fakeNotify{replaces: replaces, icon: icon, summary: summary, body: body, actions: actions, entry: entry})
	if replaces != 0 {
		return replaces, nil
	}
	f.next++
	return f.next, nil
}

func (f *fakeNotifications) CloseNotification(id uint32) *dbus.Error {
	f.mu.Lock()
	f.closed = append(f.closed, id)
	f.mu.Unlock()
	return nil
}

func (f *fakeNotifications) GetServerInformation() (string, string, string, string, *dbus.Error) {
	return "fake", "dens", "1", "1.2", nil
}

func waitClick(t *testing.T, n *Notifier) string {
	t.Helper()
	select {
	case id := <-n.Clicks():
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("no click came")
		return ""
	}
}

// The helper shows a notification through the desktop's notification
// service as Dens's, its text escaped where the service reads markup, a
// newer one replacing it; a click on its body comes back as its ID, and
// taking it back closes it (M12.3).
func TestNotifierShowsThroughTheDesktopsService(t *testing.T) {
	conn := sessionBus(t)
	if _, err := OpenNotifier("chat.dens.Dens", false); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("with no notification service: %v", err)
	}
	f := startNotifications(t, conn)
	n, err := OpenNotifier("chat.dens.Dens", false)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()

	if err := n.Show("main/d1/7", DesktopNotification{Title: "Alice · Den", Body: `<b>hi</b> & <a href="x">bye</a>`}); err != nil {
		t.Fatal(err)
	}
	if err := n.Show("main/d1/7", DesktopNotification{Title: "Alice · Den", Body: "again"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := slices.Clone(f.calls)
	f.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("Notify calls: %+v", calls)
	}
	first := calls[0]
	if first.body != "&lt;b&gt;hi&lt;/b&gt; &amp; &lt;a href=&quot;x&quot;&gt;bye&lt;/a&gt;" || first.summary != "Alice · Den" ||
		first.replaces != 0 || first.entry != "chat.dens.Dens" || first.icon != "chat.dens.Dens" || !slices.Equal(first.actions, []string{"default", "Open"}) {
		t.Fatalf("the first notification: %+v", first)
	}
	if calls[1].replaces != 41 {
		t.Fatalf("the newer notification replaced %d, want 41", calls[1].replaces)
	}

	// Another app's notification's click isn't the helper's.
	_ = conn.Emit(notificationsPath, notificationsIface+".ActionInvoked", uint32(7), "default")
	_ = conn.Emit(notificationsPath, notificationsIface+".ActionInvoked", uint32(41), "default")
	if id := waitClick(t, n); id != "main/d1/7" {
		t.Fatalf("a click came back as %q", id)
	}
	if err := n.Withdraw("main/d1/7"); err != nil {
		t.Fatal(err)
	}
	// What the service closed by itself, the helper doesn't close again.
	if err := n.Show("main/d1/8", DesktopNotification{Title: "Bob", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Emit(notificationsPath, notificationsIface+".NotificationClosed", uint32(42), uint32(2))
	time.Sleep(100 * time.Millisecond)
	if err := n.Withdraw("main/d1/8"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Equal(f.closed, []uint32{41}) {
		t.Fatalf("closed %v, want 41 alone", f.closed)
	}
}

// From the Flatpak's sandbox, notifications go through the desktop's
// Notification portal, whose word of a click comes back as a signal; where
// the desktop has no such portal, there's no notifier (M12.3).
func TestNotifierThroughThePortal(t *testing.T) {
	conn := sessionBus(t)
	f := startFlatpakPortals(t, conn)
	f.mu.Lock()
	f.noNotifications = true
	f.mu.Unlock()
	if _, err := OpenNotifier("chat.dens.Dens", true); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("with no Notification portal: %v", err)
	}
	f.mu.Lock()
	f.noNotifications = false
	f.mu.Unlock()
	n, err := OpenNotifier("chat.dens.Dens", true)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if err := n.Show("main/d1/7", DesktopNotification{Title: "Alice · Den", Body: "<b>as written</b>"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Emit(portalPath, portalNotification+".ActionInvoked", "main/d1/7", "open", []dbus.Variant{})
	if id := waitClick(t, n); id != "main/d1/7" {
		t.Fatalf("a click came back as %q", id)
	}
	if err := n.Withdraw("main/d1/7"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Equal(f.notified, []string{"main/d1/7: Alice · Den: <b>as written</b>"}) || !slices.Equal(f.actions, []string{"open"}) ||
		!slices.Equal(f.removed, []string{"main/d1/7"}) {
		t.Fatalf("notified %q with %q, removed %q", f.notified, f.actions, f.removed)
	}
}
