package denclient_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// fourInAGroup makes a group of alice, bob, carol and dave, which each of
// their Dens holds.
func fourInAGroup(t *testing.T) (*denHost, []*denclient.Manager, []string, string, string) {
	t.Helper()
	h, ms, ids, denID := groupDen(t)
	g, err := ms[0].MakeGroup(context.Background(), denID, ids[1:], "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		groupOf(t, m, denID, g, ids)
	}
	return h, ms, ids, denID, g
}

// messageOn returns a message as a member's Dens shows it, from the
// channel's history, once cond holds of it.
func messageOn(t *testing.T, m *denclient.Manager, denID, channel, id, what string, cond func(denclient.PageMessage) bool) denclient.PageMessage {
	t.Helper()
	var got denclient.PageMessage
	eventually(t, what, func() bool {
		for _, msg := range history(t, m, denID, channel) {
			if msg.ID == id {
				got = msg
				return cond(msg)
			}
		}
		return false
	})
	return got
}

// A group's message opens as edited only by its author or a member they
// signed for: bob's tick and his edit after it, whom alice named, read as
// her message on carol's and dave's Dens; carol, whom only the den names,
// gets no edit from her own Dens, and one sealed past it reads as alice's
// to nobody, nor does a reply's quote of it. Once alice names carol in
// bob's place, carol's tick reads as hers, and bob's Dens edits no more.
func TestGroupEditorsTheAuthorSigned(t *testing.T) {
	h, ms, ids, denID, g := fourInAGroup(t)
	alice, bob, carol, dave := ms[0], ms[1], ms[2], ms[3]
	ctx := context.Background()
	list, err := alice.Send(ctx, denID, g, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] snacks\n[ ] drinks", Editors: []string{ids[1]}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.SetTask(ctx, denID, g, list.ID, 0, denproto.TaskRequest{Checked: true, Text: "snacks"}); err != nil {
		t.Fatalf("bob ticks: %v", err)
	}
	if _, err := bob.Edit(ctx, denID, g, list.ID, denproto.EditRequest{Revision: 2, Text: "[x] snacks\n[ ] drinks\n[ ] ice"}); err != nil {
		t.Fatalf("bob edits after his tick: %v", err)
	}
	for _, m := range []*denclient.Manager{carol, dave} {
		got := messageOn(t, m, denID, g, list.ID, "bob's edit", func(m denclient.PageMessage) bool { return m.Revision == 3 })
		if got.Locked != "" || got.Text != "[x] snacks\n[ ] drinks\n[ ] ice" || got.AuthorID != ids[0] || !slices.Equal(got.Editors, ids[1:2]) ||
			got.EditedBy != ids[1] {
			t.Errorf("bob's edit reads %+v", got)
		}
	}

	// The den names carol too.
	mid, _ := denproto.ParseID(list.ID)
	cid, _ := denproto.ParseID(ids[2])
	if _, err := h.s.db.Exec(`INSERT INTO den_message_editors (message_id, member_id) VALUES (?, ?)`, mid, cid); err != nil {
		t.Fatal(err)
	}
	if got := messageOn(t, carol, denID, g, list.ID, "the den's list", func(denclient.PageMessage) bool { return true }); slices.Contains(got.Editors, ids[2]) {
		t.Errorf("carol's Dens reads the den's editors: %+v", got.Editors)
	}
	const forged = "[x] snacks\n[x] drinks\n\nalice never wrote this"
	if _, err := carol.Edit(ctx, denID, g, list.ID, denproto.EditRequest{Revision: 3, Text: forged}); !isInput(err) {
		t.Fatalf("carol's Dens edits a message alice didn't name her for: %v", err)
	}
	if err := denclient.EditAs(carol, denID, g, list.ID, forged, true); err != nil {
		t.Fatalf("carol's edit past her Dens: %v", err)
	}
	reply := sendReply(t, dave, denID, g, list.ID, "about that list")
	for _, m := range []*denclient.Manager{alice, bob, dave} {
		got := messageOn(t, m, denID, g, list.ID, "carol's edit", func(m denclient.PageMessage) bool { return m.Revision == 4 })
		if got.Locked != "broken" || got.Text != "" || got.Waiting != nil || len(got.Editors) != 0 {
			t.Errorf("carol's edit, whom only the den named, reads %+v", got)
		}
		quote := messageOn(t, m, denID, g, reply.ID, "dave's reply", func(denclient.PageMessage) bool { return true })
		if quote.Reply == nil || !quote.Reply.Locked || strings.Contains(quote.Reply.Text, "never wrote") {
			t.Errorf("the quote of carol's edit reads %+v", quote.Reply)
		}
	}

	// alice names carol in bob's place, on a list of her own.
	second, err := alice.Send(ctx, denID, g, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] cake", Editors: []string{ids[1]}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alice.Edit(ctx, denID, g, second.ID, denproto.EditRequest{Revision: 1, Text: "[ ] cake", Editors: &[]string{ids[2]}}); err != nil {
		t.Fatal(err)
	}
	if _, err := carol.SetTask(ctx, denID, g, second.ID, 0, denproto.TaskRequest{Checked: true, Text: "cake"}); err != nil {
		t.Fatalf("carol ticks once alice named her: %v", err)
	}
	got := messageOn(t, dave, denID, g, second.ID, "carol's tick", func(m denclient.PageMessage) bool { return m.Revision == 3 })
	if got.Locked != "" || got.Text != "[x] cake" || !slices.Equal(got.Editors, ids[2:3]) {
		t.Errorf("carol's tick reads %+v", got)
	}
	if _, err := bob.SetTask(ctx, denID, g, second.ID, 0, denproto.TaskRequest{Checked: false, Text: "cake"}); !isInput(err) {
		t.Errorf("bob's Dens ticks once alice took him off: %v", err)
	}
}

