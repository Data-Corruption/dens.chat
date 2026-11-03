package database

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// releasedSchema is the schema version each release left its databases at,
// newest last, which later builds must migrate from.
var releasedSchema = []struct {
	release string
	version int
	// added are the tables later steps make, which the release lacks.
	added []string
}{
	// v1.0.0 through v1.6.0 shipped the initial step alone.
	{"v1.6.0", 1, []string{"sounds", "den_named_files", "den_reactions", "den_dm_reactions"}},
	// v1.7.0 added M14.2's own sounds and M14.3's soundboard.
	{"v1.7.0", 3, []string{"den_reactions", "den_dm_reactions"}},
}

// A database a release left, with a row in every table, migrates to this
// build's schema keeping every row (M14.2, the first migration since
// v1.0.0), and so does a backup made of it, which a restore installs as
// it is: the steps before it never change, so running them alone makes the
// release's schema. A table keeps every column it had, with the same
// values; a step may add columns, as M14.3 adds the den's limits for its
// soundboard and M15.2 its limit for emoji, which hold their defaults, and
// tables, as M15.3 adds the reactions', which start empty.
func TestMigrationFromReleasedSchemasKeepsEveryRow(t *testing.T) {
	bi := build.BuildInfo{DefaultLogLevel: "warn"}
	for _, released := range releasedSchema {
		t.Run(released.release, func(t *testing.T) {
			dir, backedUp := t.TempDir(), t.TempDir()
			log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { log.Close() })
			ctx := context.Background()

			old, err := Open(filepath.Join(dir, FileName))
			if err != nil {
				t.Fatal(err)
			}
			if v, err := newMigrator(bi).Upto(released.version).Run(ctx, old, log); err != nil || v != released.version {
				old.Close()
				t.Fatalf("make %s's schema: version %d, %v", released.release, v, err)
			}
			fillEveryTable(t, old, released.version)
			tables, err := Tables(ctx, old)
			if err != nil {
				t.Fatal(err)
			}
			// The tables' counters too, since an ID is never given out twice.
			tables = append(tables, "sqlite_sequence")
			columns := map[string]string{}
			before := map[string]string{}
			for _, table := range tables {
				columns[table] = columnsOf(t, old, table)
				if before[table] = dumpColumns(t, old, table, columns[table]); before[table] == "" {
					t.Errorf("%s's %s holds no row to keep", released.release, table)
				}
			}
			// A backup holds a copy of the database, made as a backup is.
			if err := Snapshot(ctx, old, filepath.Join(backedUp, FileName)); err != nil {
				t.Fatal(err)
			}
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}

			for what, at := range map[string]string{"the database": dir, "its backup": backedUp} {
				db, err := New(at, log, bi, ApplyPendingMigrations)
				if err != nil {
					t.Fatalf("migrate %s from %s: %v", what, released.release, err)
				}
				defer db.Close()
				if v, err := FileSchemaVersion(ctx, db); err != nil || v != SchemaVersion(bi) {
					t.Fatalf("%s migrated to schema %d, %v; want %d", what, v, err, SchemaVersion(bi))
				}
				for _, table := range tables {
					if after := dumpColumns(t, db, table, columns[table]); after != before[table] {
						t.Errorf("%s's %s changed in the migration: before\n%s\nafter\n%s", what, table, before[table], after)
					}
				}
				now, err := Tables(ctx, db)
				if err != nil {
					t.Fatal(err)
				}
				for _, table := range released.added {
					if !slices.Contains(now, table) || dumpTable(t, db, table) != "" {
						t.Errorf("after the migration %s's tables are %v, %s holding %q", what, now, table, dumpTable(t, db, table))
					}
				}
				var sounds, longest, wait int
				if err := db.QueryRow(`SELECT sounds, sound_longest, sound_wait FROM den`).Scan(&sounds, &longest, &wait); err != nil {
					t.Fatal(err)
				}
				if released.version < 3 && (sounds != 50 || longest != 10 || wait != 5) {
					t.Errorf("%s's den has the soundboard's limits %d, %d and %d, not the defaults", what, sounds, longest, wait)
				}
				var emojis int
				if err := db.QueryRow(`SELECT emojis FROM den`).Scan(&emojis); err != nil || emojis != 50 {
					t.Errorf("%s's den keeps %d emoji, %v, not the default", what, emojis, err)
				}
				for _, check := range []string{"PRAGMA foreign_key_check", "PRAGMA integrity_check"} {
					if got := dumpQuery(t, db, check); got != "" && got != `["ok"]` {
						t.Errorf("%s after the migration: %s", check, got)
					}
				}
			}
		})
	}
}

// columnsOf lists a table's columns, quoted for a query.
func columnsOf(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, `"`+strings.ReplaceAll(name, `"`, `""`)+`"`)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(names, ", ")
}

// dumpColumns lists a table's rows in some of its columns, as dumpTable
// does.
func dumpColumns(t *testing.T, db *sql.DB, table, columns string) string {
	t.Helper()
	return dumpQuery(t, db, `SELECT `+columns+` FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`)
}

