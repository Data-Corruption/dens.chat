package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Polls (M15.4) pass through as the den sends them, each as this member
// may see it: the den holds to a poll's rules. Its events carry how it
// stands, the totals in batches, a voter's own votes to their sessions,
// and the final totals as it ends, which the cache applies to its copy of
// the message, as it does a reaction's.

// applyPoll applies a poll's event to a message as the den sent it, and
// reports whether that changed it. An update carries how the poll stands
// as far as this member may see: the totals, and who voted for what with
// them, replacing those held, where it carries them, and this member's own
// votes where it's to their sessions. Nothing changes a poll once it
// ended, but its end, which carries the final totals.
func applyPoll(m *denproto.Message, e denproto.Event) bool {
	if m.Poll == nil {
		return false
	}
	p := *m.Poll
	switch e.T {
	case denproto.EventPollUpdated:
		var u denproto.PollUpdated
		if json.Unmarshal(e.D, &u) != nil || u.MessageID != m.ID || p.Ended {
			return false
		}
		p.Voted = u.Voted
		if u.Totals != nil {
			p.Totals, p.Voters = u.Totals, u.Voters
		}
		if u.Mine != nil {
			p.Mine = *u.Mine
			if len(p.Mine) == 0 {
				p.Mine = nil
			}
		}
	case denproto.EventPollEnded:
		var x denproto.PollEnded
		if json.Unmarshal(e.D, &x) != nil || x.MessageID != m.ID {
			return false
		}
		p.Ended, p.EndsAt, p.Tally = true, x.EndsAt, x.Tally
	default:
		return false
	}
	// One that doesn't fit the poll is left for the den's next word on it.
	if p.Check() != nil || reflect.DeepEqual(p, *m.Poll) {
		return false
	}
	m.Poll = &p
	return true
}

// checkPollSend checks a poll a message is to go with: in a channel, and
// without files or other editors, as the den would.
func checkPollSend(req denproto.SendRequest, dm bool) (*denproto.PollRequest, error) {
	if req.Poll == nil {
		return nil, nil
	}
	if dm {
		return nil, inputError(errors.New("polls go in channels, not DMs"))
	}
	if len(req.Attachments) > 0 || len(req.Editors) > 0 {
		return nil, inputError(errors.New("a poll goes without files or other editors"))
	}
	poll, err := req.Poll.Clean()
	if err != nil {
		return nil, inputError(err)
	}
	return &poll, nil
}

// Vote sets the member's vote on a channel's poll, the choices they pick by
// their place in it, or none to take it back, and returns the poll as they
// now see it.
func (m *Manager) Vote(ctx context.Context, denID, messageID string, choices []int) (*denproto.Poll, error) {
	return m.pollCall(ctx, denID, messageID, "vote", denproto.VoteRequest{Choices: choices})
}

// EndPoll ends the member's own poll before its time, and returns it as it
// ended.
func (m *Manager) EndPoll(ctx context.Context, denID, messageID string) (*denproto.Poll, error) {
	return m.pollCall(ctx, denID, messageID, "end", nil)
}

func (m *Manager) pollCall(ctx context.Context, denID, messageID, what string, req any) (*denproto.Poll, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	if err := checkID("message", messageID); err != nil {
		return nil, err
	}
	if v, ok := req.(denproto.VoteRequest); ok && len(v.Choices) > denproto.MaxPollChoices {
		return nil, inputError(errors.New("a poll has at most 10 choices"))
	}
	var p denproto.Poll
	if err := c.call(ctx, http.MethodPost, "/api/messages/"+messageID+"/poll/"+what, req, &p); err != nil {
		return nil, err
	}
	if p.Check() != nil {
		return nil, errMalformed
	}
	return &p, nil
}
