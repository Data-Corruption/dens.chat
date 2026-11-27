package den

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Custom roles restrict channel visibility. Staff (moderators and the owner)
// always see every channel and manage all roles. A non-staff member sees a
// role-gated channel when they hold at least one of its roles, and learns only
// about roles referenced by channels they can see.

var errRoleNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such role")

// channelVisibleSQL reports whether den_channels (aliased c) is visible to a
// member given SQL expressions for staff (bool) and member ID.
func channelVisibleSQL(staffExpr, memberExpr string) string {
	return `(` + staffExpr + ` OR (c.staff_only = 0 AND (` +
		`NOT EXISTS (SELECT 1 FROM den_channel_roles cr WHERE cr.channel_id = c.id) OR ` +
		`EXISTS (SELECT 1 FROM den_channel_roles cr JOIN den_member_roles mr ON mr.role_id = cr.role_id ` +
		`WHERE cr.channel_id = c.id AND mr.member_id = ` + memberExpr + `))))`
}

// eventVisibleSQL reports whether den_events (aliased e, left-joined with
// den_channels aliased c) is visible to a member given SQL expressions for
// staff (bool) and member ID.
func eventVisibleSQL(staffExpr, memberExpr string) string {
	return `(` + staffExpr + ` OR (` + eventStaffOnly + ` = 0 AND (` +
		`(e.channel_id IS NOT NULL AND (` +
		`NOT EXISTS (SELECT 1 FROM den_channel_roles cr WHERE cr.channel_id = e.channel_id) OR ` +
		`EXISTS (SELECT 1 FROM den_channel_roles cr JOIN den_member_roles mr ON mr.role_id = cr.role_id ` +
		`WHERE cr.channel_id = e.channel_id AND mr.member_id = ` + memberExpr + `))) OR ` +
		`(e.channel_id IS NULL AND (` +
		`NOT EXISTS (SELECT 1 FROM den_event_roles er WHERE er.event_id = e.id) OR ` +
		`EXISTS (SELECT 1 FROM den_event_roles er JOIN den_member_roles mr ON mr.role_id = er.role_id ` +
		`WHERE er.event_id = e.id AND mr.member_id = ` + memberExpr + `))))))`
}

// loadRoleSlots assigns a Hub bit slot for every existing role on den boot.
func (d *Den) loadRoleSlots(ctx context.Context) error {
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM den_roles ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		d.Hub.EnsureRole(id)
	}
	return rows.Err()
}

// parseRoleIDs checks and parses role IDs into ascending numeric order.
func parseRoleIDs(raw []string) ([]int64, error) {
	if err := denproto.CheckRoleIDs(raw); err != nil {
		return nil, invalid("role_ids: %v", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]int64, len(raw))
	for i, s := range raw {
		id, _ := denproto.ParseID(s)
		out[i] = id
	}
	slices.Sort(out)
	return out, nil
}

// channelMask returns the Hub bitmask for a channel's custom roles.
func (d *Den) channelMask(c denproto.Channel) uint64 {
	if len(c.RoleIDs) == 0 {
		return 0
	}
	ids := make([]int64, 0, len(c.RoleIDs))
	for _, s := range c.RoleIDs {
		if id, err := denproto.ParseID(s); err == nil {
			ids = append(ids, id)
		}
	}
	return d.Hub.Mask(ids)
}

// audienceOf returns the Hub audience for a channel.
func (d *Den) audienceOf(c denproto.Channel) Audience {
	switch {
	case c.Kind == denproto.KindNotes:
		if len(c.Members) != 1 {
			return nobody
		}
		member, _ := denproto.ParseID(c.Members[0])
		return notesOf(member)
	case c.Kind == denproto.KindDM && len(c.Members) == 2:
		a, _ := denproto.ParseID(c.Members[0])
		b, _ := denproto.ParseID(c.Members[1])
		return Pair(a, b)
	case c.StaffOnly:
		return Staff
	case len(c.RoleIDs) > 0:
		return Roles(d.channelMask(c))
	}
	return Everyone
}

