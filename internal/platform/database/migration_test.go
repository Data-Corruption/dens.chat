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
		m16Tables, m17Tables, m19Tables, m23Tables, m25Tables)},
	// v1.7.0 added own sounds and the soundboard.
	{"v1.7.0", 3, slices.Concat([]string{"den_reactions", "den_dm_reactions", "den_polls", "den_poll_votes"}, m16Tables, m17Tables, m19Tables, m23Tables, m25Tables)},
	// v1.8.0 added emoji, reactions and polls.
	{"v1.8.0", 6, slices.Concat(m16Tables, m17Tables, m19Tables, m23Tables, m25Tables)},
	// v1.9.0 added identities, group DMs and disappearing messages.
	{"v1.9.0", 9, slices.Concat(m17Tables, m19Tables, m23Tables, m25Tables)},
	// v1.10.0 added spoilers, notes, events, badges and hidden ones, and
	// v1.10.1 nothing more.
	{"v1.10.1", 14, slices.Concat(m19Tables, m23Tables, m25Tables)},
	// v1.11.0 added the chat background and name styles, and v1.12.0
	// nothing more.
	{"v1.12.0", 16, slices.Concat(m23Tables, m25Tables)},
	// v1.13.0 added call records, and v1.14.0 nothing more.
	{"v1.14.0", 17, slices.Concat(m23Tables, m25Tables)},
	// v1.15.0 added the audit log.
	{"v1.15.0", 18, m25Tables},
	// v1.16.0 added forwards and statuses, and removed group DMs.
	{"v1.16.0", 22, m25Tables},
}

// m16Tables are the tables v1.9.0's steps add that stay: identities and
// records.
var m16Tables = []string{"den_identities", "den_records"}

// groupTables are the tables of group DMs, which v1.9.0's steps add and No
// group DMs drops.
var groupTables = []string{"den_group_dms", "den_group_members", "den_group_changes"}

// kept dumps the rows of a table that a migration from a release keeps:
// all but a group DM's, which No group DMs deletes, its channel and its
// tables, whose counter goes with them.
func kept(t *testing.T, db *sql.DB, table, columns string) string {
	t.Helper()
	switch {
	case slices.Contains(groupTables, table):
		return ""
	case table == "den_channels":
		return dumpQuery(t, db, `SELECT `+columns+` FROM den_channels WHERE kind != 'group_dm'`)
	case table == "sqlite_sequence":
		return dumpQuery(t, db, `SELECT `+columns+` FROM sqlite_sequence WHERE name NOT IN ('den_group_changes')`)
	}
	return dumpColumns(t, db, table, columns)
}

// m17Tables are the tables v1.10.0's steps add: events and who's
// interested in them, and badges and their awards.
var m17Tables = []string{"den_events", "den_event_interest", "den_badges", "den_badge_awards"}

// m19Tables are the tables v1.11.0's steps add: the chat background.
// Name styles are a column.
var m19Tables = []string{"background"}

// m23Tables are the tables v1.15.0's steps add: the audit log.
var m23Tables = []string{"den_audit"}

// m25Tables are the tables Channel roles and Collections add: roles,
// collections and their links.
var m25Tables = []string{"den_roles", "den_member_roles", "den_channel_roles", "den_event_roles", "den_collections", "den_collection_items"}

// retiredKeys are the tables of DM keys, which Identities changes for a
// release before it: an exchange still running, or checked by one member
// alone, retires.
var retiredKeys = []string{"den_dm_keys", "den_dm_key_members"}

