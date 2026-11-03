package denclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/notify"
)

// pollIn says what a page of history shows of a message's poll: how many
// voted, the totals and the voters where they show, the member's own votes
// and whether it ended.
func pollIn(t *testing.T, m *denclient.Manager, denID, channelID, messageID string) string {
	t.Helper()
	page, err := m.History(context.Background(), denID, channelID, denclient.HistoryQuery{Around: messageID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range page.Messages {
		if msg.ID == messageID && msg.Poll != nil {
			return said(*msg.Poll)
		}
	}
	t.Fatalf("no poll on message %s", messageID)
	return ""
}

func said(p denproto.Poll) string {
	s := fmt.Sprintf("voted %d", p.Voted)
	if p.Totals != nil {
		s += fmt.Sprintf(" totals %v", p.Totals)
	}
	if p.Voters != nil {
		s += fmt.Sprintf(" voters %v", p.Voters)
	}
	if p.Mine != nil {
		s += fmt.Sprintf(" mine %v", p.Mine)
	}
	if p.Ended {
		s += " ended"
	}
	return s
}

func sendPoll(t *testing.T, m *denclient.Manager, denID, channelID, question string, p denproto.PollRequest) denclient.PageMessage {
	t.Helper()
	msg, err := m.Send(context.Background(), denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Text: question, Poll: &p})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// A poll reaches each member's page as they may see it, and its events as
// the den sends them, which a client that caches the den applies to its
// copy: here the totals show after voting, with who voted for what, so a
// member who hasn't voted hears only how many have, until they vote, and
// their vote is final; its author ends it early, and everyone hears the
// final totals. A poll that can't go doesn't reach the den.
func TestPolls(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	carol, stop := cachedMember(t, h, owner, denID, newCachedStore(t))
	defer stop()
	stream, done := carol.Stream()
	defer done()
	ctx := context.Background()
	msg := sendPoll(t, owner, denID, channelID, "Lunch?", denproto.PollRequest{Choices: []string{"Tacos", "Pho"}, Hours: 24,
		ShowVoters: denproto.VotersAlways, ShowTotals: denproto.TotalsVoted})
	if got := pollIn(t, carol, denID, channelID, msg.ID); got != "voted 0" {
		t.Fatalf("carol's page shows the new poll as %q", got)
	}
	bob, carolID := me(t, member, denID).ID, me(t, carol, denID).ID

	p, err := member.Vote(ctx, denID, msg.ID, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := said(*p), fmt.Sprintf("voted 1 totals [0 1] voters [[] [%s]] mine [1]", bob); got != want {
		t.Errorf("bob's vote answered %q, not %q", got, want)
	}
	var u denproto.PollUpdated
	if err := json.Unmarshal(pageEvent(t, stream, denproto.EventPollUpdated), &u); err != nil || u.Voted != 1 || u.Totals != nil || u.Mine != nil {
		t.Fatalf("carol's page heard %+v, %v", u, err)
	}
	eventually(t, "carol's cache to count bob's vote", func() bool { return pollIn(t, carol, denID, channelID, msg.ID) == "voted 1" })

	if _, err := carol.Vote(ctx, denID, msg.ID, []int{0}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("voted 2 totals [1 1] voters [[%s] [%s]] mine [0]", carolID, bob)
	eventually(t, "carol's cache to hold her vote and the totals", func() bool { return pollIn(t, carol, denID, channelID, msg.ID) == want })
	if _, err := carol.Vote(ctx, denID, msg.ID, []int{1}); err == nil {
		t.Error("carol changed a final vote")
	}
	if got := pollIn(t, owner, denID, channelID, msg.ID); got != "voted 2" {
		t.Errorf("alice, who didn't vote, sees %q", got)
	}

	if _, err := member.EndPoll(ctx, denID, msg.ID); err == nil {
		t.Error("bob ended alice's poll")
	}
	ended, err := owner.EndPoll(ctx, denID, msg.ID)
	if err != nil || !ended.Ended {
		t.Fatalf("alice ended her poll: %+v, %v", ended, err)
	}
	var x denproto.PollEnded
	if err := json.Unmarshal(pageEvent(t, stream, denproto.EventPollEnded), &x); err != nil || x.MessageID != msg.ID || !slices.Equal(x.Totals, []int{1, 1}) {
		t.Fatalf("carol's page heard the end as %+v, %v", x, err)
	}
	eventually(t, "carol's cache to keep the end", func() bool {
		return pollIn(t, carol, denID, channelID, msg.ID) == want+" ended"
	})

	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	good := denproto.PollRequest{Choices: []string{"a", "b"}, Hours: 1, ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive}
	for what, req := range map[string]denproto.SendRequest{
		"in a DM":          {Nonce: denproto.Random(16), Text: "In a DM?", Poll: &good},
		"with editors":     {Nonce: denproto.Random(16), Text: "Shared?", Poll: &good, Editors: []string{bob}},
		"of one choice":    {Nonce: denproto.Random(16), Text: "One?", Poll: &denproto.PollRequest{Choices: []string{"a"}, Hours: 1, ShowVoters: "never", ShowTotals: "live"}},
		"with no question": {Nonce: denproto.Random(16), Text: " ", Poll: &good},
	} {
		channel := channelID
		if what == "in a DM" {
			channel = dm
		}
		if _, err := owner.Send(ctx, denID, channel, req); err == nil {
			t.Errorf("a poll %s went", what)
		}
	}
}

// An install that was away sees the votes cast meanwhile as it comes back:
// the den's change log names the poll's message, though its revision
// stayed, and the client's cache takes it again as the den has it.
func TestPollsWhileAway(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	s := newCachedStore(t)
	carol, stop := cachedMember(t, h, owner, denID, s)
	msg := sendPoll(t, owner, denID, channelID, "Away?", denproto.PollRequest{Choices: []string{"yes", "no"}, Hours: 24,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive})
	eventually(t, "carol to hold the poll", func() bool { return pollIn(t, carol, denID, channelID, msg.ID) == "voted 0 totals [0 0]" })
	stop()
	if _, err := member.Vote(context.Background(), denID, msg.ID, []int{0}); err != nil {
		t.Fatal(err)
	}
	h.restart()
	carol, stop = restartMember(t, h, s, func() {})
	defer stop()
	eventually(t, "carol's cache to take the poll again", func() bool {
		return pollIn(t, carol, denID, channelID, msg.ID) == "voted 1 totals [1 0]"
	})
}

// Staff's @everyone counts as a mention of every member who can see the
// channel, on their own Dens as the message arrives, and reaches its
// notices as an @everyone, told from a mention by name; a member's is a
// mention of the member named everyone, if there is one.
func TestEveryoneArrives(t *testing.T) {
	heard := &notices{}
	_, owner, member, denID, general := chatDen(t, func(m *denclient.Manager) { m.Notices = heard })
	everyone := send(t, owner, denID, general, "@everyone lunch")
	if !everyone.Everyone {
		t.Fatal("the owner's @everyone isn't marked")
	}
	if n := heard.waitMessage(t, everyone.ID); !n.Everyone || n.Mention {
		t.Errorf("the @everyone reached bob's notices as %+v", n)
	}
	viewOf(t, member, denID, "the @everyone counted", func(v denclient.View) bool {
		i := slices.IndexFunc(v.ReadStates, func(r denproto.ReadState) bool { return r.ChannelID == general })
		return i >= 0 && v.ReadStates[i].MentionCount == 1
	})
	both := send(t, owner, denID, general, "@everyone and @bob")
	if n := heard.waitMessage(t, both.ID); n != (notify.Message{Den: denID, DenName: "Chat Den", Channel: general, ChannelName: "general",
		ID: both.ID, Author: "Alice", Mention: true, Everyone: true, Text: "@everyone and @bob"}) {
		t.Errorf("an @everyone naming bob reached his notices as %+v", n)
	}
	mine := send(t, member, denID, general, "@everyone from bob")
	if mine.Everyone {
		t.Error("a member's @everyone is marked")
	}
}
