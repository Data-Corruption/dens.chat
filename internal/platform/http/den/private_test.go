package den

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// call makes a request as a client does, with a bearer token when given,
// decodes a JSON answer into out when given, and returns the status.
func (f *fixture) call(method, path string, token denproto.Bytes, body, out any) int {
	f.t.Helper()
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, r)
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Content-Type", "application/json")
	if token != nil {
		req.Header.Set("Authorization", "Bearer "+token.String())
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			f.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// approve signs in on a new device with key, over HTTP: it asks with the
// password, the device holding existing answers and approves, and the new
// device reveals, waits, and takes its session.
func (f *fixture) approve(existing denproto.Bytes, username string, verifier denproto.Bytes, key ed25519.PrivateKey) denproto.Bytes {
	f.t.Helper()
	info, _ := f.d.Info()
	var ch denproto.ChallengeResponse
	if f.call(http.MethodPost, "/api/auth/challenge", nil, denproto.ChallengeRequest{ClientNonce: denproto.Random(32)}, &ch) != http.StatusOK {
		f.t.Fatal("challenge")
	}
	pub := denproto.Bytes(key.Public().(ed25519.PublicKey))
	xctx := denproto.DeviceExchangeContext(info.ID, username, denproto.ID(ed25519.PublicKey(pub)))
	starter, offer, _ := denproto.StartExchange(xctx)
	var pending denproto.PendingSignIn
	if status := f.call(http.MethodPost, "/api/auth/password", nil, denproto.PasswordLoginRequest{
		Username: username, Verifier: verifier, PublicKey: pub, DeviceLabel: "laptop",
		Nonce: ch.Nonce, Proof: denproto.Prove(key, info.ID, ch.Nonce), Offer: offer,
	}, &pending); status != http.StatusAccepted {
		f.t.Fatalf("password sign-in: %d", status)
	}

	// The existing device finds the request in its snapshot.
	c, first := f.dial(existing, "")
	c.CloseNow()
	var ready denproto.Ready
	if json.Unmarshal(first[0].D, &ready); len(ready.DeviceRequests) != 1 {
		f.t.Fatalf("requests in the snapshot: %+v", ready.DeviceRequests)
	}
	req := ready.DeviceRequests[0]
	responder, answer, _ := denproto.AnswerExchange(denproto.DeviceExchangeContext(info.ID, username, req.KeyID), req.Offer)
	requests := "/api/me/device-requests/" + req.ID.String()
	if status := f.call(http.MethodPost, requests+"/answer", existing, denproto.DeviceAnswerRequest{Answer: answer}, nil); status != http.StatusOK {
		f.t.Fatalf("answer: %d", status)
	}

	var st denproto.PendingStatus
	if status := f.call(http.MethodGet, "/api/auth/pending?after=1", pending.PendingToken, nil, &st); status != http.StatusOK || st.Answer == nil {
		f.t.Fatalf("waiting for the answer: %d %+v", status, st)
	}
	reveal, fresh, err := starter.Finish(*st.Answer)
	if err != nil {
		f.t.Fatal(err)
	}
	if status := f.call(http.MethodPost, "/api/auth/pending/reveal", pending.PendingToken, denproto.KeyRevealRequest{Reveal: reveal}, nil); status != http.StatusNoContent {
		f.t.Fatalf("reveal: %d", status)
	}
	old, err := responder.Finish(reveal)
	if err != nil || old.Check != fresh.Check {
		f.t.Fatalf("the check codes: %v", err)
	}
	handover, _ := denproto.SealHandover(old.Key, xctx, denproto.NewSeal())
	if status := f.call(http.MethodPost, requests+"/approve", existing, denproto.ApproveRequest{Handover: handover}, nil); status != http.StatusNoContent {
		f.t.Fatalf("approve: %d", status)
	}
	if status := f.call(http.MethodGet, "/api/auth/pending?after="+strconv.FormatInt(st.Version, 10), pending.PendingToken, nil, &st); status != http.StatusOK ||
		st.Status != denproto.PendingApproved || st.Session == nil || !denproto.Equal(st.Handover, handover) {
		f.t.Fatalf("approved: %d %+v", status, st)
	}
	return st.Session.Token
}

// The routes of M1.7 answer as the protocol says, and a waiting sign-in
// is asked after only with its own token.
func TestPrivateRoutes(t *testing.T) {
	f := newFixture(t)
	alice, _ := f.owner()
	bob := f.member(alice, "bob")
	ctx := context.Background()
	b, _ := f.d.Authenticate(ctx, bob)
	var dm denproto.Channel
	if f.call(http.MethodPost, "/api/dms", alice, denproto.DMRequest{MemberID: denproto.FormatID(b.MemberID)}, &dm) != http.StatusOK {
		t.Fatal("open a DM")
	}
	info, _ := f.d.Info()
	a, _ := f.d.Authenticate(ctx, alice)
	cid, _ := denproto.ParseID(dm.ID)
	starter, offer, _ := denproto.StartExchange(denproto.DMExchangeContext(info.ID, cid, a.MemberID, b.MemberID))
	keys := "/api/dms/" + dm.ID + "/keys"
	var k denproto.DMKey
	if status := f.call(http.MethodPost, keys, alice, denproto.KeyStartRequest{Offer: offer, State: denproto.Random(100)}, &k); status != http.StatusCreated || k.Stage != denproto.StageOffered {
		t.Fatalf("start: %d %+v", status, k)
	}
	if status := f.call(http.MethodPost, keys, bob, denproto.KeyStartRequest{Offer: offer, State: denproto.Random(100)}, nil); status != http.StatusConflict {
		t.Fatalf("a second start: %d", status)
	}
	var list denproto.DMKeyList
	if status := f.call(http.MethodGet, keys, bob, nil, &list); status != http.StatusOK || len(list.Keys) != 1 || list.Keys[0].Exchange == nil {
		t.Fatalf("list: %d %+v", status, list)
	}
	_, answer, _ := denproto.AnswerExchange(denproto.DMExchangeContext(info.ID, cid, a.MemberID, b.MemberID), offer)
	if status := f.call(http.MethodPost, keys+"/"+k.ID+"/answer", bob, denproto.KeyAnswerRequest{Answer: answer, State: denproto.Random(100)}, &k); status != http.StatusOK || k.Stage != denproto.StageAnswered {
		t.Fatalf("answer: %d %+v", status, k)
	}
	reveal, _, _ := starter.Finish(answer)
	if status := f.call(http.MethodPost, keys+"/"+k.ID+"/reveal", alice, denproto.KeyRevealRequest{Reveal: reveal}, &k); status != http.StatusOK || k.Stage != denproto.StageRevealed {
		t.Fatalf("reveal: %d %+v", status, k)
	}
	if status := f.call(http.MethodPut, keys+"/"+k.ID+"/sealed", alice, denproto.KeySealRequest{Sealed: denproto.Random(denproto.SealedKeySize)}, &k); status != http.StatusOK || len(k.Checks) != 1 {
		t.Fatalf("seal: %d %+v", status, k)
	}
	if status := f.call(http.MethodPut, keys+"/999/sealed", alice, denproto.KeySealRequest{Sealed: denproto.Random(denproto.SealedKeySize)}, nil); status != http.StatusNotFound {
		t.Fatalf("another key: %d", status)
	}
	if status := f.call(http.MethodPost, "/api/me/seal", alice, denproto.StartOverRequest{Verifier: denproto.Random(32), SealCheck: denproto.Random(32)}, nil); status != http.StatusForbidden {
		t.Fatalf("starting over with the wrong password: %d", status)
	}

	// A sealed upload goes in as it came.
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/api/uploads/sealed", strings.NewReader("noise"))
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Authorization", "Bearer "+alice.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var file denproto.File
	json.NewDecoder(resp.Body).Decode(&file)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || !file.Sealed || file.Size != 5 {
		t.Fatalf("a sealed upload: %d %+v", resp.StatusCode, file)
	}

	// Asking after a sign-in takes its token: not a session's, nor none.
	if status := f.call(http.MethodGet, "/api/auth/pending", alice, nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("asking with a session token: %d", status)
	}
	if status := f.call(http.MethodGet, "/api/auth/pending", nil, nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("asking with nothing: %d", status)
	}
	if status := f.call(http.MethodPost, "/api/me/device-requests/"+denproto.Bytes(denproto.Random(16)).String()+"/refuse", alice, nil, nil); status != http.StatusNotFound {
		t.Fatalf("refusing nothing: %d", status)
	}
	if status := f.call(http.MethodPost, "/api/me/device-requests/short/refuse", alice, nil, nil); status != http.StatusNotFound {
		t.Fatalf("refusing a malformed ID: %d", status)
	}
}

// No frame passes what a client reads, however large the events queued
// behind it, whether replayed on a resume or sent live.
func TestFramesFitWhatClientsRead(t *testing.T) {
	f := newFixture(t)
	token, _ := f.owner()
	c, first := f.dial(token, "")
	var ready denproto.Ready
	json.Unmarshal(first[0].D, &ready)
	c.CloseNow()
	big := strings.Repeat("x", 200<<10)
	publish := func() {
		for range 20 {
			if err := f.d.Hub.Publish("test.big", map[string]string{"x": big}, dens.Everyone); err != nil {
				t.Fatal(err)
			}
		}
	}
	publish()
	c, events := f.dial(token, ready.Epoch+"."+strconv.FormatUint(ready.Seq, 10))
	defer c.CloseNow()
	got := len(events) - 2 // resumed, and presence
	for got < 20 {
		got += len(read(t, c))
	}
	publish()
	for got < 40 {
		got += len(read(t, c))
	}
}
