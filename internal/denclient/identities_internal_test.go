package denclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

func hashOf(b byte) denproto.Bytes { return bytes.Repeat([]byte{b}, denproto.IdentityHashSize) }

// Two devices that kept the same identity for someone agree on it: the
// mark where either compared, the hold where either holds, unless either
// verified them or sent anyway at it. Two that kept different ones show it
// as a change: the one kept later stands, the other joins the past, and
// where the member verified the older, the DM holds.
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
		{"the same, never held on one", &keptIdentity{Hash: x, KeptAt: 5, Held: true, Changes: []int64{5}},
			&keptIdentity{Hash: x, KeptAt: 5, Changes: []int64{5}}, keptIdentity{Hash: x, KeptAt: 5, Held: true, Changes: []int64{5}}},
		{"the same, sent anyway on one", &keptIdentity{Hash: x, KeptAt: 5, Held: true, Changes: []int64{5}},
			&keptIdentity{Hash: x, KeptAt: 5, LetGo: x, Changes: []int64{5}}, keptIdentity{Hash: x, KeptAt: 5, LetGo: x, Changes: []int64{5}}},
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

// The four states a device's hold on a member can be in.
type holdState int

const (
	neverHeld holdState = iota
	holding
	compared
	letGo
)

func (s holdState) String() string { return [...]string{"never held", "held", "verified", "let go"}[s] }

// keptAs is what a device kept for a member: identity h, kept at, in s.
type keptAs struct {
	h  byte
	at int64
	s  holdState
}

func (k keptAs) kept() *keptIdentity {
	out := &keptIdentity{Hash: hashOf(k.h), KeptAt: k.at}
	switch k.s {
	case holding:
		out.Held = true
	case compared:
		out.Verified = true
	case letGo:
		out.LetGo = hashOf(k.h)
	}
	return out
}

// hold is what a merge decides of a hold, leaving out the past and the
// changes, whose order may follow the merge's.
type hold struct {
	h                     byte
	verified, held, letGo bool
}

func holdOf(t *testing.T, k *keptIdentity) hold {
	t.Helper()
	// A let-go names the identity kept, or nothing, and never sits beside a
	// hold.
	if k.LetGo != nil && !bytes.Equal(k.LetGo, k.Hash) || k.Held && k.LetGo != nil {
		t.Fatalf("merged to %+v", k)
	}
	return hold{k.Hash[0], k.Verified, k.Held, k.LetGo != nil}
}

// orders merges devices' entries in every order a member's devices could:
// each order of them, folded from the left and from the right.
func orders(ks []keptAs) [][]*keptIdentity {
	var out [][]*keptIdentity
	var permute func(done []keptAs, left []keptAs)
	permute = func(done, left []keptAs) {
		if len(left) == 0 {
			l, r := done[0].kept(), done[len(done)-1].kept()
			for i := 1; i < len(done); i++ {
				l = mergeIdentity(l, done[i].kept())
				r = mergeIdentity(done[len(done)-1-i].kept(), r)
			}
			out = append(out, []*keptIdentity{l, r})
			return
		}
		for i := range left {
			rest := append(slices.Clone(left[:i]), left[i+1:]...)
			permute(append(slices.Clone(done), left[i]), rest)
		}
	}
	permute(nil, ks)
	return out
}

func allStates() []holdState { return []holdState{neverHeld, holding, compared, letGo} }

// At one identity, two or three devices hold wherever any held, unless any
// verified the member or sent anyway, in whatever order they merge.
func TestMergingHoldsAtOneIdentity(t *testing.T) {
	for n := 2; n <= 3; n++ {
		var each func(ks []keptAs)
		each = func(ks []keptAs) {
			if len(ks) < n {
				for _, s := range allStates() {
					each(append(slices.Clone(ks), keptAs{1, int64(3 + len(ks)), s}))
				}
				return
			}
			var want hold
			want.h = 1
			for _, k := range ks {
				want.held = want.held || k.s == holding
				want.verified = want.verified || k.s == compared
				want.letGo = want.letGo || k.s == letGo
			}
			want.held = want.held && !want.verified && !want.letGo
			for _, merged := range orders(ks) {
				for _, k := range merged {
					if got := holdOf(t, k); got != want {
						t.Errorf("%v merged to %+v, want %+v", ks, got, want)
					}
				}
			}
		}
		each(nil)
	}
}

// At two identities, the newer stands, held where either held or the
// older was verified, unless the newer is verified or let go at.
func TestMergingHoldsAtTwoIdentities(t *testing.T) {
	for _, o := range allStates() {
		for _, n := range allStates() {
			older, newer := keptAs{1, 1, o}, keptAs{2, 5, n}
			want := hold{h: 2, verified: n == compared, letGo: n == letGo,
				held: (n == holding || o == holding || o == compared) && n != compared && n != letGo}
			for _, got := range []*keptIdentity{mergeIdentity(older.kept(), newer.kept()), mergeIdentity(newer.kept(), older.kept())} {
				if h := holdOf(t, got); h != want {
					t.Errorf("older %v, newer %v: merged to %+v, want %+v", o, n, h, want)
				}
			}
		}
	}
}