// fillEveryTable puts a row in each table of a schema, joined as the den's
// and the client's rows are: a member with a device, a session and a
// recovery code, an invite, a group and channels, a message with a
// mention, an editor and a file, a DM's key, a change and a read position;
// the client's joined den, browser session and vault; and the settings
// the first step wrote. From version 3, v1.7.0's, a sound of the member's
// own and one on the den's soundboard, its limits not at their defaults.
func fillEveryTable(t *testing.T, db *sql.DB, version int) {
	t.Helper()
	rows := []string{
		`INSERT INTO vault (id, check_value, password_wrap, updated_at, dm_seal) VALUES (1, x'0102', '{"v":1}', 1, x'0304')`,
		`INSERT INTO client_sessions (token_hash, created_at, expires_at, last_seen_at) VALUES ('hash', 1, 2, 1)`,
		`INSERT INTO joined_dens (den_id, profile, device_key, joined_at, dm_seal) VALUES (x'0a', x'0b', x'0c', 1, x'0d')`,
		`INSERT INTO den (id, name, url, public_key, private_key, created_at, file_size, member_storage, den_storage,
			call_members, den_callers, shares, share_viewers, share_bitrate, share_height, share_fps, retention, sync_epoch, changes_floor, expired_through)
			VALUES (1, 'Den', 'https://den.test', x'01', x'02', 1, 10, 20, 30, 15, 50, 1, 10, 2500000, 1080, 30, 7, x'03', 4, 5)`,
		`INSERT INTO den_invites (id, code_hash, role, created_by, created_at, expires_at, max_uses, uses) VALUES (1, x'11', 'owner', NULL, 1, 2, 1, 1)`,
		`INSERT INTO den_members (id, username, display_name, role, verifier_hash, bio, joined_at, invite_id, seal_check)
			VALUES (1, 'alice', 'Alice', 'owner', x'12', x'13', 1, 1, x'14')`,
		`INSERT INTO den_members (id, username, display_name, role, verifier_hash, joined_at, left_at, banned_at, banned_by, seal_check)
			VALUES (2, 'bob', 'Bob', 'member', x'15', 2, 3, 3, 1, x'16')`,
		`INSERT INTO den_devices (key_id, member_id, public_key, label, created_at, last_seen_at) VALUES (x'21', 1, x'22', 'Laptop (Linux)', 1, 2)`,
		`INSERT INTO den_recovery_codes (code_hash, member_id) VALUES (x'23', 1)`,
		`INSERT INTO den_sessions (token_hash, member_id, key_id, created_at, expires_at) VALUES (x'24', 1, x'21', 1, 2)`,
		`INSERT INTO den_groups (id, name, position) VALUES (1, 'Group', 0)`,
		`INSERT INTO den_channels (id, group_id, name, kind, position, created_at) VALUES (1, 1, 'general', 'text', 0, 1)`,
		`INSERT INTO den_channels (id, name, kind, position, created_at, bitrate) VALUES (2, 'Lounge', 'voice', 1, 1, 96000)`,
		`INSERT INTO den_channels (id, name, kind, position, dm_low, dm_high, created_at) VALUES (3, '', 'dm', 0, 1, 2, 1)`,
		`INSERT INTO den_messages (id, channel_id, author_id, created_at, revision, edited_at, edited_by, text, reply_to, nonce)
			VALUES (1, 1, 1, 1, 2, 2, 1, x'31', NULL, x'32')`,
		`INSERT INTO den_messages (id, channel_id, author_id, created_at, text, reply_to, nonce, key_id) VALUES (2, 3, 2, 2, x'33', 1, x'34', 1)`,
		`INSERT INTO den_mentions (member_id, channel_id, message_id) VALUES (2, 1, 1)`,
		`INSERT INTO den_message_editors (message_id, member_id) VALUES (1, 2)`,
		`INSERT INTO den_files (id, blob, uploader_id, message_id, position, name, type, size, width, height, animated, thumb_width, thumb_height, thumb_size, duration_ms, sealed, created_at)
			VALUES (1, x'41', 1, 1, 0, x'42', 'image/png', 100, 10, 20, 0, 5, 10, 50, NULL, 0, 1)`,
		`INSERT INTO den_files (id, blob, uploader_id, name, type, size, created_at, replaces) VALUES (2, x'43', 1, x'44', 'audio', 200, 2, 1)`,
		`UPDATE den_members SET avatar_id = 1, banner_id = 1 WHERE id = 1`,
		`INSERT INTO den_dm_keys (id, channel_id, started_by, started_at, stage, offer, answer, reveal) VALUES (1, 3, 1, 1, 'revealed', x'51', x'52', x'53')`,
		`INSERT INTO den_dm_key_members (key_id, member_id, state, sealed, checked_at) VALUES (1, 1, x'54', x'55', 2)`,
		`INSERT INTO den_changes (id, kind, ref, channel_id, at) VALUES (1, 'message', 1, 1, 2)`,
		`INSERT INTO den_read_states (member_id, channel_id, message_id, closed) VALUES (1, 1, 1, 0)`,
	}
	if version >= 3 {
		rows = append(rows,
			`INSERT INTO sounds (name, file, set_at) VALUES ('message', x'61', 3)`,
			`INSERT INTO den_files (id, blob, uploader_id, name, type, size, duration_ms, created_at) VALUES (3, x'45', 1, x'46', 'audio/ogg', 300, 2400, 3)`,
			`INSERT INTO den_named_files (id, kind, name, file_id, added_by, added_at) VALUES (1, 'sound', 'Air horn', 3, 1, 3)`,
			`UPDATE den SET sounds = 20, sound_longest = 30, sound_wait = 0`,
		)
	}
	for _, q := range rows {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// dumpTable lists a table's rows, in order, as text.
func dumpTable(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	return dumpQuery(t, db, `SELECT * FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`)
}

func dumpQuery(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%q", values))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}
