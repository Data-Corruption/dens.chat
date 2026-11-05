package denproto

import (
	"bytes"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
)

func fixedSeal(b byte) []byte { return bytes.Repeat([]byte{b}, SealSize) }

// An identity comes from the seal and the den's ID alone, so every device
// holding the seal makes the same one, and a den sees nothing of what the
// member is at another. These are fixed cases: a change to how identities
// are made changes every member's code.
func TestIdentityFromSeal(t *testing.T) {
	den, other := bytes.Repeat([]byte{9}, IDSize), bytes.Repeat([]byte{10}, IDSize)
	a, err := DeriveIdentity(fixedSeal(7), den)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if got := hex.EncodeToString(a.Public().Hash()); got != "59804b3649bcb5f0db0611df23329c2baf90766a00c2b3240699e17507f608d3" {
		t.Errorf("the identity's hash is %s", got)
	}
	if err := a.Public().Check(); err != nil || len(a.Public().KEM)+len(a.Public().Sig) != IdentitySize {
		t.Errorf("the identity's keys are %d and %d bytes: %v", len(a.Public().KEM), len(a.Public().Sig), err)
	}
	again, err := DeriveIdentity(fixedSeal(7), den)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if !bytes.Equal(again.Public().KEM, a.Public().KEM) || !bytes.Equal(again.Public().Sig, a.Public().Sig) {
		t.Error("another device holding the seal made another identity")
	}
	for what, seal := range map[string][]byte{"another den": fixedSeal(7), "another seal": fixedSeal(8)} {
		d := den
		if what == "another den" {
			d = other
		}
		b, err := DeriveIdentity(seal, d)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(b.Public().KEM, a.Public().KEM) || bytes.Equal(b.Public().Sig, a.Public().Sig) {
			t.Errorf("%s makes a key of the same identity", what)
		}
		b.Close()
	}
	if _, err := DeriveIdentity(fixedSeal(7)[:31], den); err == nil {
		t.Error("an identity came from a short seal")
	}
	// A key's statement carries its maker's identity as the hash of its
	// X-Wing key and its signing key, which hash to the identity.
	if !bytes.Equal(IdentityHash(KEMHash(a.Public().KEM), a.Public().Sig), a.Public().Hash()) {
		t.Error("the identity's hash isn't made from its parts")
	}
	for _, bad := range []Identity{
		{KEM: a.Public().KEM[:100], Sig: a.Public().Sig},
		{KEM: a.Public().KEM, Sig: a.Public().Sig[:31]},
		{KEM: bytes.Repeat([]byte{0xff}, IdentityKEMSize), Sig: a.Public().Sig},
	} {
		if bad.Check() == nil {
			t.Errorf("an identity of %d and %d bytes passed", len(bad.KEM), len(bad.Sig))
		}
	}
}

// A code is 20 digits made from an identity, the member and the den, the
// same wherever it's made. Fixed cases.
func TestCodes(t *testing.T) {
	den := bytes.Repeat([]byte{9}, IDSize)
	hash, _ := hex.DecodeString("59804b3649bcb5f0db0611df23329c2baf90766a00c2b3240699e17507f608d3")
	for _, c := range []struct {
		member int64
		den    []byte
		hash   []byte
		want   string
	}{
		{42, den, hash, "84108124167015020168"},
		{43, den, hash, "78127238854898767202"},
		{42, bytes.Repeat([]byte{10}, IDSize), hash, "14464906224657939940"},
		{42, den, bytes.Repeat([]byte{1}, IdentityHashSize), "12577697407631823588"},
	} {
		if got := Code(c.hash, c.den, c.member); got != c.want {
			t.Errorf("member %d's code is %s, want %s", c.member, got, c.want)
		}
	}
	code := Code(hash, den, 42)
	if got := FormatCode(code); got != "8410 8124 1670 1502 0168" {
		t.Errorf("the code shows as %q", got)
	}
	for typed, ok := range map[string]bool{
		"8410 8124 1670 1502 0168":  true,
		"8410-8124-1670-1502-0168":  true,
		"84108124167015020168":      true,
		" 8410 8124\t1670 15020168": true,
		"8410 8124 1670 1502 0169":  false,
		"8410 8124 1670 1502":       false,
		"":                          false,
	} {
		if CodeTyped(code, typed) != ok {
			t.Errorf("%q typed for the code: want %v", typed, ok)
		}
	}
	if CodeTyped("", "") {
		t.Error("an empty code matched")
	}
}

