package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// The portals the Flatpak leans on (M9.3), from inside its sandbox: the
// desktop's Secret, Background and Notification portals, and Flatpak's own,
// which starts sandboxes and watches for updates.
const (
	portalSecret       = "org.freedesktop.portal.Secret"
	portalBackground   = "org.freedesktop.portal.Background"
	portalNotification = "org.freedesktop.portal.Notification"

	flatpakPortalName = "org.freedesktop.portal.Flatpak"
	flatpakPortalPath = "/org/freedesktop/portal/Flatpak"
	flatpakPortal     = "org.freedesktop.portal.Flatpak"
	updateMonitor     = "org.freedesktop.portal.Flatpak.UpdateMonitor"

	// spawnLatestVersion is FLATPAK_SPAWN_FLAGS_LATEST_VERSION: Spawn runs
	// the newest build of the app the system has, not the running one.
	spawnLatestVersion = 1 << 1
)

// maxSecret bounds what the Secret portal's pipe may carry; a keyring's
// secret for an app is 64 bytes.
const maxSecret = 1024

var (
	// ErrNothingToUpdate is Flatpak's update monitor finding the newest
	// build installed already, as when something else installed it first.
	ErrNothingToUpdate = errors.New("nothing to update")
	// ErrUpdateRefused is the monitor refusing to install an update: the
	// desktop won't ask the member, as GNOME won't for an app with no
	// window, the member said no, or the update asks for a new
	// permission, which only a software center grants.
	ErrUpdateRefused = errors.New("Flatpak won't install the update from Dens")
)

// portalConn is a private connection to the session bus for the portals'
// calls, with each request's answer handed to whoever waits for it, and
// the bus's other signals to other.
type portalConn struct {
	conn   *dbus.Conn
	sender string

	mu    sync.Mutex
	waits map[dbus.ObjectPath]chan *dbus.Signal
	seq   int
	other func(*dbus.Signal)
	done  chan struct{}
}

// dialPortal connects to the session bus for the portals, watching the
// signals matches name besides the requests' answers.
func dialPortal(other func(*dbus.Signal), matches ...[]dbus.MatchOption) (*portalConn, error) {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("authenticate on the session bus: %w", err)
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("hello on the session bus: %w", err)
	}
	p := &portalConn{
		conn:   conn,
		sender: strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_"),
		waits:  map[dbus.ObjectPath]chan *dbus.Signal{},
		other:  other,
		done:   make(chan struct{}),
	}
	for _, match := range append([][]dbus.MatchOption{{dbus.WithMatchInterface(portalRequest), dbus.WithMatchMember("Response")}}, matches...) {
		if err := conn.AddMatchSignal(match...); err != nil {
			conn.Close()
			return nil, err
		}
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	go p.dispatch(signals)
	return p, nil
}

func (p *portalConn) dispatch(signals <-chan *dbus.Signal) {
	defer close(p.done)
	for s := range signals {
		if s.Name == portalRequest+".Response" {
			p.mu.Lock()
			wait := p.waits[s.Path]
			p.mu.Unlock()
			if wait != nil && len(s.Body) == 2 {
				wait <- s
			}
			continue
		}
		if p.other != nil {
			p.other(s)
		}
	}
}

func (p *portalConn) Close() error { return p.conn.Close() }

func (p *portalConn) token() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	return fmt.Sprintf("dens%d_%d", os.Getpid(), p.seq)
}

// request calls a desktop portal's method that answers through a Request,
// and waits for the answer. The method's last argument is its options, to
// which it adds the handle token, so the answer's path is known before the
// call and none arrives unwatched.
func (p *portalConn) request(ctx context.Context, method string, args ...any) (uint32, map[string]dbus.Variant, error) {
	token := p.token()
	path := dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", portalPath, p.sender, token))
	answer := make(chan *dbus.Signal, 1)
	p.mu.Lock()
	p.waits[path] = answer
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.waits, path)
		p.mu.Unlock()
	}()
	args[len(args)-1].(map[string]dbus.Variant)["handle_token"] = dbus.MakeVariant(token)
	if err := p.conn.Object(portalName, portalPath).CallWithContext(ctx, method, 0, args...).Err; err != nil {
		return 0, nil, fmt.Errorf("%s: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-p.done:
		return 0, nil, fmt.Errorf("%s: the session bus went away", method)
	case s := <-answer:
		code, _ := s.Body[0].(uint32)
		results, _ := s.Body[1].(map[string]dbus.Variant)
		return code, results, nil
	}
}

