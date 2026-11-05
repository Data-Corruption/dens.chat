package denclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// reactionsIn says what a page of history shows of a message's reactions:
// a channel's, each emoji (or #ID for the den's) and its members, and a
// DM's, each member's set.
func reactionsIn(t *testing.T, m *denclient.Manager, denID, channelID, messageID string) string {
	t.Helper()
	page, err := m.History(context.Background(), denID, channelID, denclient.HistoryQuery{Around: messageID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range page.Messages {
		if msg.ID != messageID {
			continue
		}
		var out []string
		for _, r := range msg.Reactions {
			e := r.Emoji
			if e == "" {
				e = "#" + r.EmojiID
			}
			out = append(out, fmt.Sprintf("%s%v", e, r.Members))
		}
		for _, s := range msg.DMReactions {
			out = append(out, fmt.Sprintf("%s@%d%v", s.MemberID, s.Revision, s.Emoji))
		}
		return strings.Join(out, " ")
	}
	t.Fatalf("no message %s", messageID)
	return ""
}

// pageEvent waits for a page event of a type.
func pageEvent(t *testing.T, stream <-chan denclient.PageEvent, typ string) json.RawMessage {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-stream:
			if !ok {
				t.Fatal("the page stream closed")
			}
			for _, ev := range e.Events {
				if ev.T == typ {
					return ev.D
				}
			}
		case <-timeout:
			t.Fatalf("no %s reached the page", typ)
		}
	}
}

func react(t *testing.T, m *denclient.Manager, denID, channelID, messageID string, r denclient.PageReaction) *denclient.PageReactionSet {
	t.Helper()
	set, err := m.React(context.Background(), denID, channelID, messageID, r)
	if err != nil {
		t.Fatalf("react %+v: %v", r, err)
	}
	return set
}

// A channel's reactions reach each member's page as the den tells of them,
// one member's one emoji at a time, and a client that caches the den keeps
// them with their message, applying each change to it; staff take an
// emoji's reactions off; and a reaction that isn't an emoji goes nowhere.
func TestChannelReactions(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	carol, stop := cachedMember(t, h, owner, denID, newCachedStore(t))
	defer stop()
	stream, done := carol.Stream()
	defer done()
	msg := send(t, member, denID, channelID, "react to this")
	// Carol's page loads it, and her client keeps it.
	if got := reactionsIn(t, carol, denID, channelID, msg.ID); got != "" {
		t.Fatalf("a new message shows %q", got)
	}
	alice, bob, carolID := me(t, owner, denID).ID, me(t, member, denID).ID, me(t, carol, denID).ID

	react(t, owner, denID, channelID, msg.ID, denclient.PageReaction{Emoji: "👍", On: true})
	var added denproto.ReactionChange
	if err := json.Unmarshal(pageEvent(t, stream, denproto.EventReactionAdded), &added); err != nil || added.MemberID != alice || added.Emoji != "👍" {
		t.Fatalf("carol's page heard %+v, %v", added, err)
	}
	react(t, carol, denID, channelID, msg.ID, denclient.PageReaction{Emoji: "👍", On: true})
	react(t, carol, denID, channelID, msg.ID, denclient.PageReaction{Emoji: "👍", On: true})
	party := addEmoji(t, owner, denID, "party", emojiPNG(t, 64, 64))
	react(t, member, denID, channelID, msg.ID, denclient.PageReaction{EmojiID: party.ID, On: true})
	want := fmt.Sprintf("👍[%s %s] #%s[%s]", alice, carolID, party.ID, bob)
	eventually(t, "carol's cache to hold the reactions", func() bool { return reactionsIn(t, carol, denID, channelID, msg.ID) == want })
	if got := reactionsIn(t, member, denID, channelID, msg.ID); got != want {
		t.Errorf("bob's history shows %q, not %q", got, want)
	}

	// Taking one back, and staff taking an emoji's off.
	react(t, carol, denID, channelID, msg.ID, denclient.PageReaction{Emoji: "👍"})
	if err := owner.ClearReaction(context.Background(), denID, msg.ID, denproto.ReactionRequest{EmojiID: party.ID}); err != nil {
		t.Fatal(err)
	}
	var cleared denproto.ReactionCleared
	if err := json.Unmarshal(pageEvent(t, stream, denproto.EventReactionCleared), &cleared); err != nil || !slices.Equal(cleared.Members, []string{bob}) {
		t.Fatalf("carol's page heard %+v cleared, %v", cleared, err)
	}
	want = fmt.Sprintf("👍[%s]", alice)
	eventually(t, "carol's cache to take them off", func() bool { return reactionsIn(t, carol, denID, channelID, msg.ID) == want })

	for _, bad := range []denclient.PageReaction{{Emoji: "a", On: true}, {Emoji: ":party:", On: true}, {Name: "party", On: true}, {On: true},
		{Emoji: "👍", EmojiID: party.ID, On: true}} {
		if _, err := carol.React(context.Background(), denID, channelID, msg.ID, bad); err == nil {
			t.Errorf("%+v went to the den", bad)
		}
	}
}

