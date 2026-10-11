// Command fake stands in for the desktop's portal on a private session bus,
// so the spike's calls and signals can be checked without a desktop: it
// answers each request as a portal would, and a second after binding sends
// a press and release of push to talk.
//
//	dbus-run-session -- sh -c 'fake & sleep 0.5; timeout -s INT 4 dens-portal-spike -log /tmp/portal.log -no-desktop-file'
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const portalPath = "/org/freedesktop/portal/desktop"

type portal struct {
	conn    *dbus.Conn
	session dbus.ObjectPath
}

type shortcut struct {
	ID   string
	Opts map[string]dbus.Variant
}

func (p *portal) request(sender dbus.Sender, opts map[string]dbus.Variant, results map[string]dbus.Variant) dbus.ObjectPath {
	token, _ := opts["handle_token"].Value().(string)
	path := dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", portalPath, strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_"), token))
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = p.conn.Emit(path, "org.freedesktop.portal.Request.Response", uint32(0), results)
	}()
	return path
}

func (p *portal) CreateSession(sender dbus.Sender, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	token, _ := opts["session_handle_token"].Value().(string)
	p.session = dbus.ObjectPath(fmt.Sprintf("%s/session/%s/%s", portalPath, strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_"), token))
	return p.request(sender, opts, map[string]dbus.Variant{"session_handle": dbus.MakeVariant(string(p.session))}), nil
}

func (p *portal) ListShortcuts(sender dbus.Sender, session dbus.ObjectPath, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return p.request(sender, opts, map[string]dbus.Variant{"shortcuts": dbus.MakeVariant([]shortcut{})}), nil
}

func (p *portal) BindShortcuts(sender dbus.Sender, session dbus.ObjectPath, shortcuts []shortcut, parent string, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	var bound []shortcut
	for _, s := range shortcuts {
		trigger, _ := s.Opts["preferred_trigger"].Value().(string)
		bound = append(bound, shortcut{ID: s.ID, Opts: map[string]dbus.Variant{
			"description":         s.Opts["description"],
			"trigger_description": dbus.MakeVariant("Press " + trigger),
		}})
	}
	go func() {
		time.Sleep(time.Second)
		_ = p.conn.Emit(portalPath, "org.freedesktop.portal.GlobalShortcuts.Activated", session, "ptt", uint64(1000), map[string]dbus.Variant{})
		time.Sleep(300 * time.Millisecond)
		_ = p.conn.Emit(portalPath, "org.freedesktop.portal.GlobalShortcuts.Deactivated", session, "ptt", uint64(1300), map[string]dbus.Variant{})
	}()
	return p.request(sender, opts, map[string]dbus.Variant{"shortcuts": dbus.MakeVariant(bound)}), nil
}

func (p *portal) ConfigureShortcuts(session dbus.ObjectPath, parent string, opts map[string]dbus.Variant) *dbus.Error {
	return nil
}

type registry struct{}

func (registry) Register(appID string, opts map[string]dbus.Variant) *dbus.Error {
	fmt.Fprintf(os.Stderr, "fake: registered %s\n", appID)
	return nil
}

type properties struct{}

func (properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface == "org.freedesktop.portal.GlobalShortcuts" && name == "version" {
		return dbus.MakeVariant(uint32(2)), nil
	}
	return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("no property %s.%s", iface, name))
}

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	p := &portal{conn: conn}
	for iface, v := range map[string]any{
		"org.freedesktop.portal.GlobalShortcuts": p,
		"org.freedesktop.host.portal.Registry":   registry{},
		"org.freedesktop.DBus.Properties":        properties{},
	} {
		if err := conn.Export(v, portalPath, iface); err != nil {
			panic(err)
		}
	}
	if reply, err := conn.RequestName("org.freedesktop.portal.Desktop", dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		panic(fmt.Sprintf("name: %v %v", reply, err))
	}
	select {}
}
