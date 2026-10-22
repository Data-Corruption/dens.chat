package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/server"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

// pagePort is the Flatpak's own client port, apart from an installed
// Dens's 8484 (M9's choices).
const pagePort = 8494

var pageURL = fmt.Sprintf("http://localhost:%d/", pagePort)

var errBusy = errors.New("another instance holds the service's lock")

func socketPath() string { return filepath.Join(runDir(), "control.sock") }

// lockService takes the lock the running service holds for its life, in
// the runtime directory every instance of the app shares.
func lockService() (*os.File, error) {
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(runDir(), "service.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errBusy
		}
		return nil, err
	}
	return f, nil
}

// client reaches the running service over Dens's own control endpoint,
// which checks that the service runs as this user.
func client() (control.Client, error) {
	me, err := host.CurrentUser()
	if err != nil {
		return control.Client{}, err
	}
	return control.Client{Endpoint: socketPath(), Server: host.ControlServer{Account: me}, Timeout: 10 * time.Second}, nil
}

type pong struct {
	Version  string `json:"version"`
	Instance string `json:"instance"`
	Started  string `json:"started"`
	How      string `json:"how"`
	// Update is how the last update this service tried ended, if it did.
	Update string `json:"update,omitempty"`
}

func ping() (pong, error) {
	c, err := client()
	if err != nil {
		return pong{}, err
	}
	var p pong
	err = c.Call(control.Request{Op: "ping"}, &p)
	return p, err
}

// launch is what dens launch will do from the desktop file: open the page
// of the service that runs, or become the service and open it once ready.
// A launch that loses the race to start the service waits for the one that
// won.
func launch(args []string) error {
	fs := flag.NewFlagSet("launch", flag.ExitOnError)
	noOpen := fs.Bool("no-open", false, "don't open the page")
	_ = fs.Parse(args)
	started := time.Now()
	if p, err := ping(); err == nil {
		logf("reached the running service (%s, instance %s, started %s %s) in %v", p.Version, p.Instance, p.How, p.Started, time.Since(started).Round(time.Millisecond))
		if !*noOpen {
			return ask("open", nil)
		}
		return nil
	}
	lock, err := lockService()
	if err == nil {
		logf("no service answered; this launch becomes it")
		return serve(lock, daemonOptions{how: "from launch", open: !*noOpen})
	}
	if !errors.Is(err, errBusy) {
		return err
	}
	logf("another launch is starting the service; waiting for it")
	for time.Since(started) < 30*time.Second {
		time.Sleep(100 * time.Millisecond)
		if p, err := ping(); err == nil {
			logf("reached the service another launch started (%s, instance %s) after %v", p.Version, p.Instance, time.Since(started).Round(time.Millisecond))
			if !*noOpen {
				return ask("open", nil)
			}
			return nil
		}
	}
	return errors.New("the service another launch started never answered")
}

func runDaemonCommand(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	autostart := fs.Bool("autostart", false, "started at sign-in, from the autostart entry")
	secret := fs.Bool("secret", false, "ask for the app's secret twice as it starts, timing each")
	open := fs.Bool("open", false, "open the page once ready")
	restarted := fs.Bool("restarted", false, "started by an update, through Spawn")
	_ = fs.Parse(args)
	lock, err := lockService()
	if errors.Is(err, errBusy) {
		logf("a service already runs; this one stops")
		return nil
	}
	if err != nil {
		return err
	}
	how := "by hand"
	switch {
	case *autostart:
		how = "at sign-in"
	case *restarted:
		how = "by an update"
	}
	return serve(lock, daemonOptions{how: how, open: *open, secret: *secret})
}

// ask has the running service do something.
func ask(op string, args []string) error {
	c, err := client()
	if err != nil {
		return err
	}
	req := control.Request{Op: op}
	if op == "status" {
		// The control protocol has no field for a message; the spike's
		// status rides in the password's.
		req.Password = strings.Join(args, " ")
	}
	before, _ := ping()
	var result map[string]any
	if err := c.Call(req, &result); err != nil {
		return fmt.Errorf("the service: %w", err)
	}
	logf("%s: the service answered %v", op, result)
	if op != "update" {
		return nil
	}
	logf("waiting for the service to restart into the update")
	started := time.Now()
	for time.Since(started) < 5*time.Minute {
		time.Sleep(time.Second)
		p, err := ping()
		if err == nil && p.Instance != before.Instance {
			logf("the service restarted: %s (was %s), instance %s, after %v", p.Version, before.Version, p.Instance, time.Since(started).Round(time.Second))
			return nil
		}
		if err == nil && p.Update != "" {
			return fmt.Errorf("the update didn't install: %s", p.Update)
		}
	}
	return errors.New("the service didn't come back within 5 minutes; see the log")
}

