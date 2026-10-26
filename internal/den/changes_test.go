package den

import (
	"bytes"
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// changes asks for what changed since a version as a member, and fails
// the test on an error.
func (f *fixture) changes(s *Session, since, upto int64) denproto.Changes {
	f.t.Helper()
	c, err := f.d.Changes(context.Background(), s, since, upto)
	if err != nil {
		f.t.Fatal(err)
	}
	if c.Messages == nil || c.Deleted == nil {
		f.t.Fatalf("changes lists aren't arrays: %+v", c)
	}
	return c
}

// sync is where the den's changes stand, as ready has it for a member.
func (f *fixture) sync(s *Session) denproto.Sync {
	f.t.Helper()
	r, err := f.d.snapshot(context.Background(), s.MemberID, IsStaff(s.Role), 0, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := denproto.CheckSync(r.Sync); err != nil {
		f.t.Fatal(err)
	}
	return r.Sync
}

// changed lists the IDs a page of changes names, edited and deleted.
func changed(c denproto.Changes) (edited, deleted []string) {
	for _, m := range c.Messages {
		edited = append(edited, m.ID)
	}
	for _, d := range c.Deleted {
		deleted = append(deleted, d.ID)
	}
	return edited, deleted
}

func id(s string) int64 {
	n, _ := denproto.ParseID(s)
	return n
}

func TestChangesLogEditsTicksAndDeletes(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	// The two joins were the den's first changes (M6.2).
	start := f.sync(member)
	if start.Version != 2 || start.Floor != 0 || start.Expired != "" {
		t.Fatalf("a new den's sync: %+v", start)
	}
	a := f.send(member, general.ID, "first")
	b := f.send(member, general.ID, "[ ] milk\n[ ] eggs")
	c := f.send(owner, general.ID, "going")
	d := f.send(owner, general.ID, "staying")
	// Sending isn't a change: new messages come as history.
	if v := f.sync(member).Version; v != start.Version {
		t.Fatalf("sending moved the version to %d", v)
	}

	if _, err := f.d.Edit(ctx, member, a.ID, denproto.EditRequest{Revision: 1, Text: "first, edited"}); err != nil {
		t.Fatal(err)
	}
	afterEdit := f.sync(member).Version
	if _, err := f.d.SetTask(ctx, member, b.ID, 1, denproto.TaskRequest{Checked: true, Text: "eggs"}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Delete(ctx, owner, c.ID); err != nil {
		t.Fatal(err)
	}
	// A second edit takes the place of the first, at a newer version.
	if _, err := f.d.Edit(ctx, member, a.ID, denproto.EditRequest{Revision: 2, Text: "first, edited twice"}); err != nil {
		t.Fatal(err)
	}
	now := f.sync(member)
	if now.Version != afterEdit+3 {
		t.Fatalf("four changes took the version from 0 to %d", now.Version)
	}

	all := f.changes(member, start.Version, 0)
	edited, deleted := changed(all)
	if !slices.Equal(edited, []string{b.ID, a.ID}) || !slices.Equal(deleted, []string{c.ID}) || all.Version != now.Version || all.More {
		t.Fatalf("changes since 0: edited %v, deleted %v, %+v", edited, deleted, all)
	}
	if all.Messages[1].Text != "first, edited twice" || all.Messages[1].Revision != 3 || all.Messages[0].Text != "[ ] milk\n[x] eggs" {
		t.Fatalf("changed messages as they are now: %+v", all.Messages)
	}
	if all.Deleted[0].ChannelID != general.ID {
		t.Fatalf("a deleted message's channel: %+v", all.Deleted[0])
	}

	// Since a version, only what changed after it; up to a message, only
	// changes to messages up to it.
	if e, x := changed(f.changes(member, afterEdit, 0)); !slices.Equal(e, []string{b.ID, a.ID}) || !slices.Equal(x, []string{c.ID}) {
		t.Fatalf("changes since %d: %v %v", afterEdit, e, x)
	}
	if e, x := changed(f.changes(member, start.Version, id(b.ID))); !slices.Equal(e, []string{b.ID, a.ID}) || len(x) != 0 {
		t.Fatalf("changes up to %s: %v %v", b.ID, e, x)
	}
	if e, x := changed(f.changes(member, now.Version, 0)); len(e)+len(x) != 0 {
		t.Fatalf("changes since now: %v %v", e, x)
	}
	if _, err := f.d.Changes(ctx, member, now.Version+1, 0); !denproto.IsCode(err, denproto.CodeChangesGone) {
		t.Fatalf("changes since a version the den hasn't reached: %v", err)
	}
	_ = d
}

func TestChangesSeeWhatTheMemberSeesNow(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	staffOnly := f.newChannel(owner, "staff", denproto.ChannelRequest{StaffOnly: ptr(true)})
	doomed := f.newChannel(owner, "doomed", denproto.ChannelRequest{})
	carol := f.member(owner, "carol")
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: denproto.FormatID(carol.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	secret := f.send(owner, staffOnly.ID, "staff only")
	gone := f.send(owner, doomed.ID, "in a channel that goes")
	private := f.send(carol, dm.ID, "between the two")
	for _, m := range []denproto.Message{secret, gone} {
		if _, err := f.d.Edit(ctx, owner, m.ID, denproto.EditRequest{Revision: 1, Text: "edited"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.d.Delete(ctx, carol, private.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.DeleteChannel(ctx, owner, doomed.ID); err != nil {
		t.Fatal(err)
	}

	// A deleted channel's changes go to nobody; a staff-only channel's to
	// staff; a DM's to its two members.
	if e, x := changed(f.changes(owner, 0, 0)); !slices.Equal(e, []string{secret.ID}) || len(x) != 0 {
		t.Fatalf("the owner's changes: %v %v", e, x)
	}
	if e, x := changed(f.changes(member, 0, 0)); len(e) != 0 || !slices.Equal(x, []string{private.ID}) {
		t.Fatalf("the member's changes: %v %v", e, x)
	}
	if e, x := changed(f.changes(carol, 0, 0)); len(e) != 0 || !slices.Equal(x, []string{private.ID}) {
		t.Fatalf("carol's changes: %v %v", e, x)
	}

	// What the member sees now decides, not what they saw at the change.
	if _, err := f.d.UpdateChannel(ctx, owner, staffOnly.ID, denproto.ChannelRequest{StaffOnly: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if e, _ := changed(f.changes(member, 0, 0)); !slices.Equal(e, []string{secret.ID}) {
		t.Fatalf("the member's changes once the channel is open to them: %v", e)
	}
}

func TestBansLogTheirDeletesWhileRetentionAndDeletedChannelsDont(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	other := f.newChannel(owner, "other", denproto.ChannelRequest{})
	carol := f.member(owner, "carol")
	spam := []denproto.Message{f.send(carol, general.ID, "spam"), f.send(carol, other.ID, "more spam")}
	f.send(member, other.ID, "in a channel about to go")
	before := f.sync(owner).Version
	if err := f.d.Remove(ctx, owner, denproto.FormatID(carol.MemberID), denproto.RemoveRequest{Ban: true, DeleteMessages: 3600}); err != nil {
		t.Fatal(err)
	}
	if e, x := changed(f.changes(member, before, 0)); len(e) != 0 || !slices.Equal(x, []string{spam[0].ID, spam[1].ID}) {
		t.Fatalf("a ban's deletes: %v %v", e, x)
	}

	afterBan := f.sync(owner).Version
	if err := f.d.DeleteChannel(ctx, owner, other.ID); err != nil {
		t.Fatal(err)
	}
	old := f.send(member, general.ID, "old")
	f.clock = f.clock.Add(48 * time.Hour)
	f.send(member, general.ID, "young")
	f.setRetention(owner, 1)
	f.d.expire()
	s := f.sync(member)
	if s.Version != afterBan || s.Expired != old.ID {
		t.Fatalf("after a deleted channel and a retention pass: %+v, want version %d and expired %s", s, afterBan, old.ID)
	}
}

func TestPruningMovesTheFloor(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	old := f.send(member, general.ID, "old")
	young := f.send(member, general.ID, "young")
	if _, err := f.d.Edit(ctx, member, old.ID, denproto.EditRequest{Revision: 1, Text: "old, edited"}); err != nil {
		t.Fatal(err)
	}
	oldVersion := f.sync(member).Version
	f.clock = f.clock.Add(changeLogAge - time.Hour)
	if _, err := f.d.Edit(ctx, member, young.ID, denproto.EditRequest{Revision: 1, Text: "young, edited"}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(2 * time.Hour)
	if err := f.d.pruneChanges(ctx); err != nil {
		t.Fatal(err)
	}
	s := f.sync(member)
	if s.Floor != oldVersion || s.Version != oldVersion+1 {
		t.Fatalf("after pruning a change of %d: %+v", oldVersion, s)
	}
	if _, err := f.d.Changes(ctx, member, oldVersion-1, 0); !denproto.IsCode(err, denproto.CodeChangesGone) {
		t.Fatalf("changes since below the floor: %v", err)
	}
	if e, _ := changed(f.changes(member, s.Floor, 0)); !slices.Equal(e, []string{young.ID}) {
		t.Fatalf("changes since the floor: %v", e)
	}

	// Once every change is old, the floor reaches the version, and stays.
	f.clock = f.clock.Add(changeLogAge)
	if err := f.d.pruneChanges(ctx); err != nil {
		t.Fatal(err)
	}
	if s := f.sync(member); s.Floor != s.Version || s.Version != oldVersion+1 {
		t.Fatalf("after pruning every change: %+v", s)
	}
	if err := f.d.pruneChanges(ctx); err != nil {
		t.Fatal(err)
	}
	if s := f.sync(member); s.Floor != oldVersion+1 {
		t.Fatalf("pruning again moved the floor: %+v", s)
	}
}

func TestChangesComeInPages(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.seed(general, member, denproto.ChangesPage+5)
	var ids []int64
	rows, err := f.db.Query(`SELECT id FROM den_messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n int64
		rows.Scan(&n)
		ids = append(ids, n)
	}
	rows.Close()
	cid := id(general.ID)
	err = f.d.tx(ctx, func(tx *sql.Tx) error {
		for _, n := range ids {
			if err := logMessage(ctx, tx, n, cid, f.clock); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The members' joins came first, as changes a page of messages' skips.
	joins := int64(2)
	first := f.changes(member, 0, 0)
	if !first.More || len(first.Messages) != denproto.ChangesPage || first.Version != joins+int64(denproto.ChangesPage) {
		t.Fatalf("the first page: more %v, %d messages, version %d", first.More, len(first.Messages), first.Version)
	}
	rest := f.changes(member, first.Version, 0)
	if rest.More || len(rest.Messages) != 5 || rest.Version != joins+int64(len(ids)) {
		t.Fatalf("the second page: more %v, %d messages, version %d", rest.More, len(rest.Messages), rest.Version)
	}

	// Long messages fill a page before its count does, and the next page
	// starts where it stopped.
	// Four bytes a character, 16,000 a message: 100 pass a page's budget.
	long := strings.Repeat("𝄞", denproto.MaxTextRunes)
	err = f.d.tx(ctx, func(tx *sql.Tx) error {
		for _, n := range ids[:100] {
			sealed, err := f.v.Seal([]byte(long), textAD(n))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE den_messages SET text = ? WHERE id = ?`, sealed, n); err != nil {
				return err
			}
			if err := logMessage(ctx, tx, n, cid, f.clock); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	since, seen := rest.Version, 0
	for pages := 0; ; pages++ {
		page := f.changes(member, since, 0)
		if len(page.Messages) == 0 && page.More {
			t.Fatalf("a page with nothing in it says there's more: %+v", page)
		}
		seen += len(page.Messages)
		since = page.Version
		if !page.More {
			if pages == 0 {
				t.Fatal("100 long messages fit one page")
			}
			break
		}
	}
	if seen != 100 {
		t.Fatalf("the pages held %d long messages, not 100", seen)
	}
}

func TestNewSyncEpoch(t *testing.T) {
	f, _, member := chatFixture(t)
	before := f.sync(member)
	if err := f.d.NewSyncEpoch(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := f.sync(member)
	if bytes.Equal(before.Epoch, after.Epoch) || after.Version != before.Version {
		t.Fatalf("a new epoch: %+v, before %+v", after, before)
	}
	// It survives the den opening again, as after a restart.
	f.open()
	if again := f.sync(member); !bytes.Equal(again.Epoch, after.Epoch) {
		t.Fatalf("the epoch after opening again: %x, want %x", again.Epoch, after.Epoch)
	}
}

// A restore moves the den's ID counters on (M11): a backup's are where
// they were when it was made, and nothing made after the restore may take
// the ID of what the backup lacks, which a link elsewhere may name.
func TestAdvanceIDs(t *testing.T) {
	f, owner, _ := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.send(owner, c.ID, "in the backup")
	lost := f.send(owner, c.ID, "after the backup")
	// What the backup holds: not the second message, and the counter where
	// it was before it.
	id, _ := denproto.ParseID(lost.ID)
	for _, q := range []string{`DELETE FROM den_messages WHERE id = ?`, `UPDATE sqlite_sequence SET seq = ? - 1 WHERE name = 'den_messages'`} {
		if _, err := f.db.ExecContext(ctx, q, id); err != nil {
			t.Fatal(err)
		}
	}
	seq := func(table string) int64 {
		t.Helper()
		var n int64
		if err := f.db.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = ?`, table).Scan(&n); err != nil {
			t.Fatalf("%s's counter: %v", table, err)
		}
		return n
	}
	version := seq("den_changes")
	if err := f.d.AdvanceIDs(ctx); err != nil {
		t.Fatal(err)
	}
	at := f.clock.UnixMicro()
	if next := f.send(owner, c.ID, "after the restore"); next.ID != denproto.FormatID(at+1) {
		t.Fatalf("the next message's ID is %s, want %d", next.ID, at+1)
	}
	if next := f.newChannel(owner, "later", denproto.ChannelRequest{}); next.ID != denproto.FormatID(at+1) {
		t.Fatalf("the next channel's ID is %s, want %d", next.ID, at+1)
	}
	// Every counter moves, one the backup had no row for yet too, as for
	// DM keys here; the change log's counts versions, which a new sync
	// epoch starts again.
	for _, table := range []string{"den_members", "den_invites", "den_groups", "den_files", "den_dm_keys"} {
		if got := seq(table); got != at {
			t.Errorf("%s's counter is %d, want %d", table, got, at)
		}
	}
	if got := seq("den_changes"); got != version {
		t.Errorf("the change log's counter moved from %d to %d", version, got)
	}
	// The service starting again before the restore is done does it again,
	// and no counter moves back, whatever the clock says.
	f.clock = f.clock.Add(-time.Hour)
	if err := f.d.AdvanceIDs(ctx); err != nil {
		t.Fatal(err)
	}
	if got := seq("den_members"); got != at {
		t.Fatalf("den_members' counter after a second pass: %d, want %d", got, at)
	}
}

// members lists the usernames a snapshot sends, with the cursor it took.
func (f *fixture) membersSince(s *Session, mc *membersCursor) ([]string, *int64) {
	f.t.Helper()
	r, err := f.d.snapshot(context.Background(), s.MemberID, IsStaff(s.Role), 0, mc)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := denproto.CheckSync(r.Sync); err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, m := range r.Members {
		names = append(names, m.Username)
	}
	slices.Sort(names)
	return names, r.Sync.MembersSince
}

func TestReadySendsOnlyTheMembersChangedSince(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	dave := f.member(owner, "dave")
	start := f.sync(member)
	cursor := &membersCursor{epoch: start.Epoch, version: start.Version}
	if names, since := f.membersSince(member, cursor); len(names) != 0 || since == nil || *since != start.Version {
		t.Fatalf("with nothing changed: %v since %v", names, since)
	}

	// A join, a new display name, a role and a departure each send their
	// member again; nobody else's record goes.
	f.member(owner, "erin")
	name := "Carol C"
	if _, err := f.d.UpdateProfile(ctx, carol, denproto.ProfileRequest{DisplayName: &name}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.SetRole(ctx, owner, denproto.FormatID(dave.MemberID), denproto.RoleRequest{Role: denproto.RoleModerator}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Leave(ctx, member); err != nil {
		t.Fatal(err)
	}
	names, since := f.membersSince(owner, cursor)
	if !slices.Equal(names, []string{"bob", "carol", "dave", "erin"}) || since == nil || *since != start.Version {
		t.Fatalf("members changed since %d: %v (since %v)", start.Version, names, since)
	}

	// A cursor of another epoch, past the version, or below the floor gets
	// every member.
	all := []string{"alice", "bob", "carol", "dave", "erin"}
	for _, mc := range []*membersCursor{
		nil,
		{epoch: denproto.Random(denproto.SyncEpochSize), version: start.Version},
		{epoch: start.Epoch, version: f.sync(owner).Version + 1},
	} {
		if names, since := f.membersSince(owner, mc); !slices.Equal(names, all) || since != nil {
			t.Fatalf("cursor %+v: %v (since %v)", mc, names, since)
		}
	}
	f.clock = f.clock.Add(changeLogAge + time.Hour)
	if err := f.d.pruneChanges(ctx); err != nil {
		t.Fatal(err)
	}
	if names, since := f.membersSince(owner, cursor); !slices.Equal(names, all) || since != nil {
		t.Fatalf("a cursor below the floor: %v (since %v)", names, since)
	}
}

func TestMembersCursorRoundTrips(t *testing.T) {
	epoch := denproto.Random(denproto.SyncEpochSize)
	e, v, ok := denproto.ParseMembersCursor(denproto.MembersCursor(epoch, 42))
	if !ok || !bytes.Equal(e, epoch) || v != 42 {
		t.Fatalf("round trip: %x %d %v", e, v, ok)
	}
	for _, bad := range []string{"", "x", "abc.1", denproto.Bytes(epoch[:8]).String() + ".1", epoch.String() + ".-1", epoch.String() + ".x"} {
		if _, _, ok := denproto.ParseMembersCursor(bad); ok {
			t.Fatalf("parsed %q", bad)
		}
	}
}
