package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

// RunService runs fn under the SCM when the process was started as a
// service, and directly otherwise (a development run from a console). The
// SCM sees RUNNING once fn calls ready; a stop or shutdown request cancels
// fn's context. An error's message goes to the Application event log under
// the service's name, which the installer registers as an event source.
//
// A refusal (ErrRefused) stops the service with ExitRefused, a
// service-specific exit code, which the SCM treats as a stop rather than a
// failure, so it doesn't start a service the lifecycle state won't let run.
// Any other error ends the process without reporting the stop. The SCM
// takes that for a crash, the only failure its recovery actions answer, and
// restarts the service, as systemd's Restart=on-failure does (M8).
func RunService(ctx context.Context, name string, fn RunFunc) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return fn(ctx, func() {})
	}
	h := &serviceHandler{name: name, ctx: ctx, fn: fn}
	if err := svc.Run(name, h); err != nil {
		return errors.Join(err, h.err)
	}
	return h.err
}

type serviceHandler struct {
	name string
	ctx  context.Context
	fn   RunFunc
	err  error
}

// Tests replace how a failed service records why it stopped, and ends.
var (
	recordFailure = reportFailure
	crash         = func() { os.Exit(1) }
)

func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	// Migrations run before readiness; the hint only tells the SCM to be
	// patient, the installer does its own waiting.
	status <- svc.Status{State: svc.StartPending, WaitHint: 120_000}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()

	readyCh := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- h.fn(ctx, func() { once.Do(func() { close(readyCh) }) })
	}()

	for {
		select {
		case <-readyCh:
			readyCh = nil
			status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
		case err := <-done:
			h.err = err
			switch {
			case err == nil:
				return false, 0
			case errors.Is(err, ErrRefused):
				recordFailure(h.name, err)
				return true, ExitRefused
			default:
				// fn has closed what it opened, so ending here loses nothing.
				recordFailure(h.name, err)
				crash()
				return true, 1
			}
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 15_000}
				cancel()
			}
		}
	}
}

// reportFailure records why the service stopped in the Application event
// log. It reaches an operator even when the service couldn't open its own
// log directory.
func reportFailure(name string, err error) {
	elog, openErr := eventlog.Open(name)
	if openErr != nil {
		return
	}
	defer elog.Close()
	_ = elog.Error(1, err.Error())
}

// AdminCommand phrases command the way an administrator runs it here.
func AdminCommand(command string) string { return command + "   (in an elevated terminal)" }

// ServiceLogHint says where to find out why a service stopped: the
// Application event log has the reason, and its own log has the rest.
func ServiceLogHint(service, logDir string) string {
	return fmt.Sprintf("the Application event log (source %s), or its log in %s", service, logDir)
}