// waitForInterface waits up to wait for the desktop portal to offer iface,
// which it may not yet while the desktop starts its backends, as at
// sign-in. A session bus with no desktop portal at all has none at once.
func (p *portalConn) waitForInterface(ctx context.Context, iface string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		_, err := p.conn.Object(portalName, portalPath).GetProperty(iface + ".version")
		if err == nil {
			return nil
		}
		var dbusErr dbus.Error
		if errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" || !time.Now().Before(deadline) {
			return fmt.Errorf("the desktop has no %s portal: %w", strings.TrimPrefix(iface, "org.freedesktop.portal."), errors.ErrUnsupported)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// RetrieveSecret asks the desktop's Secret portal for the secret it keeps
// for this app in the member's keyring (M9.3), which the portal writes to a
// pipe the call hands it. It waits up to wait for the portal's Secret
// interface before taking its absence for no keyring (errors.ErrUnsupported),
// and for the answer as long as ctx allows: a locked keyring prompts, and
// the request waits for the member. A prompt turned down is
// ErrDesktopCancelled. The caller clears the secret.
func RetrieveSecret(ctx context.Context, wait time.Duration) ([]byte, error) {
	p, err := dialPortal(nil)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	if err := p.waitForInterface(ctx, portalSecret, wait); err != nil {
		return nil, err
	}
	if !p.conn.SupportsUnixFDs() {
		return nil, errors.New("the session bus can't hand the Secret portal a pipe")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	code, _, err := p.request(ctx, portalSecret+".RetrieveSecret", dbus.UnixFD(w.Fd()), map[string]dbus.Variant{})
	// The portal holds its own copy of the pipe once the call is made, so
	// the read ends when it closes that.
	w.Close()
	if err != nil {
		return nil, err
	}
	switch code {
	case 0:
	case 1:
		return nil, fmt.Errorf("the keyring's prompt: %w", ErrDesktopCancelled)
	default:
		return nil, fmt.Errorf("the keyring refused the secret (answer %d)", code)
	}
	_ = r.SetReadDeadline(time.Now().Add(10 * time.Second))
	secret, err := io.ReadAll(io.LimitReader(r, maxSecret+1))
	switch {
	case err != nil:
		clear(secret)
		return nil, fmt.Errorf("read the secret: %w", err)
	case len(secret) == 0:
		return nil, errors.New("the keyring gave an empty secret")
	case len(secret) > maxSecret:
		clear(secret)
		return nil, fmt.Errorf("the keyring's secret is over %d bytes", maxSecret)
	}
	return secret, nil
}

// RequestBackground asks the desktop to let the app run with no window
// (M9.3) and, with autostart, to start commandline at the member's sign-in,
// which the portal writes an autostart entry for, or without, to stop. It
// reports whether the desktop allows each; no desktop in the spike asked
// the member.
func RequestBackground(ctx context.Context, reason string, autostart bool, commandline []string) (background, autostarted bool, err error) {
	p, err := dialPortal(nil)
	if err != nil {
		return false, false, err
	}
	defer p.Close()
	if err := p.waitForInterface(ctx, portalBackground, 0); err != nil {
		return false, false, err
	}
	opts := map[string]dbus.Variant{
		"reason":           dbus.MakeVariant(reason),
		"autostart":        dbus.MakeVariant(autostart),
		"dbus-activatable": dbus.MakeVariant(false),
	}
	if autostart {
		opts["commandline"] = dbus.MakeVariant(commandline)
	}
	code, results, err := p.request(ctx, portalBackground+".RequestBackground", "", opts)
	if err != nil {
		return false, false, err
	}
	if code != 0 {
		return false, false, fmt.Errorf("the desktop answered %d", code)
	}
	background, _ = results["background"].Value().(bool)
	autostarted, _ = results["autostart"].Value().(bool)
	return background, autostarted, nil
}

// SetBackgroundStatus is the line the desktop shows under the app among
// its background apps, as GNOME's Background Apps does (M9.3).
func SetBackgroundStatus(message string) error {
	p, err := dialPortal(nil)
	if err != nil {
		return err
	}
	defer p.Close()
	return p.conn.Object(portalName, portalPath).Call(portalBackground+".SetStatus", 0,
		map[string]dbus.Variant{"message": dbus.MakeVariant(message)}).Err
}

// Notify shows a desktop notification through the portal (M9.3): how the
// Flatpak's launch, which has no window, says why Dens didn't start.
func Notify(id, title, body string) error {
	p, err := dialPortal(nil)
	if err != nil {
		return err
	}
	defer p.Close()
	return p.conn.Object(portalName, portalPath).Call(portalNotification+".AddNotification", 0, id, map[string]dbus.Variant{
		"title": dbus.MakeVariant(title),
		"body":  dbus.MakeVariant(body),
	}).Err
}

// SpawnLatest starts argv as a sandbox of its own, from the newest build of
// this Flatpak the system has (M9.3), through Flatpak's portal: nothing
// that ran this process holds it, as a terminal would. files become the new
// process's descriptors from 3 on.
func SpawnLatest(argv []string, files ...*os.File) error {
	p, err := dialPortal(nil)
	if err != nil {
		return err
	}
	defer p.Close()
	cstring := func(s string) []byte { return append([]byte(s), 0) }
	args := make([][]byte, 0, len(argv))
	for _, a := range argv {
		args = append(args, cstring(a))
	}
	fds := map[uint32]dbus.UnixFD{}
	for i, f := range files {
		fds[uint32(3+i)] = dbus.UnixFD(f.Fd())
	}
	var pid uint32
	return p.conn.Object(flatpakPortalName, flatpakPortalPath).Call(flatpakPortal+".Spawn", 0,
		cstring("/"), args, fds, map[string]string{}, uint32(spawnLatestVersion), map[string]dbus.Variant{}).Store(&pid)
}

// FlatpakUpdate is what Flatpak's update monitor found (M9.3): the commits
// of the running build, the newest one installed, and the remote's newest.
type FlatpakUpdate struct {
	Running, Local, Remote string
}

// UpdateMonitor is Flatpak's update monitor for this app (M9.3). Flatpak's
// portal checks the app's remote every 30 minutes, the first time 30
// minutes after the first monitor starts, and reports each update it finds.
type UpdateMonitor struct {
	p       *portalConn
	handle  dbus.ObjectPath
	updates chan FlatpakUpdate

	mu       sync.Mutex
	progress chan map[string]dbus.Variant
	once     sync.Once
}

// OpenUpdateMonitor starts an update monitor. It fails where the session
// bus has no Flatpak portal.
func OpenUpdateMonitor() (*UpdateMonitor, error) {
	m := &UpdateMonitor{updates: make(chan FlatpakUpdate, 4)}
	p, err := dialPortal(m.signal, []dbus.MatchOption{dbus.WithMatchInterface(updateMonitor)})
	if err != nil {
		return nil, err
	}
	m.p = p
	token := p.token()
	m.handle = dbus.ObjectPath(fmt.Sprintf("%s/update_monitor/%s/%s", flatpakPortalPath, p.sender, token))
	var handle dbus.ObjectPath
	if err := p.conn.Object(flatpakPortalName, flatpakPortalPath).Call(flatpakPortal+".CreateUpdateMonitor", 0,
		map[string]dbus.Variant{"handle_token": dbus.MakeVariant(token)}).Store(&handle); err != nil {
		p.Close()
		return nil, fmt.Errorf("start Flatpak's update monitor: %w", err)
	}
	if handle != m.handle {
		p.Close()
		return nil, fmt.Errorf("Flatpak's update monitor is at %s, not %s", handle, m.handle)
	}
	return m, nil
}

func (m *UpdateMonitor) signal(s *dbus.Signal) {
	if s.Path != m.handle || len(s.Body) != 1 {
		return
	}
	info, _ := s.Body[0].(map[string]dbus.Variant)
	switch s.Name {
	case updateMonitor + ".UpdateAvailable":
		commit := func(key string) string { c, _ := info[key].Value().(string); return c }
		select {
		case m.updates <- FlatpakUpdate{Running: commit("running-commit"), Local: commit("local-commit"), Remote: commit("remote-commit")}:
		default:
		}
	case updateMonitor + ".Progress":
		m.mu.Lock()
		progress := m.progress
		m.mu.Unlock()
		if progress != nil {
			select {
			case progress <- info:
			default:
			}
		}
	}
}

// Updates delivers each update the monitor finds.
func (m *UpdateMonitor) Updates() <-chan FlatpakUpdate { return m.updates }

// Update has the monitor install the update it found, and waits until it's
// installed: ErrNothingToUpdate where the newest build is installed already,
// and ErrUpdateRefused where Flatpak won't install it from Dens.
func (m *UpdateMonitor) Update(ctx context.Context) error {
	progress := make(chan map[string]dbus.Variant, 16)
	m.mu.Lock()
	if m.progress != nil {
		m.mu.Unlock()
		return errors.New("an update is under way")
	}
	m.progress = progress
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.progress = nil
		m.mu.Unlock()
	}()
	err := m.p.conn.Object(flatpakPortalName, m.handle).CallWithContext(ctx, updateMonitor+".Update", 0, "", map[string]dbus.Variant{}).Err
	var dbusErr dbus.Error
	if errors.As(err, &dbusErr) && (dbusErr.Name == "org.freedesktop.DBus.Error.AccessDenied" || dbusErr.Name == "org.freedesktop.portal.Error.NotAllowed") {
		return fmt.Errorf("%w: %v", ErrUpdateRefused, err)
	}
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.p.done:
			return errors.New("update: the session bus went away")
		case info := <-progress:
			status, _ := info["status"].Value().(uint32)
			switch status {
			case 0:
				continue
			case 1:
				return ErrNothingToUpdate
			case 2:
				return nil
			}
			name, _ := info["error"].Value().(string)
			message, _ := info["error_message"].Value().(string)
			if name == "org.freedesktop.portal.Error.NotAllowed" {
				return fmt.Errorf("%w: %s", ErrUpdateRefused, message)
			}
			return fmt.Errorf("update: %s: %s", name, message)
		}
	}
}

