// Command fprobe answers M9's Flatpak spike from inside the sandbox, on a
// real desktop: how a service that dens launch starts lives under
// bubblewrap, what the Background, Secret and global shortcuts portals do
// for an app with no window, and how the update monitor and Spawn restart
// it into a newer build. Where Dens has a seam already (the control
// endpoint, OpenURL, locked memory, low-priority children, the X11 key
// state, the loopback listeners) it runs that seam, so what works here
// works for Dens.
//
// It logs to stderr and to $XDG_DATA_HOME/spike.log, which outside the
// sandbox is ~/.var/app/chat.dens.Spike/data/spike.log; the VM's
// collect.sh copies it to the shared folder.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

// version names the build, which build.sh sets: v1, v2 or v3.
var version = "dev"

var (
	appID   = "chat.dens.Spike"
	command string
	logFile *os.File
	// instance is Flatpak's ID for this sandbox. Every sandbox numbers its
	// own processes, so the service is pid 2 in each of them.
	instance = "none"
	// quiet keeps a running service's log out of the terminal or journal
	// it started from: it goes to the log file alone.
	quiet bool
)

func logf(format string, args ...any) {
	line := fmt.Sprintf("%s %s instance %s %s: %s\n", time.Now().Format("2006-01-02 15:04:05.000"), version, instance, command, fmt.Sprintf(format, args...))
	if !quiet {
		os.Stderr.WriteString(line)
	}
	if logFile != nil {
		logFile.WriteString(line)
	}
}

func fail(err error) {
	logf("error: %v", err)
	os.Exit(1)
}

// dataDir is where the app keeps its files: $XDG_DATA_HOME, which Flatpak
// points at the app's own directory.
func dataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "fprobe")
}

// runDir is the runtime directory every instance of the app shares, where
// the control socket lives, as Dens's will.
func runDir() string {
	return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "app", appID)
}

const usage = `fprobe COMMAND [FLAGS]

  report               the sandbox, its environment and the portals' versions
  auto                 report, checks, and two secrets, in one go
  launch               what dens launch will do: reach the running service, or become it
  daemon               the service: control socket, page, bus name, update monitor
  ping|open|quit       ask the running service
  status MESSAGE       have the service set its background status line
  update               have the service install the update it found and restart into it
  secret [-count N]    ask the Secret portal for the app's secret
  background           ask to run in the background, and to start at sign-in (-autostart)
  shortcuts            bind the voice actions through the global shortcuts portal
  x11keys              read keys over the X11 socket, as the helper does under X11
  checks               locked memory and a low-priority child, in the sandbox
`