// A group's message from before its author's Dens signed editors: one its
// author last signed opens, with no editors whatever the den says, for
// them to name again; one an editor last changed, as an older Dens seals
// every edit, opens for its author alone, as waiting, and for nobody else,
// nor its quote, until they keep it, which signs it as theirs with no
// editors. Nothing else of theirs signs it: an edit or a tick waits for
// Keep, and Keep of a message that doesn't wait, or of another revision
// than their page showed, signs nothing.
func TestGroupEditsFromBefore(t *testing.T) {
	h, ms, ids, denID, g := fourInAGroup(t)
	alice, bob, carol, dave := ms[0], ms[1], ms[2], ms[3]
	ctx := context.Background()

	keyID, nonce, sealed, err := denclient.SealAs(alice, denID, g, ids[0], "[ ] snacks\n[ ] drinks")
	if err != nil {
		t.Fatal(err)
	}
	aid, _ := denproto.ParseID(ids[0])
	old, err := h.d.Send(ctx, &den.Session{MemberID: aid, Version: denproto.Version}, g,
		denproto.SendRequest{Nonce: nonce, Sealed: sealed, KeyID: keyID, Editors: []string{ids[2]}})
	if err != nil {
		t.Fatal(err)
	}
	got := messageOn(t, bob, denID, g, old.ID, "alice's message from before", func(denclient.PageMessage) bool { return true })
	if got.Locked != "" || got.Text != "[ ] snacks\n[ ] drinks" || len(got.Editors) != 0 {
		t.Errorf("a message alice last signed, from before, reads %+v", got)
	}
	if _, err := carol.SetTask(ctx, denID, g, old.ID, 0, denproto.TaskRequest{Checked: true, Text: "snacks"}); !isInput(err) {
		t.Errorf("carol's Dens ticks a message from before, which names nobody: %v", err)
	}

	list, err := alice.Send(ctx, denID, g, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] snacks\n[ ] drinks", Editors: []string{ids[2]}})
	if err != nil {
		t.Fatal(err)
	}
	const ticked = "[ ] snacks\n[x] drinks"
	if err := denclient.EditAs(carol, denID, g, list.ID, ticked, false); err != nil {
		t.Fatalf("carol's tick as an older Dens seals it: %v", err)
	}
	reply := sendReply(t, dave, denID, g, list.ID, "drinks done")
	for _, m := range []*denclient.Manager{bob, dave} {
		got := messageOn(t, m, denID, g, list.ID, "carol's tick", func(m denclient.PageMessage) bool { return m.Revision == 2 })
		if got.Locked != "author" || got.Text != "" || got.Waiting != nil {
			t.Errorf("carol's tick from an older Dens reads %+v", got)
		}
	}
	waiting := messageOn(t, alice, denID, g, list.ID, "carol's tick", func(m denclient.PageMessage) bool { return m.Revision == 2 })
	if waiting.Locked != "author" || waiting.Text != "" || waiting.Waiting == nil || waiting.Waiting.By != ids[2] || waiting.Waiting.Text != ticked {
		t.Fatalf("carol's tick on alice's Dens: %+v %+v", waiting, waiting.Waiting)
	}
	for _, m := range []*denclient.Manager{alice, bob} {
		quote := messageOn(t, m, denID, g, reply.ID, "dave's reply", func(denclient.PageMessage) bool { return true })
		if quote.Reply == nil || !quote.Reply.Locked || quote.Reply.Text != "" {
			t.Errorf("the quote of carol's tick reads %+v", quote.Reply)
		}
	}
	if _, err := alice.SetTask(ctx, denID, g, list.ID, 0, denproto.TaskRequest{Checked: true, Text: "snacks"}); !isInput(err) {
		t.Errorf("alice's tick signed carol's: %v", err)
	}
	if _, err := alice.Edit(ctx, denID, g, list.ID, denproto.EditRequest{Revision: 2, Text: ticked}); !isInput(err) {
		t.Errorf("alice's edit signed carol's: %v", err)
	}
	shown := waiting.Waiting.Shown
	var conflict *denclient.ErrEditConflict
	if _, err := alice.Keep(ctx, denID, g, old.ID, shown); !errors.As(err, &conflict) || conflict.Current.Waiting != nil {
		t.Errorf("alice kept a message that doesn't wait: %v", err)
	}
	if _, err := alice.Keep(ctx, denID, g, list.ID, denproto.Random(len(shown))); !errors.As(err, &conflict) {
		t.Errorf("alice kept another revision than her page showed: %v", err)
	}
	if _, err := bob.Keep(ctx, denID, g, list.ID, shown); !isInput(err) {
		t.Errorf("bob kept alice's message: %v", err)
	}
	if _, err := alice.Keep(ctx, denID, g, list.ID, shown); err != nil {
		t.Fatalf("alice keeps carol's tick: %v", err)
	}
	for _, m := range []*denclient.Manager{alice, bob, dave} {
		got := messageOn(t, m, denID, g, list.ID, "alice's keep", func(m denclient.PageMessage) bool { return m.Revision == 3 })
		if got.Locked != "" || got.Text != ticked || got.AuthorID != ids[0] || len(got.Editors) != 0 || got.EditedBy != "" {
			t.Errorf("the message alice kept reads %+v", got)
		}
	}
}

