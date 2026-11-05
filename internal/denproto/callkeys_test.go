package denproto

import (
	"bytes"
	"encoding/hex"
	"slices"
	"testing"
)

// A call key's copy opens only with the identity it was sealed to, and
// only for the den, the call, the key and the member it was sealed for;
// a DM key's copy is no call key's.
func TestCallKeyCopies(t *testing.T) {
	den := bytes.Repeat([]byte{9}, IDSize)
	alice, err := DeriveIdentity(fixedSeal(7), den)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	bob, err := DeriveIdentity(fixedSeal(8), den)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	key, kid := Random(CallKeySize), Random(KIDSize)
	copied, err := SealCallKeyCopy(bob.Public(), den, 5, kid, 2, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != CallKeyCopySize {
		t.Fatalf("a call key's copy is %d bytes", len(copied))
	}
	if got, err := bob.OpenCallKeyCopy(den, 5, kid, 2, copied); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("bob's copy opened to %x, %v", got, err)
	}
	for what, open := range map[string]func() ([]byte, error){
		"another identity": func() ([]byte, error) { return alice.OpenCallKeyCopy(den, 5, kid, 2, copied) },
		"another call":     func() ([]byte, error) { return bob.OpenCallKeyCopy(den, 6, kid, 2, copied) },
		"another key":      func() ([]byte, error) { return bob.OpenCallKeyCopy(den, 5, Random(KIDSize), 2, copied) },
		"another member":   func() ([]byte, error) { return bob.OpenCallKeyCopy(den, 5, kid, 3, copied) },
		"another den": func() ([]byte, error) {
			return bob.OpenCallKeyCopy(bytes.Repeat([]byte{10}, IDSize), 5, kid, 2, copied)
		},
		"a changed copy": func() ([]byte, error) {
			changed := slices.Clone(copied)
			changed[len(changed)-1] ^= 1
			return bob.OpenCallKeyCopy(den, 5, kid, 2, changed)
		},
		"a DM's opening": func() ([]byte, error) { return bob.OpenKeyCopy(den, 5, kid, 2, copied) },
	} {
		if _, err := open(); err == nil {
			t.Errorf("a call key's copy opened with %s", what)
		}
	}
	dm, err := SealKeyCopy(bob.Public(), den, 5, kid, 2, Random(DMKeySize))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.OpenCallKeyCopy(den, 5, kid, 2, dm); err == nil {
		t.Error("a DM key's copy opened as a call key's")
	}
	for what, seal := range map[string]func() (Bytes, error){
		"a DM's key":           func() (Bytes, error) { return SealCallKeyCopy(bob.Public(), den, 5, kid, 2, Random(DMKeySize)) },
		"a short ID":           func() (Bytes, error) { return SealCallKeyCopy(bob.Public(), den, 5, kid[:8], 2, key) },
		"a malformed identity": func() (Bytes, error) { return SealCallKeyCopy(Identity{KEM: Random(10)}, den, 5, kid, 2, key) },
	} {
		if _, err := seal(); err == nil {
			t.Errorf("%s was sealed as a call's key", what)
		}
	}
	// The commitment names the key and its ID, under a context of its own.
	if bytes.Equal(CallKeyCommit(kid, key), CallKeyCommit(kid, Random(CallKeySize))) ||
		bytes.Equal(CallKeyCommit(kid, key), CallKeyCommit(Random(KIDSize), key)) || bytes.Equal(CallKeyCommit(kid, key), KeyCommit(kid, key)) {
		t.Error("a call key's commitment doesn't name its key and its ID alone")
	}
}

// A call key's statement checks only against its maker's signing key, as
// it was signed, and never as a DM key's. A fixed case: its form is what
// every member's Dens signs and checks.
func TestCallKeyStatements(t *testing.T) {
	den := bytes.Repeat([]byte{9}, IDSize)
	alice, _ := DeriveIdentity(fixedSeal(7), den)
	bob, _ := DeriveIdentity(fixedSeal(8), den)
	carol, _ := DeriveIdentity(fixedSeal(9), den)
	defer alice.Close()
	defer bob.Close()
	defer carol.Close()
	kid := bytes.Repeat([]byte{0x01}, KIDSize)
	key := bytes.Repeat([]byte{0x02}, CallKeySize)
	s := CallKeyStatement{DenID: den, Channel: 5, KID: kid, Commit: CallKeyCommit(kid, key), MadeBy: 1,
		Members: []StatedMember{{2, bob.Public().Hash()}, {3, carol.Public().Hash()}}}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	sig := alice.SignCallKey(s)
	if got := hex.EncodeToString(sig); got != "833633b188832e34920835a2541aac95bf2ab08f6e98418c417b7d660d30af17d2ac0731afeffabedf295a8c8ffd219a64976783b033080a7f6ea44c6fbc0f0d" {
		t.Errorf("alice's signature is %s", got)
	}
	if !VerifyCallKey(alice.Public().Sig, s, sig) {
		t.Fatal("alice's statement doesn't check against her key")
	}
	if VerifyCallKey(bob.Public().Sig, s, sig) {
		t.Error("alice's statement checks against bob's key")
	}
	if !bytes.Equal(s.Named(3), carol.Public().Hash()) || s.Named(1) != nil || s.Named(4) != nil {
		t.Error("the statement names its members' identities oddly")
	}
	// The same fields, signed as a DM's key, check as no call's, and the
	// other way round.
	dm := KeyStatement{DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 1,
		Members: []StatedMember{{1, alice.Public().Hash()}, {2, bob.Public().Hash()}}}
	call := CallKeyStatement{DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 1, Members: dm.Members[1:]}
	if VerifyCallKey(alice.Public().Sig, call, alice.Sign(dm)) || VerifyStatement(alice.Public().Sig, dm, alice.SignCallKey(call)) {
		t.Error("a DM key's statement passes for a call key's, or the other way round")
	}
	changes := map[string]func(*CallKeyStatement){
		"another den":      func(s *CallKeyStatement) { s.DenID = bytes.Repeat([]byte{10}, IDSize) },
		"another call":     func(s *CallKeyStatement) { s.Channel = 6 },
		"another key":      func(s *CallKeyStatement) { s.KID = Random(KIDSize) },
		"another commit":   func(s *CallKeyStatement) { s.Commit = CallKeyCommit(s.KID, Random(CallKeySize)) },
		"another maker":    func(s *CallKeyStatement) { s.MadeBy = 4 },
		"another identity": func(s *CallKeyStatement) { s.Members[1].Identity = alice.Public().Hash() },
		"another member":   func(s *CallKeyStatement) { s.Members[1].Member = 4 },
		"a member left out": func(s *CallKeyStatement) {
			s.Members = s.Members[:1]
		},
	}
	for what, change := range changes {
		c := s
		c.Members = slices.Clone(s.Members)
		change(&c)
		if VerifyCallKey(alice.Public().Sig, c, sig) {
			t.Errorf("the signature checks for %s", what)
		}
	}
	for what, bad := range map[string]CallKeyStatement{
		"members out of order": {DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 1,
			Members: []StatedMember{s.Members[1], s.Members[0]}},
		"its maker among them": {DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 2, Members: s.Members},
		"no members":           {DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 1},
		"a short ID":           {DenID: den, Channel: 5, KID: kid[:8], Commit: s.Commit, MadeBy: 1, Members: s.Members},
		"no channel":           {DenID: den, KID: kid, Commit: s.Commit, MadeBy: 1, Members: s.Members},
	} {
		if bad.Check() == nil {
			t.Errorf("a statement with %s checks out", what)
		}
	}
	if CallKeyNumber([]byte{0x01, 0x23, 0xff}) != 0x0123 || CallKeyNumber(nil) != 0 {
		t.Error("a key's number isn't its ID's first two bytes")
	}
}

