package denproto

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// The list holds every fully qualified emoji up to EmojiVersion, skin
// tones and all, and nothing else: no components, no forms missing their
// variation selector, no text, and nothing newer.
func TestIsEmoji(t *testing.T) {
	for _, e := range []string{"😀", "👍", "👍🏽", "❤️", "🧑🏻‍🤝‍🧑🏼", "🫱🏻‍🫲🏼", "👨‍👩‍👧‍👦", "🇺🇸", "🏴󠁧󠁢󠁳󠁣󠁴󠁿", "#️⃣", "🫩"} {
		if !IsEmoji(e) {
			t.Errorf("%q isn't on the list", e)
		}
	}
	for _, e := range []string{"", "a", ":)", "🏻", "❤", "👍👍", "👍 ", "🫪", "🧑‍🩰", "🧑🏻‍🩰", "‍", "️"} {
		if IsEmoji(e) {
			t.Errorf("%q is on the list", e)
		}
	}
	if EmojiVersion != "16.0" {
		t.Errorf("the list stops at Emoji %s", EmojiVersion)
	}
}

// The page's list, which its picker shows, is made from the same lines as
// denproto's: each emoji on the page, and each of its forms in a skin
// tone, is on denproto's list, and denproto's list has nothing more but
// the forms of two people of two different tones, which the page offers
// none of.
func TestEmojiListsAgree(t *testing.T) {
	data, err := os.ReadFile("../ui/assets/emoji.json")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Version string            `json:"version"`
		Groups  []string          `json:"groups"`
		Emoji   []json.RawMessage `json:"emoji"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Version != EmojiVersion || len(page.Groups) != 9 {
		t.Errorf("the page's list is Emoji %s in %d groups", page.Version, len(page.Groups))
	}
	onPage := map[string]bool{}
	for _, raw := range page.Emoji {
		var e []any
		if err := json.Unmarshal(raw, &e); err != nil || len(e) < 4 || len(e) > 5 {
			t.Fatalf("an entry %s", raw)
		}
		list := []any{e[0]}
		if len(e) == 5 {
			tones, _ := e[4].([]any)
			if len(tones) != 5 {
				t.Fatalf("%s has %d tones", e[0], len(tones))
			}
			list = append(list, tones...)
		}
		for _, x := range list {
			s, _ := x.(string)
			if !IsEmoji(s) || onPage[s] {
				t.Errorf("the page lists %q, which denproto's list lacks or the page has twice", s)
			}
			onPage[s] = true
		}
	}
	tones := []string{"\U0001F3FB", "\U0001F3FC", "\U0001F3FD", "\U0001F3FE", "\U0001F3FF"}
	for e := range unicodeEmoji() {
		if onPage[e] {
			continue
		}
		used := 0
		for _, tone := range tones {
			if strings.Contains(e, tone) {
				used++
			}
		}
		if used < 2 {
			t.Errorf("denproto's list has %q, which the page lacks", e)
		}
	}
	if !slices.Contains(page.Groups, "Flags") {
		t.Errorf("the page's groups are %v", page.Groups)
	}
}
