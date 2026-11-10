package denproto

import (
	"errors"
	"slices"
)

// NameFonts are the fonts a name's style may name, each a face the page
// carries, Latin alone: a name in another script shows in the page's own.
var NameFonts = []string{"pacifico", "fredoka", "zilla-slab", "space-mono", "orbitron", "pixelify-sans", "bangers", "cinzel"}

// maxNameFontBytes bounds a font's name as a den sends it.
const maxNameFontBytes = 32

// NameStyle is how a member's name shows to everyone: a font of
// NameFonts, or none; a color, #rrggbb, or none; and with a color, a
// second that the name runs to from its start to its end. The zero style
// is the page's own.
type NameStyle struct {
	Font   string `json:"font,omitempty"`
	Color  string `json:"color,omitempty"`
	Color2 string `json:"color2,omitempty"`
}

// IsZero reports whether the style changes nothing, which a member's
// record then leaves out.
func (s NameStyle) IsZero() bool { return s == NameStyle{} }

// CheckNameStyle checks a style a member sets: a font of NameFonts or
// none, and its colors, each #rrggbb in lowercase, the second only with
// the first.
func CheckNameStyle(s NameStyle) error {
	if s.Font != "" && !slices.Contains(NameFonts, s.Font) {
		return errors.New("a name's font is one of the page's")
	}
	return checkNameColors(s)
}

// CheckSentNameStyle checks a style a den sends with a member. Its font
// may be one a later Dens added, which the page shows as none, so it's
// checked only as a name: lowercase letters, digits and hyphens.
func CheckSentNameStyle(s NameStyle) error {
	if len(s.Font) > maxNameFontBytes {
		return errors.New("a name's font is too long")
	}
	for i := 0; i < len(s.Font); i++ {
		if c := s.Font[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return errors.New("a name's font isn't a font's name")
		}
	}
	return checkNameColors(s)
}

func checkNameColors(s NameStyle) error {
	if s.Color == "" && s.Color2 != "" {
		return errors.New("a name's second color needs its first")
	}
	for _, c := range []string{s.Color, s.Color2} {
		if c != "" && !isHexColor(c) {
			return errors.New("a name's color is #rrggbb, in lowercase")
		}
	}
	return nil
}

// isHexColor reports whether c is #rrggbb in lowercase hex digits, the
// only form of a color the page puts in a style.
func isHexColor(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for i := 1; i < len(c); i++ {
		if d := c[i]; (d < '0' || d > '9') && (d < 'a' || d > 'f') {
			return false
		}
	}
	return true
}
