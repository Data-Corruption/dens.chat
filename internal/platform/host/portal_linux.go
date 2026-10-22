package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// The desktop's global shortcuts portal (M7.2), which owns the keys under
// Wayland: the helper binds the voice actions through it, the desktop asks
// the member to confirm or change them, and it reports each press and
// release.
const (
	portalName      = "org.freedesktop.portal.Desktop"
	portalPath      = "/org/freedesktop/portal/desktop"
	portalShortcuts = "org.freedesktop.portal.GlobalShortcuts"
	portalRequest   = "org.freedesktop.portal.Request"
	portalSession   = "org.freedesktop.portal.Session"
	portalRegistry  = "org.freedesktop.host.portal.Registry"
)

// DesktopKeys is the desktop's global shortcuts portal, for one session.
type DesktopKeys struct {
	conn         *dbus.Conn
	portal       dbus.BusObject
	session      dbus.ObjectPath
	configurable bool
	gnome        bool

	presses chan DesktopPress
	changes chan map[string]string
	closed  chan struct{}

	mu      sync.Mutex
	pending map[dbus.ObjectPath]chan *dbus.Signal
	seq     int
	sender  string
	once    sync.Once
}

// OpenDesktopKeys connects to the desktop's global shortcuts portal under
// Wayland, as appID, and opens a session. It fails with
// errors.ErrUnsupported where there's none: X11, or a desktop without it.
// From a sandbox, as the Flatpak's (M9.3), it tries the portal under X11
// too, which Plasma's has, since the portal knows a sandboxed app by its
// Flatpak ID.
func OpenDesktopKeys(appID string, sandboxed bool) (*DesktopKeys, error) {
	if os.Getenv("WAYLAND_DISPLAY") == "" && !sandboxed {
		return nil, fmt.Errorf("not a Wayland session: %w", errors.ErrUnsupported)
	}
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
	d := &DesktopKeys{
		conn:    conn,
		portal:  conn.Object(portalName, portalPath),
		presses: make(chan DesktopPress, 64),
		changes: make(chan map[string]string, 4),
		closed:  make(chan struct{}),
		pending: make(map[dbus.ObjectPath]chan *dbus.Signal),
		sender:  strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_"),
		gnome:   strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "GNOME"),
	}
	// The portal refuses an unsandboxed app it can't name: a portal with a
	// registry takes the ID from it, which must come before any other call,
	// and an older one from the systemd unit the helper runs in. A
	// sandboxed app can't register, and needn't.
	if !sandboxed {
		_ = d.portal.Call(portalRegistry+".Register", 0, appID, map[string]dbus.Variant{}).Err
	}
	version, err := d.portal.GetProperty(portalShortcuts + ".version")
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("the desktop has no global shortcuts portal: %w", errors.ErrUnsupported)
	}
	if v, ok := version.Value().(uint32); ok && v >= 2 {
		d.configurable = true
	}
	for _, match := range [][]dbus.MatchOption{
		{dbus.WithMatchInterface(portalRequest), dbus.WithMatchMember("Response")},
		{dbus.WithMatchInterface(portalShortcuts)},
		{dbus.WithMatchInterface(portalSession), dbus.WithMatchMember("Closed")},
	} {
		if err := conn.AddMatchSignal(match...); err != nil {
			conn.Close()
			return nil, err
		}
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	go d.dispatch(signals)

	results, err := d.request(context.Background(), "CreateSession", map[string]dbus.Variant{"session_handle_token": dbus.MakeVariant("dens")})
	if err != nil {
		d.Close()
		return nil, err
	}
	var session dbus.ObjectPath
	switch v := results["session_handle"].Value().(type) {
	case string:
		session = dbus.ObjectPath(v)
	case dbus.ObjectPath:
		session = v
	}
	d.mu.Lock()
	d.session = session
	d.mu.Unlock()
	if !session.IsValid() {
		d.Close()
		return nil, errors.New("the portal gave no session")
	}
	return d, nil
}

// Bind binds the actions, once a session: the desktop may ask the member
// to confirm or change them, and keeps them for later sessions. It returns
// the desktop's names for the triggers, by action, for those it bound.
func (d *DesktopKeys) Bind(ctx context.Context, actions []DesktopAction) (map[string]string, error) {
	shortcuts := make([]desktopShortcut, 0, len(actions))
	for _, a := range actions {
		opts := map[string]dbus.Variant{"description": dbus.MakeVariant(a.Description)}
		if a.Trigger != "" {
			opts["preferred_trigger"] = dbus.MakeVariant(a.Trigger)
		}
		shortcuts = append(shortcuts, desktopShortcut{ID: a.ID, Opts: opts})
	}
	results, err := d.request(ctx, "BindShortcuts", d.currentSession(), shortcuts, "", map[string]dbus.Variant{})
	if err != nil {
		return nil, err
	}
	return shortcutNames(results["shortcuts"])
}

// Presses delivers the presses and releases the desktop reports. A desktop
// may report a held key's press again as it repeats.
func (d *DesktopKeys) Presses() <-chan DesktopPress { return d.presses }

