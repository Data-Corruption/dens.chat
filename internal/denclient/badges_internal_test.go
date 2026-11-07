package denclient

import (
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A profile's badges, as a den's answer gives them, are as many as one
// carries, each once and fit to show (M17.5).
func TestCleanBadges(t *testing.T) {
	badge := func(event, title string) denproto.MemberBadge {
		return denproto.MemberBadge{EventID: event, Title: title, Image: denproto.Image{ID: "9", Width: 128, Height: 128},
			Event: "Game night", StartsAt: 1, EarnedAt: 2}
	}
	if !cleanBadges(nil) || !cleanBadges([]denproto.MemberBadge{badge("1", "Was there"), badge("2", "Was there")}) {
		t.Error("cleanBadges refused badges fit to show")
	}
	big := badge("1", "Was there")
	big.Image.Width = 129
	many := make([]denproto.MemberBadge, denproto.MaxProfileBadges+1)
	for i := range many {
		many[i] = badge(denproto.FormatID(int64(i+1)), "x")
	}
	for name, list := range map[string][]denproto.MemberBadge{
		"twice":       {badge("1", "a"), badge("1", "b")},
		"long title":  {badge("1", strings.Repeat("x", 41))},
		"spaced":      {badge("1", " x")},
		"large":       {big},
		"no event ID": {badge("", "x")},
		"too many":    many,
	} {
		if cleanBadges(list) {
			t.Errorf("cleanBadges took %s", name)
		}
	}
}
