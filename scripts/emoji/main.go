// Command emoji writes the list of Unicode's emoji (M15.2), for the page's
// picker and for denproto, which checks reactions against it (M15.3), from
// Unicode's emoji-test.txt and CLDR's English annotations. scripts/emoji.sh
// runs it with the pinned inputs; see there.
//
// Each fully qualified emoji up to an Emoji version goes in, by that
// version as each line of emoji-test.txt names it, so the list holds only
// what the systems draw. denproto gets every one, skin tones and all. The
// page gets the emoji without a skin tone, each with its name, its group,
// its keywords from CLDR and, where it takes one, its form in each of the
// five tones: the forms whose name gives a single tone, so an emoji of two
// people gets the one where both have it.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

// tones are the skin tones, in the order the page offers them, as names
// say them.
var tones = []string{"light skin tone", "medium-light skin tone", "medium skin tone", "medium-dark skin tone", "dark skin tone"}

// line is a fully qualified emoji from emoji-test.txt.
type line struct {
	emoji, name, group string
	version            string
}

func main() {
	test := flag.String("test", "", "emoji-test.txt")
	annotations := flag.String("annotations", "", "CLDR's common/annotations/en.xml")
	derived := flag.String("derived", "", "CLDR's common/annotationsDerived/en.xml")
	upto := flag.String("upto", "", "the newest Emoji version to keep, as 16.0")
	page := flag.String("page", "", "where the page's list goes")
	goList := flag.String("go", "", "where denproto's list goes")
	flag.Parse()
	if *test == "" || *annotations == "" || *derived == "" || *upto == "" || *page == "" || *goList == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*test, *annotations, *derived, *upto, *page, *goList); err != nil {
		fmt.Fprintln(os.Stderr, "emoji:", err)
		os.Exit(1)
	}
}

