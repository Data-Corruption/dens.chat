package denproto

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	msg := DenChallengeMessage(keyID, nonce, "https://den.test")
	if !bytes.HasPrefix(msg, []byte("dens-den-v1")) || !bytes.HasSuffix(msg, []byte("https://den.test")) ||
		len(msg) != len("dens-den-v1")+64+len("https://den.test") {
		t.Fatalf("den challenge layout: %x", msg)
	}
}

func TestVerifyDen(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	clientNonce, nonce := Random(NonceSize), Random(NonceSize)
	answer := func(url string) ChallengeResponse {
		return ChallengeResponse{Nonce: nonce, DenKey: Bytes(pub), URL: url, DenSig: ed25519.Sign(priv, DenChallengeMessage(clientNonce, nonce, url))}
	}
	resp := answer("https://den.test")
	if id, err := VerifyDen(ID(pub), clientNonce, resp); err != nil || !bytes.Equal(id, ID(pub)) {
		t.Fatal(err)
	}
	// Signing in by address pins nothing and learns the den's ID.
	if id, err := VerifyDen(nil, clientNonce, resp); err != nil || !bytes.Equal(id, ID(pub)) {
		t.Fatal(err)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, err := VerifyDen(ID(other), clientNonce, resp); err == nil {
		t.Fatal("accepted a den whose key isn't the pinned one")
	}
	if _, err := VerifyDen(ID(pub), Random(NonceSize), resp); err == nil {
		t.Fatal("accepted a signature over another client nonce")
	}
	forged := resp
	forged.URL = "https://old.test"
	if _, err := VerifyDen(ID(pub), clientNonce, forged); err == nil {
		t.Fatal("accepted a signature over another address")
	}
	if _, err := VerifyDen(ID(pub), clientNonce, answer("https://Den.Test/")); err == nil {
		t.Fatal("accepted an address that isn't in its normal form")
	}
}

// A relay at an old address passes on the den's answer, signed with the
// den's own address, which doesn't match where the client reached it.
func TestCheckDenURL(t *testing.T) {
	if err := CheckDenURL("https://den.test", "https://DEN.test/"); err != nil {
		t.Fatalf("the same address: %v", err)
	}
	var moved *MovedError
	if err := CheckDenURL("https://new.test", "https://old.test"); !errors.As(err, &moved) || moved.URL != "https://new.test" {
		t.Fatalf("another address: %v", err)
	}
	if err := CheckDenURL("https://den.test", "https://den.test:8443"); !errors.As(err, &moved) {
		t.Fatalf("another port: %v", err)
	}
}

func TestFingerprint(t *testing.T) {
	id := bytes.Repeat([]byte{0xAB}, IDSize)
	fp := Fingerprint(id)
	if len(fp) != 19 || strings.Count(fp, "-") != 3 {
		t.Fatalf("fingerprint %q", fp)
	}
	if code, err := ParseRecoveryCode(fp); err != nil || !bytes.Equal(code, id[:RecoveryCodeSize]) {
		t.Fatalf("the fingerprint doesn't read back: %v", err)
	}
	if Fingerprint(nil) != "" {
		t.Fatal("a missing ID has a fingerprint")
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

// The page's renderer shows task lines as checkboxes by the same rule, and
// reads the same cases.
//
//go:embed testdata/tasks.json
var taskCases []byte

func TestTasks(t *testing.T) {
	var file struct {
		Cases []struct {
			Text  string `json:"text"`
			Tasks []Task `json:"tasks"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(taskCases, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no task cases")
	}
	for _, c := range file.Cases {
		if got := Tasks(c.Text); !slices.Equal(got, c.Tasks) {
			t.Errorf("Tasks(%q) = %+v, want %+v", c.Text, got, c.Tasks)
		}
	}
}

func TestSetTask(t *testing.T) {
	text := "list:\n```\n[ ] not a task\n```\n[ ] milk\n[x] eggs\n[ ] bread"
	got, ok := SetTask(text, 2, true)
	if want := "list:\n```\n[ ] not a task\n```\n[ ] milk\n[x] eggs\n[x] bread"; !ok || got != want {
		t.Fatalf("checking task 2: %q %v", got, ok)
	}
	if got, ok = SetTask(got, 1, false); !ok || got != "list:\n```\n[ ] not a task\n```\n[ ] milk\n[ ] eggs\n[x] bread" {
		t.Fatalf("unchecking task 1: %q %v", got, ok)
	}
	if again, ok := SetTask(got, 1, false); !ok || again != got {
		t.Fatalf("unchecking it again: %q %v", again, ok)
	}
	for _, n := range []int{-1, 3} {
		if same, ok := SetTask(text, n, true); ok || same != text {
			t.Errorf("SetTask(%d) = %q %v", n, same, ok)
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
	m.Reply = &Reply{AuthorID: "3"}
	if CheckMessage(m) != nil {
		t.Error("CheckMessage refused a reply to a message with only files")
	}
	m.ReplyTo, m.Reply = "", &Reply{AuthorID: "3", Text: "hi"}
	if CheckMessage(m) == nil {
		t.Error("CheckMessage accepted a reply preview without reply_to")
	}
	m.Reply = nil
	m.Editors = []string{"4", "6"}
	if err := CheckMessage(m); err != nil {
		t.Errorf("a message with editors: %v", err)
	}
	for _, bad := range [][]string{{"x"}, {"3"}, {"4", "4"}, make([]string, MaxEditors+1)} {
		if bad[0] == "" {
			for i := range bad {
				bad[i] = FormatID(int64(i + 10))
			}
		}
		m.Editors = bad
		if CheckMessage(m) == nil {
			t.Errorf("CheckMessage accepted the editors %q", bad)
		}
	}
}

func TestFileChecks(t *testing.T) {
	photo := File{ID: "7", Name: "cat.jpg", Type: "image/jpeg", Size: 1234, Width: 800, Height: 600, Thumb: &Thumb{Width: 640, Height: 480}}
	m := Message{ID: "5", ChannelID: "2", AuthorID: "3", Revision: 1, Attachments: []File{photo}}
	if err := CheckMessage(m); err != nil {
		t.Errorf("a message with only a photo: %v", err)
	}
	m.Attachments = nil
	if CheckMessage(m) == nil {
		t.Error("CheckMessage accepted a message with neither text nor files")
	}
	m.Attachments = []File{photo, photo}
	if CheckMessage(m) == nil {
		t.Error("CheckMessage accepted a file twice")
	}
	m.Attachments = make([]File, MaxAttachments+1)
	for i := range m.Attachments {
		m.Attachments[i] = photo
		m.Attachments[i].ID = FormatID(int64(i + 1))
	}
	if CheckMessage(m) == nil {
		t.Error("CheckMessage accepted too many files")
	}
	for _, bad := range []File{
		{ID: "x", Name: "a", Type: "text/plain"},
		{ID: "1", Name: "", Type: "text/plain"},
		{ID: "1", Name: "../etc/passwd", Type: "text/plain"},
		{ID: "1", Name: "a‮gnp.exe", Type: "text/plain"},
		{ID: "1", Name: "a", Type: "text/html; charset=utf-8"},
		{ID: "1", Name: "a", Type: "Image/PNG"},
		{ID: "1", Name: "a", Type: "text/plain", Size: -1},
		{ID: "1", Name: "a", Type: "image/png", Width: 10},
		{ID: "1", Name: "a", Type: "image/png", Width: 10, Height: 10, Thumb: &Thumb{Width: 0, Height: 5}},
		{ID: "1", Name: "a", Type: "text/plain", Thumb: &Thumb{Width: 5, Height: 5}},
	} {
		if CheckFile(bad) == nil {
			t.Errorf("CheckFile accepted %+v", bad)
		}
	}
	for in, want := range map[string]string{
		"photo.jpg":               "photo.jpg",
		"../../etc/passwd":        "_.._etc_passwd",
		"C:\\Windows\\evil.exe":   "C:_Windows_evil.exe",
		"a‮gnp.exe":               "agnp.exe",
		"  spaced   out  .txt ":   "spaced out .txt",
		"...":                     "file",
		"":                        "file",
		"\x00\x01":                "file",
		strings.Repeat("é", 200):  strings.Repeat("é", 127),
		"[strangly named 𓂃꙳⋆.jpg": "[strangly named 𓂃꙳⋆.jpg",
	} {
		if got := CleanFilename(in); got != want {
			t.Errorf("CleanFilename(%q) = %q, want %q", in, got, want)
		}
	}
	if CheckLimits(Limits{FileSize: DefaultFileSize, MemberStorage: DefaultMemberStorage, DenStorage: DefaultDenStorage}) != nil {
		t.Error("the default limits don't check")
	}
	for _, bad := range []Limits{
		{FileSize: 10, MemberStorage: 1 << 30, DenStorage: 1 << 31},
		{FileSize: 2 << 30, MemberStorage: 4 << 30, DenStorage: 8 << 30},
		{FileSize: 10 << 20, MemberStorage: 5 << 20, DenStorage: 1 << 30},
		{FileSize: 10 << 20, MemberStorage: 1 << 30, DenStorage: 5 << 20},
	} {
		if CheckLimits(bad) == nil {
			t.Errorf("CheckLimits accepted %+v", bad)
		}
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
