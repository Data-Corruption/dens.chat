package den

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// testKey is a DM's key as its members' clients hold it.
type testKey struct {
	id  string
	key denproto.Bytes
}

// sealedSend turns a send's text into a DM's sealed text, with the DM's
// key, which it makes first if the DM has none. Other channels' sends
// pass as they are.
func (f *fixture) sealedSend(s *Session, channel string, req denproto.SendRequest) denproto.SendRequest {
	f.t.Helper()
	cid, err := denproto.ParseID(channel)
	if err != nil {
		return req
	}
	c, err := f.d.channel(context.Background(), f.db, cid)
	if err != nil || c.Kind != denproto.KindDM || !visible(c, s.MemberID, false) {
		return req
	}
	other, _ := dmPartner(c, s.MemberID)
	k := f.keyFor(s.MemberID, other, cid)
	req.Sealed, req.KeyID = f.sealText(k, cid, s.MemberID, req.Nonce, 1, req.Text), k.id
	req.Text = ""
	return req
}

// sealText seals a DM message's text as its author's client does.
func (f *fixture) sealText(k testKey, channel, author int64, nonce denproto.Bytes, revision int, text string) denproto.Bytes {
	f.t.Helper()
	info, _ := f.d.Info()
	id, _ := denproto.ParseID(k.id)
	sealed, err := denproto.SealDMMessage(k.key, denproto.MessageAD{DenID: info.ID, Channel: channel, Author: author,
		Nonce: nonce, KeyID: id, Revision: revision}, denproto.DMPayload{Text: text})
	if err != nil {
		f.t.Fatal(err)
	}
	return sealed
}

// openDM opens a DM message as a member's client does.
func (f *fixture) openDM(k testKey, m denproto.Message) string {
	f.t.Helper()
	info, _ := f.d.Info()
	channel, _ := denproto.ParseID(m.ChannelID)
	author, _ := denproto.ParseID(m.AuthorID)
	id, _ := denproto.ParseID(m.KeyID)
	p, err := denproto.OpenDMMessage(k.key, denproto.MessageAD{DenID: info.ID, Channel: channel, Author: author,
		Nonce: m.Nonce, KeyID: id, Revision: m.Revision}, m.Sealed)
	if err != nil {
		f.t.Fatalf("open message %s: %v", m.ID, err)
	}
	return p.Text
}

// keyFor returns a DM's key, first making it with an exchange from a to
// b in which each checks the other's digits.
func (f *fixture) keyFor(a, b, channel int64) testKey {
	f.t.Helper()
	if k, ok := f.keys[denproto.FormatID(channel)]; ok {
		return k
	}
	k := f.exchange(&Session{MemberID: a}, &Session{MemberID: b}, channel, false)
	f.keys[denproto.FormatID(channel)] = k
	return k
}

// keyAs finds one of a DM's keys as a member sees it.
func (f *fixture) keyAs(s *Session, dm, id string) denproto.DMKey {
	f.t.Helper()
	list, err := f.d.DMKeys(context.Background(), s, dm)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, k := range list.Keys {
		if k.ID == id {
			if err := denproto.CheckDMKey(k); err != nil {
				f.t.Fatalf("key %s: %v", id, err)
			}
			return k
		}
	}
	f.t.Fatalf("member %d doesn't see key %s", s.MemberID, id)
	return denproto.DMKey{}
}

