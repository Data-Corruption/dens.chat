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