// Keep signs only what its author's page showed. Where the den, with the
// member who changed a message, puts other words in the revision's place
// before the author keeps it, their Dens signs nothing and gives the
// message as it is now, for them to keep or not.
func TestGroupKeepSignsWhatWasShown(t *testing.T) {
	h, ms, ids, denID, g := fourInAGroup(t)
	alice, bob, carol := ms[0], ms[1], ms[2]
	ctx := context.Background()
	list, err := alice.Send(ctx, denID, g, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] snacks\n[ ] drinks", Editors: []string{ids[2]}})
	if err != nil {
		t.Fatal(err)
	}
	const ticked = "[ ] snacks\n[x] drinks"
	if err := denclient.EditAs(carol, denID, g, list.ID, ticked, false); err != nil {
		t.Fatal(err)
	}
	shown := messageOn(t, alice, denID, g, list.ID, "carol's tick", func(m denclient.PageMessage) bool { return m.Waiting != nil })
	if shown.Waiting.Text != ticked || len(shown.Waiting.Shown) == 0 {
		t.Fatalf("carol's tick on alice's Dens: %+v", shown.Waiting)
	}
	const forged = "alice never wrote this"
	sealed, err := denclient.SealRevisionAs(carol, denID, g, ids[0], list.Nonce, shown.Revision, forged)
	if err != nil {
		t.Fatal(err)
	}
	mid, _ := denproto.ParseID(list.ID)
	if _, err := h.s.db.Exec(`UPDATE den_messages SET text = ? WHERE id = ?`, []byte(sealed), mid); err != nil {
		t.Fatal(err)
	}
	var conflict *denclient.ErrEditConflict
	if _, err := alice.Keep(ctx, denID, g, list.ID, shown.Waiting.Shown); !errors.As(err, &conflict) {
		t.Fatalf("alice kept words her page never showed: %v", err)
	}
	now := conflict.Current.Waiting
	if now == nil || now.Text != forged || bytes.Equal(now.Shown, shown.Waiting.Shown) {
		t.Fatalf("the message as it is now: %+v", now)
	}
	got := messageOn(t, bob, denID, g, list.ID, "the message alice didn't keep", func(denclient.PageMessage) bool { return true })
	if got.Locked != "author" || got.Revision != shown.Revision {
		t.Errorf("after a Keep that signed nothing, bob reads %+v", got)
	}
	if _, err := alice.Keep(ctx, denID, g, list.ID, now.Shown); err != nil {
		t.Fatalf("alice keeps what her page shows now: %v", err)
	}
	got = messageOn(t, bob, denID, g, list.ID, "alice's keep", func(m denclient.PageMessage) bool { return m.Revision == shown.Revision+1 })
	if got.Locked != "" || got.Text != forged {
		t.Errorf("the message alice kept reads %+v", got)
	}
}

