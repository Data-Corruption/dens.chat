package host

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sys/windows/svc"
)

// RunService runs fn under the SCM when the process was started as a
// service, and directly otherwise (a development run from a console). The
// SCM sees RUNNING once fn calls ready; a stop or shutdown request cancels
// fn's context. An error ends the service with a service-specific exit code
// (ExitRefused for ErrRefused), which the SCM treats as a non-crash failure.
func RunService(ctx context.Context, name string, fn RunFunc) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return fn(ctx, func() {})
	}
	h := &serviceHandler{ctx: ctx, fn: fn}
	if err := svc.Run(name, h); err != nil {
		return errors.Join(err, h.err)
	}
	return h.err
}

type serviceHandler struct {
	ctx context.Context
	fn  RunFunc
	err error
}

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
				return true, ExitRefused
			default:
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

// AdminCommand phrases command the way an administrator runs it here.
func AdminCommand(command string) string { return command + "   (in an elevated terminal)" }
