// Package host holds the operating-system-specific runtime pieces of Dens.
// Each piece is a small function pair in _linux.go and _windows.go, so
// nothing above this package needs build tags: running under the service
// manager, the host-bound data key, the local control endpoint and its peer
// identity, locked memory for key material, low-priority child processes,
// service control, opening a URL, and free disk space. Go builds _linux.go
// files for Android too, so where Android differs, an _android.go file
// takes the piece's place and its Linux one says !android (M12).
package host

import (
	"context"
	"errors"
	"fmt"
)

// ExitRefused is the exit status of a service start that must not be retried
// automatically: lifecycle state doesn't allow this binary to run, or a
// migration failed. The systemd unit lists it in RestartPreventExitStatus;
// on Windows it is a service-specific exit code, which the SCM treats as a
// non-crash failure and doesn't answer with recovery actions.
const ExitRefused = 78

// ErrRefused marks errors that end the service with ExitRefused.
var ErrRefused = errors.New("service start refused")

// ErrServiceNotInstalled is returned by service control for an unknown service.
var ErrServiceNotInstalled = errors.New("service is not installed")

// RunFunc is a service body. It calls ready once when the service is fully
// started and returns when ctx is cancelled or it fails.
type RunFunc func(ctx context.Context, ready func()) error

// Identity is an operating-system account: a decimal UID on Linux and a SID
// string on Windows. Name is for messages only and never compared.
type Identity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Equal reports whether both identities name the same account.
func (i Identity) Equal(other Identity) bool { return i.ID != "" && i.ID == other.ID }

func (i Identity) String() string {
	if i.Name == "" {
		return i.ID
	}
	return fmt.Sprintf("%s (%s)", i.Name, i.ID)
}

// ControlServer says who must be serving a control endpoint before a client
// trusts it. On Linux the server's peer UID must be Account. On Windows, if
// ServiceName is set, the pipe's server process must be the process the SCM
// reports for that service; otherwise (a development service run from a
// console) the server process must run as Account.
type ControlServer struct {
	Account     Identity
	ServiceName string
}

// DesktopAction is a shortcut the helper binds with the desktop (M7.2): its
// ID, what it does, and the trigger it would like, in the XDG shortcuts
// format, or none.
type DesktopAction struct {
	ID, Description, Trigger string
}

// DesktopPress is a bound shortcut going down or up.
type DesktopPress struct {
	ID   string
	Down bool
}

// ErrDesktopCancelled is the member turning the desktop's dialog down.
var ErrDesktopCancelled = errors.New("cancelled")

// ServiceState is what service control reports about an installed service.
type ServiceState struct {
	Running bool
	PID     int
	// ExitCode is the last exit status when the service is stopped.
	ExitCode int
}

// HostKeySize is the length of the data key in bytes.
const HostKeySize = 32

// HostKeyCredential is the credential name of the data key, both inside the
// encrypted credential and in $CREDENTIALS_DIRECTORY, on Linux.
const HostKeyCredential = "datakey"

func checkKeySize(key []byte) error {
	if len(key) != HostKeySize {
		return fmt.Errorf("data key is %d bytes, want %d", len(key), HostKeySize)
	}
	return nil
}
