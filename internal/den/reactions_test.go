package den

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// reactionsOn returns a message's reactions as a member's history has
// them, each as emoji (or #ID for the den's) and its members.
func (f *fixture) reactionsOn(s *Session, channel, message string) []string {
	f.t.Helper()
	h, err := f.d.History(context.Background(), s, channel, HistoryQuery{Around: mustInt(f.t, message), Limit: 1})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, m := range h.Messages {
		if m.ID != message {
			continue
		}
		if err := denproto.CheckMessage(m); err != nil {
			f.t.Fatalf("message %s: %v", message, err)
		}
		var out []string
		for _, r := range m.Reactions {
			e := r.Emoji
			if e == "" {
				e = "#" + r.EmojiID
			}
			out = append(out, fmt.Sprintf("%s%v", e, r.Members))
		}
		return out
	}
	f.t.Fatalf("no message %s in history", message)
	return nil
}

func (f *fixture) messageNow(s *Session, channel, message string) denproto.Message {
	f.t.Helper()
	h, err := f.d.History(context.Background(), s, channel, HistoryQuery{Around: mustInt(f.t, message), Limit: 1})
	if err != nil || len(h.Messages) == 0 || h.Messages[0].ID != message {
		f.t.Fatalf("message %s: %v", message, err)
	}
	return h.Messages[0]
}

// changedSince lists the messages the change log says changed after a
// version, with their revisions.
func (f *fixture) changedSince(s *Session, version int64) map[string]int {
	f.t.Helper()
	c, err := f.d.Changes(context.Background(), s, version, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]int{}
	for _, m := range c.Messages {
		out[m.ID] = m.Revision
	}
	return out
}

func (f *fixture) version() int64 {
	f.t.Helper()
	s, err := syncState(context.Background(), f.db)
	if err != nil {
		f.t.Fatal(err)
	}
	return s.Version
}

func sid(s *Session) string { return denproto.FormatID(s.MemberID) }

