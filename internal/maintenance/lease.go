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
	"golang.org/x/mod/semver"
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

// AcquireServiceLease takes an installed service's lifecycle lock without
// waiting, then reads the lifecycle state and authorizes the start. Every
// failure wraps host.ErrRefused: a service that can't start here must not
// be restarted in a loop by its service manager.
func AcquireServiceLease(l layout.Layout, version string) (*Lease, error) {
	refuse := func(err error) (*Lease, error) {
		return nil, fmt.Errorf("%w: %w", host.ErrRefused, err)
	}
	if l.Form.Session() {
		return refuse(fmt.Errorf("AcquireServiceLease called on the %s layout", l.Form))
	}
	if err := l.CheckControl(l.State, l.LifecycleLock); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return refuse(errors.New("not installed; run the installer"))
		}
		return refuse(err)
	}
	// The installed service may only read its control directory.
	lock, err := xsyscall.AcquireLock(context.Background(), l.LifecycleLock, xsyscall.LockOptions{
		Mode:     xsyscall.ModeExclusive,
		ReadOnly: true,
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

// SessionOptions say how a session form's service takes its lease.
type SessionOptions struct {
	// Wait is how long a start waits for the lifecycle lock another
	// process holds: none for a development instance, whose developer
	// wants to know at once, and LifecycleLockTimeout for the Flatpak, whose
	// restart hands the lock from an older build to the newest (M9.3).
	Wait time.Duration
	// Snapshot keeps a copy of the data an older version left, before this
	// version migrates it. It runs under the lock and before the transition
	// is published, so a snapshot that fails leaves the state as it was,
	// for the next start to try again.
	Snapshot func(from State) error
}

// SessionPlan is how a session form's service starts: the transition it
// publishes first, if any, whether it keeps a snapshot before, and the
// mode it starts in.
type SessionPlan struct {
	Mode     StartMode
	Publish  *State
	Snapshot bool
}

// RolledBackError is a session form's data from a newer version than the
// binary, as when Flatpak rolls an app back (M9.3).
type RolledBackError struct {
	Data, Binary string
}

func (e *RolledBackError) Error() string {
	return fmt.Sprintf("Dens was rolled back to %s, but its data is from %s, which %s can't read; update Dens again", e.Binary, e.Data, e.Binary)
}

// SessionStart decides how a session form's service built as version
// starts against the lifecycle state it found under the lock (none, unless
// found), as an installer decides for an installed one. A development
// instance migrates whichever way its version moves, since its data is
// disposable. The Flatpak's versions move only forward (M9.3): data an
// older version left is an update, which it keeps a snapshot of first;
// data a newer one wrote, a rollback, which it refuses; and a transition
// another build began, it resumes or takes over where the target is its
// version or older, since migrations are ordered and safe to run again,
// and refuses where it's newer.
func SessionStart(form layout.Form, state State, found bool, version string) (SessionPlan, error) {
	if !form.Session() {
		return SessionPlan{}, fmt.Errorf("SessionStart called for the %s", form)
	}
	switch {
	case !found:
		next := NewState(PhaseInstalling, "", version)
		return SessionPlan{Mode: StartMigrate, Publish: &next}, nil
	case state.Phase == PhaseReady && state.Version == version:
		return SessionPlan{Mode: StartNormal}, nil
	case state.Phase.Transitional() && state.TargetVersion == version:
		return SessionPlan{Mode: StartMigrate}, nil
	case form == layout.Development:
		next := retarget(state, version)
		return SessionPlan{Mode: StartMigrate, Publish: &next}, nil
	case state.Phase == PhaseReady && semver.Compare(state.Version, version) < 0:
		next := NewState(PhaseUpdating, state.Version, version)
		return SessionPlan{Mode: StartMigrate, Publish: &next, Snapshot: true}, nil
	case state.Phase == PhaseReady:
		return SessionPlan{}, &RolledBackError{Data: state.Version, Binary: version}
	case state.Phase.Transitional() && semver.Compare(state.TargetVersion, version) < 0:
		// The snapshot from before the transition began stays the one to
		// restore: the data may be part way through it now.
		next := retarget(state, version)
		return SessionPlan{Mode: StartMigrate, Publish: &next}, nil
	case state.Phase.Transitional():
		return SessionPlan{}, fmt.Errorf("a newer Dens, %s, began moving this data to its version; update Dens to %s or later", state.TargetVersion, state.TargetVersion)
	}
	return SessionPlan{}, fmt.Errorf("installation is %s at %s; this binary is %s", state.Phase, state.Version, version)
}

// retarget is the transition state was in, or an update from it, aimed at
// version instead. A restore stays one, so the first start of its data
// still clears what a restore's clears.
func retarget(state State, version string) State {
	switch state.Phase {
	case PhaseInstalling:
		return NewState(PhaseInstalling, "", version)
	case PhaseRestoring:
		return NewState(PhaseRestoring, version, version)
	}
	return NewState(PhaseUpdating, state.Version, version)
}

// AcquireSessionLease takes a session form's lifecycle lock, waiting as
// opts say, and only then reads its lifecycle state, so two builds never
// race to change it. It publishes what SessionStart decides, keeping a
// snapshot first where that says to. Every failure wraps host.ErrRefused.
func AcquireSessionLease(l layout.Layout, version string, opts SessionOptions) (*Lease, error) {
	refuse := func(err error) (*Lease, error) {
		return nil, fmt.Errorf("%w: %w", host.ErrRefused, err)
	}
	if !l.Form.Session() {
		return refuse(fmt.Errorf("AcquireSessionLease called on the %s layout", l.Form))
	}
	lockOpts := xsyscall.LockOptions{Mode: xsyscall.ModeExclusive}
	if opts.Wait > 0 {
		lockOpts.Timeout, lockOpts.Poll = opts.Wait, 100*time.Millisecond
	}
	lock, err := xsyscall.AcquireLock(context.Background(), l.LifecycleLock, lockOpts)
	switch {
	case errors.Is(err, xsyscall.ErrLocked) && opts.Wait > 0:
		return refuse(fmt.Errorf("another Dens is running for this instance, and didn't let go of it within %v", opts.Wait))
	case errors.Is(err, xsyscall.ErrLocked):
		return refuse(errors.New("another service instance holds the lifecycle lock"))
	case err != nil:
		return refuse(err)
	}
	fail := func(err error) (*Lease, error) {
		_ = lock.Close()
		return refuse(err)
	}
	files := []string{l.LifecycleLock}
	if _, err := os.Lstat(l.State); err == nil {
		files = append(files, l.State)
	}
	if err := l.CheckControl(files...); err != nil {
		return fail(err)
	}
	state, err := ReadState(l.State)
	found := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fail(err)
	}
	plan, err := SessionStart(l.Form, state, found, version)
	if err != nil {
		return fail(err)
	}
	if plan.Snapshot && opts.Snapshot != nil {
		if err := opts.Snapshot(state); err != nil {
			return fail(fmt.Errorf("keep a snapshot of the data from %s before updating it: %w", state.Version, err))
		}
	}
	if plan.Publish != nil {
		if err := WriteState(l.State, *plan.Publish, nil); err != nil {
			return fail(err)
		}
		state = *plan.Publish
	}
	return &Lease{lock: lock, State: state, Mode: plan.Mode}, nil
}

// MarkSessionReady publishes ready once a session form's service has
// started, as an installer does once the service it started is up.
func MarkSessionReady(l layout.Layout, version string) error {
	if !l.Form.Session() {
		return fmt.Errorf("MarkSessionReady called on the %s layout", l.Form)
	}
	return WriteState(l.State, NewState(PhaseReady, version, ""), nil)
}
