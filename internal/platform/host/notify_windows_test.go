package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A toast's content is its text, escaped as XML, a member's text included,
// with the address a click opens (M12.3).
func TestToastXML(t *testing.T) {
	got := toastXML(DesktopNotification{Title: `Alice · <Den & "co">`, Body: "</text><text>injected", Open: "http://localhost:8484/den/d1/7#message=10&x=\"1\""})
	for _, want := range []string{
		`activationType="protocol"`,
		`launch="http://localhost:8484/den/d1/7#message=10&amp;x=&#34;1&#34;"`,
		`<text>Alice · &lt;Den &amp; &#34;co&#34;&gt;</text>`,
		`<text>&lt;/text&gt;&lt;text&gt;injected</text>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the toast's content %s lacks %s", got, want)
		}
	}
	if strings.Contains(toastXML(DesktopNotification{Title: "a", Body: "b"}), "activationType") {
		t.Error("a toast with nowhere to open opens something")
	}
	if a, b := toastTag("main/d1/7"), toastTag("main/d1/7"); a != b || len(a) > 64 || a == toastTag("main/d1/8") {
		t.Errorf("tags %q and %q", a, b)
	}
}

var iidToastHistory2 = windows.GUID{Data1: 0x3BC3D253, Data2: 0x2F31, Data3: 0x4092, Data4: [8]byte{0x91, 0x29, 0x8A, 0xD5, 0xAB, 0xF0, 0x67, 0xDA}}

// showing counts the toasts the Action Center holds for appID.
func (n *Notifier) showing(appID string) (int, error) {
	var count uint32
	err := n.do(func() error {
		history2, err := n.history.query(&iidToastHistory2)
		if err != nil {
			return err
		}
		defer history2.release()
		app, err := newHString(appID)
		if err != nil {
			return err
		}
		defer app.free()
		var list *comObject
		// IToastNotificationHistory2's GetHistoryWithId, and its list's
		// get_Size.
		if err := history2.call(7, uintptr(app), uintptr(unsafe.Pointer(&list))); err != nil {
			return err
		}
		defer list.release()
		return list.call(7, uintptr(unsafe.Pointer(&count)))
	})
	return int(count), err
}

// A toast shows under the app ID a Start menu shortcut carries, and goes
// when it's taken back (M12.3). It shows on the desktop of whoever runs it,
// for a moment, so it runs only where DENS_TOAST_TEST is set, as from WSL:
//
//	GOOS=windows go test -c -o host.test.exe ./internal/platform/host
//	DENS_TOAST_TEST=1 WSLENV=DENS_TOAST_TEST ./host.test.exe -test.run TestToastShowsAndGoes -test.v
func TestToastShowsAndGoes(t *testing.T) {
	if os.Getenv("DENS_TOAST_TEST") == "" {
		t.Skip("shows a toast on this desktop: set DENS_TOAST_TEST to run it")
	}
	programs, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		t.Fatal(err)
	}
	const appID = "chat.dens.Dens.test"
	link := filepath.Join(programs, "Dens test.lnk")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := MakeShortcut(link, exe, "-test.run=None", "Dens's toast test", appID); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(link)
	// The shell takes a moment to see a new shortcut's app ID.
	time.Sleep(3 * time.Second)
	n, err := OpenNotifier(appID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if err := n.Show("test/d1/7", DesktopNotification{Title: "Dens's toast test", Body: "Goes again in a moment.", Open: "http://localhost:8484/"}); err != nil {
		t.Fatal(err)
	}
	if err := n.Show("test/d1/7", DesktopNotification{Title: "Dens's toast test", Body: "Replaced.", Open: "http://localhost:8484/"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if count, err := n.showing(appID); err != nil || count != 1 {
		t.Fatalf("the Action Center holds %d of the test's toasts, %v; want 1", count, err)
	}
	if err := n.Withdraw("test/d1/7"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if count, err := n.showing(appID); err != nil || count != 0 {
		t.Fatalf("after taking it back, the Action Center holds %d, %v", count, err)
	}
}
