package denproto

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// Polls (M15.4): a channel's message whose members vote on its choices.
// The message's text is the question, so notifications, reply quotes and
// older pages read it. As its author posts it they pick how long it runs,
// whether a member may pick more than one choice, who voted for what shows
// (never, once it ends, or always), and when the totals show: as votes
// come in, or to each member once they've voted, which makes their vote
// final. The den holds to both, sending totals and voters to nobody sooner
// than the poll allows, so a poll as a member sees it depends on who they
// are; and like a reaction, a vote changes the message without changing
// its revision.

// Events for polls: the totals as votes change, in batches, to those who
// may see them, and to a voter's own sessions their votes; and the final
// totals, to everyone, as a poll ends.
const (
	EventPollUpdated = "poll.updated"
	EventPollEnded   = "poll.ended"
)

const (
	// A poll has MinPollChoices to MaxPollChoices choices, each at most
	// MaxChoiceRunes characters of plain text.
	MinPollChoices = 2
	MaxPollChoices = 10
	MaxChoiceRunes = 80
	// A poll runs MinPollHours to MaxPollHours, two weeks.
	MinPollHours = 1
	MaxPollHours = 14 * 24
	// maxChoiceBytes bounds a choice before it's cleaned, so cleaning a
	// hostile one costs little.
	maxChoiceBytes = 1 << 10
)

// Who voted for what: never shown, the default; shown to everyone once
// the poll ends; or shown with the totals.
const (
	VotersNever  = "never"
	VotersAtEnd  = "end"
	VotersAlways = "always"
)

// When the totals show: to everyone as votes come in, the default; or to
// each member once they've voted, which makes their vote final.
const (
	TotalsLive  = "live"
	TotalsVoted = "voted"
)

// PollRequest is the poll a message is posted with: its choices, whether
// a member may pick more than one, how long it runs, in hours, and its two
// rules.
type PollRequest struct {
	Choices    []string `json:"choices"`
	Multiple   bool     `json:"multiple,omitempty"`
	Hours      int      `json:"hours"`
	ShowVoters string   `json:"show_voters"`
	ShowTotals string   `json:"show_totals"`
}

// Clean checks a poll as its author posts it, and returns it with its
// choices cleaned, as names are, keeping emoji whole: 2 to 10, each 1 to
// 80 characters with something visible, and no two the same, in any case.
func (r PollRequest) Clean() (PollRequest, error) {
	if len(r.Choices) < MinPollChoices || len(r.Choices) > MaxPollChoices {
		return r, fmt.Errorf("a poll has %d to %d choices", MinPollChoices, MaxPollChoices)
	}
	out := r
	out.Choices = make([]string, len(r.Choices))
	for i, c := range r.Choices {
		clean, err := CleanChoice(c)
		if err != nil {
			return r, fmt.Errorf("choice %d: %w", i+1, err)
		}
		if slices.ContainsFunc(out.Choices[:i], func(o string) bool { return strings.EqualFold(o, clean) }) {
			return r, errors.New("a poll's choices differ from each other")
		}
		out.Choices[i] = clean
	}
	if r.Hours < MinPollHours || r.Hours > MaxPollHours {
		return r, fmt.Errorf("a poll runs %d to %d hours", MinPollHours, MaxPollHours)
	}
	if err := checkRules(r.ShowVoters, r.ShowTotals); err != nil {
		return r, err
	}
	return out, nil
}

func checkRules(voters, totals string) error {
	if voters != VotersNever && voters != VotersAtEnd && voters != VotersAlways {
		return errors.New("who voted for what shows never, at the end or always")
	}
	if totals != TotalsLive && totals != TotalsVoted {
		return errors.New("a poll's totals show as votes come in or after voting")
	}
	return nil
}

