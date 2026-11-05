package denclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

func hashOf(b byte) denproto.Bytes { return bytes.Repeat([]byte{b}, denproto.IdentityHashSize) }

// Two devices that kept the same identity for someone agree on it: the
// mark where either compared, the hold only while both have it. Two that
// kept different ones show it as a change: the one kept later stands, the
// other joins the past, and where the member verified the older, the DM
// holds.
func TestMergingWhatDevicesKept(t *testing.T) {
	x, y := hashOf(1), hashOf(2)
	for _, c := range []struct {
		name         string
		ours, theirs *keptIdentity
		want         keptIdentity
	}{
		{"one side", nil, &keptIdentity{Hash: x, KeptAt: 5, Verified: true}, keptIdentity{Hash: x, KeptAt: 5, Verified: true}},
		{"the same, compared on one", &keptIdentity{Hash: x, KeptAt: 5}, &keptIdentity{Hash: x, KeptAt: 3, Verified: true},
			keptIdentity{Hash: x, KeptAt: 3, Verified: true}},
		{"the same, sent anyway on one", &keptIdentity{Hash: x, KeptAt: 5, Held: true, Changes: []int64{5}},
			&keptIdentity{Hash: x, KeptAt: 5, Changes: []int64{5}}, keptIdentity{Hash: x, KeptAt: 5, Changes: []int64{5}}},
		{"a change one saw", &keptIdentity{Hash: x, KeptAt: 1, Verified: true},
			&keptIdentity{Hash: y, KeptAt: 9, Held: true, Past: []denproto.Bytes{x}, Changes: []int64{9}},
			keptIdentity{Hash: y, KeptAt: 9, Held: true, Past: []denproto.Bytes{x}, Changes: []int64{9}}},
		{"different at first sight", &keptIdentity{Hash: x, KeptAt: 1, Verified: true}, &keptIdentity{Hash: y, KeptAt: 4},
			keptIdentity{Hash: y, KeptAt: 4, Held: true, Past: []denproto.Bytes{x}, Changes: []int64{4}}},
		{"different, the newer compared", &keptIdentity{Hash: x, KeptAt: 1, Verified: true}, &keptIdentity{Hash: y, KeptAt: 4, Verified: true},
			keptIdentity{Hash: y, KeptAt: 4, Verified: true, Past: []denproto.Bytes{x}, Changes: []int64{4}}},
		{"the older kept here", &keptIdentity{Hash: y, KeptAt: 4}, &keptIdentity{Hash: x, KeptAt: 1},
			keptIdentity{Hash: y, KeptAt: 4, Past: []denproto.Bytes{x}, Changes: []int64{4}}},
	} {
		got := mergeIdentity(c.ours, c.theirs)
		if !sameKept(got, &c.want) {
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(c.want)
			t.Errorf("%s: %s, want %s", c.name, a, b)
		}
		// Either way round, the same.
		if back := mergeIdentity(c.theirs, c.ours); c.ours != nil && !sameKept(back, got) {
			a, _ := json.Marshal(back)
			t.Errorf("%s the other way round: %s", c.name, a)
		}
	}
}

// Meeting an identity keeps it the first time it's needed, and sees a
// change in one kept, which drops the mark and holds where it was set.
func TestMeetingIdentities(t *testing.T) {
	r := identityRecord{Members: map[string]*keptIdentity{}}
	if r.meet("2", hashOf(1), 10, false) || r.meet("2", nil, 10, true) {
		t.Fatal("kept one not needed, or none")
	}
	if !r.meet("2", hashOf(1), 10, true) || r.meet("2", hashOf(1), 20, true) {
		t.Fatal("the first meeting kept nothing, or the second changed it")
	}
	r.Members["2"].Verified = true
	if !r.meet("2", hashOf(2), 30, false) {
		t.Fatal("a change went unseen")
	}
	k := r.Members["2"]
	if k.Verified || !k.Held || !bytes.Equal(k.Hash, hashOf(2)) || len(k.Past) != 1 || !slices.Equal(k.Changes, []int64{30}) {
		t.Fatalf("after the change: %+v", k)
	}
	// Changing back is a change too, and the past keeps each once.
	r.meet("2", hashOf(1), 40, false)
	if len(k.Past) != 1 || !bytes.Equal(k.Past[0], hashOf(2)) || len(k.Changes) != 2 {
		t.Fatalf("after changing back: %+v", k)
	}
	for i := range maxChanges + 5 {
		r.meet("2", hashOf(byte(10+i)), int64(50+i), false)
	}
	if len(k.Past) != maxPast || len(k.Changes) != maxChanges {
		t.Fatalf("a long history kept %d identities and %d changes", len(k.Past), len(k.Changes))
	}
}

