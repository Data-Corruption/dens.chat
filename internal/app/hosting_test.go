package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/types"
)

var devBuild = build.BuildInfo{Name: "dens", Version: "v1.1.0", DefaultLogLevel: "warn", DevMode: true}

// openDev opens a development instance in the test's own directories.
func openDev(t *testing.T, name string) *App {
	t.Helper()
	a := New(devBuild)
	if err := a.Open(OpenOptions{Instance: name, DevClientPort: 18484}); err != nil {
		t.Fatal(err)
	}
	return a
}

// asV1Left has an instance's data and config as v1.0.0 left them: the
// configuration as it stored it, which has no switch, and its den role.
func asV1Left(t *testing.T, a *App, denRole bool) {
	t.Helper()
	stored, err := os.ReadFile(filepath.Join("testdata", "v1.0.0", "configuration.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`UPDATE config SET value = ? WHERE key = ?`, string(stored), database.ConfigDataKey); err != nil {
		t.Fatal(err)
	}
	cfg := a.Instance
	cfg.Den.Enabled = denRole
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.WriteFileAtomic(a.Layout.InstanceConfig, append(data, '\n'), nil); err != nil {
		t.Fatal(err)
	}
}

func hostingOf(t *testing.T, a *App) *bool {
	t.Helper()
	cfg, err := config.View(a.DB)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Hosting
}

// The first start of a version with the hosting switch takes it from the
// den role v1.0.0 gave the instance, and from then on the switch alone
// decides (M9.1).
func TestHostingStartsAsV1LeftIt(t *testing.T) {
	for _, denRole := range []bool{true, false} {
		t.Run(fmt.Sprintf("den role %v", denRole), func(t *testing.T) {
			devHome(t)
			name := fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000)
			a := openDev(t, name)
			asV1Left(t, a, denRole)
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}

			a = openDev(t, name)
			if got := hostingOf(t, a); got == nil || *got != denRole {
				t.Fatalf("the switch after the first start is %v, want %v", got, denRole)
			}
			// The rest of what v1.0.0 kept is as it was.
			cfg, err := config.View(a.DB)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CacheLimit != 4<<30 || cfg.VoiceKeys["ptt"].Code != "Mouse4" || cfg.LastShutdownVersion != devBuild.Version {
				t.Fatalf("the configuration after the first start is %+v", cfg)
			}
			flipped := !denRole
			if _, err := config.Update(a.DB, func(c *types.Configuration) error {
				c.Hosting = &flipped
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}

			a = openDev(t, name)
			defer a.Close()
			if got := hostingOf(t, a); got == nil || *got != flipped {
				t.Fatalf("the switch set %v starts as %v, from the den role %v", flipped, got, denRole)
			}
		})
	}
}

// A restored backup starts with hosting off, as its browsers start
// unpaired: hosting is the machine's choice, not the data's.
func TestRestoredBackupStartsNotHosting(t *testing.T) {
	devHome(t)
	a := openDev(t, fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000))
	defer a.Close()
	on := true
	if _, err := config.Update(a.DB, func(c *types.Configuration) error {
		c.Hosting = &on
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.prepareHosting(maintenance.PhaseRestoring, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosting == nil || *cfg.Hosting {
		t.Fatalf("the switch after a restore is %v", cfg.Hosting)
	}
}

// The Flatpak hosts no den, whatever its data held, as a backup of an
// instance that hosted does (M9.3).
func TestFlatpakNeverHosts(t *testing.T) {
	devHome(t)
	a := openDev(t, fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000))
	defer a.Close()
	on := true
	if _, err := config.Update(a.DB, func(c *types.Configuration) error {
		c.Hosting = &on
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !a.CanHost() {
		t.Fatal("a development instance can't host")
	}
	a.Layout.Form = layout.Flatpak
	if a.CanHost() {
		t.Fatal("the Flatpak can host")
	}
	cfg, err := a.prepareHosting(maintenance.PhaseReady, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosting == nil || *cfg.Hosting {
		t.Fatalf("the Flatpak's switch is %v", cfg.Hosting)
	}
}

func devHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("LOCALAPPDATA", dir)
}