func main() {
	if id := os.Getenv("FLATPAK_ID"); id != "" {
		appID = id
	}
	if info, err := os.ReadFile("/.flatpak-info"); err == nil {
		for _, line := range strings.Split(string(info), "\n") {
			if v, ok := strings.CutPrefix(line, "instance-id="); ok {
				instance = v
			}
		}
	}
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	command = os.Args[1]
	args := os.Args[2:]
	if command == "low-priority-child" {
		time.Sleep(3 * time.Second)
		return
	}
	if err := os.MkdirAll(dataDir(), 0o700); err == nil {
		logFile, _ = os.OpenFile(filepath.Join(dataDir(), "spike.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	var err error
	switch command {
	case "report":
		err = report()
	case "auto":
		err = auto()
	case "launch":
		err = launch(args)
	case "daemon":
		err = runDaemonCommand(args)
	case "ping", "open", "quit", "status", "update":
		err = ask(command, args)
	case "secret":
		err = secretCommand(args)
	case "background":
		err = backgroundCommand(args)
	case "shortcuts":
		err = shortcutsCommand(args)
	case "x11keys":
		err = x11Keys(args)
	case "checks":
		err = checks()
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fail(err)
	}
}

// report says what the sandbox gives the app.
func report() error {
	logf("host: %s; runtime: %s", osName("/run/host/os-release"), osName("/etc/os-release"))
	if info, err := os.ReadFile("/.flatpak-info"); err == nil {
		for _, line := range strings.Split(string(info), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				logf(".flatpak-info %s", line)
			}
		}
	} else {
		logf("no /.flatpak-info: not in a Flatpak sandbox")
	}
	for _, k := range []string{"FLATPAK_ID", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "WAYLAND_DISPLAY", "DISPLAY", "XAUTHORITY",
		"XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "DBUS_SESSION_BUS_ADDRESS", "HOME"} {
		logf("env %s=%q", k, os.Getenv(k))
	}
	_, x11Err := os.Stat("/tmp/.X11-unix")
	logf("uid %d gid %d; /tmp/.X11-unix there: %v", os.Getuid(), os.Getgid(), x11Err == nil)
	if cgroup, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		logf("cgroup %s", strings.TrimSpace(string(cgroup)))
	}
	p, err := openPortal()
	if err != nil {
		return err
	}
	defer p.conn.Close()
	logf("bus passes file descriptors: %v", p.conn.SupportsUnixFDs())
	for _, iface := range []string{backgroundIf, secretIf, shortcutsIf, openURIIf} {
		logf("%s version %s", iface, p.version(p.desktop(), iface))
	}
	logf("%s version %s", flatpakIf, p.version(p.flatpak(), flatpakIf))
	if v, err := p.flatpak().GetProperty(flatpakIf + ".supports"); err == nil {
		logf("%s supports %v", flatpakIf, v.Value())
	}
	return nil
}

func osName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown (" + path + " unreadable)"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return "unknown"
}

// auto runs what needs nobody at the keyboard.
func auto() error {
	if err := report(); err != nil {
		return err
	}
	if err := checks(); err != nil {
		logf("checks: %v", err)
	}
	return secretCommand([]string{"-count", "2"})
}

// checks runs Dens's seams that the sandbox could break.
func checks() error {
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_MEMLOCK, &lim); err == nil {
		logf("RLIMIT_MEMLOCK %d, max %d", lim.Cur, lim.Max)
	}
	for _, n := range []int{4 << 10, 64 << 10, 1 << 20} {
		if _, free, err := host.AllocLocked(n); err != nil {
			logf("locked memory, %d bytes: %v", n, err)
		} else {
			free()
			logf("locked memory, %d bytes: ok", n)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "low-priority-child")
	if err := host.StartLowPriority(cmd); err != nil {
		logf("low-priority child: %v", err)
	} else {
		time.Sleep(200 * time.Millisecond)
		logf("low-priority child: %s", schedOf(cmd.Process.Pid))
		_ = cmd.Wait()
	}
	return nil
}

// schedOf reads a process's nice value and scheduling policy from proc.
func schedOf(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return err.Error()
	}
	// The fields after the command's name, which is in parentheses.
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+2:]))
	if len(fields) < 39 {
		return "unreadable stat"
	}
	policies := map[string]string{"0": "SCHED_OTHER", "3": "SCHED_BATCH", "5": "SCHED_IDLE"}
	return fmt.Sprintf("nice %s, policy %s", fields[16], policies[fields[38]])
}

// secretCommand asks for the app's secret count times, and compares each
// answer with the last one this app kept, across runs and reboots.
func secretCommand(args []string) error {
	fs := flag.NewFlagSet("secret", flag.ExitOnError)
	count := fs.Int("count", 1, "how many times to ask")
	timeout := fs.Duration("timeout", 3*time.Minute, "how long to wait for each answer, as through an unlock prompt")
	_ = fs.Parse(args)
	p, err := openPortal()
	if err != nil {
		return err
	}
	defer p.conn.Close()
	logf("Secret portal version %s", p.version(p.desktop(), secretIf))
	for range *count {
		checkSecret(p, *timeout)
	}
	return nil
}

