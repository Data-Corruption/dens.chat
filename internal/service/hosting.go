package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/den"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/server"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
	"github.com/Data-Corruption/dens.chat/internal/types"
)

// retryEvery is how often hosting that's on tries again to start a den
// that couldn't as the service started, as when another program held a
// port then.
const retryEvery = 30 * time.Second

// hosting serves the den this instance holds while the switch in the
// settings is on (M9.1). Turning it on binds the den listener and the
// media ports, starts the den and serves it, under a context of its own;
// turning it off cancels that and stops the den as a service stop does,
// keeping everything it holds. It's the App's Hosting.
type hosting struct {
	a        *app.App
	errorLog *log.Logger
	// ctx is the service's, and fail stops the service, as for any of its
	// parts that fails on its own.
	ctx  context.Context
	fail func(error)

	// mu orders turning hosting on and off, the retries, and the
	// service's stop.
	mu     sync.Mutex
	on     bool
	stop   context.CancelFunc // ends serving; nil while not serving
	done   chan struct{}      // closed once the den has stopped
	retry  *time.Timer
	closed bool
	// problem is why the den doesn't serve while hosting is on.
	problem string

	// state is what State reports. It never waits for mu: a stop takes
	// seconds, and every page's view of its dens reads it.
	state atomic.Pointer[app.HostingState]
}

func newHosting(ctx context.Context, a *app.App, errorLog *log.Logger, fail func(error)) *hosting {
	h := &hosting{a: a, errorLog: errorLog, ctx: ctx, fail: fail}
	h.state.Store(&app.HostingState{})
	return h
}

// start hosts as the switch was left. A den that can't start doesn't stop
// the service, which the member needs to see why: the page says, and
// hosting tries again every so often.
func (h *hosting) start() error {
	cfg, err := config.View(h.a.DB)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.on = cfg.Hosting != nil && *cfg.Hosting
	if h.on {
		h.tryLocked()
	}
	h.publishLocked()
	return nil
}

func (h *hosting) State() app.HostingState { return *h.state.Load() }

// Set turns hosting on or off, and keeps the switch. Turning it on fails,
// changing nothing, while the den can't start.
func (h *hosting) Set(_ context.Context, on bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return errors.New("the service is stopping")
	}
	if on {
		if h.stop == nil {
			if err := h.startDenLocked(true); err != nil {
				return err
			}
		}
		h.on = true
		h.cancelRetryLocked()
		h.a.Log.Info("Hosting turned on")
	} else {
		if err := save(h.a, false); err != nil {
			return err
		}
		h.on = false
		h.cancelRetryLocked()
		h.haltLocked()
		h.a.Log.Info("Hosting turned off")
	}
	h.problem = ""
	h.publishLocked()
	h.a.Dens.HostingChanged()
	return nil
}

// tryLocked starts the den, with hosting on, or plans another try.
func (h *hosting) tryLocked() {
	err := h.startDenLocked(false)
	if err == nil {
		if h.problem != "" {
			h.a.Log.Info("The den started")
		}
		h.problem = ""
		return
	}
	// Each try that fails as the last did says nothing new.
	problem := describe(err)
	if problem != h.problem {
		h.a.Log.Warnf("Hosting is on, but the den couldn't start: %v", err)
	}
	h.problem = problem
	h.retry = time.AfterFunc(retryEvery, h.retryNow)
}

func (h *hosting) retryNow() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.retry = nil
	if h.closed || !h.on || h.stop != nil {
		return
	}
	before := h.problem
	h.tryLocked()
	h.publishLocked()
	if h.problem != before {
		h.a.Dens.HostingChanged()
	}
}