// A key's copy opens only with the identity it was sealed to, and only
// for the den, the DM, the key and the member it was sealed for.
func TestKeyCopies(t *testing.T) {
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
	key, kid := Random(DMKeySize), Random(KIDSize)
	copied, err := SealKeyCopy(alice.Public(), den, 5, kid, 1, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != KeyCopySize {
		t.Fatalf("a copy is %d bytes", len(copied))
	}
	if got, err := alice.OpenKeyCopy(den, 5, kid, 1, copied); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("alice's copy opened to %x, %v", got, err)
	}
	for what, open := range map[string]func() ([]byte, error){
		"another identity": func() ([]byte, error) { return bob.OpenKeyCopy(den, 5, kid, 1, copied) },
		"another DM":       func() ([]byte, error) { return alice.OpenKeyCopy(den, 6, kid, 1, copied) },
		"another key":      func() ([]byte, error) { return alice.OpenKeyCopy(den, 5, Random(KIDSize), 1, copied) },
		"another member":   func() ([]byte, error) { return alice.OpenKeyCopy(den, 5, kid, 2, copied) },
		"another den":      func() ([]byte, error) { return alice.OpenKeyCopy(bytes.Repeat([]byte{10}, IDSize), 5, kid, 1, copied) },
		"a changed copy": func() ([]byte, error) {
			changed := slices.Clone(copied)
			changed[len(changed)-1] ^= 1
			return alice.OpenKeyCopy(den, 5, kid, 1, changed)
		},
		"a short copy": func() ([]byte, error) { return alice.OpenKeyCopy(den, 5, kid, 1, copied[:100]) },
	} {
		if _, err := open(); err == nil {
			t.Errorf("a copy opened with %s", what)
		}
	}
	if _, err := SealKeyCopy(Identity{KEM: Random(10), Sig: alice.Public().Sig}, den, 5, kid, 1, key); err == nil {
		t.Error("a key was sealed to a malformed identity")
	}
	// The commitment names the key, so a copy that opens to another, as
	// anyone can seal one to an identity, doesn't match it.
	if bytes.Equal(KeyCommit(kid, key), KeyCommit(kid, Random(DMKeySize))) || bytes.Equal(KeyCommit(kid, key), KeyCommit(Random(KIDSize), key)) {
		t.Error("a commitment doesn't name its key and its ID")
	}
}

