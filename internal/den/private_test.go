package den

import (
	"context"
	"encoding/json"
	"slices"
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

// keyFor returns a DM's key, first making it from a, sealed to both
// members' identities, as a's client does (M16.1).
func (f *fixture) keyFor(a, b, channel int64) testKey {
	f.t.Helper()
	if k, ok := f.keys[denproto.FormatID(channel)]; ok {
		return k
	}
	f.publish(a)
	f.publish(b)
	key := denproto.Random(denproto.DMKeySize)
	made, err := f.d.MakeSignedKey(context.Background(), &Session{MemberID: a}, denproto.FormatID(channel), f.keyRequest(a, b, channel, key))
	if err != nil {
		f.t.Fatalf("make a key: %v", err)
	}
	k := testKey{id: made.ID, key: key}
	f.keys[denproto.FormatID(channel)] = k
	return k
}

// identityOf makes a member's identity from the seal their clients hold.
func (f *fixture) identityOf(member int64) *denproto.IdentityKeys {
	f.t.Helper()
	info, _ := f.d.Info()
	k, err := denproto.DeriveIdentity(f.seals[member], info.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(k.Close)
	return k
}

// publish publishes a member's identity, as their Dens does as it signs
// in.
func (f *fixture) publish(member int64) {
	f.t.Helper()
	if _, err := f.d.PublishIdentity(context.Background(), &Session{MemberID: member}, f.identityOf(member).Public()); err != nil {
		f.t.Fatalf("publish member %d's identity: %v", member, err)
	}
}

// keyRequest makes a DM's key from a to b, as a's client does: sealed to
// each one's identity, and signed by a's.
func (f *fixture) keyRequest(a, b, channel int64, key []byte) denproto.SignedKeyRequest {
	f.t.Helper()
	info, _ := f.d.Info()
	ka, kb := f.identityOf(a), f.identityOf(b)
	kid := denproto.Random(denproto.KIDSize)
	members := []denproto.KeyMember{{MemberID: denproto.FormatID(a), Identity: ka.Public().Hash()}, {MemberID: denproto.FormatID(b), Identity: kb.Public().Hash()}}
	if b < a {
		members[0], members[1] = members[1], members[0]
	}
	req := denproto.SignedKeyRequest{KID: kid, Commit: denproto.KeyCommit(kid, key), Members: members}
	for _, m := range []struct {
		id int64
		k  *denproto.IdentityKeys
	}{{a, ka}, {b, kb}} {
		copied, err := denproto.SealKeyCopy(m.k.Public(), info.ID, channel, kid, m.id, key)
		if err != nil {
			f.t.Fatal(err)
		}
		req.Copies = append(req.Copies, denproto.KeyCopy{MemberID: denproto.FormatID(m.id), Copy: copied})
	}
	s, err := req.Statement(info.ID, denproto.FormatID(channel), denproto.FormatID(a))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Signature = ka.Sign(s)
	return req
}

// legacyKey writes a DM key both members checked before M16.1, with each
// one's copy sealed with their seal, as M1.7's exchange left it.
func (f *fixture) legacyKey(a, b, channel int64) testKey {
	f.t.Helper()
	info, _ := f.d.Info()
	res, err := f.db.Exec(`INSERT INTO den_dm_keys (channel_id, started_by, started_at, stage) VALUES (?, ?, 1, 'revealed')`, channel, a)
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	key := denproto.Random(denproto.DMKeySize)
	for _, m := range []int64{a, b} {
		sealed, err := denproto.SealDMKey(f.seals[m], info.ID, channel, id, m, key)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.db.Exec(`INSERT INTO den_dm_key_members (key_id, member_id, sealed, checked_at) VALUES (?, ?, ?, 2)`, id, m, sealed); err != nil {
			f.t.Fatal(err)
		}
	}
	k := testKey{id: denproto.FormatID(id), key: key}
	f.keys[denproto.FormatID(channel)] = k
	return k
}

// signedKeyAs finds one of a DM's signed keys as a member sees it.
func (f *fixture) signedKeyAs(s *Session, dm, id string) denproto.SignedKey {
	f.t.Helper()
	list, err := f.d.SignedKeys(context.Background(), s, dm)
	if err != nil {
		f.t.Fatal(err)
	}
	info, _ := f.d.Info()
	for _, k := range list.Keys {
		if k.ID == id {
			if err := denproto.CheckSignedKey(info.ID, k); err != nil {
				f.t.Fatalf("key %s: %v", id, err)
			}
			return k
		}
	}
	f.t.Fatalf("member %d doesn't see key %s", s.MemberID, id)
	return denproto.SignedKey{}
}

// keyAs finds one of a DM's keys from before M16.1 as a member sees it.
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

// A DM's key is sealed to its members' identities and signed by its
// maker's (M16.1). The den takes one only when it names the identities
// its members publish now, checks against its maker's, and has a copy for
// each of the two; a second made at once is refused, so the slower takes
// the live one; one made after an identity changed replaces the live key.
// Each member hears of it with their own copy alone, beside the keys from
// before M16.1, never among them.
func TestSignedDMKeys(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	cid := mustID(dm.ID)
	info, _ := f.d.Info()
	key := denproto.Random(denproto.DMKeySize)

	// Neither has published an identity yet, as from an older Dens.
	_, err := f.d.MakeSignedKey(ctx, owner, dm.ID, f.keyRequest(owner.MemberID, bob.MemberID, cid, key))
	wantCode(t, err, denproto.CodeInvalidField)
	f.publish(owner.MemberID)
	_, err = f.d.MakeSignedKey(ctx, owner, dm.ID, f.keyRequest(owner.MemberID, bob.MemberID, cid, key))
	wantCode(t, err, denproto.CodeInvalidField)
	f.publish(bob.MemberID)

	good := f.keyRequest(owner.MemberID, bob.MemberID, cid, key)
	for what, change := range map[string]func(*denproto.SignedKeyRequest){
		"a bad signature":      func(r *denproto.SignedKeyRequest) { r.Signature = denproto.Random(denproto.SignatureSize) },
		"another identity":     func(r *denproto.SignedKeyRequest) { r.Members[1].Identity = denproto.Random(denproto.IdentityHashSize) },
		"another member":       func(r *denproto.SignedKeyRequest) { r.Members[1].MemberID = "999" },
		"a copy too few":       func(r *denproto.SignedKeyRequest) { r.Copies = r.Copies[:1] },
		"a copy too short":     func(r *denproto.SignedKeyRequest) { r.Copies[1].Copy = r.Copies[1].Copy[:100] },
		"a copy twice":         func(r *denproto.SignedKeyRequest) { r.Copies[1] = r.Copies[0] },
		"a short key ID":       func(r *denproto.SignedKeyRequest) { r.KID = r.KID[:8] },
		"one member":           func(r *denproto.SignedKeyRequest) { r.Members = r.Members[:1] },
		"members out of order": func(r *denproto.SignedKeyRequest) { r.Members[0], r.Members[1] = r.Members[1], r.Members[0] },
	} {
		bad := good
		bad.Members, bad.Copies = slices.Clone(good.Members), slices.Clone(good.Copies)
		change(&bad)
		if _, err := f.d.MakeSignedKey(ctx, owner, dm.ID, bad); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("a key with %s: %v", what, err)
		}
	}
	// Signed by bob's identity, but sent by alice, it's refused too.
	signedByBob := f.keyRequest(bob.MemberID, owner.MemberID, cid, key)
	_, err = f.d.MakeSignedKey(ctx, owner, dm.ID, signedByBob)
	wantCode(t, err, denproto.CodeInvalidField)

	sub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	made, err := f.d.MakeSignedKey(ctx, owner, dm.ID, good)
	if err != nil {
		t.Fatal(err)
	}
	if err := denproto.CheckSignedKey(info.ID, made); err != nil || made.MadeBy != denproto.FormatID(owner.MemberID) || made.Retired != "" {
		t.Fatalf("the key alice made: %+v %v", made, err)
	}
	// Each member gets their own copy, which opens to the key.
	heard := nextEvent[denproto.SignedKey](t, sub, denproto.EventDMSignedKey)
	got, err := f.identityOf(bob.MemberID).OpenKeyCopy(info.ID, cid, heard.KID, bob.MemberID, heard.Copy)
	if err != nil || !bytesEqualKeys(got, key) || heard.ID != made.ID {
		t.Fatalf("bob's copy: %v", err)
	}
	if mine, err := f.identityOf(owner.MemberID).OpenKeyCopy(info.ID, cid, made.KID, owner.MemberID, made.Copy); err != nil || !bytesEqualKeys(mine, key) {
		t.Fatalf("alice's copy: %v", err)
	}
	s, _ := heard.Statement(info.ID)
	if !denproto.VerifyStatement(heard.Maker.Sig, s, heard.Signature) {
		t.Fatal("the key's statement doesn't check against its maker's keys")
	}
	// Bob's Dens, making one at the same moment, finds the live one.
	_, err = f.d.MakeSignedKey(ctx, bob, dm.ID, f.keyRequest(bob.MemberID, owner.MemberID, cid, denproto.Random(denproto.DMKeySize)))
	wantCode(t, err, denproto.CodeKeyExists)
	// Signed keys come beside M1.7's, never among them, whose copies an
	// older Dens checks to be its own 72 bytes.
	snap, err := f.d.snapshot(ctx, bob.MemberID, false, 0, nil)
	if err != nil || len(snap.SignedKeys) != 1 || len(snap.DMKeys) != 0 || !denproto.Equal(snap.SignedKeys[0].Copy, heard.Copy) {
		t.Fatalf("bob's snapshot: %+v %+v %v", snap.SignedKeys, snap.DMKeys, err)
	}
	if list, _ := f.d.DMKeys(ctx, bob, dm.ID); len(list.Keys) != 0 {
		t.Fatalf("an older Dens's list of the DM's keys: %+v", list.Keys)
	}
	f.keys[dm.ID] = testKey{id: made.ID, key: key}
	sent := f.send(owner, dm.ID, "x")
	if sent.KeyID != made.ID {
		t.Fatalf("sent under %s", sent.KeyID)
	}

	// Bob's identity changes: a key naming his new one replaces the live
	// key, which retires as replaced.
	f.seals[bob.MemberID] = denproto.NewSeal()
	f.publish(bob.MemberID)
	next := denproto.Random(denproto.DMKeySize)
	replaced, err := f.d.MakeSignedKey(ctx, bob, dm.ID, f.keyRequest(bob.MemberID, owner.MemberID, cid, next))
	if err != nil {
		t.Fatal(err)
	}
	old := f.signedKeyAs(owner, dm.ID, made.ID)
	if old.Retired != denproto.RetiredReplaced || old.RetiredBy != denproto.FormatID(bob.MemberID) || old.Copy == nil {
		t.Fatalf("the replaced key: %+v", old)
	}
	_, err = f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Sealed: denproto.Random(40), KeyID: made.ID})
	wantCode(t, err, denproto.CodeInvalidField)
	f.keys[dm.ID] = testKey{id: replaced.ID, key: next}
	f.send(owner, dm.ID, "after")
	// The den starts no exchange any more.
	_, err = f.d.StartKey(ctx, bob, dm.ID, denproto.KeyStartRequest{})
	wantCode(t, err, denproto.CodeInvalidField)
}

