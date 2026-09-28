package den

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func (f *fixture) joinWith(code []byte, username, password string, dev device) (denproto.JoinResponse, error) {
	nonce, proof := f.proof(dev)
	info, _ := f.d.Info()
	return f.d.Join(context.Background(), denproto.JoinRequest{
		Invite: code, Username: username, DisplayName: "Name of " + username,
		Verifier: denproto.Verifier(password, info.ID, username), PublicKey: dev.pub(),
		DeviceLabel: "test", Nonce: nonce, Proof: proof,
	})
}

func (f *fixture) invite(s *Session, uses int) denproto.Invite {
	f.t.Helper()
	inv, err := f.d.CreateInvite(context.Background(), s, denproto.InviteCreateRequest{MaxUses: uses})
	if err != nil {
		f.t.Fatal(err)
	}
	return inv
}

// member joins someone new and returns their session.
func (f *fixture) member(owner *Session, username string) *Session {
	f.t.Helper()
	joined, err := f.join(f.invite(owner, 1).Code, username, newDevice())
	if err != nil {
		f.t.Fatal(err)
	}
	s, err := f.d.Authenticate(context.Background(), joined.Token)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) send(s *Session, channel, text string) denproto.Message {
	f.t.Helper()
	m, err := f.d.Send(context.Background(), s, channel, denproto.SendRequest{Nonce: denproto.Random(16), Text: text})
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func TestRolesAndPermissions(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	ownerMessage := f.send(owner, c.ID, "from the owner")
	memberMessage := f.send(member, c.ID, "from a member")

	_, err := f.d.SetRole(ctx, member, denproto.FormatID(carol.MemberID), denproto.RoleRequest{Role: denproto.RoleModerator})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.SetRole(ctx, owner, denproto.FormatID(owner.MemberID), denproto.RoleRequest{Role: denproto.RoleMember})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.SetRole(ctx, owner, denproto.FormatID(carol.MemberID), denproto.RoleRequest{Role: denproto.RoleOwner})
	wantCode(t, err, denproto.CodeInvalidField)
	m, err := f.d.SetRole(ctx, owner, denproto.FormatID(carol.MemberID), denproto.RoleRequest{Role: denproto.RoleModerator})
	if err != nil || m.Role != denproto.RoleModerator {
		t.Fatalf("promote: %+v %v", m, err)
	}
	carol.Role = m.Role // as her next request authenticates

	// Moderators manage channels and invites, and delete members' messages,
	// but not the owner's, and don't change the den or roles.
	f.newChannel(carol, "by a moderator", denproto.ChannelRequest{})
	f.invite(carol, 1)
	if err := f.d.Delete(ctx, carol, memberMessage.ID); err != nil {
		t.Fatalf("a moderator deleting a member's message: %v", err)
	}
	wantCode(t, f.d.Delete(ctx, carol, ownerMessage.ID), denproto.CodeForbidden)
	name := "Renamed"
	_, err = f.d.Update(ctx, carol, denproto.DenUpdateRequest{Name: &name})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.SetRole(ctx, carol, denproto.FormatID(member.MemberID), denproto.RoleRequest{Role: denproto.RoleModerator})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.CreateChannel(ctx, member, denproto.ChannelRequest{Name: &name})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.CreateInvite(ctx, member, denproto.InviteCreateRequest{})
	wantCode(t, err, denproto.CodeForbidden)
}

func TestRemoveBanAndReclaim(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.moderator(owner, "carol")
	dave := f.moderator(owner, "dave")
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	bob := denproto.FormatID(member.MemberID)

	// Ranks: a moderator removes members, not other moderators or the owner.
	wantCode(t, f.d.Remove(ctx, carol, denproto.FormatID(dave.MemberID), denproto.RemoveRequest{}), denproto.CodeForbidden)
	wantCode(t, f.d.Remove(ctx, carol, denproto.FormatID(owner.MemberID), denproto.RemoveRequest{}), denproto.CodeForbidden)
	wantCode(t, f.d.Remove(ctx, member, denproto.FormatID(carol.MemberID), denproto.RemoveRequest{}), denproto.CodeForbidden)
	wantCode(t, f.d.Remove(ctx, carol, bob, denproto.RemoveRequest{DeleteMessages: 60}), denproto.CodeInvalidField)

	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	if err := f.d.Remove(ctx, carol, bob, denproto.RemoveRequest{}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if e := <-sub.Events; e.T != denproto.EventMemberLeft {
		t.Fatalf("event %+v", e)
	}
	if f.sessions(member.MemberID) != 0 {
		t.Fatal("a removed member still has sessions")
	}
	gone, _ := f.d.Member(ctx, member.MemberID)
	if gone.LeftAt == 0 || gone.Role != denproto.RoleMember {
		t.Fatalf("after removal: %+v", gone)
	}

	// Back with a new invite: the wrong password can't take the name; the
	// right one reclaims the same member, history and first join time.
	inv := f.invite(owner, 2)
	_, err := f.joinWith(inv.Code, "bob", "not bob's password", newDevice())
	wantCode(t, err, denproto.CodeUsernameTaken)
	back, err := f.join(inv.Code, "bob", newDevice())
	if err != nil || back.Member.ID != bob || back.Member.JoinedAt != gone.JoinedAt || back.Member.LeftAt != 0 {
		t.Fatalf("reclaim: %+v %v", back.Member, err)
	}
	member, _ = f.d.Authenticate(ctx, back.Token)
	f.clock = f.clock.Add(2 * time.Hour)
	old := f.send(member, c.ID, "said long ago")
	f.clock = f.clock.Add(2 * time.Hour)
	recent := f.send(member, c.ID, "said just now")
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: denproto.FormatID(owner.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	private := f.send(member, dm.ID, "a DM")

	// A ban also deletes their recent messages, but not DMs, and revokes
	// the invite they came back with, which had a use left.
	if err := f.d.Remove(ctx, carol, bob, denproto.RemoveRequest{Ban: true, DeleteMessages: 3600, RevokeInvite: true}); err != nil {
		t.Fatalf("ban: %v", err)
	}
	for _, m := range []denproto.Message{old, private} {
		if _, _, err := f.d.message(ctx, m.ID); err != nil {
			t.Fatalf("%q was deleted: %v", m.Text, err)
		}
	}
	if _, _, err := f.d.message(ctx, recent.ID); err == nil {
		t.Fatal("the recent message survived the ban")
	}
	invites, _ := f.d.Invites(ctx, owner)
	for _, i := range invites {
		if i.ID == inv.ID {
			t.Fatal("the invite they joined with is still usable")
		}
	}
	bans, err := f.d.Bans(ctx, carol)
	if err != nil || len(bans.Bans) != 1 || bans.Bans[0].Member.ID != bob || bans.Bans[0].BannedBy != denproto.FormatID(carol.MemberID) {
		t.Fatalf("bans: %+v %v", bans, err)
	}
	_, err = f.d.Bans(ctx, dave)
	if err != nil {
		t.Fatalf("another moderator listing bans: %v", err)
	}
	_, err = f.join(f.invite(owner, 1).Code, "bob", newDevice())
	wantCode(t, err, denproto.CodeBanned)

	// Lifting the ban lets them reclaim their name again.
	if err := f.d.Unban(ctx, carol, bob); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.d.Unban(ctx, carol, bob), denproto.CodeNotFound)
	if back, err = f.join(f.invite(owner, 1).Code, "bob", newDevice()); err != nil || back.Member.ID != bob {
		t.Fatalf("rejoin after unban: %+v %v", back.Member, err)
	}

	// The owner removes moderators, and a removed moderator's invites go.
	f.invite(carol, 5)
	if err := f.d.Remove(ctx, owner, denproto.FormatID(carol.MemberID), denproto.RemoveRequest{}); err != nil {
		t.Fatal(err)
	}
	invites, _ = f.d.Invites(ctx, owner)
	for _, i := range invites {
		if i.CreatedBy == denproto.FormatID(carol.MemberID) {
			t.Fatal("a removed moderator's invite is still usable")
		}
	}
}

// moderator joins someone new and makes them a moderator.
func (f *fixture) moderator(owner *Session, username string) *Session {
	f.t.Helper()
	s := f.member(owner, username)
	if _, err := f.d.SetRole(context.Background(), owner, denproto.FormatID(s.MemberID), denproto.RoleRequest{Role: denproto.RoleModerator}); err != nil {
		f.t.Fatal(err)
	}
	s.Role = denproto.RoleModerator
	return s
}

func (f *fixture) sessions(member int64) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_sessions WHERE member_id = ?`, member).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestLeave(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	wantCode(t, f.d.Leave(ctx, owner), denproto.CodeForbidden)
	if err := f.d.Leave(ctx, member); err != nil {
		t.Fatal(err)
	}
	m, _ := f.d.Member(ctx, member.MemberID)
	if m.LeftAt == 0 {
		t.Fatalf("after leaving: %+v", m)
	}
	if f.sessions(member.MemberID) != 0 {
		t.Fatal("a member who left still has sessions")
	}
	snap, err := f.d.snapshot(ctx, owner.MemberID, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range snap.Members {
		if m.ID == denproto.FormatID(member.MemberID) && m.LeftAt == 0 {
			t.Fatal("the snapshot shows them still in the den")
		}
	}
}

func TestDirectMessages(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	bob := denproto.FormatID(member.MemberID)
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})

	carolSub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	bobSub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	_, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(owner.MemberID)})
	wantCode(t, err, denproto.CodeInvalidField)
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: bob})
	if err != nil || dm.Kind != denproto.KindDM || len(dm.Members) != 2 {
		t.Fatalf("open: %+v %v", dm, err)
	}
	again, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: denproto.FormatID(owner.MemberID)})
	if err != nil || again.ID != dm.ID {
		t.Fatalf("the pair has two DMs: %s and %s (%v)", dm.ID, again.ID, err)
	}
	if e := <-bobSub.Events; e.T != denproto.EventChannelCreated {
		t.Fatalf("bob got %+v", e)
	}

	// Only the two of them see it, and every message counts for the other.
	f.send(member, dm.ID, "hi, no mention needed")
	if e := <-bobSub.Events; e.T != denproto.EventMessageCreated {
		t.Fatalf("bob got %+v", e)
	}
	select {
	case e := <-carolSub.Events:
		t.Fatalf("carol got %+v", e)
	default:
	}
	_, err = f.d.History(ctx, carol, dm.ID, HistoryQuery{})
	wantCode(t, err, denproto.CodeNotFound)
	_, err = f.d.Send(ctx, carol, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "let me in"})
	wantCode(t, err, denproto.CodeNotFound)
	snap, _ := f.d.snapshot(ctx, owner.MemberID, true, 0)
	found := false
	for _, r := range snap.ReadStates {
		if r.ChannelID == dm.ID {
			found = r.MentionCount == 1
		}
	}
	if !found {
		t.Fatalf("the owner's unread DM: %+v", snap.ReadStates)
	}
	snap, _ = f.d.snapshot(ctx, carol.MemberID, false, 0)
	for _, c := range snap.Channels {
		if c.ID == dm.ID {
			t.Fatal("carol's snapshot has their DM")
		}
	}

	// DMs aren't channels to manage, order or count.
	_, err = f.d.UpdateChannel(ctx, owner, dm.ID, denproto.ChannelRequest{Name: ptr("renamed")})
	wantCode(t, err, denproto.CodeNotFound)
	wantCode(t, f.d.DeleteChannel(ctx, owner, dm.ID), denproto.CodeNotFound)
	if got := f.order(true); got != "general:-0" {
		t.Fatalf("channel order %q", got)
	}
	second := f.newChannel(owner, "second", denproto.ChannelRequest{})
	if second.Position != 1 || general.Position != 0 {
		t.Fatalf("positions %d %d", general.Position, second.Position)
	}

	// Once bob leaves, the DM's history stays but nothing more is sent, and
	// no new DM starts with him.
	if err := f.d.Leave(ctx, member); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.History(ctx, owner, dm.ID, HistoryQuery{}); err != nil {
		t.Fatal(err)
	}
	_, err = f.d.Send(ctx, owner, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "still there?"})
	wantCode(t, err, denproto.CodeForbidden)
	if reopened, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: bob}); err != nil || reopened.ID != dm.ID {
		t.Fatalf("reopening a DM with a former member: %+v %v", reopened, err)
	}
	_, err = f.d.OpenDM(ctx, carol, denproto.DMRequest{MemberID: bob})
	wantCode(t, err, denproto.CodeNotFound)
}

func TestProfiles(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	_, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Bio: ptr(string(make([]byte, denproto.MaxBioRunes+1)))})
	wantCode(t, err, denproto.CodeInvalidField)
	m, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{DisplayName: ptr("Robert"), Bio: ptr("I like **boats**.")})
	if err != nil || m.DisplayName != "Robert" || m.Bio != "I like **boats**." {
		t.Fatalf("update: %+v %v", m, err)
	}
	e := <-sub.Events
	var event denproto.Member
	if e.T != denproto.EventMemberUpdated || json.Unmarshal(e.D, &event) != nil || event.DisplayName != "Robert" || event.Bio != "" {
		t.Fatalf("event %+v", e)
	}
	var raw []byte
	f.db.QueryRow(`SELECT bio FROM den_members WHERE id = ?`, member.MemberID).Scan(&raw)
	if bytes.Contains(raw, []byte("boats")) {
		t.Fatal("the bio is stored in the clear")
	}
	if p, err := f.d.Profile(ctx, denproto.FormatID(member.MemberID)); err != nil || p.Bio != "I like **boats**." {
		t.Fatalf("profile: %+v %v", p, err)
	}
	if m, err = f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Bio: ptr("")}); err != nil || m.Bio != "" {
		t.Fatalf("clearing the bio: %+v %v", m, err)
	}
}

func (f *fixture) readState(member int64, channel string) denproto.ReadState {
	f.t.Helper()
	snap, err := f.d.snapshot(context.Background(), member, false, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, r := range snap.ReadStates {
		if r.ChannelID == channel {
			return r
		}
	}
	f.t.Fatalf("no read state for channel %s", channel)
	return denproto.ReadState{}
}

func TestClosingDMs(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(member.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.d.CloseDM(ctx, owner, general.ID), denproto.CodeInvalidField)

	// Closing a DM nobody has read yet reports no read position at all.
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	if err := f.d.CloseDM(ctx, owner, dm.ID); err != nil {
		t.Fatal(err)
	}
	var st denproto.ReadState
	if e := <-sub.Events; e.T != denproto.EventReadStateUpdated || json.Unmarshal(e.D, &st) != nil || !st.Closed || st.ReadPosition != "" {
		t.Fatalf("closing: %+v %s", e, e.D)
	}
	if r := f.readState(owner.MemberID, dm.ID); !r.Closed || r.ReadPosition != "" {
		t.Fatalf("the owner's snapshot: %+v", r)
	}
	if f.readState(member.MemberID, dm.ID).Closed {
		t.Fatal("closing it closed it for the other member too")
	}

	// A message from the other member brings it back.
	f.send(member, dm.ID, "you there?")
	for {
		e := <-sub.Events
		var reopened denproto.ReadState
		if e.T == denproto.EventReadStateUpdated && json.Unmarshal(e.D, &reopened) == nil && reopened.ChannelID == dm.ID {
			if reopened.Closed {
				t.Fatalf("still closed after a message: %+v", reopened)
			}
			break
		}
	}
	if f.readState(owner.MemberID, dm.ID).Closed {
		t.Fatal("the snapshot still has it closed")
	}

	// So does opening it again.
	if err := f.d.CloseDM(ctx, owner, dm.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(member.MemberID)}); err != nil {
		t.Fatal(err)
	}
	if r := f.readState(owner.MemberID, dm.ID); r.Closed || r.ReadPosition == "0" {
		t.Fatalf("after opening again: %+v", r)
	}
}
