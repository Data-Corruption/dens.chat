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
	// BackgroundDim is how far the page's background covers the picture
	// behind the chat, in percent, nil until the member sets it. The
	// service keeps it with the picture, so every browser paired with the
	// instance shows the same.
	BackgroundDim *int `json:"backgroundDim,omitempty"`
	// RelayID names this install to the member's others it links with
	// (M20): 16 random bytes in hex, made as a start finds none. A restore
	// makes another, since it names the computer, not the data.
	RelayID string `json:"relayID,omitempty"`
	// RelayKeys is Keybind relay: this install's voice keys reaching a
	// call another of the member's devices holds, and theirs its own,
	// off until the member turns it on.
	RelayKeys bool `json:"relayKeys,omitempty"`
}

// Notifications are the member's choices for what notifies them (M12.3):
// each kind of message stays off until they turn it on, a notification
// carries the start of its message unless they hide it, and a den or
// channel they muted notifies nothing.
type Notifications struct {
	DMs      bool `json:"dms,omitempty"`
	Mentions bool `json:"mentions,omitempty"`
	HideText bool `json:"hideText,omitempty"`
	// EveryoneOff silences staff's @everyone (M15.4), which notifies as a
	// mention does unless the member turns it off.
	EveryoneOff bool `json:"everyoneOff,omitempty"`
	// EventsOff silences the events the member is interested in as they
	// start (M17.4), which notify unless the member turns them off.
	EventsOff bool `json:"eventsOff,omitempty"`
	// Muted holds what the member muted, by den ID.
	Muted map[string]Muted `json:"muted,omitempty"`
	// Sounds are the member's choices for the page's sounds (M14.2), by
	// name: each plays at its default volume, which the page holds, until
	// they turn it off or set its volume. They're kept beside what
	// notifies the member, as one tab of the settings holds both.
	Sounds map[string]Sound `json:"sounds,omitempty"`
}

// The background's dim is at most BackgroundMaxDim percent, so the
// picture always shows, and BackgroundDefaultDim until the member sets it.
const (
	BackgroundMaxDim     = 90
	BackgroundDefaultDim = 70
)

// SoundNames are the page's sounds (M14.2): a message, a member joining and
// leaving the call, a share starting and ending, and the member's
// microphone opening and closing.
var SoundNames = []string{"message", "joined", "left", "shareStarted", "shareEnded", "micOn", "micOff"}

// Sound is the member's choice for one of the page's sounds: off, and its
// volume in percent, nil for its default.
type Sound struct {
	Off    bool `json:"off,omitempty"`
	Volume *int `json:"volume,omitempty"`
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

// VoiceActions are what keys are bound to: push to talk, toggle mute,
// push to mute and toggle deafen.
var VoiceActions = []string{"ptt", "toggleMute", "pushMute", "toggleDeafen"}

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
