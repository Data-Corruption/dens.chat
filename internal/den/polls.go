package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Polls (M15.4): a channel's message whose members vote on its choices,
// the message's text being the question. The den holds to the poll's two
// rules, its author's picks as they posted it: who voted for what shows
// never, once it ends, or with the totals; and the totals show as votes
// come in, or to each member once they've voted, whose vote is then final.
// So the den sends each member a poll as they may see it, in history and
// in what changed while away, and sends the totals as votes change in
// batches, each to those who may see them, a busy poll costing an event a
// batch rather than one a vote; under After voting, the rest hear only how
// many voted. Never keeps who voted for what from the members, not from
// the den, which keeps each vote to count each member once and let them
// change it.
//
// A vote changes its message without changing its revision, as a reaction
// does: it records the message in the change log, so an install that was
// away fetches it again as it now stands, and waits its turn with edits
// and reactions on d.changes, so its events go out in order with the
// batches and the end. The den ends a poll at its time, by a timer set for
// the next to end, or early at its author's word, and sends everyone the
// final totals, with who voted for what where the poll shows it then. A
// poll can't be edited, neither its question nor its choices, so no vote
// stands under a question it wasn't cast for; it goes with its message.

// defaultPollDelay is how long a poll's changes gather before its totals
// go out, as presence's do.
const defaultPollDelay = 1500 * time.Millisecond

func pollAD(id int64) []byte { return []byte("den_polls.choices:" + strconv.FormatInt(id, 10)) }

// pollRow is a poll as the den keeps it: its message's, with its channel
// and author, and its votes, each choice's voters in the order they voted
// and each member's choices in order. Its Ended counts a poll past its end
// that the timer hasn't ended yet.
type pollRow struct {
	message, channel, author int64
	poll                     denproto.Poll
	votes                    [][]int64
	mine                     map[int64][]int
}

// voterIDs are each choice's voters, as the protocol names them.
func (p *pollRow) voterIDs() [][]string {
	out := make([][]string, len(p.votes))
	for i, members := range p.votes {
		out[i] = make([]string, len(members))
		for j, id := range members {
			out[i][j] = denproto.FormatID(id)
		}
	}
	return out
}

// view is the poll as a member sees it.
func (p *pollRow) view(member int64) *denproto.Poll {
	v := p.poll.ViewFor(p.voterIDs(), p.mine[member])
	return &v
}

// voters are the members who voted.
func (p *pollRow) voters() map[int64]bool {
	out := make(map[int64]bool, len(p.mine))
	for id := range p.mine {
		out[id] = true
	}
	return out
}

