package maintenance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

func flatpakLayout(t *testing.T) layout.Layout {
	t.Helper()
	app := filepath.Join(t.TempDir(), "app")
	t.Setenv("XDG_DATA_HOME", filepath.Join(app, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(app, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(app, "run"))
	l, err := layout.New("dens", "main", layout.Flatpak)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatal(err)
	}
	return l
}

// The Flatpak's start keeps a snapshot before it publishes an update, so a
// snapshot that fails leaves the data as the older version left them; a
// newer version's data it refuses; and a start that finds the lock held
// waits for it (M9.3).
func TestFlatpakLeaseSnapshotsAndWaits(t *testing.T) {
	l := flatpakLayout(t)
	if err := WriteState(l.State, NewState(PhaseReady, "v1.0.0", ""), nil); err != nil {
		t.Fatal(err)
	}
	failing := SessionOptions{Snapshot: func(State) error { return errors.New("disk full") }}
	if _, err := AcquireSessionLease(l, "v1.1.0", failing); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("a lease whose snapshot failed = %v", err)
	}
	if state, err := ReadState(l.State); err != nil || state.Phase != PhaseReady || state.Version != "v1.0.0" {
		t.Fatalf("after a failed snapshot the state is %+v, %v", state, err)
	}

	var from State
	snapshotted := SessionOptions{Snapshot: func(s State) error {
		from = s
		if state, err := ReadState(l.State); err != nil || state.Phase != PhaseReady {
			t.Errorf("the snapshot came after the transition: %+v, %v", state, err)
		}
		return nil
	}}
	lease, err := AcquireSessionLease(l, "v1.1.0", snapshotted)
	if err != nil {
		t.Fatal(err)
	}
	if from.Version != "v1.0.0" || lease.Mode != StartMigrate || lease.State.Phase != PhaseUpdating {
		t.Fatalf("the update's lease: snapshot of %q, %+v", from.Version, lease)
	}

	// The next build waits while this one holds the lock, and starts once
	// it lets go.
	got := make(chan error, 1)
	go func() {
		next, err := AcquireSessionLease(l, "v1.1.0", SessionOptions{Wait: 5 * time.Second})
		if err == nil {
			next.Close()
		}
		got <- err
	}()
	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-got:
		t.Fatalf("a held lock's second lease didn't wait: %v", err)
	default:
	}
	lease.Close()
	if err := <-got; err != nil {
		t.Fatalf("the waiting lease: %v", err)
	}
	lease, err = AcquireSessionLease(l, "v1.1.0", SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireSessionLease(l, "v1.1.0", SessionOptions{Wait: 200 * time.Millisecond}); !errors.Is(err, host.ErrRefused) || !strings.Contains(err.Error(), "didn't let go") {
		t.Fatalf("a lease that waited out the lock = %v", err)
	}
	if err := MarkSessionReady(l, "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	lease.Close()

	var rolled *RolledBackError
	if _, err := AcquireSessionLease(l, "v1.0.0", SessionOptions{}); !errors.As(err, &rolled) || !errors.Is(err, host.ErrRefused) {
		t.Fatalf("a rolled-back build's lease = %v", err)
	}

	// A state file another account could change is refused, as anywhere.
	if err := os.Chmod(l.State, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireSessionLease(l, "v1.1.0", SessionOptions{}); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("a loosened state file's lease = %v", err)
	}
}
