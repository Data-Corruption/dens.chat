package main

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

// As a Windows service, the probe runs where Dens's does: in session 0,
// under a service account, with no desktop to show a firewall prompt on.

type probeService struct {
	ctx context.Context
	o   options
}

func (p probeService) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(p.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, p.o)
	}()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case <-done:
			cancel()
			return false, 0
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		}
	}
}

func runService(ctx context.Context, o options) {
	quiet = true
	if err := svc.Run(o.service, probeService{ctx, o}); err != nil {
		logf("probe", "service: %v", err)
	}
}

// winSession names the Windows session the probe is in: 0 is the services'.
func winSession() string {
	var id uint32
	if windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &id) != nil {
		return ""
	}
	return fmt.Sprintf("session=%d", id)
}