// visibleMask reports whether a member with staff flag and custom role mask
// can see channel c.
func (d *Den) visibleMask(c denproto.Channel, member int64, staff bool, roles uint64) bool {
	switch {
	case isPrivate(c):
		return slices.Contains(c.Members, denproto.FormatID(member))
	case staff:
		return true
	case c.StaffOnly:
		return false
	case len(c.RoleIDs) > 0:
		return (d.channelMask(c) & roles) != 0
	}
	return true
}

// seesChannel reports whether member (staff or not) can see channel c, reading
// den_member_roles when c is role-gated.
func (d *Den) seesChannel(ctx context.Context, q querier, c denproto.Channel, member int64, staff bool) (bool, error) {
	switch {
	case isPrivate(c):
		return slices.Contains(c.Members, denproto.FormatID(member)), nil
	case staff:
		return true, nil
	case c.StaffOnly:
		return false, nil
	case len(c.RoleIDs) == 0:
		return true, nil
	}
	ids := make([]int64, 0, len(c.RoleIDs))
	for _, s := range c.RoleIDs {
		if id, err := denproto.ParseID(s); err == nil {
			ids = append(ids, id)
		}
	}
	return memberHasAnyRole(ctx, q, member, ids)
}

// sees reports whether session s sees channel c.
func (d *Den) sees(ctx context.Context, q querier, c denproto.Channel, s *Session) (bool, error) {
	if s.Version < kindVersion(c.Kind) {
		return false, nil
	}
	return d.seesChannel(ctx, q, c, s.MemberID, IsStaff(s.Role))
}

// memberHasAnyRole reports whether member holds at least one role in roleIDs.
func memberHasAnyRole(ctx context.Context, q querier, member int64, roleIDs []int64) (bool, error) {
	if len(roleIDs) == 0 {
		return false, nil
	}
	args := make([]any, 0, len(roleIDs)+1)
	args = append(args, member)
	for _, id := range roleIDs {
		args = append(args, id)
	}
	var ok bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_member_roles WHERE member_id = ? AND role_id IN (?`+
		strings.Repeat(", ?", len(roleIDs)-1)+`))`, args...).Scan(&ok)
	return ok, err
}

// memberRoleIDs returns a member's custom role IDs in ascending numeric order.
func memberRoleIDs(ctx context.Context, q querier, member int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT role_id FROM den_member_roles WHERE member_id = ? ORDER BY role_id`, member)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// memberRoleMask returns the Hub bitmask for a member's current custom roles.
func (d *Den) memberRoleMask(ctx context.Context, q querier, member int64) (uint64, error) {
	ids, err := memberRoleIDs(ctx, q, member)
	if err != nil {
		return 0, err
	}
	return d.Hub.Mask(ids), nil
}

