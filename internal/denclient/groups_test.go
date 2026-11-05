package denclient_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// groupDen is a den with four installs whose Dens published identities:
// alice, its owner, bob, carol and dave. setup is bob's.
func groupDen(t *testing.T, setup ...func(*denclient.Manager)) (*denHost, []*denclient.Manager, []string, string) {
	t.Helper()
	h, owner, member, denID, _ := chatDen(t, setup...)
	ms := []*denclient.Manager{owner, member}
	for _, name := range []string{"carol", "dave"} {
		invite, _, err := owner.CreateInvite(context.Background(), denID, denproto.InviteCreateRequest{})
		if err != nil {
			t.Fatal(err)
		}
		m := h.client(t, noOwnDen)
		join(t, m, invite, name)
		ms = append(ms, m)
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = me(t, m, denID).ID
	}
	for _, m := range ms {
		viewOf(t, m, denID, "everyone's identity", func(v denclient.View) bool {
			for _, id := range ids {
				if o, ok := memberIn(v, id); !ok || o.Identity == "" {
					return false
				}
			}
			return v.HasGroupDMs
		})
	}
	return h, ms, ids, denID
}

// groupOf waits until a member's Dens holds a group with its members as
// want says, and returns it.
func groupOf(t *testing.T, m *denclient.Manager, denID, id string, want []string) denclient.PageGroup {
	t.Helper()
	return viewOf(t, m, denID, "the group's members", func(v denclient.View) bool {
		g, ok := v.GroupDMs[id]
		return ok && slices.Equal(g.Members, want) && !g.Syncing
	}).GroupDMs[id]
}

