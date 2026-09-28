package denclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// gone reports whether a den no longer accepts this device: it was removed,
// left, or had its key revoked.
func gone(c *conn) bool {
	st := c.status().State
	return st == StateRemoved || st == StateRevoked
}

// Profile returns a member of a den with their bio.
func (m *Manager) Profile(ctx context.Context, denID, memberID string) (denproto.Member, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Member{}, err
	}
	if err := checkID("member", memberID); err != nil {
		return denproto.Member{}, err
	}
	var member denproto.Member
	if err := c.call(ctx, http.MethodGet, "/api/members/"+memberID, nil, &member); err != nil {
		return denproto.Member{}, err
	}
	member, ok := cleanMember(member)
	if !ok || member.ID != memberID {
		return denproto.Member{}, errors.New("the den's answer is malformed")
	}
	return member, nil
}

// UpdateProfile changes this member's display name or bio in a den.
func (m *Manager) UpdateProfile(ctx context.Context, denID string, req denproto.ProfileRequest) (denproto.Member, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Member{}, err
	}
	if req.DisplayName != nil {
		name, err := denproto.CleanName(*req.DisplayName, denproto.MaxNameRunes)
		if err != nil {
			return denproto.Member{}, inputError(fmt.Errorf("display name: %w", err))
		}
		req.DisplayName = &name
	}
	if req.Bio != nil {
		if err := denproto.CheckBio(*req.Bio); err != nil {
			return denproto.Member{}, inputError(err)
		}
	}
	var member denproto.Member
	if err := c.call(ctx, http.MethodPatch, "/api/me", req, &member); err != nil {
		return denproto.Member{}, err
	}
	member, ok := cleanMember(member)
	if !ok {
		return denproto.Member{}, errors.New("the den's answer is malformed")
	}
	return member, nil
}

// SetRole makes a member a moderator, or a member again.
func (m *Manager) SetRole(ctx context.Context, denID, memberID, role string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("member", memberID); err != nil {
		return err
	}
	if role != denproto.RoleMember && role != denproto.RoleModerator {
		return inputError(errors.New("a role is member or moderator"))
	}
	return c.call(ctx, http.MethodPatch, "/api/members/"+memberID, denproto.RoleRequest{Role: role}, nil)
}

// RemoveMember removes a member from a den, and with req.Ban keeps them out.
func (m *Manager) RemoveMember(ctx context.Context, denID, memberID string, req denproto.RemoveRequest) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("member", memberID); err != nil {
		return err
	}
	if !slices.Contains(denproto.DeleteWindows, req.DeleteMessages) {
		return inputError(errors.New("messages go back an hour, a day or a week"))
	}
	return c.call(ctx, http.MethodPost, "/api/members/"+memberID+"/remove", req, nil)
}

// Bans lists a den's banned members.
func (m *Manager) Bans(ctx context.Context, denID string) ([]denproto.Ban, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	var list denproto.BanList
	if err := c.call(ctx, http.MethodGet, "/api/bans", nil, &list); err != nil {
		return nil, err
	}
	out := make([]denproto.Ban, 0, len(list.Bans))
	for _, b := range list.Bans {
		member, ok := cleanMember(b.Member)
		if !ok || b.BannedAt <= 0 || (b.BannedBy != "" && !validID(b.BannedBy)) {
			return nil, errors.New("the den's answer is malformed")
		}
		b.Member = member
		out = append(out, b)
	}
	return out, nil
}

// Unban lifts a ban.
func (m *Manager) Unban(ctx context.Context, denID, memberID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("member", memberID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/bans/"+memberID, nil, nil)
}

// OpenDM returns the DM with another member of a den, starting it if need
// be.
func (m *Manager) OpenDM(ctx context.Context, denID, memberID string) (denproto.Channel, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Channel{}, err
	}
	if err := checkID("member", memberID); err != nil {
		return denproto.Channel{}, err
	}
	var ch denproto.Channel
	if err := c.call(ctx, http.MethodPost, "/api/dms", denproto.DMRequest{MemberID: memberID}, &ch); err != nil {
		return denproto.Channel{}, err
	}
	ch, ok := cleanChannel(ch)
	if !ok || ch.Kind != denproto.KindDM || !slices.Contains(ch.Members, memberID) {
		return denproto.Channel{}, errors.New("the den's answer is malformed")
	}
	return ch, nil
}

// CloseDM takes a DM out of this member's list until it has a new message
// or is opened again.
func (m *Manager) CloseDM(ctx context.Context, denID, channelID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("channel", channelID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodPost, "/api/dms/"+channelID+"/close", nil, nil)
}

// Leave takes this member out of a den and forgets it here.
func (m *Manager) Leave(ctx context.Context, denID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if c.own {
		return inputError(errors.New("the owner can't leave the den this computer hosts"))
	}
	if err := c.call(ctx, http.MethodPost, "/api/me/leave", nil, nil); err != nil {
		return err
	}
	return m.forget(ctx, c)
}

// Forget deletes a den from this install without telling the den, for a
// den that no longer accepts this device.
func (m *Manager) Forget(ctx context.Context, denID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if !gone(c) {
		return inputError(errors.New("leave the den first"))
	}
	return m.forget(ctx, c)
}

// SetFocus records the channel a page shows in a den, "" for none, and
// tells the den the channels this install's pages show there, so typing
// reaches this member only where they're looking.
func (m *Manager) SetFocus(page, denID, channelID string) {
	if channelID != "" && !validID(channelID) {
		return
	}
	m.focusMu.Lock()
	pages := m.focus[page]
	if pages == nil {
		pages = map[string]string{}
		m.focus[page] = pages
	}
	if channelID == "" {
		delete(pages, denID)
	} else {
		pages[denID] = channelID
	}
	channels := m.denFocusLocked(denID)
	m.focusMu.Unlock()
	if c, err := m.find(denID); err == nil {
		c.setFocus(channels)
	}
}

// DropFocus forgets what a page showed, when it closes.
func (m *Manager) DropFocus(page string) {
	m.focusMu.Lock()
	dens := m.focus[page]
	delete(m.focus, page)
	update := map[string][]string{}
	for denID := range dens {
		update[denID] = m.denFocusLocked(denID)
	}
	m.focusMu.Unlock()
	for denID, channels := range update {
		if c, err := m.find(denID); err == nil {
			c.setFocus(channels)
		}
	}
}

// dropDenFocus forgets a forgotten den's focus.
func (m *Manager) dropDenFocus(denID string) {
	m.focusMu.Lock()
	defer m.focusMu.Unlock()
	for _, dens := range m.focus {
		delete(dens, denID)
	}
}

// denFocusLocked is the channels any page shows in a den, at most as many
// as the den accepts. The caller holds m.focusMu.
func (m *Manager) denFocusLocked(denID string) []string {
	var out []string
	for _, dens := range m.focus {
		if ch, ok := dens[denID]; ok && !slices.Contains(out, ch) {
			out = append(out, ch)
		}
	}
	slices.SortFunc(out, compareIDs)
	if len(out) > denproto.MaxFocus {
		out = out[:denproto.MaxFocus]
	}
	return out
}

// Typing tells a den this member is typing in a channel.
func (m *Manager) Typing(denID, channelID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("channel", channelID); err != nil {
		return err
	}
	c.typing(channelID)
	return nil
}
