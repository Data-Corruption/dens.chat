package migrator

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// open opens a database in a temporary directory with foreign keys on for
// every connection, as the service's are.
func open(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "test.db"))+"?_txlock=immediate&_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func logger(t *testing.T) *xlog.Logger {
	t.Helper()
	log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	return log
}

// remake makes the parents table again with a looser check, by SQLite's
// procedure: the new table, its rows, the old one dropped and the new one
// renamed.
func remake(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE parents_new (id INTEGER PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('a', 'b'))) STRICT;
		INSERT INTO parents_new (id, kind) SELECT id, kind FROM parents;
		DROP TABLE parents;
		ALTER TABLE parents_new RENAME TO parents;`)
	return err
}

func first(m *Migrator) {
	m.Add("parents and children", func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			CREATE TABLE parents (id INTEGER PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('a'))) STRICT;
			CREATE TABLE children (id INTEGER PRIMARY KEY, parent INTEGER NOT NULL REFERENCES parents (id) ON DELETE CASCADE) STRICT;
			INSERT INTO parents (id, kind) VALUES (1, 'a');
			INSERT INTO children (id, parent) VALUES (10, 1), (11, 1);`)
		return err
	})
}

// A step that remakes a table runs with foreign keys off, so dropping the
// old table takes none of the rows that refer to it, and foreign keys are
// on again once the run ends.
func TestRemakingKeepsWhatRefersToTheTable(t *testing.T) {
	ctx := context.Background()
	db, log := open(t), logger(t)
	m := New()
	first(m)
	if v, err := m.Run(ctx, db, log); err != nil || v != 1 {
		t.Fatalf("first run: %d, %v", v, err)
	}
	m.AddRemaking("parents of kind b", remake)
	m.Add("a parent of kind b", func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO parents (id, kind) VALUES (2, 'b')`)
		return err
	})
	if v, err := m.Run(ctx, db, log); err != nil || v != 3 {
		t.Fatalf("the run that remakes: %d, %v", v, err)
	}
	var children int
	if err := db.QueryRow(`SELECT count(*) FROM children`).Scan(&children); err != nil || children != 2 {
		t.Errorf("%d children after the remake, %v", children, err)
	}
	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil || on != 1 {
		t.Errorf("foreign keys are %d after the run, %v", on, err)
	}
	if _, err := db.Exec(`DELETE FROM parents WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM children`).Scan(&children); err != nil || children != 0 {
		t.Errorf("deleting the parent left %d children: foreign keys don't cascade, %v", children, err)
	}
}

// A run whose steps leave a row referring to one that isn't there fails
// before it commits, leaving the database as it was.
func TestRemakingChecksForeignKeys(t *testing.T) {
	ctx := context.Background()
	db, log := open(t), logger(t)
	m := New()
	first(m)
	m.AddRemaking("a remake that loses a parent", func(ctx context.Context, tx *sql.Tx) error {
		if err := remake(ctx, tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM parents`)
		return err
	})
	if _, err := m.Run(ctx, db, log); err == nil || !strings.Contains(err.Error(), "children") {
		t.Fatalf("a run that lost the children's parent: %v", err)
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 0 {
		t.Errorf("the failed run left version %d, %v", version, err)
	}
	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil || on != 1 {
		t.Errorf("foreign keys are %d after the failed run, %v", on, err)
	}
}
