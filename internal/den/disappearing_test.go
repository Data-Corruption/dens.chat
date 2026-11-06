package den

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// setExpiry sets a DM's or a group's time as a member.
func (f *fixture) setExpiry(s *Session, channel string, after int) denproto.Channel {
	f.t.Helper()
	c, err := f.d.SetExpiry(context.Background(), s, channel, denproto.ExpiryRequest{ExpiresAfter: after})
	if err != nil {
		f.t.Fatalf("set %s's time to %d: %v", channel, after, err)
	}
	return c
}

// Any member of a DM or a group sets its time, which the den checks: 0 or
// one of the four, and for a development instance's den any time up to
// four weeks. A channel has none, someone outside the DM can't set it, nor
// a member at version 1 a group's, and a DM whose other member left takes
// none. Both members hear it, with who set it and when, which their
// snapshots carry too; the same again tells nobody.
func TestSetExpiry(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	carol := f.member(owner, "carol")
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	for _, bad := range []int{-1, 5, 2 * denproto.ExpiresHour, denproto.MaxExpiresAfter + 1} {
		_, err := f.d.SetExpiry(ctx, bob, dm.ID, denproto.ExpiryRequest{ExpiresAfter: bad})
		wantCode(t, err, denproto.CodeInvalidField)
	}
	_, err = f.d.SetExpiry(ctx, owner, general.ID, denproto.ExpiryRequest{ExpiresAfter: denproto.ExpiresDay})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.SetExpiry(ctx, carol, dm.ID, denproto.ExpiryRequest{ExpiresAfter: denproto.ExpiresDay})
	wantCode(t, err, denproto.CodeNotFound)

	c := f.setExpiry(bob, dm.ID, denproto.ExpiresDay)
	set := denproto.ExpirySet{By: denproto.FormatID(bob.MemberID), At: f.clock.UnixMilli()}
	if c.ExpiresAfter != denproto.ExpiresDay || c.ExpiresSet == nil || *c.ExpiresSet != set {
		t.Fatalf("the DM as set: %+v", c)
	}
	if got := nextEvent[denproto.Channel](t, sub, denproto.EventChannelUpdated); got.ID != dm.ID || got.ExpiresAfter != denproto.ExpiresDay ||
		got.ExpiresSet == nil || *got.ExpiresSet != set {
		t.Fatalf("the other member heard %+v", got)
	}
	f.clock = f.clock.Add(time.Minute)
	if c := f.setExpiry(owner, dm.ID, denproto.ExpiresDay); *c.ExpiresSet != set {
		t.Errorf("the same time again set it anew: %+v", c.ExpiresSet)
	}
	if got := drain(sub); slices.Contains(got, denproto.EventChannelUpdated) {
		t.Errorf("the same time again told the members: %v", got)
	}
	snap, err := f.d.snapshot(ctx, owner.MemberID, true, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(snap.Channels, func(c denproto.Channel) bool { return c.ID == dm.ID })
	if i < 0 || snap.Channels[i].ExpiresAfter != denproto.ExpiresDay || *snap.Channels[i].ExpiresSet != set {
		t.Fatalf("the snapshot's DM: %+v", snap.Channels)
	}
	if info, _ := f.d.Info(); !info.Disappearing {
		t.Error("the den doesn't say it deletes disappearing messages")
	}

	// Off is set like any time, and says who set it.
	if c := f.setExpiry(owner, dm.ID, 0); c.ExpiresAfter != 0 || c.ExpiresSet.By != denproto.FormatID(owner.MemberID) {
		t.Fatalf("turned off: %+v", c)
	}
	f.d.ShortTimes = true
	if c := f.setExpiry(owner, dm.ID, 5); c.ExpiresAfter != 5 {
		t.Fatalf("a development instance's time of seconds: %+v", c)
	}
	if err := f.d.Leave(ctx, bob); err != nil {
		t.Fatal(err)
	}
	_, err = f.d.SetExpiry(ctx, owner, dm.ID, denproto.ExpiryRequest{ExpiresAfter: denproto.ExpiresWeek})
	wantCode(t, err, denproto.CodeForbidden)
}

// A DM's message sent under a time goes as its time comes, as its author's
// delete would: with its files and reactions, told to both members, in the
// change log, and out of the reply that quotes it; an edit doesn't keep it
// longer, and one sent before the time was set stays. Its other member,
// who hadn't read it, reads past it where it was all that lay unread, so
// the DM isn't left unread once the last goes.
func TestDisappearingMessages(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	cid := mustID(dm.ID)
	key := f.keyFor(owner.MemberID, bob.MemberID, cid)
	kept := f.send(owner, dm.ID, "kept")
	if err := f.d.MarkRead(ctx, bob, dm.ID, denproto.ReadRequest{MessageID: kept.ID}); err != nil {
		t.Fatal(err)
	}
	f.setExpiry(bob, dm.ID, denproto.ExpiresHour)
	blob, err := f.d.Upload(ctx, owner, "", 5, bytes.NewReader([]byte("noise")), true, "")
	if err != nil {
		t.Fatal(err)
	}
	gone := f.mustSendFiles(owner, dm.ID, blob.ID)
	if kept.ExpiresAt != 0 || gone.ExpiresAt != f.clock.Add(time.Hour).UnixMilli() || denproto.CheckMessage(gone) != nil {
		t.Fatalf("sent before and after the time was set: %d, %d", kept.ExpiresAt, gone.ExpiresAt)
	}
	if _, err := f.d.SetDMReactions(ctx, bob, gone.ID, denproto.DMReactionsRequest{KeyID: key.id,
		Sealed: denproto.Random(denproto.SealedReactionsSize)}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(30 * time.Minute)
	edited, err := f.d.Edit(ctx, owner, gone.ID, denproto.EditRequest{Revision: 1, KeyID: key.id,
		Sealed: f.sealText(key, cid, owner.MemberID, gone.Nonce, 2, "edited")})
	if err != nil {
		t.Fatal(err)
	}
	h, err := f.d.History(ctx, bob, dm.ID, HistoryQuery{})
	if err != nil || len(h.Messages) != 2 || h.Messages[1].ExpiresAt != gone.ExpiresAt || edited.ExpiresAt != gone.ExpiresAt {
		t.Fatalf("history before it goes, the edit going at %d: %+v %v", edited.ExpiresAt, h.Messages, err)
	}
	nonce := denproto.Random(denproto.NonceBytes)
	reply, err := f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: nonce, KeyID: key.id, ReplyTo: gone.ID,
		Sealed: f.sealText(key, cid, owner.MemberID, nonce, 1, "and this")})
	if err != nil || reply.ExpiresAt != f.clock.Add(time.Hour).UnixMilli() {
		t.Fatalf("the reply: %+v %v", reply, err)
	}
	changes, err := f.d.Changes(ctx, bob, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	sub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	f.clock = time.UnixMilli(gone.ExpiresAt).Add(-time.Millisecond)
	if err := f.d.deleteDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := drain(sub); len(got) != 0 {
		t.Fatalf("before its time: %v", got)
	}
	f.clock = time.UnixMilli(gone.ExpiresAt)
	if err := f.d.deleteDue(ctx); err != nil {
		t.Fatal(err)
	}
	if d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted); d.ID != gone.ID || d.ChannelID != dm.ID ||
		!slices.Equal(d.Files, []string{blob.ID}) {
		t.Fatalf("message.deleted: %+v", d)
	}
	// What lay unread past kept was the reply alone once it went, and bob
	// reads up to it.
	if r := nextEvent[denproto.ReadState](t, sub, denproto.EventReadStateUpdated); r.ChannelID != dm.ID || r.ReadPosition != gone.ID ||
		r.MentionCount != 1 {
		t.Fatalf("bob's read state after it went: %+v", r)
	}
	h, err = f.d.History(ctx, bob, dm.ID, HistoryQuery{})
	if err != nil || len(h.Messages) != 2 || h.Messages[0].ID != kept.ID || h.Messages[1].ID != reply.ID || h.Messages[1].Reply != nil {
		t.Fatalf("history after it went: %+v %v", h.Messages, err)
	}
	if _, err := f.read(owner, blob.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("its file: %v", err)
	}
	if files, _ := f.stored(); len(files) != 0 {
		t.Errorf("stored after it went: %v", files)
	}
	var reactions int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_dm_reactions`).Scan(&reactions); err != nil || reactions != 0 {
		t.Errorf("%d sets of reactions stay, %v", reactions, err)
	}
	after, err := f.d.Changes(ctx, bob, changes.Version, 0)
	if err != nil || len(after.Deleted) != 1 || after.Deleted[0].ID != gone.ID {
		t.Errorf("the changes since: %+v %v", after, err)
	}

	// The reply goes in turn, and with it the last that was unread.
	f.clock = time.UnixMilli(reply.ExpiresAt)
	if err := f.d.deleteDue(ctx); err != nil {
		t.Fatal(err)
	}
	if r := f.readState(bob.MemberID, dm.ID); r.LastMessage != kept.ID || r.ReadPosition != reply.ID || r.MentionCount != 0 {
		t.Errorf("bob's read state once the last went: %+v", r)
	}
}

// The den's timer is set for a message's time as it's sent, and as the den
// starts deletes what passed while it was stopped, as a den restored from
// a backup does once it starts its new epoch.
func TestDisappearingByTimer(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	f.setExpiry(owner, dm.ID, denproto.ExpiresHour)
	f.d.StartTimer()
	m := f.send(owner, dm.ID, "soon gone")
	t1 := &f.d.timer
	t1.mu.Lock()
	set, at := t1.timer != nil, t1.at
	t1.mu.Unlock()
	if !set || at != m.ExpiresAt {
		t.Fatalf("the timer, set %v, goes at %d, not %d", set, at, m.ExpiresAt)
	}
	f.d.StopTimer()

	// Restored from a backup two hours on: the den starts a new epoch and
	// moves its IDs on, as the app has it do, then starts its timer.
	f.clock = f.clock.Add(2 * time.Hour)
	f.open()
	if err := f.d.NewSyncEpoch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.d.AdvanceIDs(ctx); err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	f.d.StartTimer()
	defer f.d.StopTimer()
	if d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted); d.ID != m.ID {
		t.Fatalf("the timer deleted %+v", d)
	}
}

// A group's messages disappear as a DM's do, told to its members alone,
// and none of it to a socket speaking version 1. A member who still holds
// an older message unread, sent under a longer time, keeps their read
// position short of it.
func TestDisappearingInGroups(t *testing.T) {
	f, owner, m := groupFixture(t, "carol")
	bob, carol := m[0], m[1]
	ctx := context.Background()
	g, err := f.makeGroup(owner, bob, carol)
	if err != nil {
		t.Fatal(err)
	}
	old := *bob
	old.Version = 1
	_, err = f.d.SetExpiry(ctx, &old, g.id, denproto.ExpiryRequest{ExpiresAfter: denproto.ExpiresHour})
	wantCode(t, err, denproto.CodeNotFound)
	oldSub, _, _, _ := f.d.Hub.SubscribeAt(bob.MemberID, false, 1, "", 0)
	defer f.d.Hub.Unsubscribe(oldSub)
	sub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	f.setExpiry(owner, g.id, denproto.MaxExpiresAfter)
	older, err := g.send(owner, "older")
	if err != nil {
		t.Fatal(err)
	}
	f.setExpiry(bob, g.id, denproto.ExpiresHour)
	soon, err := g.send(owner, "soon")
	if err != nil || soon.ExpiresAt != f.clock.Add(time.Hour).UnixMilli() || older.ExpiresAt != f.clock.Add(4*7*24*time.Hour).UnixMilli() {
		t.Fatalf("sent under each time: %d, %d, %v", older.ExpiresAt, soon.ExpiresAt, err)
	}
	f.clock = f.clock.Add(time.Hour)
	if err := f.d.deleteDue(ctx); err != nil {
		t.Fatal(err)
	}
	if d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted); d.ID != soon.ID {
		t.Fatalf("carol heard %+v", d)
	}
	if r := nextEvent[denproto.ReadState](t, sub, denproto.EventReadStateUpdated); r.ChannelID != g.id || r.ReadPosition != "" || r.MentionCount != 1 {
		t.Fatalf("carol's read state, the older still unread: %+v", r)
	}
	h, err := f.d.History(ctx, carol, g.id, HistoryQuery{})
	if err != nil || len(h.Messages) != 1 || h.Messages[0].ID != older.ID {
		t.Fatalf("carol's history: %+v %v", h.Messages, err)
	}
	if got := drain(oldSub); len(got) != 0 {
		t.Errorf("a socket at version 1 heard %v", got)
	}
}
