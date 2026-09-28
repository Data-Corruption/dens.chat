package denclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// viewOf waits until a client's view of a den satisfies cond.
func viewOf(t *testing.T, m *denclient.Manager, denID, what string, cond func(denclient.View) bool) denclient.View {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v, err := m.View(denID)
		if err == nil && cond(v) {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v %v", what, v, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func memberIn(v denclient.View, id string) (denproto.Member, bool) {
	for _, m := range v.Members {
		if m.ID == id {
			return m, true
		}
	}
	return denproto.Member{}, false
}

func channelNamed(v denclient.View, name string) bool {
	return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.Name == name })
}

func me(t *testing.T, m *denclient.Manager, denID string) denproto.Member {
	t.Helper()
	return viewOf(t, m, denID, "a view", func(denclient.View) bool { return true }).Me
}

// A ban closes the member's sockets right away, and their name can't come
// back.
func TestBanClosesSocketsRightAway(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	bob := me(t, member, denID).ID
	start := time.Now()
	if err := owner.RemoveMember(ctx, denID, bob, denproto.RemoveRequest{Ban: true}); err != nil {
		t.Fatal(err)
	}
	st := waitFor(t, member, "the ban", func(s denclient.Status) bool { return s.State == denclient.StateRemoved })
	if took := time.Since(start); st.Error != "You were banned from this den." || took > 2*time.Second {
		t.Fatalf("after %v: %+v", took, st)
	}
	viewOf(t, owner, denID, "bob gone", func(v denclient.View) bool {
		m, ok := memberIn(v, bob)
		return ok && m.LeftAt != 0
	})
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = member.Join(ctx, invite, denclient.JoinRequest{Username: "bob", DisplayName: "Bob", Password: "correct horse"})
	if !denproto.IsCode(err, denproto.CodeBanned) {
		t.Fatalf("rejoining when banned: %v", err)
	}
}

// A removed member joins again with a new invite as themselves, in place
// of the entry the den stopped accepting.
func TestRemovedMemberRejoinsAsThemselves(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	bob := me(t, member, denID).ID
	send(t, member, denID, channelID, "before")
	if err := owner.RemoveMember(ctx, denID, bob, denproto.RemoveRequest{}); err != nil {
		t.Fatal(err)
	}
	st := waitFor(t, member, "the removal", func(s denclient.Status) bool { return s.State == denclient.StateRemoved })
	if st.Error != "You were removed from this den." {
		t.Fatalf("status %+v", st)
	}
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	join(t, member, invite, "bob")
	if again := me(t, member, denID); again.ID != bob || again.LeftAt != 0 {
		t.Fatalf("rejoined as %+v, want member %s", again, bob)
	}
	page, err := member.History(ctx, denID, channelID, denclient.HistoryQuery{})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].AuthorID != bob {
		t.Fatalf("history after rejoining: %+v %v", page, err)
	}
}

func TestLeaveForgetsTheDen(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	bob := me(t, member, denID).ID
	var input *denclient.InputError
	if err := owner.Leave(ctx, denID); !asInput(err, &input) {
		t.Fatalf("the owner leaving their own den: %v", err)
	}
	if err := member.Forget(ctx, denID); !asInput(err, &input) {
		t.Fatalf("forgetting a den still joined: %v", err)
	}
	if err := member.Leave(ctx, denID); err != nil {
		t.Fatal(err)
	}
	if list := member.Statuses(); len(list) != 0 {
		t.Fatalf("still listed: %+v", list)
	}
	viewOf(t, owner, denID, "bob gone", func(v denclient.View) bool {
		m, ok := memberIn(v, bob)
		return ok && m.LeftAt != 0 && !slices.Contains(v.Online, bob)
	})
}

func asInput(err error, target **denclient.InputError) bool {
	e, ok := err.(*denclient.InputError)
	*target = e
	return ok
}