// Poll is a message's poll as a member sees it: its choices, whether
// several may be picked, its end, its rules and whether it ended; how it
// stands, as far as the member may see; and their own votes, by choice,
// which they always see.
type Poll struct {
	Choices    []string `json:"choices"`
	Multiple   bool     `json:"multiple,omitempty"`
	EndsAt     int64    `json:"ends_at"`
	ShowVoters string   `json:"show_voters"`
	ShowTotals string   `json:"show_totals"`
	Ended      bool     `json:"ended,omitempty"`
	Tally
	Mine []int `json:"mine,omitempty"`
}

// Tally is how a poll stands, as a member may see it: how many members
// voted, which everyone sees; the totals, a count for each choice, once
// the poll lets them see them; and where it shows who voted for what, the
// members who picked each choice, in the order they voted.
type Tally struct {
	Voted  int        `json:"voted"`
	Totals []int      `json:"totals,omitempty"`
	Voters [][]string `json:"voters,omitempty"`
}

// check checks a tally as a den sends it, for a poll of n choices.
func (t Tally) check(n int) error {
	if t.Voted < 0 || t.Totals == nil && t.Voters != nil {
		return errors.New("a poll's tally is out of range")
	}
	if t.Totals != nil && len(t.Totals) != n || t.Voters != nil && len(t.Voters) != n {
		return errors.New("a poll's tally doesn't match its choices")
	}
	for i, total := range t.Totals {
		if total < 0 || total > t.Voted || t.Voters != nil && len(t.Voters[i]) != total {
			return errors.New("a poll's tally is out of range")
		}
	}
	for _, members := range t.Voters {
		for j, id := range members {
			if _, err := ParseID(id); err != nil || slices.Contains(members[:j], id) {
				return errors.New("a poll's voters name an invalid member")
			}
		}
	}
	return nil
}

// checkVotes checks a member's votes on a poll of n choices.
func checkVotes(votes []int, n int, multiple bool) error {
	if len(votes) > 1 && !multiple || len(votes) > n {
		return errors.New("too many votes for the poll")
	}
	for i, v := range votes {
		if v < 0 || v >= n || slices.Contains(votes[:i], v) {
			return errors.New("a vote names no choice of the poll")
		}
	}
	return nil
}

// Check checks a poll as a den sends it.
func (p Poll) Check() error {
	if len(p.Choices) < MinPollChoices || len(p.Choices) > MaxPollChoices || p.EndsAt <= 0 {
		return errors.New("a poll has an invalid shape")
	}
	for i, c := range p.Choices {
		if clean, err := CleanChoice(c); err != nil || clean != c || slices.Contains(p.Choices[:i], c) {
			return errors.New("a poll has an invalid choice")
		}
	}
	if err := checkRules(p.ShowVoters, p.ShowTotals); err != nil {
		return err
	}
	if err := p.Tally.check(len(p.Choices)); err != nil {
		return err
	}
	return checkVotes(p.Mine, len(p.Choices), p.Multiple)
}

// PollUpdated is how a poll stands as its votes change: the den sends the
// totals in batches, each to those who may see them; and to a member's own
// sessions, as their vote changes, the poll as they now see it with their
// votes in Mine, which the batches leave out.
type PollUpdated struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	Tally
	Mine *[]int `json:"mine,omitempty"`
}

// Check checks an update as a den sends it.
func (u PollUpdated) Check() error {
	if err := checkIDs(u.MessageID, u.ChannelID); err != nil {
		return err
	}
	n := max(len(u.Totals), len(u.Voters))
	if n > MaxPollChoices || u.Totals != nil && n < MinPollChoices {
		return errors.New("a poll's tally doesn't match its choices")
	}
	if err := u.Tally.check(n); err != nil {
		return err
	}
	if u.Mine != nil {
		return checkVotes(*u.Mine, MaxPollChoices, true)
	}
	return nil
}

// PollEnded is a poll's final tally, which everyone who sees its channel
// gets as it ends, at its time or early by its author: the totals, and who
// voted for what where the poll shows it then.
type PollEnded struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	EndsAt    int64  `json:"ends_at"`
	Tally
}

