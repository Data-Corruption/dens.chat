package denproto

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Sign-in codes (M22.6): what a new device scans in place of typing. Add
// device's names a den's address and the member's username there; the
// digits' is the approving device's half of the check code. Each holds
// only what its screen shows beside it. A scanned code is hostile: it
// reads as one of the two, by a grammar stricter than what's typed, or as
// nothing. The page reads them by the same rules (signincode.js), and
// both test against testdata/signincodes.json.

// Kinds of sign-in code.
const (
	SignInDevice = "device"
	SignInDigits = "digits"
)

const deviceCodePrefix = "dens-device:"

// SignInCode is a sign-in code read: a den's address and a username, or
// 16 digits.
type SignInCode struct {
	Kind     string
	URL      string
	Username string
	Digits   string
}

var (
	codeAddress  = regexp.MustCompile(`^(https?)://(\[::1\]|[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*)(?::([1-9][0-9]{0,4}))?$`)
	codeLoopback = regexp.MustCompile(`^(?:localhost|\[::1\]|127(?:\.(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])){3})$`)
	codeUsername = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)
	codeDigits   = regexp.MustCompile(`^[0-9]{16}$`)
)

// ReadSignInCode reads a scanned code's text as one of the two kinds.
func ReadSignInCode(text string) (SignInCode, bool) {
	if codeDigits.MatchString(text) {
		return SignInCode{Kind: SignInDigits, Digits: text}, true
	}
	rest, ok := strings.CutPrefix(text, deviceCodePrefix)
	if !ok {
		return SignInCode{}, false
	}
	address, username, ok := strings.Cut(rest, " ")
	if !ok || !codeUsername.MatchString(username) || !denAddress(address) {
		return SignInCode{}, false
	}
	return SignInCode{Kind: SignInDevice, URL: address, Username: username}, true
}

// denAddress reports whether a code's address is one: https, or plain
// http on this computer, a host name or IPv4 address in lowercase, or
// [::1], maybe a port, and nothing after it.
func denAddress(s string) bool {
	m := codeAddress.FindStringSubmatch(s)
	if m == nil || len(s) > maxURLLength || len(m[2]) > 253 {
		return false
	}
	if m[3] != "" {
		if port, err := strconv.Atoi(m[3]); err != nil || port > 65535 {
			return false
		}
	}
	return m[1] == "https" || codeLoopback.MatchString(m[2])
}

// DeviceCode is Add device's code for a den's address and the member's
// username there, which must read back as they are.
func DeviceCode(url, username string) (string, error) {
	text := deviceCodePrefix + url + " " + username
	if c, ok := ReadSignInCode(text); !ok || c.URL != url || c.Username != username {
		return "", errors.New("that den's address doesn't fit in a code")
	}
	return text, nil
}

// DigitsCode is the code for a device's half of a check code.
func DigitsCode(half string) (string, error) {
	if !codeDigits.MatchString(half) {
		return "", errors.New("a code's digits are 16 of them")
	}
	return half, nil
}