// Three devices at two identities merge in an order the den's writes pick.
// A choice at the newer identity lifts the hold in any order; otherwise the
// hold stands wherever one stood, or the older was verified, unless a
// let-go at the older lifted it there, which an order may miss but never
// make up. Without such a let-go, the order doesn't matter.
func TestMergingHoldsThroughAThirdDevice(t *testing.T) {
	var cases [][]keptAs
	for _, a := range []byte{1, 2} {
		for _, b := range []byte{1, 2} {
			for _, c := range []byte{1, 2} {
				if a == b && b == c {
					continue
				}
				for _, sa := range allStates() {
					for _, sb := range allStates() {
						for _, sc := range allStates() {
							at := func(h byte) int64 { return int64(h) * 4 }
							cases = append(cases, []keptAs{{a, at(a), sa}, {b, at(b), sb}, {c, at(c), sc}})
						}
					}
				}
			}
		}
	}
	for _, ks := range cases {
		var choseNewer, heldNewer, verifiedOlder, heldOlder, letGoOlder bool
		for _, k := range ks {
			switch {
			case k.h == 2:
				choseNewer = choseNewer || k.s == compared || k.s == letGo
				heldNewer = heldNewer || k.s == holding
			default:
				verifiedOlder = verifiedOlder || k.s == compared
				heldOlder = heldOlder || k.s == holding
				letGoOlder = letGoOlder || k.s == letGo
			}
		}
		var first *hold
		for _, merged := range orders(ks) {
			for _, k := range merged {
				got := holdOf(t, k)
				switch {
				case got.h != 2:
					t.Errorf("%v merged to the older identity", ks)
				case choseNewer && got.held:
					t.Errorf("%v: held after a choice at the newer identity", ks)
				case !got.held && !choseNewer && (heldNewer || verifiedOlder || heldOlder && !letGoOlder):
					t.Errorf("%v: the hold lapsed with nobody choosing", ks)
				case got.held && !heldNewer && !heldOlder && !verifiedOlder:
					t.Errorf("%v: held with no hold and nothing verified before", ks)
				}
				if !letGoOlder {
					if first == nil {
						first = &got
					} else if got != *first {
						t.Errorf("%v merged to %+v in one order and %+v in another", ks, *first, got)
					}
				}
			}
		}
	}
}

// Merging a merge again with either side of it changes nothing.
func TestMergingAgainChangesNothing(t *testing.T) {
	for _, hs := range [][2]byte{{1, 1}, {1, 2}} {
		for _, sa := range allStates() {
			for _, sb := range allStates() {
				a, b := keptAs{hs[0], 1, sa}.kept(), keptAs{hs[1], 5, sb}.kept()
				m := mergeIdentity(a, b)
				want := holdOf(t, m)
				for _, again := range []*keptIdentity{mergeIdentity(m, a), mergeIdentity(m, b), mergeIdentity(a, m), mergeIdentity(b, m)} {
					if got := holdOf(t, again); got != want {
						t.Errorf("%v and %v merged to %+v, and again to %+v", sa, sb, want, got)
					}
				}
			}
		}
	}
}

// The record and this device's copy of it, as v1.15.0 wrote them: bob, 2,
// held since his keys changed, and carol, 3, verified.
const (
	recordV1150 = `{"members":{"2":{"h":"AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI","t":1700000000500,"held":true,` +
		`"p":["AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"],"c":[1700000000500]},` +
		`"3":{"h":"AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM","t":1700000000100,"v":true}},` +
		`"own":["CQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQk"]}`
	keptV1150 = `{"record":` + recordV1150 + `,"revision":4}`
)

