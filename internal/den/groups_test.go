package den

import (
	"context"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// groupFixture is a den with members whose Dens speak version 2 of the
// protocol and published their identities: its owner, alice, and bob,
// then those named.
func groupFixture(t *testing.T, names ...string) (*fixture, *Session, []*Session) {
	t.Helper()
	f, owner, bob := chatFixture(t)
	out := []*Session{bob}
	for _, s := range []*Session{owner, bob} {
		s.Version = denproto.GroupsVersion
		f.publish(s.MemberID)
	}
	for _, n := range names {
		s := f.member(owner, n)
		s.Version = denproto.GroupsVersion
		f.publish(s.MemberID)
		out = append(out, s)
	}
	return f, owner, out
}

// testGroup is a group DM as its members' Dens know it: its changes'
// state, and its live key.
type testGroup struct {
	f     *fixture
	id    string
	cid   int64
	state denproto.GroupState
	key   testKey
}

// groupKeyRequest makes a group's key from maker for members, as the
// maker's Dens does: sealed to each one's identity, and signed by theirs.
func (f *fixture) groupKeyRequest(maker int64, channel int64, members []int64, key []byte) denproto.SignedKeyRequest {
	f.t.Helper()
	info, _ := f.d.Info()
	kid := denproto.Random(denproto.KIDSize)
	sorted := slices.Sorted(slices.Values(members))
	req := denproto.SignedKeyRequest{KID: kid, Commit: denproto.KeyCommit(kid, key)}
	for _, m := range sorted {
		k := f.identityOf(m)
		req.Members = append(req.Members, denproto.KeyMember{MemberID: denproto.FormatID(m), Identity: k.Public().Hash()})
		copied, err := denproto.SealKeyCopy(k.Public(), info.ID, channel, kid, m, key)
		if err != nil {
			f.t.Fatal(err)
		}
		req.Copies = append(req.Copies, denproto.KeyCopy{MemberID: denproto.FormatID(m), Copy: copied})
	}
	// One someone outside the members makes doesn't state, and goes
	// unsigned, which is refused all the same.
	if s, err := req.Statement(info.ID, denproto.FormatID(channel), denproto.FormatID(maker)); err == nil {
		req.Signature = f.identityOf(maker).Sign(s)
	}
	return req
}

// sign signs a change as its maker's Dens does, or as signer's identity,
// as a den making one up would.
func (f *fixture) signChange(channel int64, c denproto.GroupChange, signer int64) denproto.GroupChange {
	f.t.Helper()
	info, _ := f.d.Info()
	k := f.identityOf(signer)
	maker := k.Maker()
	c.Signer = &maker
	sig, err := k.SignChange(info.ID, channel, c)
	if err != nil {
		f.t.Fatal(err)
	}
	c.Signature = sig
	return c
}

func ids(ss ...*Session) []int64 {
	out := make([]int64, len(ss))
	for i, s := range ss {
		out[i] = s.MemberID
	}
	return out
}

// makeGroup makes a group DM of maker and others.
func (f *fixture) makeGroup(maker *Session, others ...*Session) (*testGroup, error) {
	f.t.Helper()
	ctx := context.Background()
	reserved, err := f.d.ReserveGroup(ctx, maker)
	if err != nil {
		return nil, err
	}
	cid, _ := denproto.ParseID(reserved.ID)
	after := append([]int64{maker.MemberID}, ids(others...)...)
	ch := denproto.GroupChange{N: 1, Prev: denproto.FirstPrev(), Kind: denproto.ChangeMade, By: denproto.FormatID(maker.MemberID),
		Subjects: formatIDs(ids(others...)), After: formatIDs(after)}
	key := denproto.Random(denproto.DMKeySize)
	req := f.groupKeyRequest(maker.MemberID, cid, after, key)
	made, err := f.d.ChangeGroup(ctx, maker, reserved.ID, denproto.GroupChangeRequest{Change: f.signChange(cid, ch, maker.MemberID), Key: &req})
	if err != nil {
		return nil, err
	}
	info, _ := f.d.Info()
	state, err := denproto.CheckChange(info.ID, cid, nil, made)
	if err != nil {
		f.t.Fatal(err)
	}
	g := &testGroup{f: f, id: reserved.ID, cid: cid, state: state}
	g.key = testKey{id: g.liveKey(maker), key: key}
	return g, nil
}

// liveKey is the ID of the group's live key as a member sees it.
func (g *testGroup) liveKey(s *Session) string {
	g.f.t.Helper()
	list, err := g.f.d.SignedKeys(context.Background(), s, g.id)
	if err != nil {
		g.f.t.Fatal(err)
	}
	for _, k := range list.Keys {
		if k.Retired == "" {
			return k.ID
		}
	}
	return ""
}

// change makes a change to the group as s's Dens does, signed by signer,
// s unless given, with a new key where it makes a membership.
func (g *testGroup) change(s *Session, kind string, subjects []int64, signer int64) (denproto.GroupChange, error) {
	g.f.t.Helper()
	members := slices.Clone(g.state.Members)
	switch kind {
	case denproto.ChangeAdded:
		members = append(members, formatIDs(subjects)...)
	case denproto.ChangeRemoved:
		members = slices.DeleteFunc(members, func(m string) bool { return m == denproto.FormatID(subjects[0]) })
	case denproto.ChangeLeft:
		members = slices.DeleteFunc(members, func(m string) bool { return m == denproto.FormatID(s.MemberID) })
	}
	ch := denproto.GroupChange{N: g.state.N + 1, Prev: g.state.Hash, Kind: kind, By: denproto.FormatID(s.MemberID),
		Subjects: formatIDs(subjects), After: members}
	if ch.After == nil {
		ch.After = []string{}
	}
	var req denproto.GroupChangeRequest
	key := denproto.Random(denproto.DMKeySize)
	if kind == denproto.ChangeAdded || kind == denproto.ChangeRemoved {
		after := make([]int64, len(members))
		for i, m := range members {
			after[i], _ = denproto.ParseID(m)
		}
		k := g.f.groupKeyRequest(s.MemberID, g.cid, after, key)
		req.Key = &k
	}
	if kind == denproto.ChangeNamed {
		info, _ := g.f.d.Info()
		live := g.f.signedKeyAs(s, g.id, g.key.id)
		name, err := denproto.SealGroupName(g.key.key, info.ID, g.cid, live.KID, "Weekend")
		if err != nil {
			g.f.t.Fatal(err)
		}
		ch.Name = &name
	}
	if signer == 0 {
		signer = s.MemberID
	}
	req.Change = g.f.signChange(g.cid, ch, signer)
	made, err := g.f.d.ChangeGroup(context.Background(), s, g.id, req)
	if err != nil {
		return made, err
	}
	info, _ := g.f.d.Info()
	if g.state, err = denproto.CheckChange(info.ID, g.cid, &g.state, made); err != nil {
		g.f.t.Fatal(err)
	}
	if req.Key != nil {
		g.key = testKey{id: g.liveKey(s), key: key}
	}
	return made, nil
}

// send sends a group message as its author's Dens does: sealed under the
// live key and signed inside by their identity.
func (g *testGroup) send(s *Session, text string) (denproto.Message, error) {
	g.f.t.Helper()
	info, _ := g.f.d.Info()
	nonce := denproto.Random(denproto.NonceBytes)
	kid, _ := denproto.ParseID(g.key.id)
	sealed, err := denproto.SealGroupMessage(g.key.key, denproto.MessageAD{DenID: info.ID, Channel: g.cid, Author: s.MemberID,
		Nonce: nonce, KeyID: kid, Revision: 1}, g.f.identityOf(s.MemberID), s.MemberID, denproto.DMPayload{Text: text})
	if err != nil {
		g.f.t.Fatal(err)
	}
	return g.f.d.Send(context.Background(), s, g.id, denproto.SendRequest{Nonce: nonce, Sealed: sealed, KeyID: g.key.id})
}

// drain takes a subscription's events waiting now, by type.
func drain(sub *Sub) []string {
	var out []string
	for {
		select {
		case e, ok := <-sub.Events:
			if !ok {
				return out
			}
			out = append(out, e.T)
		default:
			return out
		}
	}
}

// A group DM is made by the member who reserved it, with its first key,
// and each change after is checked as every member's Dens checks it: an
// add the den makes up, a removal by a member who can't and a change by
// someone outside the group are refused, and removing passes on once the
// maker leaves. A member added reads from their joining on, holding no key
// from before; one removed loses the group; and the group goes with its
// last member.
func TestGroupDMs(t *testing.T) {
	f, owner, m := groupFixture(t, "carol", "dave", "eve")
	bob, carol, dave, eve := m[0], m[1], m[2], m[3]
	ctx := context.Background()
	carolSub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	daveSub, _, _, _ := f.d.Hub.Subscribe(dave.MemberID, false, "", 0)
	oldBob, _, _, _ := f.d.Hub.SubscribeAt(bob.MemberID, false, false, "", 0)

	if _, err := f.makeGroup(bob, carol); err == nil {
		t.Fatal("a group of two was made")
	}
	g, err := f.makeGroup(bob, carol, owner)
	if err != nil {
		t.Fatalf("make a group: %v", err)
	}
	c, err := f.d.channel(ctx, f.db, g.cid)
	if err != nil || c.Kind != denproto.KindGroupDM || !slices.Equal(c.Members, formatIDs(ids(bob, carol, owner))) || c.Changes != 1 {
		t.Fatalf("the group is %+v, %v", c, err)
	}
	if got := drain(carolSub); !slices.Equal(got, []string{denproto.EventChannelCreated, denproto.EventGroupChange, denproto.EventDMSignedKey}) {
		t.Errorf("carol heard %v as the group was made", got)
	}
	if got := drain(daveSub); len(got) != 0 {
		t.Errorf("dave, outside the group, heard %v", got)
	}

	before, err := g.send(bob, "before dave")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	// Every message counts for each other member, as in a DM.
	if r := f.readState(carol.MemberID, g.id); r.MentionCount != 1 {
		t.Errorf("carol's count is %d", r.MentionCount)
	}
	if _, err := f.d.History(ctx, dave, g.id, HistoryQuery{}); err == nil {
		t.Error("dave read the group before he was in it")
	}

	// An add the den makes up, signed by another's key for a member; a
	// removal by a member who can't; a change by someone outside.
	if _, err := g.change(carol, denproto.ChangeAdded, ids(eve), eve.MemberID); err == nil {
		t.Error("an add signed by another's identity was taken")
	}
	if _, err := g.change(carol, denproto.ChangeRemoved, ids(owner), 0); err == nil {
		t.Error("a member who isn't the remover removed one")
	}
	if _, err := g.change(eve, denproto.ChangeAdded, ids(dave), 0); err == nil {
		t.Error("someone outside the group added a member")
	}

	if _, err := g.change(carol, denproto.ChangeAdded, ids(dave), 0); err != nil {
		t.Fatalf("carol adds dave: %v", err)
	}
	if got := drain(daveSub); !slices.Equal(got, []string{denproto.EventChannelCreated, denproto.EventGroupChange, denproto.EventDMSignedKey}) {
		t.Errorf("dave heard %v as he was added", got)
	}
	h, err := f.d.History(ctx, dave, g.id, HistoryQuery{})
	if err != nil || len(h.Messages) != 0 {
		t.Errorf("dave reads %d messages from before he joined, %v", len(h.Messages), err)
	}
	keys, err := f.d.SignedKeys(ctx, dave, g.id)
	if err != nil || len(keys.Keys) != 1 || keys.Keys[0].ID != g.key.id {
		t.Errorf("dave holds keys %+v, %v: only the one made as he joined", keys.Keys, err)
	}
	changes, err := f.d.GroupChanges(ctx, dave, g.id, 0)
	if err != nil || len(changes.Changes) != 1 || changes.Changes[0].Kind != denproto.ChangeAdded {
		t.Errorf("dave gets changes %+v, %v: from the add that took him in", changes.Changes, err)
	}
	if _, err := f.d.SetDMReactions(ctx, dave, before.ID, denproto.DMReactionsRequest{KeyID: g.key.id, Sealed: make([]byte, denproto.SealedReactionsSize)}); err == nil {
		t.Error("dave reacted to a message from before he joined")
	}
	after, err := g.send(dave, "hi all")
	if err != nil {
		t.Fatalf("dave sends: %v", err)
	}
	if h, _ := f.d.History(ctx, carol, g.id, HistoryQuery{}); len(h.Messages) != 2 || h.Messages[1].ID != after.ID {
		t.Errorf("carol reads %+v", h.Messages)
	}

	// A change made against an earlier one is a conflict, and the client
	// makes it again.
	stale := g.state
	stale.N, stale.Hash = 1, nil
	{
		ch := denproto.GroupChange{N: 2, Prev: denproto.FirstPrev(), Kind: denproto.ChangeNamed, By: denproto.FormatID(carol.MemberID),
			After: g.state.Members}
		_, err := f.d.ChangeGroup(ctx, carol, g.id, denproto.GroupChangeRequest{Change: f.signChange(g.cid, ch, carol.MemberID)})
		wantCode(t, err, denproto.CodeEditConflict)
	}
	if _, err := g.change(owner, denproto.ChangeNamed, nil, 0); err != nil {
		t.Errorf("a member names the group: %v", err)
	}
	if c, _ := f.d.channel(ctx, f.db, g.cid); c.SealedName == nil || c.SealedName.Check() != nil {
		t.Errorf("the group's name is %+v", c.SealedName)
	}

	// The maker removes; once they've left the den, the member who's been
	// in the group longest does. One removed hears only that the group went,
	// none of its keys, which mean nothing to them now.
	ownerSub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	if _, err := g.change(bob, denproto.ChangeRemoved, ids(owner), 0); err != nil {
		t.Fatalf("bob removes alice: %v", err)
	}
	if got := drain(ownerSub); !slices.Equal(got, []string{denproto.EventChannelDeleted}) {
		t.Errorf("alice, removed, heard %v", got)
	}
	if _, err := f.d.History(ctx, owner, g.id, HistoryQuery{}); err == nil {
		t.Error("alice reads the group she was removed from")
	}
	if err := f.d.Leave(ctx, bob); err != nil {
		t.Fatal(err)
	}
	c, _ = f.d.channel(ctx, f.db, g.cid)
	if !slices.Equal(c.Members, formatIDs(ids(carol, dave))) {
		t.Fatalf("after bob left the den, the group holds %v", c.Members)
	}
	departed, err := f.d.GroupChanges(ctx, carol, g.id, g.state.N)
	if err != nil || len(departed.Changes) != 1 || departed.Changes[0].Kind != denproto.ChangeDeparted || departed.Changes[0].Signature != nil {
		t.Fatalf("the den's change as bob left: %+v, %v", departed.Changes, err)
	}
	info, _ := f.d.Info()
	if g.state, err = denproto.CheckChange(info.ID, g.cid, &g.state, departed.Changes[0]); err != nil {
		t.Fatalf("the den's change doesn't check: %v", err)
	}
	if g.liveKey(carol) != "" {
		t.Error("the live key outlived bob's leaving")
	}
	if _, err := g.change(dave, denproto.ChangeRemoved, ids(carol), 0); err == nil {
		t.Error("dave removed carol, who's been in the group longer")
	}
	if _, err := g.change(carol, denproto.ChangeLeft, nil, 0); err != nil {
		t.Fatalf("carol leaves: %v", err)
	}
	if _, err := g.change(dave, denproto.ChangeLeft, nil, 0); err != nil {
		t.Fatalf("dave leaves: %v", err)
	}
	if _, err := f.d.channel(ctx, f.db, g.cid); err == nil {
		t.Error("the group outlived its last member")
	}
	var messages int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_messages WHERE channel_id = ?`, g.cid).Scan(&messages); err != nil || messages != 0 {
		t.Errorf("the group's %d messages outlived it, %v", messages, err)
	}

	// A Dens speaking version 1 heard nothing of any of it.
	if got := drain(oldBob); slices.ContainsFunc(got, func(e string) bool {
		return e != denproto.EventMemberLeft && e != denproto.EventMemberUpdated
	}) {
		t.Errorf("bob's older Dens heard %v", got)
	}
}

// A Dens speaking version 1 of the protocol is sent nothing of groups: no
// channel, key, read position or event, and a request at version 1 finds
// none, so an older device of a member in groups goes on without them.
func TestGroupsUnseenAtVersion1(t *testing.T) {
	f, owner, m := groupFixture(t, "carol")
	bob, carol := m[0], m[1]
	ctx := context.Background()
	old := *bob
	old.Version = 1
	oldSub, _, _, _ := f.d.Hub.SubscribeAt(bob.MemberID, false, false, "", 0)
	g, err := f.makeGroup(owner, bob, carol)
	if err != nil {
		t.Fatal(err)
	}
	hello, err := g.send(carol, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.MarkRead(ctx, bob, g.id, denproto.ReadRequest{MessageID: hello.ID}); err != nil {
		t.Fatal(err)
	}
	if got := drain(oldSub); len(got) != 0 {
		t.Errorf("an older Dens heard %v", got)
	}
	snap, err := f.d.snapshotAt(ctx, bob.MemberID, false, false, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(snap.Channels, func(c denproto.Channel) bool { return c.Kind == denproto.KindGroupDM }) ||
		slices.ContainsFunc(snap.ReadStates, func(r denproto.ReadState) bool { return r.ChannelID == g.id }) ||
		slices.ContainsFunc(snap.SignedKeys, func(k denproto.SignedKey) bool { return k.ChannelID == g.id }) {
		t.Errorf("an older Dens's snapshot holds the group: %+v", snap)
	}
	current, err := f.d.snapshot(ctx, bob.MemberID, false, 0, nil)
	if err != nil || !slices.ContainsFunc(current.Channels, func(c denproto.Channel) bool { return c.ID == g.id }) ||
		!slices.ContainsFunc(current.SignedKeys, func(k denproto.SignedKey) bool { return k.ChannelID == g.id }) {
		t.Errorf("a Dens speaking version 2 lacks the group: %+v, %v", current, err)
	}
	if _, err := f.d.History(ctx, &old, g.id, HistoryQuery{}); err == nil {
		t.Error("a request at version 1 read the group")
	}
	if _, err := f.d.GroupChanges(ctx, &old, g.id, 0); err == nil {
		t.Error("a request at version 1 read the group's changes")
	}
	if _, err := f.d.ReserveGroup(ctx, &old); err == nil {
		t.Error("a request at version 1 reserved a group")
	}
	ch, err := f.d.Changes(ctx, &old, 0, 0)
	if err != nil || slices.ContainsFunc(ch.Messages, func(m denproto.Message) bool { return m.ChannelID == g.id }) {
		t.Errorf("an older Dens's changes hold the group's: %+v, %v", ch, err)
	}
}

// A group reserved for its maker is theirs alone to make, and they get it
// again until they do.
func TestGroupReservations(t *testing.T) {
	f, owner, m := groupFixture(t, "carol")
	bob, carol := m[0], m[1]
	ctx := context.Background()
	a, err := f.d.ReserveGroup(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.d.ReserveGroup(ctx, bob)
	if err != nil || again.ID != a.ID {
		t.Errorf("bob reserved %s, then %s, %v", a.ID, again.ID, err)
	}
	cid, _ := denproto.ParseID(a.ID)
	after := ids(carol, bob, owner)
	ch := denproto.GroupChange{N: 1, Prev: denproto.FirstPrev(), Kind: denproto.ChangeMade, By: denproto.FormatID(carol.MemberID),
		Subjects: formatIDs(after[1:]), After: formatIDs(after)}
	req := f.groupKeyRequest(carol.MemberID, cid, after, denproto.Random(denproto.DMKeySize))
	_, err = f.d.ChangeGroup(ctx, carol, a.ID, denproto.GroupChangeRequest{Change: f.signChange(cid, ch, carol.MemberID), Key: &req})
	wantCode(t, err, denproto.CodeNotFound)
	if _, err := f.d.History(ctx, bob, a.ID, HistoryQuery{}); err == nil {
		t.Error("a group nobody made yet is seen")
	}
}
