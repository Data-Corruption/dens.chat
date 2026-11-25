package database_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// A den v1.15.0 left with a group DM, of bob, alice and carol, beside a text
// channel and bob's DM with carol, holds nothing of the group once it's
// migrated and opened: its members and changes, its messages with their
// editors and mentions, its key with each member's copy, carol's
// reactions, everyone's read state, its files' rows, and its files on disk,
// which go as the den opens, again at every open after. The rest stays,
// and bob's space counts only his files that are left.
func TestGroupDMsGo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	old, err := database.Open(filepath.Join(dir, database.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := database.MigrateTo(ctx, old, log, 18); err != nil || v != 18 {
		old.Close()
		t.Fatalf("make v1.15.0's schema: version %d, %v", v, err)
	}
	blob := func(n byte) []byte { return append(make([]byte, 15), n) }
	rows := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO den_members (id, username, display_name, role, verifier_hash, joined_at, seal_check) VALUES
			(1, 'alice', 'Alice', 'owner', x'01', 1, x'02'), (2, 'bob', 'Bob', 'member', x'03', 1, x'04'), (3, 'carol', 'Carol', 'member', x'05', 1, x'06')`, nil},
		{`INSERT INTO den_channels (id, name, kind, position, created_at) VALUES (1, 'general', 'text', 0, 1)`, nil},
		{`INSERT INTO den_channels (id, name, kind, position, dm_low, dm_high, created_at) VALUES (2, '', 'dm', 0, 2, 3, 1)`, nil},
		{`INSERT INTO den_channels (id, name, kind, position, created_at) VALUES (3, '', 'group_dm', 0, 2)`, nil},
		{`INSERT INTO den_group_dms (channel_id, made_by, made_at, name, name_kid) VALUES (3, 2, 2, x'10', x'11')`, nil},
		{`INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (3, 2, 0, 1, 0), (3, 1, 1, 1, 0), (3, 3, 2, 1, 0)`, nil},
		{`INSERT INTO den_group_changes (channel_id, n, made_by, made_at, statement) VALUES (3, 1, 2, 2, x'7b7d')`, nil},
		{`INSERT INTO den_dm_keys (id, channel_id, started_by, started_at, stage, statement, signature) VALUES
			(1, 2, 2, 1, 'revealed', x'7b7d', x'20'), (2, 3, 2, 2, 'revealed', x'7b7d', x'21')`, nil},
		{`INSERT INTO den_dm_key_members (key_id, member_id, sealed, checked_at) VALUES
			(1, 2, x'30', 1), (1, 3, x'31', 1), (2, 1, x'32', 2), (2, 2, x'33', 2), (2, 3, x'34', 2)`, nil},
		{`INSERT INTO den_messages (id, channel_id, author_id, created_at, text, nonce, key_id) VALUES
			(1, 1, 2, 1, x'40', x'41', NULL), (2, 2, 2, 2, x'42', x'43', 1), (3, 3, 2, 3, x'44', x'45', 2)`, nil},
		{`INSERT INTO den_message_editors (message_id, member_id) VALUES (3, 1), (3, 3)`, nil},
		{`INSERT INTO den_mentions (member_id, channel_id, message_id) VALUES (3, 2, 2), (1, 3, 3), (3, 3, 3)`, nil},
		{`INSERT INTO den_files (id, blob, uploader_id, message_id, position, name, type, size, thumb_width, thumb_height, thumb_size, sealed, created_at)
			VALUES (1, ?, 2, 1, 0, x'50', 'image/png', 100, 5, 5, 10, 0, 1)`, []any{blob(1)}},
		{`INSERT INTO den_files (id, blob, uploader_id, message_id, position, name, type, size, sealed, created_at) VALUES
			(2, ?, 2, 2, 0, x'51', 'application/octet-stream', 200, 1, 2),
			(3, ?, 2, 3, 0, x'52', 'application/octet-stream', 300, 1, 3),
			(4, ?, 2, 3, 1, x'53', 'application/octet-stream', 40, 1, 3)`, []any{blob(2), blob(3), blob(4)}},
		{`INSERT INTO den_dm_reactions (message_id, member_id, key_id, revision, sealed, updated_at) VALUES (2, 3, 1, 1, x'60', 3), (3, 3, 2, 1, x'61', 3)`, nil},
		{`INSERT INTO den_read_states (member_id, channel_id, message_id) VALUES (2, 1, 1), (2, 2, 2), (2, 3, 3), (1, 3, 3)`, nil},
		{`INSERT INTO den_changes (kind, ref, channel_id, at) VALUES ('message', 2, 2, 3), ('message', 3, 3, 3)`, nil},
	}
	for _, r := range rows {
		if _, err := old.Exec(r.q, r.args...); err != nil {
			old.Close()
			t.Fatalf("%s: %v", r.q, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	storage := den.Storage{Dir: filepath.Join(t.TempDir(), "uploads"), Temp: t.TempDir()}
	if err := os.MkdirAll(storage.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := func(n byte) string { return hex.EncodeToString(blob(n)) }
	for _, f := range []string{name(1), name(1) + ".thumb", name(2), name(3), name(4)} {
		if err := os.WriteFile(filepath.Join(storage.Dir, f), []byte("sealed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	db, err := database.New(dir, log, build.BuildInfo{DefaultLogLevel: "warn"}, database.ApplyPendingMigrations)
	if err != nil {
		t.Fatalf("migrate v1.15.0's den: %v", err)
	}
	defer db.Close()
	v, err := vault.New(denproto.Random(vault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	for i := range 2 {
		d, err := den.Open(ctx, db, v, log, storage)
		if err != nil {
			t.Fatalf("open the den, time %d: %v", i+1, err)
		}
		d.Close()
		stored, err := os.ReadDir(storage.Dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range stored {
			names = append(names, e.Name())
		}
		if want := []string{name(1), name(1) + ".thumb", name(2)}; !slices.Equal(names, want) {
			t.Errorf("after open %d the den stores %v, want %v", i+1, names, want)
		}
	}

	for table, want := range map[string]string{
		"den_channels":        `SELECT group_concat(id) FROM (SELECT id FROM den_channels ORDER BY id)`,
		"den_messages":        `SELECT group_concat(id) FROM (SELECT id FROM den_messages ORDER BY id)`,
		"den_files":           `SELECT group_concat(id) FROM (SELECT id FROM den_files ORDER BY id)`,
		"den_dm_keys":         `SELECT group_concat(id) FROM (SELECT id FROM den_dm_keys ORDER BY id)`,
		"den_dm_key_members":  `SELECT group_concat(key_id || ':' || member_id) FROM (SELECT * FROM den_dm_key_members ORDER BY key_id, member_id)`,
		"den_dm_reactions":    `SELECT group_concat(message_id) FROM den_dm_reactions`,
		"den_message_editors": `SELECT count(*) FROM den_message_editors`,
		"den_mentions":        `SELECT group_concat(channel_id) FROM den_mentions`,
		"den_read_states":     `SELECT group_concat(channel_id) FROM (SELECT channel_id FROM den_read_states ORDER BY channel_id)`,
		"den_changes":         `SELECT group_concat(channel_id) FROM den_changes WHERE kind = 'message'`,
	} {
		var got sql.NullString
		if err := db.QueryRow(want).Scan(&got); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		expect := map[string]string{"den_channels": "1,2", "den_messages": "1,2", "den_files": "1,2", "den_dm_keys": "1",
			"den_dm_key_members": "1:2,1:3", "den_dm_reactions": "2", "den_message_editors": "0", "den_mentions": "2",
			"den_read_states": "1,2", "den_changes": "2"}[table]
		if got.String != expect {
			t.Errorf("%s after the migration holds %q, want %q", table, got.String, expect)
		}
	}
	tables, err := database.Tables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"den_group_dms", "den_group_members", "den_group_changes"} {
		if slices.Contains(tables, gone) {
			t.Errorf("%s is still there", gone)
		}
	}
	var used int64
	if err := db.QueryRow(`SELECT sum(size + coalesce(thumb_size, 0)) FROM den_files WHERE uploader_id = 2`).Scan(&used); err != nil || used != 310 {
		t.Errorf("bob's files take %d bytes, %v; want 310", used, err)
	}
	for _, check := range []string{"PRAGMA foreign_key_check", "PRAGMA integrity_check"} {
		var got string
		err := db.QueryRow(check).Scan(&got)
		if err != nil && err != sql.ErrNoRows || err == nil && got != "ok" {
			t.Errorf("%s after the migration: %q, %v", check, got, err)
		}
	}
}
