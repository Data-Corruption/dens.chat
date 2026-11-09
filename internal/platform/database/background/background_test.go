package background

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func open(t *testing.T) (*sql.DB, *vault.Vault) {
	t.Helper()
	dir := t.TempDir()
	log, err := xlog.New(filepath.Join(dir, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	db, err := database.New(filepath.Join(dir, "db"), log, build.BuildInfo{DefaultLogLevel: "warn"}, database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	v, err := vault.New(denproto.Random(vault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	return db, v
}

// The chat's background is kept sealed, one at a time: another takes its
// place under a newer version, and deleting it leaves none.
func TestBackground(t *testing.T) {
	db, v := open(t)
	ctx := context.Background()
	if _, err := Open(ctx, db, v); !errors.Is(err, ErrNone) {
		t.Fatalf("a background never set opened with %v", err)
	}
	if version, err := Version(ctx, db); err != nil || version != 0 {
		t.Fatalf("a background never set has version %d, %v", version, err)
	}
	file := []byte("\x89PNG\r\n\x1a\n a member's own picture")
	at := time.UnixMilli(1700000000000)
	version, err := Put(ctx, db, v, file, at)
	if err != nil || version != at.UnixMilli() {
		t.Fatalf("put: %d, %v", version, err)
	}
	if got, err := Open(ctx, db, v); err != nil || !bytes.Equal(got, file) {
		t.Fatalf("open: %q, %v", got, err)
	}
	if got, err := Version(ctx, db); err != nil || got != version {
		t.Fatalf("version: %d, %v", got, err)
	}
	var sealed []byte
	if err := db.QueryRow(`SELECT file FROM background`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("own picture")) {
		t.Fatal("the database holds the file unsealed")
	}

	newer := []byte("RIFF a newer picture")
	if _, err := Put(ctx, db, v, newer, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got, _ := Open(ctx, db, v); !bytes.Equal(got, newer) {
		t.Fatalf("set again, it opens as %q", got)
	}
	if got, _ := Version(ctx, db); got != at.Add(time.Second).UnixMilli() {
		t.Fatalf("set again, its version is %d", got)
	}
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM background`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("set again, %d rows, %v", rows, err)
	}
	if err := Delete(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, db, v); !errors.Is(err, ErrNone) {
		t.Fatalf("deleted, it opens with %v", err)
	}
	if got, err := Version(ctx, db); err != nil || got != 0 {
		t.Fatalf("deleted, its version is %d, %v", got, err)
	}
}