// What the den takes of a member's key, and what a member's Dens takes of
// another's: their shapes.
func TestVoiceKeyShapes(t *testing.T) {
	den := bytes.Repeat([]byte{9}, IDSize)
	alice, _ := DeriveIdentity(fixedSeal(7), den)
	bob, _ := DeriveIdentity(fixedSeal(8), den)
	defer alice.Close()
	defer bob.Close()
	key, kid := Random(CallKeySize), Random(KIDSize)
	copied, err := SealCallKeyCopy(bob.Public(), den, 5, kid, 2, key)
	if err != nil {
		t.Fatal(err)
	}
	k := VoiceKey{ChannelID: "5", KID: kid, Commit: CallKeyCommit(kid, key), Members: []KeyMember{{MemberID: "2", Identity: bob.Public().Hash()}},
		Maker: KeyMaker{KEMHash: KEMHash(alice.Public().KEM), Sig: alice.Public().Sig}, Copies: []KeyCopy{{MemberID: "2", Copy: copied}}}
	s, err := k.Statement(den, "1")
	if err != nil {
		t.Fatal(err)
	}
	k.Signature = alice.SignCallKey(s)
	if err := CheckVoiceKey(den, "1", k); err != nil {
		t.Fatal(err)
	}
	e := k.Event("1", k.Copy("2"))
	if err := CheckVoiceKeyEvent(den, e); err != nil {
		t.Fatal(err)
	}
	es, _ := e.Statement(den)
	if !VerifyCallKey(e.Maker.Sig, es, e.Signature) || k.Copy("3") != nil {
		t.Error("the key as the den passes it doesn't check as its maker signed it")
	}
	for what, change := range map[string]func(*VoiceKey){
		"no copy":              func(k *VoiceKey) { k.Copies = nil },
		"a copy for no member": func(k *VoiceKey) { k.Copies = []KeyCopy{{MemberID: "3", Copy: copied}} },
		"two copies for one": func(k *VoiceKey) {
			k.Members = append(k.Members, KeyMember{MemberID: "3", Identity: alice.Public().Hash()})
			k.Copies = append(k.Copies, k.Copies[0])
		},
		"a short copy":         func(k *VoiceKey) { k.Copies = []KeyCopy{{MemberID: "2", Copy: copied[:100]}} },
		"a short signature":    func(k *VoiceKey) { k.Signature = k.Signature[:10] },
		"a malformed maker":    func(k *VoiceKey) { k.Maker.Sig = nil },
		"a channel that isn't": func(k *VoiceKey) { k.ChannelID = "lounge" },
		"its maker among them": func(k *VoiceKey) {
			k.Members = []KeyMember{{MemberID: "1", Identity: alice.Public().Hash()}}
			k.Copies = []KeyCopy{{MemberID: "1", Copy: copied}}
		},
	} {
		c := k
		c.Members, c.Copies = slices.Clone(k.Members), slices.Clone(k.Copies)
		change(&c)
		if CheckVoiceKey(den, "1", c) == nil {
			t.Errorf("the den takes a key with %s", what)
		}
	}
	for what, change := range map[string]func(*VoiceKeyEvent){
		"no copy":      func(e *VoiceKeyEvent) { e.Copy = nil },
		"no maker":     func(e *VoiceKeyEvent) { e.MemberID = "" },
		"no signature": func(e *VoiceKeyEvent) { e.Signature = nil },
	} {
		c := e
		change(&c)
		if CheckVoiceKeyEvent(den, c) == nil {
			t.Errorf("a Dens takes a key with %s", what)
		}
	}
}