// exchange runs a DM key's exchange from a to b through the den, the way
// their clients do, each opening its state from the den at each step. It
// checks the two sides' digits match, and stores both members' copies.
func (f *fixture) exchange(a, b *Session, channel int64, restart bool) testKey {
	f.t.Helper()
	ctx := context.Background()
	info, _ := f.d.Info()
	dm := denproto.FormatID(channel)
	xctx := denproto.DMExchangeContext(info.ID, channel, a.MemberID, b.MemberID)
	starter, offer, err := denproto.StartExchange(xctx)
	if err != nil {
		f.t.Fatal(err)
	}
	state, err := denproto.SealExchangeState(f.seals[a.MemberID], info.ID, channel, a.MemberID, offer.Commit, true, starter)
	if err != nil {
		f.t.Fatal(err)
	}
	k, err := f.d.StartKey(ctx, a, dm, denproto.KeyStartRequest{Offer: offer, State: state, Restart: restart})
	if err != nil {
		f.t.Fatalf("start: %v", err)
	}

	seen := f.keyAs(b, dm, k.ID)
	responder, answer, err := denproto.AnswerExchange(xctx, seen.Exchange.Offer)
	if err != nil {
		f.t.Fatal(err)
	}
	rstate, err := denproto.SealExchangeState(f.seals[b.MemberID], info.ID, channel, b.MemberID, seen.Exchange.Offer.Commit, false, responder)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.d.AnswerKey(ctx, b, dm, k.ID, denproto.KeyAnswerRequest{Answer: answer, State: rstate}); err != nil {
		f.t.Fatalf("answer: %v", err)
	}

	mine := f.keyAs(a, dm, k.ID)
	var s denproto.Starter
	if err := denproto.OpenExchangeState(f.seals[a.MemberID], info.ID, channel, a.MemberID, mine.Exchange.Offer.Commit, true, mine.Exchange.State, &s); err != nil {
		f.t.Fatalf("the starter's state: %v", err)
	}
	reveal, ra, err := s.Finish(*mine.Exchange.Answer)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.d.RevealKey(ctx, a, dm, k.ID, denproto.KeyRevealRequest{Reveal: reveal}); err != nil {
		f.t.Fatalf("reveal: %v", err)
	}

	theirs := f.keyAs(b, dm, k.ID)
	var r denproto.Responder
	if err := denproto.OpenExchangeState(f.seals[b.MemberID], info.ID, channel, b.MemberID, theirs.Exchange.Offer.Commit, false, theirs.Exchange.State, &r); err != nil {
		f.t.Fatalf("the answering side's state: %v", err)
	}
	rb, err := r.Finish(*theirs.Exchange.Reveal)
	if err != nil {
		f.t.Fatal(err)
	}
	// Each types the digits the other reads out.
	if !denproto.CheckTyped(ra.Check, true, denproto.CheckHalf(rb.Check, false)) || !denproto.CheckTyped(rb.Check, false, denproto.CheckHalf(ra.Check, true)) {
		f.t.Fatalf("the check codes differ: %s and %s", ra.Check, rb.Check)
	}
	id, _ := denproto.ParseID(k.ID)
	for _, m := range []*Session{a, b} {
		sealed, err := denproto.SealDMKey(f.seals[m.MemberID], info.ID, channel, id, m.MemberID, ra.Key)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.d.SealKey(ctx, m, dm, k.ID, denproto.KeySealRequest{Sealed: sealed}); err != nil {
			f.t.Fatalf("seal: %v", err)
		}
	}
	return testKey{id: k.ID, key: ra.Key}
}

// askSignIn asks to sign in on a new device with a password, with an offer
// that nothing will answer.
func (f *fixture) askSignIn(username, password string, dev device) (denproto.PendingSignIn, error) {
	nonce, proof := f.proof(dev)
	info, _ := f.d.Info()
	_, offer, err := denproto.StartExchange(denproto.DeviceExchangeContext(info.ID, strings.ToLower(username), dev.id()))
	if err != nil {
		f.t.Fatal(err)
	}
	// Clients salt the verifier with the username as the den stores it.
	return f.d.PasswordLogin(context.Background(), denproto.PasswordLoginRequest{
		Username: username, Verifier: denproto.Verifier(password, info.ID, strings.ToLower(username)),
		PublicKey: dev.pub(), DeviceLabel: "laptop", Nonce: nonce, Proof: proof, Offer: offer,
	})
}

