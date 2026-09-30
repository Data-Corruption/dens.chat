package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

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
	// version 1 (PRAGMA user_version), the next to 2, and so on. Until the
	// first release the initial step changes in place.

	m.Add("Initial Schema", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE TABLE config (
				key   TEXT PRIMARY KEY,
				value TEXT NOT NULL
			) STRICT;

			-- One row: the data key's check value, and the data key sealed
			-- under the local password once one is set.
			CREATE TABLE vault (
				id            INTEGER PRIMARY KEY CHECK (id = 1),
				check_value   BLOB NOT NULL,
				password_wrap TEXT,             -- JSON; NULL until a password is set
				updated_at    INTEGER NOT NULL  -- unix milliseconds
			) STRICT;

			-- Paired browser sessions, keyed by SHA-256 of the cookie token.
			CREATE TABLE client_sessions (
				token_hash   TEXT PRIMARY KEY,
				created_at   INTEGER NOT NULL, -- unix milliseconds
				expires_at   INTEGER NOT NULL, -- unix milliseconds
				last_seen_at INTEGER NOT NULL  -- unix milliseconds
			) STRICT;

			-- Dens this install has joined, keyed by den_id. The profile (URL,
			-- name, account) and the device key are sealed with the data key.
			CREATE TABLE joined_dens (
				den_id     BLOB PRIMARY KEY,
				profile    BLOB NOT NULL,
				device_key BLOB NOT NULL,
				joined_at  INTEGER NOT NULL
			) STRICT;

			-- The den this install hosts, once created. The identity private
			-- key is sealed with the data key. The upload limits are bytes.
			CREATE TABLE den (
				id             INTEGER PRIMARY KEY CHECK (id = 1),
				name           TEXT NOT NULL,
				url            TEXT NOT NULL,
				public_key     BLOB NOT NULL,
				private_key    BLOB NOT NULL,
				created_at     INTEGER NOT NULL,
				file_size      INTEGER NOT NULL,
				member_storage INTEGER NOT NULL,
				den_storage    INTEGER NOT NULL
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
				banner_id     INTEGER REFERENCES den_files (id) ON DELETE SET NULL
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
			-- name, group or position.
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
				CHECK ((kind = 'dm') = (dm_low IS NOT NULL AND dm_high IS NOT NULL AND dm_low < dm_high))
			) STRICT;
			CREATE UNIQUE INDEX den_channels_dm ON den_channels (dm_low, dm_high) WHERE kind = 'dm';
			CREATE INDEX den_channels_dm_high ON den_channels (dm_high) WHERE kind = 'dm';

			-- AUTOINCREMENT: message IDs grow with time and are never reused,
			-- which history paging (before, after, around an ID) relies on.
			-- The text is sealed with the data key; reply_to may name a
			-- deleted message.
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
				nonce      BLOB NOT NULL -- the author's idempotency key
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
			-- the thumb columns are set when the den made a preview.
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
				created_at   INTEGER NOT NULL
			) STRICT;
			CREATE INDEX den_files_message ON den_files (message_id, position);
			CREATE INDEX den_files_uploader ON den_files (uploader_id);
			CREATE INDEX den_files_pending ON den_files (created_at) WHERE message_id IS NULL;

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

	return m
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
