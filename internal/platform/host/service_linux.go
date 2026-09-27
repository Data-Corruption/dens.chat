package host

import (
	"context"
	"fmt"
	"sync"

	"github.com/Data-Corruption/dens.chat/pkg/sdnotify"
)

// RunService runs fn as the service body under systemd's Type=notify: READY=1
// goes out when fn calls ready, and STOPPING=1 when fn returns. ctx is the
// process signal context, so the SIGTERM systemd sends on stop cancels it.
// Outside systemd the notifications are no-ops.
func RunService(ctx context.Context, _ string, fn RunFunc) error {
	var once sync.Once
	ready := func() {
		once.Do(func() { _ = sdnotify.Ready("Running") })
	}
	err := fn(ctx, ready)
	_ = sdnotify.Stopping("Stopping")
	return err
}

// AdminCommand phrases command the way an administrator runs it here.
func AdminCommand(command string) string { return "sudo " + command }

// ServiceLogHint says where to find out why a service stopped: systemd's
// journal has its error output, and its own log has the rest.
func ServiceLogHint(service, logDir string) string {
	return fmt.Sprintf("journalctl -u %s, or its log in %s", service, logDir)
}