// A record as v1.15.0 wrote it reads as it was, held where it said so;
// one an older Dens writes after, which leaves out where its member sent
// anyway, leaves the hold standing, and a newer device's let-go lifts it.
func TestRecordAsV1150WroteIt(t *testing.T) {
	var r identityRecord
	if err := json.Unmarshal([]byte(recordV1150), &r); err != nil {
		t.Fatal(err)
	}
	var k keptIdentities
	if err := json.Unmarshal([]byte(keptV1150), &k); err != nil {
		t.Fatal(err)
	}
	for what, rec := range map[string]identityRecord{"the record": r, "this device's copy": k.Record} {
		bob, carol := rec.Members["2"], rec.Members["3"]
		if bob == nil || !bob.Held || bob.Verified || len(bob.Past) != 1 || carol == nil || !carol.Verified || len(rec.Own) != 1 {
			t.Fatalf("%s as v1.15.0 wrote it: %+v %+v", what, bob, carol)
		}
	}
	if k.Revision != 4 {
		t.Errorf("this device's copy at revision %d", k.Revision)
	}

	// An older Dens of the member's sent anyway, or never held: it writes
	// bob unheld, with nothing in place of the let-go.
	older := `{"members":{"2":{"h":"AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI","t":1700000000900,` +
		`"p":["AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"],"c":[1700000000900]}}}`
	var theirs identityRecord
	if err := json.Unmarshal([]byte(older), &theirs); err != nil {
		t.Fatal(err)
	}
	r.merge(theirs)
	if !r.Members["2"].Held {
		t.Fatalf("an older Dens's write lifted the hold: %+v", r.Members["2"])
	}
	// A newer one sent anyway at bob's identity.
	r.merge(identityRecord{Members: map[string]*keptIdentity{"2": {Hash: hashOf(2), KeptAt: 1700000000700, LetGo: hashOf(2)}}})
	if bob := r.Members["2"]; bob.Held || !bytes.Equal(bob.LetGo, hashOf(2)) {
		t.Fatalf("a newer device's let-go: %+v", bob)
	}
	// What an older Dens reads of it is a record it knows, unheld.
	out, err := r.encode()
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		Members map[string]struct {
			Held bool `json:"held"`
		} `json:"members"`
	}
	if err := json.Unmarshal(out, &old); err != nil || old.Members["2"].Held {
		t.Errorf("as an older Dens reads it: %s %v", out, err)
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
	// A let-go goes at the next change: back at that identity, the member
	// having compared in between, it holds.
	r.Members["3"] = &keptIdentity{Hash: hashOf(1), KeptAt: 1, LetGo: hashOf(1)}
	r.meet("3", hashOf(2), 2, false)
	if kept := r.Members["3"]; kept.LetGo != nil || kept.Held {
		t.Fatalf("after a change from where they sent anyway: %+v", kept)
	}
	r.Members["3"].Verified = true
	r.meet("3", hashOf(1), 3, false)
	if kept := r.Members["3"]; !kept.Held || kept.LetGo != nil {
		t.Fatalf("back where they once sent anyway: %+v", kept)
	}
	if len(k.Past) != maxPast || len(k.Changes) != maxChanges {
		t.Fatalf("a long history kept %d identities and %d changes", len(k.Past), len(k.Changes))
	}
}

// A record past the most it holds drops the oldest of what it kept before
// and the oldest changes, then members kept longest ago who aren't
// verified or held, never one who is, and keeps the member's own
// identities from before they started over.
func TestRecordFits(t *testing.T) {
	r := identityRecord{Members: map[string]*keptIdentity{}, Own: []denproto.Bytes{denproto.Random(32), denproto.Random(32)}}
	for i := range 4000 {
		k := &keptIdentity{Hash: denproto.Random(denproto.IdentityHashSize), KeptAt: int64(i), Changes: []int64{1, 2, 3, 4},
			Past: []denproto.Bytes{denproto.Random(32), denproto.Random(32)}}
		k.Verified = i%100 == 0
		k.Held = i%100 == 50
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
		if (k.Verified || k.Held) && back.Members[id] == nil {
			t.Fatalf("verified or held member %s went", id)
		}
	}
	if len(back.Own) != len(r.Own) || !bytes.Equal(back.Own[0], r.Own[0]) {
		t.Fatalf("own identities from before: %d of %d kept", len(back.Own), len(r.Own))
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
// device's record, each once, the latest eight kept.
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

// A channel the den answered with keeps its kind before its event comes:
// a DM just opened can't arrive as a text channel.
func TestAnAnswersKindHolds(t *testing.T) {
	c, _, _ := testConn(t)
	if err := c.holdKind("9", denproto.KindDM); err != nil {
		t.Fatal(err)
	}
	if err := c.holdKind("9", denproto.KindNotes); err == nil {
		t.Error("the DM answered again as notes")
	}
	e, _ := denproto.NewEvent(denproto.EventChannelCreated, 1, denproto.Channel{ID: "9", Kind: denproto.KindText, Name: "general"})
	if _, _, err := c.den.applyEvent(nil, e, denproto.Member{ID: "1"}, time.Now()); err != errMalformed {
		t.Errorf("a DM just opened, coming as a text channel: %v", err)
	}
}

// What a kept state holds for good is picked out entry by entry: a kind
// that doesn't check, which a member with the den can have a Dens keep,
// takes none of the others with it.
func TestKeptForGoodByEntry(t *testing.T) {
	k := keptState{Kinds: map[string]string{"7": denproto.KindDM, "x": denproto.KindText}}
	if kinds := k.forGood(); len(kinds) != 1 || kinds["7"] != denproto.KindDM {
		t.Errorf("kinds kept: %v", kinds)
	}
}

// WriteRecord has a member's Dens write their record to a den at once, as
// its hourly write does, merging what their other devices wrote first, for
// the tests of package denclient_test that order two devices' writes.
func WriteRecord(m *Manager, denID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	return c.writeRecord(context.Background())
}