// channelRoleIDs returns each channel's custom role IDs in ascending order.
func channelRoleIDs(ctx context.Context, q querier, channelIDs []int64) (map[int64][]string, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT channel_id, role_id FROM den_channel_roles WHERE channel_id IN (?`+
		strings.Repeat(", ?", len(channelIDs)-1)+`) ORDER BY channel_id, role_id`, anys(channelIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var cid, rid int64
		if err := rows.Scan(&cid, &rid); err != nil {
			return nil, err
		}
		out[cid] = append(out[cid], denproto.FormatID(rid))
	}
	return out, rows.Err()
}

// allRoles lists every custom role on the den, ordered by name then ID.
func allRoles(ctx context.Context, q querier) ([]denproto.Role, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name FROM den_roles ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []denproto.Role
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out = append(out, denproto.Role{ID: denproto.FormatID(id), Name: name})
	}
	return out, rows.Err()
}

// allMemberRoles maps each member ID to their custom role IDs in ascending order.
func allMemberRoles(ctx context.Context, q querier) (map[int64][]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT member_id, role_id FROM den_member_roles ORDER BY member_id, role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var mid, rid int64
		if err := rows.Scan(&mid, &rid); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], denproto.FormatID(rid))
	}
	return out, rows.Err()
}

// roleNamesMap loads role names by numeric ID for the given IDs.
func roleNamesMap(ctx context.Context, q querier, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id, name FROM den_roles WHERE id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`)`, anys(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// checkRolesExist checks that every ID in ids exists in den_roles.
func checkRolesExist(ctx context.Context, q querier, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	names, err := roleNamesMap(ctx, q, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := names[id]; !ok {
			return invalid("role_ids: no such role")
		}
	}
	return nil
}

// setChannelRoles replaces a channel's custom role links within tx.
func setChannelRoles(ctx context.Context, tx *sql.Tx, channel int64, roleIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM den_channel_roles WHERE channel_id = ?`, channel); err != nil {
		return err
	}
	for _, rid := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_channel_roles (channel_id, role_id) VALUES (?, ?)`, channel, rid); err != nil {
			return err
		}
	}
	return nil
}

// filterRoleIDs returns the subset of ids present in allowed.
func filterRoleIDs(ids []string, allowed map[string]bool) []string {
	if len(ids) == 0 || len(allowed) == 0 {
		return nil
	}
	var out []string
	for _, id := range ids {
		if allowed[id] {
			out = append(out, id)
		}
	}
	return out
}

// visibleRoleSet returns the role IDs visible to viewer (all roles for staff,
// or only those referenced by channels viewer can see).
func (d *Den) visibleRoleSet(ctx context.Context, q querier, viewer int64, staff bool) (map[string]bool, error) {
	if staff {
		roles, err := allRoles(ctx, q)
		if err != nil {
			return nil, err
		}
		out := make(map[string]bool, len(roles))
		for _, r := range roles {
			out[r.ID] = true
		}
		return out, nil
	}
	chans, err := d.channels(ctx, q, viewer, false)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, c := range chans {
		for _, id := range c.RoleIDs {
			out[id] = true
		}
	}
	return out, nil
}

// roleViewersMask returns the role IDs on any channel that references at least
// one role in roleIDs. Non-staff members holding any returned role can see a
// channel referencing one of roleIDs.
func roleViewerIDs(ctx context.Context, q querier, roleIDs []int64) ([]int64, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT cr2.role_id FROM den_channel_roles cr1
		JOIN den_channel_roles cr2 ON cr2.channel_id = cr1.channel_id
		WHERE cr1.role_id IN (?`+strings.Repeat(", ?", len(roleIDs)-1)+`)`, anys(roleIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CreateRole creates a custom role. Moderators and the owner may.
func (d *Den) CreateRole(ctx context.Context, s *Session, req denproto.DenRoleRequest) (denproto.Role, error) {
	if !IsStaff(s.Role) {
		return denproto.Role{}, forbidden("only moderators and the owner manage roles")
	}
	name, err := denproto.CleanRoleName(req.Name)
	if err != nil {
		return denproto.Role{}, invalid("name: %v", err)
	}
	d.roles.Lock()
	defer d.roles.Unlock()
	var id int64
	var role denproto.Role
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_roles`).Scan(&n); err != nil {
			return err
		}
		if n >= denproto.MaxRoles {
			return invalid("a den has at most %d roles", denproto.MaxRoles)
		}
		var taken bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_roles WHERE name = ? COLLATE NOCASE)`, name).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return denproto.Errorf(http.StatusConflict, denproto.CodeRoleNameTaken, "another role has that name")
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_roles (name, created_at) VALUES (?, ?)`, name, d.now().UnixMilli())
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		role = denproto.Role{ID: denproto.FormatID(id), Name: name}
		return d.audit(ctx, tx, auditLine{kind: denproto.AuditRoleCreated, by: s.MemberID,
			details: denproto.AuditDetails{Ref: role.ID, Name: role.Name}})
	})
	if err != nil {
		return denproto.Role{}, err
	}
	d.audited()
	d.Hub.EnsureRole(id)
	d.log.Infof("Member %d created role %d", s.MemberID, id)
	return role, d.Hub.Publish(denproto.EventRoleCreated, role, Staff)
}

// RenameRole renames a custom role. Moderators and the owner may.
func (d *Den) RenameRole(ctx context.Context, s *Session, id string, req denproto.DenRoleRequest) (denproto.Role, error) {
	if !IsStaff(s.Role) {
		return denproto.Role{}, forbidden("only moderators and the owner manage roles")
	}
	rid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Role{}, errRoleNotFound
	}
	name, err := denproto.CleanRoleName(req.Name)
	if err != nil {
		return denproto.Role{}, invalid("name: %v", err)
	}
	d.roles.Lock()
	defer d.roles.Unlock()
	var oldName string
	var viewers []int64
	changed := false
	err = d.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT name FROM den_roles WHERE id = ?`, rid).Scan(&oldName)
		if errors.Is(err, sql.ErrNoRows) {
			return errRoleNotFound
		}
		if err != nil {
			return err
		}
		if oldName == name {
			return nil
		}
		var taken bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_roles WHERE name = ? COLLATE NOCASE AND id != ?)`, name, rid).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return denproto.Errorf(http.StatusConflict, denproto.CodeRoleNameTaken, "another role has that name")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_roles SET name = ? WHERE id = ?`, name, rid); err != nil {
			return err
		}
		if viewers, err = roleViewerIDs(ctx, tx, []int64{rid}); err != nil {
			return err
		}
		changed = true
		return d.audit(ctx, tx, auditLine{kind: denproto.AuditRoleRenamed, by: s.MemberID,
			details: denproto.AuditDetails{Ref: denproto.FormatID(rid), Name: name,
				Changes: []denproto.AuditChange{{Field: "name", From: oldName, To: name}}}})
	})
	if err != nil {
		return denproto.Role{}, err
	}
	role := denproto.Role{ID: denproto.FormatID(rid), Name: name}
	if !changed {
		return role, nil
	}
	d.audited()
	d.log.Infof("Member %d renamed role %d", s.MemberID, rid)
	aud := Staff
	if len(viewers) > 0 {
		aud = Roles(d.Hub.Mask(viewers))
	}
	return role, d.Hub.Publish(denproto.EventRoleUpdated, role, aud)
}

