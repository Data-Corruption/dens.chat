package denproto

import (
	_ "embed"
	"encoding/json"
	"slices"
	"testing"
)

// The page reads a style by the same rules, and the same cases.
//
//go:embed testdata/namestyles.json
var nameStyleCases []byte

func TestNameStyles(t *testing.T) {
	var file struct {
		Fonts []string `json:"fonts"`
		Cases []struct {
			Style NameStyle `json:"style"`
			Set   bool      `json:"set"`
			Sent  bool      `json:"sent"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(nameStyleCases, &file); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(file.Fonts, NameFonts) {
		t.Errorf("the cases' fonts are %q, and NameFonts %q", file.Fonts, NameFonts)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no name style cases")
	}
	for _, c := range file.Cases {
		if err := CheckNameStyle(c.Style); (err == nil) != c.Set {
			t.Errorf("a member setting %+v: %v, want it taken %t", c.Style, err, c.Set)
		}
		if err := CheckSentNameStyle(c.Style); (err == nil) != c.Sent {
			t.Errorf("a den sending %+v: %v, want it taken %t", c.Style, err, c.Sent)
		}
		if c.Set && !c.Sent {
			t.Errorf("%+v is a member's to set, and not a den's to send", c.Style)
		}
	}
	for _, font := range NameFonts {
		if err := CheckNameStyle(NameStyle{Font: font}); err != nil {
			t.Errorf("the font %q: %v", font, err)
		}
		if err := CheckSentNameStyle(NameStyle{Font: font}); err != nil {
			t.Errorf("the font %q, sent: %v", font, err)
		}
	}
}

// A member with no style carries none in their record, so an older Dens
// reads them as it always has.
func TestMemberLeavesOutNoStyle(t *testing.T) {
	plain, err := json.Marshal(Member{ID: "1", Username: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(plain, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["name_style"]; ok {
		t.Errorf("a member with no style carries one: %s", plain)
	}
	styled, err := json.Marshal(Member{ID: "1", Username: "bob", NameStyle: NameStyle{Font: "pacifico", Color: "#aa0000", Color2: "#00aa00"}})
	if err != nil {
		t.Fatal(err)
	}
	var back Member
	if err := json.Unmarshal(styled, &back); err != nil || back.NameStyle != (NameStyle{Font: "pacifico", Color: "#aa0000", Color2: "#00aa00"}) {
		t.Errorf("a styled member read back as %+v, %v, from %s", back.NameStyle, err, styled)
	}
}
