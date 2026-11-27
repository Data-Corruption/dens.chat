package denclient_test

import (
	"context"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Deleting a channel's newest message before another member opens it clears
// their unread state immediately, across single deletes (leaving the channel
// empty or at an already-read message), ban bulk deletes, and disappearing
// messages, while deleting an older message leaves the channel unread.
func TestDeleteClearsUnreadLive(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()

	statusOf := func(m *denclient.Manager) denclient.Status {
		for _, s := range m.Statuses() {
			if s.DenID == denID {
				return s
			}
		}
		return denclient.Status{}
	}
	waitUnread := func(m *denclient.Manager, unread, mentions int, what string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			s := statusOf(m)
			if s.Unread == unread && s.Mentions == mentions {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: got unread=%d mentions=%d, want %d and %d", what, s.Unread, s.Mentions, unread, mentions)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Empty channel: send -> unread; delete -> clears immediately without opening.
	stream, stop := member.Stream()
	defer stop()
	m1 := send(t, owner, denID, channelID, "only message")
	waitUnread(member, 1, 0, "empty channel became unread")
	if err := owner.DeleteMessage(ctx, denID, m1.ID); err != nil {
		t.Fatal(err)
	}
	waitUnread(member, 0, 0, "deleting only message cleared unread")
	viewOf(t, member, denID, "empty channel LastMessage cleared", func(v denclient.View) bool {
		r := readIn(v, channelID)
		return r.LastMessage == "" && r.ReadPosition == ""
	})

	// Non-empty channel: read m2, send m3 & m4, delete m3 (stays unread),
	// delete m4 (clears immediately).
	m2 := send(t, owner, denID, channelID, "kept")
	viewOf(t, member, denID, "m2 arrived", func(v denclient.View) bool { return readIn(v, channelID).LastMessage == m2.ID })
	if err := member.MarkRead(ctx, denID, channelID, m2.ID); err != nil {
		t.Fatal(err)
	}
	waitUnread(member, 0, 0, "read m2")

	m3 := send(t, owner, denID, channelID, "middle")
	m4 := send(t, owner, denID, channelID, "newest")
	waitUnread(member, 1, 0, "m3 and m4 unread")
	if err := owner.DeleteMessage(ctx, denID, m3.ID); err != nil {
		t.Fatal(err)
	}
	// Deleting non-newest m3 keeps m4 unread.
	deletedOn(t, stream, m3.ID)
	waitUnread(member, 1, 0, "deleting non-newest kept unread")
	if err := owner.DeleteMessage(ctx, denID, m4.ID); err != nil {
		t.Fatal(err)
	}
	waitUnread(member, 0, 0, "deleting newest cleared unread")
	viewOf(t, member, denID, "LastMessage reverted to m2", func(v denclient.View) bool {
		r := readIn(v, channelID)
		return r.LastMessage == m2.ID && r.ReadPosition == m2.ID
	})

	// Ban bulk delete clears unread for another member on the unopened channel.
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	carol := h.client(t, noOwnDen)
	join(t, carol, invite, "carol")
	carolID := me(t, carol, denID).ID
	send(t, carol, denID, channelID, "spam 1")
	send(t, carol, denID, channelID, "spam 2")
	waitUnread(member, 1, 0, "carol's spam unread")
	if err := owner.RemoveMember(ctx, denID, carolID, denproto.RemoveRequest{Ban: true, DeleteMessages: 3600}); err != nil {
		t.Fatal(err)
	}
	waitUnread(member, 0, 0, "ban bulk delete cleared unread")
	viewOf(t, member, denID, "LastMessage reverted after ban", func(v denclient.View) bool {
		return readIn(v, channelID).LastMessage == m2.ID
	})

	// Disappearing DM message clears unread and mention count when it expires.
	h.d.ShortTimes = true
	h.d.StartTimer()
	defer h.d.StopTimer()
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	if err := owner.SetExpiry(ctx, denID, dm, 2); err != nil {
		t.Fatal(err)
	}
	ephemeral := sendDM(t, owner, denID, dm, "vanishing dm")
	waitUnread(member, 1, 1, "disappearing DM unread")
	deletedOn(t, stream, ephemeral.ID)
	waitUnread(member, 0, 0, "disappearing DM cleared unread")
	viewOf(t, member, denID, "DM LastMessage cleared on disappearance", func(v denclient.View) bool {
		r := readIn(v, dm)
		return r.LastMessage == "" && r.MentionCount == 0
	})
}
