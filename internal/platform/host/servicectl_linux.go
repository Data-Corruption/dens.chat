package host

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ServiceStart starts a unit. With Type=notify, systemctl returns once the
// service reports ready, or with an error if it fails first.
func ServiceStart(name string) error {
	return systemctl("start", name)
}

// ServiceStop stops a unit and waits for it to exit. systemd escalates to
// SIGKILL after the unit's TimeoutStopSec, so timeout is only an upper bound
// on waiting for systemctl itself.
func ServiceStop(name string, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- systemctl("stop", name) }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout + 30*time.Second):
		return fmt.Errorf("systemctl stop %s did not finish", name)
	}
}

// ServiceStatus reports whether a unit is running and its main PID. It
// needs no privileges.
func ServiceStatus(name string) (ServiceState, error) {
	out, err := exec.Command("systemctl", "show", "--property=LoadState,ActiveState,MainPID,ExecMainStatus", name).Output()
	if err != nil {
		return ServiceState{}, fmt.Errorf("systemctl show %s: %w", name, err)
	}
	props := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			props[key] = value
		}
	}
	if props["LoadState"] == "not-found" {
		return ServiceState{}, ErrServiceNotInstalled
	}
	pid, _ := strconv.Atoi(props["MainPID"])
	code, _ := strconv.Atoi(props["ExecMainStatus"])
	return ServiceState{
		Running:  props["ActiveState"] == "active",
		PID:      pid,
		ExitCode: code,
	}, nil
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