func mustID(s string) int64 {
	id, err := denproto.ParseID(s)
	if err != nil {
		panic(err)
	}
	return id
}

func bytesEqualKeys(a, b []byte) bool { return len(a) == denproto.DMKeySize && denproto.Equal(a, b) }

// A DM's key both members checked before M16.1 stays live: both send and
// read under it, an older Dens still sees it among the DM's keys, a signed
// key can't take its place, and the den starts no exchange.
func TestLegacyDMKeys(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	cid := mustID(dm.ID)
	info, _ := f.d.Info()
	k := f.legacyKey(owner.MemberID, bob.MemberID, cid)
	first := f.send(owner, dm.ID, "from before")
	reply := f.send(bob, dm.ID, "still here")
	if first.KeyID != k.id || reply.KeyID != k.id {
		t.Fatalf("sent under %s and %s", first.KeyID, reply.KeyID)
	}
	seen := f.keyAs(bob, dm.ID, k.id)
	if len(seen.Checks) != 2 || len(seen.Sealed) != denproto.SealedKeySize || seen.Retired != "" {
		t.Fatalf("bob's view of the key: %+v", seen)
	}
	if got, err := denproto.OpenDMKey(f.seals[bob.MemberID], info.ID, cid, mustID(k.id), bob.MemberID, seen.Sealed); err != nil || !bytesEqualKeys(got, k.key) {
		t.Fatalf("bob's copy: %v", err)
	}
	h, err := f.d.History(ctx, bob, dm.ID, HistoryQuery{})
	if err != nil || len(h.Messages) != 2 || f.openDM(k, h.Messages[0]) != "from before" {
		t.Fatalf("bob's history: %+v %v", h, err)
	}
	f.publish(owner.MemberID)
	f.publish(bob.MemberID)
	_, err = f.d.MakeSignedKey(ctx, owner, dm.ID, f.keyRequest(owner.MemberID, bob.MemberID, cid, denproto.Random(denproto.DMKeySize)))
	wantCode(t, err, denproto.CodeKeyExists)
	_, err = f.d.StartKey(ctx, owner, dm.ID, denproto.KeyStartRequest{})
	wantCode(t, err, denproto.CodeInvalidField)
}

