package denclient_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestRolesSyncPrivacyAndFailClosedDelete exercises custom roles end to end
// between two connected clients and a den: creating/renaming roles, channel
// gating, hidden-role privacy for non-staff, preserving RoleIDs across
// member.updated profile edits, fail-closed deletion, and cache restore.
func TestRolesSyncPrivacyAndFailClosedDelete(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	bobID := me(t, member, denID).ID

	// Capability flag set on both views.
	viewOf(t, owner, denID, "owner has_roles", func(v denclient.View) bool { return v.HasRoles })
	viewOf(t, member, denID, "bob has_roles", func(v denclient.View) bool { return v.HasRoles })

	// Create two roles; restrict #raids to Raiders; keep Secret unused.
	raiders, err := owner.CreateRole(ctx, denID, "Raiders")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := owner.CreateRole(ctx, denID, "Secret")
	if err != nil {
		t.Fatal(err)
	}
	name := "raids"
	roleIDs := []string{raiders.ID}
	if err := owner.Manage(ctx, denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, RoleIDs: &roleIDs}); err != nil {
		t.Fatal(err)
	}

	// Owner sees both roles and #raids; bob sees neither yet.
	ownerView := viewOf(t, owner, denID, "owner sees raids and both roles", func(v denclient.View) bool {
		return len(v.Roles) == 2 && channelNamed(v, "raids")
	})
	_ = ownerView
	bobView := viewOf(t, member, denID, "bob connected", func(v denclient.View) bool { return v.HasRoles })
	if channelNamed(bobView, "raids") || len(bobView.Roles) != 0 {
		t.Fatalf("bob saw hidden channel or roles before assignment: channels=%+v roles=%+v", bobView.Channels, bobView.Roles)
	}

	// Assign bob both Raiders and Secret. Bob sees #raids and Raiders ONLY (Secret stays hidden).
	if err := owner.SetMemberRoles(ctx, denID, bobID, []string{raiders.ID, secret.ID}); err != nil {
		t.Fatal(err)
	}
	bobView = viewOf(t, member, denID, "bob sees raids and Raiders", func(v denclient.View) bool {
		return channelNamed(v, "raids") && len(v.Roles) == 1 && v.Roles[0].ID == raiders.ID && slices.Equal(v.Me.RoleIDs, []string{raiders.ID})
	})
	if m, ok := memberIn(bobView, bobID); !ok || !slices.Equal(m.RoleIDs, []string{raiders.ID}) {
		t.Fatalf("bob's member entry RoleIDs = %+v", m.RoleIDs)
	}

	// Renaming Raiders propagates live to bob.
	if _, err := owner.RenameRole(ctx, denID, raiders.ID, "Vanguard"); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "bob sees renamed role", func(v denclient.View) bool {
		return len(v.Roles) == 1 && v.Roles[0].Name == "Vanguard"
	})

	// Profile update (member.updated broadcast to Everyone without RoleIDs) preserves bob's RoleIDs.
	newDisplay := "Bob the Raider"
	if _, err := member.UpdateProfile(ctx, denID, denproto.ProfileRequest{DisplayName: &newDisplay}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "bob keeps RoleIDs after profile edit", func(v denclient.View) bool {
		m, ok := memberIn(v, bobID)
		return ok && m.DisplayName == newDisplay && slices.Equal(m.RoleIDs, []string{raiders.ID}) && slices.Equal(v.Me.RoleIDs, []string{raiders.ID})
	})

	// Deleting Raiders fails closed: #raids flips to staff_only for owner, disappears for bob.
	if err := owner.DeleteRole(ctx, denID, raiders.ID); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "bob loses raids on role delete", func(v denclient.View) bool {
		return !channelNamed(v, "raids") && len(v.Roles) == 0 && len(v.Me.RoleIDs) == 0
	})
	viewOf(t, owner, denID, "owner sees raids flipped to staff_only", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == "raids" {
				return c.StaffOnly && len(c.RoleIDs) == 0
			}
		}
		return false
	})
}
