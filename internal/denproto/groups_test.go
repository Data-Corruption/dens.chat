package denproto

import (
	"bytes"
	"errors"
	"slices"
	"testing"
)

// groupTest holds a den, a group's channel and members' identities, made
// from fixed seals, for a group's changes.
type groupTest struct {
	t       *testing.T
	den     []byte
	channel int64
	ids     map[string]*IdentityKeys
}

func newGroupTest(t *testing.T, members ...string) *groupTest {
	g := &groupTest{t: t, den: bytes.Repeat([]byte{9}, IDSize), channel: 40, ids: map[string]*IdentityKeys{}}
	for i, m := range members {
		k, err := DeriveIdentity(fixedSeal(byte(20+i)), g.den)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(k.Close)
		g.ids[m] = k
	}
	return g
}

// signed signs a change as the member it names as its maker, or as signer
// where that's given, as a den making one up would.
func (g *groupTest) signed(c GroupChange, signer string) GroupChange {
	g.t.Helper()
	if signer == "" {
		signer = c.By
	}
	k := g.ids[signer]
	maker := k.Maker()
	c.Signer = &maker
	// A change that can't be signed, being malformed, goes unsigned, which
	// is refused all the same.
	c.Signature, _ = k.SignChange(g.den, g.channel, c)
	return c
}

// next is the change after state, as a member's Dens makes it.
func next(state GroupState, kind, by string, subjects []string, after []string) GroupChange {
	return GroupChange{N: state.N + 1, Prev: state.Hash, Kind: kind, By: by, Subjects: subjects, After: after}
}

// check checks a change follows state, signed by the identity of the
// member it names, as every member's Dens does.
func (g *groupTest) check(state *GroupState, c GroupChange) (GroupState, error) {
	next, err := CheckChange(g.den, g.channel, state, c)
	if err != nil {
		return next, err
	}
	if c.Kind != ChangeDeparted && !VerifyChange(g.den, g.channel, c, g.ids[c.By].Public().Hash()) {
		return next, errors.New("the signature doesn't check")
	}
	return next, nil
}