// Close stops the monitor.
func (m *UpdateMonitor) Close() error {
	var err error
	m.once.Do(func() {
		_ = m.p.conn.Object(flatpakPortalName, m.handle).Call(updateMonitor+".Close", 0).Err
		err = m.p.Close()
	})
	return err
}

// AppName is the app's name on the session bus (M9.3), where it answers
// org.freedesktop.Application: GNOME's Background Apps quits an app with no
// window by activating the quit action there.
type AppName struct {
	conn *dbus.Conn
}

// OwnAppName takes appID on the session bus, and has quit run when
// something activates the app's quit action.
func OwnAppName(appID string, quit func()) (*AppName, error) {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	path := dbus.ObjectPath("/" + strings.ReplaceAll(appID, ".", "/"))
	actions := appActions{quit: quit}
	for iface, v := range map[string]any{"org.freedesktop.Application": application{actions}, "org.gtk.Actions": actions} {
		if err := conn.Export(v, path, iface); err != nil {
			conn.Close()
			return nil, err
		}
	}
	reply, err := conn.RequestName(appID, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("take %s on the session bus: %w", appID, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("something else holds %s on the session bus", appID)
	}
	return &AppName{conn: conn}, nil
}

// Close lets go of the name.
func (n *AppName) Close() error { return n.conn.Close() }

// application is org.freedesktop.Application for an app with no window:
// there's nothing to activate or open, and quit is its one action.
type application struct{ actions appActions }

func (application) Activate(map[string]dbus.Variant) *dbus.Error       { return nil }
func (application) Open([]string, map[string]dbus.Variant) *dbus.Error { return nil }
func (a application) ActivateAction(name string, _ []dbus.Variant, _ map[string]dbus.Variant) *dbus.Error {
	return a.actions.Activate(name, nil, nil)
}

// appActions is org.gtk.Actions, as GLib apps export beside
// org.freedesktop.Application, with the one action.
type appActions struct{ quit func() }

type actionDescription struct {
	Enabled bool
	Param   dbus.Signature
	State   []dbus.Variant
}

func (appActions) List() ([]string, *dbus.Error) { return []string{"quit"}, nil }
func (appActions) Describe(name string) (actionDescription, *dbus.Error) {
	if name != "quit" {
		return actionDescription{}, dbus.MakeFailedError(fmt.Errorf("no action %q", name))
	}
	return actionDescription{Enabled: true}, nil
}
func (a appActions) DescribeAll() (map[string]actionDescription, *dbus.Error) {
	d, _ := a.Describe("quit")
	return map[string]actionDescription{"quit": d}, nil
}
func (a appActions) Activate(name string, _ []dbus.Variant, _ map[string]dbus.Variant) *dbus.Error {
	if name != "quit" {
		return dbus.MakeFailedError(fmt.Errorf("no action %q", name))
	}
	go a.quit()
	return nil
}
func (appActions) SetState(string, dbus.Variant, map[string]dbus.Variant) *dbus.Error {
	return dbus.MakeFailedError(errors.New("no action has state"))
}