// A group's life across its members' Dens: made with a name, which each
// opens, written in and reacted to; a member added reads from their joining
// on; one removed loses it and reads nothing written after; and each
// change shows as a line, a member as its changes make them.
func TestGroupDMsBetweenInstalls(t *testing.T) {
	_, ms, ids, denID := groupDen(t)
	alice, bob, carol, dave := ms[0], ms[1], ms[2], ms[3]
	ctx := context.Background()
	if _, err := alice.MakeGroup(ctx, denID, []string{ids[1]}, ""); err == nil {
		t.Fatal("a group of two was made")
	}
	g, err := alice.MakeGroup(ctx, denID, []string{ids[1], ids[2]}, "Weekend")
	if err != nil {
		t.Fatalf("make a group: %v", err)
	}
	for _, m := range ms[:3] {
		groupOf(t, m, denID, g, ids[:3])
		// The name opens once the group's key comes, after its change.
		got := viewOf(t, m, denID, "the group's name", func(v denclient.View) bool { return v.GroupDMs[g].Name == "Weekend" }).GroupDMs[g]
		if len(got.Changes) != 1 || got.Changes[0].Kind != denproto.ChangeMade || got.Changes[0].By != ids[0] ||
			got.Changes[0].Name == nil || *got.Changes[0].Name != "Weekend" {
			t.Errorf("a member's Dens holds %+v", got)
		}
	}
	if v, _ := dave.View(denID); slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == g }) {
		t.Error("dave, outside the group, sees it")
	}

	first := sendDM(t, bob, denID, g, "hello all")
	if first.Locked != "" {
		t.Fatalf("bob's message: %+v", first)
	}
	got := history(t, carol, denID, g)
	if len(got) != 1 || got[0].Text != "hello all" || got[0].Locked != "" {
		t.Fatalf("carol reads %+v", got)
	}
	set, err := carol.React(ctx, denID, g, first.ID, denclient.PageReaction{Emoji: "👍", On: true})
	if err != nil || set == nil || !slices.Equal(set.Emoji, []string{"👍"}) {
		t.Fatalf("carol reacts: %+v, %v", set, err)
	}
	viewMessage := func(m *denclient.Manager, id string) denclient.PageMessage {
		for _, msg := range history(t, m, denID, g) {
			if msg.ID == id {
				return msg
			}
		}
		return denclient.PageMessage{}
	}
	if msg := viewMessage(alice, first.ID); len(msg.DMReactions) != 1 || msg.DMReactions[0].MemberID != ids[2] {
		t.Errorf("alice sees reactions %+v", msg.DMReactions)
	}
	// An edit is signed by its author, and a tick by the member who made
	// it, one the author named, though the den doesn't mark it edited.
	if _, err := bob.Edit(ctx, denID, g, first.ID, denproto.EditRequest{Revision: first.Revision, Text: "hello everyone"}); err != nil {
		t.Fatalf("bob edits: %v", err)
	}
	if msg := viewMessage(alice, first.ID); msg.Text != "hello everyone" || msg.Locked != "" {
		t.Errorf("alice reads bob's edit as %+v", msg)
	}
	list, err := bob.Send(ctx, denID, g, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] snacks\n[ ] drinks", Editors: []string{ids[2]}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := carol.SetTask(ctx, denID, g, list.ID, 1, denproto.TaskRequest{Checked: true, Text: "drinks"}); err != nil {
		t.Fatalf("carol ticks a task: %v", err)
	}
	if msg := viewMessage(alice, list.ID); msg.Text != "[ ] snacks\n[x] drinks" || msg.Locked != "" {
		t.Errorf("alice reads carol's tick as %+v", msg)
	}
	if _, err := alice.SetTask(ctx, denID, g, list.ID, 0, denproto.TaskRequest{Checked: true, Text: "snacks"}); err == nil {
		t.Error("alice, whom bob didn't name, ticked a task")
	}

	if err := bob.AddToGroup(ctx, denID, g, []string{ids[3]}); err != nil {
		t.Fatalf("bob adds dave: %v", err)
	}
	for _, m := range ms {
		groupOf(t, m, denID, g, ids)
	}
	if got := history(t, dave, denID, g); len(got) != 0 {
		t.Errorf("dave reads what came before he joined: %+v", got)
	}
	sendDM(t, carol, denID, g, "welcome, dave")
	if got := history(t, dave, denID, g); len(got) != 1 || got[0].Text != "welcome, dave" {
		t.Errorf("dave reads %+v", got)
	}

	if err := bob.RemoveFromGroup(ctx, denID, g, ids[2]); err == nil {
		t.Error("bob, who didn't make the group, removed carol")
	}
	if err := alice.RemoveFromGroup(ctx, denID, g, ids[2]); err != nil {
		t.Fatalf("alice removes carol: %v", err)
	}
	viewOf(t, carol, denID, "the group gone", func(v denclient.View) bool {
		_, ok := v.GroupDMs[g]
		return !ok
	})
	after := sendDM(t, dave, denID, g, "after carol")
	if after.Locked != "" {
		t.Fatalf("dave's message: %+v", after)
	}
	if _, err := carol.History(ctx, denID, g, denclient.HistoryQuery{}); err == nil {
		t.Error("carol reads the group she was removed from")
	}
	shown := groupOf(t, bob, denID, g, []string{ids[0], ids[1], ids[3]})
	kinds := make([]string, len(shown.Changes))
	for i, c := range shown.Changes {
		kinds[i] = c.Kind
	}
	if !slices.Equal(kinds, []string{denproto.ChangeMade, denproto.ChangeAdded, denproto.ChangeRemoved}) {
		t.Errorf("bob's lines are %v", kinds)
	}

	if err := dave.NameGroup(ctx, denID, g, "Long weekend"); err != nil {
		t.Fatalf("dave names the group: %v", err)
	}
	viewOf(t, alice, denID, "the new name", func(v denclient.View) bool { return v.GroupDMs[g].Name == "Long weekend" })
	if err := bob.LeaveGroup(ctx, denID, g); err != nil {
		t.Fatalf("bob leaves: %v", err)
	}
	groupOf(t, alice, denID, g, []string{ids[0], ids[3]})
	// The next message makes a key for those left.
	if msg := sendDM(t, alice, denID, g, "just us"); msg.Locked != "" {
		t.Errorf("alice's message after bob left: %+v", msg)
	}
	if got := history(t, dave, denID, g); len(got) != 3 || got[2].Text != "just us" || got[2].Locked != "" {
		t.Errorf("dave reads %+v", got)
	}
}

