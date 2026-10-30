package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/types"

	"golang.org/x/mod/semver"
)

// UpdateCheckInterval is how often the service asks the release host for
// the latest version.
const UpdateCheckInterval = 24 * time.Hour

// ChecksForUpdates reports whether this instance asks the release host for
// updates, and tells the member of them: an installed one does, given a
// release host, and so does the Android app, whose release names its host,
// and which downloads each update for its shell to install (M12). A
// development build has nothing to update to, and the Flatpak updates
// through Flatpak.
func (a *App) ChecksForUpdates() bool {
	switch a.Layout.Form {
	case layout.Installed, layout.Android:
		return a.Instance.ReleaseURL != ""
	}
	return false
}

// LatestUpdate returns the newer version the last check found, if any. A
// result from a different release source never counts.
func (a *App) LatestUpdate(cfg *types.Configuration) (string, bool) {
	if !a.ChecksForUpdates() || cfg.UpdateCheckSource != a.Instance.ReleaseURL {
		return "", false
	}
	latest := cfg.LatestUpdateVersion
	if semver.IsValid(latest) && semver.Compare(latest, a.buildInfo.Version) > 0 {
		return latest, true
	}
	return "", false
}

// UpdateCommand is what the notice tells the user to run.
func (a *App) UpdateCommand() string {
	command := a.buildInfo.Name + " update"
	if a.Layout.Instance != layout.DefaultInstance {
		command += " --instance " + a.Layout.Instance
	}
	return host.AdminCommand(command)
}

// CheckForUpdate asks the release host for the latest version and records
// the result. The request reveals only this install's IP address to the
// release host.
func (a *App) CheckForUpdate(ctx context.Context) error {
	a.updateCheckMu.Lock()
	defer a.updateCheckMu.Unlock()
	source := a.Instance.ReleaseURL
	if !a.ChecksForUpdates() {
		return errors.New("update checks are disabled for this instance")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	latest, err := a.ReleaseSource.GetLatestVersion(checkCtx, source)
	if err != nil {
		return err
	}
	_, err = config.Update(a.DB, func(cfg *types.Configuration) error {
		cfg.UpdateCheckSource = source
		cfg.LatestUpdateVersion = latest
		cfg.LastUpdateCheck = time.Now()
		return nil
	})
	if err != nil {
		return fmt.Errorf("record update check: %w", err)
	}
	a.Log.Debugf("Update check: latest %s, running %s", latest, a.buildInfo.Version)
	return nil
}

// RunUpdateChecker checks once a day while background checks are enabled.
// Preferences are reread every hour, so turning checks off takes effect
// without a restart. It never installs anything: on Android it downloads
// what it finds, which the shell hands to Android's installer once the
// member asks it to (M12).
func (a *App) RunUpdateChecker(ctx context.Context, ready func()) error {
	ready()
	if !a.ChecksForUpdates() {
		<-ctx.Done()
		return nil
	}
	android := a.Layout.Form == layout.Android
	if android {
		a.checkStagedUpdate()
	}
	for {
		delay := time.Hour
		cfg, err := config.View(a.DB)
		if err != nil {
			a.Log.Errorf("read update preferences: %v", err)
		} else if cfg.BackgroundUpdateChecks {
			due := cfg.UpdateCheckSource != a.Instance.ReleaseURL ||
				time.Since(cfg.LastUpdateCheck) >= UpdateCheckInterval
			if due {
				if err := a.CheckForUpdate(ctx); err != nil && ctx.Err() == nil {
					a.Log.Warnf("update check failed: %v", quietNetError(err))
				}
			} else if until := time.Until(cfg.LastUpdateCheck.Add(UpdateCheckInterval)); until < delay {
				delay = max(until, time.Minute)
			}
			// A download cut off tries again within the hour.
			if android {
				a.readyAndroidUpdate(ctx)
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
