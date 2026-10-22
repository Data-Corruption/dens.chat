package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	desktopName = "org.freedesktop.portal.Desktop"
	desktopPath = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	flatpakName = "org.freedesktop.portal.Flatpak"
	flatpakPath = dbus.ObjectPath("/org/freedesktop/portal/Flatpak")

	requestIf    = "org.freedesktop.portal.Request"
	backgroundIf = "org.freedesktop.portal.Background"
	secretIf     = "org.freedesktop.portal.Secret"
	shortcutsIf  = "org.freedesktop.portal.GlobalShortcuts"
	openURIIf    = "org.freedesktop.portal.OpenURI"
	flatpakIf    = "org.freedesktop.portal.Flatpak"
	monitorIf    = "org.freedesktop.portal.Flatpak.UpdateMonitor"

	// spawnLatestVersion is FLATPAK_SPAWN_FLAGS_LATEST_VERSION: Spawn runs
	// the newest installed build of the app, not the running one.
	spawnLatestVersion = 1 << 1
)

// portal is the session bus with the portals' signals sorted: each
// request's response to whoever waits for it, and everything else to
// Other.
type portal struct {
	conn   *dbus.Conn
	sender string // the unique name as request paths spell it
	Other  chan *dbus.Signal

	mu    sync.Mutex
	waits map[dbus.ObjectPath]chan *dbus.Signal
	seq   int
}

func openPortal() (*portal, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("session bus: %w", err)
	}
	for _, iface := range []string{requestIf, shortcutsIf, monitorIf} {
		if err := conn.AddMatchSignal(dbus.WithMatchInterface(iface)); err != nil {
			conn.Close()
			return nil, err
		}
	}
	in := make(chan *dbus.Signal, 256)
	conn.Signal(in)
	p := &portal{
		conn:   conn,
		sender: strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_"),
		Other:  make(chan *dbus.Signal, 256),
		waits:  map[dbus.ObjectPath]chan *dbus.Signal{},
	}
	go p.sort(in)
	return p, nil
}

func (p *portal) sort(in chan *dbus.Signal) {
	for s := range in {
		p.mu.Lock()
		wait, ok := p.waits[s.Path]
		p.mu.Unlock()
		if ok && s.Name == requestIf+".Response" {
			wait <- s
			continue
		}
		select {
		case p.Other <- s:
		default:
		}
	}
	close(p.Other)
}

func (p *portal) desktop() dbus.BusObject { return p.conn.Object(desktopName, desktopPath) }
func (p *portal) flatpak() dbus.BusObject { return p.conn.Object(flatpakName, flatpakPath) }

func (p *portal) token() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	return fmt.Sprintf("spike%d_%d", os.Getpid(), p.seq)
}

// version reads an interface's version property, or says why it can't.
func (p *portal) version(obj dbus.BusObject, iface string) string {
	v, err := obj.GetProperty(iface + ".version")
	if err != nil {
		return "missing (" + err.Error() + ")"
	}
	return fmt.Sprint(v.Value())
}

// response is a portal request's answer.
type response struct {
	Code    uint32
	Results map[string]dbus.Variant
	Took    time.Duration
}

func (r response) String() string {
	names := [...]string{"success", "cancelled", "failed"}
	return fmt.Sprintf("response %d (%s) after %v", r.Code, names[min(r.Code, 2)], r.Took.Round(time.Millisecond))
}

// request calls a portal method that answers through a Request object, and
// waits for the answer. The call's last argument is its options, to which
// it adds the handle token, so the path to wait on is known before the
// call is made.
func (p *portal) request(obj dbus.BusObject, method string, timeout time.Duration, args ...any) (response, error) {
	token := p.token()
	path := dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", desktopPath, p.sender, token))
	wait := make(chan *dbus.Signal, 1)
	p.mu.Lock()
	p.waits[path] = wait
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.waits, path)
		p.mu.Unlock()
	}()
	args[len(args)-1].(map[string]dbus.Variant)["handle_token"] = dbus.MakeVariant(token)
	started := time.Now()
	var handle dbus.ObjectPath
	if err := obj.Call(method, 0, args...).Store(&handle); err != nil {
		return response{Took: time.Since(started)}, fmt.Errorf("%s: %w", method, err)
	}
	if handle != path {
		return response{Took: time.Since(started)}, fmt.Errorf("%s: the portal answers at %s, not %s", method, handle, path)
	}
	select {
	case s := <-wait:
		r := response{Took: time.Since(started)}
		r.Code, _ = s.Body[0].(uint32)
		r.Results, _ = s.Body[1].(map[string]dbus.Variant)
		return r, nil
	case <-time.After(timeout):
		return response{Took: time.Since(started)}, fmt.Errorf("%s: no answer in %v", method, timeout)
	}
}

