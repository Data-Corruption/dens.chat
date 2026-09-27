package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"
)

// Timeouts for maintenance commands.
const (
	OperationLockTimeout = 5 * time.Minute
	// LifecycleLockTimeout covers the moment between the service manager
	// reporting a stop and the process releasing its lock.
	LifecycleLockTimeout = 30 * time.Second
)

// StartMode says how the service may start.
type StartMode int

const (
	// StartNormal opens a database already at the current schema.
	StartNormal StartMode = iota
	// StartMigrate applies pending migrations first, during a transition
	// that names this version as its target.
	StartMigrate
)

// AuthorizeStart decides whether a service built as version may start
// against state, and how.
func AuthorizeStart(state State, version string) (StartMode, error) {
	switch {
	case state.Phase == PhaseReady && state.Version == version:
		return StartNormal, nil
	case state.Phase.Transitional() && state.TargetVersion == version:
		return StartMigrate, nil
	case state.Phase == PhaseReady:
		return 0, fmt.Errorf("installed version is %s, this binary is %s", state.Version, version)
	default:
		return 0, fmt.Errorf("installation is %s (target %q); this binary is %s", state.Phase, state.TargetVersion, version)
	}
}

// Lease is the running service's exclusive hold on the lifecycle lock.
type Lease struct {
	lock  *xsyscall.Lock
	State State
	Mode  StartMode
}

// Close releases the lease.
func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	return l.lock.Close()
}

// AcquireServiceLease takes the lifecycle lock without waiting, then reads
// the lifecycle state and authorizes the start. Every failure wraps
// host.ErrRefused: a service that can't start here must not be restarted in
// a loop by its service manager.
func AcquireServiceLease(l layout.Layout, version string) (*Lease, error) {
	refuse := func(err error) (*Lease, error) {
		return nil, fmt.Errorf("%w: %w", host.ErrRefused, err)
	}
	if err := l.CheckControl(l.State, l.LifecycleLock); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return refuse(errors.New("not installed; run the installer"))
		}
		return refuse(err)
	}
	lock, err := xsyscall.AcquireLock(context.Background(), l.LifecycleLock, xsyscall.LockOptions{
		Mode:     xsyscall.ModeExclusive,
		ReadOnly: !l.Dev,
	})
	if errors.Is(err, xsyscall.ErrLocked) {
		return refuse(errors.New("another service instance or a maintenance command holds the lifecycle lock"))
	}
	if err != nil {
		return refuse(err)
	}
	state, err := ReadState(l.State)
	if err != nil {
		_ = lock.Close()
		return refuse(err)
	}
	mode, err := AuthorizeStart(state, version)
	if err != nil {
		_ = lock.Close()
		return refuse(err)
	}
	return &Lease{lock: lock, State: state, Mode: mode}, nil
}

// AcquireOperationLock serializes maintenance commands.
func AcquireOperationLock(ctx context.Context, l layout.Layout) (*xsyscall.Lock, error) {
	lock, err := xsyscall.AcquireLock(ctx, l.OperationLock, xsyscall.LockOptions{
		Mode:    xsyscall.ModeExclusive,
		Timeout: OperationLockTimeout,
		Poll:    250 * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("wait for other maintenance to finish: %w", err)
	}
	return lock, nil
}

// AcquireLifecycleLock takes the lifecycle lock exclusively for a maintenance
// command, after it has stopped the service. Holding it proves the service
// is down and keeps it from starting until released.
func AcquireLifecycleLock(ctx context.Context, l layout.Layout) (*xsyscall.Lock, error) {
	lock, err := xsyscall.AcquireLock(ctx, l.LifecycleLock, xsyscall.LockOptions{
		Mode:    xsyscall.ModeExclusive,
		Timeout: LifecycleLockTimeout,
		Poll:    100 * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("the service is still holding the lifecycle lock: %w", err)
	}
	return lock, nil
}

// EnsureDevReady marks a development instance ready for version, the way an
// installer would after a successful install or update.
func EnsureDevReady(l layout.Layout, version string) error {
	if !l.Dev {
		return errors.New("EnsureDevReady called on an installed layout")
	}
	state, err := ReadState(l.State)
	if err == nil && state.Phase == PhaseReady && state.Version == version {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// A development instance migrates on every version change; its data is
	// disposable.
	target := NewState(PhaseUpdating, version, version)
	if errors.Is(err, fs.ErrNotExist) {
		target = NewState(PhaseInstalling, "", version)
	}
	if err := WriteState(l.State, target, nil); err != nil {
		return err
	}
	lockFile, err := xsyscall.OpenNoFollow(l.LifecycleLock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("create lifecycle lock: %w", err)
	}
	return lockFile.Close()
}

// MarkDevReady publishes ready after a development service finished starting.
func MarkDevReady(l layout.Layout, version string) error {
	if !l.Dev {
		return errors.New("MarkDevReady called on an installed layout")
	}
	return WriteState(l.State, NewState(PhaseReady, version, ""), nil)
}