// startDenLocked binds the den's ports, starts it and serves it. Keeping
// the switch on comes after the ports are bound and before the den
// serves, so a failure anywhere leaves it as it was.
func (h *hosting) startDenLocked(keep bool) error {
	b, err := bind(h.a.Instance)
	if err != nil {
		return err
	}
	var cfg sfu.Config
	if h.a.Layout.Form == layout.Development {
		// Pion's own messages name addresses, so they reach the log only
		// in development instances.
		cfg.Log = func(s string) { h.a.Log.Debugf("%s", s) }
	}
	if err := h.a.Den.Start(b.udp, b.tcp, cfg); err != nil {
		_ = b.den.Close()
		_ = b.udp.Close()
		_ = b.tcp.Close()
		return fmt.Errorf("start calls: %w", err)
	}
	if keep {
		if err := save(h.a, true); err != nil {
			_ = b.den.Close()
			h.a.Den.Stop(0)
			return err
		}
	}
	ctx, stop := context.WithCancel(h.ctx)
	done := make(chan struct{})
	h.stop, h.done = stop, done
	go func() {
		defer close(done)
		err := server.Serve(ctx, den.New(h.a), h.errorLog, b.den)
		// The listener is closed now, so clients told to reconnect can't
		// reach this process again. Closing the sockets ends their calls,
		// and then the media ports close.
		h.a.Den.Stop(3 * time.Second)
		if ctx.Err() == nil {
			if err == nil {
				err = errors.New("stopped unexpectedly")
			}
			h.fail(fmt.Errorf("den listener: %w", err))
		}
	}()
	return nil
}

// haltLocked stops serving, and waits for the den to stop.
func (h *hosting) haltLocked() {
	if h.stop == nil {
		return
	}
	h.stop()
	<-h.done
	h.stop, h.done = nil, nil
}

func (h *hosting) cancelRetryLocked() {
	if h.retry != nil {
		h.retry.Stop()
		h.retry = nil
	}
}

func (h *hosting) publishLocked() {
	state := app.HostingState{On: h.on, Serving: h.stop != nil}
	if h.on && h.stop == nil {
		state.Problem = h.problem
	}
	h.state.Store(&state)
}

// close stops hosting with the service, and waits for the den to stop.
func (h *hosting) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.cancelRetryLocked()
	h.haltLocked()
}

// save keeps the switch.
func save(a *app.App, on bool) error {
	_, err := config.Update(a.DB, func(c *types.Configuration) error {
		c.Hosting = &on
		return nil
	})
	return err
}

// describe is what the page says of a den that couldn't start.
func describe(err error) string {
	var port *app.PortError
	if errors.As(err, &port) {
		return port.Error()
	}
	return "The den couldn't start: " + err.Error()
}

// bound are the den's sockets, bound and not yet served.
type bound struct {
	den net.Listener
	udp *net.UDPConn
	tcp net.Listener
}

// bind binds the den listener on loopback, and the media ports on every
// interface, IPv4 and IPv6. A port it can't bind names the installer flag
// that moves it.
func bind(cfg instance.Config) (bound, error) {
	var b bound
	var err error
	if b.den, err = net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Den.Port))); err != nil {
		return bound{}, &app.PortError{Port: cfg.Den.Port, Network: "tcp", Flag: "--den-port", Parameter: "-DenPort", Err: err}
	}
	if b.udp, err = net.ListenUDP("udp", &net.UDPAddr{Port: cfg.Den.MediaUDPPort}); err != nil {
		_ = b.den.Close()
		return bound{}, &app.PortError{Port: cfg.Den.MediaUDPPort, Network: "udp", Flag: "--media-udp-port", Parameter: "-MediaUdpPort", Err: err}
	}
	if b.tcp, err = net.Listen("tcp", ":"+strconv.Itoa(cfg.Den.MediaTCPPort)); err != nil {
		_ = b.den.Close()
		_ = b.udp.Close()
		return bound{}, &app.PortError{Port: cfg.Den.MediaTCPPort, Network: "tcp", Flag: "--media-tcp-port", Parameter: "-MediaTcpPort", Err: err}
	}
	return b, nil
}
