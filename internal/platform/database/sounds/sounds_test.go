package sounds

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

// A member's own sound is kept sealed, by the sound it replaces, which its
// seal is bound to; replacing it again takes the old one's place, and
// deleting it brings the default back.
func TestOwnSounds(t *testing.T) {
	db, v := open(t)
	ctx := context.Background()
	if _, _, err := Open(ctx, db, v, "joined"); !errors.Is(err, ErrNone) {
		t.Fatalf("a sound never replaced opened with %v", err)
	}
	file := []byte("RIFF\x24\x00\x00\x00WAVEfmt a member's own recording")
	at := time.UnixMilli(1700000000000)
	version, err := Put(ctx, db, v, "joined", file, at)
	if err != nil || version != at.UnixMilli() {
		t.Fatalf("put: %d, %v", version, err)
	}
	got, gotVersion, err := Open(ctx, db, v, "joined")
	if err != nil || !bytes.Equal(got, file) || gotVersion != version {
		t.Fatalf("open: %q, %d, %v", got, gotVersion, err)
	}
	var sealed []byte
	if err := db.QueryRow(`SELECT file FROM sounds WHERE name = 'joined'`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("own recording")) {
		t.Fatal("the database holds the file unsealed")
	}
	// A file moved to another sound doesn't open there.
	if _, err := db.Exec(`INSERT INTO sounds (name, file, set_at) VALUES ('left', ?, 1)`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(ctx, db, v, "left"); err == nil || errors.Is(err, ErrNone) {
		t.Fatalf("a file moved to another sound opened: %v", err)
	}

	newer := []byte("OggS a newer file")
	if _, err := Put(ctx, db, v, "joined", newer, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	list, err := List(ctx, db)
	if err != nil || len(list) != 2 || list["joined"] != at.Add(time.Second).UnixMilli() || list["left"] != 1 {
		t.Fatalf("list: %v, %v", list, err)
	}
	if got, _, _ := Open(ctx, db, v, "joined"); !bytes.Equal(got, newer) {
		t.Fatalf("replaced again, it opens as %q", got)
	}
	if err := Delete(ctx, db, "joined"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(ctx, db, v, "joined"); !errors.Is(err, ErrNone) {
		t.Fatalf("deleted, it opens with %v", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM sounds WHERE name = 'joined'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("deleted, %d rows remain, %v", rows, err)
	}
}