// A member's Dens publishes their identity, which the den hands any member
// who asks, a few at a time, and whose hash their record carries, so a
// change reaches everyone. Leaving takes it, and so does starting over,
// with the record it sealed.
func TestIdentities(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	id := f.identityOf(bob.MemberID).Public()
	_, err := f.d.PublishIdentity(ctx, bob, denproto.Identity{KEM: id.KEM[:100], Sig: id.Sig})
	wantCode(t, err, denproto.CodeInvalidField)
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	before := f.version()
	m, err := f.d.PublishIdentity(ctx, bob, id)
	if err != nil || m.Identity != id.Hash().String() {
		t.Fatalf("published: %+v %v", m, err)
	}
	if heard := nextEvent[denproto.Member](t, sub, denproto.EventMemberUpdated); heard.Identity != id.Hash().String() {
		t.Fatalf("alice heard %+v", heard)
	}
	if f.version() == before {
		t.Fatal("the change log has no record of the member's change")
	}
	// The same again changes nothing.
	same := f.version()
	if _, err := f.d.PublishIdentity(ctx, bob, id); err != nil || f.version() != same {
		t.Fatalf("publishing it again: %v", err)
	}
	list, err := f.d.Identities(ctx, []string{denproto.FormatID(bob.MemberID), denproto.FormatID(owner.MemberID), "12345"})
	if err != nil || len(list.Identities) != 1 || !denproto.Equal(list.Identities[0].KEM, id.KEM) || list.Identities[0].MemberID != denproto.FormatID(bob.MemberID) {
		t.Fatalf("the identities fetched: %+v %v", list, err)
	}
	_, err = f.d.Identities(ctx, nil)
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.Identities(ctx, slices.Repeat([]string{"1"}, denproto.MaxIdentities+1))
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.Identities(ctx, []string{"x"})
	wantCode(t, err, denproto.CodeInvalidField)

	// Starting over takes the identity, which came from the old seal, and
	// the record it sealed.
	f.writeRecord(bob, 0)
	info, _ := f.d.Info()
	seal := denproto.NewSeal()
	check, _ := denproto.SealCheck(seal, info.ID)
	if _, err := f.d.StartOver(ctx, bob, denproto.StartOverRequest{Verifier: f.verifier("bob", "password"), SealCheck: check}); err != nil {
		t.Fatal(err)
	}
	if heard := nextEvent[denproto.Member](t, sub, denproto.EventMemberUpdated); heard.ID != denproto.FormatID(bob.MemberID) || heard.Identity != "" {
		t.Fatalf("alice heard %+v", heard)
	}
	if rec, err := f.d.Record(ctx, bob); err != nil || rec.Revision != 0 || rec.Sealed != nil {
		t.Fatalf("bob's record after starting over: %+v %v", rec, err)
	}
	if list, _ := f.d.Identities(ctx, []string{denproto.FormatID(bob.MemberID)}); len(list.Identities) != 0 {
		t.Fatalf("bob's identity after starting over: %+v", list)
	}
	// Leaving takes it too.
	f.seals[bob.MemberID] = seal
	f.publish(bob.MemberID)
	if err := f.d.Leave(ctx, bob); err != nil {
		t.Fatal(err)
	}
	if m, _ := f.d.Member(ctx, bob.MemberID); m.Identity != "" {
		t.Fatalf("bob's record after leaving: %+v", m)
	}
}

