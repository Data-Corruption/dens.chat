package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/migrator"

	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// MigrationPolicy controls whether opening a database may change its schema.
type MigrationPolicy uint8

const (
	// RequireCurrentSchema rejects any schema version mismatch.
	RequireCurrentSchema MigrationPolicy = iota
	// ApplyPendingMigrations applies every pending migration. The service
	// uses it only when lifecycle state authorizes a migration.
	ApplyPendingMigrations
)

func newMigrator(buildInfo build.BuildInfo) *migrator.Migrator {
	m := migrator.New()

	// Add steps at the end. The first step takes a fresh database to schema
	// version 1 (PRAGMA user_version), the next to 2, and so on. v1.0.0
	// shipped the initial step, so installed copies hold databases every
	// later version reads: a released step never changes (see Data across
	// versions in the design doc).

	m.Add("Initial Schema", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE config (
				key   TEXT PRIMARY KEY,
				value TEXT NOT NULL
			) STRICT;

			-- One row: the data key's check value, the data key sealed under
			-- the local password once one is set, and the DM seal new dens
			-- start with, sealed with the data key once the first den has one.
			CREATE TABLE vault (
				id            INTEGER PRIMARY KEY CHECK (id = 1),
				check_value   BLOB NOT NULL,
				password_wrap TEXT,             -- JSON; NULL until a password is set
				updated_at    INTEGER NOT NULL, -- unix milliseconds
				dm_seal       BLOB
			) STRICT;

			-- Paired browser sessions, keyed by SHA-256 of the cookie token.
			CREATE TABLE client_sessions (
				token_hash   TEXT PRIMARY KEY,
				created_at   INTEGER NOT NULL, -- unix milliseconds
				expires_at   INTEGER NOT NULL, -- unix milliseconds
				last_seen_at INTEGER NOT NULL  -- unix milliseconds
			) STRICT;

			-- Dens this install has joined, keyed by den_id. The profile (URL,
			-- name, account), the device key and the member's DM seal there
			-- are sealed with the data key. The seal is NULL while this device
			-- has none: signed in with a recovery code, until the member types
			-- it or starts over.
			CREATE TABLE joined_dens (
				den_id     BLOB PRIMARY KEY,
				profile    BLOB NOT NULL,
				device_key BLOB NOT NULL,
				joined_at  INTEGER NOT NULL,
				dm_seal    BLOB
			) STRICT;

			-- The den this install hosts, once created. The identity private
			-- key is sealed with the data key. The upload limits are bytes.
			-- The limits for calls and screen shares (M4.2) are counts of
			-- members, bits a second, a 16:9 picture's height and frames a
			-- second. The retention period (M5) is the days a message is kept,
			-- 0 for ever. The sync epoch names the den's run of change numbers,
			-- and a restore from a backup replaces it; the changes floor is the
			-- oldest version the den still has the changes since; and expired
			-- through is the last message retention deleted (M6).
			CREATE TABLE den (
				id             INTEGER PRIMARY KEY CHECK (id = 1),
				name           TEXT NOT NULL,
				url            TEXT NOT NULL,
				public_key     BLOB NOT NULL,
				private_key    BLOB NOT NULL,
				created_at     INTEGER NOT NULL,
				file_size      INTEGER NOT NULL,
				member_storage INTEGER NOT NULL,
				den_storage    INTEGER NOT NULL,
				call_members   INTEGER NOT NULL,
				den_callers    INTEGER NOT NULL,
				shares         INTEGER NOT NULL,
				share_viewers  INTEGER NOT NULL,
				share_bitrate  INTEGER NOT NULL,
				share_height   INTEGER NOT NULL,
				share_fps      INTEGER NOT NULL,
				retention      INTEGER NOT NULL DEFAULT 0 CHECK (retention BETWEEN 0 AND 3650),
				sync_epoch     BLOB NOT NULL,
				changes_floor  INTEGER NOT NULL DEFAULT 0,
				expired_through INTEGER NOT NULL DEFAULT 0
			) STRICT;

			-- AUTOINCREMENT: a member ID is never reused, even after deletion.
			-- A member who leaves or is removed keeps their row, with left_at
			-- set, so their messages keep a name and their username stays
			-- theirs to reclaim, unless banned_at says it can't come back.
			CREATE TABLE den_members (
				id            INTEGER PRIMARY KEY AUTOINCREMENT,
				username      TEXT NOT NULL UNIQUE,
				display_name  TEXT NOT NULL,
				role          TEXT NOT NULL CHECK (role IN ('member', 'moderator', 'owner')),
				verifier_hash BLOB NOT NULL, -- SHA-256 of the password verifier
				bio           BLOB,          -- sealed with the data key
				joined_at     INTEGER NOT NULL,
				invite_id     INTEGER REFERENCES den_invites (id) ON DELETE SET NULL,
				left_at       INTEGER,
				banned_at     INTEGER,
				banned_by     INTEGER REFERENCES den_members (id),
				avatar_id     INTEGER REFERENCES den_files (id) ON DELETE SET NULL,
				banner_id     INTEGER REFERENCES den_files (id) ON DELETE SET NULL,
				seal_check    BLOB NOT NULL  -- identifies the DM seal only their devices hold
			) STRICT;
			CREATE INDEX den_members_avatar ON den_members (avatar_id) WHERE avatar_id IS NOT NULL;
			CREATE INDEX den_members_banner ON den_members (banner_id) WHERE banner_id IS NOT NULL;

			CREATE TABLE den_devices (
				key_id       BLOB PRIMARY KEY, -- SHA-256 of the public key
				member_id    INTEGER NOT NULL REFERENCES den_members (id) ON DELETE CASCADE,
				public_key   BLOB NOT NULL,
				label        TEXT NOT NULL,
				created_at   INTEGER NOT NULL,
				last_seen_at INTEGER NOT NULL
			) STRICT;
			CREATE INDEX den_devices_member ON den_devices (member_id);

			CREATE TABLE den_recovery_codes (
				code_hash BLOB PRIMARY KEY, -- SHA-256 of the raw code
				member_id INTEGER NOT NULL REFERENCES den_members (id) ON DELETE CASCADE
			) STRICT;
			CREATE INDEX den_recovery_codes_member ON den_recovery_codes (member_id);

			-- created_by is NULL for the owner bootstrap invite, which grants
			-- the owner role.
			CREATE TABLE den_invites (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				code_hash  BLOB NOT NULL UNIQUE,
				role       TEXT NOT NULL CHECK (role IN ('member', 'owner')),
				created_by INTEGER REFERENCES den_members (id) ON DELETE CASCADE,
				created_at INTEGER NOT NULL,
				expires_at INTEGER NOT NULL,
				max_uses   INTEGER NOT NULL,
				uses       INTEGER NOT NULL DEFAULT 0
			) STRICT;

			CREATE TABLE den_sessions (
				token_hash BLOB PRIMARY KEY, -- SHA-256 of the bearer token
				member_id  INTEGER NOT NULL REFERENCES den_members (id) ON DELETE CASCADE,
				key_id     BLOB NOT NULL REFERENCES den_devices (key_id) ON DELETE CASCADE,
				created_at INTEGER NOT NULL,
				expires_at INTEGER NOT NULL
			) STRICT;
			CREATE INDEX den_sessions_expiry ON den_sessions (expires_at);

			-- Positions are dense (0, 1, 2, ...) within each list: the
			-- groups, the ungrouped channels, and each group's channels.
			CREATE TABLE den_groups (
				id       INTEGER PRIMARY KEY AUTOINCREMENT,
				name     TEXT NOT NULL,
				position INTEGER NOT NULL
			) STRICT;

			-- A DM is a channel between two members, dm_low < dm_high, with no
			-- name, group or position. A voice channel has a bitrate, in bits
			-- a second, for its call's Opus (M4.2), and no other kind does.
			CREATE TABLE den_channels (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				group_id    INTEGER REFERENCES den_groups (id) ON DELETE SET NULL,
				name        TEXT NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				kind        TEXT NOT NULL CHECK (kind IN ('text', 'voice', 'dm')),
				position    INTEGER NOT NULL,
				staff_only  INTEGER NOT NULL DEFAULT 0,
				dm_low      INTEGER REFERENCES den_members (id),
				dm_high     INTEGER REFERENCES den_members (id),
				created_at  INTEGER NOT NULL,
				bitrate     INTEGER,
				CHECK ((kind = 'dm') = (dm_low IS NOT NULL AND dm_high IS NOT NULL AND dm_low < dm_high)),
				CHECK ((kind = 'voice') = (bitrate IS NOT NULL))
			) STRICT;
			CREATE UNIQUE INDEX den_channels_dm ON den_channels (dm_low, dm_high) WHERE kind = 'dm';
			CREATE INDEX den_channels_dm_high ON den_channels (dm_high) WHERE kind = 'dm';

			-- AUTOINCREMENT: message IDs grow with time and are never reused,
			-- which history paging (before, after, around an ID) relies on.
			-- The text is sealed with the data key, except in a DM, where the
			-- members' clients seal it with the DM key that key_id names and
			-- the den can't open it. reply_to may name a deleted message.
			CREATE TABLE den_messages (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				channel_id INTEGER NOT NULL REFERENCES den_channels (id) ON DELETE CASCADE,
				author_id  INTEGER NOT NULL REFERENCES den_members (id),
				created_at INTEGER NOT NULL,
				revision   INTEGER NOT NULL DEFAULT 1,
				edited_at  INTEGER,
				edited_by  INTEGER,
				text       BLOB NOT NULL,
				reply_to   INTEGER,
				nonce      BLOB NOT NULL, -- the author's idempotency key
				key_id     INTEGER        -- a DM message's key
			) STRICT;
			CREATE INDEX den_messages_channel ON den_messages (channel_id, id);
			CREATE INDEX den_messages_nonce ON den_messages (author_id, nonce);

			CREATE TABLE den_mentions (
				member_id  INTEGER NOT NULL REFERENCES den_members (id) ON DELETE CASCADE,
				channel_id INTEGER NOT NULL,
				message_id INTEGER NOT NULL REFERENCES den_messages (id) ON DELETE CASCADE,
				PRIMARY KEY (member_id, channel_id, message_id)
			) STRICT, WITHOUT ROWID;
			CREATE INDEX den_mentions_message ON den_mentions (message_id);

			-- Members besides the author who may edit a message.
			CREATE TABLE den_message_editors (
				message_id INTEGER NOT NULL REFERENCES den_messages (id) ON DELETE CASCADE,
				member_id  INTEGER NOT NULL REFERENCES den_members (id),
				PRIMARY KEY (message_id, member_id)
			) STRICT, WITHOUT ROWID;

			-- Uploads. Each is stored sealed with the data key under a random
			-- name (blob) only its row knows, and its name is sealed too. A
			-- file is pending until a message or a profile uses it; pending
			-- files are deleted after an hour. Sizes are bytes, as uploaded;
			-- the thumb columns are set when the den made a preview, and
			-- duration_ms for video and audio. A sealed file is a DM's, which
			-- the uploader's client sealed: the den knows only its size. A
			-- waiting upload made to take another file's place names it in
			-- replaces (M5), and takes only that file's place.
			CREATE TABLE den_files (
				id           INTEGER PRIMARY KEY AUTOINCREMENT,
				blob         BLOB NOT NULL UNIQUE,
				uploader_id  INTEGER NOT NULL REFERENCES den_members (id),
				message_id   INTEGER REFERENCES den_messages (id) ON DELETE CASCADE,
				position     INTEGER NOT NULL DEFAULT 0, -- its place on the message
				name         BLOB NOT NULL,
				type         TEXT NOT NULL,
				size         INTEGER NOT NULL,
				width        INTEGER,
				height       INTEGER,
				animated     INTEGER NOT NULL DEFAULT 0,
				thumb_width  INTEGER,
				thumb_height INTEGER,
				thumb_size   INTEGER,
				duration_ms  INTEGER,
				sealed       INTEGER NOT NULL DEFAULT 0,
				created_at   INTEGER NOT NULL,
				replaces     INTEGER REFERENCES den_files (id) ON DELETE SET NULL
			) STRICT;
			CREATE INDEX den_files_message ON den_files (message_id, position);
			CREATE INDEX den_files_uploader ON den_files (uploader_id);
			CREATE INDEX den_files_pending ON den_files (created_at) WHERE message_id IS NULL;
			CREATE INDEX den_files_replaces ON den_files (replaces) WHERE replaces IS NOT NULL;

			-- The keys of DMs. Each comes from an exchange between the two
			-- members' clients, which the den relays and can't take part in:
			-- the starter's offer, the answer, and the starter's reveal, kept
			-- as JSON only while the exchange runs. A retired key takes no new
			-- messages: a member started over, or started the exchange again.
			CREATE TABLE den_dm_keys (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				channel_id INTEGER NOT NULL REFERENCES den_channels (id) ON DELETE CASCADE,
				started_by INTEGER NOT NULL REFERENCES den_members (id),
				started_at INTEGER NOT NULL,
				stage      TEXT NOT NULL CHECK (stage IN ('offered', 'answered', 'revealed')),
				offer      BLOB,
				answer     BLOB,
				reveal     BLOB,
				retired_at INTEGER,
				retired_by INTEGER REFERENCES den_members (id),
				retired    TEXT CHECK (retired IN ('started_over', 'restarted')),
				CHECK ((retired_at IS NULL) = (retired IS NULL) AND (retired IS NULL) = (retired_by IS NULL))
			) STRICT;
			CREATE INDEX den_dm_keys_channel ON den_dm_keys (channel_id, id);
			-- A DM has one live key at a time.
			CREATE UNIQUE INDEX den_dm_keys_live ON den_dm_keys (channel_id) WHERE retired IS NULL;

			-- Each member's side of a DM key, sealed with their DM seal: their
			-- exchange state while it runs, and their copy of the key once
			-- they checked the code.
			CREATE TABLE den_dm_key_members (
				key_id     INTEGER NOT NULL REFERENCES den_dm_keys (id) ON DELETE CASCADE,
				member_id  INTEGER NOT NULL REFERENCES den_members (id),
				state      BLOB,
				sealed     BLOB,
				checked_at INTEGER,
				PRIMARY KEY (key_id, member_id),
				CHECK ((sealed IS NULL) = (checked_at IS NULL))
			) STRICT, WITHOUT ROWID;
			CREATE INDEX den_dm_key_members_member ON den_dm_key_members (member_id);

			-- What changed, by ID only, for clients that cached it and were
			-- away (M6): each message edited or deleted, with its channel, and
			-- each member whose record changed. A thing has one row, its
			-- latest change, and the rows are numbered in the order the changes
			-- happened; the newest number is the den's version. Rows older
			-- than 30 days go, and den.changes_floor says how far that reached.
			CREATE TABLE den_changes (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				kind       TEXT NOT NULL CHECK (kind IN ('message', 'member')),
				ref        INTEGER NOT NULL,
				channel_id INTEGER, -- a message's, kept for one that's gone
				at         INTEGER NOT NULL,
				UNIQUE (kind, ref)
			) STRICT;

			-- closed is a DM the member closed; a new message reopens it.
			CREATE TABLE den_read_states (
				member_id  INTEGER NOT NULL REFERENCES den_members (id) ON DELETE CASCADE,
				channel_id INTEGER NOT NULL REFERENCES den_channels (id) ON DELETE CASCADE,
				message_id INTEGER NOT NULL,
				closed     INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (member_id, channel_id)
			) STRICT;
		`); err != nil {
			return fmt.Errorf("create tables: %w", err)
		}

		cfg := types.DefaultConfig(buildInfo)
		data, err := json.Marshal(&cfg)
		if err != nil {
			return fmt.Errorf("marshal initial config: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO config (key, value) VALUES (?, ?)`,
			ConfigDataKey, string(data),
		); err != nil {
			return fmt.Errorf("store initial config: %w", err)
		}
		return nil
	})

	// M14.2: the member's own sounds, each a file of theirs in place of one
	// of the page's sounds, by the sound's name, sealed with the data key,
	// and when it was set, which names the file's version for the page.
	m.Add("Own sounds", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE sounds (
				name   TEXT PRIMARY KEY,
				file   BLOB NOT NULL,
				set_at INTEGER NOT NULL -- unix milliseconds
			) STRICT;
		`); err != nil {
			return fmt.Errorf("create sounds: %w", err)
		}
		return nil
	})

	// M14.3: the den's named files, each a file of the den's with a name
	// unique among those of its kind, sounds for its soundboard first; and
	// the den's limits for them: how many sounds it keeps, 0 turning the
	// soundboard off, the longest a sound may be, and how long a member
	// waits between sounds, both in seconds. A named file's file is the
	// den's, not its uploader's, and stays until staff delete the name.
	m.Add("Soundboard", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE den_named_files (
				id       INTEGER PRIMARY KEY AUTOINCREMENT,
				kind     TEXT NOT NULL,
				name     TEXT NOT NULL,
				file_id  INTEGER NOT NULL UNIQUE REFERENCES den_files (id),
				added_by INTEGER NOT NULL REFERENCES den_members (id),
				added_at INTEGER NOT NULL, -- unix milliseconds
				UNIQUE (kind, name COLLATE NOCASE)
			) STRICT;

			ALTER TABLE den ADD COLUMN sounds INTEGER NOT NULL DEFAULT 50;
			ALTER TABLE den ADD COLUMN sound_longest INTEGER NOT NULL DEFAULT 10;
			ALTER TABLE den ADD COLUMN sound_wait INTEGER NOT NULL DEFAULT 5;
		`); err != nil {
			return fmt.Errorf("create the soundboard: %w", err)
		}
		return nil
	})

	// M15.2: the den's limit for its emoji, which are named files of kind
	// emoji: how many it keeps, 0 turning them off and keeping them.
	m.Add("Emoji", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE den ADD COLUMN emojis INTEGER NOT NULL DEFAULT 50`); err != nil {
			return fmt.Errorf("add the den's emoji limit: %w", err)
		}
		return nil
	})

	// M15.3: reactions. A channel's are a row each: a member's emoji on a
	// message, one of Unicode's or one of the den's, which goes with its
	// message, and with the den's emoji as staff delete it. A DM's are a
	// sealed set for each message and member, which their client sealed
	// with the DM's key that key_id names, replaced whole against its
	// revision, as an edit is.
	m.Add("Reactions", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE den_reactions (
				message_id INTEGER NOT NULL REFERENCES den_messages (id) ON DELETE CASCADE,
				member_id  INTEGER NOT NULL REFERENCES den_members (id),
				emoji      TEXT,
				emoji_id   INTEGER REFERENCES den_named_files (id) ON DELETE CASCADE,
				created_at INTEGER NOT NULL, -- unix milliseconds
				CHECK ((emoji IS NULL) != (emoji_id IS NULL))
			) STRICT;
			CREATE INDEX den_reactions_message ON den_reactions (message_id, created_at);
			CREATE UNIQUE INDEX den_reactions_unicode ON den_reactions (message_id, member_id, emoji) WHERE emoji IS NOT NULL;
			CREATE UNIQUE INDEX den_reactions_den ON den_reactions (message_id, member_id, emoji_id) WHERE emoji_id IS NOT NULL;
			CREATE INDEX den_reactions_member ON den_reactions (member_id, created_at);
			CREATE INDEX den_reactions_emoji ON den_reactions (emoji_id) WHERE emoji_id IS NOT NULL;

			CREATE TABLE den_dm_reactions (
				message_id INTEGER NOT NULL REFERENCES den_messages (id) ON DELETE CASCADE,
				member_id  INTEGER NOT NULL REFERENCES den_members (id),
				key_id     INTEGER NOT NULL REFERENCES den_dm_keys (id),
				revision   INTEGER NOT NULL,
				sealed     BLOB NOT NULL,
				updated_at INTEGER NOT NULL, -- unix milliseconds
				PRIMARY KEY (message_id, member_id)
			) STRICT, WITHOUT ROWID;
		`); err != nil {
			return fmt.Errorf("create the reactions' tables: %w", err)
		}
		return nil
	})

	// M15.4: polls, and staff's @everyone. A poll is a row beside its
	// channel's message, with its choices, sealed with the data key as the
	// text is, its rules and its end, which an early end moves; a vote is
	// a row for each choice a member picked, and goes with its poll. A
	// message's everyone marks the @everyone staff put in it.
	m.Add("Polls", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE den_messages ADD COLUMN everyone INTEGER NOT NULL DEFAULT 0;

			CREATE TABLE den_polls (
				message_id  INTEGER PRIMARY KEY REFERENCES den_messages (id) ON DELETE CASCADE,
				choices     BLOB NOT NULL, -- JSON, sealed with the data key
				multiple    INTEGER NOT NULL,
				show_voters TEXT NOT NULL CHECK (show_voters IN ('never', 'end', 'always')),
				show_totals TEXT NOT NULL CHECK (show_totals IN ('live', 'voted')),
				ends_at     INTEGER NOT NULL, -- unix milliseconds
				ended       INTEGER NOT NULL DEFAULT 0
			) STRICT;
			CREATE INDEX den_polls_open ON den_polls (ends_at) WHERE ended = 0;

			CREATE TABLE den_poll_votes (
				message_id INTEGER NOT NULL REFERENCES den_polls (message_id) ON DELETE CASCADE,
				member_id  INTEGER NOT NULL REFERENCES den_members (id),
				choice     INTEGER NOT NULL,
				voted_at   INTEGER NOT NULL, -- unix milliseconds
				UNIQUE (message_id, member_id, choice)
			) STRICT;
		`); err != nil {
			return fmt.Errorf("create the polls' tables: %w", err)
		}
		return nil
	})

	// M16.1: identities and codes. A member's identity is the hash of the
	// two public keys their Dens published, which are kept beside the
	// member; a DM key sealed to its members' identities keeps the
	// statement its maker signed, with the maker's keys, and the
	// signature, and each member's copy where M1.7's keys keep theirs; and
	// a member's record is what their devices write every hour, sealed
	// with their seal. On the client, each joined den keeps the record of
	// the identities this member kept there, sealed with the data key.
	// The den starts no exchange any more, so those still running, or
	// checked by one member alone, retire as restarted, by whoever started
	// them, their states going as a retired key's do; what the member who
	// checked sent under one stays theirs to read.
	m.Add("Identities", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE den_members ADD COLUMN identity BLOB;
			CREATE TABLE den_identities (
				member_id INTEGER PRIMARY KEY REFERENCES den_members (id) ON DELETE CASCADE,
				kem       BLOB NOT NULL, -- the X-Wing public key
				sig       BLOB NOT NULL  -- the Ed25519 public key
			) STRICT;

			ALTER TABLE den_dm_keys ADD COLUMN statement BLOB; -- JSON
			ALTER TABLE den_dm_keys ADD COLUMN signature BLOB;

			CREATE TABLE den_records (
				member_id  INTEGER PRIMARY KEY REFERENCES den_members (id) ON DELETE CASCADE,
				revision   INTEGER NOT NULL,
				sealed     BLOB NOT NULL,
				updated_at INTEGER NOT NULL -- unix milliseconds
			) STRICT;

			ALTER TABLE joined_dens ADD COLUMN identities BLOB;
		`); err != nil {
			return fmt.Errorf("add identities: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE den_dm_keys SET retired_at = ?, retired_by = started_by, retired = 'restarted', offer = NULL, answer = NULL, reveal = NULL
			WHERE retired IS NULL AND
				(SELECT count(*) FROM den_dm_key_members m WHERE m.key_id = den_dm_keys.id AND m.sealed IS NOT NULL) < 2`,
			time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("retire the exchanges still running: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_dm_key_members SET state = NULL WHERE state IS NOT NULL`); err != nil {
			return fmt.Errorf("drop the exchanges' states: %w", err)
		}
		return nil
	})

	// M16.3: group DMs, a fourth kind of channel. SQLite changes a table's
	// checks only by making it again, so den_channels is made again with
	// group_dm among its kinds, by SQLite's procedure, which this step's run
	// takes foreign keys off for: dropping the old table with them on would
	// take every message, key and read position with it. Its counter stays
	// where it was, since a channel's ID is never given out twice, even past
	// the channels a restore moved it beyond (M11). A group DM keeps its
	// maker and its name, sealed under one of its keys, by that key's ID;
	// its members, in the order they joined, each with the change that took
	// them in, from which they get its changes, and the group's last message
	// as they joined, after which they read; and its changes, each a
	// member's statement and signature, as JSON, in order.
	m.AddRemaking("Group DMs", func(ctx context.Context, tx *sql.Tx) error {
		var seq sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = 'den_channels'`).Scan(&seq)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read the channels' counter: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE den_channels_new (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				group_id    INTEGER REFERENCES den_groups (id) ON DELETE SET NULL,
				name        TEXT NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				kind        TEXT NOT NULL CHECK (kind IN ('text', 'voice', 'dm', 'group_dm')),
				position    INTEGER NOT NULL,
				staff_only  INTEGER NOT NULL DEFAULT 0,
				dm_low      INTEGER REFERENCES den_members (id),
				dm_high     INTEGER REFERENCES den_members (id),
				created_at  INTEGER NOT NULL,
				bitrate     INTEGER,
				CHECK ((kind = 'dm') = (dm_low IS NOT NULL AND dm_high IS NOT NULL AND dm_low < dm_high)),
				CHECK ((kind = 'voice') = (bitrate IS NOT NULL))
			) STRICT;
			INSERT INTO den_channels_new (id, group_id, name, description, kind, position, staff_only, dm_low, dm_high, created_at, bitrate)
				SELECT id, group_id, name, description, kind, position, staff_only, dm_low, dm_high, created_at, bitrate FROM den_channels;
			DROP TABLE den_channels;
			ALTER TABLE den_channels_new RENAME TO den_channels;
			CREATE UNIQUE INDEX den_channels_dm ON den_channels (dm_low, dm_high) WHERE kind = 'dm';
			CREATE INDEX den_channels_dm_high ON den_channels (dm_high) WHERE kind = 'dm';

			CREATE TABLE den_group_dms (
				channel_id INTEGER PRIMARY KEY REFERENCES den_channels (id) ON DELETE CASCADE,
				made_by    INTEGER NOT NULL REFERENCES den_members (id),
				made_at    INTEGER NOT NULL, -- unix milliseconds
				name       BLOB,
				name_kid   BLOB,
				CHECK ((name IS NULL) = (name_kid IS NULL))
			) STRICT;
			CREATE INDEX den_group_dms_maker ON den_group_dms (made_by);

			CREATE TABLE den_group_members (
				channel_id INTEGER NOT NULL REFERENCES den_channels (id) ON DELETE CASCADE,
				member_id  INTEGER NOT NULL REFERENCES den_members (id),
				position   INTEGER NOT NULL, -- the order they joined in
				joined     INTEGER NOT NULL, -- the change that took them in
				since      INTEGER NOT NULL, -- the group's last message as they joined
				PRIMARY KEY (channel_id, member_id)
			) STRICT, WITHOUT ROWID;
			CREATE INDEX den_group_members_member ON den_group_members (member_id);

			CREATE TABLE den_group_changes (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				channel_id INTEGER NOT NULL REFERENCES den_channels (id) ON DELETE CASCADE,
				n          INTEGER NOT NULL,
				made_by    INTEGER REFERENCES den_members (id),
				made_at    INTEGER NOT NULL, -- unix milliseconds
				statement  BLOB NOT NULL,    -- JSON
				UNIQUE (channel_id, n)
			) STRICT;
		`); err != nil {
			return fmt.Errorf("add group DMs: %w", err)
		}
		if seq.Valid {
			res, err := tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = 'den_channels'`, seq.Int64)
			if err != nil {
				return fmt.Errorf("keep the channels' counter: %w", err)
			}
			if n, err := res.RowsAffected(); err != nil {
				return err
			} else if n == 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO sqlite_sequence (name, seq) VALUES ('den_channels', ?)`, seq.Int64); err != nil {
					return fmt.Errorf("keep the channels' counter: %w", err)
				}
			}
		}
		return nil
	})

	// M16.4: disappearing messages. A DM or a group keeps how long its
	// messages stay after they're sent, in seconds, 0 while they stay, and
	// who last set it and when; a message sent under a time keeps when it
	// goes, which the den's timer finds by the index, and which an edit
	// leaves as it was.
	m.Add("Disappearing messages", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE den_channels ADD COLUMN expires_after INTEGER NOT NULL DEFAULT 0 CHECK (expires_after >= 0);
			ALTER TABLE den_channels ADD COLUMN expires_by INTEGER REFERENCES den_members (id);
			ALTER TABLE den_channels ADD COLUMN expires_set_at INTEGER; -- unix milliseconds

			ALTER TABLE den_messages ADD COLUMN expires_at INTEGER; -- unix milliseconds
			CREATE INDEX den_messages_expiring ON den_messages (expires_at) WHERE expires_at IS NOT NULL;
		`); err != nil {
			return fmt.Errorf("add disappearing messages: %w", err)
		}
		return nil
	})

	// M17.2: spoilers. A channel's file keeps whether its sender sent it
	// as a spoiler; a DM's keeps it inside its seal, where the den can't
	// read it.
	m.Add("Spoilers", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE den_files ADD COLUMN spoiler INTEGER NOT NULL DEFAULT 0 CHECK (spoiler IN (0, 1));
		`); err != nil {
			return fmt.Errorf("add spoilers: %w", err)
		}
		return nil
	})

	// M17.3: notes to self, a fifth kind of channel, which names the one
	// member whose notes it holds; a member has one in the den at most.
	// den_channels is made again to take the kind, as M16.3's step made it,
	// its counter staying where it was.
	m.AddRemaking("Notes", func(ctx context.Context, tx *sql.Tx) error {
		var seq sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = 'den_channels'`).Scan(&seq)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read the channels' counter: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE den_channels_new (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				group_id       INTEGER REFERENCES den_groups (id) ON DELETE SET NULL,
				name           TEXT NOT NULL,
				description    TEXT NOT NULL DEFAULT '',
				kind           TEXT NOT NULL CHECK (kind IN ('text', 'voice', 'dm', 'group_dm', 'notes')),
				position       INTEGER NOT NULL,
				staff_only     INTEGER NOT NULL DEFAULT 0,
				dm_low         INTEGER REFERENCES den_members (id),
				dm_high        INTEGER REFERENCES den_members (id),
				created_at     INTEGER NOT NULL,
				bitrate        INTEGER,
				expires_after  INTEGER NOT NULL DEFAULT 0 CHECK (expires_after >= 0),
				expires_by     INTEGER REFERENCES den_members (id),
				expires_set_at INTEGER, -- unix milliseconds
				notes_of       INTEGER REFERENCES den_members (id),
				CHECK ((kind = 'dm') = (dm_low IS NOT NULL AND dm_high IS NOT NULL AND dm_low < dm_high)),
				CHECK ((kind = 'voice') = (bitrate IS NOT NULL)),
				CHECK ((kind = 'notes') = (notes_of IS NOT NULL))
			) STRICT;
			INSERT INTO den_channels_new (id, group_id, name, description, kind, position, staff_only, dm_low, dm_high, created_at, bitrate,
					expires_after, expires_by, expires_set_at)
				SELECT id, group_id, name, description, kind, position, staff_only, dm_low, dm_high, created_at, bitrate,
					expires_after, expires_by, expires_set_at FROM den_channels;
			DROP TABLE den_channels;
			ALTER TABLE den_channels_new RENAME TO den_channels;
			CREATE UNIQUE INDEX den_channels_dm ON den_channels (dm_low, dm_high) WHERE kind = 'dm';
			CREATE INDEX den_channels_dm_high ON den_channels (dm_high) WHERE kind = 'dm';
			CREATE UNIQUE INDEX den_channels_notes ON den_channels (notes_of) WHERE kind = 'notes';
		`); err != nil {
			return fmt.Errorf("add notes: %w", err)
		}
		return keepChannelCounter(ctx, tx, seq)
	})

	// M17.4: events. An event keeps its name, description and place's line
	// of text as JSON, sealed with the data key as a message's text is; its
	// banner, an upload of its maker's; its place's voice channel, which an
	// event that ended keeps as none once the channel goes; its maker, its
	// times, and whether it's to come, live or ended, which the den's timer
	// finds by the indexes. Who's interested is a row each, until the event
	// ends. The den keeps how many events each member who isn't staff may
	// have, 0 leaving them to staff.
	m.Add("Events", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE den_events (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				details    BLOB NOT NULL, -- JSON, sealed with the data key
				banner_id  INTEGER REFERENCES den_files (id) ON DELETE SET NULL,
				channel_id INTEGER REFERENCES den_channels (id) ON DELETE SET NULL,
				made_by    INTEGER NOT NULL REFERENCES den_members (id),
				made_at    INTEGER NOT NULL, -- unix milliseconds
				starts_at  INTEGER NOT NULL,
				ends_at    INTEGER NOT NULL,
				state      TEXT NOT NULL DEFAULT 'coming' CHECK (state IN ('coming', 'live', 'ended')),
				CHECK (ends_at >= starts_at)
			) STRICT;
			CREATE INDEX den_events_coming ON den_events (starts_at) WHERE state = 'coming';
			CREATE INDEX den_events_live ON den_events (ends_at) WHERE state = 'live';
			CREATE INDEX den_events_maker ON den_events (made_by);
			CREATE INDEX den_events_channel ON den_events (channel_id) WHERE channel_id IS NOT NULL;
			CREATE INDEX den_events_banner ON den_events (banner_id) WHERE banner_id IS NOT NULL;

			CREATE TABLE den_event_interest (
				event_id  INTEGER NOT NULL REFERENCES den_events (id) ON DELETE CASCADE,
				member_id INTEGER NOT NULL REFERENCES den_members (id),
				PRIMARY KEY (event_id, member_id)
			) STRICT, WITHOUT ROWID;
			CREATE INDEX den_event_interest_member ON den_event_interest (member_id);

			ALTER TABLE den ADD COLUMN member_events INTEGER NOT NULL DEFAULT 0 CHECK (member_events BETWEEN 0 AND 10);
		`); err != nil {
			return fmt.Errorf("add events: %w", err)
		}
		return nil
	})

	// M17.5: badges and their awards. A badge is an event's, one at most:
	// its picture one of the den's named files, of kind badge, named by its
	// file's ID, since titles repeat, and its title, sealed with the data
	// key as the event's own text is. An award is a badge, the member who
	// earned it and when, which is all the den keeps of who was in an
	// event's call. An event keeps whether its voice channel was staff's
	// alone as the channel goes, so an ended one stays where it was seen.
	m.Add("Badges", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE den_badges (
				event_id INTEGER PRIMARY KEY REFERENCES den_events (id) ON DELETE CASCADE,
				named_id INTEGER NOT NULL UNIQUE REFERENCES den_named_files (id),
				title    BLOB NOT NULL -- sealed with the data key
			) STRICT;

			CREATE TABLE den_badge_awards (
				event_id  INTEGER NOT NULL REFERENCES den_badges (event_id) ON DELETE CASCADE,
				member_id INTEGER NOT NULL REFERENCES den_members (id),
				earned_at INTEGER NOT NULL, -- unix milliseconds
				PRIMARY KEY (event_id, member_id)
			) STRICT, WITHOUT ROWID;
			CREATE INDEX den_badge_awards_member ON den_badge_awards (member_id, earned_at);

			ALTER TABLE den_events ADD COLUMN staff_only INTEGER NOT NULL DEFAULT 0 CHECK (staff_only IN (0, 1));
		`); err != nil {
			return fmt.Errorf("add badges: %w", err)
		}
		return nil
	})

	// M17.6: an award keeps whether its member hid it from their profile
	// card, where badges start shown. It's a step of its own, since test
	// builds of M17.5 ran the one before.
	m.Add("Hidden badges", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE den_badge_awards ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0 CHECK (hidden IN (0, 1));
		`); err != nil {
			return fmt.Errorf("add hidden badges: %w", err)
		}
		return nil
	})

	return m
}

// keepChannelCounter puts back the channels' counter as it was before
// den_channels was made again, since a channel's ID is never given out
// twice, even past the channels a restore moved it beyond (M11).
func keepChannelCounter(ctx context.Context, tx *sql.Tx, seq sql.NullInt64) error {
	if !seq.Valid {
		return nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = 'den_channels'`, seq.Int64)
	if err != nil {
		return fmt.Errorf("keep the channels' counter: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sqlite_sequence (name, seq) VALUES ('den_channels', ?)`, seq.Int64); err != nil {
			return fmt.Errorf("keep the channels' counter: %w", err)
		}
	}
	return nil
}

// SchemaVersion is the schema version this build migrates to.
func SchemaVersion(buildInfo build.BuildInfo) int { return newMigrator(buildInfo).Version() }

func prepareSchema(db *sql.DB, logger *xlog.Logger, buildInfo build.BuildInfo, policy MigrationPolicy) error {
	if policy != RequireCurrentSchema && policy != ApplyPendingMigrations {
		return fmt.Errorf("invalid migration policy %d", policy)
	}

	ctx := context.Background()
	current, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}
	m := newMigrator(buildInfo)
	required := m.Version()
	if current > required {
		return fmt.Errorf("database schema version %d is newer than this build's %d", current, required)
	}
	if current == required {
		logger.Infof("Database schema at version %d", current)
		return nil
	}
	if policy != ApplyPendingMigrations {
		return fmt.Errorf("database schema version %d is behind required version %d; run the installer", current, required)
	}
	version, err := m.Run(ctx, db, logger)
	if err != nil {
		return err
	}
	logger.Infof("Database schema at version %d", version)
	return nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}
	return current, nil
}
