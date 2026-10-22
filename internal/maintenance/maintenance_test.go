package maintenance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

func TestStateValidation(t *testing.T) {
	valid := []State{
		NewState(PhaseInstalling, "", "v1.0.0"),
		NewState(PhaseUpdating, "v1.0.0", "v1.1.0"),
		NewState(PhaseRestoring, "v1.0.0", "v1.0.0"),
		NewState(PhaseReady, "v1.0.0", ""),
		NewState(PhaseUninstalling, "v1.0.0", ""),
	}
	for _, s := range valid {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	invalid := []State{
		NewState(PhaseInstalling, "", ""),
		NewState(PhaseUpdating, "", "v1.1.0"),
		NewState(PhaseRestoring, "v1.0.0", "v1.1.0"),
		NewState(PhaseReady, "", ""),
		NewState(PhaseReady, "v1.0.0", "v1.1.0"),
		NewState(PhaseReady, "1.0.0", ""),
		NewState("bogus", "v1.0.0", ""),
		{Phase: PhaseReady, Version: "v1.0.0", ChangedAt: "yesterday"},
	}
	for _, s := range invalid {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v accepted", s)
		}
	}
}

func TestAuthorizeStart(t *testing.T) {
	cases := []struct {
		state State
		mode  StartMode
		ok    bool
	}{
		{NewState(PhaseReady, "v1.0.0", ""), StartNormal, true},
		{NewState(PhaseReady, "v0.9.0", ""), 0, false},
		{NewState(PhaseInstalling, "", "v1.0.0"), StartMigrate, true},
		{NewState(PhaseUpdating, "v0.9.0", "v1.0.0"), StartMigrate, true},
		{NewState(PhaseUpdating, "v1.0.0", "v1.1.0"), 0, false},
		{NewState(PhaseRestoring, "v1.0.0", "v1.0.0"), StartMigrate, true},
		{NewState(PhaseUninstalling, "v1.0.0", ""), 0, false},
	}
	for _, c := range cases {
		mode, err := AuthorizeStart(c.state, "v1.0.0")
		if (err == nil) != c.ok || (c.ok && mode != c.mode) {
			t.Errorf("AuthorizeStart(%+v) = %v, %v", c.state, mode, err)
		}
	}
}

func TestReadStateIsStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := WriteState(path, NewState(PhaseReady, "v1.0.0", ""), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	extra := strings.Replace(string(data), `"phase"`, `"nonce":"x","phase"`, 1)
	if err := os.WriteFile(path, []byte(extra), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(path); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := ReadState(filepath.Join(t.TempDir(), "missing.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing state = %v, want ErrNotExist", err)
	}
}

func devLayout(t *testing.T) layout.Layout {
	t.Helper()
	setDevHome(t)
	l, err := layout.New("dens", "main", layout.Development)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatal(err)
	}
	return l
}