// checkSecret asks once and logs the answer beside the one before it.
func checkSecret(p *portal, timeout time.Duration) {
	historyPath := filepath.Join(dataDir(), "secrets.txt")
	previous := ""
	if data, err := os.ReadFile(historyPath); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if f := strings.Fields(lines[len(lines)-1]); len(f) > 0 {
			previous = f[len(f)-1]
		}
	}
	s, err := p.retrieveSecret(timeout)
	if err != nil {
		logf("RetrieveSecret failed after %v: %v", s.Took.Round(time.Millisecond), err)
		return
	}
	if s.Code != 0 {
		logf("RetrieveSecret: %s", s.response)
		return
	}
	same := "the first answer kept"
	switch {
	case previous == s.Digest:
		same = "the same as the last answer"
	case previous != "":
		same = "DIFFERENT from the last answer (" + previous + ")"
	}
	logf("RetrieveSecret: %s, %d bytes, digest %s, %s", s.response, s.Bytes, s.Digest, same)
	if f, err := os.OpenFile(historyPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(f, "%s %s %s\n", time.Now().Format(time.RFC3339), version, s.Digest)
		f.Close()
	}
}

// backgroundCommand asks the Background portal to let the app run with no
// window and, with -autostart, to start the service at sign-in.
func backgroundCommand(args []string) error {
	fs := flag.NewFlagSet("background", flag.ExitOnError)
	autostart := fs.Bool("autostart", false, "start the service at sign-in")
	reason := fs.String("reason", "Dens keeps you connected to your dens while its page is closed.", "the reason the request gives")
	_ = fs.Parse(args)
	p, err := openPortal()
	if err != nil {
		return err
	}
	defer p.conn.Close()
	logf("Background portal version %s", p.version(p.desktop(), backgroundIf))
	var commandline []string
	if *autostart {
		commandline = []string{"fprobe", "daemon", "-autostart", "-secret"}
	}
	r, err := p.requestBackground(*reason, *autostart, commandline)
	if err != nil {
		return err
	}
	logf("RequestBackground(autostart %v): %s, results %v", *autostart, r, values(r.Results))
	logf("outside the sandbox, the autostart entry is ~/.config/autostart/%s.desktop", appID)
	return nil
}

