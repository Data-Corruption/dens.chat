package den

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
	"github.com/coder/websocket"
)

func (f *fixture) addMember(owner *Session, username string) *Session {
	f.t.Helper()
	ctx := context.Background()
	inv, err := f.d.CreateInvite(ctx, owner, denproto.InviteCreateRequest{})
	if err != nil {
		f.t.Fatal(err)
	}
	joined, err := f.join(inv.Code, username, newDevice())
	if err != nil {
		f.t.Fatal(err)
	}
	s, err := f.d.Authenticate(ctx, joined.Token)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) memberRolesMask(member int64) uint64 {
	f.t.Helper()
	m, err := f.d.memberRoleMask(context.Background(), f.db, member)
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func (f *fixture) snapOf(s *Session) denproto.Ready {
	f.t.Helper()
	snap, err := f.d.snapshotAt(context.Background(), s.MemberID, IsStaff(s.Role), f.memberRolesMask(s.MemberID), s.Version, 0, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return snap
}

func TestRolesCRUDAndLimits(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	mod := f.addMember(owner, "carol")
	if _, err := f.d.SetRole(ctx, owner, denproto.FormatID(mod.MemberID), denproto.RoleRequest{Role: denproto.RoleModerator}); err != nil {
		t.Fatal(err)
	}
	mod.Role = denproto.RoleModerator

	// Non-staff cannot create, rename, delete, or assign roles.
	if _, err := f.d.CreateRole(ctx, bob, denproto.DenRoleRequest{Name: "Raiders"}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob created role: %v", err)
	}

	// Owner creates; moderator renames; case-insensitive uniqueness enforced.
	raiders, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "  Raiders  "})
	if err != nil || raiders.Name != "Raiders" || denproto.CheckRole(raiders) != nil {
		t.Fatalf("create Raiders: %+v %v", raiders, err)
	}
	if _, err := f.d.CreateRole(ctx, mod, denproto.DenRoleRequest{Name: "raiders"}); !denproto.IsCode(err, denproto.CodeRoleNameTaken) {
		t.Fatalf("duplicate role name: %v", err)
	}
	if _, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: ""}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("empty role name: %v", err)
	}
	if _, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: strings.Repeat("a", denproto.MaxNameRunes+1)}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("long role name: %v", err)
	}

	if _, err := f.d.RenameRole(ctx, bob, raiders.ID, denproto.DenRoleRequest{Name: "Nope"}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob renamed role: %v", err)
	}
	renamed, err := f.d.RenameRole(ctx, mod, raiders.ID, denproto.DenRoleRequest{Name: "Raid Team"})
	if err != nil || renamed.Name != "Raid Team" || renamed.ID != raiders.ID {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	// Renaming to same name with different case works.
	renamed, err = f.d.RenameRole(ctx, owner, raiders.ID, denproto.DenRoleRequest{Name: "raid team"})
	if err != nil || renamed.Name != "raid team" {
		t.Fatalf("case-only rename: %+v %v", renamed, err)
	}

	// Assign roles: moderator allowed, non-staff forbidden.
	if _, err := f.d.SetMemberRoles(ctx, bob, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{raiders.ID}}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob set roles: %v", err)
	}
	m, err := f.d.SetMemberRoles(ctx, mod, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{raiders.ID}})
	if err != nil || !slices.Equal(m.RoleIDs, []string{raiders.ID}) {
		t.Fatalf("set bob roles: %+v %v", m, err)
	}

	// Reopening the den reloads existing role slots into Hub.
	f.open()
	raidersID, _ := denproto.ParseID(raiders.ID)
	if f.d.Hub.Mask([]int64{raidersID}) == 0 {
		t.Fatal("Hub lost role slot after reopen")
	}

	// Cap at MaxRoles (50).
	for i := 1; i < denproto.MaxRoles; i++ {
		if _, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Role " + denproto.FormatID(int64(i))}); err != nil {
			t.Fatalf("create role %d: %v", i, err)
		}
	}
	if _, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Role 51"}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("51st role: %v", err)
	}

	// Delete role: non-staff forbidden, moderator allowed.
	if err := f.d.DeleteRole(ctx, bob, raiders.ID); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob deleted role: %v", err)
	}
	if err := f.d.DeleteRole(ctx, mod, raiders.ID); err != nil {
		t.Fatalf("mod deleted role: %v", err)
	}

	// Every written audit line across all pages passes CheckAuditLine.
	kinds := map[string]bool{}
	for before := ""; ; {
		log, err := f.d.AuditLog(ctx, owner, before)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range log.Lines {
			if err := denproto.CheckAuditLine(l); err != nil {
				t.Errorf("invalid audit line %+v: %v", l, err)
			}
			kinds[l.Kind] = true
		}
		if log.Next == "" {
			break
		}
		before = log.Next
	}
	for _, want := range []string{denproto.AuditRoleCreated, denproto.AuditRoleRenamed, denproto.AuditRoleDeleted, denproto.AuditMemberRolesChanged} {
		if !kinds[want] {
			t.Errorf("missing audit kind %s", want)
		}
	}
}

