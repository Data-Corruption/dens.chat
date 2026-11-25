package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/notify"
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
// from 0 to 100 percent.
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

// v1_12_0Row is the configuration as v1.12.0 kept it: v1.11.0's, with
// Relay.
var v1_12_0Row = v1_11_0Row[:len(v1_11_0Row)-1] + `,"relayID":"00112233445566778899aabbccddeeff","relayKeys":true,"relaySound":true}`

// The configuration v1.12.0 kept reads as it was, with no hover set for
// the chat's background, and takes one.
func TestReadsV1_12_0Configuration(t *testing.T) {
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
	if _, err := db.Exec(`UPDATE config SET value = ? WHERE key = ?`, v1_12_0Row, database.ConfigDataKey); err != nil {
		t.Fatal(err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackgroundHover != nil || cfg.BackgroundDim == nil || *cfg.BackgroundDim != 40 || !cfg.RelayKeys || !cfg.RelaySound {
		t.Errorf("v1.12.0's configuration read as %+v", cfg)
	}
	for _, hover := range []int{-1, 101} {
		if _, err := Update(db, func(c *types.Configuration) error {
			c.BackgroundHover = &hover
			return nil
		}); err == nil {
			t.Errorf("a hover of %d was kept", hover)
		}
	}
	hover := 55
	if _, err := Update(db, func(c *types.Configuration) error {
		c.BackgroundHover = &hover
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if cfg, err = View(db); err != nil || cfg.BackgroundHover == nil || *cfg.BackgroundHover != 55 || *cfg.BackgroundDim != 40 {
		t.Errorf("the hover kept as %+v, %v", cfg.BackgroundHover, err)
	}
}

// v1_14_0Row is the configuration as v1.14.0 kept it: v1.12.0's, with the
// hover, and a channel and a DM muted in a second den.
var v1_14_0Row = strings.Replace(v1_12_0Row[:len(v1_12_0Row)-1], `"muted":{"abc":{"den":true}}`,
	`"muted":{"abc":{"den":true},"def":{"channels":["11","12"]}}`, 1) + `,"backgroundHover":55}`

// shown keeps the messages a hub showed.
type shown []string

func (s *shown) Show(n notify.Notification) bool {
	*s = append(*s, n.Message)
	return true
}
func (*shown) Withdraw([]string)            {}
func (*shown) Settings(types.Notifications) {}

// The configuration v1.14.0 kept reads as it was: its muted channels
// stay at Nothing, notifying nothing, with no channel at All messages
// and Do not disturb off; and a channel set to All messages leaves them
// as they were.
func TestReadsV1_14_0Configuration(t *testing.T) {
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
	if !strings.Contains(v1_14_0Row, `"def":{"channels":["11","12"]}`) {
		t.Fatal("the v1.14.0 row lost its muted channels")
	}
	if _, err := db.Exec(`UPDATE config SET value = ? WHERE key = ?`, v1_14_0Row, database.ConfigDataKey); err != nil {
		t.Fatal(err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatal(err)
	}
	n := cfg.Notifications
	if m := n.Muted["def"]; m.Den || !slices.Equal(m.Channels, []string{"11", "12"}) || m.All != nil || !n.Muted["abc"].Den {
		t.Errorf("v1.14.0's mutes read as %+v", n.Muted)
	}
	if n.DoNotDisturb || !n.DoNotDisturbUntil.IsZero() || cfg.BackgroundHover == nil || *cfg.BackgroundHover != 55 {
		t.Errorf("v1.14.0's configuration read as %+v", cfg)
	}

	helper := &shown{}
	h := notify.New(logger, n, func(n types.Notifications) error {
		_, err := Update(db, func(c *types.Configuration) error {
			c.Notifications = n
			return nil
		})
		return err
	}, helper, false)
	for _, m := range []notify.Message{
		{Den: "def", Channel: "11", ID: "100", Author: "Alice", Mention: true, Text: "@bob"},
		{Den: "def", Channel: "13", ID: "101", Author: "Alice", Mention: true, Text: "@bob"},
		{Den: "def", Channel: "12", ID: "102", Author: "Alice", DM: true, Text: "hi"},
	} {
		h.Message(m)
	}
	if !slices.Equal(*helper, []string{"101"}) {
		t.Errorf("with v1.14.0's mutes, the helper showed %v, want 101 alone", *helper)
	}
	if err := h.SetLevel("def", "13", notify.LevelAll); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM config WHERE key = ?`, database.ConfigDataKey).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"def":{"channels":["11","12"],"all":["13"]}`) {
		t.Errorf("a channel set to All messages kept %s", raw)
	}
}

// The configuration v1.11.0 kept reads as it was, with Relay off and no
// Relay ID, and takes them.
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

// v1_15_0Row is the configuration as v1.15.0 kept it, marshaled from its
// Configuration: v1.14.0's, with a channel at All messages and Do not
// disturb on until a time.
const v1_15_0Row = `{"logLevel":"info","hosting":true,"denExported":"2026-11-01T10:00:00Z","updateNotifications":true,` +
	`"backgroundUpdateChecks":true,"lastUpdateCheck":"2026-11-09T08:00:00Z","updateCheckSource":"background",` +
	`"latestUpdateVersion":"v1.11.0","lastShutdownVersion":"v1.11.0","cacheLimit":1073741824,` +
	`"voiceKeys":{"ptt":{"code":"KeyV","label":"V","ctrl":false,"alt":false,"shift":false,"meta":false}},"desktopKeys":true,` +
	`"startAtSignIn":false,"notifications":{"dms":true,"mentions":true,"eventsOff":true,` +
	`"muted":{"abc":{"den":true},"def":{"channels":["11","12"],"all":["13"]}},"dnd":true,"dndUntil":"2026-11-22T18:00:00Z",` +
	`"sounds":{"message":{"off":true}}},"backgroundDim":40,"backgroundHover":55,` +
	`"relayID":"00112233445566778899aabbccddeeff","relayKeys":true,"relaySound":true}`

// The configuration v1.15.0 kept reads as it was, with no name for the
// device, and takes one cleaned as a display name is, refusing one that
// isn't.
func TestReadsV1_15_0Configuration(t *testing.T) {
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
	if _, err := db.Exec(`UPDATE config SET value = ? WHERE key = ?`, v1_15_0Row, database.ConfigDataKey); err != nil {
		t.Fatal(err)
	}
	cfg, err := View(db)
	if err != nil {
		t.Fatal(err)
	}
	n := cfg.Notifications
	if cfg.DeviceName != "" || !n.DoNotDisturb || !slices.Equal(n.Muted["def"].All, []string{"13"}) || !cfg.RelayKeys {
		t.Errorf("v1.15.0's configuration read as %+v", cfg)
	}
	for _, bad := range []string{" Pearl", "Pe‮arl", "​", strings.Repeat("a", 33)} {
		if _, err := Update(db, func(c *types.Configuration) error {
			c.DeviceName = bad
			return nil
		}); err == nil {
			t.Errorf("the device's name %q was taken", bad)
		}
	}
	if cfg, err = Update(db, func(c *types.Configuration) error {
		c.DeviceName = "Bob's desk"
		return nil
	}); err != nil || cfg.DeviceName != "Bob's desk" {
		t.Fatalf("naming the device: %+v %v", cfg, err)
	}
	if cfg, err = View(db); err != nil || cfg.DeviceName != "Bob's desk" || !cfg.Notifications.DoNotDisturb {
		t.Errorf("the name kept as %q, %v", cfg.DeviceName, err)
	}
}
