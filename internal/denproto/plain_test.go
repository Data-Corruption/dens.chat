package denproto

import (
	"strings"
	"testing"
)

func TestPlain(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"hello", "hello"},
		{"  **bold** and *it* and _it_ and ~~gone~~  ", "bold and it and it and gone"},
		{"the answer is ||42||, keep it quiet", "the answer is (spoiler), keep it quiet"},
		// A spoiler hides what's in it, marks and all.
		{"||**nested** `code`||", "(spoiler)"},
		// A delimiter that never closes is text.
		{"2 * 3 = 6, and || alone", "2 * 3 = 6, and || alone"},
		// Inside code, nothing is a mark, and a spoiler there is text, as the
		// page shows it.
		{"run `a || b` now", "run a || b now"},
		{"see https://example.com/a_b_c_d and _this_", "see https://example.com/a_b_c_d and this"},
		{"hi @alice_b, **@bob**", "hi @alice_b, @bob"},
		{"line one\nline two\n\n> quoted\n[ ] a task\n[x] done", "line one line two quoted a task done"},
		{"```\nfmt.Println(\"||x||\")\n```\nafter", `fmt.Println("||x||") after`},
		// Bidi overrides and other format characters go, as from names.
		{"pay ‮exe.gpj‬ now​\x07", "pay exe.gpj now"},
		{"", ""},
		{"||only a spoiler||", "(spoiler)"},
	} {
		if got := Plain(c.text, 0); got != c.want {
			t.Errorf("Plain(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestPlainShortens(t *testing.T) {
	long := strings.Repeat("word ", 40)
	got := Plain(long, 50)
	if n := len([]rune(got)); n > 50 || !strings.HasSuffix(got, "word…") {
		t.Errorf("Plain shortened to %q (%d characters), want a word's end before an ellipsis, within 50", got, n)
	}
	// No space in the last fifth: cut where the limit falls.
	unbroken := strings.Repeat("é", 80)
	if got := Plain(unbroken, 20); got != strings.Repeat("é", 19)+"…" {
		t.Errorf("Plain(80 é, 20) = %q", got)
	}
	if got := Plain("short enough", 20); got != "short enough" {
		t.Errorf("Plain kept %q of a short text", got)
	}
}
