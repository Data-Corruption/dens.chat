package denclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// CreateRole adds a custom role to a den, as staff.
func (m *Manager) CreateRole(ctx context.Context, denID, name string) (denproto.Role, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Role{}, err
	}
	name, err = denproto.CleanRoleName(name)
	if err != nil {
		return denproto.Role{}, inputError(err)
	}
	var role denproto.Role
	if err := c.call(ctx, http.MethodPost, "/api/roles", denproto.DenRoleRequest{Name: name}, &role); err != nil {
		return denproto.Role{}, err
	}
	if denproto.CheckRole(role) != nil {
		return denproto.Role{}, errors.New("the den's answer is malformed")
	}
	return role, nil
}

// RenameRole changes a custom role's name on a den, as staff.
func (m *Manager) RenameRole(ctx context.Context, denID, roleID, name string) (denproto.Role, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Role{}, err
	}
	if err := checkID("role", roleID); err != nil {
		return denproto.Role{}, err
	}
	name, err = denproto.CleanRoleName(name)
	if err != nil {
		return denproto.Role{}, inputError(err)
	}
	var role denproto.Role
	if err := c.call(ctx, http.MethodPatch, "/api/roles/"+roleID, denproto.DenRoleRequest{Name: name}, &role); err != nil {
		return denproto.Role{}, err
	}
	if denproto.CheckRole(role) != nil || role.ID != roleID {
		return denproto.Role{}, errors.New("the den's answer is malformed")
	}
	return role, nil
}

// DeleteRole removes a custom role from a den, as staff.
func (m *Manager) DeleteRole(ctx context.Context, denID, roleID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("role", roleID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/roles/"+roleID, nil, nil)
}

// SetMemberRoles replaces the custom roles a member holds on a den, as staff.
func (m *Manager) SetMemberRoles(ctx context.Context, denID, memberID string, roleIDs []string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("member", memberID); err != nil {
		return err
	}
	if roleIDs == nil {
		roleIDs = []string{}
	}
	if err := denproto.CheckRoleIDs(roleIDs); err != nil {
		return inputError(err)
	}
	return c.call(ctx, http.MethodPut, "/api/members/"+memberID+"/roles", denproto.MemberRolesRequest{RoleIDs: roleIDs}, nil)
}
