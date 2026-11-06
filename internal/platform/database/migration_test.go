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
	{"v1.6.0", 1, slices.Concat([]string{"sounds", "den_named_files", "den_reactions", "den_dm_reactions", "den_polls", "den_poll_votes"},
		m16Tables, m17Tables)},
	// v1.7.0 added M14.2's own sounds and M14.3's soundboard.
	{"v1.7.0", 3, slices.Concat([]string{"den_reactions", "den_dm_reactions", "den_polls", "den_poll_votes"}, m16Tables, m17Tables)},
	// v1.8.0 added M15.2's emoji, M15.3's reactions and M15.4's polls.
	{"v1.8.0", 6, slices.Concat(m16Tables, m17Tables)},
	// v1.9.0 added M16.1's identities, M16.3's group DMs and M16.4's
	// disappearing messages.
	{"v1.9.0", 9, m17Tables},
}

// m16Tables are the tables M16's steps add: M16.1's identities and
// records, and M16.3's group DMs.
var m16Tables = []string{"den_identities", "den_records", "den_group_dms", "den_group_members", "den_group_changes"}

// m17Tables are the tables M17's steps add: M17.4's events and who's
// interested in them.
var m17Tables = []string{"den_events", "den_event_interest"}

// retiredKeys are the tables of DM keys, which M16.1's step changes for a
// release before it: an exchange still running, or checked by one member
// alone, retires.
var retiredKeys = []string{"den_dm_keys", "den_dm_key_members"}

