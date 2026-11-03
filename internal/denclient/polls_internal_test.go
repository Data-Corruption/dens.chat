package denclient

import (
	"fmt"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func pollSaid(m denproto.Message) string {
	p := m.Poll
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
		s += fmt.Sprintf(" ended at %d", p.EndsAt)
	}
	return s
}

// A poll's events apply to its message: an update's totals replace those
// held, with its voters, and one without totals leaves them, since a
// member never stops seeing them; the member's own votes change only by
// the update to their sessions; an end brings the final totals, after
// which no update changes it; and one that doesn't fit the poll is left
// for the den's next word.
func TestApplyingPolls(t *testing.T) {
	m := denproto.Message{ID: "5", ChannelID: "2", AuthorID: "1", Revision: 1, Text: "Which?",
		Poll: &denproto.Poll{Choices: []string{"a", "b"}, EndsAt: 100, ShowVoters: denproto.VotersAlways, ShowTotals: denproto.TotalsVoted}}
	held := m.Poll
	one, none := []int{1}, []int{}
	steps := []struct {
		typ     string
		d       any
		changed bool
		want    string
	}{
		{denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "5", ChannelID: "2", Tally: denproto.Tally{Voted: 2}}, true, "voted 2"},
		{denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "6", ChannelID: "2", Tally: denproto.Tally{Voted: 3}}, false, "voted 2"},
		{denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "5", ChannelID: "2", Tally: denproto.Tally{Voted: 3,
			Totals: []int{2, 1}, Voters: [][]string{{"7", "8"}, {"9"}}}, Mine: &one}, true, "voted 3 totals [2 1] voters [[7 8] [9]] mine [1]"},
		{denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "5", ChannelID: "2", Tally: denproto.Tally{Voted: 4}}, true,
			"voted 4 totals [2 1] voters [[7 8] [9]] mine [1]"},
		{denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "5", ChannelID: "2", Tally: denproto.Tally{Voted: 4,
			Totals: []int{1, 2, 0}}}, false, "voted 4 totals [2 1] voters [[7 8] [9]] mine [1]"},
		{denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "5", ChannelID: "2", Tally: denproto.Tally{Voted: 3,
			Totals: []int{2, 0}, Voters: [][]string{{"7", "8"}, {}}}, Mine: &none}, true, "voted 3 totals [2 0] voters [[7 8] []]"},
		{denproto.EventPollEnded, denproto.PollEnded{MessageID: "5", ChannelID: "2", EndsAt: 90, Tally: denproto.Tally{Voted: 3,
			Totals: []int{3, 0}, Voters: [][]string{{"7", "8", "9"}, {}}}}, true, "voted 3 totals [3 0] voters [[7 8 9] []] ended at 90"},
		{denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "5", ChannelID: "2", Tally: denproto.Tally{Voted: 9}}, false,
			"voted 3 totals [3 0] voters [[7 8 9] []] ended at 90"},
	}
	for i, s := range steps {
		if changed := applyTally(&m, reactionEvent(t, s.typ, s.d)); changed != s.changed || pollSaid(m) != s.want {
			t.Fatalf("step %d: changed %v, %q; want %v, %q", i, changed, pollSaid(m), s.changed, s.want)
		}
	}
	if held.Voted != 0 || held.Ended {
		t.Errorf("applying changed the poll the message held before: %+v", held)
	}
	plain := denproto.Message{ID: "5", ChannelID: "2", AuthorID: "1", Revision: 1, Text: "no poll"}
	if applyTally(&plain, reactionEvent(t, steps[0].typ, steps[0].d)) || plain.Poll != nil {
		t.Error("a poll's event changed a message without one")
	}
}

// What the den sends of a poll after a request that was out while its
// events came takes them again, in order, the newest last.
func TestReplayingPolls(t *testing.T) {
	c := &conn{}
	mark := c.tallies.mark()
	for _, voted := range []int{1, 2} {
		c.tallies.add(reactionEvent(t, denproto.EventPollUpdated, denproto.PollUpdated{MessageID: "5", ChannelID: "2",
			Tally: denproto.Tally{Voted: voted, Totals: []int{voted, 0}}}))
	}
	ms := []denproto.Message{{ID: "5", ChannelID: "2", AuthorID: "1", Revision: 1, Text: "Which?",
		Poll: &denproto.Poll{Choices: []string{"a", "b"}, EndsAt: 100, ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive,
			Tally: denproto.Tally{Voted: 1, Totals: []int{1, 0}}}}}
	c.replayTallies(mark, ms)
	if got := pollSaid(ms[0]); got != "voted 2 totals [2 0]" {
		t.Errorf("replayed, the page holds %q", got)
	}
}
