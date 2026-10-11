// Package cache is the client's cache of what it fetches from other dens
// (M6): files and their previews, kept as they're read, a chunk at a time,
// and where each den's changes stood when the client last synced with it.
//
// It holds only copies of what dens hold, so backups leave it out, and a
// cache of another format, or one sealed under another data key, is
// emptied rather than read. Everything in it is sealed with the data key.
package cache

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Format is the cache's format, kept as its database's user_version. A
// cache of another format starts again from empty: it holds nothing that
// can't be fetched again.
const Format = 1

// The cache's size, which the member chooses in the settings.
const (
	DefaultLimit int64 = 2 << 30
	MinLimit     int64 = 512 << 20
	MaxLimit     int64 = 50 << 30
)

const (
	dbName   = "cache.db"
	filesDir = "files"
	// idleEntries is how many files no handle reads stay known in memory,
	// with where their chunks are, so a player seeking through a video
	// doesn't make the cache read its slots again for each range.
	idleEntries = 64
)

// Store is the cache: a database of what it holds, and a directory of
// files, each a run of sealed chunks.
type Store struct {
	dir string
	db  *sql.DB
	v   *vault.Vault
	log *xlog.Logger
	now func() time.Time

	// mu guards what the cache holds: its rows and files, the entries in
	// memory and the space they take. A chunk is read and written without
	// it, under its entry's own locks.
	mu      sync.Mutex
	limit   int64
	used    int64
	entries map[key]*entry
	idle    []*entry // entries no handle reads, least recently used first
	dens    map[string]int64
	closed  bool
}

// Open opens the cache in dir, emptying it first if it's of another
// format or sealed under another key. limit is its size in bytes, or 0 for
// DefaultLimit.
func Open(dir string, v *vault.Vault, log *xlog.Logger, limit int64) (*Store, error) {
	s := &Store{dir: dir, v: v, log: log, now: time.Now, limit: clampLimit(limit), entries: map[key]*entry{},
		dens: map[string]int64{}}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

func clampLimit(n int64) int64 {
	if n == 0 {
		return DefaultLimit
	}
	return min(max(n, MinLimit), MaxLimit)
}

func (s *Store) open() error {
	if err := os.MkdirAll(filepath.Join(s.dir, filesDir), 0o700); err != nil {
		return fmt.Errorf("create the cache: %w", err)
	}
	db, err := database.Open(filepath.Join(s.dir, dbName))
	if err != nil {
		return fmt.Errorf("open the cache: %w", err)
	}
	why, err := s.check(db)
	if err != nil {
		db.Close()
		return fmt.Errorf("read the cache: %w", err)
	}
	if why != "" {
		db.Close()
		s.log.Infof("Emptying the cache: %s", why)
		if err := s.wipe(); err != nil {
			return err
		}
		if db, err = database.Open(filepath.Join(s.dir, dbName)); err != nil {
			return fmt.Errorf("open the cache: %w", err)
		}
		if _, err := s.check(db); err != nil {
			db.Close()
			return fmt.Errorf("set up the cache: %w", err)
		}
	}
	s.db = db
	if err := s.tidy(); err != nil {
		db.Close()
		return fmt.Errorf("tidy the cache: %w", err)
	}
	return nil
}

// check sets up a new cache, and says why an existing one can't be read,
// or "" when it can.
func (s *Store) check(db *sql.DB) (string, error) {
	ctx := context.Background()
	var format int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&format); err != nil {
		return "", err
	}
	if format == 0 {
		var tables int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema`).Scan(&tables); err != nil {
			return "", err
		}
		if tables != 0 {
			return "it has no format", nil
		}
		return "", s.create(ctx, db)
	}
	if format != Format {
		return fmt.Sprintf("its format is %d, and this version of Dens keeps %d", format, Format), nil
	}
	var check []byte
	if err := db.QueryRowContext(ctx, `SELECT check_value FROM meta WHERE id = 1`).Scan(&check); err != nil {
		return "", err
	}
	if s.v.VerifyCheckValue(check) != nil {
		return "it's sealed under another key", nil
	}
	return "", nil
}

func (s *Store) create(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		-- The data key's check value: a cache sealed under another key is
		-- emptied, not read.
		CREATE TABLE meta (
			id          INTEGER PRIMARY KEY CHECK (id = 1),
			check_value BLOB NOT NULL
		) STRICT;

		-- The dens the cache holds anything of, and the epoch and version
		-- of their changes the client last synced at; epoch is NULL before
		-- the first.
		CREATE TABLE dens (
			den     INTEGER PRIMARY KEY,
			den_id  BLOB NOT NULL UNIQUE,
			epoch   BLOB,
			version INTEGER NOT NULL DEFAULT 0
		) STRICT;

		-- Files and their previews (thumb), each held in the file name
		-- names, of which held bytes are on disk, as slots of sealed chunks.
		-- A file is on a message, in its channel, or is a member's picture.
		CREATE TABLE files (
			den     INTEGER NOT NULL,
			id      INTEGER NOT NULL,
			thumb   INTEGER NOT NULL,
			name    TEXT NOT NULL UNIQUE,
			size    INTEGER NOT NULL,
			held    INTEGER NOT NULL DEFAULT 0,
			message INTEGER,
			channel INTEGER,
			member  INTEGER,
			used_at INTEGER NOT NULL,
			PRIMARY KEY (den, id, thumb)
		) STRICT;
		CREATE INDEX files_used ON files (used_at);
		CREATE INDEX files_message ON files (den, message) WHERE message IS NOT NULL;
		CREATE INDEX files_channel ON files (den, channel) WHERE channel IS NOT NULL;
		CREATE INDEX files_member ON files (den, member) WHERE member IS NOT NULL;
	`)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (id, check_value) VALUES (1, ?)`, s.v.CheckValue()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, Format)); err != nil {
		return err
	}
	return tx.Commit()
}

// wipe empties the cache's directory of what the cache keeps there.
func (s *Store) wipe() error {
	for _, name := range []string{dbName, dbName + "-wal", dbName + "-shm"} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("empty the cache: %w", err)
		}
	}
	files := filepath.Join(s.dir, filesDir)
	if err := os.RemoveAll(files); err != nil {
		return fmt.Errorf("empty the cache: %w", err)
	}
	return os.MkdirAll(files, 0o700)
}

// tidy matches the files on disk with the rows that name them, as after a
// crash between the two, and counts the space they take.
func (s *Store) tidy() error {
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx, `SELECT name, held FROM files`)
	if err != nil {
		return err
	}
	named := map[string]bool{}
	for rows.Next() {
		var name string
		var held int64
		if err := rows.Scan(&name, &held); err != nil {
			rows.Close()
			return err
		}
		named[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	dir := filepath.Join(s.dir, filesDir)
	list, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	on := map[string]bool{}
	for _, d := range list {
		if !named[d.Name()] {
			if err := os.Remove(filepath.Join(dir, d.Name())); err != nil {
				s.log.Warnf("remove a file the cache no longer holds: %v", err)
			}
			continue
		}
		on[d.Name()] = true
	}
	for name := range named {
		if !on[name] {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE name = ?`, name); err != nil {
				return err
			}
		}
	}
	return s.db.QueryRowContext(ctx, `SELECT coalesce(sum(held), 0) FROM files`).Scan(&s.used)
}