// A database a release left, with a row in every table, migrates to this
// build's schema keeping every row (M14.2, the first migration since
// v1.0.0), and so does a backup made of it, which a restore installs as
// it is: the steps before it never change, so running them alone makes the
// release's schema. A table keeps every column it had, with the same
// values; a step may add columns, as M14.3 adds the den's limits for its
// soundboard and M15.2 its limit for emoji, which hold their defaults, as
// does M15.4's mark of a message's @everyone and M16.1's identities, and
// tables, as M15.3 adds the reactions', M15.4 the polls', M16.1 the
// identities' and the records' and M16.3 the group DMs', which start
// empty. M16.1 retires the DM keys whose exchanges hadn't finished, and
// keeps every row of them. M16.3 makes den_channels again, with foreign
// keys off, which keeps every row that refers to a channel, and its
// counter where a restore moved it. M16.4 adds each DM's time, which
// starts off, and each message's, which none has; a database v1.9.0 left,
// with identities, a group DM and a time set, keeps them all. M17.2 adds a
// file's spoiler, which none is. M17.3 makes den_channels again to take
// notes, keeping every row, M16.4's times among them, and the counter, as
// M16.3's step did. M17.4 adds events and who's interested in them, which
// start empty, and the den's limit for members' events, which starts at 0,
// leaving them to staff.
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
					if released.version < 7 && slices.Contains(retiredKeys, table) {
						continue
					}
					if after := dumpColumns(t, db, table, columns[table]); after != before[table] {
						t.Errorf("%s's %s changed in the migration: before\n%s\nafter\n%s", what, table, before[table], after)
					}
				}
				if released.version < 7 {
					checkRetired(t, db, what)
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
				if err := db.QueryRow(`SELECT emojis FROM den`).Scan(&emojis); err != nil || (released.version < 4 && emojis != 50) {
					t.Errorf("%s's den keeps %d emoji, %v, not the default", what, emojis, err)
				}
				var marked, messages int
				if err := db.QueryRow(`SELECT count(*) FILTER (WHERE everyone != 0), count(*) FROM den_messages`).Scan(&marked, &messages); err != nil ||
					(released.version < 6 && marked != 0) || messages == 0 {
					t.Errorf("%s's %d messages hold %d marks of @everyone, %v", what, messages, marked, err)
				}
				var identities, statements, kept int
				if err := db.QueryRow(`SELECT (SELECT count(*) FROM den_members WHERE identity IS NOT NULL),
					(SELECT count(*) FROM den_dm_keys WHERE statement IS NOT NULL OR signature IS NOT NULL),
					(SELECT count(*) FROM joined_dens WHERE identities IS NOT NULL)`).Scan(&identities, &statements, &kept); err != nil ||
					(released.version < 7 && identities+statements+kept != 0) {
					t.Errorf("%s holds %d identities, %d signed keys and %d kept records after the migration, %v", what, identities, statements, kept, err)
				}
				// den_channels was made again, with group DMs among its kinds.
				if _, err := db.Exec(`INSERT INTO den_channels (name, kind, position, created_at) VALUES ('', 'group_dm', 0, 9)`); err != nil {
					t.Errorf("%s takes no group DM after the migration: %v", what, err)
				}
				var channel int64
				if err := db.QueryRow(`SELECT max(id) FROM den_channels`).Scan(&channel); err != nil || channel != 1700000000000001 {
					t.Errorf("%s gave a new channel ID %d, %v, not one past the counter", what, channel, err)
				}
				// And again with a member's notes among them (M17.3), one each,
				// and naming their member alone.
				if _, err := db.Exec(`INSERT INTO den_channels (name, kind, position, created_at, notes_of) VALUES ('', 'notes', 0, 9, 1)`); err != nil {
					t.Errorf("%s takes no notes after the migration: %v", what, err)
				}
				if err := db.QueryRow(`SELECT max(id) FROM den_channels`).Scan(&channel); err != nil || channel != 1700000000000002 {
					t.Errorf("%s gave the notes' channel ID %d, %v, not the next past the counter", what, channel, err)
				}
				for _, q := range []string{
					`INSERT INTO den_channels (name, kind, position, created_at, notes_of) VALUES ('', 'notes', 0, 9, 1)`,
					`INSERT INTO den_channels (name, kind, position, created_at) VALUES ('', 'notes', 0, 9)`,
					`INSERT INTO den_channels (name, kind, position, created_at, notes_of) VALUES ('x', 'text', 0, 9, 3)`,
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				// Nothing disappears until a member sets a time (M16.4).
				var timed, expiring int
				if err := db.QueryRow(`SELECT
					(SELECT count(*) FROM den_channels WHERE expires_after != 0 OR expires_by IS NOT NULL OR expires_set_at IS NOT NULL),
					(SELECT count(*) FROM den_messages WHERE expires_at IS NOT NULL)`).Scan(&timed, &expiring); err != nil ||
					(released.version < 9 && timed+expiring != 0) {
					t.Errorf("%s holds %d timed DMs and %d messages that disappear after the migration, %v", what, timed, expiring, err)
				}
				if _, err := db.Exec(`UPDATE den_channels SET expires_after = -1 WHERE id = 3`); err == nil {
					t.Errorf("%s takes a DM's time below 0", what)
				}
				// No file went as a spoiler before M17.2.
				var spoilers int
				if err := db.QueryRow(`SELECT count(*) FROM den_files WHERE spoiler != 0`).Scan(&spoilers); err != nil || spoilers != 0 {
					t.Errorf("%s holds %d spoilers after the migration, %v", what, spoilers, err)
				}
				if _, err := db.Exec(`UPDATE den_files SET spoiler = 2 WHERE id = 1`); err == nil {
					t.Errorf("%s takes a spoiler other than 0 or 1", what)
				}
				// Events are staff's until the owner says otherwise (M17.4),
				// and each is to come, live or ended, ending no sooner than
				// it starts.
				var memberEvents int
				if err := db.QueryRow(`SELECT member_events FROM den`).Scan(&memberEvents); err != nil || memberEvents != 0 {
					t.Errorf("%s's den lets each member have %d events, %v, not 0", what, memberEvents, err)
				}
				if _, err := db.Exec(`INSERT INTO den_events (details, channel_id, made_by, made_at, starts_at, ends_at) VALUES (x'00', 1, 1, 9, 10, 20)`); err != nil {
					t.Errorf("%s takes no event after the migration: %v", what, err)
				} else if _, err := db.Exec(`INSERT INTO den_event_interest (event_id, member_id) VALUES (last_insert_rowid(), 1)`); err != nil {
					t.Errorf("%s takes nobody's interest after the migration: %v", what, err)
				}
				for _, q := range []string{
					`UPDATE den SET member_events = 11`,
					`INSERT INTO den_events (details, made_by, made_at, starts_at, ends_at, state) VALUES (x'00', 1, 9, 10, 20, 'gone')`,
					`INSERT INTO den_events (details, made_by, made_at, starts_at, ends_at) VALUES (x'00', 1, 9, 20, 10)`,
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				var on int
				if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil || on != 1 {
					t.Errorf("%s has foreign keys %d after the migration, %v", what, on, err)
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

// checkRetired checks M16.1's step retired the DM key whose exchange one
// member alone had checked, by its starter, dropping its exchange and the
// states, and kept the one both members checked as it was, with every
// member's copy of each.
func checkRetired(t *testing.T, db *sql.DB, what string) {
	t.Helper()
	got := dumpQuery(t, db, `SELECT id, channel_id, started_by, started_at, stage, offer, answer, reveal,
		retired_at > 1000, retired_by, retired FROM den_dm_keys`)
	want := `['\x01' '\x03' '\x01' '\x01' "revealed" <nil> <nil> <nil> '\x01' '\x01' "restarted"]
['\x02' '\x04' '\x03' '\x02' "revealed" <nil> <nil> <nil> <nil> <nil> <nil>]`
	if got != want {
		t.Errorf("%s's DM keys after the migration:\n%s\nwant\n%s", what, got, want)
	}
	got = dumpTable(t, db, "den_dm_key_members")
	want = `['\x01' '\x01' <nil> "U" '\x02']
['\x02' '\x01' <nil> "V" '\x03']
['\x02' '\x03' <nil> "W" '\x04']`
	if got != want {
		t.Errorf("%s's copies of DM keys after the migration:\n%s\nwant\n%s", what, got, want)
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
// own and one on the den's soundboard, its limits not at their defaults;
// from version 6, v1.8.0's, an emoji, reactions in a channel and a DM, and
// a poll with @everyone and a vote; from version 9, v1.9.0's, a member's
// identity and record, a signed DM key, the identities a joined den kept,
// a group DM with its members and its first change, and a DM's time with a
// message sent under it.
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
		// A DM both members checked, whose key stays (M16.1).
		`INSERT INTO den_members (id, username, display_name, role, verifier_hash, joined_at, seal_check) VALUES (3, 'carol', 'Carol', 'member', x'17', 3, x'18')`,
		`INSERT INTO den_channels (id, name, kind, position, dm_low, dm_high, created_at) VALUES (4, '', 'dm', 0, 1, 3, 2)`,
		`INSERT INTO den_dm_keys (id, channel_id, started_by, started_at, stage) VALUES (2, 4, 3, 2, 'revealed')`,
		`INSERT INTO den_dm_key_members (key_id, member_id, sealed, checked_at) VALUES (2, 1, x'56', 3), (2, 3, x'57', 4)`,
		`INSERT INTO den_changes (id, kind, ref, channel_id, at) VALUES (1, 'message', 1, 1, 2)`,
		`INSERT INTO den_read_states (member_id, channel_id, message_id, closed) VALUES (1, 1, 1, 0)`,
		// A restore moved the channels' counter past their IDs (M11).
		`UPDATE sqlite_sequence SET seq = 1700000000000000 WHERE name = 'den_channels'`,
	}
	if version >= 3 {
		rows = append(rows,
			`INSERT INTO sounds (name, file, set_at) VALUES ('message', x'61', 3)`,
			`INSERT INTO den_files (id, blob, uploader_id, name, type, size, duration_ms, created_at) VALUES (3, x'45', 1, x'46', 'audio/ogg', 300, 2400, 3)`,
			`INSERT INTO den_named_files (id, kind, name, file_id, added_by, added_at) VALUES (1, 'sound', 'Air horn', 3, 1, 3)`,
			`UPDATE den SET sounds = 20, sound_longest = 30, sound_wait = 0`,
		)
	}
	if version >= 6 {
		rows = append(rows,
			`UPDATE den SET emojis = 20`,
			`INSERT INTO den_files (id, blob, uploader_id, name, type, size, width, height, created_at) VALUES (4, x'47', 1, x'48', 'image/png', 400, 64, 64, 4)`,
			`INSERT INTO den_named_files (id, kind, name, file_id, added_by, added_at) VALUES (2, 'emoji', 'party', 4, 1, 4)`,
			`INSERT INTO den_reactions (message_id, member_id, emoji, created_at) VALUES (1, 1, '👍', 4)`,
			`INSERT INTO den_reactions (message_id, member_id, emoji_id, created_at) VALUES (1, 2, 2, 5)`,
			`INSERT INTO den_dm_reactions (message_id, member_id, key_id, revision, sealed, updated_at) VALUES (2, 1, 1, 1, x'62', 5)`,
			`INSERT INTO den_messages (id, channel_id, author_id, created_at, text, nonce, everyone) VALUES (3, 1, 1, 6, x'35', x'36', 1)`,
			`INSERT INTO den_polls (message_id, choices, multiple, show_voters, show_totals, ends_at, ended) VALUES (3, x'63', 1, 'end', 'voted', 7, 0)`,
			`INSERT INTO den_poll_votes (message_id, member_id, choice, voted_at) VALUES (3, 2, 0, 6)`,
		)
	}
	if version >= 9 {
		rows = append(rows,
			`UPDATE den_members SET identity = x'71' WHERE id = 1`,
			`INSERT INTO den_identities (member_id, kem, sig) VALUES (1, x'72', x'73')`,
			`UPDATE den_dm_keys SET statement = x'74', signature = x'75' WHERE id = 2`,
			`INSERT INTO den_records (member_id, revision, sealed, updated_at) VALUES (1, 1, x'76', 7)`,
			`UPDATE joined_dens SET identities = x'77'`,
			`INSERT INTO den_channels (id, name, kind, position, created_at) VALUES (5, '', 'group_dm', 0, 7)`,
			`INSERT INTO den_group_dms (channel_id, made_by, made_at, name, name_kid) VALUES (5, 1, 7, x'78', x'79')`,
			`INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (5, 1, 0, 1, 0), (5, 3, 1, 1, 0)`,
			`INSERT INTO den_group_changes (channel_id, n, made_by, made_at, statement) VALUES (5, 1, 1, 7, x'7a')`,
			`UPDATE den_channels SET expires_after = 3600, expires_by = 1, expires_set_at = 7 WHERE id = 4`,
			`INSERT INTO den_messages (id, channel_id, author_id, created_at, text, nonce, key_id, expires_at) VALUES (4, 4, 1, 8, x'37', x'38', 2, 3600008)`,
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