// An add the den makes up, signed by no member, is refused by every
// member's Dens: the group shows as broken, the one it adds shows nowhere,
// and nothing more is sent there.
func TestGroupRefusesAMadeUpAdd(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice, bob := ms[0], ms[1]
	ctx := context.Background()
	g, err := alice.MakeGroup(ctx, denID, []string{ids[1], ids[2]}, "")
	if err != nil {
		t.Fatal(err)
	}
	groupOf(t, bob, denID, g, ids[:3])
	// The den writes an add of dave, as if bob signed it with a key of the
	// den's own, and tells the group's Dens there's a change.
	var statement []byte
	if err := h.s.db.QueryRow(`SELECT statement FROM den_group_changes WHERE channel_id = ? ORDER BY n DESC LIMIT 1`, g).Scan(&statement); err != nil {
		t.Fatal(err)
	}
	var last denproto.GroupChange
	if err := json.Unmarshal(statement, &last); err != nil {
		t.Fatal(err)
	}
	info, _ := h.d.Info()
	cid, _ := denproto.ParseID(g)
	prev, err := last.Hash(info.ID, cid)
	if err != nil {
		t.Fatal(err)
	}
	fake, err := denproto.DeriveIdentity(denproto.Random(denproto.SealSize), info.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	add := denproto.GroupChange{N: last.N + 1, Prev: prev, Kind: denproto.ChangeAdded, By: ids[1], Subjects: []string{ids[3]},
		After: []string{ids[0], ids[1], ids[2], ids[3]}}
	maker := fake.Maker()
	add.Signer = &maker
	if add.Signature, err = fake.SignChange(info.ID, cid, add); err != nil {
		t.Fatal(err)
	}
	forged, _ := json.Marshal(add)
	if _, err := h.s.db.Exec(`INSERT INTO den_group_changes (channel_id, n, made_by, made_at, statement) VALUES (?, ?, ?, 1, ?)`,
		g, add.N, ids[1], forged); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.db.Exec(`INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (?, ?, 9, ?, 0)`,
		g, ids[3], add.N); err != nil {
		t.Fatal(err)
	}
	h.restart()
	for _, m := range ms[:3] {
		got := viewOf(t, m, denID, "the group broken", func(v denclient.View) bool { return v.GroupDMs[g].Broken != "" }).GroupDMs[g]
		if slices.Contains(got.Members, ids[3]) {
			t.Errorf("a member's Dens shows dave in the group: %+v", got)
		}
	}
	if _, err := alice.Send(ctx, denID, g, denproto.SendRequest{Nonce: denproto.Random(16), Text: "to the den's add"}); err == nil {
		t.Error("alice sent to a group whose changes don't check")
	}
}

// A member adds someone straight after making a group, before its events
// reach their Dens: the change syncs the group's changes first.
func TestGroupChangedAtOnce(t *testing.T) {
	_, ms, ids, denID := groupDen(t)
	ctx := context.Background()
	g, err := ms[0].MakeGroup(ctx, denID, []string{ids[1], ids[2]}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ms[0].AddToGroup(ctx, denID, g, []string{ids[3]}); err != nil {
		t.Fatalf("an add straight after making the group: %v", err)
	}
	for _, m := range ms {
		groupOf(t, m, denID, g, ids)
	}
}

// A member who started over still checks the changes to their groups they
// made before, signed by the identity they had then, which their record
// holds: on the device that started over, and on a device of theirs that
// signs in after, which checks the group from its start. What they write
// after goes under a key sealed to their new identity.
func TestGroupAfterStartingOver(t *testing.T) {
	h, ms, ids, denID := groupDen(t, shortRecord)
	alice, bob := ms[0], ms[1]
	ctx := context.Background()
	g, err := alice.MakeGroup(ctx, denID, []string{ids[1], ids[2]}, "")
	if err != nil {
		t.Fatal(err)
	}
	groupOf(t, bob, denID, g, ids[:3])
	if err := bob.AddToGroup(ctx, denID, g, []string{ids[3]}); err != nil {
		t.Fatal(err)
	}
	groupOf(t, bob, denID, g, ids)
	if _, err := bob.StartOver(ctx, denID, "correct horse"); err != nil {
		t.Fatal(err)
	}
	viewOf(t, alice, denID, "bob's keys changed", func(v denclient.View) bool { return len(v.Identities[ids[1]].Changes) == 1 })
	if msg := sendDM(t, bob, denID, g, "after starting over"); msg.Locked != "" {
		t.Fatalf("bob's message after starting over: %+v", msg)
	}
	viewOf(t, alice, denID, "bob's message", func(denclient.View) bool {
		got := history(t, alice, denID, g)
		return len(got) == 1 && got[0].Text == "after starting over" && got[0].Locked == ""
	})
	laptop := h.client(t, noOwnDen, shortRecord)
	signInApproved(t, laptop, bob, denID, denclient.SignInRequest{Den: "https://example.com", Username: "bob", Password: "correct horse"}, false)
	waitFor(t, laptop, "the laptop connected", connected)
	if got := groupOf(t, laptop, denID, g, ids); got.Broken != "" {
		t.Fatalf("bob's laptop finds the group broken: %+v", got)
	}
	if got := history(t, laptop, denID, g); len(got) != 1 || got[0].Text != "after starting over" || got[0].Locked != "" {
		t.Errorf("bob's laptop reads %+v", got)
	}
}
