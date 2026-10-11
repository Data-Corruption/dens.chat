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

	// CacheLimit is the size of the client's cache in bytes, which the
	// member chooses (M6); 0 until they do, for the default.
	CacheLimit int64 `json:"cacheLimit,omitempty"`

	// VoiceKeys are the member's keys for the voice actions, by action. The
	// service keeps them, not the browser, so the shortcut helper can watch
	// them in other programs (M7).
	VoiceKeys map[string]VoiceKey `json:"voiceKeys,omitempty"`
	// DesktopKeys is whether the member had the desktop take the voice
	// keys, where it owns them (M7.2), so the helper binds them with it as
	// it starts.
	DesktopKeys bool `json:"desktopKeys,omitempty"`
}

// VoiceActions are what keys are bound to: push to talk, toggle mute and
// push to mute.
var VoiceActions = []string{"ptt", "toggleMute", "pushMute"}

// VoiceKey is a key bound to a voice action. Code names it by its place on
// the keyboard, as the page's KeyboardEvent.code does, or names a mouse's
// side button, Mouse4 or Mouse5; Label is what the page shows for it. A
// key fires only with exactly its modifiers held.
type VoiceKey struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Ctrl  bool   `json:"ctrl"`
	Alt   bool   `json:"alt"`
	Shift bool   `json:"shift"`
	Meta  bool   `json:"meta"`
}

// DefaultConfig returns the preferences of a new installation.
func DefaultConfig(buildInfo build.BuildInfo) Configuration {
	return Configuration{
		LogLevel:               buildInfo.DefaultLogLevel,
		UpdateNotifications:    true,
		BackgroundUpdateChecks: true,
	}
}
