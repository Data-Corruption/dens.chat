// Package database provides the embedded SQLite database for the service.
//
// The driver is github.com/ncruces/go-sqlite3: SQLite compiled to Wasm and
// translated to pure Go, so there is no cgo and cross-compiling stays a plain
// GOOS/GOARCH matter. v0.35.3 is a hard minimum: earlier versions corrupt
// data under concurrent WAL access on Windows (upstream issue 404).
//
// The exposed API is plain database/sql; only the DSN below is
// driver-specific.
package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" database/sql driver
)

// ConfigDataKey is the key of the marshaled config struct in the config table.
const ConfigDataKey = "data"

// The schema lives in migration.go. The schema version is PRAGMA
// user_version, managed by pkg/migrator.

// FileName is the SQLite database file name inside the db directory.
// SQLite keeps "-wal" and "-shm" siblings beside it while the DB is open.
const FileName = "app.db"

// New opens (creating if needed) the SQLite database in the given directory
// and enforces the requested migration policy.
//
// Connection behavior, applied per pooled connection via the DSN:
//   - _txlock=immediate: write transactions take the write lock at BEGIN,
//     avoiding read-to-write upgrade failures.
//   - busy_timeout(10000): writers wait up to 10s for each other instead of
//     failing with SQLITE_BUSY.
//   - journal_mode(wal): negotiated once below before migration.
//   - synchronous(normal): the standard WAL durability tradeoff (atomicity is
//     preserved on crash; at most the last transactions before an OS crash
//     may roll back).
//   - foreign_keys(on): enforce FK constraints (off by default in SQLite).
//   - secure_delete(on): overwrite deleted content, so a deleted message
//     doesn't linger in free pages.
func New(
	directory string,
	logger *xlog.Logger,
	buildInfo build.BuildInfo,
	migrationPolicy MigrationPolicy,
) (*sql.DB, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create database dir: %w", err)
	}
	path := filepath.Join(directory, FileName)

	db, err := Open(path)
	if err != nil {
		return nil, err
	}

	if err := prepareSchema(db, logger, buildInfo, migrationPolicy); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare database schema: %w", err)
	}
	logger.Infof("SQLite initialized at %s", path)

	return db, nil
}

// Open opens (creating if needed) the SQLite database at path, with the
// connection behavior New describes, in WAL mode, and leaves its schema to
// the caller: the client's cache keeps a format of its own (M6).
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) +
		"?_txlock=immediate" +
		"&_pragma=busy_timeout(10000)" +
		"&_pragma=synchronous(normal)" +
		"&_pragma=foreign_keys(on)" +
		"&_pragma=secure_delete(on)"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	// Each connection carries its own sandboxed Wasm memory, so keep the pool
	// modest. SQLite serializes writers anyway; readers scale with the pool.
	db.SetMaxOpenConns(4)

	if err := enableWAL(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL journal mode: %w", err)
	}
	return db, nil
}

// enableWAL negotiates the persistent journal mode before migration.
// Retrying only SQLite lock errors keeps a first start deterministic without
// hiding malformed DSNs or I/O failures.
func enableWAL(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for {
		var mode string
		err := db.QueryRowContext(ctx, `PRAGMA journal_mode = wal`).Scan(&mode)
		if err == nil {
			if strings.EqualFold(mode, "wal") {
				return nil
			}
			return fmt.Errorf("SQLite selected journal mode %q, want wal", mode)
		}
		if !errors.Is(err, sqlite3.BUSY) && !errors.Is(err, sqlite3.LOCKED) {
			return err
		}

		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("timed out waiting to set WAL journal mode: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// Snapshot writes a consistent copy of the open database to path with VACUUM
// INTO, which is safe while the database is in use. path must not exist.
func Snapshot(ctx context.Context, db *sql.DB, path string) error {
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	return nil
}

// IsDenTable reports whether a table holds the den's rows rather than the
// client's: the den's tables are den and den_*, and the client's the rest,
// so a den's export keeps the den's alone, and removing the den empties
// them (M10).
func IsDenTable(name string) bool { return name == "den" || strings.HasPrefix(name, "den_") }

// Querier runs queries: a *sql.DB or a *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Tables lists the database's tables, but for SQLite's own.
func Tables(ctx context.Context, q Querier) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// SnapshotDen writes a copy of the open database to path, as Snapshot does,
// that holds the den alone: its export, which moves it to another computer
// (M10). The den's tables stay as they are, and so do the data key sealed
// under the local password and its check value; every other table empties,
// the joined dens with their device keys and DM seals and the paired
// browsers among them, the vault forgets the DM seal new dens start with,
// and the settings start as a new install's. Those rows go with
// secure_delete on, and the copy is vacuumed, so no free page keeps them.
func SnapshotDen(ctx context.Context, db *sql.DB, path string, buildInfo build.BuildInfo) error {
	if err := Snapshot(ctx, db, path); err != nil {
		return err
	}
	snapshot, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(on)&_pragma=secure_delete(on)")
	if err != nil {
		return fmt.Errorf("open the snapshot: %w", err)
	}
	defer snapshot.Close()
	snapshot.SetMaxOpenConns(1)
	// A rollback journal rather than the log, so the copy is one file.
	var mode string
	if err := snapshot.QueryRowContext(ctx, `PRAGMA journal_mode = delete`).Scan(&mode); err != nil {
		return fmt.Errorf("set the snapshot's journal: %w", err)
	}
	if !strings.EqualFold(mode, "delete") {
		return fmt.Errorf("the snapshot kept journal mode %q", mode)
	}
	settings, err := json.Marshal(types.DefaultConfig(buildInfo))
	if err != nil {
		return err
	}
	tx, err := snapshot.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	tables, err := Tables(ctx, tx)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if IsDenTable(table) || table == "vault" || table == "config" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`); err != nil {
			return fmt.Errorf("empty %s in the snapshot: %w", table, err)
		}
	}
	for _, step := range []struct {
		query string
		args  []any
	}{
		{`UPDATE vault SET dm_seal = NULL`, nil},
		{`DELETE FROM config WHERE key != ?`, []any{ConfigDataKey}},
		{`UPDATE config SET value = ? WHERE key = ?`, []any{string(settings), ConfigDataKey}},
	} {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("leave the client out of the snapshot: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := snapshot.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum the snapshot: %w", err)
	}
	return snapshot.Close()
}

// OpenReadOnly opens the database file at path without the ability to change
// it, and without enforcing a schema version. Restore uses it to inspect a
// backup before installing it.
func OpenReadOnly(path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout(10000)"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

// FileSchemaVersion reads the schema version of an open database.
func FileSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	return schemaVersion(ctx, db)
}