// sendReply sends a message in reply to another.
func sendReply(t *testing.T, m *denclient.Manager, denID, channel, to, text string) denclient.PageMessage {
	t.Helper()
	msg, err := m.Send(context.Background(), denID, channel, denproto.SendRequest{Nonce: denproto.Random(16), Text: text, ReplyTo: to})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// A group shows the name of its latest naming, whatever the den sends: an
// older name, none, or one a member sealed that no change of theirs
// carries.
func TestGroupNameFromItsChanges(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice, bob, carol := ms[0], ms[1], ms[2]
	ctx := context.Background()
	g, err := alice.MakeGroup(ctx, denID, ids[1:3], "Weekend")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[:3] {
		groupOf(t, m, denID, g, ids[:3])
	}
	if err := bob.NameGroup(ctx, denID, g, "Long weekend"); err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[:3] {
		viewOf(t, m, denID, "the rename", func(v denclient.View) bool { return v.GroupDMs[g].Name == "Long weekend" })
	}
	cid, _ := denproto.ParseID(g)
	var older []byte
	if err := h.s.db.QueryRow(`SELECT statement FROM den_group_changes WHERE channel_id = ? AND n = 1`, cid).Scan(&older); err != nil {
		t.Fatal(err)
	}
	var made denproto.GroupChange
	if err := json.Unmarshal(older, &made); err != nil || made.Name == nil {
		t.Fatalf("the group's making: %+v %v", made, err)
	}
	theirs, err := denclient.SealNameAs(carol, denID, g, "carol's pick")
	if err != nil {
		t.Fatal(err)
	}
	for what, name := range map[string]*denproto.SealedName{"an older name": made.Name, "none": nil, "a name no change carries": &theirs} {
		for _, m := range ms[1:3] {
			denSays(t, h, m, denID, g, func(c *denproto.Channel) { c.SealedName = name })
			if got := groupOf(t, m, denID, g, ids[:3]); got.Name != "Long weekend" {
				t.Errorf("with %s from the den, a member's Dens names the group %q", what, got.Name)
			}
		}
	}

	// dave, added since the naming, reads the name bob's key carries,
	// signed by bob; none the den keeps unsigned, nor one another member
	// signed for a key they didn't make.
	dave := ms[3]
	if err := bob.AddToGroup(ctx, denID, g, ids[3:4]); err != nil {
		t.Fatal(err)
	}
	carried := viewOf(t, dave, denID, "the name bob's key carries", func(v denclient.View) bool { return v.GroupDMs[g].Name == "Long weekend" })
	var sealed *denproto.SealedName
	for _, c := range carried.Channels {
		if c.ID == g {
			sealed = c.SealedName
		}
	}
	if sealed == nil || sealed.Sig == nil {
		t.Fatalf("the den sends the carried name as %+v", sealed)
	}
	unsigned := *sealed
	unsigned.Sig = nil
	// carol seals one under bob's key, which dave holds, and signs it.
	carols, err := denclient.SealNameAs(carol, denID, g, "carol's pick")
	if err != nil || !bytes.Equal(carols.KID, sealed.KID) {
		t.Fatalf("carol's name under bob's key: %+v %v", carols, err)
	}
	carols.Sig = carolSigns(t, carol, denID, g, carols)
	elsewhere := theirs
	elsewhere.Sig = carolSigns(t, carol, denID, g, theirs)
	for what, name := range map[string]*denproto.SealedName{"unsigned": &unsigned, "one signed by another than its key's maker": &carols,
		"one under a key dave doesn't hold": &elsewhere} {
		denSays(t, h, dave, denID, g, func(c *denproto.Channel) { c.SealedName = name })
		if got := groupOf(t, dave, denID, g, ids); got.Name != "" {
			t.Errorf("with %s from the den, dave's Dens names the group %q", what, got.Name)
		}
	}
	if err := carol.NameGroup(ctx, denID, g, "Longer weekend"); err != nil {
		t.Fatal(err)
	}
	viewOf(t, dave, denID, "carol's naming", func(v denclient.View) bool { return v.GroupDMs[g].Name == "Longer weekend" })
}

// denSays has the den tell a member's Dens a channel changed, as change
// says, and waits until their Dens took it: the den's word of another
// channel, after it, reaches their view.
func denSays(t *testing.T, h *denHost, m *denclient.Manager, denID, channel string, change func(*denproto.Channel)) {
	t.Helper()
	v, err := m.View(denID)
	if err != nil {
		t.Fatal(err)
	}
	var ch, other denproto.Channel
	for _, c := range v.Channels {
		switch {
		case c.ID == channel:
			ch = c
		case c.Kind == denproto.KindText:
			other = c
		}
	}
	if ch.ID == "" || other.ID == "" {
		t.Fatalf("no channel %s, or no text channel, in %+v", channel, v.Channels)
	}
	change(&ch)
	other.Description = denproto.FormatID(time.Now().UnixNano())
	mid, _ := denproto.ParseID(me(t, m, denID).ID)
	for _, c := range []denproto.Channel{ch, other} {
		if err := h.d.Hub.Publish(denproto.EventChannelUpdated, c, den.OnlyMember(mid)); err != nil {
			t.Fatal(err)
		}
	}
	viewOf(t, m, denID, "the den's word", func(v denclient.View) bool {
		return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == other.ID && c.Description == other.Description })
	})
}