// writeRecord writes a member's record over a revision, sealed as their
// client seals it.
func (f *fixture) writeRecord(s *Session, revision int64) denproto.Record {
	f.t.Helper()
	info, _ := f.d.Info()
	sealed, err := denproto.SealRecord(f.seals[s.MemberID], info.ID, s.MemberID, revision+1, []byte(`{"members":{}}`))
	if err != nil {
		f.t.Fatal(err)
	}
	rec, err := f.d.WriteRecord(context.Background(), s, denproto.RecordWrite{Revision: revision, Sealed: sealed})
	if err != nil {
		f.t.Fatalf("write the record: %v", err)
	}
	return rec
}

// A member's record is written over the revision it was merged from, and
// a write over another is refused; it's padded to its sizes; and the
// member's own sessions hear of each revision, nobody else's.
func TestRecords(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	if rec, err := f.d.Record(ctx, bob); err != nil || rec.Revision != 0 || rec.Sealed != nil {
		t.Fatalf("before the first write: %+v %v", rec, err)
	}
	mine, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(mine)
	theirs, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(theirs)
	if rec := f.writeRecord(bob, 0); rec.Revision != 1 {
		t.Fatalf("the first write: %+v", rec)
	}
	if u := nextEvent[denproto.RecordUpdated](t, mine, denproto.EventRecordUpdated); u.Revision != 1 {
		t.Fatalf("bob's sessions heard %+v", u)
	}
	_, err := f.d.WriteRecord(ctx, bob, denproto.RecordWrite{Revision: 0, Sealed: denproto.Random(16<<10 + 40)})
	wantCode(t, err, denproto.CodeEditConflict)
	_, err = f.d.WriteRecord(ctx, bob, denproto.RecordWrite{Revision: 1, Sealed: denproto.Random(1000)})
	wantCode(t, err, denproto.CodeInvalidField)
	if rec := f.writeRecord(bob, 1); rec.Revision != 2 {
		t.Fatalf("the second write: %+v", rec)
	}
	rec, err := f.d.Record(ctx, bob)
	if err != nil || rec.Revision != 2 || !denproto.CheckSealedRecord(rec.Sealed) {
		t.Fatalf("the record: %+v %v", rec, err)
	}
	info, _ := f.d.Info()
	if _, err := denproto.OpenRecord(f.seals[bob.MemberID], info.ID, bob.MemberID, 2, rec.Sealed); err != nil {
		t.Fatalf("the record doesn't open as bob wrote it: %v", err)
	}
	if other, _ := f.d.Record(ctx, owner); other.Revision != 0 {
		t.Fatalf("alice's record: %+v", other)
	}
	// Alice heard nothing of bob's record.
	f.send(owner, f.newChannel(owner, "general", denproto.ChannelRequest{}).ID, "marker")
	for {
		e := <-theirs.Events
		if e.T == denproto.EventRecordUpdated {
			t.Fatal("alice heard of bob's record")
		}
		if e.T == denproto.EventMessageCreated {
			break
		}
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
	var retired denproto.SignedKey
	if e := <-sub.Events; e.T != denproto.EventDMSignedKey || json.Unmarshal(e.D, &retired) != nil ||
		retired.Retired != denproto.RetiredStartedOver || retired.RetiredBy != denproto.FormatID(bob.MemberID) || retired.Copy == nil {
		t.Fatalf("the owner heard %s %s", e.T, e.D)
	}
	snap, _ := f.d.snapshot(ctx, bob.MemberID, false, 0, nil)
	if !denproto.Equal(snap.SealCheck, check) || len(snap.SignedKeys) != 2 || snap.Me.Identity != "" {
		t.Fatalf("bob's snapshot: %+v %+v", snap.SignedKeys, snap.Me)
	}
	for _, k := range snap.SignedKeys {
		if k.Copy != nil || k.Retired != denproto.RetiredStartedOver {
			t.Fatalf("bob's view of an old key: %+v", k)
		}
	}
	// Nothing more goes out under the old keys; a new key, sealed to the
	// identity the new seal makes, lets both send again.
	dm := dms[owner]
	cid, _ := denproto.ParseID(dm.ID)
	old := f.keys[dm.ID]
	nonce := denproto.Random(16)
	_, err = f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: nonce, Sealed: f.sealText(old, cid, owner.MemberID, nonce, 1, "x"), KeyID: old.id})
	wantCode(t, err, denproto.CodeInvalidField)
	delete(f.keys, dm.ID)
	fresh := f.keyFor(bob.MemberID, owner.MemberID, cid)
	f.send(owner, dm.ID, "after")
	if fresh.id == old.id {
		t.Fatal("the new key reused the key's ID")
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
	if k := f.signedKeyAs(back, dm.ID, f.keys[dm.ID].id); k.Retired != "" || k.Copy == nil {
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
	if k := f.signedKeyAs(back, dm.ID, f.keys[dm.ID].id); k.Retired != denproto.RetiredStartedOver || k.Copy != nil {
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
	snap, _ := f.d.snapshot(ctx, bob.MemberID, false, 0, nil)
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
