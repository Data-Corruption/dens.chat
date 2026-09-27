// Package types holds configuration shapes shared across packages.
package types

import (
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
)

// Configuration is the service's own preferences, stored in the database and
// changed from the settings page. What an administrator decides at install
// (ports, den role, desktop user) lives in the instance config instead.
type Configuration struct {
	LogLevel string `json:"logLevel"`

	UpdateNotifications    bool      `json:"updateNotifications"`
	BackgroundUpdateChecks bool      `json:"backgroundUpdateChecks"`
	LastUpdateCheck        time.Time `json:"lastUpdateCheck"`
	UpdateCheckSource      string    `json:"updateCheckSource"`
	LatestUpdateVersion    string    `json:"latestUpdateVersion"`

	// LastShutdownVersion is the version that last stopped cleanly, which
	// tells a restarted service whether it was updated.
	LastShutdownVersion string `json:"lastShutdownVersion"`
}

// DefaultConfig returns the preferences of a new installation.
func DefaultConfig(buildInfo build.BuildInfo) Configuration {
	return Configuration{
		LogLevel:               buildInfo.DefaultLogLevel,
		UpdateNotifications:    true,
		BackgroundUpdateChecks: true,
	}
}