// signIn signs in on a new device with a password, and has approver, one
// of the member's devices, approve it: the two run the exchange through
// the den, each checks the other's digits, and the approving device hands
// over the member's seal, which the new device opens and checks.
func (f *fixture) signIn(approver *Session, username, password string, dev device) (denproto.SignInResponse, error) {
	f.t.Helper()
	ctx := context.Background()
	nonce, proof := f.proof(dev)
	info, _ := f.d.Info()
	name := strings.ToLower(username)
	xctx := denproto.DeviceExchangeContext(info.ID, name, dev.id())
	starter, offer, err := denproto.StartExchange(xctx)
	if err != nil {
		f.t.Fatal(err)
	}
	pending, err := f.d.PasswordLogin(ctx, denproto.PasswordLoginRequest{
		Username: username, Verifier: denproto.Verifier(password, info.ID, name),
		PublicKey: dev.pub(), DeviceLabel: "laptop", Nonce: nonce, Proof: proof, Offer: offer,
	})
	if err != nil {
		return denproto.SignInResponse{}, err
	}
	st, err := f.d.Pending(ctx, pending.PendingToken, 0)
	if err != nil || st.Status != denproto.PendingWaiting {
		f.t.Fatalf("waiting: %+v %v", st, err)
	}

	var req denproto.DeviceRequest
	for _, r := range f.d.memberRequests(approver.MemberID) {
		if denproto.Equal(r.KeyID, dev.id()) {
			req = r
		}
	}
	if err := denproto.CheckDeviceRequest(req); err != nil {
		f.t.Fatalf("the request as the member's devices see it: %v", err)
	}
	responder, answer, err := denproto.AnswerExchange(denproto.DeviceExchangeContext(info.ID, name, req.KeyID), req.Offer)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.d.AnswerRequest(ctx, approver, req.ID, denproto.DeviceAnswerRequest{Answer: answer}); err != nil {
		f.t.Fatalf("answer: %v", err)
	}

	st, err = f.d.Pending(ctx, pending.PendingToken, st.Version)
	if err != nil || st.Status != denproto.PendingAnswered || st.Answer == nil {
		f.t.Fatalf("answered: %+v %v", st, err)
	}
	reveal, fresh, err := starter.Finish(*st.Answer)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.d.RevealPending(ctx, pending.PendingToken, reveal); err != nil {
		f.t.Fatalf("reveal: %v", err)
	}
	for _, r := range f.d.memberRequests(approver.MemberID) {
		if denproto.Equal(r.ID, req.ID) {
			req = r
		}
	}
	if req.Reveal == nil {
		f.t.Fatal("the member's devices don't see the reveal")
	}
	old, err := responder.Finish(*req.Reveal)
	if err != nil {
		f.t.Fatal(err)
	}
	if !denproto.CheckTyped(fresh.Check, true, denproto.CheckHalf(old.Check, false)) || !denproto.CheckTyped(old.Check, false, denproto.CheckHalf(fresh.Check, true)) {
		f.t.Fatalf("the check codes differ: %s and %s", fresh.Check, old.Check)
	}
	handover, err := denproto.SealHandover(old.Key, xctx, f.seals[approver.MemberID])
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.d.ApproveRequest(ctx, approver, req.ID, denproto.ApproveRequest{Handover: handover}); err != nil {
		return denproto.SignInResponse{}, err
	}

	st, err = f.d.Pending(ctx, pending.PendingToken, st.Version)
	if err != nil || st.Status != denproto.PendingApproved || st.Session == nil {
		f.t.Fatalf("approved: %+v %v", st, err)
	}
	seal, err := denproto.OpenHandover(fresh.Key, xctx, st.Handover)
	if err != nil {
		f.t.Fatalf("the handed-over seal: %v", err)
	}
	if check, _ := denproto.SealCheck(seal, info.ID); !denproto.Equal(check, st.Session.SealCheck) {
		f.t.Fatal("the handed-over seal isn't the member's")
	}
	return *st.Session, nil
}

