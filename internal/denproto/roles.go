package denproto

import (
	"errors"
	"fmt"
	"slices"
)

// Custom roles restrict which members can see a channel. Staff (moderators and
// the owner) manage roles and always see every channel.
const (
	// MaxRoles is the most custom roles a den may have.
	MaxRoles = 50

	// CodeRoleNameTaken reports a role name already used on the den
	// (compared case-insensitively).
	CodeRoleNameTaken = "role_name_taken"

	// Event types for custom roles.
	EventRoleCreated        = "role.created"
	EventRoleUpdated        = "role.updated"
	EventRoleDeleted        = "role.deleted"
	EventMemberRolesUpdated = "member.roles"
)

// Role is a custom role on a den.
type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DenRoleRequest creates or renames a custom role.
type DenRoleRequest struct {
	Name string `json:"name"`
}

// RoleDeleted names a deleted custom role.
type RoleDeleted struct {
	ID string `json:"id"`
}

// MemberRolesRequest replaces a member's custom roles.
type MemberRolesRequest struct {
	RoleIDs []string `json:"role_ids"`
}

// MemberRoles is a member's custom roles after staff change them.
type MemberRoles struct {
	MemberID string   `json:"member_id"`
	RoleIDs  []string `json:"role_ids,omitempty"`
}

// CleanRoleName strips hidden characters and checks length (1 to 32 runes).
func CleanRoleName(raw string) (string, error) {
	return CleanName(raw, MaxNameRunes)
}

// CheckRole checks a role from a den before a client keeps it.
func CheckRole(r Role) error {
	if _, err := ParseID(r.ID); err != nil {
		return errors.New("role has an invalid ID")
	}
	clean, err := CleanRoleName(r.Name)
	if err != nil || clean != r.Name {
		return errors.New("role has an invalid name")
	}
	return nil
}

// CheckRoleIDs checks a list of role IDs: within MaxRoles, valid IDs, no
// duplicates.
func CheckRoleIDs(ids []string) error {
	if len(ids) > MaxRoles {
		return fmt.Errorf("at most %d roles", MaxRoles)
	}
	for i, id := range ids {
		if _, err := ParseID(id); err != nil {
			return errors.New("invalid role ID")
		}
		if slices.Contains(ids[:i], id) {
			return errors.New("duplicate role ID")
		}
	}
	return nil
}