// secret is what one RetrieveSecret gave: never the secret itself, only
// its length and the start of its SHA-256, which tell two answers apart.
type secret struct {
	response
	Bytes  int
	Digest string
}

// retrieveSecret asks the Secret portal for the app's secret, which it
// writes to a pipe the call hands over.
func (p *portal) retrieveSecret(timeout time.Duration) (secret, error) {
	if !p.conn.SupportsUnixFDs() {
		return secret{}, errors.New("this bus connection can't pass file descriptors")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return secret{}, err
	}
	defer r.Close()
	resp, err := p.request(p.desktop(), secretIf+".RetrieveSecret", timeout, dbus.UnixFD(w.Fd()), map[string]dbus.Variant{})
	// The portal holds its own copy of the pipe once the call is made, so
	// ours goes, and the read ends when the portal closes its copy.
	w.Close()
	s := secret{response: resp}
	if err != nil || resp.Code != 0 {
		return s, err
	}
	_ = r.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, err := io.ReadAll(io.LimitReader(r, 4096))
	s.Bytes = len(data)
	sum := sha256.Sum256(data)
	s.Digest = hex.EncodeToString(sum[:8])
	clear(data)
	if err != nil {
		return s, fmt.Errorf("read the secret: %w", err)
	}
	return s, nil
}

// requestBackground asks to run in the background and, with autostart, to
// start at sign-in with commandline.
func (p *portal) requestBackground(reason string, autostart bool, commandline []string) (response, error) {
	opts := map[string]dbus.Variant{
		"reason":           dbus.MakeVariant(reason),
		"autostart":        dbus.MakeVariant(autostart),
		"dbus-activatable": dbus.MakeVariant(false),
	}
	if len(commandline) > 0 {
		opts["commandline"] = dbus.MakeVariant(commandline)
	}
	return p.request(p.desktop(), backgroundIf+".RequestBackground", 5*time.Minute, "", opts)
}

// setStatus is the line the desktop shows under a background app, from
// version 2 of the Background portal.
func (p *portal) setStatus(message string) error {
	return p.desktop().Call(backgroundIf+".SetStatus", 0, map[string]dbus.Variant{"message": dbus.MakeVariant(message)}).Err
}

// createUpdateMonitor starts the Flatpak portal's update monitor, whose
// signals arrive on Other.
func (p *portal) createUpdateMonitor() (dbus.ObjectPath, error) {
	var handle dbus.ObjectPath
	err := p.flatpak().Call(flatpakIf+".CreateUpdateMonitor", 0, map[string]dbus.Variant{"handle_token": dbus.MakeVariant(p.token())}).Store(&handle)
	return handle, err
}

// update asks the monitor to install the update it found, which it does
// only when the update asks for no new permissions; Progress signals
// follow.
func (p *portal) update(monitor dbus.ObjectPath) error {
	return p.conn.Object(flatpakName, monitor).Call(monitorIf+".Update", 0, "", map[string]dbus.Variant{}).Err
}

// spawnLatest starts argv in the newest installed build of this app, as a
// new sandbox that outlives this one.
func (p *portal) spawnLatest(argv []string) (uint32, error) {
	cstr := func(s string) []byte { return append([]byte(s), 0) }
	var av [][]byte
	for _, a := range argv {
		av = append(av, cstr(a))
	}
	var pid uint32
	err := p.flatpak().Call(flatpakIf+".Spawn", 0, cstr("/"), av, map[uint32]dbus.UnixFD{}, map[string]string{},
		uint32(spawnLatestVersion), map[string]dbus.Variant{}).Store(&pid)
	return pid, err
}

// describe prints a signal's body for the log.
func describe(s *dbus.Signal) string {
	var parts []string
	for _, b := range s.Body {
		if m, ok := b.(map[string]dbus.Variant); ok {
			var kv []string
			for k, v := range m {
				kv = append(kv, fmt.Sprintf("%s=%v", k, v.Value()))
			}
			parts = append(parts, "{"+strings.Join(kv, ", ")+"}")
			continue
		}
		parts = append(parts, fmt.Sprint(b))
	}
	return strings.TrimPrefix(s.Name, "org.freedesktop.portal.") + " " + strings.Join(parts, " ")
}
