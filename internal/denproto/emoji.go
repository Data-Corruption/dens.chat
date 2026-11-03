package denproto

import (
	"errors"
	"fmt"
	"strings"
)

// The den's emoji (M15.2): images staff add, which members write by name in
// the markdown subset, as :name:, and pick beside Unicode's. Each is one of
// the den's named files, as a sound is (M14.3), its file open to every
// member, since every page draws it. The den never parses a message for
// them: the page shows a name the den has as its emoji, and any other as
// the text it is.

// Events for the den's emoji, which every member hears.
const (
	EventEmojiCreated = "emoji.created"
	EventEmojiUpdated = "emoji.updated"
	EventEmojiDeleted = "emoji.deleted"
)

// CodeEmojiNameTaken refuses an emoji a name another of the den's emoji
// has, so the page can say so rather than that a detail was wrong.
const CodeEmojiNameTaken = "emoji_name_taken"

// Emoji is one of the den's emoji: its name, unique among them; its image,
// with its size and whether it's animated; and who added it and when.
type Emoji struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   Image  `json:"image"`
	AddedBy string `json:"added_by"`
	AddedAt int64  `json:"added_at"`
}

// EmojiDeleted names an emoji staff deleted.
type EmojiDeleted struct {
	ID string `json:"id"`
}

// EmojiRequest adds an emoji: its name, and an upload of the staff
// member's, waiting to be used, which becomes its image.
type EmojiRequest struct {
	Name   string `json:"name"`
	FileID string `json:"file_id"`
}

// EmojiUpdate renames an emoji.
type EmojiUpdate struct {
	Name string `json:"name"`
}

// EmojiLimits are a den's limits for its emoji, which its owner sets: how
// many it keeps, 0 turning them off and keeping them.
type EmojiLimits struct {
	Emojis int `json:"emojis"`
}

// DefaultEmojiLimits are a new den's.
var DefaultEmojiLimits = EmojiLimits{Emojis: 50}

// MaxEmojis is the most emoji an owner lets a den keep.
const MaxEmojis = 500

// CheckEmojiLimits checks limits an owner sets, or a den sends.
func CheckEmojiLimits(l EmojiLimits) error {
	if l.Emojis < 0 || l.Emojis > MaxEmojis {
		return fmt.Errorf("a den keeps 0 to %d emoji", MaxEmojis)
	}
	return nil
}

// An emoji shows at 48 pixels at most, so its image is at most 128 on its
// longer side, for dense screens: a still image, which the page draws as a
// PNG of that size, or an animated GIF, which the member's local service
// makes as it makes an animated picture (M15.1), of a size of its own.
const (
	MaxEmojiSide         = 128
	MaxEmojiSize         = 256 << 10
	MaxAnimatedEmojiSize = 512 << 10
)

// NormalizeEmojiName lowercases and trims an emoji's name and checks it, as
// a username is: 2 to 32 characters of a-z, 0-9 and _, so :name: reads the
// same in every message.
func NormalizeEmojiName(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 2 || len(s) > 32 {
		return "", errors.New("an emoji's name is 2 to 32 characters")
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return "", errors.New("an emoji's name uses only letters, digits and _")
		}
	}
	return s, nil
}

// CheckEmoji checks an emoji as a den describes it, before a client keeps
// or shows it.
func CheckEmoji(e Emoji) error {
	for _, id := range []string{e.ID, e.Image.ID, e.AddedBy} {
		if _, err := ParseID(id); err != nil {
			return errors.New("an emoji names an invalid ID")
		}
	}
	if name, err := NormalizeEmojiName(e.Name); err != nil || name != e.Name {
		return errors.New("an emoji has an invalid name")
	}
	if e.Image.Width < 1 || e.Image.Width > MaxEmojiSide || e.Image.Height < 1 || e.Image.Height > MaxEmojiSide {
		return errors.New("an emoji has an invalid size")
	}
	if e.AddedAt <= 0 {
		return errors.New("an emoji has no time")
	}
	return nil
}