type daemonOptions struct {
	how    string // how it started
	open   bool
	secret bool
}

// service is the spike's stand-in for Dens's service in the Flatpak form.
type service struct {
	opts    daemonOptions
	started time.Time
	p       *portal
	lock    *os.File
	monitor dbus.ObjectPath

	stop     chan string
	release  sync.Once
	cancel   context.CancelFunc
	wg       sync.WaitGroup // the control endpoint and the page
	mu       sync.Mutex
	opens    int
	updating bool
	status   string
	failed   string // why the last update failed
	// installed is whether the last UpdateAvailable found the update on
	// disk already, as a software center that updates by itself leaves it,
	// so a restart is all it takes.
	installed bool
}

func serve(lock *os.File, opts daemonOptions) error {
	s := &service{opts: opts, started: time.Now(), lock: lock, stop: make(chan string, 4)}
	logf("the service starts %s; parent %s", opts.how, parentOf(os.Getppid()))
	if info, err := os.ReadFile("/.flatpak-info"); err == nil {
		for _, line := range strings.Split(string(info), "\n") {
			if strings.HasPrefix(line, "instance-id=") || strings.HasPrefix(line, "app-commit=") {
				logf("%s", line)
			}
		}
	}
	var err error
	if s.p, err = openPortal(); err != nil {
		return err
	}
	defer s.p.conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	defer s.shutdown()

	// The control endpoint, as Dens's: the same socket and checks.
	me, err := host.CurrentUser()
	if err != nil {
		return err
	}
	ln, err := host.ListenControl(socketPath(), me)
	if err != nil {
		return fmt.Errorf("control endpoint: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir(), "control-logs"), 0o700); err != nil {
		return err
	}
	logs, err := xlog.New(filepath.Join(dataDir(), "control-logs"), "debug")
	if err != nil {
		return err
	}
	defer logs.Close()
	cs := &control.Server{Allowed: me, Handlers: s.handlers(), Log: logs, WriteTimeout: time.Minute}
	logf("control endpoint at %s", socketPath())

	// The page, on loopback, where the member's browser outside the
	// sandbox must reach it. A build that an update just started may find
	// the port still held by the one it replaces, for a moment.
	var lns []net.Listener
	for try := 0; ; try++ {
		if lns, err = server.ListenLoopback(pagePort); err == nil || try == 50 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("page: %w", err)
	}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		_ = cs.Serve(ctx, ln)
	}()
	go func() {
		defer s.wg.Done()
		_ = server.Serve(ctx, http.HandlerFunc(s.page), log.New(os.Stderr, "http: ", 0), lns...)
	}()
	logf("the page at %s", pageURL)

	s.ownName()
	if cgroup, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		logf("cgroup %s", strings.TrimSpace(string(cgroup)))
	}
	logf("the service runs; from here its log goes only to %s", filepath.Join(dataDir(), "spike.log"))
	quiet = true

	if s.monitor, err = s.p.createUpdateMonitor(); err != nil {
		logf("CreateUpdateMonitor: %v", err)
	} else {
		logf("update monitor at %s", s.monitor)
	}
	if opts.secret {
		go func() {
			// At sign-in the portal may still be starting its backends,
			// and answers that it has no Secret interface until it has.
			started := time.Now()
			for {
				v, err := s.p.desktop().GetProperty(secretIf + ".version")
				if err == nil {
					logf("the Secret portal (version %v) was there %v after the service started", v.Value(), time.Since(started).Round(time.Millisecond))
					break
				}
				if time.Since(started) > time.Minute {
					logf("the Secret portal wasn't there a minute after the service started: %v", err)
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			checkSecret(s.p, 3*time.Minute)
			checkSecret(s.p, 3*time.Minute)
		}()
	}
	if opts.open {
		if err := host.OpenURL(pageURL); err != nil {
			logf("OpenURL: %v", err)
		} else {
			logf("asked the desktop to open the page")
		}
	}

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, unix.SIGTERM, unix.SIGINT, unix.SIGHUP)
	heartbeat := time.NewTicker(time.Minute)
	defer heartbeat.Stop()
	var reason string
	for reason == "" {
		select {
		case sig := <-signals:
			reason = "signal " + sig.String()
		case reason = <-s.stop:
		case <-heartbeat.C:
			logf("alive for %v", time.Since(s.started).Round(time.Second))
		case sig, ok := <-s.p.Other:
			if !ok {
				reason = "the bus closed"
				break
			}
			s.signal(sig)
		}
	}
	logf("stopping: %s, after %v", reason, time.Since(s.started).Round(time.Second))
	return nil
}

