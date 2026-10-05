package denproto

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// Links (M3). When a message is sent or edited, or a channel's description
// or a member's bio set, the den rewrites the links in it, and a DM's
// sender does the same before sealing: YouTube, Reddit and X links become
// one canonical URL, and every link loses its tracking parameters. A link
// is what the page renders as one, so findLinks follows the page's renderer
// (markdown.jsx) step by step, and both are tested against the cases in
// testdata/links.json.

// CleanLinks rewrites the links in text. A link stays as it is if the page
// would find another link once it's rewritten, as when what's left ends in
// punctuation the page leaves out of a link.
func CleanLinks(text string) string {
	links := findLinks(text)
	for k := 0; k < len(links); k++ {
		l := links[k]
		raw := text[l.start:l.end]
		clean := cleanLink(raw)
		if clean == raw {
			continue
		}
		next := text[:l.start] + clean + text[l.end:]
		found := findLinks(next)
		if !sameLinks(text, links, next, found, k, clean) {
			continue
		}
		text, links = next, found
	}
	return text
}

// sameLinks reports whether found, the links in next, are links, the
// links in text, with link k replaced by clean.
func sameLinks(text string, links []span, next string, found []span, k int, clean string) bool {
	if len(found) != len(links) {
		return false
	}
	for j, f := range found {
		want := text[links[j].start:links[j].end]
		if j == k {
			want = clean
		}
		if next[f.start:f.end] != want {
			return false
		}
	}
	return true
}

// span is where a link sits in a text, in bytes.
type span struct{ start, end int }

// findLinks returns the links the page's renderer finds in a text, in
// order: outside code blocks, in each task line, quote line and paragraph
// line as blocks() in markdown.jsx splits them.
func findLinks(text string) []span {
	var out []span
	lines := strings.Split(text, "\n")
	at := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		start := at
		at += len(line) + 1
		if isFence(line) {
			// A code block runs to the next fence, or to the end.
			for i++; i < len(lines) && !isFence(lines[i]); i++ {
				at += len(lines[i]) + 1
			}
			if i < len(lines) {
				at += len(lines[i]) + 1
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, taskOpen) || strings.HasPrefix(line, taskDone):
			out = inlineLinks(out, line[len(taskOpen):], start+len(taskOpen), 0)
		case strings.HasPrefix(line, ">"):
			n := 1
			if strings.HasPrefix(line[1:], " ") {
				n = 2
			}
			out = inlineLinks(out, line[n:], start+n, 0)
		default:
			out = inlineLinks(out, line, start, 0)
		}
	}
	return out
}

// isFence reports whether a line opens or closes a code block, trimmed of
// spaces as JavaScript's trim() does.
func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimFunc(line, jsSpace), "```")
}

// delimiters open the spans the page's renderer nests, in the order it
// tries them: bold, strikethrough, spoiler, and italic with * or _.
var delimiters = []string{"**", "~~", "||", "*", "_"}

// inlineLinks appends the links in one line of inline text, as inline() in
// markdown.jsx finds them: code spans first, then links, mentions and
// delimited spans, whose insides it walks the same way. base is where s
// starts in the whole text, and prev the character before it (0 for none),
// which decides whether a link or mention can start at its first byte.
// Every decision is on ASCII, so walking bytes where the page walks UTF-16
// units finds the same things.
func inlineLinks(out []span, s string, base int, prev byte) []span {
	for i := 0; i < len(s); {
		c := s[i]
		before := prev
		if i > 0 {
			before = s[i-1]
		}
		if c == '`' {
			if e := strings.IndexByte(s[i+1:], '`'); e > 0 {
				i += e + 2
				continue
			}
		}
		if c == 'h' && !isNameByte(before) {
			if n := linkLength(s[i:]); n > 0 {
				end := i + len(trimLink(s[i:i+n]))
				out = append(out, span{base + i, base + end})
				i = end
				continue
			}
		}
		if c == '@' && !isNameByte(before) && before != '@' {
			j := i + 1
			for j < len(s) && isNameByte(s[j]) {
				j++
			}
			if n := j - i - 1; n >= 2 && n <= 32 {
				i = j
				continue
			}
		}
		if d := delimiterAt(s, i); d != "" {
			if end := findClose(s, d, i+len(d)); end > i+len(d) {
				out = inlineLinks(out, s[i+len(d):end], base+i+len(d), d[len(d)-1])
				i = end + len(d)
				continue
			}
		}
		i++
	}
	return out
}

