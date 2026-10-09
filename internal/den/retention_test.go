package den

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// setRetention sets the den's retention period as its owner.
func (f *fixture) setRetention(owner *Session, days int) {
	f.t.Helper()
	if _, err := f.d.Update(context.Background(), owner, denproto.DenUpdateRequest{Retention: &days}); err != nil {
		f.t.Fatal(err)
	}
}

// nextEvent reads events from sub until one of type t, and returns its
// data.
func nextEvent[T any](t *testing.T, sub *Sub, typ string) T {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-sub.Events:
			if !ok {
				t.Fatalf("the subscription ended before %s", typ)
			}
			if e.T != typ {
				continue
			}
			var v T
			if err := json.Unmarshal(e.D, &v); err != nil {
				t.Fatal(err)
			}
			return v
		case <-timeout:
			t.Fatalf("no %s", typ)
		}
	}
}

func TestRetentionDeletesOldMessages(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	carol := f.member(owner, "carol")
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: denproto.FormatID(carol.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	file := f.mustUpload(member, "old.txt", []byte("old"))
	old := f.mustSendFiles(member, general.ID, file.ID)
	blob, err := f.d.Upload(ctx, member, "", 5, bytes.NewReader([]byte("noise")), true)
	if err != nil {
		t.Fatal(err)
	}
	oldDM := f.mustSendFiles(member, dm.ID, blob.ID)

	// Neither an edit nor a reply keeps a message past the period.
	f.clock = f.clock.Add(36 * time.Hour)
	if _, err := f.d.Edit(ctx, member, old.ID, denproto.EditRequest{Revision: 1, Text: "edited later"}); err != nil {
		t.Fatal(err)
	}
	reply, err := f.d.Send(ctx, owner, general.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "a reply", ReplyTo: old.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(13 * time.Hour)

	sub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	f.setRetention(owner, 2)
	if got := nextEvent[denproto.Den](t, sub, denproto.EventDenUpdated); got.Retention != 2 {
		t.Fatalf("den.updated: %+v", got)
	}
	f.d.expire()
	if got := nextEvent[denproto.MessagesExpired](t, sub, denproto.EventMessagesExpired); got.Through != oldDM.ID {
		t.Fatalf("messages.expired names %s, not the last old message %s", got.Through, oldDM.ID)
	}

	h, err := f.d.History(ctx, member, general.ID, HistoryQuery{Limit: 10})
	if err != nil || len(h.Messages) != 1 || h.Messages[0].ID != reply.ID {
		t.Fatalf("the channel after retention: %+v %v", h.Messages, err)
	}
	if h.Messages[0].Reply != nil {
		t.Fatalf("a reply still quotes a message retention deleted: %+v", h.Messages[0].Reply)
	}
	if h, err := f.d.History(ctx, member, dm.ID, HistoryQuery{Limit: 10}); err != nil || len(h.Messages) != 0 {
		t.Fatalf("the DM after retention: %+v %v", h.Messages, err)
	}
	for _, id := range []string{file.ID, blob.ID} {
		if _, err := f.read(member, id, false); !denproto.IsCode(err, denproto.CodeNotFound) {
			t.Fatalf("file %s after retention: %v", id, err)
		}
	}
	if files, _ := f.stored(); len(files) != 0 {
		t.Fatalf("stored after retention: %v", files)
	}

	// Nothing more has passed the period, so another pass says nothing.
	f.d.expire()
	f.send(owner, general.ID, "after")
	if e := nextEvent[denproto.Message](t, sub, denproto.EventMessageCreated); e.Text != "after" {
		t.Fatalf("the next event: %+v", e)
	}
}

func TestRetentionOldestFirst(t *testing.T) {
	f, owner, _ := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.seed(general, owner, 2*expireBatch+100)
	f.clock = f.clock.Add(24*time.Hour + time.Minute)
	young := f.send(owner, general.ID, "young")
	// A message from before the clock last ran back stays as long as a
	// younger one sent before it does.
	backdated := f.send(owner, general.ID, "backdated")
	if _, err := f.db.Exec(`UPDATE den_messages SET created_at = ? WHERE id = ?`,
		f.clock.Add(-48*time.Hour).UnixMilli(), mustInt(t, backdated.ID)); err != nil {
		t.Fatal(err)
	}

	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	f.setRetention(owner, 1)
	f.d.expire()
	through := nextEvent[denproto.MessagesExpired](t, sub, denproto.EventMessagesExpired).Through
	if mustInt(t, through) != mustInt(t, young.ID)-1 {
		t.Fatalf("messages.expired names %s; the young message is %s", through, young.ID)
	}
	h, err := f.d.History(ctx, owner, general.ID, HistoryQuery{Limit: 10})
	if err != nil || len(h.Messages) != 2 || h.Messages[0].ID != young.ID || h.Messages[1].ID != backdated.ID {
		t.Fatalf("after the pass: %+v %v", h.Messages, err)
	}

	// Once the young one passes the period, both go.
	f.clock = f.clock.Add(24*time.Hour + time.Minute)
	f.d.expire()
	if got := nextEvent[denproto.MessagesExpired](t, sub, denproto.EventMessagesExpired).Through; got != backdated.ID {
		t.Fatalf("the next pass names %s, not %s", got, backdated.ID)
	}
}

func TestRetentionSetting(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	days := 30
	_, err := f.d.Update(ctx, member, denproto.DenUpdateRequest{Retention: &days})
	wantCode(t, err, denproto.CodeForbidden)
	for _, bad := range []int{-1, denproto.MaxRetention + 1} {
		_, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{Retention: &bad})
		wantCode(t, err, denproto.CodeInvalidField)
	}
	f.setRetention(owner, 30)
	if info, _ := f.d.Info(); info.Retention != 30 {
		t.Fatalf("retention: %d", info.Retention)
	}
	f.open()
	if info, _ := f.d.Info(); info.Retention != 30 {
		t.Fatalf("retention after reopening: %d", info.Retention)
	}
	// Someone with an invite sees the den's name, not its settings.
	preview, err := f.d.Preview(ctx, f.invite(owner, 1).Code)
	if err != nil || preview.Retention != 0 {
		t.Fatalf("a join preview: %+v %v", preview, err)
	}

	// Off, nothing goes however old.
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.send(owner, general.ID, "kept")
	f.setRetention(owner, 0)
	f.clock = f.clock.Add(10 * 365 * 24 * time.Hour)
	f.d.expire()
	if h, _ := f.d.History(ctx, owner, general.ID, HistoryQuery{Limit: 10}); len(h.Messages) != 1 {
		t.Fatalf("with retention off: %+v", h.Messages)
	}
}

func TestRetentionPreview(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.seed(general, owner, 3)
	file := f.mustUpload(member, "a.txt", []byte("twelve bytes"))
	f.mustSendFiles(member, general.ID, file.ID)
	f.clock = f.clock.Add(10 * 24 * time.Hour)
	f.send(owner, general.ID, "recent")

	if p, err := f.d.RetentionPreview(ctx, owner, 7); err != nil || p.Messages != 4 || p.Bytes != 12 {
		t.Fatalf("7 days: %+v %v", p, err)
	}
	if p, err := f.d.RetentionPreview(ctx, owner, 30); err != nil || p.Messages != 0 || p.Bytes != 0 {
		t.Fatalf("30 days: %+v %v", p, err)
	}
	_, err := f.d.RetentionPreview(ctx, member, 7)
	wantCode(t, err, denproto.CodeForbidden)
	for _, bad := range []int{0, denproto.MaxRetention + 1} {
		_, err := f.d.RetentionPreview(ctx, owner, bad)
		wantCode(t, err, denproto.CodeInvalidField)
	}

	// A pass deletes what the preview counted, and nothing more.
	f.setRetention(owner, 7)
	f.d.expire()
	if h, err := f.d.History(ctx, owner, general.ID, HistoryQuery{Limit: 10}); err != nil || len(h.Messages) != 1 || h.Messages[0].Text != "recent" {
		t.Fatalf("after the pass: %+v %v", h.Messages, err)
	}
	if p, err := f.d.RetentionPreview(ctx, owner, 7); err != nil || p.Messages != 0 || p.Bytes != 0 {
		t.Fatalf("the preview after the pass: %+v %v", p, err)
	}
}

func TestRetentionPlansPasses(t *testing.T) {
	f, owner, _ := chatFixture(t)
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.send(owner, general.ID, "old")
	f.clock = f.clock.Add(48 * time.Hour)
	recent := f.send(owner, general.ID, "recent")
	f.setRetention(owner, 1)

	// Started, the den deletes what passed the period while it was off.
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	f.d.StartRetention()
	if got := nextEvent[denproto.MessagesExpired](t, sub, denproto.EventMessagesExpired).Through; mustInt(t, got) != mustInt(t, recent.ID)-1 {
		t.Fatalf("the first pass names %s", got)
	}

	// The next pass is due when the oldest message passes the period, and
	// no sooner than an hour after the last.
	at, err := f.d.nextExpiry(1, f.clock)
	if err != nil || !at.Equal(f.clock.Add(24*time.Hour)) {
		t.Fatalf("the next pass: %v %v", at, err)
	}
	if at, _ := f.d.nextExpiry(1, f.clock.Add(25*time.Hour)); !at.Equal(f.clock.Add(26 * time.Hour)) {
		t.Fatalf("a pass due within the hour after the last: %v", at)
	}
	if _, err := f.db.Exec(`DELETE FROM den_messages`); err != nil {
		t.Fatal(err)
	}
	if at, _ := f.d.nextExpiry(1, time.Time{}); !at.Equal(f.clock.Add(24 * time.Hour)) {
		t.Fatalf("with no messages, the soonest a new one could pass: %v", at)
	}
}