func TestChannelRoleVisibilityAndEnforcement(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.addMember(owner, "carol")

	raiders, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Raiders"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Secret"})
	if err != nil {
		t.Fatal(err)
	}

	// Contradictory staff_only + role_ids rejected on create.
	if _, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{
		Name: ptr("bad"), StaffOnly: ptr(true), RoleIDs: &[]string{raiders.ID},
	}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("contradictory create: %v", err)
	}

	// Create role-gated channel for Raiders, and an unused secret role.
	raids := f.newChannel(owner, "raids", denproto.ChannelRequest{RoleIDs: &[]string{raiders.ID}})
	if raids.StaffOnly || !slices.Equal(raids.RoleIDs, []string{raiders.ID}) {
		t.Fatalf("created raids: %+v", raids)
	}

	// Assign bob both Raiders and Secret; carol gets nothing.
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{raiders.ID, secret.ID}}); err != nil {
		t.Fatal(err)
	}

	// Non-staff snapshot privacy:
	// - bob sees #raids and Raiders, NEVER Secret (not referenced by any channel bob sees).
	// - carol sees neither #raids nor Raiders nor Secret, and bob's RoleIDs is empty for carol.
	// - owner sees everything including Secret.
	ownerSnap := f.snapOf(owner)
	if !ownerSnap.Den.Roles || len(ownerSnap.Roles) != 2 {
		t.Fatalf("owner snap roles: %+v (has=%v)", ownerSnap.Roles, ownerSnap.Den.Roles)
	}
	bobSnap := f.snapOf(bob)
	if !slices.ContainsFunc(bobSnap.Channels, func(c denproto.Channel) bool { return c.ID == raids.ID }) {
		t.Fatalf("bob cannot see #raids")
	}
	if len(bobSnap.Roles) != 1 || bobSnap.Roles[0].ID != raiders.ID {
		t.Fatalf("bob leaked roles: %+v", bobSnap.Roles)
	}
	if !slices.Equal(bobSnap.Me.RoleIDs, []string{raiders.ID}) {
		t.Fatalf("bob Me.RoleIDs leaked hidden role: %+v", bobSnap.Me.RoleIDs)
	}
	carolSnap := f.snapOf(carol)
	if slices.ContainsFunc(carolSnap.Channels, func(c denproto.Channel) bool { return c.ID == raids.ID }) {
		t.Fatalf("carol saw #raids")
	}
	if len(carolSnap.Roles) != 0 {
		t.Fatalf("carol leaked roles: %+v", carolSnap.Roles)
	}
	for _, m := range carolSnap.Members {
		if len(m.RoleIDs) != 0 {
			t.Fatalf("carol leaked member %s RoleIDs: %+v", m.Username, m.RoleIDs)
		}
	}
	// Profile endpoint also filters hidden roles for non-staff.
	if p, err := f.d.Profile(ctx, carol, denproto.FormatID(bob.MemberID)); err != nil || len(p.RoleIDs) != 0 {
		t.Fatalf("carol profile of bob leaked roles: %+v %v", p.RoleIDs, err)
	}
	if p, err := f.d.Profile(ctx, bob, denproto.FormatID(bob.MemberID)); err != nil || !slices.Equal(p.RoleIDs, []string{raiders.ID}) {
		t.Fatalf("bob profile of bob: %+v %v", p.RoleIDs, err)
	}

	// Subscriptions for live event & privacy checks.
	ownerSub, _, _, _ := f.d.Hub.SubscribeAt(owner.MemberID, true, 0, owner.Version, "", 0)
	defer f.d.Hub.Unsubscribe(ownerSub)
	bobSub, _, _, _ := f.d.Hub.SubscribeAt(bob.MemberID, false, f.memberRolesMask(bob.MemberID), bob.Version, "", 0)
	defer f.d.Hub.Unsubscribe(bobSub)
	carolSub, _, _, _ := f.d.Hub.SubscribeAt(carol.MemberID, false, 0, carol.Version, "", 0)
	defer f.d.Hub.Unsubscribe(carolSub)

	// Renaming unused Secret role reaches staff only, never bob or carol.
	if _, err := f.d.RenameRole(ctx, owner, secret.ID, denproto.DenRoleRequest{Name: "Top Secret"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(drain(ownerSub), denproto.EventRoleUpdated) {
		t.Fatal("owner missed role.updated")
	}
	if got := drain(bobSub); len(got) != 0 {
		t.Fatalf("bob heard hidden role rename: %v", got)
	}
	if got := drain(carolSub); len(got) != 0 {
		t.Fatalf("carol heard hidden role rename: %v", got)
	}

	// Renaming Raiders reaches owner and bob (who sees #raids), NOT carol.
	if _, err := f.d.RenameRole(ctx, owner, raiders.ID, denproto.DenRoleRequest{Name: "Vanguard"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(drain(ownerSub), denproto.EventRoleUpdated) {
		t.Fatal("owner missed Raiders rename")
	}
	if !slices.Contains(drain(bobSub), denproto.EventRoleUpdated) {
		t.Fatal("bob missed visible role rename")
	}
	if got := drain(carolSub); len(got) != 0 {
		t.Fatalf("carol heard Raiders rename: %v", got)
	}

	// Access enforcement in #raids:
	// - bob posts a message with a task, file, and poll; carol cannot read, post, react, vote, tick, open file, or see changes.
	up, err := f.d.Upload(ctx, bob, "plan.txt", 4, strings.NewReader("loot"), false, "")
	if err != nil {
		t.Fatal(err)
	}
	// Co-editor check: carol rejected, owner accepted.
	if _, err := f.d.Send(ctx, bob, raids.ID, denproto.SendRequest{
		Nonce: denproto.Random(16), Text: "- [ ] pull boss", Editors: []string{denproto.FormatID(carol.MemberID)},
	}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("carol accepted as co-editor in role-gated channel: %v", err)
	}
	msg, err := f.d.Send(ctx, bob, raids.ID, denproto.SendRequest{
		Nonce: denproto.Random(16), Text: "- [ ] pull boss @carol @bob",
		Attachments: []string{up.ID},
		Editors:     []string{denproto.FormatID(owner.MemberID)},
	})
	if err != nil {
		t.Fatalf("bob send in #raids: %v", err)
	}
	// Live message event reached owner & bob, NOT carol.
	if !slices.Contains(drain(ownerSub), denproto.EventMessageCreated) || !slices.Contains(drain(bobSub), denproto.EventMessageCreated) {
		t.Fatal("owner/bob missed message in #raids")
	}
	if got := drain(carolSub); len(got) != 0 {
		t.Fatalf("carol received event from #raids: %v", got)
	}
	// @everyone from owner notifies bob (holder), NOT carol (non-holder).
	if _, err := f.d.Send(ctx, owner, raids.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "@everyone ready"}); err != nil {
		t.Fatal(err)
	}
	drain(ownerSub)
	drain(bobSub)
	if got := drain(carolSub); len(got) != 0 {
		t.Fatalf("carol got @everyone from #raids: %v", got)
	}
	// Carol cannot read history, send, react, tick, open file, or see #raids in Changes.
	if _, err := f.d.History(ctx, carol, raids.ID, HistoryQuery{}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol read history: %v", err)
	}
	if _, err := f.d.Send(ctx, carol, raids.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "hi"}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol sent in #raids: %v", err)
	}
	if err := f.d.React(ctx, carol, msg.ID, denproto.ReactionRequest{Emoji: "👍"}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol reacted in #raids: %v", err)
	}
	if _, err := f.d.SetTask(ctx, carol, msg.ID, 0, denproto.TaskRequest{Text: "pull boss", Checked: true}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol ticked task: %v", err)
	}
	if rc, err := f.d.OpenFile(ctx, carol, up.ID, false); err == nil {
		rc.Close()
		t.Fatal("carol opened file from #raids")
	}
	// Bob can open file and poll works with role-scoped Voters/NonVoters audiences.
	rc, err := f.d.OpenFile(ctx, bob, up.ID, false)
	if err != nil {
		t.Fatalf("bob open file: %v", err)
	}
	rc.Close()

	pollMsg := f.sendPoll(owner, raids.ID, "Boss?", denproto.PollRequest{
		Choices: []string{"A", "B"}, Hours: 24, ShowVoters: denproto.VotersAlways, ShowTotals: denproto.TotalsVoted,
	})
	drain(ownerSub)
	drain(bobSub)
	drain(carolSub)
	if _, err := f.d.Vote(ctx, carol, pollMsg.ID, denproto.VoteRequest{Choices: []int{0}}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol voted in #raids: %v", err)
	}
	if _, err := f.d.Vote(ctx, bob, pollMsg.ID, denproto.VoteRequest{Choices: []int{0}}); err != nil {
		t.Fatalf("bob vote: %v", err)
	}
	f.d.flushPolls()
	if !slices.Contains(drain(ownerSub), denproto.EventPollUpdated) || !slices.Contains(drain(bobSub), denproto.EventPollUpdated) {
		t.Fatal("owner/bob missed poll.updated in #raids")
	}
	if got := drain(carolSub); len(got) != 0 {
		t.Fatalf("carol leaked poll.updated from #raids: %v", got)
	}

	// Edit message and verify Changes exposes it to bob & owner, never carol.
	if _, err := f.d.Edit(ctx, bob, msg.ID, denproto.EditRequest{Revision: msg.Revision, Text: "- [ ] pull boss now"}); err != nil {
		t.Fatal(err)
	}
	carolChanges, err := f.d.Changes(ctx, carol, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(carolChanges.Messages) != 0 || len(carolChanges.Deleted) != 0 {
		t.Fatalf("carol leaked changes from #raids: %+v", carolChanges)
	}
	bobChanges, err := f.d.Changes(ctx, bob, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(bobChanges.Messages, func(m denproto.Message) bool { return m.ID == msg.ID }) {
		t.Fatalf("bob missed edit in Changes: %+v", bobChanges)
	}

	// MarkAllRead skips #raids for carol (no read state written), updates for bob.
	if err := f.d.MarkAllRead(ctx, carol); err != nil {
		t.Fatal(err)
	}
	var n int
	cid, _ := denproto.ParseID(raids.ID)
	if err := f.db.QueryRow(`SELECT count(*) FROM den_read_states WHERE member_id = ? AND channel_id = ?`, carol.MemberID, cid).Scan(&n); err != nil || n != 0 {
		t.Fatalf("carol got read state in #raids: n=%d err=%v", n, err)
	}

	// Revoking bob's role triggers targeted _refresh and cuts access immediately.
	drain(bobSub)
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(drain(bobSub), eventRefresh) {
		t.Fatal("bob did not receive targeted refresh on role removal")
	}
	if _, err := f.d.History(ctx, bob, raids.ID, HistoryQuery{}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("bob still read #raids after role removal: %v", err)
	}

	// Mode transitions on UpdateChannel:
	// Roles -> Staff only clears den_channel_roles.
	updated, err := f.d.UpdateChannel(ctx, owner, raids.ID, denproto.ChannelRequest{StaffOnly: ptr(true)})
	if err != nil || !updated.StaffOnly || len(updated.RoleIDs) != 0 {
		t.Fatalf("switch to staff_only: %+v %v", updated, err)
	}
	// Staff only -> Selected roles clears staff_only.
	updated, err = f.d.UpdateChannel(ctx, owner, raids.ID, denproto.ChannelRequest{RoleIDs: &[]string{raiders.ID}})
	if err != nil || updated.StaffOnly || !slices.Equal(updated.RoleIDs, []string{raiders.ID}) {
		t.Fatalf("switch to roles: %+v %v", updated, err)
	}
	// Selected roles -> Everyone.
	updated, err = f.d.UpdateChannel(ctx, owner, raids.ID, denproto.ChannelRequest{RoleIDs: &[]string{}})
	if err != nil || updated.StaffOnly || len(updated.RoleIDs) != 0 {
		t.Fatalf("switch to everyone: %+v %v", updated, err)
	}
	if _, err := f.d.History(ctx, carol, raids.ID, HistoryQuery{}); err != nil {
		t.Fatalf("carol could not read #raids after opening to everyone: %v", err)
	}
}

func TestRoleDeletionFailClosedAndEndedEvents(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.addMember(owner, "carol")

	r1, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Beta"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{r1.ID}}); err != nil {
		t.Fatal(err)
	}

	soleText := f.newChannel(owner, "alpha-only", denproto.ChannelRequest{RoleIDs: &[]string{r1.ID}})
	sharedText := f.newChannel(owner, "shared", denproto.ChannelRequest{RoleIDs: &[]string{r1.ID, r2.ID}})
	alphaVoice := f.newChannel(owner, "Alpha Voice", denproto.ChannelRequest{Kind: denproto.KindVoice, RoleIDs: &[]string{r1.ID}})

	// Create an event in Alpha Voice, mark bob interested, end it, then delete Alpha Voice.
	// Ended event must snapshot den_channel_roles into den_event_roles.
	ev := f.makeEvent(owner, f.eventAt("Raid Night", alphaVoice.ID, time.Hour, time.Hour))
	if _, err := f.d.SetInterest(ctx, carol, ev.ID, true); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol marked interest in hidden role event: %v", err)
	}
	if _, err := f.d.SetInterest(ctx, bob, ev.ID, true); err != nil {
		t.Fatalf("bob interest: %v", err)
	}
	if got := f.eventsOf(carol); got != "" {
		t.Fatalf("carol saw hidden event: %s", got)
	}
	if got := f.eventsOf(bob); !strings.Contains(got, "Raid Night:1+mine") {
		t.Fatalf("bob events: %s", got)
	}
	// Advance to live and end the event, then delete its voice channel.
	f.clock = f.clock.Add(time.Hour + time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.d.EndEvent(ctx, owner, ev.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.DeleteChannel(ctx, owner, alphaVoice.ID); err != nil {
		t.Fatal(err)
	}
	// After deleting voice channel, bob (holder of r1) still sees the ended event card; carol does not.
	if _, err := f.d.Event(ctx, bob, ev.ID); err != nil {
		t.Fatalf("bob lost ended event after voice channel delete: %v", err)
	}
	if _, err := f.d.Event(ctx, carol, ev.ID); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol saw ended event after voice channel delete: %v", err)
	}

	// Delete r1:
	// - soleText flips to staff_only = 1 (never Everyone!)
	// - sharedText stays role-gated on r2 alone (staff_only = 0)
	// - ended event flips to staff_only = 1 (bob loses access, owner keeps it)
	if err := f.d.DeleteRole(ctx, owner, r1.ID); err != nil {
		t.Fatal(err)
	}
	soleCID, _ := denproto.ParseID(soleText.ID)
	soleAfter, err := f.d.channel(ctx, f.db, soleCID)
	if err != nil || !soleAfter.StaffOnly || len(soleAfter.RoleIDs) != 0 {
		t.Fatalf("soleText after role delete: %+v %v", soleAfter, err)
	}
	sharedCID, _ := denproto.ParseID(sharedText.ID)
	sharedAfter, err := f.d.channel(ctx, f.db, sharedCID)
	if err != nil || sharedAfter.StaffOnly || !slices.Equal(sharedAfter.RoleIDs, []string{r2.ID}) {
		t.Fatalf("sharedText after role delete: %+v %v", sharedAfter, err)
	}
	// Neither bob nor carol can see soleText or the ended event now.
	for _, s := range []*Session{bob, carol} {
		if _, err := f.d.History(ctx, s, soleText.ID, HistoryQuery{}); !denproto.IsCode(err, denproto.CodeNotFound) {
			t.Fatalf("member %d saw flipped channel: %v", s.MemberID, err)
		}
		if _, err := f.d.Event(ctx, s, ev.ID); !denproto.IsCode(err, denproto.CodeNotFound) {
			t.Fatalf("member %d saw flipped ended event: %v", s.MemberID, err)
		}
	}
	if _, err := f.d.Event(ctx, owner, ev.ID); err != nil {
		t.Fatalf("owner lost flipped ended event: %v", err)
	}

	// Audit line for role.deleted lists #alpha-only under channels_staff_only.
	log, err := f.d.AuditLog(ctx, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range log.Lines {
		if l.Kind == denproto.AuditRoleDeleted && l.Name == "Alpha" {
			found = true
			if err := denproto.CheckAuditLine(l); err != nil {
				t.Fatalf("bad role.deleted audit line: %v", err)
			}
			if len(l.Changes) != 1 || l.Changes[0].Field != "channels_staff_only" || l.Changes[0].To != "#alpha-only" {
				t.Fatalf("role.deleted changes = %+v, want #alpha-only", l.Changes)
			}
		}
	}
	if !found {
		t.Fatal("no role.deleted audit line for Alpha")
	}

	// Erase clears role tables too.
	if err := f.d.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	var cnt int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_roles`).Scan(&cnt); err != nil || cnt != 0 {
		t.Fatalf("den_roles not cleared by Erase: cnt=%d err=%v", cnt, err)
	}
}

func TestRoleVoiceKicksAndMove(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.addMember(owner, "carol")

	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.StartCalls(udp, tcp, sfu.Config{Loopback: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.StopCalls() })

	raiders, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Raiders"})
	if err != nil {
		t.Fatal(err)
	}
	vip, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "VIP"})
	if err != nil {
		t.Fatal(err)
	}
	lounge := f.newChannel(owner, "Lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	raidVoice := f.newChannel(owner, "Raid Voice", denproto.ChannelRequest{Kind: denproto.KindVoice, RoleIDs: &[]string{raiders.ID}})

	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{raiders.ID}}); err != nil {
		t.Fatal(err)
	}

	bobSub, _, _, _ := f.d.Hub.SubscribeAt(bob.MemberID, false, f.memberRolesMask(bob.MemberID), bob.Version, "", 0)
	defer f.d.Hub.Unsubscribe(bobSub)
	carolSub, _, _, _ := f.d.Hub.SubscribeAt(carol.MemberID, false, 0, carol.Version, "", 0)
	defer f.d.Hub.Unsubscribe(carolSub)

	mkSock := func(s *Session) *socket {
		return &socket{member: s.MemberID, keyID: s.KeyID, closeReq: make(chan closeRequest, 1), voiceOut: make(chan denproto.Event, 16)}
	}
	next := func(s *socket, want string) denproto.Event {
		t.Helper()
		select {
		case e := <-s.voiceOut:
			if e.T != want {
				t.Fatalf("got %s %s, want %s", e.T, e.D, want)
			}
			return e
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %s", want)
		}
		return denproto.Event{}
	}
	endedFor := func(s *socket, reason string) {
		t.Helper()
		var x denproto.VoiceEnded
		if e := next(s, denproto.EventVoiceEnded); json.Unmarshal(e.D, &x) != nil || x.Reason != reason {
			t.Fatalf("ended %s, want %s", e.D, reason)
		}
	}

	// Non-holder carol cannot join Raid Voice (returns not_found so hidden channel IDs aren't oracleable).
	carolSock := mkSock(carol)
	f.d.joinCall(ctx, carol, carolSock, carolSub, denproto.VoiceJoin{ChannelID: raidVoice.ID, Sealed: true})
	endedFor(carolSock, denproto.VoiceNotFound)

	// Carol joins Lounge; staff cannot move her into Raid Voice (not_found, matching staff_only).
	f.d.joinCall(ctx, carol, carolSock, carolSub, denproto.VoiceJoin{ChannelID: lounge.ID, Sealed: true})
	next(carolSock, denproto.EventVoiceOffer)
	if err := f.d.MoveMember(ctx, owner, denproto.FormatID(carol.MemberID), denproto.MoveRequest{ChannelID: raidVoice.ID}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("moved non-holder into role-gated voice: %v", err)
	}

	// Bob joins Raid Voice; visibleCalls shows it to bob and staff, never carol.
	bobSock := mkSock(bob)
	f.d.joinCall(ctx, bob, bobSock, bobSub, denproto.VoiceJoin{ChannelID: raidVoice.ID, Sealed: true})
	next(bobSock, denproto.EventVoiceOffer)
	if !slices.ContainsFunc(f.d.visibleCalls(false, f.memberRolesMask(bob.MemberID)), func(c denproto.Call) bool { return c.ChannelID == raidVoice.ID }) {
		t.Fatal("bob cannot see own role-gated call")
	}
	if slices.ContainsFunc(f.d.visibleCalls(false, 0), func(c denproto.Call) bool { return c.ChannelID == raidVoice.ID }) {
		t.Fatal("non-holder saw role-gated call in visibleCalls")
	}

	// 1. Revoking bob's role immediately kicks him with VoiceForbidden.
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	endedFor(bobSock, denproto.VoiceForbidden)

	// 2. Re-grant Raiders, rejoin, then narrow Raid Voice to VIP -> immediate kick.
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{raiders.ID}}); err != nil {
		t.Fatal(err)
	}
	f.d.joinCall(ctx, bob, bobSock, bobSub, denproto.VoiceJoin{ChannelID: raidVoice.ID, Sealed: true})
	next(bobSock, denproto.EventVoiceOffer)
	if _, err := f.d.UpdateChannel(ctx, owner, raidVoice.ID, denproto.ChannelRequest{RoleIDs: &[]string{vip.ID}}); err != nil {
		t.Fatal(err)
	}
	endedFor(bobSock, denproto.VoiceForbidden)

	// 3. Grant VIP, rejoin, then delete VIP -> immediate kick (and channel flips to staff_only).
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{vip.ID}}); err != nil {
		t.Fatal(err)
	}
	f.d.joinCall(ctx, bob, bobSock, bobSub, denproto.VoiceJoin{ChannelID: raidVoice.ID, Sealed: true})
	next(bobSock, denproto.EventVoiceOffer)
	if err := f.d.DeleteRole(ctx, owner, vip.ID); err != nil {
		t.Fatal(err)
	}
	endedFor(bobSock, denproto.VoiceForbidden)

	// Clean up carol's call.
	f.d.leaveCall(carolSock)
}

func TestGatedZeroRoleMaskFailsClosed(t *testing.T) {
	if got := unsafe.Sizeof(Audience{}); got != 48 {
		t.Fatalf("Audience size = %d, want 48", got)
	}
	f, owner, bob := chatFixture(t)
	h := f.d.Hub
	r1, _ := h.EnsureRole(9001)
	r2, _ := h.EnsureRole(9002)

	voters := map[int64]bool{owner.MemberID: true, bob.MemberID: true}
	staffSub, _, _, _ := h.SubscribeAt(owner.MemberID, true, 0, denproto.Version, "", 0)
	defer h.Unsubscribe(staffSub)
	bobSub, _, _, _ := h.SubscribeAt(bob.MemberID, false, r1, denproto.Version, "", 0)
	defer h.Unsubscribe(bobSub)
	otherSub, _, _, _ := h.SubscribeAt(999, false, r2, denproto.Version, "", 0)
	defer h.Unsubscribe(otherSub)
	plainSub, _, _, _ := h.SubscribeAt(998, false, 0, denproto.Version, "", 0)
	defer h.Unsubscribe(plainSub)

	// A gated channel whose role mask resolved to 0 (e.g. concurrent role delete)
	// must fail closed for both Voters and NonVoters.
	if err := h.Publish("gated-zero-voters", nil, Voters(voters, false, true, 0)); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish("gated-zero-nonvoters", nil, NonVoters(voters, false, true, 0)); err != nil {
		t.Fatal(err)
	}
	// A gated channel with a live bit includes only staff and matching holders.
	if err := h.Publish("gated-r1-voters", nil, Voters(voters, false, true, r1)); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish("gated-r1-nonvoters", nil, NonVoters(voters, false, true, r1)); err != nil {
		t.Fatal(err)
	}

	drain := func(sub *Sub) []string {
		var out []string
		for len(sub.Events) > 0 {
			out = append(out, (<-sub.Events).T)
		}
		return out
	}
	if got := drain(staffSub); !slices.Equal(got, []string{"gated-zero-voters", "gated-r1-voters"}) {
		t.Fatalf("staff got %v", got)
	}
	if got := drain(bobSub); !slices.Equal(got, []string{"gated-r1-voters"}) {
		t.Fatalf("bob (r1 voter) got %v, want only gated-r1-voters", got)
	}
	if got := drain(otherSub); len(got) != 0 {
		t.Fatalf("other (r2 non-voter) got %v, want none", got)
	}
	if got := drain(plainSub); len(got) != 0 {
		t.Fatalf("plain (roleless non-voter) got %v, want none", got)
	}

	// roomVisible and publishCallsLocked must also fail closed when gated && roles == 0.
	room := &callRoom{gated: true, roles: 0, began: f.d.now()}
	if roomVisible(room, false, 0) || roomVisible(room, false, r1) {
		t.Fatal("roomVisible allowed non-staff into gated room with zero role mask")
	}
	if !roomVisible(room, true, 0) {
		t.Fatal("roomVisible denied staff on gated room with zero role mask")
	}
	f.d.calls.mu.Lock()
	f.d.calls.rooms[777] = room
	f.d.publishCallsLocked(777)
	f.d.calls.mu.Unlock()

	if got := drain(staffSub); !slices.Equal(got, []string{denproto.EventVoiceState}) {
		t.Fatalf("staff call state = %v, want [voice.state]", got)
	}
	for name, sub := range map[string]*Sub{"bob": bobSub, "other": otherSub, "plain": plainSub} {
		if got := drain(sub); len(got) != 0 {
			t.Fatalf("%s leaked call state for gated zero-mask room: %v", name, got)
		}
	}
}

func TestSocketSubscribeSerializedWithRoleRevoke(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()

	raiders, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Raiders"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{raiders.ID}}); err != nil {
		t.Fatal(err)
	}
	rid, _ := denproto.ParseID(raiders.ID)
	raidersMask := f.d.Hub.Mask([]int64{rid})
	// ServeSocket checks session expiry against wall time.
	bob.ExpiresAt = time.Now().Add(time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.d.ServeSocket(w, r, bob)
	}))
	defer srv.Close()

	// Hold d.roles write lock while bob connects: ServeSocket must wait on
	// d.roles.RLock() before reading memberRoleMask and subscribing.
	f.d.roles.Lock()
	dialed := make(chan *websocket.Conn, 1)
	dialErr := make(chan error, 1)
	go func() {
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
		c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{CompressionMode: websocket.CompressionNoContextTakeover})
		if err != nil {
			dialErr <- err
			return
		}
		dialed <- c
	}()

	var conn *websocket.Conn
	select {
	case err := <-dialErr:
		f.d.roles.Unlock()
		t.Fatal(err)
	case conn = <-dialed:
	case <-time.After(5 * time.Second):
		f.d.roles.Unlock()
		t.Fatal("timed out waiting for websocket upgrade")
	}
	defer conn.CloseNow()

	// Revoke bob's role while holding d.roles.Lock(), before ServeSocket subscribes.
	if _, err := f.db.ExecContext(ctx, `DELETE FROM den_member_roles WHERE member_id = ?`, bob.MemberID); err != nil {
		f.d.roles.Unlock()
		t.Fatal(err)
	}
	f.d.Hub.SetMemberRoles(bob.MemberID, 0)
	f.d.roles.Unlock()

	readFrame := func() []denproto.Event {
		t.Helper()
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, data, err := conn.Read(rctx)
		if err != nil {
			t.Fatalf("read ws frame: %v", err)
		}
		evs, err := denproto.DecodeFrame(data)
		if err != nil {
			t.Fatalf("decode ws frame: %v", err)
		}
		return evs
	}

	// Drain initial ready event.
	gotReady := false
	for !gotReady {
		for _, ev := range readFrame() {
			if ev.T == denproto.EventReady {
				gotReady = true
			}
		}
	}

	// Publish a role-gated event followed by an Everyone sentinel: bob must
	// never receive the gated event because his subscribe saw the revoke.
	if err := f.d.Hub.Publish("secret.role.event", nil, Roles(raidersMask)); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Hub.Publish("public.sentinel", nil, Everyone); err != nil {
		t.Fatal(err)
	}
	for {
		for _, ev := range readFrame() {
			if ev.T == "secret.role.event" {
				t.Fatal("bob received role-gated event on socket subscribed concurrently with revoke")
			}
			if ev.T == "public.sentinel" {
				return
			}
		}
	}
}

func TestJoinCallRechecksVisibilityUnderLock(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()

	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.StartCalls(udp, tcp, sfu.Config{Loopback: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.StopCalls() })

	raiders, err := f.d.CreateRole(ctx, owner, denproto.DenRoleRequest{Name: "Raiders"})
	if err != nil {
		t.Fatal(err)
	}
	raidVoice := f.newChannel(owner, "Raid Voice", denproto.ChannelRequest{Kind: denproto.KindVoice, RoleIDs: &[]string{raiders.ID}})
	cid, _ := denproto.ParseID(raidVoice.ID)
	if _, err := f.d.SetMemberRoles(ctx, owner, denproto.FormatID(bob.MemberID), denproto.MemberRolesRequest{RoleIDs: []string{raiders.ID}}); err != nil {
		t.Fatal(err)
	}

	bobSub, _, _, _ := f.d.Hub.SubscribeAt(bob.MemberID, false, f.memberRolesMask(bob.MemberID), bob.Version, "", 0)
	defer f.d.Hub.Unsubscribe(bobSub)
	bobSock := &socket{member: bob.MemberID, keyID: bob.KeyID, closeReq: make(chan closeRequest, 1), voiceOut: make(chan denproto.Event, 16)}

	expectNotFound := func(label string) {
		t.Helper()
		select {
		case e := <-bobSock.voiceOut:
			var ended denproto.VoiceEnded
			if e.T != denproto.EventVoiceEnded || json.Unmarshal(e.D, &ended) != nil || ended.Reason != denproto.VoiceNotFound {
				t.Fatalf("%s: got %s %s, want voice.ended %s", label, e.T, e.D, denproto.VoiceNotFound)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: timed out waiting for refusal", label)
		}
		f.d.calls.mu.Lock()
		defer f.d.calls.mu.Unlock()
		if f.d.calls.byMember[bob.MemberID] != nil || f.d.calls.rooms[cid] != nil {
			t.Fatalf("%s: call or room created despite visibility change", label)
		}
	}

	// Case 1: channel flips to staff_only after joinCall's pre-lock check,
	// while joinCall waits on calls.mu.
	f.d.calls.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.d.joinCall(ctx, bob, bobSock, bobSub, denproto.VoiceJoin{ChannelID: raidVoice.ID, Sealed: true})
	}()
	time.Sleep(30 * time.Millisecond)
	if _, err := f.db.ExecContext(ctx, `DELETE FROM den_channel_roles WHERE channel_id = ?`, cid); err != nil {
		f.d.calls.mu.Unlock()
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, `UPDATE den_channels SET staff_only = 1 WHERE id = ?`, cid); err != nil {
		f.d.calls.mu.Unlock()
		t.Fatal(err)
	}
	f.d.calls.mu.Unlock()
	<-done
	expectNotFound("channel flipped to staff_only")

	// Case 2: channel restored to Raiders, then bob's role is revoked in DB
	// while joinCall waits on calls.mu (even though bobSub still has the stale mask).
	rid, _ := denproto.ParseID(raiders.ID)
	if _, err := f.db.ExecContext(ctx, `UPDATE den_channels SET staff_only = 0 WHERE id = ?`, cid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, `INSERT INTO den_channel_roles (channel_id, role_id) VALUES (?, ?)`, cid, rid); err != nil {
		t.Fatal(err)
	}
	f.d.calls.mu.Lock()
	done = make(chan struct{})
	go func() {
		defer close(done)
		f.d.joinCall(ctx, bob, bobSock, bobSub, denproto.VoiceJoin{ChannelID: raidVoice.ID, Sealed: true})
	}()
	time.Sleep(30 * time.Millisecond)
	if _, err := f.db.ExecContext(ctx, `DELETE FROM den_member_roles WHERE member_id = ?`, bob.MemberID); err != nil {
		f.d.calls.mu.Unlock()
		t.Fatal(err)
	}
	f.d.calls.mu.Unlock()
	<-done
	expectNotFound("member role revoked in DB")
}
