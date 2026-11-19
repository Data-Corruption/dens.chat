package denproto

import (
	_ "embed"
	"encoding/json"
	"testing"
)

// The page reads sign-in codes from the same file, so both agree on every
// case.
//
//go:embed testdata/signincodes.json
var signInCodeCases []byte

type signInCodeCase struct {
	Text     string `json:"text"`
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Digits   string `json:"digits"`
	Why      string `json:"why"`
}

func readSignInCodeCases(t testing.TB) []signInCodeCase {
	var file struct {
		Cases []signInCodeCase `json:"cases"`
	}
	if err := json.Unmarshal(signInCodeCases, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no sign-in code cases")
	}
	return file.Cases
}

func TestReadSignInCode(t *testing.T) {
	for _, c := range readSignInCodeCases(t) {
		got, ok := ReadSignInCode(c.Text)
		want := SignInCode{Kind: c.Kind, URL: c.URL, Username: c.Username, Digits: c.Digits}
		if ok != (c.Kind != "") || got != want {
			t.Errorf("ReadSignInCode(%q) = %+v, %v; want %+v (%s)", c.Text, got, ok, want, c.Why)
		}
	}
}

// TestSignInCodesMade checks that each code that reads is the one Dens
// makes for what it holds, and that its address is a den's, as signing in
// takes it.
func TestSignInCodesMade(t *testing.T) {
	for _, c := range readSignInCodeCases(t) {
		switch c.Kind {
		case SignInDevice:
			if text, err := DeviceCode(c.URL, c.Username); err != nil || text != c.Text {
				t.Errorf("DeviceCode(%q, %q) = %q, %v; want %q", c.URL, c.Username, text, err, c.Text)
			}
			if url, err := NormalizeDenURL(c.URL); err != nil || url != c.URL {
				t.Errorf("NormalizeDenURL(%q) = %q, %v; a code's address must be one as it is", c.URL, url, err)
			}
			if name, err := NormalizeUsername(c.Username); err != nil || name != c.Username {
				t.Errorf("NormalizeUsername(%q) = %q, %v", c.Username, name, err)
			}
		case SignInDigits:
			if text, err := DigitsCode(c.Digits); err != nil || text != c.Text {
				t.Errorf("DigitsCode(%q) = %q, %v; want %q", c.Digits, text, err, c.Text)
			}
		}
	}
}

func TestSignInCodesRefused(t *testing.T) {
	for _, bad := range [][2]string{
		{"https://Den.example.com", "bob"},
		{"https://den.example.com/", "bob"},
		{"http://den.example.com", "bob"},
		{"https://den.example.com", "Bob"},
		{"https://den.example.com", "bob alice"},
	} {
		if text, err := DeviceCode(bad[0], bad[1]); err == nil {
			t.Errorf("DeviceCode(%q, %q) = %q; want an error", bad[0], bad[1], text)
		}
	}
	for _, bad := range []string{"", "1234 5678 9012 3456", "123456789012345", "12345678901234567"} {
		if text, err := DigitsCode(bad); err == nil {
			t.Errorf("DigitsCode(%q) = %q; want an error", bad, text)
		}
	}
}

// A code read from a frame of a camera is whatever someone showed it.
func FuzzReadSignInCode(f *testing.F) {
	for _, c := range readSignInCodeCases(f) {
		f.Add(c.Text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		c, ok := ReadSignInCode(text)
		switch {
		case !ok:
			if c != (SignInCode{}) {
				t.Fatalf("refused %q but read %+v", text, c)
			}
		case c.Kind == SignInDigits:
			if text != c.Digits || len(c.Digits) != 16 {
				t.Fatalf("read %q as digits %q", text, c.Digits)
			}
		default:
			if url, err := NormalizeDenURL(c.URL); err != nil || url != c.URL {
				t.Fatalf("read %q as address %q, which isn't a den's: %v", text, c.URL, err)
			}
			if again, err := DeviceCode(c.URL, c.Username); err != nil || again != text {
				t.Fatalf("read %q but made %q", text, again)
			}
		}
	})
}
