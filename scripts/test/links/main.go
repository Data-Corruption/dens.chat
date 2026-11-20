// Command links checks the contributors' docs: every relative link and
// anchor in the root's Markdown, docs/*.md and docs/dev resolves, and every
// docs/dev path a comment names exists, in code, scripts and workflows. The site's pages under
// docs/content are Hugo's to check. ./scripts/test.sh -lint runs it from the
// repository's root.
//
// Files are those Git tracks and those it would track, so a new file counts
// before it's staged and an ignored one never does: CI's checkout has only
// what's committed.
package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// checked reports whether a file's links and headings are this check's: the
// root's Markdown, the docs directory's own and everything under docs/dev.
func checked(name string) bool {
	if !strings.HasSuffix(name, ".md") {
		return false
	}
	dir := path.Dir(name)
	return dir == "." || dir == "docs" || dir == "docs/dev" || strings.HasPrefix(dir, "docs/dev/")
}

// skipped names files whose docs/dev paths aren't references to check: the
// Markdown, whose links are, the ignore files' patterns, and the media
// module, 24 MB of generated Go.
func skipped(name string) bool {
	if strings.HasSuffix(name, ".md") {
		return true
	}
	switch name {
	case ".gitignore", ".ignore", ".gitattributes":
		return true
	}
	return strings.HasPrefix(name, "internal/media/ffmpeg/module/")
}

var (
	fence      = regexp.MustCompile("^ {0,3}(```+|~~~+)")
	heading    = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	inlineLink = regexp.MustCompile(`!?\[([^\]]*)\]\(\s*<?([^)\s>]*)>?(?:\s+"[^"]*")?\s*\)`)
	definition = regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:[ \t]*<?([^\s>]+)>?`)
	devPath    = regexp.MustCompile(`docs/dev/[A-Za-z0-9_./-]*(#[A-Za-z0-9_-]+)?`)
	scheme     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

type doc struct {
	text    string          // with fenced code and code spans blanked, newlines kept
	anchors map[string]bool // GitHub's anchors for its headings
}

type checker struct {
	files    map[string]bool
	dirs     map[string]bool
	docs     map[string]*doc
	problems []string
	links    int
	paths    int
}

func main() {
	c, err := newChecker()
	if err != nil {
		fmt.Fprintln(os.Stderr, "links:", err)
		os.Exit(1)
	}
	names := make([]string, 0, len(c.files))
	for name := range c.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if checked(name) {
			if err := c.checkLinks(name); err != nil {
				fmt.Fprintln(os.Stderr, "links:", err)
				os.Exit(1)
			}
		}
		if !skipped(name) {
			if err := c.checkPaths(name); err != nil {
				fmt.Fprintln(os.Stderr, "links:", err)
				os.Exit(1)
			}
		}
	}
	if len(c.problems) > 0 {
		for _, p := range c.problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr, "error: %d broken links or paths\n", len(c.problems))
		os.Exit(1)
	}
	fmt.Printf("🟢 links passed (%d links, %d docs/dev paths)\n", c.links, c.paths)
}

func newChecker() (*checker, error) {
	out, err := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}
	c := &checker{files: map[string]bool{}, dirs: map[string]bool{".": true}, docs: map[string]*doc{}}
	for _, name := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		// A tracked file deleted from the tree is gone for every link to it.
		if info, err := os.Lstat(name); err != nil || !info.Mode().IsRegular() {
			continue
		}
		c.files[name] = true
		for d := path.Dir(name); d != "."; d = path.Dir(d) {
			c.dirs[d] = true
		}
	}
	return c, nil
}

