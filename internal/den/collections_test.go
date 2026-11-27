package den

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func strPtr(s string) *string { return &s }

func sendText(t *testing.T, f *fixture, s *Session, channelID, text string) denproto.Message {
	t.Helper()
	m, err := f.d.Send(context.Background(), s, channelID, denproto.SendRequest{
		Nonce: denproto.Random(denproto.NonceBytes),
		Text:  text,
	})
	if err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
	return m
}

func sendSealed(t *testing.T, f *fixture, s *Session, channelID, keyID string) denproto.Message {
	t.Helper()
	m, err := f.d.Send(context.Background(), s, channelID, denproto.SendRequest{
		Nonce:  denproto.Random(denproto.NonceBytes),
		Sealed: []byte("sealed-payload"),
		KeyID:  keyID,
	})
	if err != nil {
		t.Fatalf("send sealed in %s: %v", channelID, err)
	}
	return m
}

func TestSharedCollectionsCRUDPermissionsAndAudit(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	mod := f.addMember(owner, "carol")
	if _, err := f.d.SetRole(ctx, owner, denproto.FormatID(mod.MemberID), denproto.RoleRequest{Role: denproto.RoleModerator}); err != nil {
		t.Fatal(err)
	}
	mod.Role = denproto.RoleModerator

	info, ok := f.d.Info()
	if !ok || !info.Collections {
		t.Fatalf("expected Den.Info().Collections = true, got %+v", info)
	}

	general, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("general")})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("other")})
	if err != nil {
		t.Fatal(err)
	}
	voice, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("Lounge"), Kind: denproto.KindVoice})
	if err != nil {
		t.Fatal(err)
	}

	// Non-staff cannot create shared collections; non-text channels rejected.
	if _, err := f.d.CreateChannelCollection(ctx, bob, general.ID, denproto.CollectionRequest{Name: strPtr("Guides")}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob create shared: %v", err)
	}
	if _, err := f.d.CreateChannelCollection(ctx, owner, voice.ID, denproto.CollectionRequest{Name: strPtr("Voice")}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("voice shared collection: %v", err)
	}

	// Owner creates default staff-only collection; moderator cannot duplicate case-insensitively.
	guides, err := f.d.CreateChannelCollection(ctx, owner, general.ID, denproto.CollectionRequest{Name: strPtr("  Guides 🏆  ")})
	if err != nil || guides.Name != "Guides 🏆" || guides.WhoAdds != denproto.CollectionAddStaff || guides.ChannelID != general.ID || denproto.CheckCollection(guides) != nil {
		t.Fatalf("create Guides: %+v %v", guides, err)
	}
	if _, err := f.d.CreateChannelCollection(ctx, mod, general.ID, denproto.CollectionRequest{Name: strPtr("guides 🏆")}); !denproto.IsCode(err, denproto.CodeCollectionNameTaken) {
		t.Fatalf("duplicate shared name: %v", err)
	}
	// Same name in another channel is allowed.
	otherGuides, err := f.d.CreateChannelCollection(ctx, mod, other.ID, denproto.CollectionRequest{Name: strPtr("Guides 🏆")})
	if err != nil {
		t.Fatalf("same name in other channel: %v", err)
	}
	_ = otherGuides

	// Name validation: empty, >50 runes rejected.
	if _, err := f.d.CreateChannelCollection(ctx, owner, general.ID, denproto.CollectionRequest{Name: strPtr("")}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := f.d.CreateChannelCollection(ctx, owner, general.ID, denproto.CollectionRequest{Name: strPtr(strings.Repeat("x", denproto.MaxCollectionNameRunes+1))}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("long name: %v", err)
	}

	mGen := sendText(t, f, bob, general.ID, "raid strat")
	mGen2 := sendText(t, f, owner, general.ID, "loot rules")
	mOther := sendText(t, f, bob, other.ID, "wrong channel")

	// who_adds == "staff": bob cannot add; staff can; cross-channel rejected.
	if _, err := f.d.AddCollectionItem(ctx, bob, guides.ID, denproto.CollectionItemRequest{MessageID: mGen.ID}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob added to staff collection: %v", err)
	}
	if _, err := f.d.AddCollectionItem(ctx, owner, guides.ID, denproto.CollectionItemRequest{MessageID: mOther.ID}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("cross-channel add: %v", err)
	}
	ev, err := f.d.AddCollectionItem(ctx, owner, guides.ID, denproto.CollectionItemRequest{MessageID: mGen2.ID})
	if err != nil || ev.Count != 1 {
		t.Fatalf("owner add: %+v %v", ev, err)
	}
	// Idempotent re-add does not fail or double count.
	ev2, err := f.d.AddCollectionItem(ctx, mod, guides.ID, denproto.CollectionItemRequest{MessageID: mGen2.ID})
	if err != nil || ev2.Count != 1 || ev2.AddedBy != denproto.FormatID(owner.MemberID) {
		t.Fatalf("idempotent re-add: %+v %v", ev2, err)
	}

	// Non-staff cannot update or delete shared collection.
	if _, err := f.d.UpdateCollection(ctx, bob, guides.ID, denproto.CollectionRequest{Name: strPtr("Hack")}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob updated shared: %v", err)
	}
	if err := f.d.DeleteCollection(ctx, bob, guides.ID); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob deleted shared: %v", err)
	}

	// Moderator switches who_adds to everyone and renames; case-only rename works too.
	updated, err := f.d.UpdateCollection(ctx, mod, guides.ID, denproto.CollectionRequest{
		Name:    strPtr("Raid Guides"),
		WhoAdds: strPtr(denproto.CollectionAddEveryone),
	})
	if err != nil || updated.Name != "Raid Guides" || updated.WhoAdds != denproto.CollectionAddEveryone || updated.Count != 1 {
		t.Fatalf("update shared: %+v %v", updated, err)
	}
	if _, err := f.d.UpdateCollection(ctx, mod, guides.ID, denproto.CollectionRequest{Name: strPtr("raid guides")}); err != nil {
		t.Fatalf("case-only rename: %v", err)
	}

	// Now bob can add and remove his own item, but cannot remove owner's item.
	ev, err = f.d.AddCollectionItem(ctx, bob, guides.ID, denproto.CollectionItemRequest{MessageID: mGen.ID})
	if err != nil || ev.Count != 2 {
		t.Fatalf("bob add to everyone collection: %+v %v", ev, err)
	}
	if _, err := f.d.RemoveCollectionItem(ctx, bob, guides.ID, mGen2.ID); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob removed owner's item: %v", err)
	}
	// Self-remove by bob succeeds without writing an audit line.
	ev, err = f.d.RemoveCollectionItem(ctx, bob, guides.ID, mGen.ID)
	if err != nil || ev.Count != 1 {
		t.Fatalf("bob self-remove: %+v %v", ev, err)
	}
	// Re-add by bob, then moderator removes bob's item -> writes collection.item_removed audit line.
	if _, err := f.d.AddCollectionItem(ctx, bob, guides.ID, denproto.CollectionItemRequest{MessageID: mGen.ID}); err != nil {
		t.Fatal(err)
	}
	ev, err = f.d.RemoveCollectionItem(ctx, mod, guides.ID, mGen.ID)
	if err != nil || ev.Count != 1 {
		t.Fatalf("mod remove bob's item: %+v %v", ev, err)
	}

	// Delete shared collection.
	if err := f.d.DeleteCollection(ctx, mod, guides.ID); err != nil {
		t.Fatalf("mod delete shared: %v", err)
	}

	// Verify audit lines: created, updated, item_removed (for bob only), deleted.
	log, err := f.d.AuditLog(ctx, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, l := range log.Lines {
		if err := denproto.CheckAuditLine(l); err != nil {
			t.Errorf("invalid audit line %+v: %v", l, err)
		}
		kinds[l.Kind]++
		if l.Kind == denproto.AuditCollectionItemRemoved && l.MemberID != denproto.FormatID(bob.MemberID) {
			t.Errorf("item_removed member_id = %q, want bob", l.MemberID)
		}
	}
	for _, want := range []string{
		denproto.AuditCollectionCreated,
		denproto.AuditCollectionUpdated,
		denproto.AuditCollectionDeleted,
		denproto.AuditCollectionItemRemoved,
	} {
		if kinds[want] == 0 {
			t.Errorf("missing audit kind %s", want)
		}
	}
	if kinds[denproto.AuditCollectionItemRemoved] != 1 {
		t.Errorf("expected exactly 1 collection.item_removed audit line (not self-remove), got %d", kinds[denproto.AuditCollectionItemRemoved])
	}
}