// TestReactionsInAChannel checks a channel's reactions: one of Unicode's
// emoji from the list, or one of the den's while emoji are on, and nothing
// else; each a member's once, reacting again changing nothing; their order;
// taking one back; the events, which those who see the channel hear; the
// change log's record of the message, its revision as it was; and a
// message's 20 different emoji.
func TestReactionsInAChannel(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	staff := f.newChannel(owner, "staff", denproto.ChannelRequest{StaffOnly: ptr(true)})
	m := f.send(owner, c.ID, "react to this")
	hidden := f.send(owner, staff.ID, "staff only")
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	before := f.version()

	thumbs := denproto.ReactionRequest{Emoji: "👍"}
	if err := f.d.React(ctx, member, m.ID, thumbs); err != nil {
		t.Fatal(err)
	}
	if got := nextEvent[denproto.ReactionChange](t, sub, denproto.EventReactionAdded); got != (denproto.ReactionChange{
		MessageID: m.ID, ChannelID: c.ID, MemberID: sid(member), Emoji: "👍"}) {
		t.Errorf("members heard of %+v", got)
	}
	// Again, it changes nothing, and nobody hears of it.
	if err := f.d.React(ctx, member, m.ID, thumbs); err != nil {
		t.Fatal(err)
	}
	if err := f.d.React(ctx, owner, m.ID, thumbs); err != nil {
		t.Fatal(err)
	}
	if got := nextEvent[denproto.ReactionChange](t, sub, denproto.EventReactionAdded); got.MemberID != sid(owner) {
		t.Errorf("the second reaction heard was %+v, not the owner's", got)
	}
	if err := f.d.React(ctx, member, m.ID, denproto.ReactionRequest{Emoji: "👍🏽"}); err != nil {
		t.Fatal(err)
	}
	want := []string{fmt.Sprintf("👍[%s %s]", sid(member), sid(owner)), fmt.Sprintf("👍🏽[%s]", sid(member))}
	if got := f.reactionsOn(owner, c.ID, m.ID); !slices.Equal(got, want) {
		t.Errorf("history has %q, not %q", got, want)
	}
	// The change log records the message, at the revision it had.
	if got := f.changedSince(member, before); got[m.ID] != 1 {
		t.Errorf("the changes since the reactions are %v", got)
	}
	if got := f.messageNow(member, c.ID, m.ID); got.Revision != 1 || got.EditedAt != 0 {
		t.Errorf("a reaction changed the message's revision or marked it edited: %+v", got)
	}

	// Only an emoji from the list, or the den's, by ID, once.
	for _, bad := range []denproto.ReactionRequest{{Emoji: "a"}, {Emoji: ":)"}, {Emoji: "👍👍"}, {Emoji: "🏻"}, {Emoji: "❤"},
		{Emoji: "👍 "}, {Emoji: "‮👍"}, {EmojiID: "x"}, {Emoji: "👍", EmojiID: "1"}, {}} {
		wantCode(t, f.d.React(ctx, member, m.ID, bad), denproto.CodeInvalidField)
	}
	wantCode(t, f.d.React(ctx, member, hidden.ID, thumbs), denproto.CodeNotFound)
	wantCode(t, f.d.React(ctx, member, "999999", thumbs), denproto.CodeNotFound)

	party := f.mustAddEmoji(owner, "party", testPNG(t, 64, 64))
	if err := f.d.React(ctx, member, m.ID, denproto.ReactionRequest{EmojiID: party.ID}); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.d.React(ctx, member, m.ID, denproto.ReactionRequest{EmojiID: "999999"}), denproto.CodeInvalidField)
	f.setEmojiLimits(owner, denproto.EmojiLimits{Emojis: 0})
	wantCode(t, f.d.React(ctx, owner, m.ID, denproto.ReactionRequest{EmojiID: party.ID}), denproto.CodeInvalidField)
	f.setEmojiLimits(owner, denproto.DefaultEmojiLimits)
	if got := f.reactionsOn(member, c.ID, m.ID); len(got) != 3 || got[2] != fmt.Sprintf("#%s[%s]", party.ID, sid(member)) {
		t.Errorf("with the den's emoji, history has %q", got)
	}

	// Taking one back.
	if err := f.d.Unreact(ctx, member, m.ID, thumbs); err != nil {
		t.Fatal(err)
	}
	if got := nextEvent[denproto.ReactionChange](t, sub, denproto.EventReactionRemoved); got.MemberID != sid(member) || got.Emoji != "👍" {
		t.Errorf("members heard of %+v taken back", got)
	}
	if err := f.d.Unreact(ctx, member, m.ID, thumbs); err != nil {
		t.Fatal(err)
	}
	if got := f.reactionsOn(owner, c.ID, m.ID); got[0] != fmt.Sprintf("👍[%s]", sid(owner)) {
		t.Errorf("after taking one back, history has %q", got)
	}

	// A message holds 20 different emoji, which more members may add to.
	list := []string{"😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "🙃", "🫠", "😉", "😊", "😇", "🥰", "😍", "🤩", "😘", "😗", "😚", "😙"}
	m2 := f.send(owner, c.ID, "twenty")
	for _, e := range list[:denproto.MaxReactionEmoji] {
		if err := f.d.React(ctx, owner, m2.ID, denproto.ReactionRequest{Emoji: e}); err != nil {
			t.Fatalf("%s: %v", e, err)
		}
	}
	wantCode(t, f.d.React(ctx, member, m2.ID, denproto.ReactionRequest{Emoji: list[20]}), denproto.CodeInvalidField)
	if err := f.d.React(ctx, member, m2.ID, denproto.ReactionRequest{Emoji: list[0]}); err != nil {
		t.Errorf("one of the twenty again: %v", err)
	}
}

