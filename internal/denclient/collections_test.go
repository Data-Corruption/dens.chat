package denclient_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// TestCollectionsSyncDMOpeningAndRoleFiltering exercises shared and personal
// collections end to end across two connected clients: live collection events,
// opening DM messages (and DM file attachments via OpenFile) and Notes inside
// personal collections after a den restart without loading DM history first,
// and role-loss visibility filtering of both items and counts.
func TestCollectionsSyncDMOpeningAndRoleFiltering(t *testing.T) {
	h, alice, bob, denID, general := chatDen(t)
	ctx := context.Background()
	bobID := me(t, bob, denID).ID

	viewOf(t, alice, denID, "alice has_collections", func(v denclient.View) bool { return v.HasCollections })
	viewOf(t, bob, denID, "bob has_collections", func(v denclient.View) bool { return v.HasCollections })

	// Shared collection on #general with who_adds = everyone.
	guides, err := alice.CreateChannelCollection(ctx, denID, general, "Guides", denproto.CollectionAddEveryone)
	if err != nil {
		t.Fatal(err)
	}
	viewOf(t, bob, denID, "bob sees Guides live", func(v denclient.View) bool {
		return len(v.Collections) == 1 && v.Collections[0].ID == guides.ID && v.Collections[0].Name == "Guides"
	})

	// Post a message in #general and have bob add it to Guides.
	genMsg, err := alice.Send(ctx, denID, general, denproto.SendRequest{
		Nonce: denproto.Random(denproto.NonceBytes),
		Text:  "Welcome guide",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.AddCollectionItem(ctx, denID, guides.ID, genMsg.ID); err != nil {
		t.Fatal(err)
	}
	viewOf(t, alice, denID, "alice sees Guides count=1", func(v denclient.View) bool {
		return len(v.Collections) == 1 && v.Collections[0].Count == 1
	})

	// Role-gated channel #raids visible to bob initially.
	raiders, err := alice.CreateRole(ctx, denID, "Raiders")
	if err != nil {
		t.Fatal(err)
	}
	raidsName := "raids"
	rids := []string{raiders.ID}
	if err := alice.Manage(ctx, denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &raidsName, RoleIDs: &rids}); err != nil {
		t.Fatal(err)
	}
	if err := alice.SetMemberRoles(ctx, denID, bobID, rids); err != nil {
		t.Fatal(err)
	}
	bobView := viewOf(t, bob, denID, "bob sees #raids", func(v denclient.View) bool { return channelNamed(v, "raids") })
	var raidsID string
	for _, c := range bobView.Channels {
		if c.Name == "raids" {
			raidsID = c.ID
		}
	}
	raidMsg, err := alice.Send(ctx, denID, raidsID, denproto.SendRequest{
		Nonce: denproto.Random(denproto.NonceBytes),
		Text:  "Secret boss strat",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Open a DM between alice and bob, send a sealed message with a photo, and open bob's Notes.
	dm := openDM(t, alice, bob, denID)
	readyDM(t, alice, bob, denID, dm)
	dmFiles := sendFiles(t, alice, denID, dm)
	dmTextMsg, err := alice.Send(ctx, denID, dm, denproto.SendRequest{
		Nonce: denproto.Random(denproto.NonceBytes),
		Text:  "secret dm recipe",
	})
	if err != nil {
		t.Fatal(err)
	}
	notes := openNotes(t, bob, denID).ID
	noteMsg, err := bob.Send(ctx, denID, notes, denproto.SendRequest{
		Nonce: denproto.Random(denproto.NonceBytes),
		Text:  "private note to self",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Bob creates personal collection "Saved" and saves channel, role-gated, DM text, DM photo, and Notes messages.
	saved, err := bob.CreatePersonalCollection(ctx, denID, "Saved")
	if err != nil {
		t.Fatal(err)
	}
	for _, mid := range []string{genMsg.ID, raidMsg.ID, dmTextMsg.ID, dmFiles.message, noteMsg.ID} {
		if _, err := bob.AddCollectionItem(ctx, denID, saved.ID, mid); err != nil {
			t.Fatalf("add %s: %v", mid, err)
		}
	}
	ids, err := bob.MessageCollections(ctx, denID, genMsg.ID)
	if err != nil || !slices.Equal(ids, []string{guides.ID, saved.ID}) {
		t.Fatalf("bob MessageCollections: %v %v", ids, err)
	}

	// Restart the den so bob's in-memory dmFiles map is wiped, then load CollectionItems WITHOUT loading DM history first!
	h.restart()
	waitFor(t, bob, "bob reconnected", connected)
	waitFor(t, alice, "alice reconnected", connected)

	got, err := bob.CollectionItems(ctx, denID, saved.ID)
	if err != nil || got.Collection.Count != 5 || len(got.Items) != 5 {
		t.Fatalf("CollectionItems after restart: count=%d len=%d err=%v", got.Collection.Count, len(got.Items), err)
	}
	byID := map[string]denclient.PageMessage{}
	for _, it := range got.Items {
		byID[it.Message.ID] = it.Message
	}
	if byID[dmTextMsg.ID].Locked != "" || byID[dmTextMsg.ID].Text != "secret dm recipe" {
		t.Fatalf("DM text not opened in collection: %+v", byID[dmTextMsg.ID])
	}
	if byID[noteMsg.ID].Locked != "" || byID[noteMsg.ID].Text != "private note to self" {
		t.Fatalf("Notes not opened in collection: %+v", byID[noteMsg.ID])
	}
	if byID[dmFiles.message].Locked != "" || len(byID[dmFiles.message].Attachments) != 2 {
		t.Fatalf("DM attachments not opened in collection: %+v", byID[dmFiles.message])
	}
	// DM photo opens via OpenFile immediately using keys populated by CollectionItems!
	rawPhoto, err := readUnder(bob, denID, dm, dmFiles.message, dmFiles.photo.ID, 0, 0)
	if err != nil || media.Sniff(rawPhoto) != media.JPEG || int64(len(rawPhoto)) != dmFiles.photo.Size {
		t.Fatalf("DM photo open after CollectionItems: len=%d err=%v", len(rawPhoto), err)
	}

	// Revoke bob's Raiders role -> bob's View.Collections count drops from 5 to 4 and raidMsg vanishes.
	if err := alice.SetMemberRoles(ctx, denID, bobID, []string{}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, bob, denID, "bob Saved count drops on role loss", func(v denclient.View) bool {
		for _, c := range v.Collections {
			if c.ID == saved.ID {
				return c.Count == 4
			}
		}
		return false
	})
	got, err = bob.CollectionItems(ctx, denID, saved.ID)
	if err != nil || got.Collection.Count != 4 || len(got.Items) != 4 {
		t.Fatalf("CollectionItems after role loss: count=%d len=%d err=%v", got.Collection.Count, len(got.Items), err)
	}

	// Re-grant Raiders -> count returns to 5 and raidMsg reappears.
	if err := alice.SetMemberRoles(ctx, denID, bobID, rids); err != nil {
		t.Fatal(err)
	}
	viewOf(t, bob, denID, "bob Saved count restores on role regain", func(v denclient.View) bool {
		for _, c := range v.Collections {
			if c.ID == saved.ID {
				return c.Count == 5
			}
		}
		return false
	})

	// Fresh Collections() query matches.
	cols, err := bob.Collections(ctx, denID)
	if err != nil || len(cols) != 2 {
		t.Fatalf("Collections: %+v %v", cols, err)
	}

	// Update and delete shared collection propagate live.
	newName := "Channel Guides"
	if _, err := alice.UpdateCollection(ctx, denID, guides.ID, denproto.CollectionRequest{Name: &newName}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, bob, denID, "bob sees renamed Guides", func(v denclient.View) bool {
		for _, c := range v.Collections {
			if c.ID == guides.ID {
				return c.Name == newName
			}
		}
		return false
	})
	if err := alice.DeleteCollection(ctx, denID, guides.ID); err != nil {
		t.Fatal(err)
	}
	viewOf(t, bob, denID, "bob sees deleted Guides gone", func(v denclient.View) bool {
		for _, c := range v.Collections {
			if c.ID == guides.ID {
				return false
			}
		}
		return true
	})
}

// TestCollectionsOversizedMultiPageAndNameFolding verifies that an oversized
// collection exceeding the 896 KiB page budget (and 1 MiB response limit)
// pages cleanly end to end, and that the den and client agree on Unicode
// name folding for both roles and collections.
func TestCollectionsOversizedMultiPageAndNameFolding(t *testing.T) {
	h, alice, bob, denID, general := chatDen(t)
	ctx := context.Background()

	// Den and client agree on École/école roles and İ/i vs İ/i\u0307 collections.
	if _, err := alice.CreateRole(ctx, denID, "École"); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.CreateRole(ctx, denID, "école"); !denproto.IsCode(err, denproto.CodeRoleNameTaken) {
		t.Fatalf("duplicate École/école role: %v", err)
	}
	if _, err := alice.CreateRole(ctx, denID, "İ"); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.CreateRole(ctx, denID, "i\u0307"); err != nil {
		t.Fatalf("distinct İ and i\\u0307 roles: %v", err)
	}
	if _, err := alice.CreateChannelCollection(ctx, denID, general, "İ", denproto.CollectionAddStaff); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.CreateChannelCollection(ctx, denID, general, "i", denproto.CollectionAddStaff); !denproto.IsCode(err, denproto.CodeCollectionNameTaken) {
		t.Fatalf("duplicate İ/i collection: %v", err)
	}
	if _, err := alice.CreateChannelCollection(ctx, denID, general, "i\u0307", denproto.CollectionAddStaff); err != nil {
		t.Fatalf("distinct İ and i\\u0307 collections: %v", err)
	}
	// Reconnecting loads the snapshot cleanly without rejecting the den.
	h.restart()
	waitFor(t, alice, "alice reconnected with folded roles/collections", connected)
	waitFor(t, bob, "bob reconnected with folded roles/collections", connected)

	// Build an oversized collection (>1.08 MiB JSON) directly on the den to
	// stay fast and bypass per-member send rate limits, then fetch via client.
	bigCol, err := alice.CreateChannelCollection(ctx, denID, general, "Oversized", denproto.CollectionAddStaff)
	if err != nil {
		t.Fatal(err)
	}
	const total = 90
	bigText := strings.Repeat("あ", 4000)
	ownerID := me(t, alice, denID).ID
	oid, _ := denproto.ParseID(ownerID)
	ownerSess := &den.Session{MemberID: oid, Role: denproto.RoleOwner, Version: denproto.Version}
	want := make([]string, total)
	for i := range total {
		m, err := h.d.Send(ctx, ownerSess, general, denproto.SendRequest{
			Nonce: denproto.Random(denproto.NonceBytes),
			Text:  bigText,
		})
		if err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		if _, err := h.d.AddCollectionItem(ctx, ownerSess, bigCol.ID, denproto.CollectionItemRequest{MessageID: m.ID}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
		want[total-1-i] = m.ID
	}

	got, err := bob.CollectionItems(ctx, denID, bigCol.ID)
	if err != nil {
		t.Fatalf("bob CollectionItems oversized: %v", err)
	}
	if got.Collection.Count != total || len(got.Items) != total {
		t.Fatalf("got count=%d len=%d, want %d", got.Collection.Count, len(got.Items), total)
	}
	gotIDs := make([]string, len(got.Items))
	for i, it := range got.Items {
		gotIDs[i] = it.Message.ID
	}
	if !slices.Equal(gotIDs, want) {
		t.Fatal("oversized multi-page collection items returned out of order")
	}
}
