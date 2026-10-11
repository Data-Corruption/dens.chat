// Command portal answers M7.2's questions on a real Linux desktop: how the
// global shortcuts portal behaves for an unsandboxed app on GNOME and KDE.
// It registers the app ID the helper will use, binds the three voice
// actions with preferred triggers, and logs the desktop's dialog, its
// answer, and every press and release, with the time each was held, until
// Ctrl+C.
//
// It logs to the console and to a file, by default beside the binary when
// it runs from VMware's shared folder, so the results reach the host.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	appID       = "chat.dens.Dens"
	portalName  = "org.freedesktop.portal.Desktop"
	portalPath  = "/org/freedesktop/portal/desktop"
	shortcutsIf = "org.freedesktop.portal.GlobalShortcuts"
	requestIf   = "org.freedesktop.portal.Request"
	registryIf  = "org.freedesktop.host.portal.Registry"
)

type shortcut struct {
	ID   string
	Opts map[string]dbus.Variant
}

var logf func(format string, args ...any)

func main() {
	logPath := flag.String("log", "", "log file (default: portal-<desktop>.log beside the binary)")
	configure := flag.Bool("configure", false, "after binding, ask the desktop to show its settings for the shortcuts (ConfigureShortcuts, version 2)")
	noRegister := flag.Bool("no-register", false, "skip registering the app ID, to see what the portal does without it")
	noDesktopFile := flag.Bool("no-desktop-file", false, "don't write ~/.local/share/applications/"+appID+".desktop")
	triggers := flag.String("triggers", "F8,F9,F10", "preferred triggers for push to talk, toggle mute and push to mute, in the XDG shortcuts format")
	flag.Parse()

	desktop := strings.ToLower(strings.ReplaceAll(os.Getenv("XDG_CURRENT_DESKTOP"), ":", "-"))
	if desktop == "" {
		desktop = "unknown"
	}
	if *logPath == "" {
		exe, _ := os.Executable()
		*logPath = filepath.Join(filepath.Dir(exe), "portal-"+desktop+".log")
	}
	file, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()
	out := io.MultiWriter(os.Stdout, file)
	logf = func(format string, args ...any) {
		fmt.Fprintf(out, "%s  %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	}
	if err := run(*configure, *noRegister, *noDesktopFile, strings.Split(*triggers, ",")); err != nil {
		logf("error: %v", err)
		os.Exit(1)
	}
}

func run(configure, noRegister, noDesktopFile bool, triggers []string) error {
	cgroup, _ := os.ReadFile("/proc/self/cgroup")
	release, _ := os.ReadFile("/etc/os-release")
	logf("start: %s, session %s, desktop %s, WAYLAND_DISPLAY=%q DISPLAY=%q",
		prettyName(string(release)), os.Getenv("XDG_SESSION_TYPE"), os.Getenv("XDG_CURRENT_DESKTOP"), os.Getenv("WAYLAND_DISPLAY"), os.Getenv("DISPLAY"))
	logf("cgroup: %s", strings.TrimSpace(string(cgroup)))

	if !noDesktopFile {
		path, err := writeDesktopFile()
		if err != nil {
			return err
		}
		logf("desktop file: %s", path)
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("session bus: %w", err)
	}
	defer conn.Close()
	portal := conn.Object(portalName, portalPath)

	if noRegister {
		logf("registry: skipped")
	} else if err := portal.Call(registryIf+".Register", 0, appID, map[string]dbus.Variant{}).Err; err != nil {
		logf("registry: Register(%s) failed: %v", appID, err)
	} else {
		logf("registry: registered as %s", appID)
	}

	version, err := portal.GetProperty(shortcutsIf + ".version")
	if err != nil {
		return fmt.Errorf("the portal has no global shortcuts here: %w", err)
	}
	logf("GlobalShortcuts version %v", version.Value())

	if err := conn.AddMatchSignal(dbus.WithMatchInterface(requestIf), dbus.WithMatchMember("Response")); err != nil {
		return err
	}
	if err := conn.AddMatchSignal(dbus.WithMatchInterface(shortcutsIf)); err != nil {
		return err
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	seq := 0
	request := func(method string, args ...any) (map[string]dbus.Variant, error) {
		seq++
		token := fmt.Sprintf("dens%d", seq)
		path := dbus.ObjectPath(fmt.Sprintf("%s/request/%s/%s", portalPath, sender, token))
		opts := args[len(args)-1].(map[string]dbus.Variant)
		opts["handle_token"] = dbus.MakeVariant(token)
		started := time.Now()
		if err := portal.Call(shortcutsIf+"."+method, 0, args...).Err; err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		for s := range signals {
			if s.Path != path || s.Name != requestIf+".Response" {
				report(s)
				continue
			}
			code := s.Body[0].(uint32)
			results := s.Body[1].(map[string]dbus.Variant)
			logf("%s answered after %v: response %d (%s)", method, time.Since(started).Round(time.Millisecond), code, [...]string{"success", "cancelled", "failed"}[min(code, 2)])
			if code != 0 {
				return results, fmt.Errorf("%s: response %d", method, code)
			}
			return results, nil
		}
		return nil, errors.New("the bus closed")
	}

	results, err := request("CreateSession", map[string]dbus.Variant{"session_handle_token": dbus.MakeVariant("dens")})
	if err != nil {
		return err
	}
	var session dbus.ObjectPath
	switch v := results["session_handle"].Value().(type) {
	case string:
		session = dbus.ObjectPath(v)
	case dbus.ObjectPath:
		session = v
	}
	logf("session %s", session)

	if results, err := request("ListShortcuts", session, map[string]dbus.Variant{}); err == nil {
		logShortcuts("already bound", results)
	} else {
		logf("ListShortcuts: %v", err)
	}

	names := []string{"ptt", "toggleMute", "pushMute"}
	descriptions := []string{"Push to talk", "Toggle mute", "Push to mute"}
	var bind []shortcut
	for i, id := range names {
		opts := map[string]dbus.Variant{"description": dbus.MakeVariant(descriptions[i])}
		if i < len(triggers) && strings.TrimSpace(triggers[i]) != "" {
			opts["preferred_trigger"] = dbus.MakeVariant(strings.TrimSpace(triggers[i]))
		}
		bind = append(bind, shortcut{ID: id, Opts: opts})
	}
	logf("binding ptt, toggleMute and pushMute, preferring %s; answer the desktop's dialog if it shows one", strings.Join(triggers, ", "))
	results, err = request("BindShortcuts", session, bind, "", map[string]dbus.Variant{})
	if err != nil {
		return err
	}
	logShortcuts("bound", results)

	if configure {
		if err := portal.Call(shortcutsIf+".ConfigureShortcuts", 0, session, "", map[string]dbus.Variant{}).Err; err != nil {
			logf("ConfigureShortcuts: %v", err)
		} else {
			logf("ConfigureShortcuts: asked the desktop to show its settings")
		}
	}

	logf("press and hold the shortcuts, with any window in front; Ctrl+C here stops")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	for {
		select {
		case <-stop:
			_ = conn.Object(portalName, session).Call("org.freedesktop.portal.Session.Close", 0).Err
			logf("stop")
			return nil
		case s := <-signals:
			report(s)
		}
	}
}

var downAt = map[string]time.Time{}

// report logs the portal's signals: presses, releases and changed shortcuts.
func report(s *dbus.Signal) {
	switch s.Name {
	case shortcutsIf + ".Activated", shortcutsIf + ".Deactivated":
		id, _ := s.Body[1].(string)
		stamp, _ := s.Body[2].(uint64)
		if strings.HasSuffix(s.Name, ".Activated") {
			downAt[id] = time.Now()
			logf("%s down  (desktop timestamp %d)", id, stamp)
		} else {
			held := ""
			if at, ok := downAt[id]; ok {
				held = fmt.Sprintf(", held %v", time.Since(at).Round(time.Millisecond))
			}
			logf("%s up  (desktop timestamp %d%s)", id, stamp, held)
		}
	case shortcutsIf + ".ShortcutsChanged":
		logShortcuts("changed", map[string]dbus.Variant{"shortcuts": dbus.MakeVariant(s.Body[1])})
	}
}

func logShortcuts(what string, results map[string]dbus.Variant) {
	v, ok := results["shortcuts"]
	if !ok {
		logf("%s: no shortcuts in the answer: %v", what, results)
		return
	}
	var list []shortcut
	if err := v.Store(&list); err != nil {
		logf("%s: %v (%v)", what, err, v)
		return
	}
	if len(list) == 0 {
		logf("%s: none", what)
	}
	for _, s := range list {
		desc, trigger := "", ""
		if d, ok := s.Opts["description"]; ok {
			desc, _ = d.Value().(string)
		}
		if t, ok := s.Opts["trigger_description"]; ok {
			trigger, _ = t.Value().(string)
		}
		logf("%s: %s (%s) -> %q", what, s.ID, desc, trigger)
	}
}

// writeDesktopFile installs the .desktop file the portal looks the app ID
// up by, for this user.
func writeDesktopFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	exe, _ := os.Executable()
	path := filepath.Join(dir, appID+".desktop")
	content := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Dens\nComment=Self-hosted chat (portal spike)\nExec=%s\nIcon=audio-input-microphone\nTerminal=true\n", exe)
	return path, os.WriteFile(path, []byte(content), 0o644)
}

func prettyName(release string) string {
	for _, line := range strings.Split(release, "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return "unknown system"
}