// polls loads the polls of the messages with the given IDs, with their
// votes, as of now.
func (d *Den) polls(ctx context.Context, q querier, ids []int64, now time.Time) (map[int64]*pollRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	in := `(?` + strings.Repeat(", ?", len(ids)-1) + `)`
	rows, err := q.QueryContext(ctx, `SELECT p.message_id, m.channel_id, m.author_id, p.choices, p.multiple, p.show_voters, p.show_totals,
		p.ends_at, p.ended FROM den_polls p JOIN den_messages m ON m.id = p.message_id WHERE p.message_id IN `+in, args...)
	if err != nil {
		return nil, err
	}
	out := map[int64]*pollRow{}
	for rows.Next() {
		p := &pollRow{mine: map[int64][]int{}}
		var sealed []byte
		var ended bool
		if err := rows.Scan(&p.message, &p.channel, &p.author, &sealed, &p.poll.Multiple, &p.poll.ShowVoters, &p.poll.ShowTotals,
			&p.poll.EndsAt, &ended); err != nil {
			rows.Close()
			return nil, err
		}
		plain, err := d.v.Open(sealed, pollAD(p.message))
		if err == nil {
			err = json.Unmarshal(plain, &p.poll.Choices)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		p.poll.Ended = ended || now.UnixMilli() >= p.poll.EndsAt
		p.votes = make([][]int64, len(p.poll.Choices))
		out[p.message] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	rows, err = q.QueryContext(ctx, `SELECT message_id, member_id, choice FROM den_poll_votes WHERE message_id IN `+in+`
		ORDER BY message_id, voted_at, rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var message, member int64
		var choice int
		if err := rows.Scan(&message, &member, &choice); err != nil {
			return nil, err
		}
		p := out[message]
		if p == nil || choice < 0 || choice >= len(p.votes) {
			continue
		}
		p.votes[choice] = append(p.votes[choice], member)
		p.mine[member] = append(p.mine[member], choice)
	}
	for _, p := range out {
		for _, mine := range p.mine {
			slices.Sort(mine)
		}
		p.poll.Voted = len(p.mine)
	}
	return out, rows.Err()
}

// cleanPoll checks a poll a message is posted with: in a channel, with a
// question, which the text's own check asks for, and without files or
// editors, since neither the message nor its question changes once it's
// posted.
func cleanPoll(req denproto.SendRequest, dm bool) (*denproto.PollRequest, error) {
	if req.Poll == nil {
		return nil, nil
	}
	if dm {
		return nil, invalid("poll: polls go in channels; a DM's votes would be sealed from the den that counts them")
	}
	if len(req.Attachments) > 0 || len(req.Editors) > 0 {
		return nil, invalid("poll: a poll goes without files or other editors")
	}
	poll, err := req.Poll.Clean()
	if err != nil {
		return nil, invalid("poll: %v", err)
	}
	return &poll, nil
}

// insertPoll keeps a message's poll within tx, its choices sealed with the
// data key as its text is, and returns it as everyone first sees it.
func (d *Den) insertPoll(ctx context.Context, tx *sql.Tx, message int64, req *denproto.PollRequest, now time.Time) (*denproto.Poll, error) {
	plain, err := json.Marshal(req.Choices)
	if err != nil {
		return nil, err
	}
	sealed, err := d.v.Seal(plain, pollAD(message))
	if err != nil {
		return nil, err
	}
	ends := now.Add(time.Duration(req.Hours) * time.Hour).UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO den_polls (message_id, choices, multiple, show_voters, show_totals, ends_at)
		VALUES (?, ?, ?, ?, ?, ?)`, message, sealed, req.Multiple, req.ShowVoters, req.ShowTotals, ends); err != nil {
		return nil, err
	}
	p := denproto.Poll{Choices: req.Choices, Multiple: req.Multiple, EndsAt: ends, ShowVoters: req.ShowVoters, ShowTotals: req.ShowTotals}
	p.Tally = p.TallyFor(nil, false)
	return &p, nil
}

// pollOf finds the poll on a channel's message the member can see, within
// tx, as of now.
func (d *Den) pollOf(ctx context.Context, tx *sql.Tx, mid int64, now time.Time) (*pollRow, error) {
	if err := messageThere(ctx, tx, mid); err != nil {
		return nil, err
	}
	polls, err := d.polls(ctx, tx, []int64{mid}, now)
	if err != nil {
		return nil, err
	}
	p := polls[mid]
	if p == nil {
		return nil, invalid("that message has no poll")
	}
	return p, nil
}

// Vote sets the member's vote on a poll: the choices they pick, one unless
// it takes several, or none to take their vote back. The den checks that
// the poll is open, and under After voting, that they haven't voted, since
// their vote is final once they've seen the totals. It answers with the
// poll as they now see it, which their own sessions hear too; the totals
// go to the rest in the next batch.
func (d *Den) Vote(ctx context.Context, s *Session, messageID string, req denproto.VoteRequest) (denproto.Poll, error) {
	d.changes.Lock()
	defer d.changes.Unlock()
	c, cid, mid, err := d.reactable(ctx, s, messageID)
	if err != nil {
		return denproto.Poll{}, err
	}
	now := d.now()
	var p *pollRow
	changed := false
	err = d.tx(ctx, func(tx *sql.Tx) error {
		found, err := d.pollOf(ctx, tx, mid, now)
		if err != nil {
			return err
		}
		p = found
		choices := slices.Clone(req.Choices)
		slices.Sort(choices)
		mine := p.mine[s.MemberID]
		switch {
		case p.poll.Ended:
			return forbidden("the poll has ended")
		case len(choices) > 1 && !p.poll.Multiple:
			return invalid("choices: this poll takes one choice")
		case slices.ContainsFunc(choices, func(i int) bool { return i < 0 || i >= len(p.poll.Choices) }) || len(slices.Compact(slices.Clone(choices))) != len(choices):
			return invalid("choices: each one of the poll's, once")
		case p.poll.ShowTotals == denproto.TotalsVoted && len(mine) > 0:
			return forbidden("your vote is final: this poll's results showed once you voted")
		case slices.Equal(choices, mine):
			return nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_poll_votes WHERE message_id = ? AND member_id = ?`, mid, s.MemberID); err != nil {
			return err
		}
		for _, choice := range choices {
			if _, err := tx.ExecContext(ctx, `INSERT INTO den_poll_votes (message_id, member_id, choice, voted_at) VALUES (?, ?, ?, ?)`,
				mid, s.MemberID, choice, now.UnixMilli()); err != nil {
				return err
			}
		}
		changed = true
		if p, err = d.pollOf(ctx, tx, mid, now); err != nil {
			return err
		}
		return logMessage(ctx, tx, mid, cid, now)
	})
	if err != nil {
		return denproto.Poll{}, err
	}
	view := p.view(s.MemberID)
	if !changed {
		return *view, nil
	}
	mine := view.Mine
	if mine == nil {
		mine = []int{}
	}
	if err := d.Hub.Publish(denproto.EventPollUpdated, denproto.PollUpdated{MessageID: denproto.FormatID(mid), ChannelID: c.ID,
		Tally: view.Tally, Mine: &mine}, OnlyMember(s.MemberID)); err != nil {
		return *view, err
	}
	d.pollChanged(mid)
	return *view, nil
}

// EndPoll ends a poll before its time, at its author's word, and tells
// everyone who sees the channel its final totals. One that ended already
// stays as it is.
func (d *Den) EndPoll(ctx context.Context, s *Session, messageID string) (denproto.Poll, error) {
	d.changes.Lock()
	defer d.changes.Unlock()
	c, cid, mid, err := d.reactable(ctx, s, messageID)
	if err != nil {
		return denproto.Poll{}, err
	}
	now := d.now()
	var p *pollRow
	changed := false
	err = d.tx(ctx, func(tx *sql.Tx) error {
		found, err := d.pollOf(ctx, tx, mid, now)
		if err != nil {
			return err
		}
		p = found
		if p.author != s.MemberID {
			return forbidden("only its author ends a poll early")
		}
		if p.poll.Ended {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_polls SET ended = 1, ends_at = ? WHERE message_id = ?`, now.UnixMilli(), mid); err != nil {
			return err
		}
		p.poll.Ended, p.poll.EndsAt, changed = true, now.UnixMilli(), true
		return logMessage(ctx, tx, mid, cid, now)
	})
	if err != nil {
		return denproto.Poll{}, err
	}
	if changed {
		d.log.Infof("Member %d ended poll %d early", s.MemberID, mid)
		if err := d.publishEnd(c, p); err != nil {
			return *p.view(s.MemberID), err
		}
		d.planPolls()
	}
	return *p.view(s.MemberID), nil
}

// publishEnd tells everyone who sees a poll's channel its final totals,
// and who voted for what where it shows it then.
func (d *Den) publishEnd(c denproto.Channel, p *pollRow) error {
	return d.Hub.Publish(denproto.EventPollEnded, denproto.PollEnded{MessageID: denproto.FormatID(p.message), ChannelID: c.ID,
		EndsAt: p.poll.EndsAt, Tally: p.poll.TallyFor(p.voterIDs(), true)}, audienceOf(c))
}

// pollBatch gathers the polls whose votes changed, for the next batch of
// totals.
type pollBatch struct {
	mu        sync.Mutex
	pending   map[int64]bool
	scheduled bool
	closed    bool
}

// close drops the batch waiting, as the den closes: its clients start
// again from a ready with the next process.
func (b *pollBatch) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending, b.closed = nil, true
}

// pollChanged has a poll's totals go out with the next batch.
func (d *Den) pollChanged(message int64) {
	b := &d.pollBatch
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[int64]bool{}
	}
	b.pending[message] = true
	if !b.scheduled && !b.closed {
		b.scheduled = true
		time.AfterFunc(d.PollDelay, d.flushPolls)
	}
}

