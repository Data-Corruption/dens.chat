package denproto

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// A DM's set seals to one length whatever it holds, so its size says
// nothing of how many; opens only with its own bindings; and sealing
// refuses what isn't an emoji or a name, twice, or past the most.
func TestSealedReactionSets(t *testing.T) {
	key := Random(DMKeySize)
	ad := ReactionsAD{DenID: Random(IDSize), Channel: 3, Message: 41, Member: 7, KeyID: 2, Revision: 5}
	longest := "\U0001F9D1\U0001F3FB‍❤️‍\U0001F48B‍\U0001F9D1\U0001F3FC"
	full := []string{longest}
	for _, e := range []string{"😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "🙃", "🫠", "😉", "😊", "😇", "🥰", "😍", "🤩", "😘"} {
		full = append(full, e)
	}
	full = append(full, strings.Repeat("n", 32))
	if len(full) != MaxReactionEmoji || !IsEmoji(longest) {
		t.Fatalf("the full set holds %d", len(full))
	}
	for _, set := range [][]string{nil, {"👍"}, {"👍", "party_parrot", "🧑🏽‍🤝‍🧑🏿"}, full} {
		sealed, err := SealReactions(key, ad, ReactionSet{Emoji: set})
		if err != nil {
			t.Fatalf("seal %q: %v", set, err)
		}
		if len(sealed) != SealedReactionsSize {
			t.Errorf("a set of %d seals to %d bytes, not %d", len(set), len(sealed), SealedReactionsSize)
		}
		got, err := OpenReactions(key, ad, sealed)
		if err != nil || !slices.Equal(got.Emoji, append([]string{}, set...)) {
			t.Errorf("%q opens as %q, %v", set, got.Emoji, err)
		}
	}
	sealed, err := SealReactions(key, ad, ReactionSet{Emoji: []string{"👍"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, moved := range map[string]ReactionsAD{
		"den":      {DenID: Random(IDSize), Channel: 3, Message: 41, Member: 7, KeyID: 2, Revision: 5},
		"DM":       {DenID: ad.DenID, Channel: 4, Message: 41, Member: 7, KeyID: 2, Revision: 5},
		"message":  {DenID: ad.DenID, Channel: 3, Message: 42, Member: 7, KeyID: 2, Revision: 5},
		"member":   {DenID: ad.DenID, Channel: 3, Message: 41, Member: 8, KeyID: 2, Revision: 5},
		"key":      {DenID: ad.DenID, Channel: 3, Message: 41, Member: 7, KeyID: 1, Revision: 5},
		"revision": {DenID: ad.DenID, Channel: 3, Message: 41, Member: 7, KeyID: 2, Revision: 4},
	} {
		if _, err := OpenReactions(key, moved, sealed); err == nil {
			t.Errorf("a set opens with another %s", name)
		}
	}
	if _, err := OpenReactions(Random(DMKeySize), ad, sealed); err == nil {
		t.Error("a set opens with another key")
	}
	if _, err := OpenReactions(key, ad, sealed[:len(sealed)-1]); err == nil {
		t.Error("a set of another length opens")
	}
	for _, bad := range [][]string{{"a"}, {"Party"}, {"👍", "👍"}, {":party:"}, {"<b>"}, {"👍👍"}, append(slices.Clone(full), "🎉")} {
		if _, err := SealReactions(key, ad, ReactionSet{Emoji: bad}); err == nil {
			t.Errorf("%q sealed", bad)
		}
	}
}

// A set the other member's client wrote keeps only Unicode's emoji from
// the list and names the den's emoji may have, each once, and at most the
// most a message holds.
func TestOpenedSetsDropTheRest(t *testing.T) {
	key := Random(DMKeySize)
	ad := ReactionsAD{DenID: Random(IDSize), Channel: 3, Message: 41, Member: 7, KeyID: 2, Revision: 1}
	hostile := `{"emoji":["👍","hello world","‮party","PARTY","party","👍","<img src=x>","👍👍","🏻","x","` +
		strings.Repeat("y", 33) + `","🎉"]}`
	var many []string
	for _, e := range []string{"😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "🙃", "🫠", "😉", "😊", "😇", "🥰", "😍", "🤩", "😘", "😗", "😚", "😙", "😋"} {
		many = append(many, `"`+e+`"`)
	}
	cases := map[string][]string{
		hostile: {"👍", "party", "🎉"},
		`{"emoji":[` + strings.Join(many, ",") + `]}`: {"😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "🙃", "🫠", "😉", "😊", "😇", "🥰", "😍", "🤩", "😘", "😗", "😚"},
	}
	for plain, want := range cases {
		padded := append([]byte(plain), bytes.Repeat([]byte{' '}, reactionsPlain-len(plain))...)
		sealed, err := sealWith(key, padded, ad.bytes())
		if err != nil {
			t.Fatal(err)
		}
		got, err := OpenReactions(key, ad, sealed)
		if err != nil || !slices.Equal(got.Emoji, want) {
			t.Errorf("%s opens as %q, %v; want %q", plain[:40], got.Emoji, err, want)
		}
	}
	for _, plain := range []string{`{"emoji":"👍"}`, `{"emoji":[1]}`, `not json`} {
		padded := append([]byte(plain), bytes.Repeat([]byte{' '}, reactionsPlain-len(plain))...)
		sealed, _ := sealWith(key, padded, ad.bytes())
		if _, err := OpenReactions(key, ad, sealed); err == nil {
			t.Errorf("%s opens", plain)
		}
	}
}

func TestReactionRequests(t *testing.T) {
	for _, ok := range []ReactionRequest{{Emoji: "👍"}, {Emoji: "👍🏿"}, {Emoji: "🇺🇸"}, {EmojiID: "12"}} {
		if err := ok.Check(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, bad := range []ReactionRequest{{}, {Emoji: "a"}, {Emoji: "👍", EmojiID: "1"}, {EmojiID: "0"}, {EmojiID: "x"}, {Emoji: "❤"}} {
		if err := bad.Check(); err == nil {
			t.Errorf("%+v passes", bad)
		}
	}
}

// A message from a den carries reactions a client keeps only within what
// a message holds: a channel's, each emoji once with its members, and a
// DM's sealed, a set a member.
func TestMessagesCarryTheirReactions(t *testing.T) {
	base := Message{ID: "5", ChannelID: "2", AuthorID: "1", CreatedAt: 1, Revision: 1, Text: "hi"}
	dm := Message{ID: "6", ChannelID: "3", AuthorID: "1", CreatedAt: 1, Revision: 1, Nonce: Random(NonceBytes), Sealed: Random(100), KeyID: "1"}
	set := SealedReactions{MemberID: "1", KeyID: "1", Revision: 1, Sealed: Random(SealedReactionsSize)}
	with := func(m Message, rs ...Reaction) Message {
		m.Reactions = rs
		return m
	}
	sets := func(m Message, rs ...SealedReactions) Message {
		m.DMReactions = rs
		return m
	}
	var twenty, many []Reaction
	for i := range MaxReactionEmoji + 1 {
		twenty = append(twenty, Reaction{EmojiID: FormatID(int64(i + 1)), Members: []string{"1"}})
	}
	members := make([]string, MaxReactions/2+1)
	for i := range members {
		members[i] = FormatID(int64(i + 1))
	}
	many = []Reaction{{Emoji: "👍", Members: members}, {Emoji: "🎉", Members: members}}
	for name, c := range map[string]struct {
		m  Message
		ok bool
	}{
		"none":            {base, true},
		"some":            {with(base, Reaction{Emoji: "👍", Members: []string{"1", "2"}}, Reaction{EmojiID: "4", Members: []string{"2"}}), true},
		"twenty":          {with(base, twenty[:MaxReactionEmoji]...), true},
		"twenty-one":      {with(base, twenty...), false},
		"too many":        {with(base, many...), false},
		"twice":           {with(base, Reaction{Emoji: "👍", Members: []string{"1"}}, Reaction{Emoji: "👍", Members: []string{"2"}}), false},
		"member twice":    {with(base, Reaction{Emoji: "👍", Members: []string{"1", "1"}}), false},
		"nobody":          {with(base, Reaction{Emoji: "👍"}), false},
		"text":            {with(base, Reaction{Emoji: "hi", Members: []string{"1"}}), false},
		"both":            {with(base, Reaction{Emoji: "👍", EmojiID: "1", Members: []string{"1"}}), false},
		"a DM's sets":     {sets(dm, set, SealedReactions{MemberID: "2", KeyID: "1", Revision: 3, Sealed: Random(SealedReactionsSize)}), true},
		"a set twice":     {sets(dm, set, set), false},
		"a short set":     {sets(dm, SealedReactions{MemberID: "1", KeyID: "1", Revision: 1, Sealed: Random(10)}), false},
		"revision 0":      {sets(dm, SealedReactions{MemberID: "1", KeyID: "1", Sealed: Random(SealedReactionsSize)}), false},
		"a DM's in clear": {with(dm, Reaction{Emoji: "👍", Members: []string{"1"}}), false},
		"a channel's set": {sets(base, set), false},
	} {
		if err := CheckMessage(c.m); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	// What a client keeps of a message reads back the same.
	m := with(base, Reaction{Emoji: "👍", Members: []string{"1"}})
	b, _ := json.Marshal(m)
	var back Message
	if err := json.Unmarshal(b, &back); err != nil || !slices.EqualFunc(back.Reactions, m.Reactions, func(a, b Reaction) bool {
		return a.Emoji == b.Emoji && slices.Equal(a.Members, b.Members)
	}) {
		t.Errorf("%s reads back as %+v, %v", b, back, err)
	}
}