// A session form decides how to start from the state it finds (M9.3): a
// development instance migrates whichever way its version moves, and the
// Flatpak only forward, keeping a snapshot as it updates, refusing a
// rollback, and taking over only a transition to its version or older.
func TestSessionStart(t *testing.T) {
	type want struct {
		mode     StartMode
		publish  string // phase version>target, or "" for none
		snapshot bool
		refused  string
	}
	cases := []struct {
		form  layout.Form
		state *State
		want  want
	}{
		{layout.Development, nil, want{StartMigrate, "installing >v1.1.0", false, ""}},
		{layout.Development, ptr(NewState(PhaseReady, "v1.1.0", "")), want{StartNormal, "", false, ""}},
		{layout.Development, ptr(NewState(PhaseReady, "v1.2.0", "")), want{StartMigrate, "updating v1.2.0>v1.1.0", false, ""}},
		{layout.Development, ptr(NewState(PhaseRestoring, "v1.0.0", "v1.0.0")), want{StartMigrate, "restoring v1.1.0>v1.1.0", false, ""}},

		{layout.Flatpak, nil, want{StartMigrate, "installing >v1.1.0", false, ""}},
		{layout.Flatpak, ptr(NewState(PhaseReady, "v1.1.0", "")), want{StartNormal, "", false, ""}},
		{layout.Flatpak, ptr(NewState(PhaseReady, "v1.0.0", "")), want{StartMigrate, "updating v1.0.0>v1.1.0", true, ""}},
		{layout.Flatpak, ptr(NewState(PhaseReady, "v1.2.0", "")), want{refused: "rolled back to v1.1.0"}},
		{layout.Flatpak, ptr(NewState(PhaseUpdating, "v1.0.0", "v1.1.0")), want{StartMigrate, "", false, ""}},
		{layout.Flatpak, ptr(NewState(PhaseRestoring, "v1.1.0", "v1.1.0")), want{StartMigrate, "", false, ""}},
		{layout.Flatpak, ptr(NewState(PhaseUpdating, "v0.9.0", "v1.0.0")), want{StartMigrate, "updating v0.9.0>v1.1.0", false, ""}},
		{layout.Flatpak, ptr(NewState(PhaseInstalling, "", "v1.0.0")), want{StartMigrate, "installing >v1.1.0", false, ""}},
		{layout.Flatpak, ptr(NewState(PhaseRestoring, "v1.0.0", "v1.0.0")), want{StartMigrate, "restoring v1.1.0>v1.1.0", false, ""}},
		{layout.Flatpak, ptr(NewState(PhaseUpdating, "v1.1.0", "v1.2.0")), want{refused: "newer Dens, v1.2.0"}},
		{layout.Flatpak, ptr(NewState(PhaseUninstalling, "v1.1.0", "")), want{refused: "uninstalling"}},
	}
	for _, c := range cases {
		state, found := State{}, c.state != nil
		if found {
			state = *c.state
		}
		plan, err := SessionStart(c.form, state, found, "v1.1.0")
		if c.want.refused != "" {
			if err == nil || !strings.Contains(err.Error(), c.want.refused) {
				t.Errorf("%s from %+v: %+v, %v; want refused: %s", c.form, c.state, plan, err, c.want.refused)
			}
			continue
		}
		published := ""
		if plan.Publish != nil {
			if err := plan.Publish.Validate(); err != nil {
				t.Errorf("%s from %+v publishes an invalid %+v: %v", c.form, c.state, *plan.Publish, err)
			}
			published = fmt.Sprintf("%s %s>%s", plan.Publish.Phase, plan.Publish.Version, plan.Publish.TargetVersion)
		}
		if err != nil || plan.Mode != c.want.mode || published != c.want.publish || plan.Snapshot != c.want.snapshot {
			t.Errorf("%s from %+v: mode %v, publish %q, snapshot %v, %v; want %+v", c.form, c.state, plan.Mode, published, plan.Snapshot, err, c.want)
		}
	}
	var rolled *RolledBackError
	if _, err := SessionStart(layout.Flatpak, NewState(PhaseReady, "v1.2.0", ""), true, "v1.1.0"); !errors.As(err, &rolled) || rolled.Data != "v1.2.0" {
		t.Fatalf("a rollback's refusal = %v", err)
	}
	if _, err := SessionStart(layout.Installed, State{}, false, "v1.1.0"); err == nil {
		t.Fatal("SessionStart decided for an installed instance")
	}
}

func ptr[T any](v T) *T { return &v }

// A session form's lease takes the lock before it reads the state, and
// publishes the start it decides on; a second start is refused, and the
// installed lease is no session's.
func TestSessionLeaseIsExclusiveAndPublishes(t *testing.T) {
	l := devLayout(t)
	if _, err := AcquireServiceLease(l, "v1.0.0"); !errors.Is(err, host.ErrRefused) {
		t.Fatalf("the installed lease on a session layout = %v, want ErrRefused", err)
	}
	lease, err := AcquireSessionLease(l, "v1.0.0", SessionOptions{})
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}
	if lease.Mode != StartMigrate || lease.State.Phase != PhaseInstalling {
		t.Fatalf("fresh development instance: mode %v, state %+v", lease.Mode, lease.State)
	}
	if state, err := ReadState(l.State); err != nil || state.Phase != PhaseInstalling || state.TargetVersion != "v1.0.0" {
		t.Fatalf("published %+v, %v", state, err)
	}
	if _, err := AcquireSessionLease(l, "v1.0.0", SessionOptions{}); !errors.Is(err, host.ErrRefused) {
		t.Fatalf("second lease = %v, want ErrRefused", err)
	}
	if err := MarkSessionReady(l, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err = AcquireSessionLease(l, "v1.0.0", SessionOptions{})
	if err != nil {
		t.Fatalf("lease after ready: %v", err)
	}
	if lease.Mode != StartNormal {
		t.Fatalf("mode = %v, want normal", lease.Mode)
	}
	lease.Close()
	lease, err = AcquireSessionLease(l, "v0.9.0", SessionOptions{})
	if err != nil || lease.Mode != StartMigrate || lease.State.TargetVersion != "v0.9.0" {
		t.Fatalf("an older development build's lease = %+v, %v; want it migrating", lease, err)
	}
	lease.Close()
}
