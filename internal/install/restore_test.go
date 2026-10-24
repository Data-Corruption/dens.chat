package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, host.HostKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

// emptyTarget is the layout of an installed instance that holds nothing
// yet, as a restore's target.
func emptyTarget(t *testing.T) layout.Layout {
	t.Helper()
	l, err := newFake(t).Layout("main")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// A wrong password is refused once the database is out, before any upload
// is written, rather than after all of them (M10).
func TestRestoreChecksBeforeUploads(t *testing.T) {
	key := testKey(t)
	archive := makeBackupWith(t, key, "correct horse battery", map[string]string{"0123456789abcdef": "sealed bytes"})
	bi := buildInfo("v1.0.0")

	staging := t.TempDir()
	if _, _, err := openBackup(bi, archive, staging, "wrong wrong wrong", emptyTarget(t)); err == nil {
		t.Fatal("the wrong password opened the backup")
	}
	if _, err := os.Stat(backup.UploadsPath(staging)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an upload was written before the password was checked: %v", err)
	}

	staging = t.TempDir()
	got, _, err := openBackup(bi, archive, staging, "correct horse battery", emptyTarget(t))
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("openBackup: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(backup.UploadsPath(staging), "0123456789abcdef")); err != nil || string(data) != "sealed bytes" {
		t.Fatalf("the upload: %q, %v", data, err)
	}
}

// A disk without room for the backup is refused before anything is
// written, and one that runs out partway stops before it fills, saying
// how much more the rest needs (M10).
func TestRestoreNeedsRoom(t *testing.T) {
	archive := makeBackupWith(t, testKey(t), "correct horse battery", map[string]string{"0123456789abcdef": "sealed bytes"})
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	bi := buildInfo("v1.0.0")
	free := freeSpace
	t.Cleanup(func() { freeSpace = free })

	freeSpace = func(string) (uint64, error) { return uint64(info.Size()) + restoreFloor - 1, nil }
	staging := t.TempDir()
	_, _, err = openBackup(bi, archive, staging, "correct horse battery", emptyTarget(t))
	if err == nil || !strings.Contains(err.Error(), "takes that much, and 1 GB to spare") {
		t.Fatalf("a backup the disk hasn't room for: %v", err)
	}
	if entries, _ := os.ReadDir(staging); len(entries) != 0 {
		t.Fatalf("a refused restore wrote %v", entries)
	}

	// Room as it starts, and then none.
	asks := 0
	freeSpace = func(string) (uint64, error) {
		asks++
		if asks == 1 {
			return 1 << 40, nil
		}
		return restoreFloor, nil
	}
	_, _, err = openBackup(bi, archive, t.TempDir(), "correct horse battery", emptyTarget(t))
	if err == nil || !strings.Contains(err.Error(), "ran out of room partway") || !strings.Contains(err.Error(), "more") {
		t.Fatalf("a restore that ran out of room: %v", err)
	}
}

// holdIn gives the instance at target a database that hosts the den named
// Held with this public key, where there is one, and has joined this many
// dens. The database stays open, as the service keeps it, until the test
// closes it.
func holdIn(t *testing.T, target layout.Layout, public []byte, joined int) *sql.DB {
	t.Helper()
	logger, err := xlog.New(t.TempDir(), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logger.Close() })
	db, err := database.New(target.DB, logger, buildInfo("v1.0.0"), database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if public != nil {
		insertDen(t, ctx, db, "Held", public)
	}
	for i := range joined {
		if _, err := db.ExecContext(ctx, `INSERT INTO joined_dens (den_id, profile, device_key, joined_at) VALUES (?, x'00', x'00', 0)`,
			[]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// A den's export goes only where nothing would be lost (M10). Over an
// instance that has joined a den, or hosts another, it's refused before
// any upload is extracted, saying what the instance holds and where to
// restore it instead. An instance that holds nothing yet takes it, as does
// one holding the same den, which is a move done again. A backup still
// replaces any instance.
func TestAnExportGoesWhereNothingIsLost(t *testing.T) {
	key := testKey(t)
	bi := buildInfo("v1.0.0")
	public, other := testKey(t), testKey(t)
	export := makeExportOf(t, key, "correct horse battery", public)
	whole := makeBackup(t, key, "correct horse battery")
	for _, c := range []struct {
		name    string
		db      bool
		den     []byte
		joined  int
		refused string
	}{
		{name: "no database yet"},
		{name: "an empty database", db: true},
		{name: "the same den", db: true, den: public},
		{name: "another den", db: true, den: other, refused: "instance main hosts the den Held; restoring a den's export replaces all"},
		{name: "joined dens", db: true, joined: 3, refused: "instance main has accounts in 3 dens; restoring"},
		{name: "the same den and an account", db: true, den: public, joined: 1,
			refused: "instance main hosts the den Held and has an account in 1 den; restoring"},
	} {
		t.Run(c.name, func(t *testing.T) {
			target := emptyTarget(t)
			if c.db {
				if err := holdIn(t, target, c.den, c.joined).Close(); err != nil {
					t.Fatal(err)
				}
			}
			staging := t.TempDir()
			_, _, err := openBackup(bi, export, staging, "correct horse battery", target)
			switch {
			case c.refused == "" && err != nil:
				t.Fatalf("the export: %v", err)
			case c.refused != "" && (err == nil || !strings.Contains(err.Error(), c.refused) || !strings.Contains(err.Error(), movingGuide)):
				t.Fatalf("the export: %v", err)
			}
			if _, statErr := os.Stat(backup.UploadsPath(staging)); c.refused != "" && !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("a refused export wrote its uploads: %v", statErr)
			}
			if _, _, err := openBackup(bi, whole, t.TempDir(), "correct horse battery", target); err != nil {
				t.Fatalf("a backup: %v", err)
			}
		})
	}
}

// Reading what an instance holds leaves nothing beside its database, as
// root must not (M10): while the service's connections hold it open, its
// WAL and index are read as they are, and once they've closed, the file
// alone. A WAL a crash left without its index isn't read.
func TestReadingHoldingsLeavesNothing(t *testing.T) {
	target := emptyTarget(t)
	public := testKey(t)
	db := holdIn(t, target, public, 2)
	path := filepath.Join(target.DB, database.FileName)
	ctx := context.Background()
	check := func(when string) {
		t.Helper()
		before := filesIn(t, target.DB)
		h, err := readHoldings(ctx, path)
		if err != nil || h.den != "Held" || h.joined != 2 || !bytes.Equal(h.denID, denproto.ID(public)) {
			t.Fatalf("%s: %+v %v", when, h, err)
		}
		if after := filesIn(t, target.DB); !slices.Equal(before, after) {
			t.Fatalf("%s, reading left %v where there were %v", when, after, before)
		}
	}
	check("held open")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if files := filesIn(t, target.DB); !slices.Equal(files, []string{database.FileName}) {
		t.Fatalf("closing left %v", files)
	}
	check("closed")

	if err := os.WriteFile(path+"-wal", []byte("frames a crash left"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readHoldings(ctx, path); !errors.Is(err, errCrashed) {
		t.Fatalf("a WAL without its index: %v", err)
	}
	if files := filesIn(t, target.DB); !slices.Equal(files, []string{database.FileName, database.FileName + "-wal"}) {
		t.Fatalf("reading a crash's WAL left %v", files)
	}
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
