// Package vaultstore reads and writes the vault row: the data key's check
// value and its password-wrapped copy.
package vaultstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Row is the stored vault state.
type Row struct {
	CheckValue []byte
	// PasswordWrap is the JSON password wrap, or empty until a local
	// password is set.
	PasswordWrap string
	UpdatedAt    time.Time
}

// Get returns the vault row, or nil if the vault is not initialized yet.
func Get(ctx context.Context, db *sql.DB) (*Row, error) {
	var row Row
	var wrap sql.NullString
	var updated int64
	err := db.QueryRowContext(ctx,
		`SELECT check_value, password_wrap, updated_at FROM vault WHERE id = 1`,
	).Scan(&row.CheckValue, &wrap, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read vault: %w", err)
	}
	row.PasswordWrap = wrap.String
	row.UpdatedAt = time.UnixMilli(updated)
	return &row, nil
}

// Init records the data key's check value in a new vault.
func Init(ctx context.Context, db *sql.DB, checkValue []byte) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO vault (id, check_value, updated_at) VALUES (1, ?, ?)`,
		checkValue, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("initialize vault: %w", err)
	}
	return nil
}

// SetPasswordWrap stores a new password-wrapped copy of the data key.
func SetPasswordWrap(ctx context.Context, db *sql.DB, wrap string) error {
	result, err := db.ExecContext(ctx,
		`UPDATE vault SET password_wrap = ?, updated_at = ? WHERE id = 1`,
		wrap, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("store password wrap: %w", err)
	}
	if n, err := result.RowsAffected(); err == nil && n != 1 {
		return errors.New("vault is not initialized")
	}
	return nil
}
