package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/updatelease"
	"github.com/Data-Corruption/dens.chat/internal/platform/release"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func TestCheckForUpdatePersistsResult(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Chmod(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	logger, err := xlog.New(filepath.Join(tmp, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	buildInfo := build.Info()
	buildInfo.Version = "v1.0.0"
	db, err := database.New(filepath.Join(tmp, "db"), logger, buildInfo, database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	appLayout := layout.FromStorage(tmp, "sprout")
	if err := appLayout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appLayout.ReleaseURL, []byte("https://example.invalid/"), 0600); err != nil {
		t.Fatal(err)
	}

	a := &App{
		DB:            db,
		Log:           logger,
		Layout:        appLayout,
		ReleaseSource: &MockReleaseSource{LatestVersion: "v1.1.0"},
		buildInfo:     buildInfo,
	}
	available, err := a.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("expected update to be available")
	}

	cfg, err := config.View(db)
	if err != nil {
		t.Fatal(err)
	}
	if !a.UpdateAvailable(cfg) || cfg.LastUpdateCheck.IsZero() {
		t.Fatalf("notification state not persisted: %+v", cfg)
	}
}

type updateTestReleaseSource interface {
	GetLatestVersion(context.Context, string) (string, error)
}

func newUpdateTestApp(t *testing.T, source updateTestReleaseSource) *App {
	t.Helper()
	tmp := t.TempDir()
	if err := os.Chmod(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	logger, err := xlog.New(filepath.Join(tmp, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	buildInfo := build.Info()
	buildInfo.Version = "v1.0.0"
	db, err := database.New(filepath.Join(tmp, "db"), logger, buildInfo, database.ApplyPendingMigrations)
	if err != nil {
		logger.Close()
		t.Fatal(err)
	}
	appLayout := layout.FromStorage(tmp, "sprout")
	if err := appLayout.Ensure(); err != nil {
		db.Close()
		logger.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(appLayout.ReleaseURL, []byte("https://example.invalid/"), 0600); err != nil {
		db.Close()
		logger.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		logger.Close()
	})
	return &App{
		DB:            db,
		Log:           logger,
		Layout:        appLayout,
		ReleaseSource: source,
		buildInfo:     buildInfo,
	}
}

func TestManualUpdateCheckIgnoresPeriodicLease(t *testing.T) {
	a := newUpdateTestApp(t, &MockReleaseSource{LatestVersion: "v1.1.0"})
	now := time.Now()
	if _, claimed, err := updatelease.Claim(
		context.Background(),
		a.DB,
		now,
		time.Minute,
	); err != nil {
		t.Fatal(err)
	} else if !claimed {
		t.Fatal("failed to establish periodic lease")
	}

	available, err := a.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("manual check did not run while periodic lease was held")
	}
	if _, claimed, err := updatelease.Claim(
		context.Background(),
		a.DB,
		now.Add(time.Second),
		time.Minute,
	); err != nil {
		t.Fatal(err)
	} else if claimed {
		t.Fatal("manual check removed the periodic lease")
	}
}

func TestFailedPeriodicUpdateCheckLeavesLease(t *testing.T) {
	a := newUpdateTestApp(t, &MockReleaseSource{Error: context.DeadlineExceeded})
	if _, _, err := a.checkForPeriodicUpdate(context.Background()); err == nil {
		t.Fatal("periodic check unexpectedly succeeded")
	}
	if _, claimed, err := updatelease.Claim(
		context.Background(),
		a.DB,
		time.Now(),
		time.Minute,
	); err != nil {
		t.Fatal(err)
	} else if claimed {
		t.Fatal("failed periodic check released its lease")
	}
}

func TestServiceUpdateCheckerKeepsRunningWithInvalidMetadata(t *testing.T) {
	a := newUpdateTestApp(t, &MockReleaseSource{LatestVersion: "v1.1.0"})
	if err := os.WriteFile(a.Layout.ReleaseURL, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunUpdateChecker(ctx, func() {}) }()
	select {
	case err := <-done:
		t.Fatalf("checker stopped on optional metadata error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("checker did not stop after cancellation")
	}
}

type blockingReleaseSource struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func TestCloseCancelsAndJoinsOneShotUpdateCheck(t *testing.T) {
	source := &blockingReleaseSource{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	a := newUpdateTestApp(t, source)
	if err := a.StartUpdateCheckIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("one-shot check did not start")
	}

	done := make(chan error, 1)
	go func() { done <- a.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and join the one-shot check")
	}
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("release source calls = %d, want 1", calls)
	}
}

func (s *blockingReleaseSource) GetLatestVersion(ctx context.Context, _ string) (string, error) {
	s.calls.Add(1)
	s.started <- struct{}{}
	select {
	case <-s.release:
		return "v1.1.0", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestDiscoveryFollowsMirrorAndInvalidatesOldSource(t *testing.T) {
	var calls atomic.Int32
	latest := "v1.1.0"
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/approved/version" {
			t.Errorf("unexpected mirror request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, latest)
	}))
	defer mirror.Close()
	a := newUpdateTestApp(t, &release.GenericReleaseSource{})
	source := mirror.URL + "/approved/"
	if err := os.WriteFile(a.Layout.ReleaseURL, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	available, completed, err := a.checkForPeriodicUpdate(context.Background())
	if err != nil || !available || !completed {
		t.Fatalf("mirror check: available=%t completed=%t err=%v", available, completed, err)
	}
	cfg, err := config.View(a.DB)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpdateCheckSource != source || cfg.LatestUpdateVersion != latest || !a.UpdateAvailable(cfg) {
		t.Fatalf("wrong mirror observation: %+v", cfg)
	}
	if a.periodicUpdateCheckDue(cfg, time.Now()) {
		t.Fatal("fresh mirror observation already due")
	}
	a.buildInfo.Version = latest
	if a.UpdateAvailable(cfg) {
		t.Fatal("successful upgrade left a stale notice")
	}
	a.buildInfo.Version = "v1.0.0"
	if err := os.WriteFile(a.Layout.ReleaseURL, []byte(mirror.URL+"/other/"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a.UpdateAvailable(cfg) || !a.periodicUpdateCheckDue(cfg, time.Now()) {
		t.Fatal("source change reused the previous mirror observation")
	}
	if err := os.Remove(a.Layout.ReleaseURL); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CheckForUpdate(context.Background()); !errors.Is(err, ErrUpdatesDisabled) {
		t.Fatalf("missing source: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("missing source made a network request; calls=%d", calls.Load())
	}
}

func TestHiddenNoticesDoNotDisableBackgroundChecks(t *testing.T) {
	a := newUpdateTestApp(t, &MockReleaseSource{LatestVersion: "v1.1.0"})
	if _, err := config.Update(a.DB, func(cfg *types.Configuration) error { cfg.UpdateNotifications = false; return nil }); err != nil {
		t.Fatal(err)
	}
	available, completed, err := a.checkForPeriodicUpdate(context.Background())
	if err != nil || !available || !completed {
		t.Fatalf("hidden notices disabled checking: %t %t %v", available, completed, err)
	}
	if _, err := config.Update(a.DB, func(cfg *types.Configuration) error {
		cfg.BackgroundUpdateChecks = false
		cfg.LastUpdateCheck = time.Time{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, completed, err = a.checkForPeriodicUpdate(context.Background())
	if err != nil || completed {
		t.Fatalf("disabled background check ran: %t %v", completed, err)
	}
	available, err = a.CheckForUpdate(context.Background())
	if err != nil || !available {
		t.Fatalf("background preference blocked manual check: %t %v", available, err)
	}
}
