package database_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/clientsessions"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/internal/types"
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

// A den's export holds the den alone (M10): every den table as it is, the
// data key's check value and its password wrap, and none of the client,
// whose rows leave no trace in the file. It's one file, which an older
// release's restore opens as it does a backup's.
func TestSnapshotDenHoldsTheDenAlone(t *testing.T) {
	db, err := openTestDB(t, filepath.Join(t.TempDir(), "db"), database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	bi := build.BuildInfo{DefaultLogLevel: "warn"}
	if err := vaultstore.Init(ctx, db, []byte("check-value-1234")); err != nil {
		t.Fatal(err)
	}
	if err := vaultstore.SetPasswordWrap(ctx, db, `{"wrap":"under the local password"}`); err != nil {
		t.Fatal(err)
	}
	if err := clientsessions.Create(ctx, db, "browser-session-marker", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Update(db, func(c *types.Configuration) error {
		c.LogLevel = "debug"
		c.SetHosting(true)
		c.CacheLimit = 1 << 30
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE vault SET dm_seal = CAST('dm-seal-marker' AS BLOB)`,
		`INSERT INTO joined_dens (den_id, profile, device_key, joined_at, dm_seal)
			VALUES (CAST('den-id-marker' AS BLOB), CAST('profile-marker' AS BLOB), CAST('device-key-marker' AS BLOB), 1, CAST('member-seal-marker' AS BLOB))`,
		`INSERT INTO den (id, name, url, public_key, private_key, created_at, file_size, member_storage, den_storage,
			call_members, den_callers, shares, share_viewers, share_bitrate, share_height, share_fps, sync_epoch)
			VALUES (1, 'Marker Den', 'https://den.test', x'01', x'02', 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, x'03')`,
		`INSERT INTO den_members (username, display_name, role, verifier_hash, joined_at, seal_check)
			VALUES ('alice', 'Alice Marker', 'owner', x'04', 1, x'05')`,
		`INSERT INTO den_groups (name, position) VALUES ('Group Marker', 0)`,
		`INSERT INTO den_channels (name, kind, position, created_at) VALUES ('general-marker', 'text', 0, 1)`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	path := filepath.Join(t.TempDir(), "db.sqlite")
	if err := database.SnapshotDen(ctx, db, path, bi); err != nil {
		t.Fatal(err)
	}
	if extra, _ := filepath.Glob(path + "-*"); len(extra) > 0 {
		t.Fatalf("the snapshot left %v beside it", extra)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"browser-session-marker", "dm-seal-marker", "den-id-marker", "profile-marker", "device-key-marker", "member-seal-marker"} {
		if bytes.Contains(file, []byte(gone)) {
			t.Errorf("the snapshot's file still holds %q", gone)
		}
	}
	for _, kept := range []string{"Marker Den", "Alice Marker", "Group Marker", "general-marker"} {
		if !bytes.Contains(file, []byte(kept)) {
			t.Errorf("the snapshot's file lacks %q", kept)
		}
	}

	snapshot, err := database.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var mode string
	if err := snapshot.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "delete" {
		t.Fatalf("the snapshot's journal mode is %q, %v", mode, err)
	}
	tables, err := database.Tables(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		live, copied := dump(t, db, table), dump(t, snapshot, table)
		switch {
		case database.IsDenTable(table):
			if live != copied {
				t.Errorf("%s differs: the instance has\n%s\nand the snapshot\n%s", table, live, copied)
			}
		case table == "vault" || table == "config":
		case copied != "":
			t.Errorf("the snapshot's %s holds\n%s", table, copied)
		}
	}
	row, err := vaultstore.Get(ctx, snapshot)
	if err != nil || string(row.CheckValue) != "check-value-1234" || row.PasswordWrap != `{"wrap":"under the local password"}` {
		t.Fatalf("the snapshot's vault is %+v, %v", row, err)
	}
	var seal []byte
	if err := snapshot.QueryRow(`SELECT dm_seal FROM vault`).Scan(&seal); err != nil || seal != nil {
		t.Fatalf("the snapshot's vault keeps the DM seal %q, %v", seal, err)
	}
	cfg, err := config.View(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if fresh := types.DefaultConfig(bi); !reflect.DeepEqual(*cfg, fresh) {
		t.Fatalf("the snapshot's settings are %+v, not a new install's %+v", cfg, fresh)
	}
}

// dump lists a table's rows, in order, as text.
func dump(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%q", values))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}