// A statement checks only against its maker's signing key, as it was
// signed.
func TestKeyStatements(t *testing.T) {
	den := bytes.Repeat([]byte{9}, IDSize)
	alice, _ := DeriveIdentity(fixedSeal(7), den)
	bob, _ := DeriveIdentity(fixedSeal(8), den)
	defer alice.Close()
	defer bob.Close()
	key, kid := Random(DMKeySize), Random(KIDSize)
	s := KeyStatement{DenID: den, Channel: 5, KID: kid, Commit: KeyCommit(kid, key), MadeBy: 1,
		Members: []StatedMember{{1, alice.Public().Hash()}, {2, bob.Public().Hash()}}}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	sig := alice.Sign(s)
	if !VerifyStatement(alice.Public().Sig, s, sig) {
		t.Fatal("alice's statement doesn't check against her key")
	}
	if VerifyStatement(bob.Public().Sig, s, sig) {
		t.Error("alice's statement checks against bob's key")
	}
	if !bytes.Equal(s.Named(2), bob.Public().Hash()) || s.Named(3) != nil {
		t.Error("the statement names its members' identities oddly")
	}
	changes := map[string]func(*KeyStatement){
		"another den":      func(s *KeyStatement) { s.DenID = bytes.Repeat([]byte{10}, IDSize) },
		"another DM":       func(s *KeyStatement) { s.Channel = 6 },
		"another key":      func(s *KeyStatement) { s.KID = Random(KIDSize) },
		"another commit":   func(s *KeyStatement) { s.Commit = KeyCommit(s.KID, Random(DMKeySize)) },
		"another maker":    func(s *KeyStatement) { s.MadeBy = 2 },
		"another identity": func(s *KeyStatement) { s.Members[1].Identity = alice.Public().Hash() },
		"another member":   func(s *KeyStatement) { s.Members[1].Member = 3 },
	}
	for what, change := range changes {
		c := s
		c.Members = slices.Clone(s.Members)
		change(&c)
		if VerifyStatement(alice.Public().Sig, c, sig) {
			t.Errorf("the signature checks for %s", what)
		}
	}
	for what, bad := range map[string]KeyStatement{
		"members out of order": {DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 1,
			Members: []StatedMember{{2, bob.Public().Hash()}, {1, alice.Public().Hash()}}},
		"a maker outside it": {DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 3, Members: s.Members},
		"one member":         {DenID: den, Channel: 5, KID: kid, Commit: s.Commit, MadeBy: 1, Members: s.Members[:1]},
		"a short ID":         {DenID: den, Channel: 5, KID: kid[:8], Commit: s.Commit, MadeBy: 1, Members: s.Members},
	} {
		if bad.Check() == nil {
			t.Errorf("a statement with %s checks out", what)
		}
	}
}

// A record is padded to a power of four from 16 KiB, and opens only for
// the den, the member and the revision it was written at.
func TestRecords(t *testing.T) {
	den := bytes.Repeat([]byte{9}, IDSize)
	seal := fixedSeal(7)
	for size, want := range map[int]int{0: 16 << 10, 16<<10 - 4: 16 << 10, 16<<10 - 3: 64 << 10, 200 << 10: 256 << 10} {
		sealed, err := SealRecord(seal, den, 1, 3, bytes.Repeat([]byte{'x'}, size))
		if err != nil {
			t.Fatal(err)
		}
		if len(sealed) != want+recordOverhead || !CheckSealedRecord(sealed) {
			t.Errorf("a record of %d bytes seals to %d", size, len(sealed))
		}
		back, err := OpenRecord(seal, den, 1, 3, sealed)
		if err != nil || len(back) != size {
			t.Errorf("a record of %d bytes opened to %d, %v", size, len(back), err)
		}
	}
	if _, err := SealRecord(seal, den, 1, 3, make([]byte, MaxRecord+1)); err == nil {
		t.Error("a record past the largest size sealed")
	}
	sealed, err := SealRecord(seal, den, 1, 3, []byte(`{"identities":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	for what, open := range map[string]func() ([]byte, error){
		"another revision": func() ([]byte, error) { return OpenRecord(seal, den, 1, 2, sealed) },
		"another member":   func() ([]byte, error) { return OpenRecord(seal, den, 2, 3, sealed) },
		"another den":      func() ([]byte, error) { return OpenRecord(seal, bytes.Repeat([]byte{10}, IDSize), 1, 3, sealed) },
		"another seal":     func() ([]byte, error) { return OpenRecord(fixedSeal(8), den, 1, 3, sealed) },
		"a cut record":     func() ([]byte, error) { return OpenRecord(seal, den, 1, 3, sealed[:len(sealed)-1]) },
	} {
		if _, err := open(); err == nil {
			t.Errorf("a record opened for %s", what)
		}
	}
	if CheckSealedRecord(make([]byte, 1000)) {
		t.Error("a record of no padded size passed")
	}
	if !strings.Contains(string(mustOpen(t, seal, den, sealed)), "identities") {
		t.Error("the record doesn't hold what was sealed")
	}
}

func mustOpen(t *testing.T, seal, den, sealed []byte) []byte {
	t.Helper()
	plain, err := OpenRecord(seal, den, 1, 3, sealed)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}
