package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckControlRefusesLoosenedOrSwappedFiles(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	l, err := New("dens", "main", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureDev(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.State, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.State); err != nil {
		t.Fatalf("private state refused: %v", err)
	}

	if err := os.Chmod(l.State, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.State); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("world-readable state accepted: %v", err)
	}

	if err := os.Remove(l.State); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, l.State); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.State); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked state accepted: %v", err)
	}
}
