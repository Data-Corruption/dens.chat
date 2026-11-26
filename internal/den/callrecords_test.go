package den

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// callIn returns the call in a channel as a member's ready lists it, or
// none.
func (f *fixture) callIn(channel string) (denproto.Call, bool) {
	for _, c := range f.d.visibleCalls(false, 0) {
		if c.ChannelID == channel {
			return c, true
		}
	}
	return denproto.Call{}, false
}

// storedRecord reads a channel's record as the database holds it.
func (f *fixture) storedRecord(channel string) int64 {
	f.t.Helper()
	cid, _ := denproto.ParseID(channel)
	ms, err := storedRecord(context.Background(), f.db, cid)
	if err != nil {
		f.t.Fatal(err)
	}
	return ms
}

// heldCall returns a member's call, held or not.
func (f *fixture) heldCall(member int64) *call {
	f.d.calls.mu.Lock()
	defer f.d.calls.mu.Unlock()
	return f.d.calls.byMember[member]
}

// A call's length counts from its first member's join, through a held
// call, and goes with the room; its record is the channel's as the call
// began. The record is written a minute past the last one kept, at the
// call's end, and as the den stops, and the den reads it once it opens
// again.
func TestCallLengthAndRecord(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	f.startCalls()
	lounge := f.newChannel(owner, "Lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	length := func(want, record int64) {
		t.Helper()
		c, ok := f.callIn(lounge.ID)
		if !ok || c.ElapsedMS == nil || *c.ElapsedMS != want || c.RecordMS != record {
			t.Fatalf("the call is %+v, %t; want %d ms long against a record of %d", c, ok, want, record)
		}
	}
	record := func(want int64) {
		t.Helper()
		if got := f.storedRecord(lounge.ID); got != want {
			t.Fatalf("the record is %d, want %d", got, want)
		}
	}

	start := f.clock
	alice := f.inCall(owner, lounge.ID)
	length(0, 0)
	f.clock = start.Add(5 * time.Second)
	bob := f.inCall(member, lounge.ID)
	length(5000, 0)
	// Alice's call is held while her socket is away, and still counts.
	f.d.socketClosed(alice)
	f.clock = start.Add(50 * time.Second)
	length(50000, 0)
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	f.d.muteCall(bob, denproto.VoiceMute{Muted: true})
	if s := nextEvent[denproto.VoiceState](t, sub, denproto.EventVoiceState); len(s.Calls) != 1 || s.Calls[0].ElapsedMS == nil ||
		*s.Calls[0].ElapsedMS != 50000 {
		t.Fatalf("voice.state carries %+v", s)
	}

	// With no record yet, each whole minute of the call writes its length.
	f.d.recordDue()
	record(0)
	f.clock = start.Add(61 * time.Second)
	f.d.recordDue()
	record(61000)
	f.clock = start.Add(90 * time.Second)
	f.d.recordDue()
	record(61000)
	f.clock = start.Add(121 * time.Second)
	f.d.recordDue()
	record(121000)
	length(121000, 0)

	// The call ends as its last member leaves, held or not, longer than the
	// record, which takes its length.
	f.clock = start.Add(130 * time.Second)
	f.d.endHeld(f.heldCall(owner.MemberID))
	length(130000, 0)
	f.d.leaveCall(bob)
	if c, ok := f.callIn(lounge.ID); ok {
		t.Fatalf("the call stayed: %+v", c)
	}
	record(130000)

	// The next call shows the record as it began, while its own length
	// passes it and is written a minute past the record kept.
	next := start.Add(time.Hour)
	f.clock = next
	f.inCall(member, lounge.ID)
	length(0, 130000)
	f.clock = next.Add(2*time.Minute + 59*time.Second)
	f.d.recordDue()
	record(130000)
	f.clock = next.Add(3 * time.Minute)
	f.d.recordDue()
	record(180000)
	length(180000, 130000)
	if at := f.d.nextRecord(); at != next.Add(4*time.Minute).UnixMilli() {
		t.Fatalf("the next write is due at %d, want 4 minutes in", at-next.UnixMilli())
	}

	// The den stops with the call going on, which writes its length, and
	// opens again with the record.
	f.clock = next.Add(3*time.Minute + 20*time.Second)
	f.open()
	f.startCalls()
	record(200000)
	if got, err := f.d.CallRecord(ctx, owner, lounge.ID); err != nil || got.RecordMS != 200000 {
		t.Fatalf("the reopened den reads the record as %+v, %v", got, err)
	}
	f.clock = next.Add(time.Hour)
	f.inCall(member, lounge.ID)
	length(0, 200000)
	// A shorter call leaves the record as it was.
	f.clock = next.Add(time.Hour + time.Minute)
	f.d.memberGone(member.MemberID)
	record(200000)
}

