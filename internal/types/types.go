// Package types holds configuration shapes shared across packages.
package types

import (
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
)

// Configuration is the service's own preferences, stored in the database and
// changed from the settings page. What an administrator decides at install
// (ports, desktop user) lives in the instance config instead.
type Configuration struct {
	LogLevel string `json:"logLevel"`

	// Hosting is whether this instance serves its den (M9.1), which the
	// desktop user turns on and off in the settings. It's nil until the
	// first start of a version that has it, which takes it from the den
	// role v1.0's installer gave the instance.
	Hosting *bool `json:"hosting,omitempty"`
	// DenExported is when the den was last exported to move it to another
	// computer (M10), and zero when it hasn't been since hosting was last
	// on: what reached the den since, the export doesn't hold.
	DenExported time.Time `json:"denExported,omitzero"`

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
	// StartAtSignIn is whether the Flatpak starts at the member's sign-in
	// once its key is remembered (M9.3): nil until they choose, which is on.
	StartAtSignIn *bool `json:"startAtSignIn,omitempty"`
	// Notifications are what the service notifies the member of (M12.3).
	// The service keeps them, so every browser paired with the instance
	// shares them, as it shares the voice keys.
	Notifications Notifications `json:"notifications,omitzero"`
}

// Notifications are the member's choices for what notifies them (M12.3):
// each kind stays off until they turn it on, a notification carries the
// start of its message unless they hide it, and a den or channel they
// muted notifies nothing.
type Notifications struct {
	DMs      bool `json:"dms,omitempty"`
	Mentions bool `json:"mentions,omitempty"`
	HideText bool `json:"hideText,omitempty"`
	// Muted holds what the member muted, by den ID.
	Muted map[string]Muted `json:"muted,omitempty"`
}

// Muted is what a member muted in one den: the whole den, and channels of
// it, DMs among them, by ID.
type Muted struct {
	Den      bool     `json:"den,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// SetHosting turns the hosting switch. Turning it on forgets an export of
// the den (M10), which can't hold what reaches the den from then on.
func (c *Configuration) SetHosting(on bool) {
	c.Hosting = &on
	if on {
		c.DenExported = time.Time{}
	}
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
