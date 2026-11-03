package denproto

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestCleanChoice checks that a poll's choice is cleaned as a name is, its
// hidden characters out and its spaces folded, keeping each of Unicode's
// emoji whole, joiners and tags and all, where a name loses them.
func TestCleanChoice(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"  Tacos   al pastor ", "Tacos al pastor"},
		{"👩‍💻 coding", "👩‍💻 coding"},
		{"🏳️‍🌈🏴󠁧󠁢󠁳󠁣󠁴󠁿", "🏳️‍🌈🏴󠁧󠁢󠁳󠁣󠁴󠁿"},
		{"👨🏽‍❤️‍👨🏻 x", "👨🏽‍❤️‍👨🏻 x"},
		{"a‍b", "ab"},
		{"‮evil", "evil"},
		{"👩‍", "👩"},
		{"1️⃣ first", "1️⃣ first"},
		{"Ünïcødé ✓", "Ünïcødé ✓"},
	} {
		got, err := CleanChoice(c.in)
		if err != nil || got != c.want {
			t.Errorf("CleanChoice(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if got, _ := CleanName("👩‍💻 coding", MaxNameRunes); got != "👩💻 coding" {
		t.Errorf("a name keeps its joiners: %q", got)
	}
	for _, bad := range []string{"", "   ", "​‍", "ㅤ", strings.Repeat("x", MaxChoiceRunes+1), "\xff", strings.Repeat(" ", 2000) + "x"} {
		if got, err := CleanChoice(bad); err == nil {
			t.Errorf("CleanChoice(%q) = %q, want an error", bad, got)
		}
	}
	if _, err := CleanChoice(strings.Repeat("é", MaxChoiceRunes)); err != nil {
		t.Errorf("a choice of %d characters: %v", MaxChoiceRunes, err)
	}
}

// TestPollRequestClean checks a poll as its author posts it.
func TestPollRequestClean(t *testing.T) {
	good := PollRequest{Choices: []string{" A ", "B"}, Hours: 24, ShowVoters: VotersNever, ShowTotals: TotalsLive}
	p, err := good.Clean()
	if err != nil || !slices.Equal(p.Choices, []string{"A", "B"}) || !slices.Equal(good.Choices, []string{" A ", "B"}) {
		t.Fatalf("Clean = %+v, %v, from %+v", p, err, good)
	}
	for what, change := range map[string]func(*PollRequest){
		"one choice":       func(p *PollRequest) { p.Choices = p.Choices[:1] },
		"eleven":           func(p *PollRequest) { p.Choices = strings.Split("abcdefghijk", "") },
		"two alike":        func(p *PollRequest) { p.Choices = []string{"Oui", "OUI"} },
		"alike once clean": func(p *PollRequest) { p.Choices = []string{"a b", " a​  b"} },
		"no hours":         func(p *PollRequest) { p.Hours = 0 },
		"past two weeks":   func(p *PollRequest) { p.Hours = MaxPollHours + 1 },
		"no voters' rule":  func(p *PollRequest) { p.ShowVoters = "" },
		"no totals' rule":  func(p *PollRequest) { p.ShowTotals = "after" },
	} {
		p := good
		p.Choices = slices.Clone(good.Choices)
		change(&p)
		if _, err := p.Clean(); err == nil {
			t.Errorf("a poll with %s was clean", what)
		}
	}
	p = good
	p.Choices, p.Hours = strings.Split("abcdefghij", ""), MaxPollHours
	if _, err := p.Clean(); err != nil {
		t.Errorf("ten choices for two weeks: %v", err)
	}
}

// TestTallyFor checks what each pair of rules shows a member who voted
// and one who didn't, before the end and after it.
func TestTallyFor(t *testing.T) {
	votes := [][]string{{"7"}, {"8", "9"}}
	show := func(tl Tally) string {
		s := fmt.Sprint(tl.Voted)
		if tl.Totals != nil {
			s += fmt.Sprint(" ", tl.Totals)
		}
		if tl.Voters != nil {
			s += fmt.Sprint(" ", tl.Voters)
		}
		return s
	}
	for _, c := range []struct {
		voters, totals string
		ended, voted   bool
		want           string
	}{
		{VotersNever, TotalsLive, false, false, "3 [1 2]"},
		{VotersNever, TotalsLive, true, true, "3 [1 2]"},
		{VotersNever, TotalsVoted, false, false, "3"},
		{VotersNever, TotalsVoted, false, true, "3 [1 2]"},
		{VotersNever, TotalsVoted, true, false, "3 [1 2]"},
		{VotersAtEnd, TotalsLive, false, true, "3 [1 2]"},
		{VotersAtEnd, TotalsLive, true, false, "3 [1 2] [[7] [8 9]]"},
		{VotersAtEnd, TotalsVoted, false, false, "3"},
		{VotersAtEnd, TotalsVoted, true, false, "3 [1 2] [[7] [8 9]]"},
		{VotersAlways, TotalsLive, false, false, "3 [1 2] [[7] [8 9]]"},
		{VotersAlways, TotalsVoted, false, false, "3"},
		{VotersAlways, TotalsVoted, false, true, "3 [1 2] [[7] [8 9]]"},
	} {
		p := Poll{Choices: []string{"a", "b"}, ShowVoters: c.voters, ShowTotals: c.totals, Ended: c.ended, Tally: Tally{Voted: 3}}
		if got := show(p.TallyFor(votes, c.voted)); got != c.want {
			t.Errorf("%s/%s, ended %v, voted %v: %s, want %s", c.voters, c.totals, c.ended, c.voted, got, c.want)
		}
	}
	p := Poll{Choices: []string{"a", "b"}, ShowVoters: VotersAlways, ShowTotals: TotalsVoted, Tally: Tally{Voted: 3}}
	if v := p.ViewFor(votes, []int{1}); !slices.Equal(v.Mine, []int{1}) || show(v.Tally) != "3 [1 2] [[7] [8 9]]" {
		t.Errorf("a voter's view: %+v", v)
	}
}

// TestPollChecks checks what a client takes of a den's polls: a message's,
// an update and an end, each in range and fitting the poll's choices; and
// that a DM's message holds no poll and no @everyone.
func TestPollChecks(t *testing.T) {
	poll := Poll{Choices: []string{"a", "b"}, EndsAt: 1, ShowVoters: VotersAlways, ShowTotals: TotalsLive,
		Tally: Tally{Voted: 2, Totals: []int{1, 2}, Voters: [][]string{{"5"}, {"5", "6"}}}, Mine: []int{0, 1}, Multiple: true}
	m := Message{ID: "1", ChannelID: "2", AuthorID: "3", Revision: 1, Text: "Which?", Poll: &poll}
	if err := CheckMessage(m); err != nil {
		t.Fatalf("a fine poll: %v", err)
	}
	for what, change := range map[string]func(*Poll){
		"one choice":           func(p *Poll) { p.Choices = p.Choices[:1] },
		"a hidden character":   func(p *Poll) { p.Choices[1] = "b‮" },
		"two alike":            func(p *Poll) { p.Choices[1] = "a" },
		"a total past voters":  func(p *Poll) { p.Totals = []int{1, 3} },
		"too few totals":       func(p *Poll) { p.Totals = []int{1} },
		"voters without total": func(p *Poll) { p.Totals = nil },
		"voters off the total": func(p *Poll) { p.Voters = [][]string{{"5"}, {"5"}} },
		"a voter twice":        func(p *Poll) { p.Voters[1] = []string{"6", "6"} },
		"a bad voter":          func(p *Poll) { p.Voters[0] = []string{"x"} },
		"two votes for one":    func(p *Poll) { p.Multiple = false },
		"a vote for none":      func(p *Poll) { p.Mine = []int{2} },
		"a rule unknown":       func(p *Poll) { p.ShowTotals = "never" },
		"no end":               func(p *Poll) { p.EndsAt = 0 },
	} {
		p := poll
		p.Choices, p.Totals, p.Mine = slices.Clone(poll.Choices), slices.Clone(poll.Totals), slices.Clone(poll.Mine)
		p.Voters = [][]string{slices.Clone(poll.Voters[0]), slices.Clone(poll.Voters[1])}
		change(&p)
		bad := m
		bad.Poll = &p
		if CheckMessage(bad) == nil {
			t.Errorf("a poll with %s passed", what)
		}
	}
	dm := Message{ID: "1", ChannelID: "2", AuthorID: "3", Revision: 1, Sealed: Bytes("x"), KeyID: "4", Nonce: Random(NonceBytes)}
	if err := CheckMessage(dm); err != nil {
		t.Fatalf("a DM's message: %v", err)
	}
	for what, change := range map[string]func(*Message){
		"a poll":    func(m *Message) { m.Poll = &poll },
		"@everyone": func(m *Message) { m.Everyone = true },
	} {
		bad := dm
		change(&bad)
		if CheckMessage(bad) == nil {
			t.Errorf("a DM's message with %s passed", what)
		}
	}

	mine := []int{3}
	for _, u := range []PollUpdated{
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: 4}},
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: 4, Totals: []int{1, 4}}, Mine: &mine},
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: 1, Totals: []int{1, 0}, Voters: [][]string{{"5"}, {}}}},
	} {
		if err := u.Check(); err != nil {
			t.Errorf("%+v: %v", u, err)
		}
	}
	far := []int{10}
	for _, u := range []PollUpdated{
		{MessageID: "x", ChannelID: "2"},
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: -1}},
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: 1, Totals: []int{1}}},
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: 1, Totals: slices.Repeat([]int{0}, 11)}},
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: 1}, Mine: &far},
	} {
		if u.Check() == nil {
			t.Errorf("%+v passed", u)
		}
	}
	if err := (PollEnded{MessageID: "1", ChannelID: "2", EndsAt: 5, Tally: Tally{Voted: 1, Totals: []int{0, 1}}}).Check(); err != nil {
		t.Errorf("an end: %v", err)
	}
	for _, x := range []PollEnded{
		{MessageID: "1", ChannelID: "2", Tally: Tally{Voted: 1, Totals: []int{0, 1}}},
		{MessageID: "1", ChannelID: "2", EndsAt: 5, Tally: Tally{Voted: 1}},
	} {
		if x.Check() == nil {
			t.Errorf("%+v passed", x)
		}
	}
}

// TestMentionsEveryone checks @everyone by the rule that finds mentions.
func TestMentionsEveryone(t *testing.T) {
	for text, want := range map[string]bool{
		"@everyone":             true,
		"hey **@Everyone**!":    true,
		"`@everyone`":           false,
		"me@everyone":           false,
		"@everyones":            false,
		"https://x/@everyone":   false,
		"```\n@everyone\n```":   false,
		"@@everyone":            false,
		"line\n@EVERYONE, look": true,
	} {
		if got := MentionsEveryone(text); got != want {
			t.Errorf("MentionsEveryone(%q) = %v", text, got)
		}
	}
}