// A group's changes follow from one another, each signed by a member who
// could make it: any member adds, the first in the order they joined
// removes, anyone leaves, and the den takes out only members who left it.
// So the den can't add anyone: an add it makes up, a removal by a member
// who can't, and a change by someone outside the group are each refused,
// and removing passes on once the maker leaves.
func TestGroupChanges(t *testing.T) {
	g := newGroupTest(t, "1", "2", "3", "4", "5", "6")
	made := g.signed(GroupChange{N: 1, Prev: FirstPrev(), Kind: ChangeMade, By: "1", Subjects: []string{"2", "3"}, After: []string{"1", "2", "3"}}, "")
	s, err := g.check(nil, made)
	if err != nil {
		t.Fatalf("a group made by its maker: %v", err)
	}
	if s.N != 1 || !slices.Equal(s.Members, []string{"1", "2", "3"}) || s.Remover() != "1" {
		t.Fatalf("the group made is %+v", s)
	}

	added := g.signed(next(s, ChangeAdded, "2", []string{"4"}, []string{"1", "2", "3", "4"}), "")
	s2, err := g.check(&s, added)
	if err != nil {
		t.Fatalf("a member adds another: %v", err)
	}
	// The member added knows the group from the add that took them in.
	if start, err := CheckStart(g.den, g.channel, "4", added); err != nil || !slices.Equal(start.Members, s2.Members) || !bytes.Equal(start.Hash, s2.Hash) {
		t.Errorf("the member added starts from %+v, %v", start, err)
	}
	if _, err := CheckStart(g.den, g.channel, "3", added); err == nil {
		t.Error("a member started from an add that didn't take them in")
	}

	for what, c := range map[string]GroupChange{
		// The den makes up an add, signed by a key of its own for a member.
		"an add the den signed for a member":  g.signed(next(s2, ChangeAdded, "2", []string{"6"}, []string{"1", "2", "3", "4", "6"}), "6"),
		"an add nobody signed":                next(s2, ChangeAdded, "2", []string{"6"}, []string{"1", "2", "3", "4", "6"}),
		"an add by someone outside the group": g.signed(next(s2, ChangeAdded, "5", []string{"6"}, []string{"1", "2", "3", "4", "6"}), ""),
		"a removal by a member who can't":     g.signed(next(s2, ChangeRemoved, "2", []string{"3"}, []string{"1", "2", "4"}), ""),
		"a removal of nobody in the group":    g.signed(next(s2, ChangeRemoved, "1", []string{"5"}, []string{"1", "2", "3", "4"}), ""),
		"a remover removing themselves":       g.signed(next(s2, ChangeRemoved, "1", []string{"1"}, []string{"2", "3", "4"}), ""),
		"a leave by someone outside":          g.signed(next(s2, ChangeLeft, "5", nil, []string{"1", "2", "3", "4"}), ""),
		"an add that names others after it":   g.signed(next(s2, ChangeAdded, "2", []string{"6"}, []string{"1", "2", "3", "4", "6", "5"}), ""),
		"an add of a member already in":       g.signed(next(s2, ChangeAdded, "2", []string{"3"}, []string{"1", "2", "3", "4", "3"}), ""),
		"an add past ten members": g.signed(GroupChange{N: s2.N + 1, Prev: s2.Hash, Kind: ChangeAdded, By: "2",
			Subjects: []string{"5", "6", "7", "8", "9", "10", "11"}, After: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}}, ""),
		"a change that skips one":              g.signed(GroupChange{N: s2.N + 2, Prev: s2.Hash, Kind: ChangeLeft, By: "3", After: []string{"1", "2", "4"}}, ""),
		"a change after another than the last": g.signed(next(s, ChangeLeft, "3", nil, []string{"1", "2"}), ""),
		"a departure the den signed":           g.signed(next(s2, ChangeDeparted, "", []string{"3"}, []string{"1", "2", "4"}), "1"),
		"a departure of nobody there":          next(s2, ChangeDeparted, "", []string{"5"}, []string{"1", "2", "3", "4"}),
		"a naming without a name":              g.signed(next(s2, ChangeNamed, "3", nil, []string{"1", "2", "3", "4"}), ""),
		"a second making":                      g.signed(GroupChange{N: 1, Prev: FirstPrev(), Kind: ChangeMade, By: "5", Subjects: []string{"6", "4"}, After: []string{"5", "6", "4"}}, ""),
	} {
		if _, err := g.check(&s2, c); err == nil {
			t.Errorf("%s was taken", what)
		}
	}

	// The maker removes; once they've left, the member who's been in the
	// group longest does.
	removed := g.signed(next(s2, ChangeRemoved, "1", []string{"3"}, []string{"1", "2", "4"}), "")
	s3, err := g.check(&s2, removed)
	if err != nil {
		t.Fatalf("the maker removes a member: %v", err)
	}
	departed := next(s3, ChangeDeparted, "", []string{"1"}, []string{"2", "4"})
	s4, err := g.check(&s3, departed)
	if err != nil || s4.Remover() != "2" {
		t.Fatalf("the den takes out the maker, who left it: %+v, %v", s4, err)
	}
	if _, err := g.check(&s4, g.signed(next(s4, ChangeRemoved, "4", []string{"2"}, []string{"4"}), "")); err == nil {
		t.Error("a member who joined later removed the longest")
	}
	removedAgain := g.signed(next(s4, ChangeRemoved, "2", []string{"4"}, []string{"2"}), "")
	s5, err := g.check(&s4, removedAgain)
	if err != nil {
		t.Fatalf("removing passes on to the longest member: %v", err)
	}
	key := bytes.Repeat([]byte{3}, DMKeySize)
	kid := bytes.Repeat([]byte{4}, KIDSize)
	name, err := SealGroupName(key, g.den, g.channel, kid, "Weekend")
	if err != nil {
		t.Fatal(err)
	}
	c := next(s5, ChangeNamed, "2", nil, []string{"2"})
	c.Name = &name
	named := g.signed(c, "")
	if _, err := g.check(&s5, named); err != nil {
		t.Errorf("a member names the group: %v", err)
	}
	// The name is signed: another in its place doesn't check.
	other, _ := SealGroupName(key, g.den, g.channel, kid, "Other")
	swapped := named
	swapped.Name = &other
	if VerifyChange(g.den, g.channel, swapped, g.ids["2"].Public().Hash()) {
		t.Error("a naming checked with another name in its place")
	}
	// A change is bound to its group and den.
	if VerifyChange(g.den, g.channel+1, named, g.ids["2"].Public().Hash()) ||
		VerifyChange(bytes.Repeat([]byte{8}, IDSize), g.channel, named, g.ids["2"].Public().Hash()) {
		t.Error("a change checked in another group or den")
	}

	for what, c := range map[string]GroupChange{
		"two members":      {N: 1, Prev: FirstPrev(), Kind: ChangeMade, By: "1", Subjects: []string{"2"}, After: []string{"1", "2"}},
		"its maker second": {N: 1, Prev: FirstPrev(), Kind: ChangeMade, By: "2", Subjects: []string{"1", "3"}, After: []string{"1", "2", "3"}},
		"a member twice":   {N: 1, Prev: FirstPrev(), Kind: ChangeMade, By: "1", Subjects: []string{"2", "2"}, After: []string{"1", "2", "2"}},
		"another prev":     {N: 1, Prev: bytes.Repeat([]byte{1}, 32), Kind: ChangeMade, By: "1", Subjects: []string{"2", "3"}, After: []string{"1", "2", "3"}},
		"an unknown kind":  {N: 1, Prev: FirstPrev(), Kind: "joined", By: "1", Subjects: []string{"2", "3"}, After: []string{"1", "2", "3"}},
		"an ID that isn't": {N: 1, Prev: FirstPrev(), Kind: ChangeMade, By: "1", Subjects: []string{"x", "3"}, After: []string{"1", "x", "3"}},
	} {
		if _, err := g.check(nil, g.signed(c, "1")); err == nil {
			t.Errorf("a group made with %s was taken", what)
		}
	}
}