func TestPersonalCollectionsIsolationVisibilityAndExpiry(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	f.d.SetClock(func() time.Time { return now })
	f.d.ShortTimes = true
	owner.Version = denproto.NotesVersion
	bob.Version = denproto.NotesVersion

	// Create general channel, role-gated #raids channel, DM between owner & bob, and bob's Notes.
	general, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("general")})
	if err != nil {
		t.Fatal(err)
	}
	raiders, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Raiders"})
	if err != nil {
		t.Fatal(err)
	}
	roleIDs := []string{raiders.ID}
	raids, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("raids"), RoleIDs: &roleIDs})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: roleIDs}); err != nil {
		t.Fatal(err)
	}
	dm, err := f.d.OpenDM(ctx, bob, denproto.DMRequest{MemberID: denproto.FormatID(owner.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	dmKeyID := f.keyFor(owner.MemberID, bob.MemberID, mustID(dm.ID)).id
	notes, err := f.d.OpenNotes(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}

	mGen := sendText(t, f, owner, general.ID, "public note")
	mRaid := sendText(t, f, owner, raids.ID, "secret raid plan")
	mDM := sendSealed(t, f, bob, dm.ID, dmKeyID)
	mNote := sendSealed(t, f, bob, notes.ID, notes.ID)

	// Disappearing DM message (expires in 10s).
	if _, err := f.d.SetExpiry(ctx, bob, dm.ID, denproto.ExpiryRequest{ExpiresAfter: 10}); err != nil {
		t.Fatal(err)
	}
	mExpire := sendSealed(t, f, bob, dm.ID, dmKeyID)

	// Personal collection rejects who_adds.
	if _, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{
		Name:    strPtr("Saved"),
		WhoAdds: strPtr(denproto.CollectionAddEveryone),
	}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("personal with who_adds: %v", err)
	}
	saved, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{Name: strPtr("Saved")})
	if err != nil || saved.ChannelID != "" || saved.WhoAdds != "" || denproto.CheckCollection(saved) != nil {
		t.Fatalf("create personal Saved: %+v %v", saved, err)
	}
	// Owner can also have a personal collection named "Saved" (separate scope).
	ownerSaved, err := f.d.CreatePersonalCollection(ctx, owner, denproto.CollectionRequest{Name: strPtr("Saved")})
	if err != nil {
		t.Fatalf("owner personal Saved: %v", err)
	}
	_ = ownerSaved

	// Owner gets 404 not_found on every operation targeting bob's personal collection.
	if _, err := f.d.CollectionItems(ctx, owner, saved.ID, CollectionItemsQuery{}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("owner read bob personal: %v", err)
	}
	if _, err := f.d.UpdateCollection(ctx, owner, saved.ID, denproto.CollectionRequest{Name: strPtr("X")}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("owner updated bob personal: %v", err)
	}
	if err := f.d.DeleteCollection(ctx, owner, saved.ID); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("owner deleted bob personal: %v", err)
	}
	if _, err := f.d.AddCollectionItem(ctx, owner, saved.ID, denproto.CollectionItemRequest{MessageID: mGen.ID}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("owner added to bob personal: %v", err)
	}
	if _, err := f.d.RemoveCollectionItem(ctx, owner, saved.ID, mGen.ID); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("owner removed from bob personal: %v", err)
	}

	// Bob saves messages from text, role-gated text, DM, expiring DM, and Notes.
	for _, mid := range []string{mGen.ID, mRaid.ID, mDM.ID, mNote.ID, mExpire.ID} {
		now = now.Add(time.Millisecond)
		if _, err := f.d.AddCollectionItem(ctx, bob, saved.ID, denproto.CollectionItemRequest{MessageID: mid}); err != nil {
			t.Fatalf("bob save %s: %v", mid, err)
		}
	}
	// MessageCollections shows saved.ID for bob, empty for owner.
	mcBob, err := f.d.MessageCollections(ctx, bob, mGen.ID)
	if err != nil || !slices.Equal(mcBob.CollectionIDs, []string{saved.ID}) {
		t.Fatalf("bob MessageCollections: %+v %v", mcBob, err)
	}
	mcOwner, err := f.d.MessageCollections(ctx, owner, mGen.ID)
	if err != nil || len(mcOwner.CollectionIDs) != 0 {
		t.Fatalf("owner MessageCollections leaked bob personal: %+v %v", mcOwner, err)
	}

	items, err := f.d.CollectionItems(ctx, bob, saved.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != 5 || len(items.Items) != 5 {
		t.Fatalf("initial items: count=%d len=%d err=%v", items.Collection.Count, len(items.Items), err)
	}
	// Order is newest-added first.
	if items.Items[0].Message.ID != mExpire.ID || items.Items[4].Message.ID != mGen.ID {
		t.Fatalf("unexpected item order: first=%s last=%s", items.Items[0].Message.ID, items.Items[4].Message.ID)
	}

	// Revoke bob's Raiders role: mRaid vanishes from items AND counts (Ready, List, Items).
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	items, err = f.d.CollectionItems(ctx, bob, saved.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != 4 || len(items.Items) != 4 {
		t.Fatalf("after role loss items: count=%d len=%d err=%v", items.Collection.Count, len(items.Items), err)
	}
	for _, it := range items.Items {
		if it.Message.ID == mRaid.ID {
			t.Fatal("role-gated message leaked after role revocation")
		}
	}
	list, err := f.d.ListCollections(ctx, bob)
	if err != nil || len(list.Collections) != 1 || list.Collections[0].Count != 4 {
		t.Fatalf("ListCollections after role loss: %+v %v", list, err)
	}
	snap := f.snapOf(bob)
	if len(snap.Collections) != 1 || snap.Collections[0].Count != 4 {
		t.Fatalf("Ready.Collections after role loss: %+v", snap.Collections)
	}

	// Re-grant Raiders: mRaid and count return!
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: roleIDs}); err != nil {
		t.Fatal(err)
	}
	items, err = f.d.CollectionItems(ctx, bob, saved.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != 5 || len(items.Items) != 5 {
		t.Fatalf("after role regain: count=%d len=%d err=%v", items.Collection.Count, len(items.Items), err)
	}

	// Advance clock past mExpire's 10s expiry: filtered even before deleteDue runs.
	now = now.Add(15 * time.Second)
	items, err = f.d.CollectionItems(ctx, bob, saved.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != 4 || len(items.Items) != 4 {
		t.Fatalf("after expiry before deleteDue: count=%d len=%d err=%v", items.Collection.Count, len(items.Items), err)
	}
	// deleteDue deletes the message row and cascades den_collection_items.
	if err := f.d.deleteDue(ctx); err != nil {
		t.Fatal(err)
	}
	var rawCount int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_collection_items WHERE message_id = ?`, mExpire.ID).Scan(&rawCount); err != nil || rawCount != 0 {
		t.Fatalf("expired message item not cascaded: %d, %v", rawCount, err)
	}

	// Personal collections never produce audit log entries.
	log, err := f.d.AuditLog(ctx, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range log.Lines {
		if strings.HasPrefix(l.Kind, "collection.") {
			t.Errorf("personal collection wrote audit line: %+v", l)
		}
	}
}

func TestCollectionsBoundsCascadesDepartAndReopen(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	f.d.SetClock(func() time.Time { return now })

	ch, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("general")})
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("temp")})
	if err != nil {
		t.Fatal(err)
	}

	// Cap at 20 shared collections per channel.
	var firstShared denproto.Collection
	for i := range denproto.MaxChannelCollections {
		c, err := f.d.CreateChannelCollection(ctx, owner, ch.ID, denproto.CollectionRequest{
			Name:    strPtr("Shared " + denproto.FormatID(int64(i+1))),
			WhoAdds: strPtr(denproto.CollectionAddEveryone),
		})
		if err != nil {
			t.Fatalf("create shared %d: %v", i, err)
		}
		if i == 0 {
			firstShared = c
		}
	}
	if _, err := f.d.CreateChannelCollection(ctx, owner, ch.ID, denproto.CollectionRequest{Name: strPtr("Shared 21")}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("21st shared collection: %v", err)
	}

	// Cap at 20 personal collections per member.
	var bobFirst denproto.Collection
	for i := range denproto.MaxPersonalCollections {
		c, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{
			Name: strPtr("Mine " + denproto.FormatID(int64(i+1))),
		})
		if err != nil {
			t.Fatalf("create personal %d: %v", i, err)
		}
		if i == 0 {
			bobFirst = c
		}
	}
	if _, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{Name: strPtr("Mine 21")}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("21st personal collection: %v", err)
	}

	// Cap at 200 items per collection.
	msgs := make([]denproto.Message, denproto.MaxCollectionItems+1)
	for i := range msgs {
		msgs[i] = sendText(t, f, owner, ch.ID, "msg "+denproto.FormatID(int64(i+1)))
	}
	for i := range denproto.MaxCollectionItems {
		if _, err := f.d.AddCollectionItem(ctx, owner, firstShared.ID, denproto.CollectionItemRequest{MessageID: msgs[i].ID}); err != nil {
			t.Fatalf("add item %d: %v", i, err)
		}
	}
	if _, err := f.d.AddCollectionItem(ctx, owner, firstShared.ID, denproto.CollectionItemRequest{MessageID: msgs[denproto.MaxCollectionItems].ID}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("201st item: %v", err)
	}

	// Reopening the den still unseals collection names cleanly.
	f.open()
	list, err := f.d.ListCollections(ctx, owner)
	if err != nil || len(list.Collections) != denproto.MaxChannelCollections || list.Collections[0].Name != "Shared 1" || list.Collections[0].Count != denproto.MaxCollectionItems {
		t.Fatalf("after reopen: len=%d err=%v", len(list.Collections), err)
	}

	// Author delete cascades collection item.
	if err := f.d.Delete(ctx, owner, msgs[0].ID); err != nil {
		t.Fatal(err)
	}
	items, err := f.d.CollectionItems(ctx, owner, firstShared.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != denproto.MaxCollectionItems-1 {
		t.Fatalf("after author delete: count=%d err=%v", items.Collection.Count, err)
	}

	// Staff delete of bob's message cascades item.
	bobMsg := sendText(t, f, bob, ch.ID, "bob post")
	if _, err := f.d.AddCollectionItem(ctx, bob, firstShared.ID, denproto.CollectionItemRequest{MessageID: bobMsg.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Delete(ctx, owner, bobMsg.ID); err != nil {
		t.Fatal(err)
	}
	items, err = f.d.CollectionItems(ctx, owner, firstShared.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != denproto.MaxCollectionItems-1 {
		t.Fatalf("after staff delete: count=%d err=%v", items.Collection.Count, err)
	}

	// Retention expiry cascades collection items.
	days := 1
	if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{Retention: &days}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(48 * time.Hour)
	f.d.expire()
	items, err = f.d.CollectionItems(ctx, owner, firstShared.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != 0 {
		t.Fatalf("after retention expiry: count=%d err=%v", items.Collection.Count, err)
	}

	// Ban with deleteRecent cascades bob's message items AND depart deletes bob's personal collections.
	bobMsg2 := sendText(t, f, bob, ch.ID, "recent bob post")
	if _, err := f.d.AddCollectionItem(ctx, bob, firstShared.ID, denproto.CollectionItemRequest{MessageID: bobMsg2.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.AddCollectionItem(ctx, bob, bobFirst.ID, denproto.CollectionItemRequest{MessageID: bobMsg2.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Remove(ctx, owner, denproto.FormatID(bob.MemberID), denproto.RemoveRequest{Ban: true, DeleteMessages: 3600}); err != nil {
		t.Fatal(err)
	}
	items, err = f.d.CollectionItems(ctx, owner, firstShared.ID, CollectionItemsQuery{})
	if err != nil || items.Collection.Count != 0 {
		t.Fatalf("after ban deleteRecent: count=%d err=%v", items.Collection.Count, err)
	}
	var bobCols int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_collections WHERE owner_id = ?`, bob.MemberID).Scan(&bobCols); err != nil || bobCols != 0 {
		t.Fatalf("depart left %d personal collections for bob, %v", bobCols, err)
	}

	// Deleting a channel cascades its shared collections.
	if _, err := f.d.CreateChannelCollection(ctx, owner, ch2.ID, denproto.CollectionRequest{Name: strPtr("Temp Col")}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.DeleteChannel(ctx, owner, ch2.ID); err != nil {
		t.Fatal(err)
	}
	var ch2Cols int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_collections WHERE channel_id = ?`, ch2.ID).Scan(&ch2Cols); err != nil || ch2Cols != 0 {
		t.Fatalf("channel delete left %d shared collections, %v", ch2Cols, err)
	}

	// Erase removes all collection rows cleanly.
	if err := f.d.Erase(ctx); err != nil {
		t.Fatalf("Erase: %v", err)
	}
	var left int
	if err := f.db.QueryRow(`SELECT (SELECT count(*) FROM den_collections) + (SELECT count(*) FROM den_collection_items)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("Erase left %d collection rows, %v", left, err)
	}
}

