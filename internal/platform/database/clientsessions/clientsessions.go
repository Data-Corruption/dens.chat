// Package clientsessions stores paired browser sessions. A session lasts
// Lifetime from its last extension and is extended at most once a day while
// in use, so a browser that keeps using Dens stays paired and one left alone
// for a month has to pair again. Sensitive actions re-ask for the local
// password regardless.
package clientsessions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Lifetime is how long an unused session stays valid.
const Lifetime = 30 * 24 * time.Hour

// renewAfter is how stale an expiry may get before a request extends it.
const renewAfter = 24 * time.Hour

// Create stores a new session for the token with hash tokenHash.
func Create(ctx context.Context, db *sql.DB, tokenHash string, now time.Time) error {
	ms := now.UnixMilli()
	_, err := db.ExecContext(ctx,
		`INSERT INTO client_sessions (token_hash, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?)`,
		tokenHash, ms, now.Add(Lifetime).UnixMilli(), ms)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// Validate reports whether the session is valid at now, extending it when
// its expiry is more than a day old. renewed reports an extension, after
// which the cookie's lifetime should be extended too.
func Validate(ctx context.Context, db *sql.DB, tokenHash string, now time.Time) (valid, renewed bool, err error) {
	var expires, lastSeen int64
	err = db.QueryRowContext(ctx,
		`SELECT expires_at, last_seen_at FROM client_sessions WHERE token_hash = ?`, tokenHash,
	).Scan(&expires, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("read session: %w", err)
	}
	if now.UnixMilli() >= expires {
		return false, false, nil
	}
	if now.Sub(time.UnixMilli(lastSeen)) >= renewAfter {
		if _, err := db.ExecContext(ctx,
			`UPDATE client_sessions SET expires_at = ?, last_seen_at = ? WHERE token_hash = ?`,
			now.Add(Lifetime).UnixMilli(), now.UnixMilli(), tokenHash,
		); err != nil {
			return false, false, fmt.Errorf("extend session: %w", err)
		}
		return true, true, nil
	}
	return true, false, nil
}

// Delete removes one session. Deleting an unknown session is not an error.
func Delete(ctx context.Context, db *sql.DB, tokenHash string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM client_sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired removes every session expired at now.
func DeleteExpired(ctx context.Context, db *sql.DB, now time.Time) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM client_sessions WHERE expires_at <= ?`, now.UnixMilli()); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}

// DeleteAll signs out every paired browser.
func DeleteAll(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM client_sessions`); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}
