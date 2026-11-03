package denproto

import (
	"strings"
	"testing"
)

func TestEmojiLimits(t *testing.T) {
	for _, l := range []EmojiLimits{DefaultEmojiLimits, {Emojis: 0}, {Emojis: MaxEmojis}} {
		if err := CheckEmojiLimits(l); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}
	for _, l := range []EmojiLimits{{Emojis: -1}, {Emojis: MaxEmojis + 1}} {
		if CheckEmojiLimits(l) == nil {
			t.Errorf("%+v passed", l)
		}
	}
}

// A name is lowercased on entry, as a username is, and is 2 to 32 of a-z,
// 0-9 and _.
func TestNormalizeEmojiName(t *testing.T) {
	for in, want := range map[string]string{
		"party":                 "party",
		"  Party_Parrot ":       "party_parrot",
		"a1":                    "a1",
		strings.Repeat("x", 32): strings.Repeat("x", 32),
	} {
		if got, err := NormalizeEmojiName(in); err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "a", strings.Repeat("x", 33), "party-parrot", ":party:", "party parrot", "pärty", "😀😀"} {
		if got, err := NormalizeEmojiName(in); err == nil {
			t.Errorf("%q passed as %q", in, got)
		}
	}
}

func TestCheckEmoji(t *testing.T) {
	good := Emoji{ID: "7", Name: "party", Image: Image{ID: "12", Width: 128, Height: 64, Animated: true}, AddedBy: "3", AddedAt: 1}
	if err := CheckEmoji(good); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Emoji){
		"no ID":             func(e *Emoji) { e.ID = "" },
		"no image":          func(e *Emoji) { e.Image.ID = "x" },
		"nobody added it":   func(e *Emoji) { e.AddedBy = "" },
		"no name":           func(e *Emoji) { e.Name = "" },
		"a name in capital": func(e *Emoji) { e.Name = "Party" },
		"a name with a dot": func(e *Emoji) { e.Name = "par.ty" },
		"no width":          func(e *Emoji) { e.Image.Width = 0 },
		"too wide":          func(e *Emoji) { e.Image.Width = MaxEmojiSide + 1 },
		"too high":          func(e *Emoji) { e.Image.Height = MaxEmojiSide + 1 },
		"no time":           func(e *Emoji) { e.AddedAt = 0 },
	} {
		e := good
		change(&e)
		if CheckEmoji(e) == nil {
			t.Errorf("%s: passed", name)
		}
	}
}