func TestCollectionNameFolding(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	ch, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("general")})
	if err != nil {
		t.Fatal(err)
	}

	// Shared collection: İ collides with i under FoldName (previously missed by EqualFold);
	// École collides with école; i\u0307 coexists with İ while I\u0307 collides with i\u0307.
	colI, err := f.d.CreateChannelCollection(ctx, owner, ch.ID, denproto.CollectionRequest{Name: strPtr("İ")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.CreateChannelCollection(ctx, owner, ch.ID, denproto.CollectionRequest{Name: strPtr("i")}); !denproto.IsCode(err, denproto.CodeCollectionNameTaken) {
		t.Fatalf("shared İ/i: %v", err)
	}
	if _, err := f.d.CreateChannelCollection(ctx, owner, ch.ID, denproto.CollectionRequest{Name: strPtr("i\u0307")}); err != nil {
		t.Fatalf("shared İ + i\\u0307: %v", err)
	}
	if _, err := f.d.CreateChannelCollection(ctx, owner, ch.ID, denproto.CollectionRequest{Name: strPtr("I\u0307")}); !denproto.IsCode(err, denproto.CodeCollectionNameTaken) {
		t.Fatalf("shared I\\u0307/i\\u0307: %v", err)
	}
	if _, err := f.d.UpdateCollection(ctx, owner, colI.ID, denproto.CollectionRequest{Name: strPtr("I\u0307")}); !denproto.IsCode(err, denproto.CodeCollectionNameTaken) {
		t.Fatalf("update collision İ->I\\u0307: %v", err)
	}

	// Personal collection follows the exact same folding rule.
	if _, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{Name: strPtr("École")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{Name: strPtr("école")}); !denproto.IsCode(err, denproto.CodeCollectionNameTaken) {
		t.Fatalf("personal École/école: %v", err)
	}
	if _, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{Name: strPtr("İ")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.CreatePersonalCollection(ctx, bob, denproto.CollectionRequest{Name: strPtr("i")}); !denproto.IsCode(err, denproto.CodeCollectionNameTaken) {
		t.Fatalf("personal İ/i: %v", err)
	}
}

func TestCollectionItemsPagination(t *testing.T) {
	f, owner, _ := chatFixture(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	f.d.SetClock(func() time.Time { return now })

	ch, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: strPtr("general")})
	if err != nil {
		t.Fatal(err)
	}
	col, err := f.d.CreateChannelCollection(ctx, owner, ch.ID, denproto.CollectionRequest{Name: strPtr("Big")})
	if err != nil {
		t.Fatal(err)
	}

	// 90 messages of ~12 KiB each (~1.08 MiB raw) exceed history's 896 KiB
	// page budget and the client's 1 MiB response cap. Add pairs at identical
	// timestamps so tie-breaking on message_id DESC is exercised too.
	const total = 90
	bigText := strings.Repeat("あ", 4000) // 4000 runes, 12000 bytes
	wantOrder := make([]string, total)
	for i := range total {
		if i%2 == 0 {
			now = now.Add(time.Second)
		}
		m := sendText(t, f, owner, ch.ID, bigText)
		if _, err := f.d.AddCollectionItem(ctx, owner, col.ID, denproto.CollectionItemRequest{MessageID: m.ID}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
		wantOrder[total-1-i] = m.ID
	}

	// Partial cursor is rejected.
	if _, err := f.d.CollectionItems(ctx, owner, col.ID, CollectionItemsQuery{BeforeAt: now.UnixMilli()}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("before_at without before_id: %v", err)
	}

	var got []string
	var q CollectionItemsQuery
	pages := 0
	for {
		page, err := f.d.CollectionItems(ctx, owner, col.ID, q)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		raw, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) >= denproto.MaxBody {
			t.Fatalf("page %d JSON size %d >= MaxBody %d", pages, len(raw), denproto.MaxBody)
		}
		if page.Collection.Count != total {
			t.Fatalf("page %d Collection.Count = %d, want %d", pages, page.Collection.Count, total)
		}
		for _, it := range page.Items {
			got = append(got, it.Message.ID)
		}
		if !page.HasMore {
			if page.NextBeforeID != "" || page.NextBeforeAt != 0 {
				t.Fatalf("final page carried cursor: at=%d id=%q", page.NextBeforeAt, page.NextBeforeID)
			}
			break
		}
		nextID, err := denproto.ParseID(page.NextBeforeID)
		if err != nil || page.NextBeforeAt <= 0 {
			t.Fatalf("page %d bad cursor: at=%d id=%q err=%v", pages, page.NextBeforeAt, page.NextBeforeID, err)
		}
		q = CollectionItemsQuery{BeforeAt: page.NextBeforeAt, BeforeID: nextID}
		if pages > 10 {
			t.Fatal("too many pages")
		}
	}
	if pages < 2 {
		t.Fatalf("expected multi-page split for oversized collection, got %d page", pages)
	}
	if !slices.Equal(got, wantOrder) {
		t.Fatalf("reassembled %d items (want %d), order mismatch", len(got), len(wantOrder))
	}
}