// A den that hides a removal shows nobody the member removed as in the
// group again, whether it answers from before where a member's Dens stands
// or drops the group and lists it again from the member's start; and so
// across a restart, from the cache. A member removed and added again while
// away starts over from the add.
func TestGroupRolledBack(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice := ms[0]
	ctx := context.Background()
	s := newCachedStore(t)
	erin, stop := cachedGroupMember(t, h, alice, denID, s, "erin", ids)
	eid := me(t, erin, denID).ID
	members := []string{ids[0], eid, ids[2], ids[3]}
	makeOne := func() string {
		t.Helper()
		g, err := alice.MakeGroup(ctx, denID, members[1:], "")
		if err != nil {
			t.Fatal(err)
		}
		groupOf(t, erin, denID, g, members)
		if err := alice.RemoveFromGroup(ctx, denID, g, ids[3]); err != nil {
			t.Fatal(err)
		}
		groupOf(t, erin, denID, g, members[:3])
		return g
	}
	cid := func(g string) int64 { id, _ := denproto.ParseID(g); return id }
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := h.s.db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// hide takes the removal out of the den's changes.
	hide := func(g string) {
		exec(`PRAGMA foreign_keys = OFF`)
		exec(`DELETE FROM den_group_changes WHERE channel_id = ? AND n = 2`, cid(g))
	}
	notBack := func(g, when string) {
		t.Helper()
		got := viewOf(t, erin, denID, "the group broken "+when, func(v denclient.View) bool {
			gr, ok := v.GroupDMs[g]
			return ok && gr.Broken != ""
		}).GroupDMs[g]
		if slices.Contains(got.Members, ids[3]) {
			t.Errorf("%s, dave, removed, is in the group again on erin's Dens: %+v", when, got)
		}
	}

	// The den answers past where erin's Dens stands with the group's first
	// change.
	early := makeOne()
	stop()
	var first []byte
	if err := h.s.db.QueryRow(`SELECT statement FROM den_group_changes WHERE channel_id = ? AND n = 1`, cid(early)).Scan(&first); err != nil {
		t.Fatal(err)
	}
	hide(early)
	exec(`INSERT INTO den_group_changes (channel_id, n, made_by, made_at, statement) VALUES (?, 3, ?, 1, ?)`, cid(early), ids[0], first)
	h.restart()
	erin, stop = restartMember(t, h, s, stop)
	notBack(early, "answered from before")

	// The den drops the group from erin's list, then lists it again from
	// her start, the removal hidden.
	dropped := makeOne()
	exec(`DELETE FROM den_group_members WHERE channel_id = ? AND member_id = ?`, cid(dropped), eid)
	h.restart()
	viewOf(t, erin, denID, "the group dropped", func(v denclient.View) bool { _, ok := v.GroupDMs[dropped]; return !ok })
	stop()
	hide(dropped)
	exec(`INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (?, ?, 1, 1, 0)`, cid(dropped), eid)
	h.restart()
	erin, stop = restartMember(t, h, s, stop)
	notBack(dropped, "listed again")

	// Removed and added again while erin's Dens was away.
	again := makeOne()
	stop()
	if err := alice.RemoveFromGroup(ctx, denID, again, eid); err != nil {
		t.Fatal(err)
	}
	if err := alice.AddToGroup(ctx, denID, again, []string{eid}); err != nil {
		t.Fatal(err)
	}
	erin, stop = restartMember(t, h, s, stop)
	if got := groupOf(t, erin, denID, again, []string{ids[0], ids[2], eid}); got.Broken != "" {
		t.Errorf("added again while away, erin's Dens finds the group broken: %+v", got)
	}
}

