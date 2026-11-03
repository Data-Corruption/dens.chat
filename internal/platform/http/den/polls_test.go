package den

import (
	"net/http"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestPollsOverHTTP checks the polls' routes (M15.4): a poll posted with
// its message, a member's vote answered with the poll as they now see it,
// its author alone ending it early, and no vote after its end.
func TestPollsOverHTTP(t *testing.T) {
	f := newFixture(t)
	alice, _ := f.owner()
	bob := f.member(alice, "bob")
	a := f.session(alice)
	c := f.channel(a, "general", denproto.KindText, false)
	var m denproto.Message
	poll := denproto.PollRequest{Choices: []string{"yes", "no"}, Hours: 24, ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsVoted}
	if status := f.call(http.MethodPost, "/api/channels/"+c.ID+"/messages", alice,
		denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Text: "Ship it?", Poll: &poll}, &m); status != http.StatusCreated {
		t.Fatalf("send: %d", status)
	}
	if m.Poll == nil || !slices.Equal(m.Poll.Choices, poll.Choices) || m.Poll.Totals != nil {
		t.Fatalf("the poll went out as %+v", m.Poll)
	}
	path := "/api/messages/" + m.ID + "/poll/"
	var p denproto.Poll
	if status := f.call(http.MethodPost, path+"vote", bob, denproto.VoteRequest{Choices: []int{0}}, &p); status != http.StatusOK ||
		!slices.Equal(p.Mine, []int{0}) || !slices.Equal(p.Totals, []int{1, 0}) {
		t.Fatalf("bob's vote: %d, %+v", status, p)
	}
	for _, step := range []struct {
		path  string
		token denproto.Bytes
		body  any
		want  int
	}{
		{path + "vote", bob, denproto.VoteRequest{Choices: []int{1}}, http.StatusForbidden},
		{path + "vote", alice, denproto.VoteRequest{Choices: []int{0, 1}}, http.StatusBadRequest},
		{path + "end", bob, nil, http.StatusForbidden},
		{path + "end", alice, nil, http.StatusOK},
		{path + "vote", alice, denproto.VoteRequest{Choices: []int{1}}, http.StatusForbidden},
		{"/api/messages/999999/poll/vote", bob, denproto.VoteRequest{Choices: []int{0}}, http.StatusNotFound},
	} {
		if status := f.call(http.MethodPost, step.path, step.token, step.body, nil); status != step.want {
			t.Fatalf("POST %s: %d, want %d", step.path, status, step.want)
		}
	}
	var h denproto.History
	if f.call(http.MethodGet, "/api/channels/"+c.ID+"/messages", alice, nil, &h) != http.StatusOK || len(h.Messages) != 1 ||
		h.Messages[0].Poll == nil || !h.Messages[0].Poll.Ended || !slices.Equal(h.Messages[0].Poll.Totals, []int{1, 0}) {
		t.Fatalf("history after the end: %+v", h)
	}
}
