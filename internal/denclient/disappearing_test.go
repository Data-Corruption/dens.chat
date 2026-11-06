package denclient_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// deletedOn waits for a page stream to hear that a message was deleted.
func deletedOn(t *testing.T, stream <-chan denclient.PageEvent, id string) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case e, ok := <-stream:
			if !ok {
				t.Fatal("the page stream closed")
			}
			for _, ev := range e.Events {
				var d denproto.MessageDeleted
				if ev.T == denproto.EventMessageDeleted && json.Unmarshal(ev.D, &d) == nil && d.ID == id {
					return
				}
			}
		case <-timeout:
			t.Fatalf("the page never heard that %s went", id)
		}
	}
}

// A DM's member sets its messages to disappear (M16.4), which reaches the
// other's view with who set it, and a message sent then goes once its time
// passes: from the den, and from the other member's cache, page and
// notifications, whether their Dens can reach the den or not.
func TestDisappearingMessages(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	h.d.ShortTimes = true // a time of seconds, as a development instance's den takes
	h.d.StartTimer()
	defer h.d.StopTimer()
	ctx := context.Background()
	s := newCachedStore(t)
	heard := &notices{}
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	address := h.public.Listener.Addr().String()
	carol, stop := h.clientVia(t, s, noOwnDen, address, func(m *denclient.Manager) { m.Notices = heard })
	join(t, carol, invite, "carol")
	dm := openDM(t, owner, carol, denID)
	readyDM(t, owner, carol, denID, dm)

	if err := carol.SetExpiry(ctx, denID, dm, -1); err == nil {
		t.Fatal("a time below 0 went to the den")
	}
	if err := owner.SetExpiry(ctx, denID, dm, 3); err != nil {
		t.Fatal(err)
	}
	alice := me(t, owner, denID).ID
	viewOf(t, carol, denID, "the DM's time, and who set it", func(v denclient.View) bool {
		i := slices.IndexFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == dm })
		return v.Disappearing && i >= 0 && v.Channels[i].ExpiresAfter == 3 && v.Channels[i].ExpiresSet != nil &&
			v.Channels[i].ExpiresSet.By == alice
	})

	// Connected: the den deletes it, and the cache lets it go.
	stream, stopStream := carol.Stream()
	sent := sendDM(t, owner, denID, dm, "gone soon")
	if sent.ExpiresAt == 0 {
		t.Fatalf("a message sent under a time: %+v", sent.Message)
	}
	heard.waitMessage(t, sent.ID)
	if p := pageOf(t, carol, denID, dm, denclient.HistoryQuery{Limit: 50}); !slices.ContainsFunc(p.Messages, func(m denclient.PageMessage) bool {
		return m.ID == sent.ID && m.ExpiresAt == sent.ExpiresAt
	}) {
		t.Fatalf("carol's history before it went: %+v", p.Messages)
	}
	deletedOn(t, stream, sent.ID)
	stopStream()
	heard.waitSaid(t, "deleted "+dm+" "+sent.ID)
	for _, m := range []*denclient.Manager{owner, carol} {
		if slices.ContainsFunc(history(t, m, denID, dm), func(m denclient.PageMessage) bool { return m.ID == sent.ID }) {
			t.Fatal("a history still holds it")
		}
	}

	// Offline: the member's Dens can't reach the den, and lets it go all
	// the same, from its cache, its page and its notifications.
	offline := sendDM(t, owner, denID, dm, "gone while away")
	heard.waitMessage(t, offline.ID)
	eventually(t, "the message in carol's cache", func() bool {
		return slices.ContainsFunc(pageOf(t, carol, denID, dm, denclient.HistoryQuery{Limit: 50}).Messages,
			func(m denclient.PageMessage) bool { return m.ID == offline.ID })
	})
	stop()
	carol, stop = h.clientVia(t, s, noOwnDen, hangUp(t), func(m *denclient.Manager) { m.Notices = heard })
	defer stop()
	stream, stopStream = carol.Stream()
	defer stopStream()
	deletedOn(t, stream, offline.ID)
	heard.waitSaid(t, "deleted "+dm+" "+offline.ID)
	if v, _ := carol.View(denID); v.State == denclient.StateConnected {
		t.Fatal("a den that's away says it's connected")
	}
	if p := pageOf(t, carol, denID, dm, denclient.HistoryQuery{Limit: 50}); slices.ContainsFunc(p.Messages, func(m denclient.PageMessage) bool {
		return m.ID == offline.ID
	}) {
		t.Fatalf("carol's cache still holds it offline: %+v", p.Messages)
	}
}
