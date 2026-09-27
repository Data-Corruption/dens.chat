// Package server runs an HTTP handler on the service's listeners.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

// ListenLoopback listens on port on both loopback addresses. With only
// IPv4 bound, another local user could bind [::1] on the same port and catch
// browsers that resolve localhost to IPv6. If IPv6 loopback doesn't exist on
// this host nobody can bind it, so only IPv4 is used.
func ListenLoopback(port int) ([]net.Listener, error) {
	v4, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	v6, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)))
	if err == nil {
		return []net.Listener{v4, v6}, nil
	}
	if probe, probeErr := net.Listen("tcp6", "[::1]:0"); probeErr == nil {
		// IPv6 loopback works, so something else holds this port on it.
		_ = probe.Close()
		_ = v4.Close()
		return nil, fmt.Errorf("[::1]:%d is taken: %w", port, err)
	}
	return []net.Listener{v4}, nil
}

// Serve serves handler on every listener until ctx is cancelled or a
// listener fails, then shuts down gracefully. It closes the listeners.
func Serve(ctx context.Context, handler http.Handler, errorLog *log.Logger, listeners ...net.Listener) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          errorLog,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	results := make(chan error, len(listeners))
	for _, ln := range listeners {
		go func() { results <- srv.Serve(ln) }()
	}

	var failure error
	select {
	case <-ctx.Done():
	case err := <-results:
		failure = fmt.Errorf("listener stopped: %w", err)
		results <- nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)
	for range listeners {
		if err := <-results; err != nil && !errors.Is(err, http.ErrServerClosed) && failure == nil {
			failure = err
		}
	}
	if failure != nil {
		return failure
	}
	return shutdownErr
}