// delimiterAt returns the first delimiter s has at i, or "". The page
// tries no other once one matches, even if that one never closes.
func delimiterAt(s string, i int) string {
	for _, d := range delimiters {
		if strings.HasPrefix(s[i:], d) {
			return d
		}
	}
	return ""
}

// findClose finds the delimiter d that closes a span opened before start:
// the first one after something other than a space, skipping code spans.
// It returns -1 if there's none.
func findClose(s, d string, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == '`' {
			if e := strings.IndexByte(s[i+1:], '`'); e >= 0 {
				i += e + 1
				continue
			}
		}
		if strings.HasPrefix(s[i:], d) {
			if r, _ := utf8.DecodeLastRuneInString(s[:i]); !jsSpace(r) {
				return i
			}
		}
	}
	return -1
}

// trimLink drops what most likely ends a sentence rather than a link, as
// the page does: punctuation at the end, and a ")" there that closes no
// "(" in the link, so a link in parentheses loses the closing one but
// https://en.wikipedia.org/wiki/Go_(programming_language) keeps its own.
func trimLink(s string) string {
	for len(s) > 0 {
		switch last := s[len(s)-1]; {
		case strings.IndexByte(".,;:!?]", last) >= 0:
			s = s[:len(s)-1]
		case last == ')' && strings.Count(s, ")") > strings.Count(s, "("):
			s = s[:len(s)-1]
		default:
			return s
		}
	}
	return s
}

// jsSpace reports whether JavaScript's \s matches r.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// cleanLink rewrites one link: a known site's to its canonical form, and
// any other without its tracking parameters. A link that doesn't parse, or
// names a user or password before its host, stays as it is.
func cleanLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return raw
	}
	// Hosts compare in lowercase, as DNS does, and exactly: a port or a
	// trailing dot makes another host.
	host := strings.ToLower(u.Host)
	if c, ok := canonical(host, u.EscapedPath(), u.RawQuery); ok {
		return c
	}
	return dropTracking(raw, host)
}

// canonical returns the one form a YouTube video's, Reddit post's or X
// post's link is written in, keeping only what identifies it, and a
// YouTube link's start time.
func canonical(host, path, query string) (string, bool) {
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		seg := segments(path)
		switch {
		case path == "/watch":
			return youTube(param(query, "v"), param(query, "t"))
		case len(seg) == 2 && (seg[0] == "shorts" || seg[0] == "live"):
			return youTube(seg[1], param(query, "t"))
		case len(seg) == 2 && seg[0] == "embed":
			return youTube(seg[1], embedStart(query))
		}
	case "youtu.be":
		if seg := segments(path); len(seg) == 1 {
			return youTube(seg[0], param(query, "t"))
		}
	case "youtube-nocookie.com", "www.youtube-nocookie.com":
		if seg := segments(path); len(seg) == 2 && seg[0] == "embed" {
			return youTube(seg[1], embedStart(query))
		}
	case "reddit.com", "www.reddit.com", "old.reddit.com", "new.reddit.com", "np.reddit.com", "m.reddit.com":
		return redditPost(segments(strings.TrimSuffix(path, "/")))
	case "redd.it":
		if seg := segments(strings.TrimSuffix(path, "/")); len(seg) == 1 && redditID(seg[0]) {
			return "https://www.reddit.com/comments/" + seg[0], true
		}
	case "x.com", "www.x.com", "mobile.x.com", "twitter.com", "www.twitter.com", "mobile.twitter.com":
		seg := segments(path)
		if len(seg) >= 4 && seg[0] == "i" && seg[1] == "web" && seg[2] == "status" && statusID(seg[3]) {
			return "https://x.com/i/status/" + seg[3], true
		}
		if len(seg) >= 3 && xName(seg[0]) && seg[1] == "status" && statusID(seg[2]) {
			return "https://x.com/i/status/" + seg[2], true
		}
	}
	return "", false
}

func youTube(id, start string) (string, bool) {
	if !videoID(id) {
		return "", false
	}
	out := "https://www.youtube.com/watch?v=" + id
	if startTime(start) {
		out += "&t=" + start
	}
	return out, true
}

// embedStart is an embed's start time: start, as embeds say it, or t.
func embedStart(query string) string {
	if s := param(query, "start"); s != "" {
		return s
	}
	return param(query, "t")
}

