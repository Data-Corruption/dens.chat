package den

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Deleting a message carries the channel's newest remaining message ID, or
// "" once none is left, across single deletes, ban bulk deletes and
// disappearing messages; older clients that don't read it still get
// MarkRead past a deleted ID.
func TestMessageDeletedLastMessage(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	empty := f.newChannel(owner, "empty", denproto.ChannelRequest{})

	sub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	// Single delete: deleting the newest reverts to the previous message;
	// deleting a non-newest keeps the newer ID; deleting the last leaves "".
	m1 := f.send(owner, general.ID, "first")
	m2 := f.send(owner, general.ID, "second")
	m3 := f.send(owner, general.ID, "third")

	if err := f.d.Delete(ctx, owner, m2.ID); err != nil {
		t.Fatal(err)
	}
	if d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted); d.ID != m2.ID || d.LastMessage == nil || *d.LastMessage != m3.ID {
		t.Fatalf("deleting non-newest: %+v", d)
	}

	if err := f.d.Delete(ctx, owner, m3.ID); err != nil {
		t.Fatal(err)
	}
	if d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted); d.ID != m3.ID || d.LastMessage == nil || *d.LastMessage != m1.ID {
		t.Fatalf("deleting newest leaving older: %+v", d)
	}

	only := f.send(owner, empty.ID, "only")
	if err := f.d.Delete(ctx, owner, only.ID); err != nil {
		t.Fatal(err)
	}
	if d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted); d.ID != only.ID || d.LastMessage == nil || *d.LastMessage != "" {
		t.Fatalf("deleting only message: %+v", d)
	}

	// MarkRead accepts a deleted message ID, which the page's fallback for
	// older dens relies on to clear stale unread marks.
	if err := f.d.MarkRead(ctx, bob, empty.ID, denproto.ReadRequest{MessageID: only.ID}); err != nil {
		t.Fatalf("MarkRead with deleted ID: %v", err)
	}
	if r := f.readState(bob.MemberID, empty.ID); r.ReadPosition != only.ID || r.LastMessage != "" {
		t.Fatalf("read state after MarkRead with deleted ID: %+v", r)
	}

	// Ban bulk delete populates LastMessage for every channel it touches,
	// both one left with older messages and one left empty.
	carol := f.member(owner, "carol")
	spam1 := f.send(carol, general.ID, "spam in general 1")
	spam2 := f.send(carol, general.ID, "spam in general 2")
	spamEmpty := f.send(carol, empty.ID, "spam in empty")
	if err := f.d.Remove(ctx, owner, denproto.FormatID(carol.MemberID), denproto.RemoveRequest{Ban: true, DeleteMessages: 3600}); err != nil {
		t.Fatal(err)
	}
	got := map[string]*string{}
	for range 3 {
		d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted)
		got[d.ID] = d.LastMessage
	}
	for _, want := range []struct {
		id, last string
	}{{spam1.ID, m1.ID}, {spam2.ID, m1.ID}, {spamEmpty.ID, ""}} {
		if got[want.id] == nil || *got[want.id] != want.last {
			t.Fatalf("ban delete %s: got %v, want %q", want.id, got[want.id], want.last)
		}
	}

	// Disappearing messages populate LastMessage both when an older message
	// remains and when the DM empties.
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	kept := f.send(owner, dm.ID, "kept")
	f.setExpiry(owner, dm.ID, denproto.ExpiresHour)
	ephemeral := f.send(owner, dm.ID, "ephemeral")
	f.clock = time.UnixMilli(ephemeral.ExpiresAt)
	if err := f.d.deleteDue(ctx); err != nil {
		t.Fatal(err)
	}
	if d := nextEvent[denproto.MessageDeleted](t, sub, denproto.EventMessageDeleted); d.ID != ephemeral.ID || d.LastMessage == nil || *d.LastMessage != kept.ID {
		t.Fatalf("disappearing leaving older: %+v", d)
	}

	dm2, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(f.member(owner, "dave").MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	subOwner, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(subOwner)
	f.setExpiry(owner, dm2.ID, denproto.ExpiresHour)
	sole := f.send(owner, dm2.ID, "sole")
	f.clock = time.UnixMilli(sole.ExpiresAt)
	if err := f.d.deleteDue(ctx); err != nil {
		t.Fatal(err)
	}
	if d := nextEvent[denproto.MessageDeleted](t, subOwner, denproto.EventMessageDeleted); d.ID != sole.ID || d.LastMessage == nil || *d.LastMessage != "" {
		t.Fatalf("disappearing emptying DM: %+v", d)
	}

	// Wire JSON distinguishes omitted (older den) from "" (empty channel)
	// and a remaining ID.
	var omitted denproto.MessageDeleted
	omitted.ID, omitted.ChannelID = "1", "2"
	b, err := json.Marshal(omitted)
	if err != nil || strings.Contains(string(b), "last_message_id") {
		t.Fatalf("nil LastMessage marshaled as %s (%v)", b, err)
	}
	emptyStr := ""
	omitted.LastMessage = &emptyStr
	b, err = json.Marshal(omitted)
	if err != nil || !strings.Contains(string(b), `"last_message_id":""`) {
		t.Fatalf("empty LastMessage marshaled as %s (%v)", b, err)
	}
	var decoded denproto.MessageDeleted
	if err := json.Unmarshal(b, &decoded); err != nil || decoded.LastMessage == nil || *decoded.LastMessage != "" {
		t.Fatalf("unmarshaled empty LastMessage: %+v (%v)", decoded, err)
	}
}