func run(testPath, annotationsPath, derivedPath, upto, pagePath, goPath string) error {
	f, err := os.Open(testPath)
	if err != nil {
		return err
	}
	defer f.Close()
	lines, err := readTest(f, upto)
	if err != nil {
		return err
	}
	keywords := map[string][]string{}
	for _, path := range []string{annotationsPath, derivedPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := readAnnotations(data, keywords); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	list, err := pageList(lines, keywords, upto)
	if err != nil {
		return err
	}
	if err := os.WriteFile(pagePath, list, 0o644); err != nil {
		return err
	}
	return os.WriteFile(goPath, goSource(lines, upto), 0o644)
}

// readTest reads emoji-test.txt's fully qualified emoji up to an Emoji
// version, in its order, CLDR's, with their groups. Components, the skin
// tones and hair styles alone, aren't emoji to pick.
func readTest(r io.Reader, upto string) ([]line, error) {
	max, err := parseVersion(upto)
	if err != nil {
		return nil, err
	}
	var out []line
	group := ""
	seen := map[string]bool{}
	s := bufio.NewScanner(r)
	for n := 1; s.Scan(); n++ {
		text := s.Text()
		if g, ok := strings.CutPrefix(text, "# group: "); ok {
			group = g
			continue
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		points, rest, ok := strings.Cut(text, ";")
		status, comment, ok2 := strings.Cut(rest, "#")
		if !ok || !ok2 {
			return nil, fmt.Errorf("line %d: no status or comment", n)
		}
		if strings.TrimSpace(status) != "fully-qualified" || group == "Component" {
			continue
		}
		var emoji strings.Builder
		for _, p := range strings.Fields(points) {
			cp, err := strconv.ParseUint(p, 16, 32)
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", n, err)
			}
			emoji.WriteRune(rune(cp))
		}
		// The comment is the emoji, its version and its name.
		fields := strings.Fields(comment)
		if len(fields) < 3 || fields[0] != emoji.String() || !strings.HasPrefix(fields[1], "E") {
			return nil, fmt.Errorf("line %d: a comment that doesn't name the emoji", n)
		}
		v, err := parseVersion(fields[1][1:])
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		if v > max {
			continue
		}
		e := emoji.String()
		if seen[e] {
			return nil, fmt.Errorf("line %d: %s twice", n, e)
		}
		seen[e] = true
		name := strings.Join(fields[2:], " ")
		out = append(out, line{emoji: e, name: name, group: group, version: fields[1][1:]})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(out) < 1000 {
		return nil, fmt.Errorf("only %d emoji", len(out))
	}
	return out, nil
}

// parseVersion reads an Emoji version, as 15.1, into a number that orders
// them.
func parseVersion(v string) (int, error) {
	major, minor, ok := strings.Cut(v, ".")
	a, err1 := strconv.Atoi(major)
	b, err2 := strconv.Atoi(minor)
	if !ok || err1 != nil || err2 != nil || b < 0 || b > 9 {
		return 0, fmt.Errorf("an Emoji version of %q", v)
	}
	return a*10 + b, nil
}

// readAnnotations reads CLDR's annotations' keywords into keywords, by
// emoji without U+FE0F, as CLDR writes them.
func readAnnotations(data []byte, keywords map[string][]string) error {
	var doc struct {
		Annotations []struct {
			CP   string `xml:"cp,attr"`
			Type string `xml:"type,attr"`
			Text string `xml:",chardata"`
		} `xml:"annotations>annotation"`
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	// The DTD it names isn't needed to read it.
	d.Strict = true
	if err := d.Decode(&doc); err != nil {
		return err
	}
	if len(doc.Annotations) == 0 {
		return errors.New("no annotations")
	}
	for _, a := range doc.Annotations {
		if a.Type != "" {
			continue
		}
		var words []string
		for w := range strings.SplitSeq(a.Text, "|") {
			if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
				words = append(words, w)
			}
		}
		keywords[bare(a.CP)] = words
	}
	return nil
}

// bare is an emoji without U+FE0F, as CLDR keys them.
func bare(s string) string { return strings.ReplaceAll(s, "️", "") }

// toned splits a name into the name of the emoji it tones and its tone:
// "man: medium skin tone, red hair" is "man: red hair" in the medium tone.
// A name of two tones, as for two people of different tones, is mixed; a
// name of none has no tone.
func toned(name string) (base string, tone int, mixed bool) {
	head, tail, ok := strings.Cut(name, ": ")
	if !ok {
		return name, -1, false
	}
	var rest []string
	tone = -1
	for item := range strings.SplitSeq(tail, ", ") {
		t := slices.Index(tones, item)
		switch {
		case t < 0:
			rest = append(rest, item)
		case tone >= 0 && t != tone:
			mixed = true
		default:
			tone = t
		}
	}
	if tone < 0 {
		return name, -1, false
	}
	if len(rest) == 0 {
		return head, tone, mixed
	}
	return head + ": " + strings.Join(rest, ", "), tone, mixed
}

// entry is an emoji as the page has it: itself, its name, its group's
// index, its keywords beyond its name's words, and its form in each tone
// where it takes one, which the list writes as an array.
type entry struct {
	emoji, name string
	group       int
	keywords    string
	tones       []string
}

func (e entry) array() []any {
	a := []any{e.emoji, e.name, e.group, e.keywords}
	if e.tones != nil {
		a = append(a, e.tones)
	}
	return a
}

// marshal writes JSON as it is, without escaping what HTML would take for
// markup, as the & in a group's name: the page reads it as data.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// pageList is the page's list: the Emoji version it stops at, the groups'
// names in order, and the emoji, each where its group lists it.
func pageList(lines []line, keywords map[string][]string, upto string) ([]byte, error) {
	var groups []string
	var entries []*entry
	byName := map[string]*entry{}
	for _, l := range lines {
		base, tone, mixed := toned(l.name)
		if tone >= 0 {
			// A form of an emoji listed before it, in one tone.
			if mixed {
				continue
			}
			e := byName[base]
			if e == nil {
				return nil, fmt.Errorf("%s (%s) tones no emoji", l.emoji, l.name)
			}
			if e.tones == nil {
				e.tones = make([]string, len(tones))
			}
			if e.tones[tone] != "" {
				return nil, fmt.Errorf("%s has two forms in the %s", base, tones[tone])
			}
			e.tones[tone] = l.emoji
			continue
		}
		if len(groups) == 0 || groups[len(groups)-1] != l.group {
			if slices.Contains(groups, l.group) {
				return nil, fmt.Errorf("the group %s comes twice", l.group)
			}
			groups = append(groups, l.group)
		}
		if byName[l.name] != nil {
			return nil, fmt.Errorf("two emoji named %s", l.name)
		}
		words, ok := keywords[bare(l.emoji)]
		if !ok {
			return nil, fmt.Errorf("%s (%s) has no keywords", l.emoji, l.name)
		}
		e := &entry{emoji: l.emoji, name: l.name, group: len(groups) - 1, keywords: beyond(l.name, words)}
		byName[l.name] = e
		entries = append(entries, e)
	}
	for _, e := range entries {
		if e.tones != nil && slices.Contains(e.tones, "") {
			return nil, fmt.Errorf("%s (%s) lacks a tone", e.emoji, e.name)
		}
	}
	var buf bytes.Buffer
	head, err := marshal(groups)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&buf, "{\"version\":%q,\"groups\":%s,\"emoji\":[\n", upto, head)
	// One emoji a line, so a change of list reads as lines in review.
	for i, e := range entries {
		b, err := marshal(e.array())
		if err != nil {
			return nil, err
		}
		buf.Write(b)
		if i < len(entries)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("]}\n")
	return buf.Bytes(), nil
}

// beyond is an emoji's keywords but those whose every word its name holds,
// which a search finds by its name already, as one string, | between them.
func beyond(name string, words []string) string {
	split := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ' ' || r == ':' || r == ',' })
	}
	inName := split(name)
	var out []string
	for _, w := range words {
		covered := true
		for _, part := range split(w) {
			covered = covered && slices.Contains(inName, part)
		}
		if !covered && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return strings.Join(out, "|")
}

// goSource is denproto's list: every fully qualified emoji, a line each.
func goSource(lines []line, upto string) []byte {
	var b strings.Builder
	b.WriteString("// Code generated by scripts/emoji.sh; DO NOT EDIT.\n\n")
	b.WriteString("package denproto\n\n")
	b.WriteString("// EmojiVersion is the newest Emoji version the list of Unicode's emoji\n")
	b.WriteString("// holds (M15.2).\n")
	fmt.Fprintf(&b, "const EmojiVersion = %q\n\n", upto)
	b.WriteString("// emojiList is every fully qualified emoji up to EmojiVersion, skin tones\n")
	b.WriteString("// and all, a line each, in CLDR's order.\n")
	b.WriteString("const emojiList = \"\" +\n")
	for i, l := range lines {
		end := " +\n"
		if i == len(lines)-1 {
			end = "\n"
		}
		fmt.Fprintf(&b, "\t%s%s", strconv.QuoteToASCII(l.emoji+"\n"), end)
	}
	return []byte(b.String())
}