func (c *checker) load(name string) (*doc, error) {
	if d, ok := c.docs[name]; ok {
		return d, nil
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	d := parse(string(raw))
	c.docs[name] = d
	return d, nil
}

// parse blanks a document's fenced code and code spans, whose brackets and
// hashes are text, and collects its headings' anchors.
func parse(raw string) *doc {
	lines := strings.Split(raw, "\n")
	anchors := map[string]bool{}
	seen := map[string]int{}
	open := ""
	for i, line := range lines {
		if m := fence.FindStringSubmatch(line); m != nil {
			// A fence closes on a run of its own character at least as long.
			if open == "" {
				open = m[1]
				lines[i] = ""
				continue
			}
			if m[1][0] == open[0] && len(m[1]) >= len(open) {
				open = ""
				lines[i] = ""
				continue
			}
		}
		if open != "" {
			lines[i] = ""
			continue
		}
		if m := heading.FindStringSubmatch(line); m != nil {
			a := Slug(m[2])
			if n := seen[a]; n > 0 {
				anchors[fmt.Sprintf("%s-%d", a, n)] = true
			} else {
				anchors[a] = true
			}
			seen[a]++
		}
	}
	return &doc{text: blankSpans(strings.Join(lines, "\n")), anchors: anchors}
}

// blankSpans replaces each code span, a run of backticks to the next run of
// the same length, with spaces, keeping newlines and so line numbers.
func blankSpans(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		closeAt := -1
		for j := i + n; j < len(b); {
			if b[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(b) && b[j+m] == '`' {
				m++
			}
			if m == n {
				closeAt = j
				break
			}
			j += m
		}
		if closeAt < 0 {
			i += n
			continue
		}
		for k := i; k < closeAt+n; k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
		i = closeAt + n
	}
	return string(b)
}

// Slug is the anchor GitHub gives a heading: its text lowercased, with
// everything but letters, marks, numbers, connectors, hyphens and spaces
// dropped, and each space a hyphen. Inline code and emphasis lose their
// markers and a link keeps its text, as the rendered heading shows them.
func Slug(text string) string {
	text = inlineLink.ReplaceAllString(text, "$1")
	text = strings.ToLower(text)
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.Pc):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func lineOf(text string, offset int) int {
	return 1 + strings.Count(text[:offset], "\n")
}

func (c *checker) checkLinks(name string) error {
	d, err := c.load(name)
	if err != nil {
		return err
	}
	check := func(target string, offset int) error {
		if target == "" || scheme.MatchString(target) || strings.HasPrefix(target, "//") {
			return nil
		}
		c.links++
		where := fmt.Sprintf("%s:%d", name, lineOf(d.text, offset))
		file, anchor, _ := strings.Cut(target, "#")
		if file == "" {
			file = name
		} else {
			unescaped, err := url.PathUnescape(file)
			if err != nil {
				c.problems = append(c.problems, fmt.Sprintf("%s: %s doesn't decode", where, target))
				return nil
			}
			if strings.HasPrefix(unescaped, "/") {
				file = path.Clean(strings.TrimPrefix(unescaped, "/"))
			} else {
				file = path.Join(path.Dir(name), unescaped)
			}
		}
		return c.resolve(where, target, file, anchor)
	}
	for _, m := range inlineLink.FindAllStringSubmatchIndex(d.text, -1) {
		if err := check(d.text[m[4]:m[5]], m[0]); err != nil {
			return err
		}
	}
	for _, m := range definition.FindAllStringSubmatchIndex(d.text, -1) {
		if err := check(d.text[m[2]:m[3]], m[0]); err != nil {
			return err
		}
	}
	return nil
}

// resolve checks a link's file exists and, where the file is one of the
// checked docs, that it has the anchor.
func (c *checker) resolve(where, target, file, anchor string) error {
	if file == ".." || strings.HasPrefix(file, "../") {
		c.problems = append(c.problems, fmt.Sprintf("%s: %s leaves the repository", where, target))
		return nil
	}
	if !c.files[file] && !c.dirs[file] {
		c.problems = append(c.problems, fmt.Sprintf("%s: %s: no such file", where, target))
		return nil
	}
	if anchor == "" || !c.files[file] || !checked(file) {
		return nil
	}
	d, err := c.load(file)
	if err != nil {
		return err
	}
	if !d.anchors[anchor] {
		c.problems = append(c.problems, fmt.Sprintf("%s: %s: %s has no heading #%s", where, target, file, anchor))
	}
	return nil
}

// checkPaths checks each docs/dev path a file names, as from the
// repository's root.
func (c *checker) checkPaths(name string) error {
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if bytes.IndexByte(raw[:min(len(raw), 8000)], 0) >= 0 {
		return nil
	}
	text := string(raw)
	for _, m := range devPath.FindAllStringIndex(text, -1) {
		ref := text[m[0]:m[1]]
		file, anchor, _ := strings.Cut(ref, "#")
		// A sentence's full stop or a path's trailing slash isn't the path.
		file = strings.TrimRight(file, "./")
		c.paths++
		where := fmt.Sprintf("%s:%d", name, lineOf(text, m[0]))
		if err := c.resolve(where, ref, file, anchor); err != nil {
			return err
		}
	}
	return nil
}