func TestDMKeyExchange(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	cid, _ := denproto.ParseID(dm.ID)
	info, _ := f.d.Info()
	ownerSub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	bobSub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	carolSub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)

	xctx := denproto.DMExchangeContext(info.ID, cid, owner.MemberID, bob.MemberID)
	starter, offer, _ := denproto.StartExchange(xctx)
	state, _ := denproto.SealExchangeState(f.seals[owner.MemberID], info.ID, cid, owner.MemberID, offer.Commit, true, starter)
	k, err := f.d.StartKey(ctx, owner, dm.ID, denproto.KeyStartRequest{Offer: offer, State: state})
	if err != nil || k.Stage != denproto.StageOffered || k.StartedBy != denproto.FormatID(owner.MemberID) || !denproto.Equal(k.Exchange.State, state) {
		t.Fatalf("start: %+v %v", k, err)
	}
	// Each member hears of it with their own view: the answering side has
	// no state of theirs yet. Nobody else hears.
	var seen denproto.DMKey
	if e := <-bobSub.Events; e.T != denproto.EventDMKey || json.Unmarshal(e.D, &seen) != nil || seen.Exchange == nil || seen.Exchange.State != nil {
		t.Fatalf("bob got %s %s", e.T, e.D)
	}
	if e := <-ownerSub.Events; e.T != denproto.EventDMKey || json.Unmarshal(e.D, &seen) != nil || !denproto.Equal(seen.Exchange.State, state) {
		t.Fatalf("the owner got %s", e.T)
	}
	select {
	case e := <-carolSub.Events:
		t.Fatalf("carol got %s", e.T)
	default:
	}
	_, err = f.d.DMKeys(ctx, carol, dm.ID)
	wantCode(t, err, denproto.CodeNotFound)

	// While it runs, nothing else starts, and nothing is sent with it.
	_, err = f.d.StartKey(ctx, bob, dm.ID, denproto.KeyStartRequest{Offer: offer, State: state})
	wantCode(t, err, denproto.CodeKeyExists)
	_, err = f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(40), KeyID: k.ID})
	wantCode(t, err, denproto.CodeInvalidField)
	// Each step comes from the right member, in order.
	responder, answer, _ := denproto.AnswerExchange(xctx, offer)
	rstate, _ := denproto.SealExchangeState(f.seals[bob.MemberID], info.ID, cid, bob.MemberID, offer.Commit, false, responder)
	_, err = f.d.AnswerKey(ctx, owner, dm.ID, k.ID, denproto.KeyAnswerRequest{Answer: answer, State: rstate})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.RevealKey(ctx, owner, dm.ID, k.ID, denproto.KeyRevealRequest{Reveal: denproto.ExchangeReveal{X: denproto.Random(32), Nonce: denproto.Random(32)}})
	wantCode(t, err, denproto.CodeKeyExists)
	_, err = f.d.SealKey(ctx, owner, dm.ID, k.ID, denproto.KeySealRequest{Sealed: denproto.Random(denproto.SealedKeySize)})
	wantCode(t, err, denproto.CodeKeyExists)
	_, err = f.d.AnswerKey(ctx, bob, dm.ID, "999", denproto.KeyAnswerRequest{Answer: answer, State: rstate})
	wantCode(t, err, denproto.CodeNotFound)
	_, err = f.d.AnswerKey(ctx, bob, dm.ID, k.ID, denproto.KeyAnswerRequest{Answer: denproto.ExchangeAnswer{X: answer.X}, State: rstate})
	wantCode(t, err, denproto.CodeInvalidField)
	if _, err := f.d.AnswerKey(ctx, bob, dm.ID, k.ID, denproto.KeyAnswerRequest{Answer: answer, State: rstate}); err != nil {
		t.Fatal(err)
	}
	_, err = f.d.AnswerKey(ctx, bob, dm.ID, k.ID, denproto.KeyAnswerRequest{Answer: answer, State: rstate})
	wantCode(t, err, denproto.CodeKeyExists)
	reveal, ra, _ := starter.Finish(answer)
	_, err = f.d.RevealKey(ctx, bob, dm.ID, k.ID, denproto.KeyRevealRequest{Reveal: reveal})
	wantCode(t, err, denproto.CodeForbidden)
	// The den turns away a reveal its commitment didn't fix.
	_, err = f.d.RevealKey(ctx, owner, dm.ID, k.ID, denproto.KeyRevealRequest{Reveal: denproto.ExchangeReveal{X: reveal.X, Nonce: denproto.Random(32)}})
	wantCode(t, err, denproto.CodeInvalidField)
	if _, err := f.d.RevealKey(ctx, owner, dm.ID, k.ID, denproto.KeyRevealRequest{Reveal: reveal}); err != nil {
		t.Fatal(err)
	}
	rb, _ := responder.Finish(reveal)

	// Checking stores the member's copy, which lets them send with it.
	sealedFor := func(s *Session) denproto.Bytes {
		id, _ := denproto.ParseID(k.ID)
		sealed, _ := denproto.SealDMKey(f.seals[s.MemberID], info.ID, cid, id, s.MemberID, ra.Key)
		return sealed
	}
	_, err = f.d.SealKey(ctx, owner, dm.ID, k.ID, denproto.KeySealRequest{Sealed: denproto.Random(10)})
	wantCode(t, err, denproto.CodeInvalidField)
	mine, err := f.d.SealKey(ctx, owner, dm.ID, k.ID, denproto.KeySealRequest{Sealed: sealedFor(owner)})
	if err != nil || !mine.Checked(denproto.FormatID(owner.MemberID)) || mine.Checked(denproto.FormatID(bob.MemberID)) || mine.Exchange == nil {
		t.Fatalf("the owner's check: %+v %v", mine, err)
	}
	// The owner's state stays until bob has checked too, so the owner's
	// Dens can still show the digits bob has yet to type.
	if !denproto.Equal(mine.Exchange.State, state) {
		t.Fatal("the owner's state went with their check")
	}
	key := testKey{id: k.ID, key: ra.Key}
	nonce := denproto.Random(16)
	sent, err := f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: nonce, Sealed: f.sealText(key, cid, owner.MemberID, nonce, 1, "x"), KeyID: k.ID})
	if err != nil {
		t.Fatalf("sending after checking: %v", err)
	}
	// Bob hasn't checked, so he can't send yet, and until he has, a
	// restart can still retire the key.
	_, err = f.d.Send(ctx, bob, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(40), KeyID: k.ID})
	wantCode(t, err, denproto.CodeInvalidField)
	if !bytesEqualKeys(ra.Key, rb.Key) {
		t.Fatal("the two sides' keys differ")
	}
	theirs, err := f.d.SealKey(ctx, bob, dm.ID, k.ID, denproto.KeySealRequest{Sealed: sealedFor(bob)})
	if err != nil || len(theirs.Checks) != 2 || theirs.Exchange != nil || len(theirs.Sealed) != denproto.SealedKeySize {
		t.Fatalf("bob's check: %+v %v", theirs, err)
	}
	// Once both checked, the exchange is gone, and each sees only their own
	// copy.
	var row struct{ offer, answer, reveal []byte }
	if err := f.db.QueryRow(`SELECT offer, answer, reveal FROM den_dm_keys WHERE id = ?`, k.ID).Scan(&row.offer, &row.answer, &row.reveal); err != nil || row.offer != nil || row.answer != nil || row.reveal != nil {
		t.Fatalf("the finished exchange is kept: %v", err)
	}
	var states int
	f.db.QueryRow(`SELECT count(*) FROM den_dm_key_members WHERE state IS NOT NULL`).Scan(&states)
	if states != 0 {
		t.Fatalf("%d states outlived the exchange", states)
	}
	snap, err := f.d.snapshot(ctx, bob.MemberID, false, 0)
	if err != nil || len(snap.DMKeys) != 1 || snap.DMKeys[0].Exchange != nil || snap.DMKeys[0].Sealed == nil {
		t.Fatalf("bob's snapshot: %+v %v", snap.DMKeys, err)
	}
	if got, err := denproto.OpenDMKey(f.seals[bob.MemberID], info.ID, cid, mustID(k.ID), bob.MemberID, snap.DMKeys[0].Sealed); err != nil || !bytesEqualKeys(got, rb.Key) {
		t.Fatalf("bob's copy: %v", err)
	}
	h, err := f.d.History(ctx, bob, dm.ID, HistoryQuery{})
	if err != nil || len(h.Messages) != 1 || f.openDM(key, h.Messages[0]) != "x" || h.Messages[0].ID != sent.ID {
		t.Fatalf("bob's history: %+v %v", h, err)
	}
	// Both checked, so it stays: no restart.
	_, err = f.d.StartKey(ctx, bob, dm.ID, denproto.KeyStartRequest{Offer: offer, State: state, Restart: true})
	wantCode(t, err, denproto.CodeKeyExists)
}