// Close closes the cache's database. Handles still open keep reading what
// they hold.
func (s *Store) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.db.Close()
}

// Limit is the cache's size, and Used how much it holds.
func (s *Store) Limit() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}

func (s *Store) Used() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

// SetLimit changes the cache's size, and lets go of what was used longest
// ago until it fits. A size outside MinLimit to MaxLimit is refused.
func (s *Store) SetLimit(n int64) error {
	if n < MinLimit || n > MaxLimit {
		return fmt.Errorf("the cache's size is %d to %d bytes", MinLimit, MaxLimit)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limit = n
	return s.evictLocked()
}

// Clear empties the cache of files. Where each den's changes stood stays:
// an empty cache is as current as any.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropLocked(`1`)
}

// den returns a den's number in the cache, adding the den first if the
// cache holds nothing of it yet. The caller holds s.mu.
func (s *Store) denLocked(denID []byte) (int64, error) {
	if n, ok := s.dens[string(denID)]; ok {
		return n, nil
	}
	var n int64
	err := s.db.QueryRow(`INSERT INTO dens (den_id) VALUES (?) ON CONFLICT (den_id) DO UPDATE SET den_id = den_id RETURNING den`,
		denID).Scan(&n)
	if err != nil {
		return 0, err
	}
	s.dens[string(denID)] = n
	return n, nil
}

// Synced is where a den's changes stood when the client last synced with
// it: the den's epoch and version.
type Synced struct {
	Epoch   []byte
	Version int64
}

// Synced returns where a den's changes stood when the client last synced
// with it, and false before the first time.
func (s *Store) Synced(denID []byte) (Synced, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sy Synced
	err := s.db.QueryRow(`SELECT epoch, version FROM dens WHERE den_id = ?`, denID).Scan(&sy.Epoch, &sy.Version)
	if errors.Is(err, sql.ErrNoRows) || err == nil && sy.Epoch == nil {
		return Synced{}, false, nil
	}
	return sy, err == nil, err
}

// SetSynced records where a den's changes stood as the client synced with
// it.
func (s *Store) SetSynced(denID []byte, sy Synced) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, err := s.denLocked(denID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE dens SET epoch = ?, version = ? WHERE den = ?`, bytes.Clone(sy.Epoch), sy.Version, den)
	return err
}

// DropDen forgets everything the cache holds of a den.
func (s *Store) DropDen(denID []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok := s.dens[string(denID)]
	if !ok {
		if err := s.db.QueryRow(`SELECT den FROM dens WHERE den_id = ?`, denID).Scan(&den); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
	}
	if err := s.dropLocked(`den = ?`, den); err != nil {
		return err
	}
	delete(s.dens, string(denID))
	_, err := s.db.Exec(`DELETE FROM dens WHERE den = ?`, den)
	return err
}
