package maintenance

import (
	"errors"
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
	l, err := layout.New("dens", "main", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureDev(); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestServiceLeaseIsExclusiveAndFollowsState(t *testing.T) {
	l := devLayout(t)
	if _, err := AcquireServiceLease(l, "v1.0.0"); !errors.Is(err, host.ErrRefused) {
		t.Fatalf("lease without state = %v, want ErrRefused", err)
	}

	if err := EnsureDevReady(l, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	lease, err := AcquireServiceLease(l, "v1.0.0")
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}
	if lease.Mode != StartMigrate {
		t.Fatalf("fresh development instance mode = %v, want migrate", lease.Mode)
	}
	if _, err := AcquireServiceLease(l, "v1.0.0"); !errors.Is(err, host.ErrRefused) {
		t.Fatalf("second lease = %v, want ErrRefused", err)
	}
	if err := MarkDevReady(l, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}

	lease, err = AcquireServiceLease(l, "v1.0.0")
	if err != nil {
		t.Fatalf("lease after ready: %v", err)
	}
	if lease.Mode != StartNormal {
		t.Fatalf("mode = %v, want normal", lease.Mode)
	}
	lease.Close()

	if _, err := AcquireServiceLease(l, "v2.0.0"); !errors.Is(err, host.ErrRefused) {
		t.Fatalf("other version's lease = %v, want ErrRefused", err)
	}
}