// A record past the most it holds drops the oldest of what it kept before
// and the oldest changes, then members kept longest ago who aren't
// verified, never one who is.
func TestRecordFits(t *testing.T) {
	r := identityRecord{Members: map[string]*keptIdentity{}}
	for i := range 4000 {
		k := &keptIdentity{Hash: denproto.Random(denproto.IdentityHashSize), KeptAt: int64(i), Changes: []int64{1, 2, 3, 4},
			Past: []denproto.Bytes{denproto.Random(32), denproto.Random(32)}}
		k.Verified = i%100 == 0
		r.Members[fmt.Sprint(i+1)] = k
	}
	out, err := r.encode()
	if err != nil || len(out) > denproto.MaxRecord {
		t.Fatalf("encoded to %d bytes: %v", len(out), err)
	}
	var back identityRecord
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	for id, k := range r.Members {
		if k.Verified && back.Members[id] == nil {
			t.Fatalf("verified member %s went", id)
		}
	}
	if len(back.Members) >= len(r.Members) || back.Members["4000"] == nil || back.Members["2"] != nil {
		t.Fatalf("kept %d of %d members, the newest %v, an old one %v", len(back.Members), len(r.Members), back.Members["4000"] != nil, back.Members["2"] != nil)
	}
}

// testConn is a connection to a den with a DM between this member, 1, and
// bob, 2, whose identity this Dens kept, as far as checking keys goes.
func testConn(t *testing.T) (*conn, *denproto.IdentityKeys, *denproto.IdentityKeys) {
	t.Helper()
	denID := bytes.Repeat([]byte{9}, denproto.IDSize)
	seal, bobSeal := denproto.NewSeal(), denproto.NewSeal()
	secret, err := vault.NewSecret(seal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secret.Close)
	mine, _ := denproto.DeriveIdentity(seal, denID)
	bob, _ := denproto.DeriveIdentity(bobSeal, denID)
	t.Cleanup(mine.Close)
	t.Cleanup(bob.Close)
	s := newState()
	s.identities = true
	s.channels["5"] = denproto.Channel{ID: "5", Kind: denproto.KindDM, Members: []string{"1", "2"}}
	c := &conn{j: &joined{denID: denID, seal: secret}, den: s, ids: newKept(), verdicts: map[string]string{}, copies: map[string]denproto.Bytes{}}
	c.profile.Member.ID = "1"
	c.ids.Record.Members["2"] = &keptIdentity{Hash: bob.Public().Hash(), KeptAt: 1}
	return c, mine, bob
}

// makeKey makes a signed key for the DM, as its maker's Dens makes it:
// sealed to the identities named, signed by the maker's.
func makeKey(t *testing.T, c *conn, id string, maker *denproto.IdentityKeys, makerID string, named map[string]*denproto.IdentityKeys, key []byte) denproto.SignedKey {
	t.Helper()
	kid := denproto.Random(denproto.KIDSize)
	k := denproto.SignedKey{ID: id, ChannelID: "5", MadeBy: makerID, MadeAt: 1, KID: kid, Commit: denproto.KeyCommit(kid, key),
		Maker: denproto.KeyMaker{KEMHash: denproto.KEMHash(maker.Public().KEM), Sig: maker.Public().Sig}}
	for _, m := range []string{"1", "2"} {
		k.Members = append(k.Members, denproto.KeyMember{MemberID: m, Identity: named[m].Public().Hash()})
	}
	who, _ := denproto.ParseID("1")
	copied, err := denproto.SealKeyCopy(named["1"].Public(), c.j.denID, 5, kid, who, key)
	if err != nil {
		t.Fatal(err)
	}
	k.Copy = copied
	s, err := k.Statement(c.j.denID)
	if err != nil {
		t.Fatal(err)
	}
	k.Signature = maker.Sign(s)
	return k
}

