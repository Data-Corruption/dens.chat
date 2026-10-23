// Package service runs the parts of the service process: the control
// endpoint for the desktop user's CLI, the client listener for their
// browser, the den listener while hosting is on, and the update checker.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/client"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/server"
)

// Run binds every listener, reports ready, and serves until ctx is
// cancelled or a part fails. Binding comes first so that readiness means
// the whole service is reachable: with hosting on, the den too, unless it
// can't start, which leaves the rest serving so the page can say why.
func Run(ctx context.Context, a *app.App, ready func()) error {
	l := a.Layout
	controlLn, err := host.ListenControl(l.ControlEndpoint, a.Instance.DesktopUser)
	if err != nil {
		return fmt.Errorf("control endpoint: %w", err)
	}
	clientLns, err := server.ListenLoopback(a.Instance.ClientPort)
	if err != nil {
		_ = controlLn.Close()
		return fmt.Errorf("client listener: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorLog := log.New(logWriter{a}, "", 0)
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	start := func(name string, run func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := run(ctx)
			if ctx.Err() == nil {
				if err == nil {
					err = errors.New("stopped unexpectedly")
				}
				fail(fmt.Errorf("%s: %w", name, err))
			}
		}()
	}

	// A session form's service stops when asked, having no service manager
	// to stop it.
	quit := func(why string) {
		a.Log.Infof("Stopping: %s", why)
		cancel()
	}
	hosting := newHosting(ctx, a, errorLog, fail)
	if err := hosting.start(); err != nil {
		_ = controlLn.Close()
		for _, ln := range clientLns {
			_ = ln.Close()
		}
		return fmt.Errorf("hosting: %w", err)
	}
	a.Hosting = hosting
	if l.Form == layout.Flatpak {
		f := newFlatpak(ctx, a, quit)
		a.Flatpak, a.RememberingChanged = f, f.resync
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.run()
		}()
	}

	// A backup's archive streams for as long as it takes; a client that
	// stops reading for a minute is let go.
	controlServer := &control.Server{
		Allowed:      a.Instance.DesktopUser,
		Handlers:     controlHandlers(a, quit),
		Log:          a.Log,
		WriteTimeout: time.Minute,
	}
	start("control endpoint", func(ctx context.Context) error { return controlServer.Serve(ctx, controlLn) })
	start("client listener", func(ctx context.Context) error {
		return server.Serve(ctx, client.New(a), errorLog, clientLns...)
	})
	start("update checker", func(ctx context.Context) error { return a.RunUpdateChecker(ctx, func() {}) })

	// What needs the data key starts once the instance has it: at once, but
	// for the Flatpak starting locked (M9.3), which its keyring may open
	// while the page offers the local password.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-a.Unlocked():
		case <-ctx.Done():
			return
		}
		start("den connections", a.Dens.Run)
		// A session form has no installer to publish ready for it, and
		// its start isn't done before it has its key.
		if l.Form.Session() && a.Lease.Mode == maintenance.StartMigrate {
			if err := a.MarkSessionReady(); err != nil {
				fail(err)
			}
		}
	}()
	if a.Locked() {
		a.TryKeyring()
	}
	a.Log.Infof("Ready: client listener on loopback port %d, the page at %s", a.Instance.ClientPort, guard.PageOrigin(a.Instance.ClientPort))
	ready()

	<-ctx.Done()
	hosting.close()
	wg.Wait()
	close(failures)
	var joined error
	for err := range failures {
		joined = errors.Join(joined, err)
	}
	if joined == nil {
		a.Log.Info("Service stopped")
	}
	return joined
}

// logWriter sends net/http's internal errors to the service log.
type logWriter struct{ a *app.App }

func (w logWriter) Write(p []byte) (int, error) {
	w.a.Log.Warnf("http: %s", p)
	return len(p), nil
}
