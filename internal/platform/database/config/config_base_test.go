package config

import (
	"path/filepath"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func TestUpdateNormalizesAndValidatesBaseLogLevel(t *testing.T) {
	tmp := t.TempDir()
	logger, err := xlog.New(filepath.Join(tmp, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.New(filepath.Join(tmp, "db"), logger, build.BuildInfo{
		DefaultLogLevel: "warn",
	}, database.ApplyPendingMigrations)
	if err != nil {
		logger.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		logger.Close()
	})

	updated, err := Update(db, func(cfg *types.Configuration) error {
		cfg.LogLevel = " INFO "
		return nil
	})
	if err != nil {
		t.Fatalf("update valid log level: %v", err)
	}
	if updated.LogLevel != "info" {
		t.Fatalf("normalized log level = %q, want info", updated.LogLevel)
	}
	if _, err := Update(db, func(cfg *types.Configuration) error {
		cfg.LogLevel = "verbose"
		return nil
	}); err == nil {
		t.Fatal("invalid log level was accepted")
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	if _, err := UpdateTx(tx, func(cfg *types.Configuration) error {
		cfg.LogLevel = "debug"
		return nil
	}); err != nil {
		tx.Rollback()
		t.Fatalf("transactional update: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("roll back transactional update: %v", err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatalf("view config after rollback: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("rolled-back log level = %q, want info", cfg.LogLevel)
	}
}