func mustID(s string) int64 {
	id, err := denproto.ParseID(s)
	if err != nil {
		panic(err)
	}
	return id
}

func bytesEqualKeys(a, b []byte) bool { return len(a) == denproto.DMKeySize && denproto.Equal(a, b) }

// A restart retires a key only one member checked, and nothing more is
// sent with it.
func TestDMKeyRestart(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	cid, _ := denproto.ParseID(dm.ID)
	info, _ := f.d.Info()
	first := f.exchange(owner, bob, cid, false)
	// Pretend bob's check never happened: his digits didn't match.
	if _, err := f.db.Exec(`UPDATE den_dm_key_members SET sealed = NULL, checked_at = NULL WHERE member_id = ?`, bob.MemberID); err != nil {
		t.Fatal(err)
	}
	xctx := denproto.DMExchangeContext(info.ID, cid, bob.MemberID, owner.MemberID)
	starter, offer, _ := denproto.StartExchange(xctx)
	state, _ := denproto.SealExchangeState(f.seals[bob.MemberID], info.ID, cid, bob.MemberID, offer.Commit, true, starter)
	_, err := f.d.StartKey(ctx, bob, dm.ID, denproto.KeyStartRequest{Offer: offer, State: state})
	wantCode(t, err, denproto.CodeKeyExists)
	second, err := f.d.StartKey(ctx, bob, dm.ID, denproto.KeyStartRequest{Offer: offer, State: state, Restart: true})
	if err != nil {
		t.Fatal(err)
	}
	old := f.keyAs(owner, dm.ID, first.id)
	if old.Retired != denproto.RetiredRestarted || old.RetiredBy != denproto.FormatID(bob.MemberID) || old.Sealed == nil {
		t.Fatalf("the restarted key: %+v", old)
	}
	_, err = f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(40), KeyID: first.id})
	wantCode(t, err, denproto.CodeInvalidField)
	if got := f.keyAs(owner, dm.ID, second.ID); got.Exchange == nil || got.Exchange.State != nil || got.Stage != denproto.StageOffered {
		t.Fatalf("the new key: %+v", got)
	}
}

