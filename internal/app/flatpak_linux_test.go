package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// The Flatpak's snapshot of an older version's data, kept as it updates
// them, is a backup the restore takes: of that version, without the cache,
// and the last one only (M9.3). A rollback's refusal names it.
func TestSnapshotKeepsTheLastOneAsABackup(t *testing.T) {
	devHome(t)
	a := openDev(t, fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000))
	l := a.Layout
	if err := os.WriteFile(filepath.Join(l.Cache, "cached"), []byte("a den's file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	l.Snapshot = filepath.Join(t.TempDir(), "snapshot")
	if err := os.Mkdir(l.Snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &App{buildInfo: devBuild, Layout: l}
	for _, from := range []string{"v1.0.0", "v1.0.1"} {
		if err := s.snapshot(maintenance.NewState(maintenance.PhaseReady, from, "")); err != nil {
			t.Fatalf("snapshot of %s: %v", from, err)
		}
	}
	entries, err := os.ReadDir(l.Snapshot)
	if err != nil || len(entries) != 1 || !strings.Contains(entries[0].Name(), "v1.0.1") {
		t.Fatalf("the snapshots kept: %v, %v", entries, err)
	}
	if s.lastSnapshot() != s.snapshotKept || s.snapshotKept != filepath.Join(l.Snapshot, entries[0].Name()) {
		t.Fatalf("the last snapshot is %q, kept %q", s.lastSnapshot(), s.snapshotKept)
	}
	if info, err := os.Stat(s.snapshotKept); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the snapshot's file: %v, %v", info, err)
	}

	file, err := os.Open(s.snapshotKept)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	dir := t.TempDir()
	manifest, err := backup.Extract(file, dir, backup.ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v1.0.1" || manifest.SchemaVersion != database.SchemaVersion(devBuild) || manifest.Instance != l.Instance {
		t.Fatalf("the snapshot's manifest: %+v", manifest)
	}
	if _, err := os.Stat(backup.DatabasePath(dir)); err != nil {
		t.Fatalf("the snapshot holds no database: %v", err)
	}
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		found = found || d != nil && d.Name() == "cached"
		return nil
	})
	if found {
		t.Fatal("the snapshot holds the cache")
	}

	refusal := s.explainRollback(fmt.Errorf("%w: %w", host.ErrRefused, &maintenance.RolledBackError{Data: "v1.1.0", Binary: "v1.0.1"}))
	for _, want := range []string{"rolled back to v1.0.1", "restore " + s.snapshotKept, "flatpak run --command=dens chat.dens.Dens"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Errorf("the rollback's refusal %q doesn't say %q", refusal, want)
		}
	}
}