// Check checks an end as a den sends it.
func (e PollEnded) Check() error {
	if err := checkIDs(e.MessageID, e.ChannelID); err != nil {
		return err
	}
	if e.EndsAt <= 0 || len(e.Totals) < MinPollChoices || len(e.Totals) > MaxPollChoices {
		return errors.New("a poll's end has an invalid shape")
	}
	return e.Tally.check(len(e.Totals))
}

func checkIDs(ids ...string) error {
	for _, id := range ids {
		if _, err := ParseID(id); err != nil {
			return errors.New("an event names an invalid ID")
		}
	}
	return nil
}

// VoteRequest is a member's vote on a poll: the choices they pick, by
// their place in it, one unless the poll takes several; none takes their
// vote back, where the poll lets them.
type VoteRequest struct {
	Choices []int `json:"choices"`
}

// TallyFor is how a poll stands as a member may see it, by its rules, from
// each choice's voters in the order they voted; voted says whether the
// member voted. Everyone sees how many voted. The totals show as votes
// come in, or to a member once they voted, and to everyone once the poll
// ended; who voted for what shows with the totals where the poll always
// shows it, and to everyone once it ended where it shows it then.
func (p Poll) TallyFor(votes [][]string, voted bool) Tally {
	t := Tally{Voted: p.Voted}
	if p.ShowTotals != TotalsLive && !p.Ended && !voted {
		return t
	}
	t.Totals = make([]int, len(p.Choices))
	for i := range t.Totals {
		if i < len(votes) {
			t.Totals[i] = len(votes[i])
		}
	}
	if p.ShowVoters == VotersAlways || p.ShowVoters == VotersAtEnd && p.Ended {
		t.Voters = make([][]string, len(p.Choices))
		for i := range t.Voters {
			t.Voters[i] = []string{}
			if i < len(votes) {
				t.Voters[i] = slices.Clone(votes[i])
			}
		}
	}
	return t
}

// ViewFor is a poll as a member sees it, who voted for mine, by TallyFor.
func (p Poll) ViewFor(votes [][]string, mine []int) Poll {
	p.Tally, p.Mine = p.TallyFor(votes, len(mine) > 0), mine
	return p
}

// Everyone is the name staff's @everyone mentions every member by who can
// see the channel (M15.4). A den may have a member of that name from
// before, whom anyone else's @everyone mentions, and new members can't
// take it.
const Everyone = "everyone"

// MentionsEveryone reports whether a text mentions everyone, by the rule
// that finds mentions.
func MentionsEveryone(text string) bool { return slices.Contains(Mentions(text), Everyone) }

// CleanChoice cleans a poll's choice as names are cleaned, keeping each of
// Unicode's emoji whole: one the list holds keeps the joiners and tags
// cleaning takes out of the rest, so 👩‍💻 stays one emoji. It's 1 to
// MaxChoiceRunes characters, with something visible among them.
func CleanChoice(s string) (string, error) {
	if len(s) > maxChoiceBytes {
		return "", fmt.Errorf("a choice is at most %d characters", MaxChoiceRunes)
	}
	return cleanText(s, MaxChoiceRunes, true, "choice")
}

// joinedEmoji are Unicode's emoji, as the list holds them, that hold a
// format character, a joiner or a tag, which cleaning takes out of the
// rest of a text; and the longest of them, in bytes.
var joinedEmoji = sync.OnceValues(func() (map[string]bool, int) {
	set, longest := map[string]bool{}, 0
	for e := range unicodeEmoji() {
		if strings.IndexFunc(e, hiddenRune) >= 0 {
			set[e] = true
			longest = max(longest, len(e))
		}
	}
	return set, longest
})

// joinedEmojiAt returns the length of the longest of joinedEmoji that s
// starts with, or 0.
func joinedEmojiAt(s string) int {
	if s == "" || s[0] < utf8.RuneSelf {
		return 0
	}
	set, longest := joinedEmoji()
	for n := min(len(s), longest); n > 0; n-- {
		if (n == len(s) || utf8.RuneStart(s[n])) && set[s[:n]] {
			return n
		}
	}
	return 0
}