// TestStaffClearReactions checks that staff take one emoji's reactions off
// a channel's message, those of the members they outrank alone, and that a
// DM's reactions are its members' alone.
func TestStaffClearReactions(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	mod := f.moderator(owner, "carol")
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	m := f.send(member, c.ID, "a message")
	for _, s := range []*Session{member, mod, owner} {
		if err := f.d.React(ctx, s, m.ID, denproto.ReactionRequest{Emoji: "🎉"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.d.React(ctx, member, m.ID, denproto.ReactionRequest{Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	before := f.version()

	wantCode(t, f.d.ClearReactions(ctx, member, m.ID, denproto.ReactionRequest{Emoji: "🎉"}), denproto.CodeForbidden)
	if err := f.d.ClearReactions(ctx, mod, m.ID, denproto.ReactionRequest{Emoji: "🎉"}); err != nil {
		t.Fatal(err)
	}
	if got := nextEvent[denproto.ReactionCleared](t, sub, denproto.EventReactionCleared); !slices.Equal(got.Members, []string{sid(member)}) ||
		got.Emoji != "🎉" || got.MessageID != m.ID || got.ChannelID != c.ID {
		t.Errorf("members heard of %+v cleared", got)
	}
	want := []string{fmt.Sprintf("🎉[%s %s]", sid(mod), sid(owner)), fmt.Sprintf("👍[%s]", sid(member))}
	if got := f.reactionsOn(owner, c.ID, m.ID); !slices.Equal(got, want) {
		t.Errorf("after a moderator's clearing, history has %q, not %q", got, want)
	}
	if got := f.changedSince(owner, before); got[m.ID] != 1 {
		t.Errorf("the change log after clearing has %v", got)
	}
	if err := f.d.ClearReactions(ctx, owner, m.ID, denproto.ReactionRequest{Emoji: "🎉"}); err != nil {
		t.Fatal(err)
	}
	want = []string{fmt.Sprintf("🎉[%s]", sid(owner)), fmt.Sprintf("👍[%s]", sid(member))}
	if got := f.reactionsOn(owner, c.ID, m.ID); !slices.Equal(got, want) {
		t.Errorf("after the owner's clearing, history has %q, not %q", got, want)
	}
	// Nothing left that they outrank: nothing happens.
	if err := f.d.ClearReactions(ctx, mod, m.ID, denproto.ReactionRequest{Emoji: "🎉"}); err != nil {
		t.Fatal(err)
	}

	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: sid(owner)})
	if err != nil {
		t.Fatal(err)
	}
	d := f.send(member, dm.ID, "in a DM")
	wantCode(t, f.d.ClearReactions(ctx, owner, d.ID, denproto.ReactionRequest{Emoji: "🎉"}), denproto.CodeForbidden)
	wantCode(t, f.d.React(ctx, owner, d.ID, denproto.ReactionRequest{Emoji: "🎉"}), denproto.CodeInvalidField)
}

// TestDeletedEmojiTakesItsReactions checks that one of the den's emoji
// takes its reactions as staff delete it, and the change log records their
// messages.
func TestDeletedEmojiTakesItsReactions(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	m := f.send(owner, c.ID, "a message")
	other := f.send(owner, c.ID, "another")
	party := f.mustAddEmoji(owner, "party", testPNG(t, 64, 64))
	if err := f.d.React(ctx, member, m.ID, denproto.ReactionRequest{EmojiID: party.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.React(ctx, member, m.ID, denproto.ReactionRequest{Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	before := f.version()
	if err := f.d.DeleteEmoji(ctx, owner, party.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.reactionsOn(owner, c.ID, m.ID); !slices.Equal(got, []string{fmt.Sprintf("👍[%s]", sid(member))}) {
		t.Errorf("after the emoji went, history has %q", got)
	}
	changed := f.changedSince(owner, before)
	if _, ok := changed[m.ID]; !ok || len(changed) != 1 {
		t.Errorf("the change log after the emoji went has %v, not %s alone (not %s)", changed, m.ID, other.ID)
	}
}

// TestBanTakesRecentReactions checks that a ban that deletes a member's
// recent messages takes the reactions they added in that time too, in
// channels, telling those who see each, and leaves those from before.
func TestBanTakesRecentReactions(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	old := f.send(owner, c.ID, "old")
	if err := f.d.React(ctx, member, old.ID, denproto.ReactionRequest{Emoji: "😀"}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(2 * time.Hour)
	recent := f.send(owner, c.ID, "recent")
	spam := f.send(member, c.ID, "spam")
	for _, msg := range []denproto.Message{old, recent, spam} {
		if err := f.d.React(ctx, member, msg.ID, denproto.ReactionRequest{Emoji: "💩"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.d.React(ctx, owner, recent.ID, denproto.ReactionRequest{Emoji: "💩"}); err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	before := f.version()
	if err := f.d.Remove(ctx, owner, sid(member), denproto.RemoveRequest{Ban: true, DeleteMessages: 3600}); err != nil {
		t.Fatal(err)
	}
	var heard []string
	for range 2 {
		e := nextEvent[denproto.ReactionChange](t, sub, denproto.EventReactionRemoved)
		heard = append(heard, e.MessageID+e.Emoji)
	}
	slices.Sort(heard)
	if want := []string{old.ID + "💩", recent.ID + "💩"}; !slices.Equal(heard, want) {
		t.Errorf("the ban told of %q taken off, not %q", heard, want)
	}
	if got := f.reactionsOn(owner, c.ID, old.ID); !slices.Equal(got, []string{fmt.Sprintf("😀[%s]", sid(member))}) {
		t.Errorf("the reaction from before the ban's time went too: %q", got)
	}
	if got := f.reactionsOn(owner, c.ID, recent.ID); !slices.Equal(got, []string{fmt.Sprintf("💩[%s]", sid(owner))}) {
		t.Errorf("after the ban, the recent message's reactions are %q", got)
	}
	changed := f.changedSince(owner, before)
	if _, ok := changed[recent.ID]; !ok {
		t.Errorf("the change log after the ban has %v", changed)
	}
}

// sealSet seals a member's set on a DM's message as their client does.
func (f *fixture) sealSet(k testKey, channel, message string, member int64, revision int, set ...string) denproto.Bytes {
	f.t.Helper()
	info, _ := f.d.Info()
	ch, _ := denproto.ParseID(channel)
	mid, _ := denproto.ParseID(message)
	kid, _ := denproto.ParseID(k.id)
	sealed, err := denproto.SealReactions(k.key, denproto.ReactionsAD{DenID: info.ID, Channel: ch, Message: mid, Member: member,
		KeyID: kid, Revision: revision}, denproto.ReactionSet{Emoji: set})
	if err != nil {
		f.t.Fatal(err)
	}
	return sealed
}

// openSet opens a member's set as their partner's client does.
func (f *fixture) openSet(k testKey, channel, message string, r denproto.SealedReactions) ([]string, error) {
	info, _ := f.d.Info()
	ch, _ := denproto.ParseID(channel)
	mid, _ := denproto.ParseID(message)
	kid, _ := denproto.ParseID(r.KeyID)
	who, _ := denproto.ParseID(r.MemberID)
	set, err := denproto.OpenReactions(k.key, denproto.ReactionsAD{DenID: info.ID, Channel: ch, Message: mid, Member: who,
		KeyID: kid, Revision: r.Revision}, r.Sealed)
	return set.Emoji, err
}

// TestDMReactionsSealed checks a DM's reactions: each member's set, sealed
// by their client with the DM's live key at the revision after the one it
// was made against, which the den keeps whole, refusing one made against
// another as a conflict that carries the set as it is; both members
// hearing of it, and history carrying it with its message, where it opens
// as its member sealed it, and nowhere else; the change log's record, the
// revision as it was; and a channel's ways refused.
func TestDMReactionsSealed(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: sid(owner)})
	if err != nil {
		t.Fatal(err)
	}
	m := f.send(owner, dm.ID, "a DM")
	other := f.send(owner, dm.ID, "another")
	k := f.keys[dm.ID]
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	before := f.version()

	set := denproto.DMReactionsRequest{Revision: 0, KeyID: k.id, Sealed: f.sealSet(k, dm.ID, m.ID, member.MemberID, 1, "👍", "party")}
	got, err := f.d.SetDMReactions(ctx, member, m.ID, set)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.MemberID != sid(member) || got.KeyID != k.id || len(got.Sealed) != denproto.SealedReactionsSize {
		t.Errorf("the den kept %+v", got)
	}
	heard := nextEvent[denproto.DMReactions](t, sub, denproto.EventDMReactions)
	if heard.MessageID != m.ID || heard.ChannelID != dm.ID || heard.Revision != 1 || heard.MemberID != sid(member) {
		t.Errorf("the other member heard of %+v", heard)
	}
	held := f.messageNow(owner, dm.ID, m.ID)
	if len(held.DMReactions) != 1 || len(held.Reactions) != 0 {
		t.Fatalf("history carries %+v", held)
	}
	if opened, err := f.openSet(k, dm.ID, m.ID, held.DMReactions[0]); err != nil || !slices.Equal(opened, []string{"👍", "party"}) {
		t.Errorf("the set opens as %q, %v", opened, err)
	}
	// Moved to another message, member or revision, it doesn't open.
	moved := held.DMReactions[0]
	if _, err := f.openSet(k, dm.ID, other.ID, moved); err == nil {
		t.Error("a set opens on another message")
	}
	moved.MemberID = sid(owner)
	if _, err := f.openSet(k, dm.ID, m.ID, moved); err == nil {
		t.Error("a set opens as another member's")
	}
	moved = held.DMReactions[0]
	moved.Revision = 2
	if _, err := f.openSet(k, dm.ID, m.ID, moved); err == nil {
		t.Error("a set opens at another revision")
	}
	if got := f.changedSince(owner, before); got[m.ID] != 1 {
		t.Errorf("the change log after a set has %v", got)
	}

	// Another device's set made against revision 0 meets a conflict, with
	// the set as it is now.
	_, err = f.d.SetDMReactions(ctx, member, m.ID, denproto.DMReactionsRequest{Revision: 0, KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, m.ID, member.MemberID, 1, "🎉")})
	current, conflict := ReactionsConflict(err)
	if !conflict || current == nil || current.Revision != 1 {
		t.Fatalf("a stale set got %v, %+v", err, current)
	}
	if _, err := f.d.SetDMReactions(ctx, member, m.ID, denproto.DMReactionsRequest{Revision: 1, KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, m.ID, member.MemberID, 2, "👍", "party", "🎉")}); err != nil {
		t.Fatal(err)
	}
	// The owner's own set, against none, is theirs alone.
	if _, err := f.d.SetDMReactions(ctx, owner, m.ID, denproto.DMReactionsRequest{Revision: 3, KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, m.ID, owner.MemberID, 4, "😂")}); err == nil {
		t.Error("a first set made against revision 3 was kept")
	} else if current, ok := ReactionsConflict(err); !ok || current != nil {
		t.Errorf("a first set against another revision got %v, %+v", err, current)
	}
	if _, err := f.d.SetDMReactions(ctx, owner, m.ID, denproto.DMReactionsRequest{Revision: 0, KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, m.ID, owner.MemberID, 1, "😂")}); err != nil {
		t.Fatal(err)
	}
	held = f.messageNow(member, dm.ID, m.ID)
	if len(held.DMReactions) != 2 || held.DMReactions[0].MemberID != sid(owner) || held.DMReactions[1].Revision != 2 || held.Revision != 1 {
		t.Errorf("history carries %+v", held.DMReactions)
	}

	// A set of another size, or not with the live key, is refused, and so
	// are a channel's ways in a DM, and a DM's in a channel.
	_, err = f.d.SetDMReactions(ctx, member, m.ID, denproto.DMReactionsRequest{Revision: 2, KeyID: k.id, Sealed: denproto.Random(64)})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.SetDMReactions(ctx, member, m.ID, denproto.DMReactionsRequest{Revision: 2, KeyID: "999999",
		Sealed: f.sealSet(k, dm.ID, m.ID, member.MemberID, 3)})
	wantCode(t, err, denproto.CodeInvalidField)
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	cm := f.send(owner, c.ID, "in a channel")
	_, err = f.d.SetDMReactions(ctx, member, cm.ID, denproto.DMReactionsRequest{Revision: 0, KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, cm.ID, member.MemberID, 1)})
	wantCode(t, err, denproto.CodeInvalidField)
	carol := f.member(owner, "carol")
	_, err = f.d.SetDMReactions(ctx, carol, m.ID, denproto.DMReactionsRequest{Revision: 0, KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, m.ID, carol.MemberID, 1)})
	wantCode(t, err, denproto.CodeNotFound)

	// Once the other member has left, nothing more changes.
	if err := f.d.Leave(ctx, member); err != nil {
		t.Fatal(err)
	}
	_, err = f.d.SetDMReactions(ctx, owner, m.ID, denproto.DMReactionsRequest{Revision: 1, KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, m.ID, owner.MemberID, 2)})
	wantCode(t, err, denproto.CodeForbidden)
}

// TestReactionsGoWithTheirMessage checks that a message's reactions, a
// channel's and a DM's, go with it.
func TestReactionsGoWithTheirMessage(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	m := f.send(member, c.ID, "going")
	if err := f.d.React(ctx, owner, m.ID, denproto.ReactionRequest{Emoji: "👋"}); err != nil {
		t.Fatal(err)
	}
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: sid(owner)})
	if err != nil {
		t.Fatal(err)
	}
	d := f.send(member, dm.ID, "going too")
	k := f.keys[dm.ID]
	if _, err := f.d.SetDMReactions(ctx, owner, d.ID, denproto.DMReactionsRequest{KeyID: k.id,
		Sealed: f.sealSet(k, dm.ID, d.ID, owner.MemberID, 1, "👋")}); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []denproto.Message{m, d} {
		if err := f.d.Delete(ctx, member, msg.ID); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := f.db.QueryRow(`SELECT (SELECT count(*) FROM den_reactions) + (SELECT count(*) FROM den_dm_reactions)`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d reactions stayed after their messages, %v", n, err)
	}
}