// This member's Dens uses a signed key only once its statement checks
// against its maker's identity, as kept, and names the identities kept for
// each member: any kept for reading, the current one for sending. A key
// whose signature doesn't check, whose maker isn't the identity kept, or
// that names another identity for this member is refused, and so is a
// copy that opens to another key than the one committed to.
func TestCheckingSignedKeys(t *testing.T) {
	c, mine, bob := testConn(t)
	other, _ := denproto.DeriveIdentity(denproto.NewSeal(), c.j.denID)
	defer other.Close()
	key := denproto.Random(denproto.DMKeySize)
	both := map[string]*denproto.IdentityKeys{"1": mine, "2": bob}

	good := makeKey(t, c, "10", bob, "2", both, key)
	ours := makeKey(t, c, "11", mine, "1", both, key)
	for _, k := range []denproto.SignedKey{good, ours} {
		if !c.den.cleanSigned(c.j.denID, k) {
			t.Fatalf("key %s doesn't check its shape", k.ID)
		}
		if v := c.verdictLocked(k, true); v != "" {
			t.Fatalf("key %s: %s", k.ID, v)
		}
		got, locked := c.signedKeyLocked(k)
		if locked != "" || !bytes.Equal(got, key) {
			t.Fatalf("key %s opened: %s", k.ID, locked)
		}
		// Kept sealed with the seal, it opens again without X-Wing.
		if c.copies[k.ID] == nil {
			t.Fatalf("key %s wasn't kept", k.ID)
		}
		if again, locked := c.signedKeyLocked(k); locked != "" || !bytes.Equal(again, key) {
			t.Fatalf("key %s opened again: %s", k.ID, locked)
		}
	}

	badSig := good
	badSig.Signature = slices.Clone(good.Signature)
	badSig.Signature[0] ^= 1
	if v := c.verdictLocked(badSig, false); v != lockedBroken {
		t.Errorf("a signature that doesn't check: %q", v)
	}
	// A maker other than the identity kept for bob: someone in the middle.
	impostor := makeKey(t, c, "12", other, "2", map[string]*denproto.IdentityKeys{"1": mine, "2": other}, key)
	if v := c.verdictLocked(impostor, false); v != lockedKeys {
		t.Errorf("a key from an identity bob never had: %q", v)
	}
	// A key naming another identity for this member.
	notMine := makeKey(t, c, "13", bob, "2", map[string]*denproto.IdentityKeys{"1": other, "2": bob}, key)
	if v := c.verdictLocked(notMine, false); v != lockedKeys {
		t.Errorf("a key naming another identity for this member: %q", v)
	}
	// A copy that opens to another key than the one committed to.
	swapped := makeKey(t, c, "14", bob, "2", both, key)
	who, _ := denproto.ParseID("1")
	swapped.Copy, _ = denproto.SealKeyCopy(mine.Public(), c.j.denID, 5, swapped.KID, who, denproto.Random(denproto.DMKeySize))
	if _, locked := c.signedKeyLocked(swapped); locked != lockedBroken {
		t.Errorf("a copy of another key: %q", locked)
	}

	// Bob's identity changes: his old keys still read, but nothing is sent
	// under them.
	c.ids.Record.meet("2", other.Public().Hash(), 2, false)
	clear(c.verdicts)
	if v := c.verdictLocked(good, false); v != "" {
		t.Errorf("reading under bob's old identity: %q", v)
	}
	if v := c.verdictLocked(good, true); v != lockedKeys {
		t.Errorf("sending under bob's old identity: %q", v)
	}
	// One's own copy gone, as after starting over, is lost.
	gone := good
	gone.Copy, gone.Retired, gone.RetiredBy, gone.RetiredAt = nil, denproto.RetiredStartedOver, "1", 5
	if v := c.verdictLocked(gone, false); v != lockedLost {
		t.Errorf("a key after starting over: %q", v)
	}
}

// A member's own identities before they started over join from every
// device's record, each once, the latest eight kept (M16.3).
func TestRecordKeepsOwnPast(t *testing.T) {
	ours := identityRecord{Members: map[string]*keptIdentity{}, Own: []denproto.Bytes{hashOf(1)}}
	theirs := identityRecord{Members: map[string]*keptIdentity{}, Own: []denproto.Bytes{hashOf(1), hashOf(2)}}
	if !ours.merge(theirs) || len(ours.Own) != 2 || !bytes.Equal(ours.Own[1], hashOf(2)) {
		t.Fatalf("merged own identities: %x", ours.Own)
	}
	if ours.merge(theirs) {
		t.Error("merging the same own identities again changed the record")
	}
	for i := range 10 {
		ours.merge(identityRecord{Own: []denproto.Bytes{hashOf(byte(10 + i))}})
	}
	if len(ours.Own) != maxPast || !bytes.Equal(ours.Own[maxPast-1], hashOf(19)) {
		t.Errorf("own identities after ten more: %d, the last %x", len(ours.Own), ours.Own[len(ours.Own)-1])
	}
}