// Changes delivers the desktop's names for the triggers, by action, when
// the member changes them in the desktop's settings.
func (d *DesktopKeys) Changes() <-chan map[string]string { return d.changes }

// Closed is closed when the desktop ends the session.
func (d *DesktopKeys) Closed() <-chan struct{} { return d.closed }

// CanConfigure says whether the desktop can show its settings for the
// shortcuts (the portal's version 2).
func (d *DesktopKeys) CanConfigure() bool { return d.configurable }

// Configure asks the desktop to show its settings for the shortcuts.
func (d *DesktopKeys) Configure() error {
	if !d.configurable {
		return fmt.Errorf("the desktop can't show its settings for the shortcuts: %w", errors.ErrUnsupported)
	}
	return d.portal.Call(portalShortcuts+".ConfigureShortcuts", 0, d.currentSession(), "", map[string]dbus.Variant{}).Err
}

// Note says where the member changes the keys when Dens can't open the
// place, for the settings to show.
func (d *DesktopKeys) Note() string {
	if d.configurable {
		return ""
	}
	if d.gnome {
		return "Change them in GNOME's Settings, under Apps, then Dens."
	}
	return "Change them in your desktop's keyboard shortcut settings."
}

// Close ends the session; the desktop keeps the keys for the next one.
func (d *DesktopKeys) Close() error {
	var err error
	d.once.Do(func() {
		if session := d.currentSession(); session != "" {
			_ = d.conn.Object(portalName, session).Call(portalSession+".Close", 0).Err
		}
		err = d.conn.Close()
	})
	return err
}

func (d *DesktopKeys) currentSession() dbus.ObjectPath {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.session
}

type desktopShortcut struct {
	ID   string
	Opts map[string]dbus.Variant
}

// request calls a portal method that answers with a Request's Response,
// and waits for it. The Request's path is known before the call, from the
// connection's name and the token, so no answer can arrive unwatched.
func (d *DesktopKeys) request(ctx context.Context, method string, args ...any) (map[string]dbus.Variant, error) {
	d.mu.Lock()
	d.seq++
	token := fmt.Sprintf("dens%d", d.seq)
	path := dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", portalPath, d.sender, token))
	answer := make(chan *dbus.Signal, 1)
	d.pending[path] = answer
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.pending, path)
		d.mu.Unlock()
	}()
	args[len(args)-1].(map[string]dbus.Variant)["handle_token"] = dbus.MakeVariant(token)
	if err := d.portal.CallWithContext(ctx, portalShortcuts+"."+method, 0, args...).Err; err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.closed:
		return nil, fmt.Errorf("%s: the portal went away", method)
	case s := <-answer:
		code, _ := s.Body[0].(uint32)
		results, _ := s.Body[1].(map[string]dbus.Variant)
		switch code {
		case 0:
			return results, nil
		case 1:
			return nil, fmt.Errorf("%s: %w", method, ErrDesktopCancelled)
		default:
			return nil, fmt.Errorf("%s: the desktop answered %d", method, code)
		}
	}
}

// dispatch routes the bus's signals: answers to requests, and the session's
// presses, changes and end.
func (d *DesktopKeys) dispatch(signals <-chan *dbus.Signal) {
	defer close(d.closed)
	for s := range signals {
		switch s.Name {
		case portalRequest + ".Response":
			d.mu.Lock()
			answer := d.pending[s.Path]
			d.mu.Unlock()
			if answer != nil && len(s.Body) == 2 {
				answer <- s
			}
		case portalShortcuts + ".Activated", portalShortcuts + ".Deactivated":
			if len(s.Body) < 2 || s.Body[0] != d.currentSession() {
				continue
			}
			id, _ := s.Body[1].(string)
			select {
			case d.presses <- DesktopPress{ID: id, Down: s.Name == portalShortcuts+".Activated"}:
			default:
			}
		case portalShortcuts + ".ShortcutsChanged":
			if len(s.Body) < 2 || s.Body[0] != d.currentSession() {
				continue
			}
			if names, err := shortcutNames(dbus.MakeVariant(s.Body[1])); err == nil {
				select {
				case d.changes <- names:
				default:
				}
			}
		case portalSession + ".Closed":
			if s.Path == d.currentSession() {
				// The rest of the session's signals go unread, so the
				// connection goes too.
				go d.Close()
				for range signals {
				}
				return
			}
		}
	}
}

// shortcutNames reads a portal's list of shortcuts into the desktop's
// names for their triggers, by ID.
func shortcutNames(v dbus.Variant) (map[string]string, error) {
	var list []desktopShortcut
	if err := v.Store(&list); err != nil {
		return nil, fmt.Errorf("read the desktop's shortcuts: %w", err)
	}
	names := make(map[string]string, len(list))
	for _, s := range list {
		name := ""
		if t, ok := s.Opts["trigger_description"]; ok {
			name, _ = t.Value().(string)
		}
		names[s.ID] = name
	}
	return names, nil
}
