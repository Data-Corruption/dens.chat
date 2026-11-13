// Package background keeps the picture behind the member's chat: one file
// of theirs, sealed with the data key as the vault's fields are. It stays
// on this install: the page shows it from the client listener.
package background

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// MaxSize is the largest file a background takes, in bytes.
const MaxSize = 10 << 20

// ErrNone is a chat with no picture behind it.
var ErrNone = errors.New("the chat has no background")

var fileAD = []byte("background.file")

// Put keeps file as the chat's background, in place of any before, and
// returns its version: when it was set, in unix milliseconds, and always
// past the one it replaces, since two set within a millisecond, or with the
// clock set back, would otherwise share one and pages keep the first.
func Put(ctx context.Context, db *sql.DB, v *vault.Vault, file []byte, now time.Time) (int64, error) {
	sealed, err := v.Seal(file, fileAD)
	if err != nil {
		return 0, err
	}
	var at int64
	if err := db.QueryRowContext(ctx, `INSERT INTO background (id, set_at, file) VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET set_at = max(excluded.set_at, background.set_at + 1), file = excluded.file
		RETURNING set_at`, now.UnixMilli(), sealed).Scan(&at); err != nil {
		return 0, fmt.Errorf("keep the chat's background: %w", err)
	}
	return at, nil
}

// Open returns the chat's background, opened, or ErrNone where there's
// none.
func Open(ctx context.Context, db *sql.DB, v *vault.Vault) ([]byte, error) {
	var sealed []byte
	err := db.QueryRowContext(ctx, `SELECT file FROM background WHERE id = 1`).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNone
	}
	if err != nil {
		return nil, fmt.Errorf("read the chat's background: %w", err)
	}
	file, err := v.Open(sealed, fileAD)
	if err != nil {
		return nil, fmt.Errorf("open the chat's background: %w", err)
	}
	return file, nil
}

// Version returns the background's version, or 0 where there's none.
func Version(ctx context.Context, db *sql.DB) (int64, error) {
	var at int64
	err := db.QueryRowContext(ctx, `SELECT set_at FROM background WHERE id = 1`).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read the chat's background's version: %w", err)
	}
	return at, nil
}

// Delete forgets the chat's background.
func Delete(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM background`); err != nil {
		return fmt.Errorf("forget the chat's background: %w", err)
	}
	return nil
}