// Only DM messages come sealed, each with the DM's live key; the den
// passes them on unopened, with what opening them takes.
func TestSealedDMMessages(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	cid, _ := denproto.ParseID(dm.ID)
	key := f.keyFor(owner.MemberID, bob.MemberID, cid)

	for name, req := range map[string]struct {
		channel string
		req     denproto.SendRequest
	}{
		"a channel message sealed": {general.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Sealed: denproto.Random(40), KeyID: key.id}},
		"a DM message in the open": {dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x"}},
		"a DM message with both":   {dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Sealed: denproto.Random(40), KeyID: key.id}},
		"no key":                   {dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(40)}},
		"another key":              {dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(40), KeyID: "12345"}},
		"too large":                {dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(denproto.MaxSealed + 1), KeyID: key.id}},
	} {
		if _, err := f.d.Send(ctx, owner, req.channel, req.req); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("%s: %v", name, err)
		}
	}

	first := f.send(owner, dm.ID, "hello [ ] milk")
	if first.Text != "" || first.KeyID != key.id || len(first.Nonce) != denproto.NonceBytes || denproto.CheckMessage(first) != nil {
		t.Fatalf("sent: %+v", first)
	}
	nonce := denproto.Random(16)
	reply, err := f.d.Send(ctx, bob, dm.ID, denproto.SendRequest{Nonce: nonce, Sealed: f.sealText(key, cid, bob.MemberID, nonce, 1, "hi"), KeyID: key.id, ReplyTo: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	// A reply carries the original sealed, for the client to quote.
	if r := reply.Reply; r == nil || r.Text != "" || f.openDM(key, denproto.Message{ChannelID: dm.ID, AuthorID: r.AuthorID, KeyID: r.KeyID, Nonce: r.Nonce, Revision: r.Revision, Sealed: r.Sealed}) != "hello [ ] milk" {
		t.Fatalf("the reply's quote: %+v", reply.Reply)
	}
	if err := denproto.CheckMessage(reply); err != nil {
		t.Fatal(err)
	}
	// History carries a DM message's nonce, since opening it takes it, and
	// still leaves a channel message's out.
	f.send(owner, general.ID, "in the open")
	for channel, wantNonce := range map[string]bool{dm.ID: true, general.ID: false} {
		h, err := f.d.History(ctx, bob, channel, HistoryQuery{})
		if err != nil || len(h.Messages) == 0 {
			t.Fatalf("history: %v", err)
		}
		for _, m := range h.Messages {
			if (len(m.Nonce) > 0) != wantNonce || denproto.CheckMessage(m) != nil {
				t.Fatalf("history of %s: %+v", channel, m)
			}
		}
	}

	// Edits come sealed too, at the next revision, and the den takes the
	// edit's word for whether the text changed.
	ticked := f.sealText(key, cid, owner.MemberID, first.Nonce, 2, "hello [x] milk")
	_, err = f.d.Edit(ctx, owner, first.ID, denproto.EditRequest{Revision: 1, Text: "hello [x] milk"})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.Edit(ctx, owner, first.ID, denproto.EditRequest{Revision: 1, Sealed: ticked})
	wantCode(t, err, denproto.CodeInvalidField)
	m, err := f.d.Edit(ctx, owner, first.ID, denproto.EditRequest{Revision: 1, Sealed: ticked, KeyID: key.id, Unedited: true})
	if err != nil || m.Revision != 2 || m.EditedAt != 0 || f.openDM(key, m) != "hello [x] milk" {
		t.Fatalf("a tick: %+v %v", m, err)
	}
	m, err = f.d.Edit(ctx, owner, first.ID, denproto.EditRequest{Revision: 2, Sealed: f.sealText(key, cid, owner.MemberID, first.Nonce, 3, "bye"), KeyID: key.id})
	if err != nil || m.Revision != 3 || m.EditedAt == 0 || f.openDM(key, m) != "bye" {
		t.Fatalf("an edit: %+v %v", m, err)
	}
	h, _ := f.d.History(ctx, owner, general.ID, HistoryQuery{})
	_, err = f.d.Edit(ctx, owner, h.Messages[0].ID, denproto.EditRequest{Revision: 1, Text: "x", Unedited: true})
	wantCode(t, err, denproto.CodeInvalidField)
	// The den can't tick what it can't read.
	_, err = f.d.SetTask(ctx, owner, first.ID, 0, denproto.TaskRequest{Checked: true, Text: "milk"})
	wantCode(t, err, denproto.CodeInvalidField)
}

// Starting over retires the member's DM keys and drops their copies, signs
// out their other devices, and takes the password.
func TestStartOver(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	info, _ := f.d.Info()
	dms := map[*Session]denproto.Channel{}
	for _, other := range []*Session{owner, carol} {
		dm, _ := f.d.OpenDM(ctx, bob, denproto.DMRequest{MemberID: denproto.FormatID(other.MemberID)})
		dms[other] = dm
		f.send(other, dm.ID, "before")
	}
	laptop, err := f.signIn(bob, "bob", "password", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	seal := denproto.NewSeal()
	check, _ := denproto.SealCheck(seal, info.ID)
	_, err = f.d.StartOver(ctx, bob, denproto.StartOverRequest{Verifier: f.verifier("bob", "wrong"), SealCheck: check})
	wantCode(t, err, denproto.CodeWrongPassword)
	_, err = f.d.StartOver(ctx, bob, denproto.StartOverRequest{Verifier: f.verifier("bob", "password"), SealCheck: check[:8]})
	wantCode(t, err, denproto.CodeInvalidField)
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	out, err := f.d.StartOver(ctx, bob, denproto.StartOverRequest{Verifier: f.verifier("bob", "password"), SealCheck: check})
	if err != nil || out.SignedOut != 1 {
		t.Fatalf("start over: %+v %v", out, err)
	}
	f.seals[bob.MemberID] = seal
	if _, err := f.d.Authenticate(ctx, laptop.Token); err == nil {
		t.Fatal("the other device, which holds the old seal, is still signed in")
	}
	// The other member hears the key retired, and keeps their copy.
	var retired denproto.DMKey
	if e := <-sub.Events; e.T != denproto.EventDMKey || json.Unmarshal(e.D, &retired) != nil ||
		retired.Retired != denproto.RetiredStartedOver || retired.RetiredBy != denproto.FormatID(bob.MemberID) || retired.Sealed == nil {
		t.Fatalf("the owner heard %s %s", e.T, e.D)
	}
	snap, _ := f.d.snapshot(ctx, bob.MemberID, false, 0)
	if !denproto.Equal(snap.SealCheck, check) || len(snap.DMKeys) != 2 {
		t.Fatalf("bob's snapshot: %+v", snap.DMKeys)
	}
	for _, k := range snap.DMKeys {
		if k.Sealed != nil || k.Retired != denproto.RetiredStartedOver || len(k.Checks) != 1 {
			t.Fatalf("bob's view of an old key: %+v", k)
		}
	}
	// Nothing more goes out under the old keys; a new exchange, with the
	// new seal, lets both send again.
	dm := dms[owner]
	cid, _ := denproto.ParseID(dm.ID)
	old := f.keys[dm.ID]
	nonce := denproto.Random(16)
	_, err = f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: nonce, Sealed: f.sealText(old, cid, owner.MemberID, nonce, 1, "x"), KeyID: old.id})
	wantCode(t, err, denproto.CodeInvalidField)
	fresh := f.exchange(bob, owner, cid, false)
	f.keys[dm.ID] = fresh
	f.send(owner, dm.ID, "after")
	if fresh.id == old.id {
		t.Fatal("the new exchange reused the key's ID")
	}
}

