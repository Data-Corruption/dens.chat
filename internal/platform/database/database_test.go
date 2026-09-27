package database_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/clientsessions"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func openTestDB(t *testing.T, dir string, policy database.MigrationPolicy) (*sql.DB, error) {
	t.Helper()
	logger, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logger.Close() })
	return database.New(dir, logger, build.BuildInfo{DefaultLogLevel: "warn"}, policy)
}

func TestMigrationPolicy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "db")
	if db, err := openTestDB(t, dir, database.RequireCurrentSchema); err == nil {
		db.Close()
		t.Fatal("a fresh database opened without an authorized migration")
	}
	db, err := openTestDB(t, dir, database.ApplyPendingMigrations)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg, err := config.View(db)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "warn" || !cfg.UpdateNotifications || !cfg.BackgroundUpdateChecks {
		t.Fatalf("default config = %+v", cfg)
	}
	db.Close()

	db, err = openTestDB(t, dir, database.RequireCurrentSchema)
	if err != nil {
		t.Fatalf("reopen current schema: %v", err)
	}
	defer db.Close()
	if got := db.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("pool size = %d, want 4", got)
	}
	var secureDelete int
	if err := db.QueryRow("PRAGMA secure_delete").Scan(&secureDelete); err != nil {
		t.Fatal(err)
	}
	if secureDelete != 1 {
		t.Fatalf("secure_delete = %d, want 1", secureDelete)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("database directory mode = %04o", info.Mode().Perm())
		}
	}
}

func TestSnapshotOpensReadOnly(t *testing.T) {
	db, err := openTestDB(t, filepath.Join(t.TempDir(), "db"), database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := vaultstore.Init(ctx, db, []byte("check-value-1234")); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := database.Snapshot(ctx, db, snapshot); err != nil {
		t.Fatal(err)
	}
	copyDB, err := database.OpenReadOnly(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	row, err := vaultstore.Get(ctx, copyDB)
	if err != nil || row == nil || string(row.CheckValue) != "check-value-1234" {
		t.Fatalf("snapshot vault = %+v, %v", row, err)
	}
	version, err := database.FileSchemaVersion(ctx, copyDB)
	if err != nil || version != database.SchemaVersion(build.BuildInfo{}) {
		t.Fatalf("snapshot schema version = %d, %v", version, err)
	}
	if _, err := copyDB.Exec(`DELETE FROM vault`); err == nil {
		t.Fatal("read-only snapshot accepted a write")
	}
}

func TestVaultRow(t *testing.T) {
	db, err := openTestDB(t, filepath.Join(t.TempDir(), "db"), database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if row, err := vaultstore.Get(ctx, db); err != nil || row != nil {
		t.Fatalf("empty vault = %+v, %v", row, err)
	}
	if err := vaultstore.SetPasswordWrap(ctx, db, "{}"); err == nil {
		t.Fatal("password wrap stored without a vault")
	}
	if err := vaultstore.Init(ctx, db, []byte("kcv")); err != nil {
		t.Fatal(err)
	}
	if err := vaultstore.Init(ctx, db, []byte("again")); err == nil {
		t.Fatal("vault initialized twice")
	}
	if err := vaultstore.SetPasswordWrap(ctx, db, `{"v":1}`); err != nil {
		t.Fatal(err)
	}
	row, err := vaultstore.Get(ctx, db)
	if err != nil || row.PasswordWrap != `{"v":1}` || string(row.CheckValue) != "kcv" {
		t.Fatalf("vault = %+v, %v", row, err)
	}
}

func TestClientSessionsSlideAndExpire(t *testing.T) {
	db, err := openTestDB(t, filepath.Join(t.TempDir(), "db"), database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	start := time.Now()
	if err := clientsessions.Create(ctx, db, "h1", start); err != nil {
		t.Fatal(err)
	}
	if ok, renewed, err := clientsessions.Validate(ctx, db, "h1", start.Add(time.Hour)); err != nil || !ok || renewed {
		t.Fatalf("fresh session = %v, %v, %v", ok, renewed, err)
	}
	// Used again after 29 days: still valid, and extended.
	later := start.Add(29 * 24 * time.Hour)
	if ok, renewed, _ := clientsessions.Validate(ctx, db, "h1", later); !ok || !renewed {
		t.Fatalf("session in use: valid %v renewed %v", ok, renewed)
	}
	if ok, _, _ := clientsessions.Validate(ctx, db, "h1", later.Add(20*24*time.Hour)); !ok {
		t.Fatal("extension did not take")
	}
	// Left alone past its lifetime: expired.
	if ok, _, _ := clientsessions.Validate(ctx, db, "h1", later.Add(80*24*time.Hour)); ok {
		t.Fatal("abandoned session still valid")
	}
	if ok, _, _ := clientsessions.Validate(ctx, db, "unknown", start); ok {
		t.Fatal("unknown session valid")
	}
	if err := clientsessions.Delete(ctx, db, "h1"); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := clientsessions.Validate(ctx, db, "h1", start); ok {
		t.Fatal("deleted session valid")
	}
}