// flushPolls sends the totals of the polls whose votes changed since the
// last batch, each to those who may see them: as votes come in, everyone
// who sees the channel; after voting, the poll's voters, and to the rest
// how many voted. A poll that ended has had its final totals sent, and
// one deleted goes unsaid.
func (d *Den) flushPolls() {
	b := &d.pollBatch
	b.mu.Lock()
	ids := slices.Sorted(maps.Keys(b.pending))
	b.pending, b.scheduled = nil, false
	closed := b.closed
	b.mu.Unlock()
	if closed || len(ids) == 0 {
		return
	}
	d.changes.Lock()
	defer d.changes.Unlock()
	ctx := context.Background()
	polls, err := d.polls(ctx, d.db, ids, d.now())
	if err != nil {
		d.log.Errorf("send the polls' totals: %v", err)
		return
	}
	for _, id := range ids {
		p := polls[id]
		if p == nil || p.poll.Ended {
			continue
		}
		c, err := d.channel(ctx, d.db, p.channel)
		if err == nil {
			err = d.publishTotals(c, p)
		}
		if err != nil && !errors.Is(err, errChannelNotFound) {
			d.log.Errorf("send poll %d's totals: %v", id, err)
		}
	}
}

// publishTotals sends a poll's totals to those who may see them.
func (d *Den) publishTotals(c denproto.Channel, p *pollRow) error {
	update := denproto.PollUpdated{MessageID: denproto.FormatID(p.message), ChannelID: c.ID}
	votes := p.voterIDs()
	if p.poll.ShowTotals == denproto.TotalsLive {
		update.Tally = p.poll.TallyFor(votes, false)
		return d.Hub.Publish(denproto.EventPollUpdated, update, audienceOf(c))
	}
	voters := p.voters()
	update.Tally = p.poll.TallyFor(votes, true)
	if err := d.Hub.Publish(denproto.EventPollUpdated, update, Voters(voters, c.StaffOnly)); err != nil {
		return err
	}
	update.Tally = p.poll.TallyFor(votes, false)
	return d.Hub.Publish(denproto.EventPollUpdated, update, NonVoters(voters, c.StaffOnly))
}

