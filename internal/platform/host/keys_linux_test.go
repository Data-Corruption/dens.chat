package host

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyStateNeedsAnX11Display(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	if _, err := OpenKeyState(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("under Wayland, OpenKeyState = %v, want ErrUnsupported", err)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "")
	t.Setenv("DISPLAY", "")
	if _, err := OpenKeyState(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("with no display, OpenKeyState = %v, want ErrUnsupported", err)
	}
}

// startXvfb runs a virtual X server for the test, with access control off,
// and points DISPLAY at it.
func startXvfb(t *testing.T) {
	t.Helper()
	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		t.Skip("no Xvfb to read keys from")
	}
	n := 0
	for n = 40; n < 90; n++ {
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", n)); errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	cmd := exec.Command(xvfb, fmt.Sprintf(":%d", n), "-ac", "-nolisten", "tcp", "-screen", "0", "640x480x24")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	for range 100 {
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", n)); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Setenv("DISPLAY", fmt.Sprintf(":%d", n))
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "x11")
	t.Setenv("XAUTHORITY", filepath.Join(t.TempDir(), "none"))
}

// fakeKey presses or releases a keycode through the XTEST extension, as
// xdotool does.
func fakeKey(t *testing.T, x *x11Conn, keycode byte, press bool) {
	t.Helper()
	x.mu.Lock()
	defer x.mu.Unlock()
	name := []byte("XTEST")
	req := []byte{98, 0}
	req = binary.LittleEndian.AppendUint16(req, uint16(2+(len(name)+3)/4))
	req = binary.LittleEndian.AppendUint16(req, uint16(len(name)))
	req = append(req, 0, 0)
	req = append(req, padded(name)...)
	if _, err := x.conn.Write(req); err != nil {
		t.Fatal(err)
	}
	var reply [32]byte
	for {
		if _, err := io.ReadFull(x.conn, reply[:]); err != nil {
			t.Fatal(err)
		}
		if reply[0] == 1 {
			break
		}
		if reply[0] == 0 {
			t.Fatalf("QueryExtension failed: %d", reply[1])
		}
	}
	if reply[8] == 0 {
		t.Skip("the X server has no XTEST")
	}
	kind := byte(3) // KeyRelease
	if press {
		kind = 2 // KeyPress
	}
	fake := make([]byte, 36)
	fake[0], fake[1] = reply[9], 2 // XTEST's FakeInput
	binary.LittleEndian.PutUint16(fake[2:], 9)
	fake[4], fake[5] = kind, keycode
	if _, err := x.conn.Write(fake); err != nil {
		t.Fatal(err)
	}
}

func TestKeyStateReadsX11Keys(t *testing.T) {
	startXvfb(t)
	ks, err := OpenKeyState()
	if err != nil {
		t.Fatal(err)
	}
	defer ks.Close()
	v, ok := ks.Key("KeyV")
	if !ok || v != 47+8 {
		t.Fatalf("KeyV is keycode %d, %v", v, ok)
	}
	if _, ok := ks.Key("Mouse4"); ok {
		t.Fatal("a mouse button resolved to a key")
	}
	if ks.Down(v) {
		t.Fatal("V reads as held before any press")
	}
	fakeKey(t, ks.x, byte(v), true)
	fakeKey(t, ks.x, 42+8, true) // left Shift
	if !ks.Down(v) {
		t.Fatal("V doesn't read as held")
	}
	if ctrl, alt, shift, meta := ks.Modifiers(); ctrl || alt || !shift || meta {
		t.Fatalf("modifiers = %v %v %v %v, want Shift alone", ctrl, alt, shift, meta)
	}
	fakeKey(t, ks.x, byte(v), false)
	fakeKey(t, ks.x, 42+8, false)
	if ks.Down(v) {
		t.Fatal("V still reads as held once let go")
	}
}

func TestXauthorityGivesTheDisplaysCookie(t *testing.T) {
	hostname, _ := os.Hostname()
	entry := func(family uint16, addr, number, name, data string) []byte {
		var b []byte
		b = binary.BigEndian.AppendUint16(b, family)
		for _, f := range []string{addr, number, name, data} {
			b = binary.BigEndian.AppendUint16(b, uint16(len(f)))
			b = append(b, f...)
		}
		return b
	}
	var file []byte
	file = append(file, entry(256, "another-host", "5", "MIT-MAGIC-COOKIE-1", "theirs")...)
	file = append(file, entry(256, hostname, "4", "MIT-MAGIC-COOKIE-1", "display four")...)
	file = append(file, entry(256, hostname, "5", "MIT-MAGIC-COOKIE-1", "display five")...)
	path := filepath.Join(t.TempDir(), "Xauthority")
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XAUTHORITY", path)
	if name, data := xauthCookie("5"); string(name) != "MIT-MAGIC-COOKIE-1" || string(data) != "display five" {
		t.Fatalf("cookie for :5 = %q %q", name, data)
	}
	if name, _ := xauthCookie("7"); name != nil {
		t.Fatalf("a cookie for a display the file doesn't hold: %q", name)
	}
}