// DeleteRole deletes a custom role, failing closed: any channel or ended event
// whose sole role was this one flips to staff_only = 1 in the same transaction.
// Affected voice callers are disconnected with VoiceForbidden and affected
// non-staff sockets receive a fresh snapshot.
func (d *Den) DeleteRole(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner manage roles")
	}
	rid, err := denproto.ParseID(id)
	if err != nil {
		return errRoleNotFound
	}
	d.roles.Lock()
	defer d.roles.Unlock()
	d.calendar.Lock()
	defer d.calendar.Unlock()
	now := d.now()
	var affectedMembers []int64
	var otherRoleIDs []int64
	var affectedChannels []denproto.Channel
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var roleName string
		err := tx.QueryRowContext(ctx, `SELECT name FROM den_roles WHERE id = ?`, rid).Scan(&roleName)
		if errors.Is(err, sql.ErrNoRows) {
			return errRoleNotFound
		}
		if err != nil {
			return err
		}
		// Load affected members BEFORE deleting the role (CASCADE clears them).
		rows, err := tx.QueryContext(ctx, `SELECT member_id FROM den_member_roles WHERE role_id = ? ORDER BY member_id`, rid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var mid int64
			if err := rows.Scan(&mid); err != nil {
				rows.Close()
				return err
			}
			affectedMembers = append(affectedMembers, mid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// Load other roles sharing a channel with this role.
		rows, err = tx.QueryContext(ctx, `SELECT DISTINCT cr2.role_id FROM den_channel_roles cr1
			JOIN den_channel_roles cr2 ON cr2.channel_id = cr1.channel_id
			WHERE cr1.role_id = ? AND cr2.role_id != ?`, rid, rid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var oid int64
			if err := rows.Scan(&oid); err != nil {
				rows.Close()
				return err
			}
			otherRoleIDs = append(otherRoleIDs, oid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// Load all channels referencing this role so we can update staff and calls.
		var affectedChanIDs []int64
		rows, err = tx.QueryContext(ctx, `SELECT channel_id FROM den_channel_roles WHERE role_id = ? ORDER BY channel_id`, rid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid int64
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return err
			}
			affectedChanIDs = append(affectedChanIDs, cid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// Find single-role channels that will flip to staff_only = 1 (fail closed).
		var flippedNames []string
		rows, err = tx.QueryContext(ctx, `SELECT c.name, c.kind FROM den_channels c
			WHERE EXISTS (SELECT 1 FROM den_channel_roles cr WHERE cr.channel_id = c.id AND cr.role_id = ?1)
			  AND NOT EXISTS (SELECT 1 FROM den_channel_roles cr WHERE cr.channel_id = c.id AND cr.role_id != ?1)
			ORDER BY c.id`, rid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cname, kind string
			if err := rows.Scan(&cname, &kind); err != nil {
				rows.Close()
				return err
			}
			if kind == denproto.KindText {
				flippedNames = append(flippedNames, "#"+cname)
			} else {
				flippedNames = append(flippedNames, cname)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `UPDATE den_channels SET staff_only = 1
			WHERE EXISTS (SELECT 1 FROM den_channel_roles cr WHERE cr.channel_id = den_channels.id AND cr.role_id = ?1)
			  AND NOT EXISTS (SELECT 1 FROM den_channel_roles cr WHERE cr.channel_id = den_channels.id AND cr.role_id != ?1)`, rid); err != nil {
			return err
		}

		// Ended events whose voice channel was deleted and whose sole role is
		// being deleted also flip to staff_only = 1 (fail closed).
		if _, err := tx.ExecContext(ctx, `UPDATE den_events SET staff_only = 1
			WHERE channel_id IS NULL
			  AND EXISTS (SELECT 1 FROM den_event_roles er WHERE er.event_id = den_events.id AND er.role_id = ?1)
			  AND NOT EXISTS (SELECT 1 FROM den_event_roles er WHERE er.event_id = den_events.id AND er.role_id != ?1)`, rid); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM den_roles WHERE id = ?`, rid); err != nil {
			return err
		}
		for _, mid := range affectedMembers {
			if err := logMember(ctx, tx, mid, now); err != nil {
				return err
			}
		}
		for _, cid := range affectedChanIDs {
			c, err := d.channel(ctx, tx, cid)
			if err != nil {
				return err
			}
			affectedChannels = append(affectedChannels, c)
		}
		line := auditLine{kind: denproto.AuditRoleDeleted, by: s.MemberID,
			details: denproto.AuditDetails{Ref: denproto.FormatID(rid), Name: roleName}}
		if len(flippedNames) > 0 {
			line.details.Changes = []denproto.AuditChange{{Field: "channels_staff_only", To: auditNames(flippedNames)}}
		}
		return d.audit(ctx, tx, line)
	})
	if err != nil {
		return err
	}
	d.audited()
	deletedBit := d.Hub.Mask([]int64{rid})
	otherMask := d.Hub.Mask(otherRoleIDs)
	d.roleDeletedInCalls(deletedBit, affectedChannels)
	d.log.Infof("Member %d deleted role %d", s.MemberID, rid)
	if err := d.Hub.Publish(denproto.EventRoleDeleted, denproto.RoleDeleted{ID: denproto.FormatID(rid)}, Staff); err != nil {
		return err
	}
	for _, c := range affectedChannels {
		if err := d.Hub.Publish(denproto.EventChannelUpdated, c, Staff); err != nil {
			return err
		}
		if c.Kind == denproto.KindVoice {
			cid, _ := denproto.ParseID(c.ID)
			if err := d.publishChannelEvents(ctx, cid); err != nil {
				return err
			}
		}
	}
	d.Hub.DeleteRole(rid, affectedMembers, otherMask)
	return nil
}

// SetMemberRoles replaces a member's custom roles. Moderators and the owner may.
func (d *Den) SetMemberRoles(ctx context.Context, s *Session, id string, req denproto.MemberRolesRequest) (denproto.Member, error) {
	if !IsStaff(s.Role) {
		return denproto.Member{}, forbidden("only moderators and the owner manage roles")
	}
	mid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Member{}, errMemberNotFound
	}
	newIDs, err := parseRoleIDs(req.RoleIDs)
	if err != nil {
		return denproto.Member{}, err
	}
	d.roles.Lock()
	defer d.roles.Unlock()
	now := d.now()
	var viewers []int64
	var eventChannels []int64
	changed := false
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_members WHERE id = ? AND left_at IS NULL)`, mid).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errMemberNotFound
		}
		if err := checkRolesExist(ctx, tx, newIDs); err != nil {
			return err
		}
		oldIDs, err := memberRoleIDs(ctx, tx, mid)
		if err != nil {
			return err
		}
		if slices.Equal(oldIDs, newIDs) {
			return nil
		}
		var added, removed []int64
		for _, rid := range newIDs {
			if !slices.Contains(oldIDs, rid) {
				added = append(added, rid)
			}
		}
		for _, rid := range oldIDs {
			if !slices.Contains(newIDs, rid) {
				removed = append(removed, rid)
			}
		}
		changedIDs := slices.Concat(added, removed)
		names, err := roleNamesMap(ctx, tx, changedIDs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_member_roles WHERE member_id = ?`, mid); err != nil {
			return err
		}
		for _, rid := range newIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO den_member_roles (member_id, role_id) VALUES (?, ?)`, mid, rid); err != nil {
				return err
			}
		}
		if err := logMember(ctx, tx, mid, now); err != nil {
			return err
		}
		if viewers, err = roleViewerIDs(ctx, tx, changedIDs); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT e.channel_id FROM den_event_interest i
			JOIN den_events e ON e.id = i.event_id
			JOIN den_channel_roles cr ON cr.channel_id = e.channel_id
			WHERE i.member_id = ? AND e.state != 'ended' AND cr.role_id IN (?`+strings.Repeat(", ?", len(changedIDs)-1)+`)`,
			append([]any{mid}, anys(changedIDs)...)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid int64
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return err
			}
			eventChannels = append(eventChannels, cid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var auditChanges []denproto.AuditChange
		if len(added) > 0 {
			addedNames := make([]string, 0, len(added))
			for _, rid := range added {
				addedNames = append(addedNames, names[rid])
			}
			auditChanges = append(auditChanges, denproto.AuditChange{Field: "roles_added", To: auditNames(addedNames)})
		}
		if len(removed) > 0 {
			removedNames := make([]string, 0, len(removed))
			for _, rid := range removed {
				removedNames = append(removedNames, names[rid])
			}
			auditChanges = append(auditChanges, denproto.AuditChange{Field: "roles_removed", To: auditNames(removedNames)})
		}
		changed = true
		return d.audit(ctx, tx, auditLine{kind: denproto.AuditMemberRolesChanged, by: s.MemberID, member: mid,
			details: denproto.AuditDetails{Changes: auditChanges}})
	})
	if err != nil {
		return denproto.Member{}, err
	}
	m, err := d.Member(ctx, mid)
	if err != nil {
		return m, err
	}
	m.RoleIDs = formatIDs(newIDs)
	if !changed {
		return m, nil
	}
	d.audited()
	newMask := d.Hub.Mask(newIDs)
	d.memberRolesChanged(mid, newMask)
	d.log.Infof("Member %d updated roles of member %d", s.MemberID, mid)
	if err := d.Hub.Publish(denproto.EventMemberRolesUpdated, denproto.MemberRoles{
		MemberID: m.ID, RoleIDs: m.RoleIDs,
	}, Staff); err != nil {
		return m, err
	}
	d.Hub.SetMemberRoles(mid, newMask)
	if viewersMask := d.Hub.Mask(viewers); viewersMask != 0 {
		d.Hub.RefreshChannelVisibility(false, false, 0, viewersMask)
	}
	for _, cid := range eventChannels {
		if err := d.publishChannelEvents(ctx, cid); err != nil {
			return m, err
		}
	}
	return m, nil
}
