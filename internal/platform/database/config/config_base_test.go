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
		"ptt":          {Code: "KeyV", Label: "V"},
		"toggleMute":   {Code: "Mouse4", Label: "Mouse 4", Ctrl: true},
		"pushMute":     {Code: "Backquote", Label: "`"},
		"toggleDeafen": {Code: "KeyB", Label: "B", Shift: true},
	}); err != nil {
		t.Fatalf("valid keys refused: %v", err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatal(err)
	}
	if k := cfg.VoiceKeys["toggleMute"]; k.Code != "Mouse4" || !k.Ctrl || !cfg.VoiceKeys["toggleDeafen"].Shift {
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

// The page's sounds are kept by name, each off or at a volume of its own,
// from 0 to 100 percent (M14.2).
func TestUpdateValidatesSounds(t *testing.T) {
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
	set := func(sounds map[string]types.Sound) error {
		_, err := Update(db, func(cfg *types.Configuration) error {
			cfg.Notifications.Sounds = sounds
			return nil
		})
		return err
	}
	volume := func(v int) *int { return &v }
	if err := set(map[string]types.Sound{"message": {Off: true}, "micOff": {Volume: volume(0)}, "joined": {Volume: volume(100)}}); err != nil {
		t.Fatalf("valid sounds refused: %v", err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.Notifications.Sounds; !s["message"].Off || *s["micOff"].Volume != 0 || *s["joined"].Volume != 100 {
		t.Fatalf("sounds kept = %+v", s)
	}
	for name, sounds := range map[string]map[string]types.Sound{
		"an unknown sound":  {"bell": {Off: true}},
		"a volume below 0":  {"left": {Volume: volume(-1)}},
		"a volume over 100": {"left": {Volume: volume(101)}},
	} {
		if err := set(sounds); err == nil {
			t.Errorf("sounds with %s were accepted", name)
		}
	}
}

// v1_11_0Row is the configuration as v1.11.0 kept it, with every field it
// wrote that a member's choices set.
const v1_11_0Row = `{"logLevel":"info","hosting":true,"denExported":"2026-11-01T10:00:00Z","updateNotifications":true,` +
	`"backgroundUpdateChecks":true,"lastUpdateCheck":"2026-11-09T08:00:00Z","updateCheckSource":"background",` +
	`"latestUpdateVersion":"v1.11.0","lastShutdownVersion":"v1.11.0","cacheLimit":1073741824,` +
	`"voiceKeys":{"ptt":{"code":"KeyV","label":"V","ctrl":false,"alt":false,"shift":false,"meta":false}},"desktopKeys":true,` +
	`"startAtSignIn":false,"notifications":{"dms":true,"mentions":true,"eventsOff":true,` +
	`"muted":{"abc":{"den":true}},"sounds":{"message":{"off":true}}},"backgroundDim":40}`

// The configuration v1.11.0 kept reads as it was, with Relay off and no
// Relay ID (M20), and takes them.
func TestReadsTheReleasedConfiguration(t *testing.T) {
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
	if _, err := db.Exec(`UPDATE config SET value = ? WHERE key = ?`, v1_11_0Row, database.ConfigDataKey); err != nil {
		t.Fatal(err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosting == nil || !*cfg.Hosting || cfg.VoiceKeys["ptt"].Code != "KeyV" || !cfg.DesktopKeys || cfg.CacheLimit != 1<<30 ||
		!cfg.Notifications.DMs || !cfg.Notifications.Muted["abc"].Den || cfg.BackgroundDim == nil || *cfg.BackgroundDim != 40 {
		t.Errorf("v1.11.0's configuration read as %+v", cfg)
	}
	if cfg.RelayID != "" || cfg.RelayKeys || cfg.RelaySound {
		t.Errorf("v1.11.0's configuration read with Relay: %q, %v, %v", cfg.RelayID, cfg.RelayKeys, cfg.RelaySound)
	}
	id := "00112233445566778899aabbccddeeff"
	if _, err := Update(db, func(c *types.Configuration) error {
		c.RelayID, c.RelayKeys, c.RelaySound = id, true, true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if cfg, err = View(db); err != nil || cfg.RelayID != id || !cfg.RelayKeys || !cfg.RelaySound || cfg.VoiceKeys["ptt"].Code != "KeyV" {
		t.Errorf("Relay kept as %q, %v, %v, %v", cfg.RelayID, cfg.RelayKeys, cfg.RelaySound, err)
	}
	if _, err := Update(db, func(c *types.Configuration) error {
		c.RelayID = "not hex"
		return nil
	}); err == nil {
		t.Error("a malformed Relay ID was kept")
	}
}