// redditPost reads a Reddit link's path: /comments/POST, also after
// /r/NAME, /u/NAME or /user/NAME, then optionally a title and a comment
// ID, which also reads Reddit's /comment/ID; or /gallery/POST.
func redditPost(seg []string) (string, bool) {
	if len(seg) == 2 && seg[0] == "gallery" && redditID(seg[1]) {
		return "https://www.reddit.com/comments/" + seg[1], true
	}
	if len(seg) >= 2 && (seg[0] == "r" || seg[0] == "u" || seg[0] == "user") && redditName(seg[1]) {
		seg = seg[2:]
	}
	if len(seg) < 2 || len(seg) > 4 || seg[0] != "comments" || !redditID(seg[1]) {
		return "", false
	}
	out := "https://www.reddit.com/comments/" + seg[1]
	// Reddit reads any title, an empty one included, as in the links PRAW
	// makes: /comments/POST//COMMENT.
	if len(seg) == 4 {
		if !redditID(seg[3]) {
			return "", false
		}
		out += "/comment/" + seg[3]
	}
	return out, true
}

// segments splits a path after its leading slash.
func segments(path string) []string {
	if !strings.HasPrefix(path, "/") {
		return nil
	}
	return strings.Split(path[1:], "/")
}

// param returns the first value a raw query gives name, as it's written.
func param(query, name string) string {
	for _, p := range strings.Split(query, "&") {
		if k, v, ok := strings.Cut(p, "="); ok && k == name {
			return v
		}
	}
	return ""
}

func videoID(s string) bool {
	return len(s) == 11 && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") == ""
}

func redditID(s string) bool {
	return len(s) >= 1 && len(s) <= 16 && strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789") == ""
}

func redditName(s string) bool {
	return len(s) >= 1 && len(s) <= 32 && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") == ""
}

func xName(s string) bool {
	return len(s) >= 1 && len(s) <= 50 && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_") == ""
}

func statusID(s string) bool {
	return len(s) >= 1 && len(s) <= 20 && strings.Trim(s, "0123456789") == ""
}

// startTime reports whether s is a YouTube start time: seconds, with or
// without an s, or hours, minutes and seconds like 1h2m3s, each optional
// but in that order.
func startTime(s string) bool {
	if s == "" || len(s) > 16 {
		return false
	}
	if digits := strings.TrimSuffix(s, "s"); digits != "" && strings.Trim(digits, "0123456789") == "" {
		return true
	}
	rest := s
	for _, unit := range []byte{'h', 'm', 's'} {
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n > 0 && n < len(rest) && rest[n] == unit {
			rest = rest[n+1:]
		}
	}
	return rest == ""
}

// tracking lists the parameters that track on any site, besides every one
// whose name starts with utm_.
var tracking = map[string]bool{
	"fbclid": true, "gclid": true, "dclid": true, "gbraid": true, "wbraid": true, "msclkid": true,
	"twclid": true, "ttclid": true, "yclid": true, "igshid": true, "igsh": true, "mc_cid": true,
	"mc_eid": true, "_hsenc": true, "_hsmi": true, "mkt_tok": true,
}

// siteTracking returns the parameters that track on a site's own hosts.
func siteTracking(host string) []string {
	switch {
	case onSite(host, "youtube.com", "youtu.be", "youtube-nocookie.com"):
		return []string{"si", "pp", "feature"}
	case onSite(host, "x.com", "twitter.com"):
		return []string{"s", "t", "ref_src", "ref_url"}
	case onSite(host, "reddit.com", "redd.it"):
		return []string{"share_id"}
	}
	return nil
}

// onSite reports whether host is one of domains or a subdomain of one.
func onSite(host string, domains ...string) bool {
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// dropTracking takes a link's tracking parameters out of its query and
// leaves the rest as it was written: the other parameters, their order
// and spelling, and the fragment. A query left empty goes with its "?".
func dropTracking(raw, host string) string {
	q := strings.IndexByte(raw, '?')
	end := len(raw)
	if h := strings.IndexByte(raw, '#'); h >= 0 {
		if q > h {
			return raw
		}
		end = h
	}
	if q < 0 {
		return raw
	}
	site := siteTracking(host)
	var kept []string
	dropped := false
	for _, p := range strings.Split(raw[q+1:end], "&") {
		name, _, _ := strings.Cut(p, "=")
		switch {
		case tracking[name] || strings.HasPrefix(name, "utm_") || contains(site, name):
			dropped = true
		case p != "":
			kept = append(kept, p)
		}
	}
	if !dropped {
		return raw
	}
	out := raw[:q]
	if len(kept) > 0 {
		out += "?" + strings.Join(kept, "&")
	}
	return out + raw[end:]
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
