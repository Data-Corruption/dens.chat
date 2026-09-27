package clientsessions

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := t.TempDir()
	logger, err := xlog.New(filepath.Join(tmp, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.New(filepath.Join(tmp, "db"), logger, build.BuildInfo{DefaultLogLevel: "warn"},
		database.ApplyPendingMigrations)
	if err != nil {
		logger.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		logger.Close()
	})
	return db
}

func validate(t *testing.T, db *sql.DB, hash string, now time.Time) (valid, renewed bool) {
	t.Helper()
	valid, renewed, err := Validate(context.Background(), db, hash, now)
	if err != nil {
		t.Fatal(err)
	}
	return valid, renewed
}

func TestSessionsSlideAtMostDaily(t *testing.T) {
	db := openDB(t)
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := Create(context.Background(), db, "h", start); err != nil {
		t.Fatal(err)
	}

	if valid, renewed := validate(t, db, "h", start.Add(23*time.Hour)); !valid || renewed {
		t.Fatalf("within a day: valid=%t renewed=%t, want valid without renewal", valid, renewed)
	}
	// Use on day 29 extends the session to 30 days from then.
	day29 := start.Add(29 * 24 * time.Hour)
	if valid, renewed := validate(t, db, "h", day29); !valid || !renewed {
		t.Fatalf("day 29: valid=%t renewed=%t, want an extension", valid, renewed)
	}
	lastUse := start.Add(Lifetime + time.Hour)
	if valid, _ := validate(t, db, "h", lastUse); !valid {
		t.Fatal("an extended session expired at its original expiry")
	}
	if valid, _ := validate(t, db, "h", lastUse.Add(Lifetime)); valid {
		t.Fatal("a session outlived 30 days without use")
	}
}

func TestUnusedSessionExpires(t *testing.T) {
	db := openDB(t)
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := Create(context.Background(), db, "h", start); err != nil {
		t.Fatal(err)
	}
	if valid, _ := validate(t, db, "h", start.Add(Lifetime)); valid {
		t.Fatal("a session was valid at its expiry")
	}
	if err := DeleteExpired(context.Background(), db, start.Add(Lifetime)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM client_sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d expired sessions remain", count)
	}
}

func TestDeleteAllSignsOutEveryBrowser(t *testing.T) {
	db := openDB(t)
	now := time.Now()
	for _, hash := range []string{"a", "b"} {
		if err := Create(context.Background(), db, hash, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteAll(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"a", "b"} {
		if valid, _ := validate(t, db, hash, now); valid {
			t.Fatalf("session %s survived DeleteAll", hash)
		}
	}
}
