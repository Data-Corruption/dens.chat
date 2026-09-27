// Package service runs the parts of the service process: the control
// endpoint for the desktop user's CLI, the client listener for their
// browser, the den listener when this instance hosts a den, and the update
// checker.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/client"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/den"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/server"
)

// Run binds every listener, reports ready, and serves until ctx is
// cancelled or a part fails. Binding comes first so that readiness means
// the whole service is reachable.
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
	var denLn net.Listener
	if a.Instance.Den.Enabled {
		denLn, err = net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(a.Instance.Den.Port)))
		if err != nil {
			_ = controlLn.Close()
			for _, ln := range clientLns {
				_ = ln.Close()
			}
			return fmt.Errorf("den listener: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorLog := log.New(logWriter{a}, "", 0)
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	start := func(name string, run func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := run(ctx)
			if ctx.Err() == nil {
				if err == nil {
					err = errors.New("stopped unexpectedly")
				}
				failures <- fmt.Errorf("%s: %w", name, err)
				cancel()
			}
		}()
	}

	controlServer := &control.Server{
		Allowed:      a.Instance.DesktopUser,
		Handlers:     controlHandlers(a),
		Log:          a.Log,
		WriteTimeout: 30 * time.Minute,
	}
	start("control endpoint", func(ctx context.Context) error { return controlServer.Serve(ctx, controlLn) })
	start("client listener", func(ctx context.Context) error {
		return server.Serve(ctx, client.New(a), errorLog, clientLns...)
	})
	if denLn != nil {
		start("den listener", func(ctx context.Context) error {
			return server.Serve(ctx, den.New(a), errorLog, denLn)
		})
	}
	start("update checker", func(ctx context.Context) error { return a.RunUpdateChecker(ctx, func() {}) })

	// A development instance has no installer to publish ready for it.
	if l.Dev && a.Lease.Mode == maintenance.StartMigrate {
		if err := maintenance.MarkDevReady(l, a.BuildInfo().Version); err != nil {
			cancel()
			wg.Wait()
			return err
		}
	}
	a.Log.Infof("Ready: client listener on http://127.0.0.1:%d", a.Instance.ClientPort)
	ready()

	<-ctx.Done()
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
