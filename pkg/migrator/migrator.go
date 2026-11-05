// Package migrator is a minimal, ordered schema migration runner for SQLite
// (or any database/sql driver that supports SQLite's PRAGMA user_version).
//
// All pending steps run inside a single write transaction, keyed on
// PRAGMA user_version: whichever process grabs the write lock first migrates;
// everyone else sees the final version and no-ops. For this to be safe under
// concurrency, connections should begin write transactions immediately
// (e.g. the ncruces driver's `_txlock=immediate` DSN parameter) so the
// transaction can't fail a read-to-write lock upgrade mid-migration.
//
// A step that remakes a table, as SQLite changes a table's checks, runs
// with foreign keys off, as SQLite's procedure for it says: dropping the
// old table with them on would delete every row that refers to it.
// SQLite can't turn them off within a transaction, so a run that includes
// such a step turns them off on its connection before it begins, for all
// its pending steps, checks every foreign key before it commits, and turns
// them on again.
package migrator

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Operation defines the actual database modification.
type Operation func(ctx context.Context, tx *sql.Tx) error

// Migration represents a single version step.
type Migration struct {
	Version int    // schema version after this step (PRAGMA user_version)
	Desc    string // Human readable description for logs
	Up      Operation
	// Remakes says the step remakes a table, so it runs with foreign keys
	// off.
	Remakes bool
}

// Migrator manages the execution of migrations.
type Migrator struct {
	steps []Migration
}

// New creates a Migrator instance with an empty migration list.
func New() *Migrator {
	return &Migrator{
		steps: make([]Migration, 0),
	}
}

// Add registers a new migration step.
// Order matters! The first step registered takes a fresh database to schema
// version 1, the next to version 2, and so on.
func (m *Migrator) Add(desc string, op Operation) {
	m.steps = append(m.steps, Migration{
		Version: len(m.steps) + 1,
		Desc:    desc,
		Up:      op,
	})
}

// AddRemaking registers a step that remakes a table, which runs with
// foreign keys off, and every foreign key checked before it commits.
func (m *Migrator) AddRemaking(desc string, op Operation) {
	m.Add(desc, op)
	m.steps[len(m.steps)-1].Remakes = true
}

// Version returns the schema version produced by all registered migrations.
func (m *Migrator) Version() int {
	return len(m.steps)
}

// Upto returns a migrator with this one's first n steps, which takes a
// database to schema version n and no further, as a test of a later step
// starts from a database a release left.
func (m *Migrator) Upto(n int) *Migrator {
	return &Migrator{steps: m.steps[:min(n, len(m.steps))]}
}

// Run executes all pending migrations based on the current PRAGMA user_version
// and updates it to the final version. It returns the schema version the
// database ends up at.
func (m *Migrator) Run(ctx context.Context, db *sql.DB, logger *xlog.Logger) (int, error) {
	// One connection throughout, since a step that remakes a table turns
	// its foreign keys off before the transaction begins.
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to take a connection for migrations: %w", err)
	}
	defer conn.Close()
	var current int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return 0, fmt.Errorf("failed to read user_version: %w", err)
	}
	remakes := false
	for _, step := range m.steps[min(current, len(m.steps)):] {
		remakes = remakes || step.Remakes
	}
	if remakes {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return current, fmt.Errorf("failed to turn foreign keys off: %w", err)
		}
		// Before the connection goes back to the pool, whatever happened.
		defer conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON") //nolint:errcheck // a failure leaves them off on one pooled connection
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	// 1. Determine where to start, now that the write lock is held: another
	// process may have migrated meanwhile.
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return 0, fmt.Errorf("failed to read user_version: %w", err)
	}
	if current > len(m.steps) {
		return current, fmt.Errorf("database schema version %d is newer than known migrations (%d); database state is unknown", current, len(m.steps))
	}
	if current == len(m.steps) {
		return current, nil // up-to-date
	}

	// 2. Apply pending migrations
	for _, step := range m.steps[current:] {
		if step.Remakes && !remakes {
			// Only another process's run, ended before this one began, could
			// leave one pending that wasn't before; this one runs again.
			return current, fmt.Errorf("migration %d remakes a table, which takes foreign keys off before it begins; run it again", step.Version)
		}
		logger.Infof("Applying migration %d: %s", step.Version, step.Desc)
		if err := step.Up(ctx, tx); err != nil {
			return current, fmt.Errorf("failed to apply migration %d (%s): %w", step.Version, step.Desc, err)
		}
	}
	if remakes {
		if err := checkForeignKeys(ctx, tx); err != nil {
			return current, err
		}
	}

	// 3. Persist the new version (part of the same transaction; PRAGMA
	// doesn't support parameters, but the value is our own step count).
	final := len(m.steps)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", final)); err != nil {
		return current, fmt.Errorf("failed to set user_version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return current, fmt.Errorf("failed to commit migrations: %w", err)
	}
	return final, nil
}

// checkForeignKeys fails if any row refers to one that isn't there, as a
// remade table would leave them if it lost rows.
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("failed to check foreign keys: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowid sql.NullInt64
		var parent string
		var fk int
		if err := rows.Scan(&table, &rowid, &parent, &fk); err != nil {
			return fmt.Errorf("failed to read a foreign key check: %w", err)
		}
		return fmt.Errorf("after the migrations, row %d of %s refers to a row of %s that isn't there", rowid.Int64, table, parent)
	}
	return rows.Err()
}