// A former member who comes back with another seal starts over: nothing
// opens the copies the old one sealed.
func TestRejoinWithAnotherSeal(t *testing.T) {
	f, owner, _ := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(carol.MemberID)})
	f.send(carol, dm.ID, "hi")
	if err := f.d.Leave(ctx, carol); err != nil {
		t.Fatal(err)
	}
	// With the same seal, as the same install would, the keys stand.
	same, err := f.joinAs(f.invite(owner, 1).Code, "carol", "password", newDevice(), f.seals[carol.MemberID])
	if err != nil {
		t.Fatal(err)
	}
	back, _ := f.d.Authenticate(ctx, same.Token)
	if k := f.keyAs(back, dm.ID, f.keys[dm.ID].id); k.Retired != "" || k.Sealed == nil {
		t.Fatalf("the key after coming back with the same seal: %+v", k)
	}
	if err := f.d.Leave(ctx, back); err != nil {
		t.Fatal(err)
	}
	other, err := f.joinAs(f.invite(owner, 1).Code, "carol", "password", newDevice(), denproto.NewSeal())
	if err != nil {
		t.Fatal(err)
	}
	back, _ = f.d.Authenticate(ctx, other.Token)
	if k := f.keyAs(back, dm.ID, f.keys[dm.ID].id); k.Retired != denproto.RetiredStartedOver || k.Sealed != nil {
		t.Fatalf("the key after coming back with another seal: %+v", k)
	}
}