// shutdown lets go of everything another service would need: the socket,
// the page's port, the bus name and the lock.
func (s *service) shutdown() {
	s.release.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		done := make(chan struct{})
		go func() { s.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			logf("the control endpoint or the page took over 5 seconds to stop")
		}
		if s.p != nil {
			_, _ = s.p.conn.ReleaseName(appID)
		}
		_ = os.Remove(socketPath())
		_ = s.lock.Close()
	})
}

func (s *service) handlers() map[string]control.Handler {
	reply := func(v any) (control.Reply, error) { return control.Reply{Result: v}, nil }
	return map[string]control.Handler{
		"ping": func(context.Context, control.Request) (control.Reply, error) {
			s.mu.Lock()
			failed := s.failed
			s.mu.Unlock()
			return reply(pong{Version: version, Instance: instance, Started: s.started.Format(time.RFC3339), How: s.opts.how, Update: failed})
		},
		"open": func(context.Context, control.Request) (control.Reply, error) {
			s.mu.Lock()
			s.opens++
			s.mu.Unlock()
			if err := host.OpenURL(pageURL); err != nil {
				return control.Reply{}, err
			}
			logf("a launch asked; opened the page")
			return reply(map[string]string{"opened": pageURL})
		},
		"status": func(_ context.Context, req control.Request) (control.Reply, error) {
			if err := s.p.setStatus(req.Password); err != nil {
				logf("SetStatus(%q): %v", req.Password, err)
				return control.Reply{}, err
			}
			s.mu.Lock()
			s.status = req.Password
			s.mu.Unlock()
			logf("SetStatus(%q): ok", req.Password)
			return reply(map[string]string{"status": req.Password})
		},
		"update": func(context.Context, control.Request) (control.Reply, error) {
			if s.monitor == "" {
				return control.Reply{}, errors.New("no update monitor")
			}
			s.mu.Lock()
			s.updating, s.failed = true, ""
			installed := s.installed
			s.mu.Unlock()
			if installed {
				logf("the update is installed already; restarting into it")
				// After this answer is on its way.
				time.AfterFunc(200*time.Millisecond, s.restart)
				return reply(map[string]string{"update": "restarting"})
			}
			if err := s.p.update(s.monitor); err != nil {
				logf("Update: %v", err)
				return control.Reply{}, err
			}
			logf("Update: asked the monitor to install the update")
			return reply(map[string]string{"update": "started"})
		},
		"quit": func(context.Context, control.Request) (control.Reply, error) {
			s.stop <- "asked to quit"
			return reply(map[string]string{"quit": "ok"})
		},
	}
}

// signal handles the update monitor's signals. A finished update restarts
// the service into the new build.
func (s *service) signal(sig *dbus.Signal) {
	if strings.HasPrefix(sig.Name, "org.freedesktop.DBus.") {
		return
	}
	logf("signal %s", describe(sig))
	if sig.Name == monitorIf+".UpdateAvailable" && len(sig.Body) > 0 {
		info, _ := sig.Body[0].(map[string]dbus.Variant)
		running, _ := info["running-commit"].Value().(string)
		local, _ := info["local-commit"].Value().(string)
		s.mu.Lock()
		s.installed = local != "" && local != running
		s.mu.Unlock()
		return
	}
	if sig.Name != monitorIf+".Progress" || len(sig.Body) == 0 {
		return
	}
	info, _ := sig.Body[0].(map[string]dbus.Variant)
	status, _ := info["status"].Value().(uint32)
	switch status {
	case 1:
		logf("Progress: there was nothing to update")
		s.mu.Lock()
		s.updating, s.failed = false, "there was nothing to update"
		s.mu.Unlock()
	case 2:
		s.mu.Lock()
		updating := s.updating
		s.mu.Unlock()
		if updating {
			s.restart()
		}
	case 3:
		logf("Progress: the update failed: %v %v", info["error"], info["error_message"])
		s.mu.Lock()
		s.updating, s.failed = false, fmt.Sprint(info["error_message"].Value())
		s.mu.Unlock()
	}
}

