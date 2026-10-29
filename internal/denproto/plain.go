package denproto

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SpoilerText stands for a spoiler in Plain's text.
const SpoilerText = "(spoiler)"

// Plain is the start of a text as a notification shows it (M12.3): its
// words without the markdown subset's marks, its lines run together, and
// each spoiler, as the page's renderer finds them, put as SpoilerText, so a
// notification never shows what a spoiler hides. Control and format
// characters go, as from names, so the text can't disguise itself or
// reorder what's around it. A text longer than limit characters stops at a
// word's end in its last fifth, or at limit, with an ellipsis.
func Plain(text string, limit int) string {
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		b.WriteByte(' ')
		if isFence(line) {
			// A code block's lines are its text, to the next fence or the
			// end.
			for i++; i < len(lines) && !isFence(lines[i]); i++ {
				b.WriteString(lines[i])
				b.WriteByte(' ')
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, taskOpen) || strings.HasPrefix(line, taskDone):
			line = line[len(taskOpen):]
		case strings.HasPrefix(line, ">"):
			line = strings.TrimPrefix(line[1:], " ")
		}
		plainInline(&b, line, marksOf(line), 0, len(line))
	}
	return shorten(squeeze(b.String()), limit)
}

// plainInline writes s from start to end without its marks, as
// inlineLinks walks it: code spans first, then links and mentions, which
// keep their marks, then delimited spans, whose insides it walks the same
// way but for a spoiler's.
func plainInline(b *strings.Builder, s string, m *marks, start, end int) {
	for i := start; i < end; {
		c := s[i]
		var before byte
		if i > 0 {
			before = s[i-1]
		}
		if c == '`' && m.code != nil && int(m.code[i]) > i {
			b.WriteString(s[i+1 : m.code[i]])
			i = int(m.code[i]) + 1
			continue
		}
		if c == 'h' && !isNameByte(before) {
			if n := linkLength(s[i:end]); n > 0 {
				b.WriteString(s[i : i+n])
				i += n
				continue
			}
		}
		if d := delimiterAt(s[:end], i); d >= 0 {
			from := i + len(delimiters[d])
			if from < end {
				if close := m.closer(d, from); close > from && close+len(delimiters[d]) <= end {
					if delimiters[d] == "||" {
						b.WriteString(SpoilerText)
					} else {
						plainInline(b, s, m, from, close)
					}
					i = close + len(delimiters[d])
					continue
				}
			}
		}
		b.WriteByte(c)
		i++
	}
}

// squeeze drops control and format characters, and runs of spaces become
// one, with none at either end.
func squeeze(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError:
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

// shorten cuts s to at most limit characters, ending with an ellipsis.
func shorten(s string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	s = string([]rune(s)[:limit-1])
	if cut := strings.LastIndexByte(s, ' '); cut >= 0 && utf8.RuneCountInString(s[:cut]) >= limit*4/5 {
		s = s[:cut]
	}
	return strings.TrimRight(s, " ") + "…"
}
