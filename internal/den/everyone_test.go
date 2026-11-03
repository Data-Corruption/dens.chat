package den

import (
	"context"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestEveryone checks staff's @everyone (M15.4): it marks the message and
// mentions each member who can see the channel but its author, a row each,
// staff alone in a staff-only channel; anyone else's mentions a member of
// that name, whom a den may have from before, and nobody new takes it.
func TestEveryone(t *testing.T) {
	f, alice, bob := chatFixture(t)
	carol := f.member(alice, "carol")
	mod := f.moderator(alice, "dave")
	c := f.newChannel(alice, "general", denproto.ChannelRequest{})
	staff := f.newChannel(alice, "staff", denproto.ChannelRequest{StaffOnly: ptr(true)})
	mentions := func(s *Session, channel string) int { return f.mentionCount(s, channel) }

	m := f.send(mod, c.ID, "**@everyone** lunch is here")
	if !m.Everyone || !f.messageNow(bob, c.ID, m.ID).Everyone {
		t.Error("a moderator's @everyone isn't marked")
	}
	for name, s := range map[string]*Session{"alice": alice, "bob": bob, "carol": carol} {
		if got := mentions(s, c.ID); got != 1 {
			t.Errorf("%s has %d mentions from staff's @everyone", name, got)
		}
	}
	if got := mentions(mod, c.ID); got != 0 {
		t.Errorf("the author has %d mentions from their own @everyone", got)
	}
	f.send(alice, staff.ID, "@everyone a staff meeting")
	if got := mentions(mod, staff.ID); got != 1 {
		t.Errorf("the moderator has %d mentions from the owner's @everyone among staff", got)
	}
	var rows int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_mentions WHERE channel_id = ?`, mustInt(t, staff.ID)).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("an @everyone among staff wrote %d rows, %v", rows, err)
	}
	// In code it's no mention, as for a name.
	if x := f.send(alice, c.ID, "`@everyone` is how staff ping"); x.Everyone {
		t.Error("an @everyone in code was marked")
	}

	// A member's mentions nobody but a member of the name.
	owner := mentions(alice, c.ID)
	x := f.send(bob, c.ID, "@everyone hello")
	if x.Everyone || mentions(carol, c.ID) != 1 {
		t.Errorf("a member's @everyone marked %v, carol at %d mentions", x.Everyone, mentions(carol, c.ID))
	}
	// Nobody new takes the name, in any case.
	for _, name := range []string{"everyone", "Everyone"} {
		_, err := f.join(f.invite(alice, 1).Code, name, newDevice())
		wantCode(t, err, denproto.CodeUsernameTaken)
	}
	// A member who had it from before keeps it, and anyone's @everyone
	// mentions them.
	if _, err := f.db.Exec(`UPDATE den_members SET username = ? WHERE id = ?`, denproto.Everyone, carol.MemberID); err != nil {
		t.Fatal(err)
	}
	f.send(bob, c.ID, "@everyone, that's you")
	if got := mentions(carol, c.ID); got != 2 {
		t.Errorf("the member named everyone has %d mentions after a member's @everyone", got)
	}
	if got := mentions(alice, c.ID); got != owner {
		t.Errorf("a member's @everyone mentioned the owner: %d mentions, not %d", got, owner)
	}
}

// TestEveryoneEdited checks @everyone through edits: it counts while the
// text holds it and staff put it there, so a co-editor who isn't staff
// takes it out but can't put it in; an edit recounts its rows, as for a
// name, mentioning neither the author nor the one who edits.
func TestEveryoneEdited(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(alice, "carol")
	c := f.newChannel(alice, "general", denproto.ChannelRequest{})
	m, err := f.sendShared(alice, c.ID, "@everyone the plan", bob)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Everyone {
		t.Fatal("the owner's @everyone isn't marked")
	}
	mentions := func(s *Session) int { return f.mentionCount(s, c.ID) }
	edit := func(s *Session, text string) denproto.Message {
		t.Helper()
		got, err := f.d.Edit(ctx, s, m.ID, denproto.EditRequest{Revision: m.Revision, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		m = got
		return got
	}
	for _, step := range []struct {
		who        *Session
		text       string
		marked     bool
		bob, carol int
		label      string
	}{
		{bob, "@everyone the plan, v2", true, 0, 1, "a co-editor keeps it"},
		{bob, "the plan, v3", false, 0, 0, "a co-editor takes it out"},
		{bob, "@everyone the plan, v4", false, 0, 0, "a co-editor can't put it back"},
		{alice, "@everyone the plan, v5", true, 1, 1, "staff put it back"},
	} {
		got := edit(step.who, step.text)
		if got.Everyone != step.marked || f.messageNow(carol, c.ID, m.ID).Everyone != step.marked {
			t.Errorf("%s: marked %v", step.label, got.Everyone)
		}
		if mentions(bob) != step.bob || mentions(carol) != step.carol || mentions(alice) != 0 {
			t.Errorf("%s: bob, carol and alice have %d, %d and %d mentions", step.label, mentions(bob), mentions(carol), mentions(alice))
		}
	}
}

// TestHubPollAudiences checks the audiences of a poll's voters and of the
// rest of its channel, live and replayed, staff's alone in a staff-only
// channel.
func TestHubPollAudiences(t *testing.T) {
	h := NewHub()
	voters := map[int64]bool{1: true, 3: true}
	sub1, _, _, _ := h.Subscribe(1, false, "", 0)
	sub2, _, _, _ := h.Subscribe(2, false, "", 0)
	sub3, _, _, _ := h.Subscribe(3, true, "", 0)
	if err := h.Publish("in", nil, Voters(voters, false)); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish("out", nil, NonVoters(voters, false)); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish("staff in", nil, Voters(voters, true)); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish("staff out", nil, NonVoters(voters, true)); err != nil {
		t.Fatal(err)
	}
	got := func(sub *Sub) []string {
		var out []string
		for len(sub.Events) > 0 {
			out = append(out, (<-sub.Events).T)
		}
		return out
	}
	for _, c := range []struct {
		sub  *Sub
		want string
	}{{sub1, "[in]"}, {sub2, "[out]"}, {sub3, "[in staff in]"}} {
		if s := fmtList(got(c.sub)); s != c.want {
			t.Errorf("member %d heard %s, not %s", c.sub.member, s, c.want)
		}
	}
	// A resume replays each to those it was for.
	_, missed, resumed, _ := h.Subscribe(2, false, h.Epoch(), 0)
	var names []string
	for _, e := range missed {
		names = append(names, e.T)
	}
	if !resumed || fmtList(names) != "[out]" {
		t.Errorf("a resume replayed %v to member 2", names)
	}
}

// mentionCount is a member's unread mentions in a channel, as the den
// counts them for their read state.
func (f *fixture) mentionCount(s *Session, channel string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_mentions m WHERE m.member_id = ?1 AND m.channel_id = ?2
		AND m.message_id > coalesce((SELECT message_id FROM den_read_states WHERE member_id = ?1 AND channel_id = ?2), 0)`,
		s.MemberID, mustInt(f.t, channel)).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func fmtList(s []string) string {
	out := "["
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out + "]"
}