func TestPresenceAndTyping(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	bob := me(t, member, denID).ID
	viewOf(t, owner, denID, "bob online", func(v denclient.View) bool { return slices.Contains(v.Online, bob) })
	stream, stop := owner.Stream()
	defer stop()

	// Typing reaches only connections showing the channel.
	member.SetFocus("bob's page", denID, channelID)
	if err := member.Typing(denID, channelID); err != nil {
		t.Fatal(err)
	}
	quiet := time.After(300 * time.Millisecond)
	for waiting := true; waiting; {
		select {
		case e := <-stream:
			for _, ev := range e.Events {
				if ev.T == denproto.EventTyping {
					t.Fatalf("typing reached a page that isn't showing the channel: %s", ev.D)
				}
			}
		case <-quiet:
			waiting = false
		}
	}
	owner.SetFocus("alice's page", denID, channelID)
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case e := <-stream:
			for _, ev := range e.Events {
				var typing denproto.Typing
				if ev.T == denproto.EventTyping && json.Unmarshal(ev.D, &typing) == nil {
					if typing.MemberID != bob || typing.ChannelID != channelID {
						t.Fatalf("typing %+v", typing)
					}
					return
				}
			}
		case <-tick.C:
			// Focus travels on its own; keep typing until it has arrived.
			_ = member.Typing(denID, channelID)
		case <-deadline:
			t.Fatal("typing never arrived")
		}
	}
}

func TestDMsBetweenClients(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	bob := me(t, member, denID).ID
	dm, err := owner.OpenDM(ctx, denID, bob)
	if err != nil || dm.Kind != denproto.KindDM {
		t.Fatalf("open: %+v %v", dm, err)
	}
	viewOf(t, member, denID, "the DM", func(v denclient.View) bool {
		return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == dm.ID })
	})
	stream, stop := owner.Stream()
	defer stop()
	send(t, member, denID, dm.ID, "psst")
	deadline := time.After(5 * time.Second)
	for counted := false; !counted; {
		select {
		case e := <-stream:
			for _, r := range e.Reads {
				counted = counted || (r.ChannelID == dm.ID && r.MentionCount == 1)
			}
		case <-deadline:
			t.Fatal("the DM never counted as unread")
		}
	}

	// Closing it lasts until the next message.
	closed := func(v denclient.View) bool {
		for _, r := range v.ReadStates {
			if r.ChannelID == dm.ID {
				return r.Closed
			}
		}
		return false
	}
	if err := owner.CloseDM(ctx, denID, dm.ID); err != nil {
		t.Fatal(err)
	}
	viewOf(t, owner, denID, "the DM closed", closed)
	send(t, member, denID, dm.ID, "still there?")
	viewOf(t, owner, denID, "the DM reopened", func(v denclient.View) bool { return !closed(v) })
}

func TestRoleChangeShowsStaffChannels(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	bob := me(t, member, denID).ID
	name, staffOnly := "staff room", true
	if err := owner.Manage(ctx, denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, StaffOnly: &staffOnly}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, owner, denID, "the staff room", func(v denclient.View) bool { return channelNamed(v, name) })
	if v, _ := member.View(denID); channelNamed(v, name) {
		t.Fatal("a member sees the staff room")
	}
	if err := owner.SetRole(ctx, denID, bob, denproto.RoleModerator); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the staff room once promoted", func(v denclient.View) bool { return channelNamed(v, name) })
	waitFor(t, member, "the new role", func(s denclient.Status) bool { return s.Role == denproto.RoleModerator })
	if err := owner.SetRole(ctx, denID, bob, denproto.RoleMember); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the staff room gone again", func(v denclient.View) bool { return !channelNamed(v, name) })
}

func TestProfilesAndBans(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	bob := me(t, member, denID).ID
	name, bio := "Robert", "I like **boats**."
	if _, err := member.UpdateProfile(ctx, denID, denproto.ProfileRequest{DisplayName: &name, Bio: &bio}); err != nil {
		t.Fatal(err)
	}
	p, err := owner.Profile(ctx, denID, bob)
	if err != nil || p.Bio != bio || p.DisplayName != name {
		t.Fatalf("profile: %+v %v", p, err)
	}
	viewOf(t, owner, denID, "the new name", func(v denclient.View) bool {
		m, ok := memberIn(v, bob)
		return ok && m.DisplayName == name && m.Bio == ""
	})
	waitFor(t, member, "bob's own new name", func(s denclient.Status) bool { return s.DisplayName == name })

	if err := owner.RemoveMember(ctx, denID, bob, denproto.RemoveRequest{Ban: true}); err != nil {
		t.Fatal(err)
	}
	bans, err := owner.Bans(ctx, denID)
	if err != nil || len(bans) != 1 || bans[0].Member.ID != bob {
		t.Fatalf("bans: %+v %v", bans, err)
	}
	if err := owner.Unban(ctx, denID, bob); err != nil {
		t.Fatal(err)
	}
	if bans, _ = owner.Bans(ctx, denID); len(bans) != 0 {
		t.Fatalf("bans after lifting: %+v", bans)
	}
}
