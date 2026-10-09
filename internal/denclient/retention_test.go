package denclient_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func readIn(v denclient.View, channel string) denproto.ReadState {
	for _, r := range v.ReadStates {
		if r.ChannelID == channel {
			return r
		}
	}
	return denproto.ReadState{}
}

// The owner's retention period (M5) reaches members, and a pass reaches
// their pages as one event: what passed the period goes, and a channel
// left empty has nothing unread.
func TestRetention(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	old := send(t, owner, denID, channelID, "old news")
	// The den takes it for two days old.
	if _, err := h.s.db.Exec(`UPDATE den_messages SET created_at = ?`, time.Now().Add(-48*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the message unread", func(v denclient.View) bool { return readIn(v, channelID).LastMessage == old.ID })

	if p, err := owner.RetentionPreview(ctx, denID, 1); err != nil || p.Messages != 1 {
		t.Fatalf("the owner's preview: %+v %v", p, err)
	}
	if _, err := member.RetentionPreview(ctx, denID, 1); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("a member's preview: %v", err)
	}
	bad := denproto.MaxRetention + 1
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Retention: &bad}); err == nil {
		t.Fatal("a period past the longest went to the den")
	}

	stream, stop := member.Stream()
	defer stop()
	days := 1
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Retention: &days}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the period", func(v denclient.View) bool { return v.Retention == 1 })
	waitFor(t, member, "the period in the den's status", func(s denclient.Status) bool { return s.Retention == 1 })

	h.d.StartRetention()
	timeout := time.After(10 * time.Second)
	for expired := false; !expired; {
		select {
		case e, ok := <-stream:
			if !ok {
				t.Fatal("the page stream closed")
			}
			for _, ev := range e.Events {
				if ev.T != denproto.EventMessagesExpired {
					continue
				}
				var x denproto.MessagesExpired
				if err := json.Unmarshal(ev.D, &x); err != nil || x.Through != old.ID {
					t.Fatalf("messages.expired reached the page as %s (%v)", ev.D, err)
				}
				expired = true
			}
		case <-timeout:
			t.Fatal("no messages.expired reached the page")
		}
	}
	viewOf(t, member, denID, "nothing unread", func(v denclient.View) bool {
		r := readIn(v, channelID)
		return r.LastMessage == "" && r.MentionCount == 0
	})
	if h, err := member.History(ctx, denID, channelID, denclient.HistoryQuery{Limit: 10}); err != nil || len(h.Messages) != 0 {
		t.Fatalf("history after the pass: %+v %v", h, err)
	}
}