// restart lets go of what the new build needs, then has the Flatpak portal
// start the newest installed build, and stops.
func (s *service) restart() {
	logf("the update is installed; restarting into it")
	s.shutdown()
	pid, err := s.p.spawnLatest([]string{"fprobe", "daemon", "-restarted"})
	if err != nil {
		logf("Spawn: %v", err)
		s.stop <- "the restart failed"
		return
	}
	logf("Spawn started the newest build as pid %d, as the Flatpak portal numbers it", pid)
	s.stop <- "restarted into the update"
}

// ownName takes the app ID on the session bus, and answers the quit action
// GNOME's Background Apps menu sends, through either interface.
func (s *service) ownName() {
	reply, err := s.p.conn.RequestName(appID, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		logf("RequestName(%s): reply %v, %v", appID, reply, err)
		return
	}
	path := dbus.ObjectPath("/" + strings.ReplaceAll(appID, ".", "/"))
	quit := func(via string) { s.stop <- "quit through " + via }
	if err := s.p.conn.Export(application{quit}, path, "org.freedesktop.Application"); err != nil {
		logf("export org.freedesktop.Application: %v", err)
	}
	if err := s.p.conn.Export(gtkActions{quit}, path, "org.gtk.Actions"); err != nil {
		logf("export org.gtk.Actions: %v", err)
	}
	logf("owns %s on the session bus, with the quit action at %s", appID, path)
}

type application struct{ quit func(string) }

func (a application) Activate(platform map[string]dbus.Variant) *dbus.Error {
	logf("org.freedesktop.Application.Activate %v", values(platform))
	return nil
}

func (a application) Open(uris []string, platform map[string]dbus.Variant) *dbus.Error {
	logf("org.freedesktop.Application.Open %v", uris)
	return nil
}

func (a application) ActivateAction(name string, params []dbus.Variant, platform map[string]dbus.Variant) *dbus.Error {
	logf("org.freedesktop.Application.ActivateAction %q %v", name, params)
	if name == "quit" {
		a.quit("org.freedesktop.Application")
	}
	return nil
}

type actionDescription struct {
	Enabled   bool
	ParamType dbus.Signature
	State     []dbus.Variant
}

type gtkActions struct{ quit func(string) }

func (gtkActions) List() ([]string, *dbus.Error) {
	logf("org.gtk.Actions.List")
	return []string{"quit"}, nil
}

func (gtkActions) Describe(name string) (actionDescription, *dbus.Error) {
	logf("org.gtk.Actions.Describe %q", name)
	return actionDescription{Enabled: true, State: []dbus.Variant{}}, nil
}

func (gtkActions) DescribeAll() (map[string]actionDescription, *dbus.Error) {
	logf("org.gtk.Actions.DescribeAll")
	return map[string]actionDescription{"quit": {Enabled: true, State: []dbus.Variant{}}}, nil
}

func (g gtkActions) Activate(name string, params []dbus.Variant, platform map[string]dbus.Variant) *dbus.Error {
	logf("org.gtk.Actions.Activate %q", name)
	if name == "quit" {
		g.quit("org.gtk.Actions")
	}
	return nil
}

func (gtkActions) SetState(name string, value dbus.Variant, platform map[string]dbus.Variant) *dbus.Error {
	logf("org.gtk.Actions.SetState %q", name)
	return nil
}

// page shows which build serves, and refreshes, so an update shows in the
// browser that's open.
func (s *service) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	opens, status := s.opens, s.status
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta http-equiv="refresh" content="5"><title>Dens spike %s</title>
<body style="font: 16px system-ui; margin: 2em">
<h1>Dens spike %s</h1>
<p>Instance %s, started %s, %v ago. Launches since: %d. Status line: %q.</p>
<p>Served from inside the sandbox on loopback port %d. The page refreshes every 5 seconds.</p>
</body>`, html.EscapeString(version), html.EscapeString(version), html.EscapeString(instance), html.EscapeString(s.opts.how),
		time.Since(s.started).Round(time.Second), opens, status, pagePort)
}

// parentOf names a process and its parent, for where the service started.
func parentOf(pid int) string {
	comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	return fmt.Sprintf("%d (%s)", pid, strings.TrimSpace(string(comm)))
}