// What a Dens keeps for good outlasts its cache. A den that says its
// changes are gone has the cache start again, and the state goes back in
// at once, so a Dens that stops before anything else changes still holds
// the den to the latest change it checked of a group: one it was dropped
// from, and listed in again from its start with a removal hidden, is
// broken.
func TestGroupMarkOutlastsItsCache(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice := ms[0]
	ctx := context.Background()
	s := newCachedStore(t)
	erin, stop := cachedGroupMember(t, h, alice, denID, s, "erin", ids)
	eid := me(t, erin, denID).ID
	members := []string{ids[0], eid, ids[2], ids[3]}
	g, err := alice.MakeGroup(ctx, denID, members[1:], "")
	if err != nil {
		t.Fatal(err)
	}
	groupOf(t, erin, denID, g, members)
	if err := alice.RemoveFromGroup(ctx, denID, g, ids[3]); err != nil {
		t.Fatal(err)
	}
	groupOf(t, erin, denID, g, members[:3])
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := h.s.db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cid, _ := denproto.ParseID(g)
	// The den drops the group from erin's list, which her Dens forgets but
	// for its latest change, then says its changes are gone, at a sync a
	// day brings.
	exec(`DELETE FROM den_group_members WHERE channel_id = ? AND member_id = ?`, cid, eid)
	h.restart()
	viewOf(t, erin, denID, "the group dropped", func(v denclient.View) bool { _, ok := v.GroupDMs[g]; return !ok })
	exec(`UPDATE den SET changes_floor = changes_floor + 1000000 WHERE id = 1`)
	if err := denclient.SyncCacheAgain(erin, denID); err != nil {
		t.Fatal(err)
	}
	// While her Dens is away, it lists the group again from her start, the
	// removal hidden.
	stop()
	exec(`PRAGMA foreign_keys = OFF`)
	exec(`DELETE FROM den_group_changes WHERE channel_id = ? AND n = 2`, cid)
	exec(`INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (?, ?, 1, 1, 0)`, cid, eid)
	h.restart()
	erin, stop = restartMember(t, h, s, stop)
	defer stop()
	got := viewOf(t, erin, denID, "the group listed again", func(v denclient.View) bool {
		gr, ok := v.GroupDMs[g]
		return ok && !gr.Syncing && (gr.Broken != "" || len(gr.Members) > 0)
	}).GroupDMs[g]
	if got.Broken == "" || slices.Contains(got.Members, ids[3]) {
		t.Errorf("listed again once the den had erin's cache start again, the group isn't broken, or dave, removed, is in it: %+v", got)
	}
}

