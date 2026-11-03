package den

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestReactionsOverHTTP checks the reactions' routes (M15.3): a member adds
// one to a channel's message and takes it back, staff take an emoji's off,
// a member can't, a DM's set goes in sealed against its revision, a stale
// one meeting a conflict that carries the set as it is, and each counts
// toward the member's other writes.
func TestReactionsOverHTTP(t *testing.T) {
	f := newFixture(t)
	alice, _ := f.owner()
	bob := f.member(alice, "bob")
	a := f.session(alice)
	c := f.channel(a, "general", denproto.KindText, false)
	var m denproto.Message
	if status := f.call(http.MethodPost, "/api/channels/"+c.ID+"/messages", alice,
		denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Text: "react"}, &m); status != http.StatusCreated {
		t.Fatalf("send: %d", status)
	}
	path := "/api/messages/" + m.ID + "/reactions"
	thumbs := denproto.ReactionRequest{Emoji: "👍"}
	for _, step := range []struct {
		path  string
		token denproto.Bytes
		body  any
		want  int
	}{
		{path, bob, thumbs, http.StatusNoContent},
		{path, bob, thumbs, http.StatusNoContent},
		{path, alice, thumbs, http.StatusNoContent},
		{path, bob, denproto.ReactionRequest{Emoji: "nope"}, http.StatusBadRequest},
		{path + "/remove", alice, thumbs, http.StatusNoContent},
		{path + "/clear", bob, thumbs, http.StatusForbidden},
		{path + "/clear", alice, thumbs, http.StatusNoContent},
		{"/api/messages/999999/reactions", bob, thumbs, http.StatusNotFound},
	} {
		if status := f.call(http.MethodPost, step.path, step.token, step.body, nil); status != step.want {
			t.Fatalf("POST %s: %d, want %d", step.path, status, step.want)
		}
	}
	var h denproto.History
	if f.call(http.MethodGet, "/api/channels/"+c.ID+"/messages", bob, nil, &h) != http.StatusOK || len(h.Messages) != 1 || len(h.Messages[0].Reactions) != 0 {
		t.Fatalf("history after the clearing: %+v", h)
	}

	// A DM's key, which alice checked, for her set.
	b := f.session(bob)
	var dm denproto.Channel
	if f.call(http.MethodPost, "/api/dms", alice, denproto.DMRequest{MemberID: denproto.FormatID(b.MemberID)}, &dm) != http.StatusOK {
		t.Fatal("open a DM")
	}
	info, _ := f.d.Info()
	cid, _ := denproto.ParseID(dm.ID)
	xctx := denproto.DMExchangeContext(info.ID, cid, a.MemberID, b.MemberID)
	starter, offer, _ := denproto.StartExchange(xctx)
	keys := "/api/dms/" + dm.ID + "/keys"
	var k denproto.DMKey
	if f.call(http.MethodPost, keys, alice, denproto.KeyStartRequest{Offer: offer, State: denproto.Random(100)}, &k) != http.StatusCreated {
		t.Fatal("start a key")
	}
	_, answer, _ := denproto.AnswerExchange(xctx, offer)
	f.call(http.MethodPost, keys+"/"+k.ID+"/answer", bob, denproto.KeyAnswerRequest{Answer: answer, State: denproto.Random(100)}, nil)
	reveal, _, _ := starter.Finish(answer)
	f.call(http.MethodPost, keys+"/"+k.ID+"/reveal", alice, denproto.KeyRevealRequest{Reveal: reveal}, nil)
	if f.call(http.MethodPut, keys+"/"+k.ID+"/sealed", alice, denproto.KeySealRequest{Sealed: denproto.Random(denproto.SealedKeySize)}, nil) != http.StatusOK {
		t.Fatal("seal the key")
	}
	var dmMessage denproto.Message
	if status := f.call(http.MethodPost, "/api/channels/"+dm.ID+"/messages", alice, denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes),
		Sealed: denproto.Random(80), KeyID: k.ID}, &dmMessage); status != http.StatusCreated {
		t.Fatalf("a DM's message: %d", status)
	}
	sealed := "/api/messages/" + dmMessage.ID + "/reactions/sealed"
	set := denproto.DMReactionsRequest{Revision: 0, KeyID: k.ID, Sealed: denproto.Random(denproto.SealedReactionsSize)}
	var held denproto.SealedReactions
	if status := f.call(http.MethodPut, sealed, alice, set, &held); status != http.StatusOK || held.Revision != 1 || held.Check() != nil {
		t.Fatalf("a set: %d %+v", status, held)
	}
	resp := f.put(sealed, alice, set)
	var conflict struct {
		Error     denproto.Error           `json:"error"`
		Reactions denproto.SealedReactions `json:"reactions"`
	}
	if resp.StatusCode != http.StatusConflict || json.NewDecoder(resp.Body).Decode(&conflict) != nil ||
		conflict.Error.Code != denproto.CodeEditConflict || conflict.Reactions.Revision != 1 {
		t.Fatalf("a stale set: %d %+v", resp.StatusCode, conflict)
	}
	resp.Body.Close()
	if status := f.call(http.MethodPost, "/api/messages/"+dmMessage.ID+"/reactions", alice, thumbs, nil); status != http.StatusBadRequest {
		t.Fatalf("a DM's reaction in the clear: %d", status)
	}

	// Reactions count toward other writes: 30 every 10 seconds.
	limited := false
	for i := range 40 {
		emoji := []string{"😀", "😃"}[i%2]
		if status := f.call(http.MethodPost, path, bob, denproto.ReactionRequest{Emoji: emoji}, nil); status == http.StatusTooManyRequests {
			limited = i >= 20
			break
		}
	}
	if !limited {
		t.Error("a member's reactions passed what other writes allow")
	}
}

// put sends a PUT with a JSON body and a bearer token, and returns the
// answer as it is.
func (f *fixture) put(path string, token denproto.Bytes, body any) *http.Response {
	f.t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, f.srv.URL+path, io.NopCloser(bytes.NewReader(data)))
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	return resp
}
