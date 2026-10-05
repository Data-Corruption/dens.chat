package denproto

import (
	_ "embed"
	"encoding/json"
	"slices"
	"testing"
)

// The page's renderer finds links by the same rule, and reads the same
// cases.
//
//go:embed testdata/links.json
var linkCases []byte

type linkCase struct {
	Text  string   `json:"text"`
	Links []string `json:"links"`
	Clean *string  `json:"clean"`
}

func readLinkCases(t testing.TB) []linkCase {
	var file struct {
		Cases []linkCase `json:"cases"`
	}
	if err := json.Unmarshal(linkCases, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no link cases")
	}
	return file.Cases
}

func linkTexts(text string) []string {
	out := []string{}
	for _, l := range findLinks(text) {
		out = append(out, text[l.start:l.end])
	}
	return out
}

func TestLinks(t *testing.T) {
	for _, c := range readLinkCases(t) {
		if got := linkTexts(c.Text); !slices.Equal(got, c.Links) {
			t.Errorf("links in %q = %q, want %q", c.Text, got, c.Links)
		}
		want := c.Text
		if c.Clean != nil {
			want = *c.Clean
			if want == c.Text {
				t.Errorf("case %q gives clean the same as text; leave it out", c.Text)
			}
		}
		got := CleanLinks(c.Text)
		if got != want {
			t.Errorf("CleanLinks(%q) = %q, want %q", c.Text, got, want)
		}
		if again := CleanLinks(got); again != got {
			t.Errorf("CleanLinks(%q) = %q, not itself", got, again)
		}
	}
}

func TestTrimLink(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.com/a.":         "https://x.com/a",
		"https://x.com/a?!":        "https://x.com/a",
		"https://x.com/a)":         "https://x.com/a",
		"https://x.com/a_(b)":      "https://x.com/a_(b)",
		"https://x.com/a_(b)).":    "https://x.com/a_(b)",
		"https://x.com/a]":         "https://x.com/a",
		"https://x.com/((a)":       "https://x.com/((a)",
		"https://x.com/a)b)":       "https://x.com/a)b",
		"https://.,;:!?])":         "https://",
		"https://x.com/a-_~*":      "https://x.com/a-_~*",
		"https://x.com/(a))(b))":   "https://x.com/(a))(b",
		"https://x.com/wiki/A_(B)": "https://x.com/wiki/A_(B)",
	} {
		if got := trimLink(in); got != want {
			t.Errorf("trimLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStartTime(t *testing.T) {
	for s, want := range map[string]bool{
		"42": true, "42s": true, "1m": true, "1h": true, "1m30s": true, "1h2m3s": true, "1h3s": true, "0": true,
		"": false, "s": false, "1x": false, "1h2": false, "1s2m": false, "m": false, "1hm": false,
		"12345678901234567": false,
	} {
		if got := startTime(s); got != want {
			t.Errorf("startTime(%q) = %v, want %v", s, got, want)
		}
	}
}

// FuzzCleanLinks checks that rewriting a text twice changes nothing more,
// and that only its links change: the page finds as many links in it as
// before, with the same text between them.
func FuzzCleanLinks(f *testing.F) {
	for _, c := range readLinkCases(f) {
		f.Add(c.Text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		clean := CleanLinks(text)
		if again := CleanLinks(clean); again != clean {
			t.Fatalf("CleanLinks(%q) = %q, then %q", text, clean, again)
		}
		before, after := findLinks(text), findLinks(clean)
		if len(before) != len(after) {
			t.Fatalf("%q has %d links, rewritten as %q %d", text, len(before), clean, len(after))
		}
		at, at2 := 0, 0
		for i := range before {
			if text[at:before[i].start] != clean[at2:after[i].start] {
				t.Fatalf("%q rewritten as %q changed the text between links", text, clean)
			}
			at, at2 = before[i].end, after[i].end
		}
		if text[at:] != clean[at2:] {
			t.Fatalf("%q rewritten as %q changed the text after its links", text, clean)
		}
	})
}