// What a Dens keeps for good outlasts a kept state that doesn't read. A
// den can have one kept, as by dropping a group while its changes are
// fetched, which leaves a group with no channel, and at the next start the
// state goes, but for the kinds of channels and the groups' latest
// changes, kept alone at once: a Dens that stops again before the den
// answers still holds a group listed again from before a removal to its
// mark, and a channel gone to its kind.
func TestWhatsKeptOutlastsItsState(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice := ms[0]
	ctx := context.Background()
	s := newCachedStore(t)
	erin, stop := cachedGroupMember(t, h, alice, denID, s, "erin", ids)
	eid := me(t, erin, denID).ID
	members := []string{ids[0], eid, ids[2], ids[3]}
	g, err := alice.MakeGroup(ctx, denID, members[1:], "")
	if err != nil {
		t.Fatal(err)
	}
	groupOf(t, erin, denID, g, members)
	if err := alice.RemoveFromGroup(ctx, denID, g, ids[3]); err != nil {
		t.Fatal(err)
	}
	groupOf(t, erin, denID, g, members[:3])
	// A group erin was in and left for good, whose kind her Dens keeps.
	gone, err := alice.MakeGroup(ctx, denID, members[1:], "")
	if err != nil {
		t.Fatal(err)
	}
	groupOf(t, erin, denID, gone, members)
	if err := alice.RemoveFromGroup(ctx, denID, gone, eid); err != nil {
		t.Fatal(err)
	}
	viewOf(t, erin, denID, "the group she left gone", func(v denclient.View) bool { _, ok := v.GroupDMs[gone]; return !ok })
	// And one she's in all along, which the den shows as it is.
	held, err := alice.MakeGroup(ctx, denID, members[1:3], "Held")
	if err != nil {
		t.Fatal(err)
	}
	groupOf(t, erin, denID, held, members[:3])
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := h.s.db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cid, _ := denproto.ParseID(g)
	raw, _ := base64.RawURLEncoding.DecodeString(denID)
	kept := func() map[string]json.RawMessage {
		t.Helper()
		state, err := s.cache.State(raw)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]json.RawMessage
		if state != nil {
			if err := json.Unmarshal(state, &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	// The den drops the group from erin's list, and her Dens stops holding
	// a state for it, as one fetched while the den dropped it.
	exec(`DELETE FROM den_group_members WHERE channel_id = ? AND member_id = ?`, cid, eid)
	h.restart()
	viewOf(t, erin, denID, "the group dropped", func(v denclient.View) bool { _, ok := v.GroupDMs[g]; return !ok })
	stop()
	state := kept()
	var marks map[string]json.RawMessage
	if err := json.Unmarshal(state["group_marks"], &marks); err != nil || marks[g] == nil {
		t.Fatalf("erin's kept marks: %s, %v", state["group_marks"], err)
	}
	state["group_states"], _ = json.Marshal(map[string]json.RawMessage{g: marks[g]})
	out, _ := json.Marshal(state)
	if err := s.cache.SaveState(raw, out); err != nil {
		t.Fatal(err)
	}
	// While her Dens is away, the den lists the group again from her
	// start, the removal hidden.
	exec(`PRAGMA foreign_keys = OFF`)
	exec(`DELETE FROM den_group_changes WHERE channel_id = ? AND n = 2`, cid)
	exec(`INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (?, ?, 1, 1, 0)`, cid, eid)
	h.restart()

	// Her Dens starts where the den doesn't answer, and stops again.
	_, away := h.clientVia(t, s, noOwnDen, "127.0.0.1:1")
	listed := func(state map[string]json.RawMessage) int {
		var channels []json.RawMessage
		_ = json.Unmarshal(state["channels"], &channels)
		return len(channels)
	}
	eventually(t, "the state that doesn't read to go", func() bool { return listed(kept()) == 0 })
	away()
	state = kept()
	var kinds map[string]string
	marks = nil
	_ = json.Unmarshal(state["kinds"], &kinds)
	_ = json.Unmarshal(state["group_marks"], &marks)
	if kinds[gone] != denproto.KindGroupDM || marks[g] == nil || marks[held] == nil || listed(state) != 0 {
		t.Fatalf("after a state that didn't read, the cache keeps kinds %s and marks %s, with channels %s", state["kinds"], state["group_marks"], state["channels"])
	}

	erin, stop = h.clientFrom(t, s, noOwnDen)
	waitFor(t, erin, "erin connected again", connected)
	got := viewOf(t, erin, denID, "the group listed again", func(v denclient.View) bool {
		gr, ok := v.GroupDMs[g]
		return ok && !gr.Syncing && (gr.Broken != "" || len(gr.Members) > 0)
	}).GroupDMs[g]
	if got.Broken == "" || slices.Contains(got.Members, ids[3]) {
		t.Errorf("listed again after a state that didn't read, the group isn't broken, or dave, removed, is in it: %+v", got)
	}
	// A group the den shows as it was checked stands again, by its mark.
	if got := groupOf(t, erin, denID, held, members[:3]); got.Broken != "" || got.Name != "Held" {
		t.Errorf("a group the den shows as it is, after a state that didn't read: %+v", got)
	}
	stop()
	kinds = nil
	_ = json.Unmarshal(kept()["kinds"], &kinds)
	if kinds[gone] != denproto.KindGroupDM {
		t.Errorf("the kind of a group erin left didn't outlast the state: %v", kinds)
	}
}

// cachedGroupMember joins a member whose install keeps a cache, and waits
// until their Dens and the others' hold each other's identities.
func cachedGroupMember(t *testing.T, h *denHost, owner *denclient.Manager, denID string, s store, name string, others []string) (*denclient.Manager, func()) {
	t.Helper()
	invite, _, err := owner.CreateInvite(context.Background(), denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	m, stop := h.clientFrom(t, s, noOwnDen)
	join(t, m, invite, name)
	id := me(t, m, denID).ID
	viewOf(t, m, denID, "everyone's identity", func(v denclient.View) bool {
		for _, o := range append([]string{id}, others...) {
			if mm, ok := memberIn(v, o); !ok || mm.Identity == "" {
				return false
			}
		}
		return v.HasGroupDMs
	})
	viewOf(t, owner, denID, name+"'s identity", func(v denclient.View) bool { mm, ok := memberIn(v, id); return ok && mm.Identity != "" })
	return m, stop
}

// Typing in a group from someone its changes don't hold doesn't show.
func TestGroupTypingFromOutside(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice, bob := ms[0], ms[1]
	g, err := alice.MakeGroup(context.Background(), denID, ids[1:3], "")
	if err != nil {
		t.Fatal(err)
	}
	groupOf(t, bob, denID, g, ids[:3])
	stream, stop := bob.Stream()
	defer stop()
	bob.SetFocus("bob's page", denID, g)
	bid, _ := denproto.ParseID(ids[1])
	for _, who := range []string{ids[3], ids[2]} {
		if err := h.d.Hub.Publish(denproto.EventTyping, denproto.Typing{ChannelID: g, MemberID: who}, den.OnlyMember(bid)); err != nil {
			t.Fatal(err)
		}
	}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e := <-stream:
			for _, ev := range e.Events {
				var typing denproto.Typing
				if ev.T != denproto.EventTyping || json.Unmarshal(ev.D, &typing) != nil {
					continue
				}
				switch typing.MemberID {
				case ids[3]:
					t.Fatal("dave, outside the group, shows as typing there")
				case ids[2]:
					return
				}
			}
		case <-timeout:
			t.Fatal("carol's typing never came")
		}
	}
}

// A group's state as v1.15.0 kept it in the cache, its naming past the
// changes kept, is checked again from the member's start after an update,
// and named once it is; and the check holds to the latest change v1.15.0
// checked, so a den that hides it breaks the group.
func TestGroupStateFromBefore(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice, carol := ms[0], ms[2]
	ctx := context.Background()
	s := newCachedStore(t)
	erin, stop := cachedGroupMember(t, h, alice, denID, s, "erin", ids)
	eid := me(t, erin, denID).ID
	members := []string{ids[0], eid, ids[2]}
	g, err := alice.MakeGroup(ctx, denID, members[1:], "Weekend")
	if err != nil {
		t.Fatal(err)
	}
	if err := carol.NameGroup(ctx, denID, g, "Long weekend"); err != nil {
		t.Fatal(err)
	}
	viewOf(t, erin, denID, "the rename", func(v denclient.View) bool { return v.GroupDMs[g].Name == "Long weekend" })
	raw, _ := base64.RawURLEncoding.DecodeString(denID)
	// asBefore keeps erin's state as v1.15.0 did: no marks, and no name
	// with a group's state, whose changes the page shows end before its
	// naming.
	asBefore := func() {
		t.Helper()
		kept, err := s.cache.State(raw)
		if err != nil || kept == nil {
			t.Fatalf("erin's cached state: %v", err)
		}
		var state map[string]json.RawMessage
		if err := json.Unmarshal(kept, &state); err != nil {
			t.Fatal(err)
		}
		var groups map[string]map[string]json.RawMessage
		if err := json.Unmarshal(state["group_states"], &groups); err != nil {
			t.Fatal(err)
		}
		for _, gs := range groups {
			for _, field := range []string{"name", "named", "names", "recheck"} {
				delete(gs, field)
			}
			gs["changes"] = json.RawMessage(`[]`)
		}
		state["group_states"], _ = json.Marshal(groups)
		delete(state, "group_marks")
		out, _ := json.Marshal(state)
		if err := s.cache.SaveState(raw, out); err != nil {
			t.Fatal(err)
		}
	}
	stop()
	asBefore()
	erin, stop = restartMember(t, h, s, stop)
	if got := groupOf(t, erin, denID, g, members); got.Name != "Long weekend" || got.Broken != "" {
		t.Errorf("erin's group from before, checked again: %+v", got)
	}

	stop()
	asBefore()
	cid, _ := denproto.ParseID(g)
	if _, err := h.s.db.Exec(`DELETE FROM den_group_changes WHERE channel_id = ? AND n = 2`, cid); err != nil {
		t.Fatal(err)
	}
	h.restart()
	erin, _ = restartMember(t, h, s, stop)
	viewOf(t, erin, denID, "the group broken", func(v denclient.View) bool { return v.GroupDMs[g].Broken != "" })
}

// carolSigns signs a name as carol's identity would a key's.
func carolSigns(t *testing.T, carol *denclient.Manager, denID, g string, n denproto.SealedName) denproto.Bytes {
	t.Helper()
	sig, err := denclient.SignNameAs(carol, denID, g, n)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}
