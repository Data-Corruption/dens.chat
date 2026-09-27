package host

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// serviceStartTimeout bounds waiting for a started service to report
// RUNNING. Migrations run before readiness.
const serviceStartTimeout = 5 * time.Minute

// ServiceStart starts a service and waits until it reports RUNNING, or
// returns its exit code if it stops first.
func ServiceStart(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("open service %s: %w", name, err)
	}
	defer s.Close()
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("start service %s: %w", name, err)
	}
	deadline := time.Now().Add(serviceStartTimeout)
	for {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("query service %s: %w", name, err)
		}
		switch status.State {
		case svc.Running:
			return nil
		case svc.Stopped:
			return fmt.Errorf("service %s stopped while starting (exit code %d)", name, exitCode(status))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service %s did not start within %v", name, serviceStartTimeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ServiceStop asks a service to stop and waits for it. If it is still
// running after timeout, its process is ended with taskkill /F, which enables
// the debug privilege an administrator needs to open a service process.
func ServiceStop(name string, timeout time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("open service %s: %w", name, err)
	}
	defer s.Close()
	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service %s: %w", name, err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if status.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("stop service %s: %w", name, err)
		}
	}
	if waitStopped(s, timeout) == nil {
		return nil
	}
	status, err = s.Query()
	if err != nil {
		return fmt.Errorf("query service %s: %w", name, err)
	}
	if status.ProcessId != 0 {
		out, err := exec.Command("taskkill.exe", "/F", "/PID", strconv.Itoa(int(status.ProcessId))).CombinedOutput()
		if err != nil {
			return fmt.Errorf("end service %s process %d: %w: %s", name, status.ProcessId, err, strings.TrimSpace(string(out)))
		}
	}
	return waitStopped(s, 30*time.Second)
}

func waitStopped(s *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := s.Query()
		if err != nil {
			return err
		}
		if status.State == svc.Stopped {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service still in state %d after %v", status.State, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ServiceStatus reports whether a service is running and its process ID. It
// asks only for SERVICE_QUERY_STATUS, which ordinary users hold by default.
func ServiceStatus(name string) (ServiceState, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return ServiceState{}, fmt.Errorf("connect to service manager: %w", err)
	}
	defer windows.CloseServiceHandle(scm)
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return ServiceState{}, err
	}
	h, err := windows.OpenService(scm, namePtr, windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return ServiceState{}, ErrServiceNotInstalled
	}
	if err != nil {
		return ServiceState{}, fmt.Errorf("open service %s: %w", name, err)
	}
	defer windows.CloseServiceHandle(h)
	var status windows.SERVICE_STATUS_PROCESS
	var needed uint32
	if err := windows.QueryServiceStatusEx(h, windows.SC_STATUS_PROCESS_INFO,
		(*byte)(unsafe.Pointer(&status)), uint32(unsafe.Sizeof(status)), &needed); err != nil {
		return ServiceState{}, fmt.Errorf("query service %s: %w", name, err)
	}
	code := int(status.Win32ExitCode)
	if status.Win32ExitCode == uint32(windows.ERROR_SERVICE_SPECIFIC_ERROR) {
		code = int(status.ServiceSpecificExitCode)
	}
	return ServiceState{
		Running:  status.CurrentState == windows.SERVICE_RUNNING,
		PID:      int(status.ProcessId),
		ExitCode: code,
	}, nil
}

func exitCode(status svc.Status) uint32 {
	if status.Win32ExitCode == uint32(windows.ERROR_SERVICE_SPECIFIC_ERROR) {
		return status.ServiceSpecificExitCode
	}
	return status.Win32ExitCode
}
