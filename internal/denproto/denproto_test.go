package denproto

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The verifier is part of the protocol: a client and a den that disagree on
// it can never log in. This vector pins the derivation; it matches the
// reference Argon2 implementation (argon2-cffi) given the same inputs.
func TestVerifierVector(t *testing.T) {
	denID := bytes.Repeat([]byte{0x11}, IDSize)
	got := hex.EncodeToString(Verifier("correct horse battery staple", denID, "alice"))
	const want = "94474efdc9b362e2cbd45371ef6b1e2d55ecf7d25c927329c1a04c2cf6a88dbb"
	if got != want {
		t.Fatalf("Verifier = %s, want %s", got, want)
	}
	if bytes.Equal(Verifier("correct horse battery staple", denID, "bob"), Verifier("correct horse battery staple", denID, "alice")) {
		t.Fatal("the username doesn't salt the verifier")
	}
	other := bytes.Repeat([]byte{0x22}, IDSize)
	if bytes.Equal(Verifier("pw", denID, "alice"), Verifier("pw", other, "alice")) {
		t.Fatal("the den doesn't salt the verifier")
	}
}

func TestSignedLayouts(t *testing.T) {
	denID := bytes.Repeat([]byte{1}, IDSize)
	keyID := bytes.Repeat([]byte{2}, IDSize)
	nonce := bytes.Repeat([]byte{3}, NonceSize)
	proof := ProofMessage(denID, keyID, nonce)
	if !bytes.HasPrefix(proof, []byte("dens-auth-v1")) || len(proof) != len("dens-auth-v1")+3*32 {
		t.Fatalf("proof message layout: %x", proof)
	}
	msg := DenChallengeMessage(keyID, nonce)
	if !bytes.HasPrefix(msg, []byte("dens-den-v1")) || len(msg) != len("dens-den-v1")+64 {
		t.Fatalf("den challenge layout: %x", msg)
	}
}