// An install that was away sees the reactions made meanwhile as it comes
// back: the den's change log names their message, though its revision
// stayed, and the client's cache takes it again as the den has it.
func TestReactionsWhileAway(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	s := newCachedStore(t)
	carol, stop := cachedMember(t, h, owner, denID, s)
	msg := send(t, member, denID, channelID, "while carol is away")
	react(t, owner, denID, channelID, msg.ID, denclient.PageReaction{Emoji: "🎉", On: true})
	alice := me(t, owner, denID).ID
	eventually(t, "carol to hold the message", func() bool {
		return reactionsIn(t, carol, denID, channelID, msg.ID) == fmt.Sprintf("🎉[%s]", alice)
	})
	stop()
	react(t, owner, denID, channelID, msg.ID, denclient.PageReaction{Emoji: "🎉"})
	react(t, member, denID, channelID, msg.ID, denclient.PageReaction{Emoji: "😮", On: true})
	// The den restarts, so carol's client comes back to a fresh snapshot,
	// and asks what changed.
	h.restart()
	changes, pages := h.changes.Load(), h.history.Load()
	carol, stop = restartMember(t, h, s, func() {})
	defer stop()
	want := fmt.Sprintf("😮[%s]", me(t, member, denID).ID)
	eventually(t, "carol's cache to take the message again", func() bool {
		return reactionsIn(t, carol, denID, channelID, msg.ID) == want
	})
	if h.changes.Load() == changes || h.history.Load() != pages {
		t.Errorf("carol's client asked for %d changes and %d pages of history", h.changes.Load()-changes, h.history.Load()-pages)
	}
}

// A DM's reactions stay sealed: each member's set leaves their Dens sealed
// with the DM's key, padded to one length, and the other member's opens
// it; the den's emoji go in it by name; and two of a member's devices
// reacting at once each keep the other's.
func TestDMReactionsStaySealed(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	msg := sendDM(t, owner, denID, dm, "a DM to react to")
	stream, done := owner.Stream()
	defer done()
	bob := me(t, member, denID).ID

	set := react(t, member, denID, dm, msg.ID, denclient.PageReaction{Emoji: "👍", On: true})
	if set == nil || set.Revision != 1 || !slices.Equal(set.Emoji, []string{"👍"}) {
		t.Fatalf("bob's set is %+v", set)
	}
	var heard denclient.PageDMReactions
	if err := json.Unmarshal(pageEvent(t, stream, denproto.EventDMReactions), &heard); err != nil ||
		heard.MemberID != bob || heard.Revision != 1 || !slices.Equal(heard.Emoji, []string{"👍"}) || heard.MessageID != msg.ID {
		t.Fatalf("alice's page heard %+v, %v", heard, err)
	}
	party := addEmoji(t, owner, denID, "party", emojiPNG(t, 64, 64))
	viewOf(t, member, denID, "the emoji", func(v denclient.View) bool { return len(v.Emoji) == 1 })
	react(t, member, denID, dm, msg.ID, denclient.PageReaction{EmojiID: party.ID, On: true})
	if got, want := reactionsIn(t, owner, denID, dm, msg.ID), fmt.Sprintf("%s@2[👍 party]", bob); got != want {
		t.Errorf("alice's history shows %q, not %q", got, want)
	}

	// The den holds a set of one length, with nothing of what's in it.
	var sealed []byte
	if err := h.s.db.QueryRow(`SELECT sealed FROM den_dm_reactions`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if len(sealed) != denproto.SealedReactionsSize || bytes.Contains(sealed, []byte("👍")) || bytes.Contains(sealed, []byte("party")) {
		t.Errorf("the den holds a set of %d bytes, readable", len(sealed))
	}

	// Two devices of a member's at once: each set made against what it
	// read meets the other's as a conflict, and tries again on it.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, e := range []string{"🎉", "😂"} {
		wg.Go(func() {
			_, errs[i] = member.React(context.Background(), denID, dm, msg.ID, denclient.PageReaction{Emoji: e, On: true})
		})
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("reacting at once: %v", errs)
	}
	got := reactionsIn(t, owner, denID, dm, msg.ID)
	for _, e := range []string{"👍", "party", "🎉", "😂"} {
		if !strings.Contains(got, e) {
			t.Errorf("after two at once, alice's history shows %q, without %s", got, e)
		}
	}
	// Taking one back, and alice's own set beside bob's.
	react(t, member, denID, dm, msg.ID, denclient.PageReaction{Name: "party"})
	react(t, owner, denID, dm, msg.ID, denclient.PageReaction{Emoji: "❤️", On: true})
	got = reactionsIn(t, member, denID, dm, msg.ID)
	if strings.Contains(got, "party") || !strings.Contains(got, "❤️") {
		t.Errorf("bob's history shows %q", got)
	}
}
