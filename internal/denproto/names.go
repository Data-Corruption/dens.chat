package denproto

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxNameRunes  = 32
	MaxLabelRunes = 64
	maxURLLength  = 200
	invitePrefix  = "dens1:"
)

// NormalizeUsername lowercases and trims a username and checks it: 2 to 32
// characters of a-z, 0-9 and _.
func NormalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 2 || len(s) > 32 {
		return "", errors.New("a username is 2 to 32 characters")
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return "", errors.New("a username uses only letters, digits and _")
		}
	}
	return s, nil
}

// CleanName strips characters that let a name disguise itself or reorder
// the text around it: control and format characters, which include the
// bidi overrides and the zero-width ones, and the fillers that render as
// blanks. It trims what's left, and checks it is 1 to max characters with
// something visible among them, so a name can't pass for nobody's (M8).
func CleanName(s string, max int) (string, error) {
	if !utf8.ValidString(s) {
		return "", errors.New("names must be valid UTF-8")
	}
	s = strings.Map(func(r rune) rune {
		if hiddenRune(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if n := utf8.RuneCountInString(s); n == 0 || n > max {
		return "", fmt.Errorf("a name is 1 to %d characters", max)
	}
	if strings.IndexFunc(s, visibleRune) < 0 {
		return "", errors.New("a name needs a letter, digit, punctuation mark or symbol")
	}
	return s, nil
}

func hiddenRune(r rune) bool {
	switch {
	case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		return true // bidi embeddings, overrides, isolates and marks, and zero-width characters among them
	case r == 0x115F, r == 0x1160, r == 0x3164, r == 0xFFA0:
		return true // Hangul fillers, letters that render as blanks
	case r == 0x2800:
		return true // the blank Braille pattern
	}
	return false
}

// visibleRune reports whether a rune shows something: a letter, digit,
// punctuation or symbol, where a mark only changes the rune before it.
func visibleRune(r rune) bool {
	return unicode.In(r, unicode.L, unicode.N, unicode.P, unicode.S)
}

// NormalizeDenURL checks a den's address and returns it without a trailing
// slash. A den lives at the root of an https origin; plain http is allowed
// only on loopback, for instances on the same machine.
func NormalizeDenURL(raw string) (string, error) {
	if len(raw) > maxURLLength {
		return "", errors.New("the den URL is too long")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return "", errors.New("a den URL looks like https://den.example.com")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !IsLoopbackHost(u.Hostname()) {
			return "", errors.New("a den URL must use https unless it is on this computer")
		}
	default:
		return "", errors.New("a den URL must use https")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// IsLoopbackHost reports whether host names this machine.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Invite is a decoded invite string.
type InviteString struct {
	DenID Bytes
	Code  Bytes
	URL   string
}

// EncodeInvite builds "dens1:" + base64url(den_id ‖ code ‖ url).
func EncodeInvite(denID, code []byte, denURL string) string {
	return invitePrefix + base64.RawURLEncoding.EncodeToString(join(denID, code, []byte(denURL)))
}

// DecodeInvite parses and checks an invite string.
func DecodeInvite(s string) (InviteString, error) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, invitePrefix)
	if !ok {
		return InviteString{}, errors.New("that isn't a Dens invite (they start with dens1:)")
	}
	raw, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil || len(raw) <= IDSize+InviteCodeSize {
		return InviteString{}, errors.New("that invite is damaged; ask for it again")
	}
	denURL, err := NormalizeDenURL(string(raw[IDSize+InviteCodeSize:]))
	if err != nil {
		return InviteString{}, fmt.Errorf("that invite's den address is invalid: %w", err)
	}
	return InviteString{
		DenID: Bytes(raw[:IDSize]),
		Code:  Bytes(raw[IDSize : IDSize+InviteCodeSize]),
		URL:   denURL,
	}, nil
}