// A group's name is sealed under its key, padded to one length, bound to
// the den, the group and the key, and cleaned as it's opened.
func TestGroupNames(t *testing.T) {
	den := bytes.Repeat([]byte{9}, IDSize)
	key, kid := bytes.Repeat([]byte{3}, DMKeySize), bytes.Repeat([]byte{4}, KIDSize)
	short, err := SealGroupName(key, den, 40, kid, "A")
	if err != nil {
		t.Fatal(err)
	}
	long, err := SealGroupName(key, den, 40, kid, "Thirty-two characters of a name.")
	if err != nil {
		t.Fatal(err)
	}
	if len(short.Sealed) != SealedGroupNameSize || len(long.Sealed) != SealedGroupNameSize {
		t.Errorf("names sealed to %d and %d bytes, not %d", len(short.Sealed), len(long.Sealed), SealedGroupNameSize)
	}
	if name, err := OpenGroupName(key, den, 40, long); err != nil || name != "Thirty-two characters of a name." {
		t.Errorf("the name opened as %q, %v", name, err)
	}
	empty, _ := SealGroupName(key, den, 40, kid, "")
	if name, err := OpenGroupName(key, den, 40, empty); err != nil || name != "" {
		t.Errorf("no name opened as %q, %v", name, err)
	}
	hidden, _ := SealGroupName(key, den, 40, kid, "  Bob‮  ")
	if name, err := OpenGroupName(key, den, 40, hidden); err != nil || name != "Bob" {
		t.Errorf("a name with a bidi override opened as %q, %v", name, err)
	}
	moved := short
	moved.KID = bytes.Repeat([]byte{5}, KIDSize)
	for what, open := range map[string]func() (string, error){
		"in another group":       func() (string, error) { return OpenGroupName(key, den, 41, short) },
		"at another den":         func() (string, error) { return OpenGroupName(key, bytes.Repeat([]byte{8}, IDSize), 40, short) },
		"under another key's ID": func() (string, error) { return OpenGroupName(key, den, 40, moved) },
		"with another key":       func() (string, error) { return OpenGroupName(bytes.Repeat([]byte{6}, DMKeySize), den, 40, short) },
	} {
		if _, err := open(); err == nil {
			t.Errorf("a name opened %s", what)
		}
	}
}

