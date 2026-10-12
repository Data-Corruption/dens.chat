package denproto

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
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

// TestHostileTextIsLinear checks that finding a text's links takes time in
// proportion to its length, whatever its delimiters (M8), as the page's
// parse does: the den does it for every message, and again for each link
// it rewrites. These lines are 16,000 bytes, four times a message's limit
// in runes. Each took the old parser from 50 ms to seconds; each takes
// well under a millisecond.
func TestHostileTextIsLinear(t *testing.T) {
	for name, line := range map[string]string{
		"unclosed *":         strings.Repeat("* ", 8000),
		"unclosed **":        strings.Repeat("** ", 5334)[:16000],
		"unclosed _":         strings.Repeat("_ ", 8000),
		"unclosed ||":        strings.Repeat("|| ", 5334)[:16000],
		"every opener":       strings.Repeat("*_~~|| ", 2286)[:16000],
		"code and openers":   strings.Repeat("*`", 8000),
		"links after a name": strings.Repeat("ahttp://", 2000),
		"long mentions":      strings.Repeat("@"+strings.Repeat("a", 40)+" ", 381)[:16000],
		"parentheses":        "http://a" + strings.Repeat(")", 15992),
	} {
		for _, shape := range []string{line, "> " + line, "[ ] " + line} {
			took := time.Duration(1 << 62)
			for range 3 {
				start := time.Now()
				findLinks(shape)
				took = min(took, time.Since(start))
			}
			if took > 100*time.Millisecond {
				t.Errorf("%s: %v", name, took)
			}
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
