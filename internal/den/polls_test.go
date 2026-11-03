package den

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// sendPoll posts a poll as s, its question the text.
func (f *fixture) sendPoll(s *Session, channel, question string, req denproto.PollRequest) denproto.Message {
	f.t.Helper()
	m, err := f.d.Send(context.Background(), s, channel, denproto.SendRequest{Nonce: denproto.Random(16), Text: question, Poll: &req})
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

// pollNow is a poll as s's history has it.
func (f *fixture) pollNow(s *Session, channel, message string) denproto.Poll {
	f.t.Helper()
	m := f.messageNow(s, channel, message)
	if err := denproto.CheckMessage(m); err != nil || m.Poll == nil {
		f.t.Fatalf("message %s has no poll fit to keep: %v", message, err)
	}
	return *m.Poll
}

// pollEvents drains the poll events a subscription holds now.
func pollEvents(t *testing.T, sub *Sub) []denproto.Event {
	t.Helper()
	var out []denproto.Event
	for {
		select {
		case e := <-sub.Events:
			if e.T == denproto.EventPollUpdated || e.T == denproto.EventPollEnded {
				out = append(out, e)
			}
		default:
			return out
		}
	}
}

func decode[T any](t *testing.T, e denproto.Event) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.D, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// tallied says what a tally shows: how many voted, and where they show,
// the totals and the voters.
func tallied(t denproto.Tally) string {
	s := fmt.Sprintf("voted %d", t.Voted)
	if t.Totals != nil {
		s += fmt.Sprintf(" totals %v", t.Totals)
	}
	if t.Voters != nil {
		s += fmt.Sprintf(" voters %v", t.Voters)
	}
	return s
}

// TestPollRules checks each pair of a poll's rules: the totals and who
// voted for what reach each member only as the rules allow, in history and
// in events, before the end and after it. Bob votes; carol, who doesn't,
// sees the totals as votes come in, and otherwise only how many voted;
// who voted for what shows with the totals where it always shows, and to
// everyone at the end where it shows then.
func TestPollRules(t *testing.T) {
	for _, voters := range []string{denproto.VotersNever, denproto.VotersAtEnd, denproto.VotersAlways} {
		for _, totals := range []string{denproto.TotalsLive, denproto.TotalsVoted} {
			t.Run(voters+"/"+totals, func(t *testing.T) {
				f, alice, bob := chatFixture(t)
				f.d.PollDelay = time.Hour
				ctx := context.Background()
				carol := f.member(alice, "carol")
				c := f.newChannel(alice, "general", denproto.ChannelRequest{})
				m := f.sendPoll(alice, c.ID, "Lunch?", denproto.PollRequest{Choices: []string{"Tacos", "Pho", "Pizza"}, Hours: 24,
					ShowVoters: voters, ShowTotals: totals})
				live, always := totals == denproto.TotalsLive, voters == denproto.VotersAlways
				b := sid(bob)

				// Nobody has voted, so everyone sees the new poll alike.
				first := "voted 0"
				if live {
					first += " totals [0 0 0]"
					if always {
						first += " voters [[] [] []]"
					}
				}
				if got := tallied(m.Poll.Tally); got != first || m.Poll.Ended || m.Poll.EndsAt != f.clock.Add(24*time.Hour).UnixMilli() {
					t.Fatalf("the new poll shows %s, ended %v, ending %d", got, m.Poll.Ended, m.Poll.EndsAt)
				}
				bobs, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
				defer f.d.Hub.Unsubscribe(bobs)
				carols, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
				defer f.d.Hub.Unsubscribe(carols)

				// Bob votes, and sees the totals, whatever the rule.
				voted := "voted 1 totals [0 1 0]"
				if always {
					voted += fmt.Sprintf(" voters [[] [%s] []]", b)
				}
				view, err := f.d.Vote(ctx, bob, m.ID, denproto.VoteRequest{Choices: []int{1}})
				if err != nil {
					t.Fatal(err)
				}
				if got := tallied(view.Tally); got != voted || !slices.Equal(view.Mine, []int{1}) {
					t.Errorf("bob sees %s, his votes %v", got, view.Mine)
				}
				// His own sessions hear it at once, the rest nothing yet.
				own := pollEvents(t, bobs)
				if len(own) != 1 {
					t.Fatalf("bob's sessions heard %d events of his vote", len(own))
				}
				u := decode[denproto.PollUpdated](t, own[0])
				if tallied(u.Tally) != voted || u.Mine == nil || !slices.Equal(*u.Mine, []int{1}) || u.MessageID != m.ID {
					t.Errorf("bob's sessions heard %+v", u)
				}
				if got := pollEvents(t, carols); len(got) != 0 {
					t.Errorf("carol heard of bob's vote before the batch: %v", got)
				}

				// Carol sees the totals as votes come in, and otherwise only
				// how many voted, in history and in the batch.
				seen := "voted 1"
				if live {
					seen = voted
				}
				if got := tallied(f.pollNow(carol, c.ID, m.ID).Tally); got != seen {
					t.Errorf("carol's history shows %s, not %s", got, seen)
				}
				if got := tallied(f.pollNow(bob, c.ID, m.ID).Tally); got != voted {
					t.Errorf("bob's history shows %s, not %s", got, voted)
				}
				f.d.flushPolls()
				heard := pollEvents(t, carols)
				if len(heard) != 1 {
					t.Fatalf("carol heard %d events in the batch", len(heard))
				}
				if u := decode[denproto.PollUpdated](t, heard[0]); tallied(u.Tally) != seen || u.Mine != nil {
					t.Errorf("carol heard %+v in the batch, not %s", u, seen)
				}
				heard = pollEvents(t, bobs)
				if len(heard) != 1 {
					t.Fatalf("bob heard %d events in the batch", len(heard))
				}
				if u := decode[denproto.PollUpdated](t, heard[0]); tallied(u.Tally) != voted || u.Mine != nil {
					t.Errorf("bob heard %+v in the batch, not %s", u, voted)
				}
				// A batch with nothing new says nothing.
				f.d.flushPolls()
				if got := len(pollEvents(t, carols)); got != 0 {
					t.Errorf("an empty batch sent %d events", got)
				}

				// At its time it ends, and everyone hears the final totals,
				// with who voted for what where it shows then.
				final := "voted 1 totals [0 1 0]"
				if voters != denproto.VotersNever {
					final += fmt.Sprintf(" voters [[] [%s] []]", b)
				}
				f.clock = f.clock.Add(24 * time.Hour)
				if err := f.d.endDue(ctx); err != nil {
					t.Fatal(err)
				}
				for who, sub := range map[string]*Sub{"bob": bobs, "carol": carols} {
					got := pollEvents(t, sub)
					if len(got) != 1 || got[0].T != denproto.EventPollEnded {
						t.Fatalf("%s heard %v as the poll ended", who, got)
					}
					if x := decode[denproto.PollEnded](t, got[0]); tallied(x.Tally) != final || x.EndsAt != f.clock.UnixMilli() {
						t.Errorf("%s heard the end as %+v, not %s", who, x, final)
					}
				}
				for who, s := range map[string]*Session{"bob": bob, "carol": carol, "alice": alice} {
					if p := f.pollNow(s, c.ID, m.ID); !p.Ended || tallied(p.Tally) != final {
						t.Errorf("after the end %s's history shows %s, ended %v", who, tallied(p.Tally), p.Ended)
					}
				}
				_, err = f.d.Vote(ctx, carol, m.ID, denproto.VoteRequest{Choices: []int{0}})
				wantCode(t, err, denproto.CodeForbidden)
				// Ending one that ended changes nothing, and tells nobody.
				if err := f.d.endDue(ctx); err != nil {
					t.Fatal(err)
				}
				if got := pollEvents(t, carols); len(got) != 0 {
					t.Errorf("a second pass told carol %v", got)
				}
			})
		}
	}
}

// TestPollVotes checks votes: one choice unless the poll takes several,
// each one of its own, once; as votes come in a member changes their vote
// and takes it back, and after voting their vote is final; the same vote
// again changes nothing and tells nobody; and each change records the
// message in the change log, its revision as it was.
func TestPollVotes(t *testing.T) {
	f, alice, bob := chatFixture(t)
	f.d.PollDelay = time.Hour
	ctx := context.Background()
	c := f.newChannel(alice, "general", denproto.ChannelRequest{})
	one := f.sendPoll(alice, c.ID, "One?", denproto.PollRequest{Choices: []string{"a", "b", "c"}, Hours: 1,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive})
	several := f.sendPoll(alice, c.ID, "Several?", denproto.PollRequest{Choices: []string{"a", "b", "c"}, Multiple: true, Hours: 1,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive})
	final := f.sendPoll(alice, c.ID, "Final?", denproto.PollRequest{Choices: []string{"a", "b"}, Hours: 1,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsVoted})
	vote := func(s *Session, m denproto.Message, choices ...int) (denproto.Poll, error) {
		return f.d.Vote(ctx, s, m.ID, denproto.VoteRequest{Choices: choices})
	}
	before := f.version()

	for _, bad := range [][]int{{0, 1}, {3}, {-1}} {
		_, err := vote(bob, one, bad...)
		wantCode(t, err, denproto.CodeInvalidField)
	}
	_, err := vote(bob, several, 1, 1)
	wantCode(t, err, denproto.CodeInvalidField)
	if p, err := vote(bob, several, 2, 0); err != nil || !slices.Equal(p.Mine, []int{0, 2}) || tallied(p.Tally) != "voted 1 totals [1 0 1]" {
		t.Fatalf("two choices: %s, %v, %v", tallied(p.Tally), p.Mine, err)
	}
	if got := f.changedSince(bob, before); got[several.ID] != 1 {
		t.Errorf("the change log since the vote is %v", got)
	}

	// As votes come in, a vote changes, and goes back.
	sub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	if _, err := vote(bob, one, 0); err != nil {
		t.Fatal(err)
	}
	if p, err := vote(bob, one, 1); err != nil || !slices.Equal(p.Mine, []int{1}) || tallied(p.Tally) != "voted 1 totals [0 1 0]" {
		t.Fatalf("a changed vote: %s, %v, %v", tallied(p.Tally), p.Mine, err)
	}
	if p, err := vote(bob, one); err != nil || p.Mine != nil || tallied(p.Tally) != "voted 0 totals [0 0 0]" {
		t.Fatalf("a vote taken back: %s, %v, %v", tallied(p.Tally), p.Mine, err)
	}
	got := pollEvents(t, sub)
	if len(got) != 3 {
		t.Fatalf("bob's sessions heard %d of his 3 votes", len(got))
	}
	if u := decode[denproto.PollUpdated](t, got[2]); u.Mine == nil || len(*u.Mine) != 0 {
		t.Errorf("a vote taken back reached his sessions as %+v", u)
	}
	// Taking back none, or voting as before, changes nothing.
	if _, err := vote(bob, one); err != nil {
		t.Fatal(err)
	}
	if _, err := vote(bob, several, 0, 2); err != nil {
		t.Fatal(err)
	}
	if got := pollEvents(t, sub); len(got) != 0 {
		t.Errorf("votes that changed nothing told bob's sessions %v", got)
	}

	// After voting, a vote is final.
	if _, err := vote(bob, final, 1); err != nil {
		t.Fatal(err)
	}
	for _, again := range [][]int{{0}, {1}, {}} {
		_, err := vote(bob, final, again...)
		wantCode(t, err, denproto.CodeForbidden)
	}
	if p := f.pollNow(bob, c.ID, final.ID); !slices.Equal(p.Mine, []int{1}) || tallied(p.Tally) != "voted 1 totals [0 1]" {
		t.Errorf("bob's final vote shows as %s, %v", tallied(p.Tally), p.Mine)
	}
	if p := f.pollNow(alice, c.ID, final.ID); p.Mine != nil || tallied(p.Tally) != "voted 1" {
		t.Errorf("alice, who didn't vote, sees %s", tallied(p.Tally))
	}

	// A message without a poll takes no vote; one the member can't see is
	// none of theirs.
	plain := f.send(alice, c.ID, "no poll here")
	_, err = vote(bob, plain, 0)
	wantCode(t, err, denproto.CodeInvalidField)
	staff := f.newChannel(alice, "staff", denproto.ChannelRequest{StaffOnly: ptr(true)})
	hidden := f.sendPoll(alice, staff.ID, "Staff?", denproto.PollRequest{Choices: []string{"a", "b"}, Hours: 1,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive})
	_, err = vote(bob, hidden, 0)
	wantCode(t, err, denproto.CodeNotFound)
}

// TestPollsEndEarly checks that a poll's author ends it before its time,
// and nobody else: everyone hears the final totals, its end moves to then,
// and no vote lands after it.
func TestPollsEndEarly(t *testing.T) {
	f, alice, bob := chatFixture(t)
	f.d.PollDelay = time.Hour
	ctx := context.Background()
	carol := f.member(alice, "carol")
	c := f.newChannel(alice, "general", denproto.ChannelRequest{})
	m := f.sendPoll(alice, c.ID, "Early?", denproto.PollRequest{Choices: []string{"yes", "no"}, Hours: 72,
		ShowVoters: denproto.VotersAtEnd, ShowTotals: denproto.TotalsVoted})
	if _, err := f.d.Vote(ctx, bob, m.ID, denproto.VoteRequest{Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	before := f.version()

	_, err := f.d.EndPoll(ctx, carol, m.ID)
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.EndPoll(ctx, bob, m.ID)
	wantCode(t, err, denproto.CodeForbidden)
	f.clock = f.clock.Add(time.Hour)
	p, err := f.d.EndPoll(ctx, alice, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("voted 1 totals [1 0] voters [[%s] []]", sid(bob))
	if !p.Ended || p.EndsAt != f.clock.UnixMilli() || tallied(p.Tally) != want {
		t.Errorf("ended early, alice sees %s, ended %v at %d", tallied(p.Tally), p.Ended, p.EndsAt)
	}
	got := pollEvents(t, sub)
	if len(got) != 1 || got[0].T != denproto.EventPollEnded {
		t.Fatalf("carol heard %v as it ended", got)
	}
	if x := decode[denproto.PollEnded](t, got[0]); tallied(x.Tally) != want || x.EndsAt != f.clock.UnixMilli() {
		t.Errorf("carol heard the end as %+v", x)
	}
	if got := f.changedSince(carol, before); got[m.ID] != 1 {
		t.Errorf("the change log since the end is %v", got)
	}
	_, err = f.d.Vote(ctx, carol, m.ID, denproto.VoteRequest{Choices: []int{1}})
	wantCode(t, err, denproto.CodeForbidden)
	// Ending it again changes nothing, and neither does its old time.
	if _, err := f.d.EndPoll(ctx, alice, m.ID); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(72 * time.Hour)
	if err := f.d.endDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := pollEvents(t, sub); len(got) != 0 {
		t.Errorf("an ended poll ended again: %v", got)
	}
}

// TestPollsEndByTimer checks the timer: started, it ends a poll whose time
// passed while the den was stopped, and tells those who see the channel.
func TestPollsEndByTimer(t *testing.T) {
	f, alice, bob := chatFixture(t)
	c := f.newChannel(alice, "general", denproto.ChannelRequest{})
	m := f.sendPoll(alice, c.ID, "Timed?", denproto.PollRequest{Choices: []string{"yes", "no"}, Hours: 1,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive})
	sub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	f.clock = f.clock.Add(2 * time.Hour)
	f.d.StartPolls()
	defer f.d.StopPolls()
	if x := nextEvent[denproto.PollEnded](t, sub, denproto.EventPollEnded); x.MessageID != m.ID || tallied(x.Tally) != "voted 0 totals [0 0]" {
		t.Errorf("the timer ended it as %+v", x)
	}
	if p := f.pollNow(bob, c.ID, m.ID); !p.Ended {
		t.Error("the timer didn't end the poll")
	}
}

// TestPollsBatch checks that a batch sends a poll's totals once however
// many votes came, and that a poll deleted meanwhile goes unsaid.
func TestPollsBatch(t *testing.T) {
	f, alice, bob := chatFixture(t)
	f.d.PollDelay = time.Hour
	ctx := context.Background()
	carol := f.member(alice, "carol")
	dave := f.member(alice, "dave")
	c := f.newChannel(alice, "general", denproto.ChannelRequest{})
	m := f.sendPoll(alice, c.ID, "Busy?", denproto.PollRequest{Choices: []string{"yes", "no"}, Hours: 1,
		ShowVoters: denproto.VotersAlways, ShowTotals: denproto.TotalsLive})
	gone := f.sendPoll(alice, c.ID, "Gone?", denproto.PollRequest{Choices: []string{"yes", "no"}, Hours: 1,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive})
	sub, _, _, _ := f.d.Hub.Subscribe(alice.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	for i, s := range []*Session{bob, carol, dave} {
		if _, err := f.d.Vote(ctx, s, m.ID, denproto.VoteRequest{Choices: []int{i % 2}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.d.Vote(ctx, bob, gone.ID, denproto.VoteRequest{Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Delete(ctx, alice, gone.ID); err != nil {
		t.Fatal(err)
	}
	f.d.flushPolls()
	got := pollEvents(t, sub)
	if len(got) != 1 {
		t.Fatalf("one batch sent alice %d events", len(got))
	}
	want := fmt.Sprintf("voted 3 totals [2 1] voters [[%s %s] [%s]]", sid(bob), sid(dave), sid(carol))
	if u := decode[denproto.PollUpdated](t, got[0]); u.MessageID != m.ID || tallied(u.Tally) != want {
		t.Errorf("the batch said %+v, not %s", u, want)
	}
	var votes int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_poll_votes WHERE message_id = ?`, mustInt(t, gone.ID)).Scan(&votes); err != nil || votes != 0 {
		t.Errorf("a deleted poll left %d votes, %v", votes, err)
	}
}

// TestPollsPosted checks what a poll is posted with: its question, 2 to 10
// choices, each cleaned as names are, emoji kept whole, and no two alike;
// how long it runs and its rules; in a channel alone, without files or
// other editors. Once posted, neither the question nor the choices change.
func TestPollsPosted(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(alice, "general", denproto.ChannelRequest{})
	good := denproto.PollRequest{Choices: []string{" Tacos  al pastor ", "👩‍💻 Code‮", "Pho"}, Hours: 1,
		ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive}
	m := f.sendPoll(bob, c.ID, "[ ] Lunch?", good)
	if want := []string{"Tacos al pastor", "👩‍💻 Code", "Pho"}; !slices.Equal(m.Poll.Choices, want) {
		t.Errorf("the choices went in as %q, not %q", m.Poll.Choices, want)
	}
	if got := f.pollNow(alice, c.ID, m.ID).Choices; !slices.Equal(got, m.Poll.Choices) {
		t.Errorf("history has the choices as %q", got)
	}
	send := func(text string, p denproto.PollRequest, mod func(*denproto.SendRequest)) error {
		req := denproto.SendRequest{Nonce: denproto.Random(16), Text: text, Poll: &p}
		if mod != nil {
			mod(&req)
		}
		_, err := f.d.Send(ctx, alice, c.ID, req)
		return err
	}
	with := func(change func(*denproto.PollRequest)) denproto.PollRequest {
		p := good
		p.Choices = slices.Clone(good.Choices)
		change(&p)
		return p
	}
	for what, p := range map[string]denproto.PollRequest{
		"one choice":        with(func(p *denproto.PollRequest) { p.Choices = p.Choices[:1] }),
		"eleven choices":    with(func(p *denproto.PollRequest) { p.Choices = slices.Repeat([]string{"x"}, 11) }),
		"two alike":         with(func(p *denproto.PollRequest) { p.Choices = []string{"Yes", "yes"} }),
		"a blank choice":    with(func(p *denproto.PollRequest) { p.Choices[1] = " ​ " }),
		"a long choice":     with(func(p *denproto.PollRequest) { p.Choices[1] = string(slices.Repeat([]rune{'é'}, 81)) }),
		"no time":           with(func(p *denproto.PollRequest) { p.Hours = 0 }),
		"over two weeks":    with(func(p *denproto.PollRequest) { p.Hours = 14*24 + 1 }),
		"an unknown rule":   with(func(p *denproto.PollRequest) { p.ShowVoters = "sometimes" }),
		"no rule of totals": with(func(p *denproto.PollRequest) { p.ShowTotals = "" }),
	} {
		if err := send("A poll", p, nil); err == nil {
			t.Errorf("a poll with %s went", what)
		} else {
			wantCode(t, err, denproto.CodeInvalidField)
		}
	}
	wantCode(t, send(" ", good, nil), denproto.CodeInvalidField)
	wantCode(t, send("With editors", good, func(r *denproto.SendRequest) { r.Editors = []string{sid(bob)} }), denproto.CodeInvalidField)
	upload := f.mustUpload(alice, "a.txt", []byte("some text"))
	wantCode(t, send("With a file", good, func(r *denproto.SendRequest) { r.Attachments = []string{upload.ID} }), denproto.CodeInvalidField)
	dm, err := f.d.OpenDM(ctx, alice, denproto.DMRequest{MemberID: sid(bob)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.d.Send(ctx, alice, dm.ID, f.sealedSend(alice, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "In a DM", Poll: &good}))
	wantCode(t, err, denproto.CodeInvalidField)

	// Its author can't edit it, nor tick its question's task.
	_, err = f.d.Edit(ctx, bob, m.ID, denproto.EditRequest{Revision: 1, Text: "[ ] Dinner?"})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.SetTask(ctx, bob, m.ID, 0, denproto.TaskRequest{Checked: true, Text: "Lunch?"})
	wantCode(t, err, denproto.CodeForbidden)
	if got := f.messageNow(alice, c.ID, m.ID); got.Text != "[ ] Lunch?" || got.Revision != 1 {
		t.Errorf("the poll's message changed: %+v", got)
	}
}