// pollTimer ends polls at their time: a timer for the next to end, set
// as the den starts and as polls are posted and ended.
type pollTimer struct {
	// pass lets one pass run at a time, and Stop wait for one under way.
	pass sync.Mutex

	mu      sync.Mutex
	timer   *time.Timer
	started bool
	closed  bool
}

// StartPolls starts ending polls at their time, beginning with those whose
// time passed while the den was stopped. Without it, as in tests, polls end
// only when endPolls is called. It starts again after StopPolls, as hosting
// does (M9.1).
func (d *Den) StartPolls() {
	t := &d.pollTimer
	t.mu.Lock()
	t.started, t.closed = true, false
	t.mu.Unlock()
	d.planPolls()
}

// StopPolls stops ending polls, and waits for a pass under way.
func (d *Den) StopPolls() {
	t := &d.pollTimer
	t.mu.Lock()
	t.closed = true
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.mu.Unlock()
	t.pass.Lock()
	t.pass.Unlock()
}

// planPolls sets the timer for the next poll to end.
func (d *Den) planPolls() {
	t := &d.pollTimer
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if !t.started || t.closed {
		return
	}
	var next sql.NullInt64
	if err := d.db.QueryRow(`SELECT min(ends_at) FROM den_polls WHERE ended = 0`).Scan(&next); err != nil {
		d.log.Errorf("plan ending polls: %v", err)
		next = sql.NullInt64{Int64: d.now().Add(time.Minute).UnixMilli(), Valid: true}
	}
	if !next.Valid {
		return
	}
	t.timer = time.AfterFunc(max(time.UnixMilli(next.Int64).Sub(d.now()), 0), d.endPolls)
}

// endPolls ends the polls whose time came, then plans the next.
func (d *Den) endPolls() {
	t := &d.pollTimer
	t.pass.Lock()
	defer t.pass.Unlock()
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	if err := d.endDue(context.Background()); err != nil {
		d.log.Errorf("end polls: %v", err)
	}
	d.planPolls()
}

// endDue ends every poll whose time came, records each in the change log
// and tells everyone who sees its channel its final totals.
func (d *Den) endDue(ctx context.Context) error {
	d.changes.Lock()
	defer d.changes.Unlock()
	now := d.now()
	var ended []*pollRow
	err := d.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT message_id FROM den_polls WHERE ended = 0 AND ends_at <= ?`, now.UnixMilli())
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		polls, err := d.polls(ctx, tx, ids, now)
		if err != nil {
			return err
		}
		for _, id := range ids {
			p := polls[id]
			if p == nil {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE den_polls SET ended = 1 WHERE message_id = ?`, id); err != nil {
				return err
			}
			if err := logMessage(ctx, tx, id, p.channel, now); err != nil {
				return err
			}
			ended = append(ended, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range ended {
		c, err := d.channel(ctx, d.db, p.channel)
		if errors.Is(err, errChannelNotFound) {
			continue
		}
		if err == nil {
			err = d.publishEnd(c, p)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