func TestVerifyDen(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	clientNonce, nonce := Random(NonceSize), Random(NonceSize)
	resp := ChallengeResponse{Nonce: nonce, DenKey: Bytes(pub), DenSig: ed25519.Sign(priv, DenChallengeMessage(clientNonce, nonce))}
	if err := VerifyDen(ID(pub), clientNonce, resp); err != nil {
		t.Fatal(err)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if VerifyDen(ID(other), clientNonce, resp) == nil {
		t.Fatal("accepted a den whose key isn't the pinned one")
	}
	if VerifyDen(ID(pub), Random(NonceSize), resp) == nil {
		t.Fatal("accepted a signature over another client nonce")
	}
}

func TestInviteRoundTrip(t *testing.T) {
	denID, code := Random(IDSize), Random(InviteCodeSize)
	s := EncodeInvite(denID, code, "https://den.example.com")
	got, err := DecodeInvite("  " + s + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.DenID, denID) || !bytes.Equal(got.Code, code) || got.URL != "https://den.example.com" {
		t.Fatalf("decoded %+v", got)
	}
	for name, bad := range map[string]string{
		"no prefix":  strings.TrimPrefix(s, "dens1:"),
		"truncated":  s[:30],
		"http URL":   EncodeInvite(denID, code, "http://den.example.com"),
		"not base64": "dens1:!!!",
	} {
		if _, err := DecodeInvite(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNormalizeDenURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://Den.Example.com/":  "https://den.example.com",
		"https://den.test:8443":     "https://den.test:8443",
		"http://127.0.0.1:8485":     "http://127.0.0.1:8485",
		"http://[::1]:8485/":        "http://[::1]:8485",
		"http://localhost:8485":     "http://localhost:8485",
		" https://den.example.com ": "https://den.example.com",
	} {
		if got, err := NormalizeDenURL(in); err != nil || got != want {
			t.Errorf("NormalizeDenURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"http://den.example.com", "https://den.example.com/path", "https://u:p@den.example.com",
		"https://den.example.com/?q=1", "ftp://den.example.com", "den.example.com", "https://",
	} {
		if _, err := NormalizeDenURL(bad); err == nil {
			t.Errorf("NormalizeDenURL(%q) accepted", bad)
		}
	}
}

func TestNames(t *testing.T) {
	if u, err := NormalizeUsername("  Alice_1 "); err != nil || u != "alice_1" {
		t.Fatalf("NormalizeUsername = %q, %v", u, err)
	}
	for _, bad := range []string{"a", strings.Repeat("a", 33), "al ice", "élan", "al-ice"} {
		if _, err := NormalizeUsername(bad); err == nil {
			t.Errorf("NormalizeUsername(%q) accepted", bad)
		}
	}
	if got, err := CleanName("  Al‮ice ​the   Great\n", MaxNameRunes); err != nil || got != "Alice the Great" {
		t.Fatalf("CleanName = %q, %v", got, err)
	}
	if got, err := CleanName("Zoë 🦊", MaxNameRunes); err != nil || got != "Zoë 🦊" {
		t.Fatalf("CleanName kept %q, %v", got, err)
	}
	for _, bad := range []string{"", "​‮", strings.Repeat("x", 33), "\xff"} {
		if _, err := CleanName(bad, MaxNameRunes); err == nil {
			t.Errorf("CleanName(%q) accepted", bad)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	code := Random(RecoveryCodeSize)
	shown := FormatRecoveryCode(code)
	if len(shown) != 19 || strings.Count(shown, "-") != 3 {
		t.Fatalf("formatted %q", shown)
	}
	for _, typed := range []string{shown, strings.ToLower(shown), strings.ReplaceAll(shown, "-", " ")} {
		got, err := ParseRecoveryCode(typed)
		if err != nil || !bytes.Equal(got, code) {
			t.Errorf("ParseRecoveryCode(%q) = %x, %v", typed, got, err)
		}
	}
	if _, err := ParseRecoveryCode("ABCD-EFGH"); err == nil {
		t.Error("accepted a short code")
	}
}

func TestFrames(t *testing.T) {
	e1, _ := NewEvent(EventDenUpdated, 7, Den{Name: "x"})
	e2, _ := NewEvent(EventAuthRenewed, 0, Renewed{ExpiresAt: 5})
	frame, err := EncodeFrame(e1, e2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(frame), `[{"t":"den.updated","seq":7,`) || strings.Contains(string(frame), `"auth.renewed","seq"`) {
		t.Fatalf("frame %s", frame)
	}
	events, err := DecodeFrame(frame)
	if err != nil || len(events) != 2 || events[0].Seq != 7 || events[1].Seq != 0 {
		t.Fatalf("decoded %+v, %v", events, err)
	}
	lo, hi, err := ParseRange(RangeHeader())
	if err != nil || lo != MinVersion || hi != Version {
		t.Fatalf("range %d-%d, %v", lo, hi, err)
	}
}

// The page highlights mentions by the same rule, and its tests read the
// same cases. They're embedded, since the Windows test runner doesn't run
// from the package directory.
//
//go:embed testdata/mentions.json
var mentionCases []byte

func TestMentions(t *testing.T) {
	var file struct {
		Cases []struct {
			Text     string   `json:"text"`
			Mentions []string `json:"mentions"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(mentionCases, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no mention cases")
	}
	for _, c := range file.Cases {
		if got := Mentions(c.Text); !slices.Equal(got, c.Mentions) {
			t.Errorf("Mentions(%q) = %q, want %q", c.Text, got, c.Mentions)
		}
	}
}

func TestChecks(t *testing.T) {
	for _, bad := range []string{"", "0", "01", "-1", "1a", "99999999999999999999"} {
		if _, err := ParseID(bad); err == nil {
			t.Errorf("ParseID(%q) accepted", bad)
		}
	}
	if id, err := ParseID("1000123"); err != nil || id != 1000123 {
		t.Fatalf("ParseID = %d, %v", id, err)
	}
	for _, bad := range []string{"", "   \n", "\xff", strings.Repeat("x", MaxTextRunes+1)} {
		if CheckText(bad) == nil {
			t.Errorf("CheckText accepted %q", bad[:min(len(bad), 10)])
		}
	}
	if CheckText(strings.Repeat("é", MaxTextRunes)) != nil {
		t.Error("CheckText refused the longest message")
	}
	m := Message{ID: "5", ChannelID: "2", AuthorID: "3", Revision: 1, Text: "hi", ReplyTo: "x"}
	if CheckMessage(m) == nil {
		t.Error("CheckMessage accepted a bad reply_to")
	}
	m.ReplyTo = "4"
	for _, bad := range []Reply{{AuthorID: "x", Text: "hi"}, {AuthorID: "3", Text: " "}, {AuthorID: "3", Text: strings.Repeat("x", ReplyExcerpt+1)}} {
		m.Reply = &bad
		if CheckMessage(m) == nil {
			t.Errorf("CheckMessage accepted the reply %+v", bad)
		}
	}
	m.ReplyTo, m.Reply = "", &Reply{AuthorID: "3", Text: "hi"}
	if CheckMessage(m) == nil {
		t.Error("CheckMessage accepted a reply preview without reply_to")
	}
}

func TestExcerpt(t *testing.T) {
	long := strings.Repeat("é", ReplyExcerpt)
	for text, want := range map[string]string{
		"hi":                 "hi",
		"\n\n  hello\nworld": "hello\nworld",
		long + "and more":    long,
	} {
		if got := Excerpt(text); got != want {
			t.Errorf("Excerpt(%q) = %q, want %q", text, got, want)
		}
	}
}
