// Package sounds keeps the member's own sounds (M14.2): files of theirs that
// take the place of the page's sounds, each by the sound's name, sealed with
// the data key as the vault's fields are. They stay on this install: the
// page plays them from the client listener.
package sounds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// MaxSize is the largest file a sound takes, in bytes.
const MaxSize = 1 << 20

// ErrNone is a sound the member hasn't replaced.
var ErrNone = errors.New("that sound plays its default")

// fileAD binds a sealed file to its sound, so one can't be moved to another.
func fileAD(name string) []byte { return []byte("sounds.file:" + name) }

// Put keeps file as the member's own for the sound name, in place of any
// before, and returns its version: when it was set, in unix milliseconds.
func Put(ctx context.Context, db *sql.DB, v *vault.Vault, name string, file []byte, now time.Time) (int64, error) {
	sealed, err := v.Seal(file, fileAD(name))
	if err != nil {
		return 0, err
	}
	at := now.UnixMilli()
	if _, err := db.ExecContext(ctx, `INSERT INTO sounds (name, file, set_at) VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET file = excluded.file, set_at = excluded.set_at`, name, sealed, at); err != nil {
		return 0, fmt.Errorf("keep the %s sound: %w", name, err)
	}
	return at, nil
}

// Open returns the member's own file for the sound name, opened, with its
// version, or ErrNone where they haven't replaced it.
func Open(ctx context.Context, db *sql.DB, v *vault.Vault, name string) ([]byte, int64, error) {
	var sealed []byte
	var at int64
	err := db.QueryRowContext(ctx, `SELECT file, set_at FROM sounds WHERE name = ?`, name).Scan(&sealed, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNone
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read the %s sound: %w", name, err)
	}
	file, err := v.Open(sealed, fileAD(name))
	if err != nil {
		return nil, 0, fmt.Errorf("open the %s sound: %w", name, err)
	}
	return file, at, nil
}

// Delete forgets the member's own file for the sound name, which brings its
// default back.
func Delete(ctx context.Context, db *sql.DB, name string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM sounds WHERE name = ?`, name); err != nil {
		return fmt.Errorf("forget the %s sound: %w", name, err)
	}
	return nil
}

// List returns the version of each sound the member replaced, by name.
func List(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, set_at FROM sounds`)
	if err != nil {
		return nil, fmt.Errorf("list the member's sounds: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name string
		var at int64
		if err := rows.Scan(&name, &at); err != nil {
			return nil, err
		}
		out[name] = at
	}
	return out, rows.Err()
}