// A group's message and each member's set of reactions are signed inside
// what's sealed, by the identity its key names for whoever wrote it, so a
// member holding the key can't write as another.
func TestGroupMessagesAndSets(t *testing.T) {
	g := newGroupTest(t, "1", "2")
	key := bytes.Repeat([]byte{3}, DMKeySize)
	ad := MessageAD{DenID: g.den, Channel: g.channel, Author: 1, Nonce: bytes.Repeat([]byte{7}, NonceBytes), KeyID: 12, Revision: 1}
	p := DMPayload{Text: "hello <all>"}
	sealed, err := SealGroupMessage(key, ad, g.ids["1"], 1, p)
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := g.ids["1"].Public().Hash(), g.ids["2"].Public().Hash()
	// The identities the message's key names for its members.
	named := func(m int64) []byte {
		switch m {
		case 1:
			return alice
		case 2:
			return bob
		}
		return nil
	}
	if got, signer, err := OpenGroupMessage(key, ad, named, sealed); err != nil || got.Text != p.Text || signer != 1 {
		t.Errorf("the message opened as %q by %d, %v", got.Text, signer, err)
	}
	swapped := func(m int64) []byte { return named(3 - m) }
	if _, _, err := OpenGroupMessage(key, ad, swapped, sealed); !errors.Is(err, ErrSignature) {
		t.Errorf("a message opened as another member's identity's: %v", err)
	}
	// Bob holds the key, and writes as Alice: his signature isn't hers.
	forged, err := SealGroupMessage(key, ad, g.ids["2"], 1, DMPayload{Text: "I owe Bob"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenGroupMessage(key, ad, named, forged); !errors.Is(err, ErrSignature) {
		t.Errorf("a member wrote as another: %v", err)
	}
	// An edit, or a tick, is signed by whoever made it, whom it names.
	edit := ad
	edit.Revision = 2
	edited, _ := SealGroupMessage(key, edit, g.ids["2"], 2, DMPayload{Text: "edited"})
	if got, signer, err := OpenGroupMessage(key, edit, named, edited); err != nil || got.Text != "edited" || signer != 2 {
		t.Errorf("an edit opened as %q by %d, %v", got.Text, signer, err)
	}
	// One signed by someone its key doesn't name doesn't open.
	outsider := func(m int64) []byte {
		if m == 2 {
			return nil
		}
		return named(m)
	}
	if _, _, err := OpenGroupMessage(key, edit, outsider, edited); !errors.Is(err, ErrSignature) {
		t.Errorf("an edit by someone the key doesn't name opened: %v", err)
	}
	moved := ad
	moved.Revision = 3
	if _, _, err := OpenGroupMessage(key, moved, named, sealed); !errors.Is(err, ErrSealedOpen) {
		t.Errorf("a message opened at another revision: %v", err)
	}
	// A DM's message isn't a group's, nor a group's a DM's.
	dm, _ := SealDMMessage(key, ad, p)
	if _, _, err := OpenGroupMessage(key, ad, named, dm); err == nil {
		t.Error("a DM's message opened as a group's")
	}
	if _, err := OpenDMMessage(key, ad, sealed); err == nil {
		t.Error("a group's message opened as a DM's")
	}

	rad := ReactionsAD{DenID: g.den, Channel: g.channel, Message: 50, Member: 2, KeyID: 12, Revision: 1}
	set, err := SealGroupReactions(key, rad, g.ids["2"], ReactionSet{Emoji: []string{"👍", "party"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != SealedReactionsSize {
		t.Errorf("a group's set sealed to %d bytes, not a DM's %d", len(set), SealedReactionsSize)
	}
	if got, err := OpenGroupReactions(key, rad, bob, set); err != nil || !slices.Equal(got.Emoji, []string{"👍", "party"}) {
		t.Errorf("the set opened as %v, %v", got.Emoji, err)
	}
	if _, err := OpenGroupReactions(key, rad, alice, set); !errors.Is(err, ErrSignature) {
		t.Errorf("a set opened as another member's: %v", err)
	}
	full := make([]string, MaxReactionEmoji)
	for i := range full {
		full[i] = "party_" + string(rune('a'+i)) + "_long_name_here"
	}
	if _, err := SealGroupReactions(key, rad, g.ids["2"], ReactionSet{Emoji: full}); err != nil {
		t.Errorf("a set of %d long names didn't fit: %v", MaxReactionEmoji, err)
	}
	dmSet, _ := SealReactions(key, rad, ReactionSet{Emoji: []string{"👍"}})
	if _, err := OpenGroupReactions(key, rad, bob, dmSet); err == nil {
		t.Error("a DM's set opened as a group's")
	}
	if _, err := OpenReactions(key, rad, set); err == nil {
		t.Error("a group's set opened as a DM's")
	}
}
