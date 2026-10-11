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

func TestUpdateValidatesVoiceKeys(t *testing.T) {
	tmp := t.TempDir()
	logger, err := xlog.New(filepath.Join(tmp, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.New(filepath.Join(tmp, "db"), logger, build.BuildInfo{DefaultLogLevel: "warn"}, database.ApplyPendingMigrations)
	if err != nil {
		logger.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		logger.Close()
	})
	set := func(keys map[string]types.VoiceKey) error {
		_, err := Update(db, func(cfg *types.Configuration) error {
			cfg.VoiceKeys = keys
			return nil
		})
		return err
	}
	if err := set(map[string]types.VoiceKey{
		"ptt":        {Code: "KeyV", Label: "V"},
		"toggleMute": {Code: "Mouse4", Label: "Mouse 4", Ctrl: true},
		"pushMute":   {Code: "Backquote", Label: "`"},
	}); err != nil {
		t.Fatalf("valid keys refused: %v", err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatal(err)
	}
	if k := cfg.VoiceKeys["toggleMute"]; k.Code != "Mouse4" || !k.Ctrl {
		t.Fatalf("keys kept = %+v", cfg.VoiceKeys)
	}
	for name, keys := range map[string]map[string]types.VoiceKey{
		"an unknown action":    {"shout": {Code: "KeyV", Label: "V"}},
		"no code":              {"ptt": {Label: "V"}},
		"a code with a space":  {"ptt": {Code: "Key V", Label: "V"}},
		"a code too long":      {"ptt": {Code: "KeyVKeyVKeyVKeyVKeyVKeyVKeyVKeyVK", Label: "V"}},
		"no label":             {"ptt": {Code: "KeyV"}},
		"an unprintable label": {"ptt": {Code: "KeyV", Label: "V\u0000"}},
	} {
		if err := set(keys); err == nil {
			t.Errorf("keys with %s were accepted", name)
		}
	}
}
