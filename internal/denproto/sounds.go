package denproto

import (
	"errors"
	"fmt"
)

// The den's soundboard (M14.3): sounds staff add, which a member in a call
// plays for everyone in it, on a track the den streams into the call.

// Events for the den's sounds, which every member hears.
const (
	EventSoundCreated = "sound.created"
	EventSoundUpdated = "sound.updated"
	EventSoundDeleted = "sound.deleted"
)

// CodeSoundNameTaken refuses a sound a name another of the den's sounds
// has, in any case, so the page can say so rather than that a detail was
// wrong.
const CodeSoundNameTaken = "sound_name_taken"

// Sound is one of the den's sounds: its name, unique in the den; its file,
// which opens for staff alone, since only the den plays it; its length, in
// milliseconds; and who added it and when.
type Sound struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FileID   string `json:"file_id"`
	Duration int64  `json:"duration_ms"`
	AddedBy  string `json:"added_by"`
	AddedAt  int64  `json:"added_at"`
}

// SoundDeleted names a sound staff deleted.
type SoundDeleted struct {
	ID string `json:"id"`
}

// SoundRequest adds a sound: its name, and an upload of the staff member's,
// waiting to be used, which becomes its file.
type SoundRequest struct {
	Name   string `json:"name"`
	FileID string `json:"file_id"`
}

// SoundUpdate renames a sound.
type SoundUpdate struct {
	Name string `json:"name"`
}

// SoundLimits are a den's limits for its soundboard, which its owner sets:
// how many sounds it keeps, 0 turning the soundboard off and keeping them;
// the longest a sound may be, and how long a member waits after their
// sound ends before they play another, both in seconds.
type SoundLimits struct {
	Sounds  int `json:"sounds"`
	Longest int `json:"longest"`
	Wait    int `json:"wait"`
}

// DefaultSoundLimits are a new den's: a short soundboard, as Discord's is.
var DefaultSoundLimits = SoundLimits{Sounds: 50, Longest: 10, Wait: 5}

// The ranges an owner sets the soundboard's limits in.
const (
	MaxSounds       = 500
	MaxSoundLongest = 5 * 60
	MaxSoundWait    = 10 * 60
)

// CheckSoundLimits checks limits an owner sets, or a den sends.
func CheckSoundLimits(l SoundLimits) error {
	switch {
	case l.Sounds < 0 || l.Sounds > MaxSounds:
		return fmt.Errorf("a den keeps 0 to %d sounds", MaxSounds)
	case l.Longest < 1 || l.Longest > MaxSoundLongest:
		return fmt.Errorf("the longest sound is 1 to %d seconds", MaxSoundLongest)
	case l.Wait < 0 || l.Wait > MaxSoundWait:
		return fmt.Errorf("a member waits 0 to %d seconds between sounds", MaxSoundWait)
	}
	return nil
}

// SoundBitrate is the bitrate a sound is in, in bits a second: the page of
// the staff member adding it encodes it as Opus at this rate, so a sound's
// size follows its length.
const SoundBitrate = 64_000

// soundHeaders is the room a sound's file has for its Ogg headers.
const soundHeaders = 4 << 10

// MaxSoundSize is the largest file a sound of a length in milliseconds may
// be: its length at SoundBitrate, with a quarter more for the encoder's
// swings and Ogg's pages, and room for its headers.
func MaxSoundSize(ms int64) int64 {
	return soundHeaders + ms*SoundBitrate/8/1000*5/4
}

// CleanSoundName makes a sound's name as a den keeps it: cleaned as a
// display name is.
func CleanSoundName(name string) (string, error) { return CleanName(name, MaxNameRunes) }

// CheckSound checks a sound as a den describes it, before a client keeps
// or shows it.
func CheckSound(s Sound) error {
	for _, id := range []string{s.ID, s.FileID, s.AddedBy} {
		if _, err := ParseID(id); err != nil {
			return errors.New("a sound names an invalid ID")
		}
	}
	if name, err := CleanSoundName(s.Name); err != nil || name != s.Name {
		return errors.New("a sound has an invalid name")
	}
	if s.Duration < 1 || s.Duration > MaxDuration {
		return errors.New("a sound has an invalid length")
	}
	if s.AddedAt <= 0 {
		return errors.New("a sound has no time")
	}
	return nil
}
