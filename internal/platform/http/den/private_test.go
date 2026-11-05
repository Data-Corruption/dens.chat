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

// signedKey publishes an identity for each member of a DM, made from a
// seal of its own, and makes the DM's key from the first, sealed to both
// and signed by the first's, as their clients do (M16.1). It returns the
// key as the first sees it.
func (f *fixture) signedKey(dm denproto.Channel, makers ...denproto.Bytes) denproto.SignedKey {
	f.t.Helper()
	ctx := context.Background()
	info, _ := f.d.Info()
	cid, _ := denproto.ParseID(dm.ID)
	key, kid := denproto.Random(denproto.DMKeySize), denproto.Random(denproto.KIDSize)
	req := denproto.SignedKeyRequest{KID: kid, Commit: denproto.KeyCommit(kid, key)}
	var ids []*denproto.IdentityKeys
	for _, token := range makers {
		s, err := f.d.Authenticate(ctx, token)
		if err != nil {
			f.t.Fatal(err)
		}
		k, err := denproto.DeriveIdentity(denproto.NewSeal(), info.ID)
		if err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, k)
		var m denproto.Member
		if status := f.call(http.MethodPut, "/api/me/identity", token, k.Public(), &m); status != http.StatusOK || m.Identity != k.Public().Hash().String() {
			f.t.Fatalf("publish an identity: %d %+v", status, m)
		}
		copied, _ := denproto.SealKeyCopy(k.Public(), info.ID, cid, kid, s.MemberID, key)
		member := denproto.KeyMember{MemberID: denproto.FormatID(s.MemberID), Identity: k.Public().Hash()}
		req.Copies = append(req.Copies, denproto.KeyCopy{MemberID: member.MemberID, Copy: copied})
		if len(req.Members) > 0 && s.MemberID < mustID(req.Members[0].MemberID) {
			req.Members = append([]denproto.KeyMember{member}, req.Members...)
		} else {
			req.Members = append(req.Members, member)
		}
	}
	first, _ := f.d.Authenticate(ctx, makers[0])
	stmt, err := req.Statement(info.ID, dm.ID, denproto.FormatID(first.MemberID))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Signature = ids[0].Sign(stmt)
	var k denproto.SignedKey
	if status := f.call(http.MethodPost, "/api/dms/"+dm.ID+"/signed-keys", makers[0], req, &k); status != http.StatusCreated {
		f.t.Fatalf("make a key: %d", status)
	}
	return k
}

func mustID(s string) int64 {
	id, err := denproto.ParseID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// The routes of M1.7 and M16.1 answer as the protocol says, and a waiting
// sign-in is asked after only with its own token.
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
	// The den starts no exchange any more (M16.1).
	_, offer, _ := denproto.StartExchange(denproto.DMExchangeContext(info.ID, cid, a.MemberID, b.MemberID))
	keys := "/api/dms/" + dm.ID + "/keys"
	if status := f.call(http.MethodPost, keys, alice, denproto.KeyStartRequest{Offer: offer, State: denproto.Random(100)}, nil); status != http.StatusBadRequest {
		t.Fatalf("start an exchange: %d", status)
	}
	if status := f.call(http.MethodGet, "/api/identities", alice, nil, nil); status != http.StatusBadRequest {
		t.Fatalf("identities of nobody: %d", status)
	}
	k := f.signedKey(dm, alice, bob)
	if k.Copy == nil || k.MadeBy != denproto.FormatID(a.MemberID) {
		t.Fatalf("the key alice made: %+v", k)
	}
	var ids denproto.IdentityList
	if status := f.call(http.MethodGet, "/api/identities?members="+denproto.FormatID(a.MemberID)+","+denproto.FormatID(b.MemberID), bob, nil, &ids); status != http.StatusOK ||
		len(ids.Identities) != 2 || ids.Identities[0].Identity().Check() != nil {
		t.Fatalf("identities: %d %+v", status, ids)
	}
	var signed denproto.SignedKeyList
	if status := f.call(http.MethodGet, "/api/dms/"+dm.ID+"/signed-keys", bob, nil, &signed); status != http.StatusOK || len(signed.Keys) != 1 ||
		signed.Keys[0].ID != k.ID || denproto.Equal(signed.Keys[0].Copy, k.Copy) {
		t.Fatalf("bob's list of the DM's signed keys: %d %+v", status, signed)
	}
	var list denproto.DMKeyList
	if status := f.call(http.MethodGet, keys, bob, nil, &list); status != http.StatusOK || len(list.Keys) != 0 {
		t.Fatalf("an older Dens's list: %d %+v", status, list)
	}
	// The member's record, written over the revision it was merged from.
	var rec denproto.Record
	if status := f.call(http.MethodGet, "/api/me/record", bob, nil, &rec); status != http.StatusOK || rec.Revision != 0 {
		t.Fatalf("the record before a write: %d %+v", status, rec)
	}
	write := denproto.RecordWrite{Revision: 0, Sealed: denproto.Random(16<<10 + 40)}
	if status := f.call(http.MethodPut, "/api/me/record", bob, write, &rec); status != http.StatusOK || rec.Revision != 1 {
		t.Fatalf("a write: %d %+v", status, rec)
	}
	if status := f.call(http.MethodPut, "/api/me/record", bob, write, nil); status != http.StatusConflict {
		t.Fatalf("a write over a stale revision: %d", status)
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
