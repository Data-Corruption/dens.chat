package denclient

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func reactionEvent(t *testing.T, typ string, d any) denproto.Event {
	t.Helper()
	e, err := denproto.NewEvent(typ, 0, d)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func said(m denproto.Message) string {
	var out []string
	for _, r := range m.Reactions {
		e := r.Emoji
		if e == "" {
			e = "#" + r.EmojiID
		}
		out = append(out, fmt.Sprintf("%s%v", e, r.Members))
	}
	for _, s := range m.DMReactions {
		out = append(out, fmt.Sprintf("%s@%d", s.MemberID, s.Revision))
	}
	return strings.Join(out, " ")
}

// A reaction's event applies to its emoji's set: twice changes nothing,
// taking back what isn't there changes nothing, the last member out takes
// the emoji with them, and staff's clearing takes those it names; a DM's
// set replaces the member's where it's newer.
func TestApplyingReactions(t *testing.T) {
	m := denproto.Message{ID: "5", ChannelID: "2", AuthorID: "1", Revision: 1, Text: "hi"}
	change := func(member, emoji string) denproto.ReactionChange {
		c := denproto.ReactionChange{MessageID: "5", ChannelID: "2", MemberID: member, Emoji: emoji}
		if !denproto.IsEmoji(emoji) {
			c.Emoji, c.EmojiID = "", emoji
		}
		return c
	}
	steps := []struct {
		typ     string
		d       any
		changed bool
		want    string
	}{
		{denproto.EventReactionAdded, change("3", "👍"), true, "👍[3]"},
		{denproto.EventReactionAdded, change("3", "👍"), false, "👍[3]"},
		{denproto.EventReactionAdded, change("4", "12"), true, "👍[3] #12[4]"},
		{denproto.EventReactionAdded, change("4", "👍"), true, "👍[3 4] #12[4]"},
		{denproto.EventReactionRemoved, change("9", "👍"), false, "👍[3 4] #12[4]"},
		{denproto.EventReactionRemoved, change("3", "👍"), true, "👍[4] #12[4]"},
		{denproto.EventReactionRemoved, change("3", "👍"), false, "👍[4] #12[4]"},
		{denproto.EventReactionAdded, change("3", "🎉"), true, "👍[4] #12[4] 🎉[3]"},
		{denproto.EventReactionCleared, denproto.ReactionCleared{MessageID: "5", ChannelID: "2", EmojiID: "12", Members: []string{"4", "7"}}, true, "👍[4] 🎉[3]"},
		{denproto.EventReactionAdded, denproto.ReactionChange{MessageID: "6", ChannelID: "2", MemberID: "3", Emoji: "😂"}, false, "👍[4] 🎉[3]"},
	}
	for i, s := range steps {
		changed := applyReaction(&m, reactionEvent(t, s.typ, s.d))
		if changed != s.changed || said(m) != s.want {
			t.Fatalf("step %d: changed %v, %q; want %v, %q", i, changed, said(m), s.changed, s.want)
		}
		if err := denproto.CheckMessage(m); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	// A message holds at most 20 different emoji.
	full := denproto.Message{ID: "5", ChannelID: "2", AuthorID: "1", Revision: 1, Text: "hi"}
	for i := range denproto.MaxReactionEmoji + 1 {
		applyReaction(&full, reactionEvent(t, denproto.EventReactionAdded, change("3", fmt.Sprint(i+1))))
	}
	if len(full.Reactions) != denproto.MaxReactionEmoji {
		t.Errorf("a message took %d different emoji", len(full.Reactions))
	}

	set := func(member string, revision int) denproto.DMReactions {
		return denproto.DMReactions{MessageID: "8", ChannelID: "3", SealedReactions: denproto.SealedReactions{MemberID: member, KeyID: "1",
			Revision: revision, Sealed: make([]byte, denproto.SealedReactionsSize)}}
	}
	dm := denproto.Message{ID: "8", ChannelID: "3", AuthorID: "1", Revision: 1, Sealed: []byte("x"), KeyID: "1"}
	for i, s := range []struct {
		d       denproto.DMReactions
		changed bool
		want    string
	}{
		{set("2", 1), true, "2@1"},
		{set("2", 1), false, "2@1"},
		{set("1", 3), true, "1@3 2@1"},
		{set("1", 2), false, "1@3 2@1"},
		{set("2", 4), true, "1@3 2@4"},
	} {
		if changed := applyReaction(&dm, reactionEvent(t, denproto.EventDMReactions, s.d)); changed != s.changed || said(dm) != s.want {
			t.Fatalf("set %d: changed %v, %q; want %v, %q", i, changed, said(dm), s.changed, s.want)
		}
	}
}

// The ring hands back the events that came after a mark, while it still
// holds them, and what it holds once more come than it keeps.
func TestTallyRing(t *testing.T) {
	var r tallyRing
	at := r.mark()
	for i := range 3 {
		r.add(denproto.Event{T: fmt.Sprint(i)})
	}
	names := func(es []denproto.Event) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.T)
		}
		return out
	}
	if got := names(r.since(at)); !slices.Equal(got, []string{"0", "1", "2"}) {
		t.Errorf("since the start: %v", got)
	}
	mid := r.mark()
	r.add(denproto.Event{T: "3"})
	if got := names(r.since(mid)); !slices.Equal(got, []string{"3"}) {
		t.Errorf("since the middle: %v", got)
	}
	for i := range ringSize {
		r.add(denproto.Event{T: fmt.Sprint(i + 4)})
	}
	if got := r.since(at); len(got) != ringSize || got[0].T != "4" {
		t.Errorf("past the ring: %d, first %v", len(got), got[0].T)
	}
	if got := r.since(r.mark()); len(got) != 0 {
		t.Errorf("since now: %v", names(got))
	}
}