func TestDeviceApproval(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	info, _ := f.d.Info()

	// Without another device, a password alone doesn't sign in.
	lone := f.member(owner, "lone")
	if err := f.d.RevokeDevice(ctx, lone, lone.KeyID); err != nil {
		t.Fatal(err)
	}
	_, err := f.askSignIn("lone", "password", newDevice())
	wantCode(t, err, denproto.CodeNoOtherDevice)

	// The request waits for the member's devices, and a refusal keeps the
	// device out.
	dev := newDevice()
	pending, err := f.askSignIn("bob", "password", dev)
	if err != nil {
		t.Fatal(err)
	}
	reqs := f.d.memberRequests(bob.MemberID)
	if len(reqs) != 1 || !denproto.Equal(reqs[0].KeyID, dev.id()) || reqs[0].Label != "laptop" || len(f.d.memberRequests(owner.MemberID)) != 0 {
		t.Fatalf("requests: %+v", reqs)
	}
	snap, _ := f.d.snapshot(ctx, bob.MemberID, false, 0)
	if len(snap.DeviceRequests) != 1 {
		t.Fatalf("bob's snapshot: %+v", snap.DeviceRequests)
	}
	_, err = f.d.AnswerRequest(ctx, owner, reqs[0].ID, denproto.DeviceAnswerRequest{Answer: denproto.ExchangeAnswer{X: denproto.Random(32), Nonce: denproto.Random(32), CT: denproto.Random(1088)}})
	wantCode(t, err, denproto.CodeNotFound)
	done := make(chan denproto.PendingStatus)
	go func() {
		st, _ := f.d.Pending(ctx, pending.PendingToken, 1)
		done <- st
	}()
	if err := f.d.RefuseRequest(ctx, bob, reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if st := <-done; st.Status != denproto.PendingRefused || st.Session != nil {
		t.Fatalf("the new device waiting: %+v", st)
	}
	if _, err := f.d.Authenticate(ctx, pending.PendingToken); err == nil {
		t.Fatal("the pending token is a session")
	}
	if len(f.d.memberRequests(bob.MemberID)) != 0 {
		t.Fatal("a refused request still waits")
	}

	// Only the device that answered approves, once the new one revealed.
	pending, _ = f.askSignIn("bob", "password", newDevice())
	req := f.d.memberRequests(bob.MemberID)[0]
	_, answer, _ := denproto.AnswerExchange(denproto.DeviceExchangeContext(info.ID, "bob", req.KeyID), req.Offer)
	if _, err := f.d.AnswerRequest(ctx, bob, req.ID, denproto.DeviceAnswerRequest{Answer: answer}); err != nil {
		t.Fatal(err)
	}
	_, err = f.d.AnswerRequest(ctx, bob, req.ID, denproto.DeviceAnswerRequest{Answer: answer})
	wantCode(t, err, denproto.CodeKeyExists)
	handover := denproto.ApproveRequest{Handover: denproto.Random(denproto.HandoverSize)}
	wantCode(t, f.d.ApproveRequest(ctx, bob, req.ID, handover), denproto.CodeKeyExists)
	// A reveal that doesn't open the offer's commitment is turned away.
	err = f.d.RevealPending(ctx, pending.PendingToken, denproto.ExchangeReveal{X: denproto.Random(32), Nonce: denproto.Random(32)})
	wantCode(t, err, denproto.CodeInvalidField)
	err = f.d.RevealPending(ctx, denproto.Random(denproto.TokenSize), denproto.ExchangeReveal{})
	wantCode(t, err, denproto.CodeUnauthorized)

	// A new password cancels what's waiting: whoever asked had the old one.
	if _, err := f.d.ChangePassword(ctx, bob, denproto.PasswordChangeRequest{Verifier: f.verifier("bob", "password"), NewVerifier: f.verifier("bob", "second")}); err != nil {
		t.Fatal(err)
	}
	if st, err := f.d.Pending(ctx, pending.PendingToken, 0); err != nil || st.Status != denproto.PendingCancelled {
		t.Fatalf("after a new password: %+v %v", st, err)
	}

	// Requests lapse after ten minutes, and a member has at most a few.
	for range maxMemberRequests {
		if _, err := f.askSignIn("bob", "second", newDevice()); err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.askSignIn("bob", "second", newDevice())
	wantCode(t, err, denproto.CodeRateLimited)
	f.clock = f.clock.Add(requestLifetime)
	if len(f.d.memberRequests(bob.MemberID)) != 0 {
		t.Fatal("requests outlived their time")
	}
	if _, err := f.d.Pending(ctx, pending.PendingToken, 0); !denproto.IsCode(err, denproto.CodeUnauthorized) {
		t.Fatalf("a lapsed request: %v", err)
	}
	if _, err := f.signIn(bob, "bob", "second", newDevice()); err != nil {
		t.Fatalf("after the lapse: %v", err)
	}
}

// A history page stops short when its messages would pass what a response
// holds, and says more are there.
func TestHistoryFitsAResponse(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	cid, _ := denproto.ParseID(dm.ID)
	key := f.keyFor(owner.MemberID, bob.MemberID, cid)
	var first denproto.Message
	for i := range 30 {
		m, err := f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(denproto.MaxSealed), KeyID: key.id, ReplyTo: first.ID})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = m
		}
	}
	h, err := f.d.History(ctx, bob, dm.ID, HistoryQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(h)
	if len(body) > denproto.MaxBody || !h.HasOlder || len(h.Messages) == 0 || len(h.Messages) >= 30 {
		t.Fatalf("a page of %d messages in %d bytes, older %t", len(h.Messages), len(body), h.HasOlder)
	}
	// The page keeps the newest, next to where it starts.
	if h.Messages[len(h.Messages)-1].ID <= h.Messages[0].ID {
		t.Fatal("the page is out of order")
	}
	older, err := f.d.History(ctx, bob, dm.ID, HistoryQuery{Before: mustID(h.Messages[0].ID), Limit: 50})
	if err != nil || len(older.Messages) == 0 || !older.HasNewer {
		t.Fatalf("the page before: %d %v", len(older.Messages), err)
	}
}
