package host

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// The desktop's notifications (M12.3): the helper shows the member's
// through the freedesktop notification service, and the Flatpak's service,
// in its sandbox, through the desktop's Notification portal, which knows
// the app by its Flatpak ID.
const (
	notificationsName  = "org.freedesktop.Notifications"
	notificationsPath  = "/org/freedesktop/Notifications"
	notificationsIface = "org.freedesktop.Notifications"
	// The action a click on a notification's body invokes, by the
	// freedesktop service's name for it, and the portal's: one that isn't
	// "app."-prefixed comes back to the app as a signal, rather than
	// through its own D-Bus actions.
	defaultAction = "default"
	portalOpen    = "open"
)

// Notifier shows notifications as the desktop's own, and hands back the
// ID of each the member clicks.
type Notifier struct {
	conn   *dbus.Conn
	portal *portalConn
	appID  string
	// markup is a freedesktop service that reads markup in a body, where
	// a member's text must be escaped.
	markup bool
	clicks chan string

	mu sync.Mutex
	// ids are the freedesktop service's IDs for the notifications showing,
	// by the helper's, and names the other way.
	ids   map[string]uint32
	names map[uint32]string
}

// OpenNotifier connects to the desktop's notifications, as appID: the
// freedesktop notification service, or from a sandbox the Notification
// portal. It fails with errors.ErrUnsupported where there's neither.
func OpenNotifier(appID string, sandboxed bool) (*Notifier, error) {
	n := &Notifier{appID: appID, clicks: make(chan string, 16), ids: map[string]uint32{}, names: map[uint32]string{}}
	if sandboxed {
		p, err := dialPortal(n.portalSignal, []dbus.MatchOption{dbus.WithMatchInterface(portalNotification), dbus.WithMatchMember("ActionInvoked")})
		if err != nil {
			return nil, err
		}
		if _, err := p.conn.Object(portalName, portalPath).GetProperty(portalNotification + ".version"); err != nil {
			p.Close()
			return nil, fmt.Errorf("the desktop has no Notification portal: %w", errors.ErrUnsupported)
		}
		n.portal = p
		return n, nil
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
	// Asking what it can do starts a service the bus starts on demand.
	var caps []string
	if err := conn.Object(notificationsName, notificationsPath).Call(notificationsIface+".GetCapabilities", 0).Store(&caps); err != nil {
		conn.Close()
		return nil, fmt.Errorf("the desktop has no notification service: %v: %w", err, errors.ErrUnsupported)
	}
	n.conn, n.markup = conn, slices.Contains(caps, "body-markup")
	if err := conn.AddMatchSignal(dbus.WithMatchInterface(notificationsIface)); err != nil {
		conn.Close()
		return nil, err
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	go n.dispatch(signals)
	return n, nil
}

func (n *Notifier) dispatch(signals <-chan *dbus.Signal) {
	for s := range signals {
		if len(s.Body) < 2 {
			continue
		}
		id, _ := s.Body[0].(uint32)
		n.mu.Lock()
		name, ok := n.names[id]
		if ok && s.Name == notificationsIface+".NotificationClosed" {
			delete(n.names, id)
			delete(n.ids, name)
		}
		n.mu.Unlock()
		if action, _ := s.Body[1].(string); ok && s.Name == notificationsIface+".ActionInvoked" && action == defaultAction {
			n.click(name)
		}
	}
}

// portalSignal takes the portal's word of a click.
func (n *Notifier) portalSignal(s *dbus.Signal) {
	if s.Name != portalNotification+".ActionInvoked" || len(s.Body) < 2 {
		return
	}
	id, _ := s.Body[0].(string)
	if action, _ := s.Body[1].(string); action == portalOpen {
		n.click(id)
	}
}

func (n *Notifier) click(id string) {
	select {
	case n.clicks <- id:
	default:
	}
}

// Show shows a notification under id, replacing what shows under it.
func (n *Notifier) Show(id string, d DesktopNotification) error {
	if n.portal != nil {
		return n.portal.conn.Object(portalName, portalPath).Call(portalNotification+".AddNotification", 0, id, map[string]dbus.Variant{
			"title":          dbus.MakeVariant(d.Title),
			"body":           dbus.MakeVariant(d.Body),
			"priority":       dbus.MakeVariant("high"),
			"default-action": dbus.MakeVariant(portalOpen),
		}).Err
	}
	body := d.Body
	if n.markup {
		body = escapeMarkup(body)
	}
	n.mu.Lock()
	replaces := n.ids[id]
	n.mu.Unlock()
	hints := map[string]dbus.Variant{
		"desktop-entry": dbus.MakeVariant(n.appID),
		"category":      dbus.MakeVariant("im.received"),
		"urgency":       dbus.MakeVariant(byte(1)),
	}
	var shown uint32
	if err := n.conn.Object(notificationsName, notificationsPath).Call(notificationsIface+".Notify", 0,
		"Dens", replaces, n.appID, d.Title, body, []string{defaultAction, "Open"}, hints, int32(-1)).Store(&shown); err != nil {
		return err
	}
	n.mu.Lock()
	if replaces != 0 && replaces != shown {
		delete(n.names, replaces)
	}
	n.ids[id], n.names[shown] = shown, id
	n.mu.Unlock()
	return nil
}

// Withdraw takes back what shows under id.
func (n *Notifier) Withdraw(id string) error {
	if n.portal != nil {
		return n.portal.conn.Object(portalName, portalPath).Call(portalNotification+".RemoveNotification", 0, id).Err
	}
	n.mu.Lock()
	shown, ok := n.ids[id]
	delete(n.ids, id)
	delete(n.names, shown)
	n.mu.Unlock()
	if !ok {
		return nil
	}
	return n.conn.Object(notificationsName, notificationsPath).Call(notificationsIface+".CloseNotification", 0, shown).Err
}

// Clicks delivers the ID of each notification the member clicks.
func (n *Notifier) Clicks() <-chan string { return n.clicks }

// Close lets go of the desktop's notifications; what shows stays.
func (n *Notifier) Close() error {
	if n.portal != nil {
		return n.portal.Close()
	}
	return n.conn.Close()
}

// escapeMarkup is text as a service that reads markup shows it as written.
func escapeMarkup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}