// Staff read and reset a channel's record, and a member does neither. A
// call going on as the record is reset shows with nothing to beat and
// writes none, and the next call sets it again.
func TestResetCallRecord(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	f.startCalls()
	lounge := f.newChannel(owner, "Lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	general := f.newChannel(owner, "general", denproto.ChannelRequest{Kind: denproto.KindText})
	code := func(err error) int {
		var e *denproto.Error
		if errors.As(err, &e) {
			return e.Status
		}
		return 0
	}

	sock := f.inCall(owner, lounge.ID)
	f.clock = f.clock.Add(40 * time.Second)
	f.d.leaveCall(sock)
	if got, err := f.d.CallRecord(ctx, owner, lounge.ID); err != nil || got.RecordMS != 40000 {
		t.Fatalf("staff read the record as %+v, %v", got, err)
	}
	if _, err := f.d.CallRecord(ctx, member, lounge.ID); code(err) != http.StatusForbidden {
		t.Fatalf("a member read the record: %v", err)
	}
	if err := f.d.ResetCallRecord(ctx, member, lounge.ID); code(err) != http.StatusForbidden {
		t.Fatalf("a member reset the record: %v", err)
	}
	for _, id := range []string{general.ID, "999", "x"} {
		if _, err := f.d.CallRecord(ctx, owner, id); code(err) != http.StatusNotFound {
			t.Fatalf("channel %s has a record: %v", id, err)
		}
		if err := f.d.ResetCallRecord(ctx, owner, id); code(err) != http.StatusNotFound {
			t.Fatalf("channel %s's record was reset: %v", id, err)
		}
	}
	if got := f.storedRecord(lounge.ID); got != 40000 {
		t.Fatalf("the record is %d after the refusals", got)
	}

	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	sock = f.inCall(owner, lounge.ID)
	if c, _ := f.callIn(lounge.ID); c.RecordMS != 40000 {
		t.Fatalf("the call began against %d", c.RecordMS)
	}
	f.clock = f.clock.Add(50 * time.Second)
	if err := f.d.ResetCallRecord(ctx, owner, lounge.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.storedRecord(lounge.ID); got != 0 {
		t.Fatalf("the record is %d after the reset", got)
	}
	var s denproto.VoiceState
	for len(s.Calls) == 0 || len(s.Calls[0].Members) == 0 || s.Calls[0].RecordMS != 0 {
		s = nextEvent[denproto.VoiceState](t, sub, denproto.EventVoiceState)
	}
	if got, err := f.d.CallRecord(ctx, owner, lounge.ID); err != nil || got.RecordMS != 0 {
		t.Fatalf("staff read the reset record as %+v, %v", got, err)
	}
	f.clock = f.clock.Add(2 * time.Minute)
	f.d.recordDue()
	f.d.leaveCall(sock)
	if got := f.storedRecord(lounge.ID); got != 0 {
		t.Fatalf("the call going on as the record was reset wrote %d", got)
	}

	sock = f.inCall(owner, lounge.ID)
	f.clock = f.clock.Add(10 * time.Second)
	f.d.leaveCall(sock)
	if got := f.storedRecord(lounge.ID); got != 10000 {
		t.Fatalf("the next call wrote %d", got)
	}
}