// A database a release left, with a row in every table, migrates to this
// build's schema keeping every row, and so does a backup made of it, which
// a restore installs as it is: the steps before it never change, so running
// them alone makes the release's schema. Each step goes by the name
// migration.go adds it under.
// A table keeps every column it had, with the same values; a step may add
// columns, as Soundboard adds the den's limits for its sounds and Emoji its
// limit for emoji, which hold their defaults, as do Polls' mark of a
// message's @everyone and Identities' column for a member's identity, and
// tables, as Reactions, Polls, Identities (the records' too) and Group DMs
// add theirs, which start empty. Identities retires the DM keys whose
// exchanges hadn't finished, and keeps every row of them. Group DMs makes
// den_channels again, with foreign keys off, which keeps every row that
// refers to a channel, and its counter where a restore moved it.
// Disappearing messages adds each DM's time, which starts off, and each
// message's, which none has; a database v1.9.0 left, with identities, a
// group DM and a time set, keeps them all. Spoilers adds a file's spoiler,
// which none is. Notes makes den_channels again to take notes, keeping
// every row, the DMs' times among them, and the counter, as Group DMs did.
// Events adds events and who's interested in them, which start empty, and
// the den's limit for members' events, which starts at 0, leaving them to
// staff. Badges adds badges and their awards, which start empty, and an
// event's staff-only mark, which none has; and Hidden badges an award's
// hidden mark, which an award starts without; a database v1.10.1 left,
// with a spoiler, notes, an event, a badge and a hidden award, keeps them
// all. Chat background adds the chat's background, which starts with none,
// and Name styles each member's name style, which none has; a database
// v1.12.0 left, with a background and a name style, keeps them. Call
// records adds each voice channel's longest call, which none has; a
// database v1.14.0 left, with a record, keeps it. Audit log adds staff's
// lines, which start empty, each by and about members alone; a database
// v1.15.0 left, with a line, keeps it. Group name signatures adds the
// signature beside a group's name, Forwards a message's forwarded mark,
// which none has, No group DMs deletes the group DM and drops its
// tables, the signatures' column among them, keeping every other row, and
// Statuses adds each member's status and its end, which none has.
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
				if dumpColumns(t, old, table, columns[table]) == "" {
					t.Errorf("%s's %s holds no row to keep", released.release, table)
				}
				before[table] = kept(t, old, table, columns[table])
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
					if released.version < 7 && slices.Contains(retiredKeys, table) || slices.Contains(groupTables, table) {
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
				for _, table := range groupTables {
					if slices.Contains(now, table) {
						t.Errorf("after the migration %s still has %s", what, table)
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
				// den_channels was made again, with group DMs among its kinds,
				// which it keeps.
				if _, err := db.Exec(`INSERT INTO den_channels (name, kind, position, created_at) VALUES ('', 'group_dm', 0, 9)`); err != nil {
					t.Errorf("%s takes no group DM after the migration: %v", what, err)
				}
				var channel int64
				if err := db.QueryRow(`SELECT max(id) FROM den_channels`).Scan(&channel); err != nil || channel != 1700000000000001 {
					t.Errorf("%s gave a new channel ID %d, %v, not one past the counter", what, channel, err)
				}
				// And again with a member's notes among them, one each,
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
				// Nothing disappears until a member sets a time.
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
				// No file went as a spoiler before v1.10.0.
				var spoilers int
				if err := db.QueryRow(`SELECT count(*) FROM den_files WHERE spoiler != 0`).Scan(&spoilers); err != nil ||
					(released.version < 14 && spoilers != 0) {
					t.Errorf("%s holds %d spoilers after the migration, %v", what, spoilers, err)
				}
				if _, err := db.Exec(`UPDATE den_files SET spoiler = 2 WHERE id = 1`); err == nil {
					t.Errorf("%s takes a spoiler other than 0 or 1", what)
				}
				// Events are staff's until the owner says otherwise,
				// and each is to come, live or ended, ending no sooner than
				// it starts.
				var memberEvents int
				if err := db.QueryRow(`SELECT member_events FROM den`).Scan(&memberEvents); err != nil || (released.version < 14 && memberEvents != 0) {
					t.Errorf("%s's den lets each member have %d events, %v, not 0", what, memberEvents, err)
				}
				if _, err := db.Exec(`INSERT INTO den_events (details, channel_id, made_by, made_at, starts_at, ends_at) VALUES (x'00', 1, 1, 9, 10, 20)`); err != nil {
					t.Errorf("%s takes no event after the migration: %v", what, err)
				} else if _, err := db.Exec(`INSERT INTO den_event_interest (event_id, member_id) VALUES (last_insert_rowid(), 1)`); err != nil {
					t.Errorf("%s takes nobody's interest after the migration: %v", what, err)
				}
				// A badge is an event's, its picture a named file, and an award
				// one member's, once.
				var event int64
				if err := db.QueryRow(`SELECT max(id) FROM den_events`).Scan(&event); err != nil {
					t.Fatal(err)
				}
				for _, q := range []string{
					`INSERT INTO den_files (id, blob, uploader_id, name, type, size, width, height, created_at) VALUES (90, x'49', 1, x'4a', 'image/png', 500, 128, 128, 9)`,
					`INSERT INTO den_named_files (id, kind, name, file_id, added_by, added_at) VALUES (90, 'badge', '` + fmt.Sprint(event) + `', 90, 1, 9)`,
					`INSERT INTO den_badges (event_id, named_id, title) VALUES (` + fmt.Sprint(event) + `, 90, x'7b')`,
					`INSERT INTO den_badge_awards (event_id, member_id, earned_at) VALUES (` + fmt.Sprint(event) + `, 1, 10)`,
					`UPDATE den_events SET staff_only = 1 WHERE id = ` + fmt.Sprint(event),
				} {
					if _, err := db.Exec(q); err != nil {
						t.Errorf("%s took no badge after the migration: %s: %v", what, q, err)
					}
				}
				var staffEvents int
				if err := db.QueryRow(`SELECT count(*) FROM den_events WHERE staff_only != 0 AND id != ?`, event).Scan(&staffEvents); err != nil ||
					(released.version < 14 && staffEvents != 0) {
					t.Errorf("%s holds %d staff-only events of its own after the migration, %v", what, staffEvents, err)
				}
				// An award starts shown, and its member hides it or not.
				var hidden int
				if err := db.QueryRow(`SELECT hidden FROM den_badge_awards WHERE event_id = ?`, event).Scan(&hidden); err != nil || hidden != 0 {
					t.Errorf("%s's award starts with hidden %d, %v", what, hidden, err)
				}
				if _, err := db.Exec(`UPDATE den_badge_awards SET hidden = 1 WHERE event_id = ?`, event); err != nil {
					t.Errorf("%s hides no award after the migration: %v", what, err)
				}
				for _, q := range []string{
					`UPDATE den SET member_events = 11`,
					`INSERT INTO den_events (details, made_by, made_at, starts_at, ends_at, state) VALUES (x'00', 1, 9, 10, 20, 'gone')`,
					`INSERT INTO den_events (details, made_by, made_at, starts_at, ends_at) VALUES (x'00', 1, 9, 20, 10)`,
					`INSERT INTO den_badge_awards (event_id, member_id, earned_at) VALUES (` + fmt.Sprint(event) + `, 1, 11)`,
					`INSERT INTO den_badges (event_id, named_id, title) VALUES (` + fmt.Sprint(event) + `, 2, x'7c')`,
					`UPDATE den_events SET staff_only = 2 WHERE id = ` + fmt.Sprint(event),
					`UPDATE den_badge_awards SET hidden = 2 WHERE event_id = ` + fmt.Sprint(event),
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				// No name has a style until its member sets one.
				var styled int
				if err := db.QueryRow(`SELECT count(*) FROM den_members WHERE name_style IS NOT NULL`).Scan(&styled); err != nil ||
					(released.version < 16 && styled != 0) {
					t.Errorf("%s holds %d name styles after the migration, %v", what, styled, err)
				}
				if _, err := db.Exec(`UPDATE den_members SET name_style = '{"font":"pacifico"}' WHERE id = 1`); err != nil {
					t.Errorf("%s takes no name style after the migration: %v", what, err)
				}
				// No voice channel has a record until a call there ends,
				// and only a voice channel takes one, longer than 0.
				var records int
				if err := db.QueryRow(`SELECT count(*) FROM den_channels WHERE call_record IS NOT NULL`).Scan(&records); err != nil ||
					(released.version < 17 && records != 0) {
					t.Errorf("%s holds %d call records after the migration, %v", what, records, err)
				}
				if _, err := db.Exec(`UPDATE den_channels SET call_record = 5000 WHERE id = 2`); err != nil {
					t.Errorf("%s takes no call record after the migration: %v", what, err)
				}
				for _, q := range []string{
					`UPDATE den_channels SET call_record = 5000 WHERE id = 1`,
					`UPDATE den_channels SET call_record = 0 WHERE id = 2`,
					`UPDATE den_channels SET call_record = -1 WHERE id = 2`,
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				// A line is when, what, who did it and to whom, and the rest
				// sealed; it names members alone, whose rows stay, so it
				// outlives the channel, message or invite it was about.
				if _, err := db.Exec(`INSERT INTO den_audit (at, kind, done_by, member_id, details) VALUES (12, 'member.removed', 1, 2, x'a1')`); err != nil {
					t.Errorf("%s takes no audit line after the migration: %v", what, err)
				}
				if _, err := db.Exec(`INSERT INTO den_audit (at, kind, done_by, details) VALUES (13, 'channel.deleted', 1, x'a2')`); err != nil {
					t.Errorf("%s takes no line about no member after the migration: %v", what, err)
				}
				// The den's machine acts through its owner invite.
				if _, err := db.Exec(`INSERT INTO den_audit (at, kind, member_id, details) VALUES (14, 'role.changed', 1, x'a4')`); err != nil {
					t.Errorf("%s takes no line by the den's machine after the migration: %v", what, err)
				}
				for _, q := range []string{
					`INSERT INTO den_audit (at, kind, done_by, member_id, details) VALUES (12, 'member.removed', 99, 2, x'a3')`,
					`INSERT INTO den_audit (at, kind, done_by, member_id, details) VALUES (12, 'member.removed', 1, 99, x'a3')`,
					`INSERT INTO den_audit (at, kind, done_by, details) VALUES (12, 'channel.deleted', 1, NULL)`,
					`INSERT INTO den_audit (at, kind, done_by, details) VALUES (NULL, 'channel.deleted', 1, x'a3')`,
					`INSERT INTO den_audit (at, kind, done_by, details) VALUES (12, NULL, 1, x'a3')`,
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				// No message is forwarded until a member forwards one,
				// and a mark is 0 or 1.
				var forwarded int
				if err := db.QueryRow(`SELECT count(*) FROM den_messages WHERE forwarded != 0`).Scan(&forwarded); err != nil ||
					(released.version < 20 && forwarded != 0) {
					t.Errorf("%s holds %d forwarded messages after the migration, %v", what, forwarded, err)
				}
				if _, err := db.Exec(`UPDATE den_messages SET forwarded = 1 WHERE id = 1`); err != nil {
					t.Errorf("%s takes no forward's mark after the migration: %v", what, err)
				}
				if _, err := db.Exec(`UPDATE den_messages SET forwarded = 2 WHERE id = 1`); err == nil {
					t.Errorf("%s takes a forward's mark other than 0 or 1", what)
				}
				// No member has a status until they set one, and an end
				// comes with a status.
				var statuses int
				if err := db.QueryRow(`SELECT count(*) FROM den_members WHERE status IS NOT NULL OR status_until IS NOT NULL`).Scan(&statuses); err != nil ||
					(released.version < 22 && statuses != 0) {
					t.Errorf("%s holds %d statuses after the migration, %v", what, statuses, err)
				}
				if _, err := db.Exec(`UPDATE den_members SET status = x'00', status_until = 9 WHERE id = 1`); err != nil {
					t.Errorf("%s takes no status after the migration: %v", what, err)
				}
				if _, err := db.Exec(`UPDATE den_members SET status = NULL WHERE id = 1`); err == nil {
					t.Errorf("%s takes a status's end with no status", what)
				}
				// Roles are unique case-insensitively, link to members,
				// channels and events, and cascade on delete.
				if _, err := db.Exec(`INSERT INTO den_roles (id, name, created_at) VALUES (1, 'Raiders', 15)`); err != nil {
					t.Errorf("%s takes no role after the migration: %v", what, err)
				}
				if _, err := db.Exec(`INSERT INTO den_roles (name, created_at) VALUES ('raiders', 16)`); err == nil {
					t.Errorf("%s took a duplicate role name in another case", what)
				}
				for _, q := range []string{
					`INSERT INTO den_member_roles (member_id, role_id) VALUES (1, 1)`,
					`INSERT INTO den_channel_roles (channel_id, role_id) VALUES (1, 1)`,
					`INSERT INTO den_event_roles (event_id, role_id) VALUES (` + fmt.Sprint(event) + `, 1)`,
				} {
					if _, err := db.Exec(q); err != nil {
						t.Errorf("%s failed %s: %v", what, q, err)
					}
				}
				for _, q := range []string{
					`INSERT INTO den_member_roles (member_id, role_id) VALUES (99, 1)`,
					`INSERT INTO den_channel_roles (channel_id, role_id) VALUES (99, 1)`,
					`INSERT INTO den_event_roles (event_id, role_id) VALUES (99999, 1)`,
					`INSERT INTO den_member_roles (member_id, role_id) VALUES (1, 99)`,
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				if _, err := db.Exec(`DELETE FROM den_roles WHERE id = 1`); err != nil {
					t.Errorf("%s failed deleting role: %v", what, err)
				}
				var leftovers int
				if err := db.QueryRow(`SELECT (SELECT count(*) FROM den_member_roles) + (SELECT count(*) FROM den_channel_roles) + (SELECT count(*) FROM den_event_roles)`).Scan(&leftovers); err != nil || leftovers != 0 {
					t.Errorf("%s kept %d role links after role delete, %v", what, leftovers, err)
				}
				// Forward links start empty on pre-step-24 schemas, accept a
				// 32-byte den ID and positive channel/message IDs, and reject
				// malformed values.
				var linked int
				if err := db.QueryRow(`SELECT count(*) FROM den_messages WHERE forward_den IS NOT NULL OR forward_channel IS NOT NULL OR forward_message IS NOT NULL`).Scan(&linked); err != nil ||
					(released.version < 24 && linked != 0) {
					t.Errorf("%s holds %d forward sources after the migration, %v", what, linked, err)
				}
				if _, err := db.Exec(`UPDATE den_messages SET forward_den = zeroblob(32), forward_channel = 1, forward_message = 2 WHERE id = 1`); err != nil {
					t.Errorf("%s takes no forward source after the migration: %v", what, err)
				}
				for _, q := range []string{
					`UPDATE den_messages SET forward_den = x'01' WHERE id = 1`,
					`UPDATE den_messages SET forward_channel = 0 WHERE id = 1`,
					`UPDATE den_messages SET forward_channel = -1 WHERE id = 1`,
					`UPDATE den_messages SET forward_message = 0 WHERE id = 1`,
					`UPDATE den_messages SET forward_message = -1 WHERE id = 1`,
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				// Collections belong to either a channel or an owner, never both or neither,
				// and cascade their items on delete.
				if _, err := db.Exec(`INSERT INTO den_collections (id, channel_id, name, who_adds, created_by, created_at) VALUES (1, 1, x'c1', 'everyone', 1, 20)`); err != nil {
					t.Errorf("%s takes no shared collection after the migration: %v", what, err)
				}
				if _, err := db.Exec(`INSERT INTO den_collections (id, owner_id, name, created_by, created_at) VALUES (2, 1, x'c2', 1, 21)`); err != nil {
					t.Errorf("%s takes no personal collection after the migration: %v", what, err)
				}
				if _, err := db.Exec(`INSERT INTO den_collection_items (collection_id, message_id, added_by, added_at) VALUES (1, 1, 1, 22), (2, 1, 1, 23)`); err != nil {
					t.Errorf("%s takes no collection items after the migration: %v", what, err)
				}
				for _, q := range []string{
					`INSERT INTO den_collections (name, created_by, created_at) VALUES (x'c3', 1, 24)`,
					`INSERT INTO den_collections (channel_id, owner_id, name, created_by, created_at) VALUES (1, 1, x'c3', 1, 24)`,
					`INSERT INTO den_collections (channel_id, name, who_adds, created_by, created_at) VALUES (1, x'c3', 'mods', 1, 24)`,
					`INSERT INTO den_collection_items (collection_id, message_id, added_by, added_at) VALUES (1, 1, 1, 25)`,
					`INSERT INTO den_collection_items (collection_id, message_id, added_by, added_at) VALUES (99, 1, 1, 25)`,
					`INSERT INTO den_collection_items (collection_id, message_id, added_by, added_at) VALUES (1, 99, 1, 25)`,
				} {
					if _, err := db.Exec(q); err == nil {
						t.Errorf("%s took %s", what, q)
					}
				}
				if _, err := db.Exec(`DELETE FROM den_collections WHERE id IN (1, 2)`); err != nil {
					t.Errorf("%s failed deleting collections: %v", what, err)
				}
				if err := db.QueryRow(`SELECT count(*) FROM den_collection_items`).Scan(&leftovers); err != nil || leftovers != 0 {
					t.Errorf("%s kept %d collection items after collection delete, %v", what, leftovers, err)
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

// checkRetired checks Identities retired the DM key whose exchange one
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
// message sent under it; from version 14, v1.10.1's, a spoiler, a member's
// notes, an event of staff's with a member interested, its badge and a
// hidden award, and the den's limit for members' events; from version 16,
// v1.12.0's, the chat's background and a member's name style; and from
// version 17, v1.14.0's, a voice channel's longest call; and from version
// 18, v1.15.0's, a line of the audit log.
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
		// A DM both members checked, whose key stays.
		`INSERT INTO den_members (id, username, display_name, role, verifier_hash, joined_at, seal_check) VALUES (3, 'carol', 'Carol', 'member', x'17', 3, x'18')`,
		`INSERT INTO den_channels (id, name, kind, position, dm_low, dm_high, created_at) VALUES (4, '', 'dm', 0, 1, 3, 2)`,
		`INSERT INTO den_dm_keys (id, channel_id, started_by, started_at, stage) VALUES (2, 4, 3, 2, 'revealed')`,
		`INSERT INTO den_dm_key_members (key_id, member_id, sealed, checked_at) VALUES (2, 1, x'56', 3), (2, 3, x'57', 4)`,
		`INSERT INTO den_changes (id, kind, ref, channel_id, at) VALUES (1, 'message', 1, 1, 2)`,
		`INSERT INTO den_read_states (member_id, channel_id, message_id, closed) VALUES (1, 1, 1, 0)`,
		// A restore moved the channels' counter past their IDs.
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
			`UPDATE den_channels SET expires_after = 3600, expires_by = 1, expires_set_at = 7 WHERE id = 4`,
			`INSERT INTO den_messages (id, channel_id, author_id, created_at, text, nonce, key_id, expires_at) VALUES (4, 4, 1, 8, x'37', x'38', 2, 3600008)`,
		)
		if version < 21 {
			rows = append(rows,
				`INSERT INTO den_channels (id, name, kind, position, created_at) VALUES (5, '', 'group_dm', 0, 7)`,
				`INSERT INTO den_group_dms (channel_id, made_by, made_at, name, name_kid) VALUES (5, 1, 7, x'78', x'79')`,
				`INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (5, 1, 0, 1, 0), (5, 3, 1, 1, 0)`,
				`INSERT INTO den_group_changes (channel_id, n, made_by, made_at, statement) VALUES (5, 1, 1, 7, x'7a')`,
			)
		}
	}
	if version >= 14 {
		rows = append(rows,
			`UPDATE den_files SET spoiler = 1 WHERE id = 1`,
			`INSERT INTO den_channels (id, name, kind, position, created_at, notes_of) VALUES (6, '', 'notes', 0, 8, 3)`,
			`UPDATE den SET member_events = 3`,
			`INSERT INTO den_events (id, details, banner_id, channel_id, made_by, made_at, starts_at, ends_at, state, staff_only)
				VALUES (1, x'81', 1, 2, 1, 9, 10, 20, 'ended', 1)`,
			`INSERT INTO den_event_interest (event_id, member_id) VALUES (1, 3)`,
			`INSERT INTO den_files (id, blob, uploader_id, name, type, size, width, height, created_at) VALUES (5, x'82', 1, x'83', 'image/png', 500, 128, 128, 9)`,
			`INSERT INTO den_named_files (id, kind, name, file_id, added_by, added_at) VALUES (3, 'badge', '1', 5, 1, 9)`,
			`INSERT INTO den_badges (event_id, named_id, title) VALUES (1, 3, x'84')`,
			`INSERT INTO den_badge_awards (event_id, member_id, earned_at, hidden) VALUES (1, 3, 11, 1)`,
		)
	}
	if version >= 16 {
		rows = append(rows,
			`INSERT INTO background (id, set_at, file) VALUES (1, 12, x'91')`,
			`UPDATE den_members SET name_style = '{"font":"bangers","colors":["#ff8800"]}' WHERE id = 3`,
		)
	}
	if version >= 17 {
		rows = append(rows, `UPDATE den_channels SET call_record = 4000 WHERE id = 2`)
	}
	if version >= 18 {
		rows = append(rows, `INSERT INTO den_audit (at, kind, done_by, member_id, details) VALUES (11, 'member.banned', 1, 2, x'a0')`)
	}
	if version >= 20 {
		rows = append(rows, `UPDATE den_messages SET forwarded = 1 WHERE id = 1`)
	}
	if version >= 22 {
		rows = append(rows, `UPDATE den_members SET status = x'a5', status_until = 99 WHERE id = 1`)
	}
	if version >= 23 {
		rows = append(rows,
			`INSERT INTO den_roles (id, name, created_at) VALUES (1, 'VIP', 15)`,
			`INSERT INTO den_member_roles (member_id, role_id) VALUES (3, 1)`,
			`INSERT INTO den_channel_roles (channel_id, role_id) VALUES (1, 1)`,
			`INSERT INTO den_event_roles (event_id, role_id) VALUES (1, 1)`,
		)
	}
	if version >= 24 {
		rows = append(rows, `UPDATE den_messages SET forward_den = zeroblob(32), forward_channel = 1, forward_message = 2 WHERE id = 1`)
	}
	for _, q := range rows {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// TestMigrationFromStep23PreservesRowsAndForwardLinks checks migrating from
// Step 23 (channel roles) through Step 24 (forward links) and keeping forward
// source columns across snapshot and reopen.
func TestMigrationFromStep23PreservesRowsAndForwardLinks(t *testing.T) {
	bi := build.BuildInfo{DefaultLogLevel: "warn"}
	dir, backedUp, reopened := t.TempDir(), t.TempDir(), t.TempDir()
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
	if v, err := newMigrator(bi).Upto(23).Run(ctx, old, log); err != nil || v != 23 {
		old.Close()
		t.Fatalf("make step 23 schema: version %d, %v", v, err)
	}
	fillEveryTable(t, old, 23)
	tables, err := Tables(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	tables = append(tables, "sqlite_sequence")
	columns := map[string]string{}
	before := map[string]string{}
	for _, table := range tables {
		columns[table] = columnsOf(t, old, table)
		before[table] = dumpColumns(t, old, table, columns[table])
	}
	if err := Snapshot(ctx, old, filepath.Join(backedUp, FileName)); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	for what, at := range map[string]string{"the step 23 database": dir, "its backup": backedUp} {
		db, err := New(at, log, bi, ApplyPendingMigrations)
		if err != nil {
			t.Fatalf("migrate %s from step 23: %v", what, err)
		}
		if v, err := FileSchemaVersion(ctx, db); err != nil || v != SchemaVersion(bi) || v < 24 {
			db.Close()
			t.Fatalf("%s migrated to schema %d, %v; want %d", what, v, err, SchemaVersion(bi))
		}
		for _, table := range tables {
			if after := dumpColumns(t, db, table, columns[table]); after != before[table] {
				t.Errorf("%s's %s changed in the migration: before\n%s\nafter\n%s", what, table, before[table], after)
			}
		}
		var denBlob []byte
		var fwdCh, fwdMsg sql.NullInt64
		if err := db.QueryRow(`SELECT forward_den, forward_channel, forward_message FROM den_messages WHERE id = 1`).Scan(&denBlob, &fwdCh, &fwdMsg); err != nil ||
			denBlob != nil || fwdCh.Valid || fwdMsg.Valid {
			t.Errorf("%s forward columns not NULL after step 24: %x %v %v %v", what, denBlob, fwdCh, fwdMsg, err)
		}
		wantDen := make([]byte, 32)
		for i := range wantDen {
			wantDen[i] = byte(i + 1)
		}
		if _, err := db.Exec(`UPDATE den_messages SET forward_den = ?, forward_channel = 7, forward_message = 42 WHERE id = 1`, wantDen); err != nil {
			t.Fatalf("%s update forward source: %v", what, err)
		}
		if what == "the step 23 database" {
			if err := Snapshot(ctx, db, filepath.Join(reopened, FileName)); err != nil {
				t.Fatal(err)
			}
		}
		db.Close()
	}

	db, err := New(reopened, log, bi, RequireCurrentSchema)
	if err != nil {
		t.Fatalf("reopen step 24 snapshot: %v", err)
	}
	defer db.Close()
	var gotDen []byte
	var gotCh, gotMsg int64
	if err := db.QueryRow(`SELECT forward_den, forward_channel, forward_message FROM den_messages WHERE id = 1`).Scan(&gotDen, &gotCh, &gotMsg); err != nil ||
		len(gotDen) != 32 || gotDen[0] != 1 || gotDen[31] != 32 || gotCh != 7 || gotMsg != 42 {
		t.Fatalf("reopened forward source = %x %d %d, %v", gotDen, gotCh, gotMsg, err)
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

// TestCollectionsMigration checks that migrating from v1.16.0's schema adds
// den_collections and den_collection_items with their constraints, and cascades
// items and shared collections when messages, channels or owners go.
func TestCollectionsMigration(t *testing.T) {
	dir := t.TempDir()
	log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	bi := build.BuildInfo{DefaultLogLevel: "warn"}
	ctx := context.Background()

	old, err := Open(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := newMigrator(bi).Upto(22).Run(ctx, old, log); err != nil || v != 22 {
		old.Close()
		t.Fatalf("make v1.16.0 schema: version %d, %v", v, err)
	}
	fillEveryTable(t, old, 22)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := New(dir, log, bi, ApplyPendingMigrations)
	if err != nil {
		t.Fatalf("migrate to current schema: %v", err)
	}
	defer db.Close()

	// Insert a shared collection and a personal collection with items.
	if _, err := db.Exec(`INSERT INTO den_collections (id, channel_id, name, who_adds, created_by, created_at) VALUES (1, 1, x'01', 'staff', 1, 100)`); err != nil {
		t.Fatalf("insert shared collection: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO den_collections (id, owner_id, name, created_by, created_at) VALUES (2, 1, x'02', 1, 101)`); err != nil {
		t.Fatalf("insert personal collection: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO den_collection_items (collection_id, message_id, added_by, added_at) VALUES (1, 1, 1, 102), (2, 1, 1, 103), (2, 3, 1, 104)`); err != nil {
		t.Fatalf("insert collection items: %v", err)
	}

	// Deleting a message cascades its collection items.
	if _, err := db.Exec(`DELETE FROM den_messages WHERE id = 1`); err != nil {
		t.Fatalf("delete message: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM den_collection_items WHERE message_id = 1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("items left after message delete: %d, %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM den_collection_items WHERE collection_id = 2`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("other item affected by message delete: %d, %v", count, err)
	}

	// Deleting a channel cascades its shared collections (and their items).
	if _, err := db.Exec(`INSERT INTO den_collection_items (collection_id, message_id, added_by, added_at) VALUES (1, 3, 1, 105)`); err != nil {
		t.Fatalf("insert shared item: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM den_channels WHERE id = 1`); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM den_collections WHERE id = 1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("shared collection left after channel delete: %d, %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM den_collection_items`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("items left after channel delete: %d, %v", count, err)
	}
	// Personal collection row stays intact when a channel is deleted.
	if err := db.QueryRow(`SELECT count(*) FROM den_collections WHERE id = 2`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("personal collection lost on channel delete: %d, %v", count, err)
	}
}