func values(m map[string]dbus.Variant) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v.Value()))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// x11Keys reads keys over the X11 socket with Dens's own reader, which
// works only where the session is X11 and the sandbox has its socket.
func x11Keys(args []string) error {
	fs := flag.NewFlagSet("x11keys", flag.ExitOnError)
	seconds := fs.Int("seconds", 20, "how long to watch")
	_ = fs.Parse(args)
	keys, err := host.OpenKeyState()
	if err != nil {
		return fmt.Errorf("Dens's X11 key reader: %w", err)
	}
	defer keys.Close()
	codes := []string{"F8", "F9", "F10", "KeyV", "ControlLeft"}
	logf("X11 key reader open; press %s with another window in front for %d seconds", strings.Join(codes, ", "), *seconds)
	down := map[string]bool{}
	deadline := time.Now().Add(time.Duration(*seconds) * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range codes {
			k, ok := keys.Key(c)
			if !ok {
				continue
			}
			if d := keys.Down(k); d != down[c] {
				down[c] = d
				logf("%s %s", c, map[bool]string{true: "down", false: "up"}[d])
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// shortcutsCommand binds the voice actions through the global shortcuts
// portal from inside the sandbox, which needs no registry call: the
// portal knows the app by its sandbox.
func shortcutsCommand(args []string) error {
	fs := flag.NewFlagSet("shortcuts", flag.ExitOnError)
	configure := fs.Bool("configure", false, "after binding, ask the desktop to show its settings for them (version 2)")
	triggers := fs.String("triggers", "F8,F9,F10", "preferred triggers for push to talk, toggle mute and push to mute")
	_ = fs.Parse(args)
	p, err := openPortal()
	if err != nil {
		return err
	}
	defer p.conn.Close()
	logf("WAYLAND_DISPLAY=%q DISPLAY=%q XDG_SESSION_TYPE=%q", os.Getenv("WAYLAND_DISPLAY"), os.Getenv("DISPLAY"), os.Getenv("XDG_SESSION_TYPE"))
	logf("GlobalShortcuts version %s", p.version(p.desktop(), shortcutsIf))
	r, err := p.request(p.desktop(), shortcutsIf+".CreateSession", time.Minute,
		map[string]dbus.Variant{"session_handle_token": dbus.MakeVariant(fmt.Sprintf("spike%d", os.Getpid()))})
	if err != nil {
		return err
	}
	logf("CreateSession: %s", r)
	var session dbus.ObjectPath
	switch v := r.Results["session_handle"].Value().(type) {
	case string:
		session = dbus.ObjectPath(v)
	case dbus.ObjectPath:
		session = v
	}
	if r, err := p.request(p.desktop(), shortcutsIf+".ListShortcuts", time.Minute, session, map[string]dbus.Variant{}); err == nil {
		logf("ListShortcuts: %s, %s", r, shortcutList(r.Results))
	} else {
		logf("ListShortcuts: %v", err)
	}
	type shortcut struct {
		ID   string
		Opts map[string]dbus.Variant
	}
	ids := []string{"ptt", "toggleMute", "pushMute"}
	names := []string{"Push to talk", "Toggle mute", "Push to mute"}
	prefer := strings.Split(*triggers, ",")
	var bind []shortcut
	for i, id := range ids {
		opts := map[string]dbus.Variant{"description": dbus.MakeVariant(names[i])}
		if i < len(prefer) && strings.TrimSpace(prefer[i]) != "" {
			opts["preferred_trigger"] = dbus.MakeVariant(strings.TrimSpace(prefer[i]))
		}
		bind = append(bind, shortcut{ID: id, Opts: opts})
	}
	logf("binding; answer the desktop's dialog if it shows one")
	r, err = p.request(p.desktop(), shortcutsIf+".BindShortcuts", 5*time.Minute, session, bind, "", map[string]dbus.Variant{})
	if err != nil {
		return err
	}
	logf("BindShortcuts: %s, %s", r, shortcutList(r.Results))
	if *configure {
		if err := p.desktop().Call(shortcutsIf+".ConfigureShortcuts", 0, session, "", map[string]dbus.Variant{}).Err; err != nil {
			logf("ConfigureShortcuts: %v", err)
		} else {
			logf("ConfigureShortcuts: asked the desktop to show its settings")
		}
	}
	logf("press the shortcuts with another window in front; Ctrl+C here stops")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, unix.SIGTERM)
	downAt := map[string]time.Time{}
	for {
		select {
		case <-stop:
			_ = p.conn.Object(desktopName, session).Call("org.freedesktop.portal.Session.Close", 0).Err
			logf("stopped")
			return nil
		case s, ok := <-p.Other:
			if !ok {
				return errors.New("the bus closed")
			}
			switch s.Name {
			case shortcutsIf + ".Activated":
				id, _ := s.Body[1].(string)
				downAt[id] = time.Now()
				logf("%s down (desktop timestamp %v)", id, s.Body[2])
			case shortcutsIf + ".Deactivated":
				id, _ := s.Body[1].(string)
				logf("%s up (desktop timestamp %v), held %v", id, s.Body[2], time.Since(downAt[id]).Round(time.Millisecond))
			default:
				logf("signal %s", describe(s))
			}
		}
	}
}

func shortcutList(results map[string]dbus.Variant) string {
	v, ok := results["shortcuts"]
	if !ok {
		return "no shortcuts in the answer"
	}
	var list []struct {
		ID   string
		Opts map[string]dbus.Variant
	}
	if err := v.Store(&list); err != nil {
		return err.Error()
	}
	if len(list) == 0 {
		return "none"
	}
	var parts []string
	for _, s := range list {
		trigger := ""
		if t, ok := s.Opts["trigger_description"]; ok {
			trigger, _ = t.Value().(string)
		}
		parts = append(parts, fmt.Sprintf("%s=%q", s.ID, trigger))
	}
	return strings.Join(parts, " ")
}
