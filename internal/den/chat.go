package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// nonceWindow is how long a send's nonce makes a retry return the original.
const nonceWindow = 10 * time.Minute

// IsStaff reports whether a role sees staff-only channels.
func IsStaff(role string) bool { return role == denproto.RoleModerator || role == denproto.RoleOwner }

func textAD(id int64) []byte { return []byte("den_messages.text:" + strconv.FormatInt(id, 10)) }

// hasMessages reports whether a channel holds messages: text channels and
// DMs do; voice channels don't.
func hasMessages(c denproto.Channel) bool {
	return c.Kind == denproto.KindText || c.Kind == denproto.KindDM
}

var (
	errChannelNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such channel")
	errMessageNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such message")
	errStaffOnly       = denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, "only moderators and the owner manage channels")
)

func invalid(format string, args ...any) error {
	return denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, format, args...)
}

// Snapshot --------------------------------------------------------------------

// snapshot is the state a member's client starts from.
func (d *Den) snapshot(ctx context.Context, member int64, staff bool, seq uint64) (denproto.Ready, error) {
	r := denproto.Ready{Epoch: d.Hub.Epoch(), Seq: seq, Members: []denproto.Member{}, Groups: []denproto.Group{},
		Channels: []denproto.Channel{}, ReadStates: []denproto.ReadState{}}
	r.Den, _ = d.Info()
	rows, err := d.db.QueryContext(ctx, `SELECT `+memberColumns+` FROM den_members ORDER BY id`)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		m, err := scanMember(rows.Scan)
		if err != nil {
			rows.Close()
			return r, err
		}
		if m.ID == denproto.FormatID(member) {
			r.Me = m
		}
		r.Members = append(r.Members, m)
	}
	rows.Close()
	r.Online = d.online()
	r.Calls = d.visibleCalls(staff)
	if r.Groups, err = d.groups(ctx, d.db); err != nil {
		return r, err
	}
	if r.Channels, err = d.channels(ctx, d.db, staff); err != nil {
		return r, err
	}
	dms, err := d.dms(ctx, d.db, member)
	if err != nil {
		return r, err
	}
	r.Channels = append(r.Channels, dms...)
	last := map[string]string{}
	rows, err = d.db.QueryContext(ctx, `SELECT channel_id, max(id) FROM den_messages GROUP BY channel_id`)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var ch, id int64
		if err := rows.Scan(&ch, &id); err != nil {
			rows.Close()
			return r, err
		}
		last[denproto.FormatID(ch)] = denproto.FormatID(id)
	}
	rows.Close()
	read := map[string]string{}
	closed := map[string]bool{}
	rows, err = d.db.QueryContext(ctx, `SELECT channel_id, message_id, closed FROM den_read_states WHERE member_id = ?`, member)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var ch, id int64
		var shut bool
		if err := rows.Scan(&ch, &id, &shut); err != nil {
			rows.Close()
			return r, err
		}
		// A DM closed before it was read has a position of 0: none yet.
		if id > 0 {
			read[denproto.FormatID(ch)] = denproto.FormatID(id)
		}
		closed[denproto.FormatID(ch)] = shut
	}
	rows.Close()
	mentions := map[string]int{}
	rows, err = d.db.QueryContext(ctx, `
		SELECT m.channel_id, count(*) FROM den_mentions m
		LEFT JOIN den_read_states r ON r.member_id = m.member_id AND r.channel_id = m.channel_id
		WHERE m.member_id = ? AND m.message_id > coalesce(r.message_id, 0)
		GROUP BY m.channel_id`, member)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var ch int64
		var n int
		if err := rows.Scan(&ch, &n); err != nil {
			rows.Close()
			return r, err
		}
		mentions[denproto.FormatID(ch)] = n
	}
	rows.Close()
	for _, ch := range r.Channels {
		if !hasMessages(ch) {
			continue
		}
		r.ReadStates = append(r.ReadStates, denproto.ReadState{
			ChannelID: ch.ID, LastMessage: last[ch.ID], ReadPosition: read[ch.ID], MentionCount: mentions[ch.ID], Closed: closed[ch.ID],
		})
	}
	if err := d.db.QueryRowContext(ctx, `SELECT seal_check FROM den_members WHERE id = ?`, member).Scan(&r.SealCheck); err != nil {
		return r, err
	}
	if r.DMKeys, err = memberKeys(ctx, d.db, member); err != nil {
		return r, err
	}
	r.DeviceRequests = d.memberRequests(member)
	return r, nil
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (d *Den) groups(ctx context.Context, q querier) ([]denproto.Group, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name, position FROM den_groups ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []denproto.Group{}
	for rows.Next() {
		var g denproto.Group
		var id int64
		if err := rows.Scan(&id, &g.Name, &g.Position); err != nil {
			return nil, err
		}
		g.ID = denproto.FormatID(id)
		out = append(out, g)
	}
	return out, rows.Err()
}

const channelColumns = `id, group_id, name, description, kind, position, staff_only, dm_low, dm_high`

func scanChannel(scan func(...any) error) (denproto.Channel, error) {
	var c denproto.Channel
	var id int64
	var group, low, high sql.NullInt64
	err := scan(&id, &group, &c.Name, &c.Description, &c.Kind, &c.Position, &c.StaffOnly, &low, &high)
	c.ID = denproto.FormatID(id)
	if low.Valid && high.Valid {
		c.Members = []string{denproto.FormatID(low.Int64), denproto.FormatID(high.Int64)}
	}
	if group.Valid {
		g := denproto.FormatID(group.Int64)
		c.GroupID = &g
	}
	return c, err
}

// channels lists the channels a member can see, without DMs.
func (d *Den) channels(ctx context.Context, q querier, staff bool) ([]denproto.Channel, error) {
	return d.queryChannels(ctx, q, `SELECT `+channelColumns+` FROM den_channels
		WHERE kind != 'dm' AND (staff_only = 0 OR ?) ORDER BY coalesce(group_id, 0), position, id`, staff)
}

// dms lists a member's DMs.
func (d *Den) dms(ctx context.Context, q querier, member int64) ([]denproto.Channel, error) {
	return d.queryChannels(ctx, q, `SELECT `+channelColumns+` FROM den_channels
		WHERE kind = 'dm' AND (dm_low = ?1 OR dm_high = ?1) ORDER BY id`, member)
}

func (d *Den) queryChannels(ctx context.Context, q querier, query string, args ...any) ([]denproto.Channel, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []denproto.Channel{}
	for rows.Next() {
		c, err := scanChannel(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *Den) channel(ctx context.Context, q querier, id int64) (denproto.Channel, error) {
	c, err := scanChannel(q.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM den_channels WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return c, errChannelNotFound
	}
	return c, err
}

// visibleChannel returns a channel the session can see, or not found.
func (d *Den) visibleChannel(ctx context.Context, s *Session, id string) (denproto.Channel, int64, error) {
	cid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Channel{}, 0, errChannelNotFound
	}
	c, err := d.channel(ctx, d.db, cid)
	if err != nil {
		return c, 0, err
	}
	if !visible(c, s.MemberID, IsStaff(s.Role)) {
		return c, 0, errChannelNotFound
	}
	return c, cid, nil
}

// visible reports whether a member can see a channel: a DM only its two
// members, a staff-only channel only staff, and any other channel anyone.
func visible(c denproto.Channel, member int64, staff bool) bool {
	switch {
	case c.Kind == denproto.KindDM:
		return slices.Contains(c.Members, denproto.FormatID(member))
	case c.StaffOnly:
		return staff
	}
	return true
}

func audienceOf(c denproto.Channel) Audience {
	switch {
	case c.Kind == denproto.KindDM && len(c.Members) == 2:
		a, _ := denproto.ParseID(c.Members[0])
		b, _ := denproto.ParseID(c.Members[1])
		return Pair(a, b)
	case c.StaffOnly:
		return Staff
	}
	return Everyone
}

// Ordering --------------------------------------------------------------------

// siblings lists a channel list (a group's, or the ungrouped one) in order.
func siblings(ctx context.Context, tx *sql.Tx, group *int64) ([]int64, error) {
	var rows *sql.Rows
	var err error
	if group == nil {
		rows, err = tx.QueryContext(ctx, `SELECT id FROM den_channels WHERE group_id IS NULL AND kind != 'dm' ORDER BY position, id`)
	} else {
		rows, err = tx.QueryContext(ctx, `SELECT id FROM den_channels WHERE group_id = ? ORDER BY position, id`, *group)
	}
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

// insertAt puts id into ids at position (clamped; nil appends).
func insertAt(ids []int64, id int64, position *int) []int64 {
	at := len(ids)
	if position != nil {
		at = max(0, min(*position, len(ids)))
	}
	ids = append(ids, 0)
	copy(ids[at+1:], ids[at:])
	ids[at] = id
	return ids
}

func without(ids []int64, id int64) []int64 {
	out := ids[:0:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func renumber(ctx context.Context, tx *sql.Tx, table string, ids []int64) error {
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET position = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return nil
}

// reorderedList is a channel list's new order, to announce.
type reorderedList struct {
	group *int64
	ids   []int64
}

// publishReorders announces channel lists twice: in full to staff, and
// without staff-only channels to everyone else, so members never learn a
// hidden channel exists.
func (d *Den) publishReorders(ctx context.Context, lists []reorderedList) error {
	hidden := map[int64]bool{}
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM den_channels WHERE staff_only = 1`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		hidden[id] = true
	}
	rows.Close()
	for _, l := range lists {
		var group *string
		if l.group != nil {
			g := denproto.FormatID(*l.group)
			group = &g
		}
		all, public := []string{}, []string{}
		for _, id := range l.ids {
			all = append(all, denproto.FormatID(id))
			if !hidden[id] {
				public = append(public, denproto.FormatID(id))
			}
		}
		if err := d.Hub.Publish(denproto.EventChannelsReordered, denproto.ChannelsReordered{GroupID: group, ChannelIDs: all}, Staff); err != nil {
			return err
		}
		if err := d.Hub.Publish(denproto.EventChannelsReordered, denproto.ChannelsReordered{GroupID: group, ChannelIDs: public}, NonStaff); err != nil {
			return err
		}
	}
	return nil
}

func sameGroup(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Channels --------------------------------------------------------------------

func (d *Den) checkChannelFields(ctx context.Context, req denproto.ChannelRequest, kind string) (group *int64, err error) {
	if req.Name != nil {
		if *req.Name, err = denproto.CleanName(*req.Name, denproto.MaxNameRunes); err != nil {
			return nil, invalid("name: %v", err)
		}
	}
	if req.Description != nil {
		if kind != denproto.KindText && *req.Description != "" {
			return nil, invalid("only text channels have descriptions")
		}
		*req.Description = denproto.CleanLinks(*req.Description)
		if err := denproto.CheckDescription(*req.Description); err != nil {
			return nil, invalid("description: %v", err)
		}
	}
	if req.GroupID != nil && *req.GroupID != "" {
		g, err := denproto.ParseID(*req.GroupID)
		if err != nil {
			return nil, invalid("no such group")
		}
		var n int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM den_groups WHERE id = ?`, g).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, invalid("no such group")
		}
		group = &g
	}
	return group, nil
}

// CreateChannel adds a channel. Only the owner may until roles land.
func (d *Den) CreateChannel(ctx context.Context, s *Session, req denproto.ChannelRequest) (denproto.Channel, error) {
	if !IsStaff(s.Role) {
		return denproto.Channel{}, errStaffOnly
	}
	kind := req.Kind
	if kind == "" {
		kind = denproto.KindText
	}
	if kind != denproto.KindText && kind != denproto.KindVoice {
		return denproto.Channel{}, invalid("kind must be text or voice")
	}
	if req.Name == nil {
		return denproto.Channel{}, invalid("a channel needs a name")
	}
	group, err := d.checkChannelFields(ctx, req, kind)
	if err != nil {
		return denproto.Channel{}, err
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	staffOnly := req.StaffOnly != nil && *req.StaffOnly
	var c denproto.Channel
	var order []int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_channels WHERE kind != 'dm'`).Scan(&n); err != nil {
			return err
		}
		if n >= denproto.MaxChannels {
			return invalid("a den has at most %d channels", denproto.MaxChannels)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_channels (group_id, name, description, kind, position, staff_only, created_at)
			VALUES (?, ?, ?, ?, 0, ?, ?)`, group, *req.Name, description, kind, staffOnly, d.now().UnixMilli())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		ids, err := siblings(ctx, tx, group)
		if err != nil {
			return err
		}
		order = insertAt(without(ids, id), id, req.Position)
		if err := renumber(ctx, tx, "den_channels", order); err != nil {
			return err
		}
		c, err = d.channel(ctx, tx, id)
		return err
	})
	if err != nil {
		return c, err
	}
	if err := d.Hub.Publish(denproto.EventChannelCreated, c, audienceOf(c)); err != nil {
		return c, err
	}
	return c, d.publishReorders(ctx, []reorderedList{{group, order}})
}

// UpdateChannel changes a channel: its name, description, group,
// position or visibility.
func (d *Den) UpdateChannel(ctx context.Context, s *Session, id string, req denproto.ChannelRequest) (denproto.Channel, error) {
	if !IsStaff(s.Role) {
		return denproto.Channel{}, errStaffOnly
	}
	cid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Channel{}, errChannelNotFound
	}
	before, err := d.channel(ctx, d.db, cid)
	if err != nil {
		return before, err
	}
	if before.Kind == denproto.KindDM {
		return denproto.Channel{}, errChannelNotFound
	}
	group, err := d.checkChannelFields(ctx, req, before.Kind)
	if err != nil {
		return before, err
	}
	var oldGroup *int64
	if before.GroupID != nil {
		g, _ := denproto.ParseID(*before.GroupID)
		oldGroup = &g
	}
	newGroup := oldGroup
	if req.GroupID != nil {
		newGroup = group
	}
	moving := !sameGroup(oldGroup, newGroup) || req.Position != nil
	var after denproto.Channel
	var lists []reorderedList
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if req.Name != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE den_channels SET name = ? WHERE id = ?`, *req.Name, cid); err != nil {
				return err
			}
		}
		if req.Description != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE den_channels SET description = ? WHERE id = ?`, *req.Description, cid); err != nil {
				return err
			}
		}
		if req.StaffOnly != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE den_channels SET staff_only = ? WHERE id = ?`, *req.StaffOnly, cid); err != nil {
				return err
			}
		}
		if moving {
			if _, err := tx.ExecContext(ctx, `UPDATE den_channels SET group_id = ? WHERE id = ?`, newGroup, cid); err != nil {
				return err
			}
			if !sameGroup(oldGroup, newGroup) {
				old, err := siblings(ctx, tx, oldGroup)
				if err != nil {
					return err
				}
				old = without(old, cid)
				if err := renumber(ctx, tx, "den_channels", old); err != nil {
					return err
				}
				lists = append(lists, reorderedList{oldGroup, old})
			}
			ids, err := siblings(ctx, tx, newGroup)
			if err != nil {
				return err
			}
			position := req.Position
			if position == nil && sameGroup(oldGroup, newGroup) {
				position = &before.Position
			}
			order := insertAt(without(ids, cid), cid, position)
			if err := renumber(ctx, tx, "den_channels", order); err != nil {
				return err
			}
			lists = append(lists, reorderedList{newGroup, order})
		}
		after, err = d.channel(ctx, tx, cid)
		return err
	})
	if err != nil {
		return after, err
	}
	if before.StaffOnly != after.StaffOnly {
		d.channelChanged(after, false)
		// Members who could see the channel lose it, or members who
		// couldn't gain it with its history: a fresh snapshot covers both.
		if err := d.Hub.Publish(denproto.EventChannelUpdated, after, Staff); err != nil {
			return after, err
		}
		if err := d.publishReorders(ctx, lists); err != nil {
			return after, err
		}
		d.Hub.RefreshNonStaff()
		return after, nil
	}
	if err := d.Hub.Publish(denproto.EventChannelUpdated, after, audienceOf(after)); err != nil {
		return after, err
	}
	return after, d.publishReorders(ctx, lists)
}

// DeleteChannel removes a channel and all its messages.
func (d *Den) DeleteChannel(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return errStaffOnly
	}
	cid, err := denproto.ParseID(id)
	if err != nil {
		return errChannelNotFound
	}
	c, err := d.channel(ctx, d.db, cid)
	if err != nil {
		return err
	}
	if c.Kind == denproto.KindDM {
		return errChannelNotFound
	}
	var group *int64
	if c.GroupID != nil {
		g, _ := denproto.ParseID(*c.GroupID)
		group = &g
	}
	var order []int64
	var blobs [][]byte
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if _, blobs, err = filesOf(ctx, tx, `m.channel_id = ?`, cid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_channels WHERE id = ?`, cid); err != nil {
			return err
		}
		ids, err := siblings(ctx, tx, group)
		if err != nil {
			return err
		}
		order = ids
		return renumber(ctx, tx, "den_channels", order)
	})
	if err != nil {
		return err
	}
	d.files.remove(blobs)
	d.channelChanged(c, true)
	if err := d.Hub.Publish(denproto.EventChannelDeleted, denproto.ChannelDeleted{ID: c.ID}, audienceOf(c)); err != nil {
		return err
	}
	return d.publishReorders(ctx, []reorderedList{{group, order}})
}

// Groups ----------------------------------------------------------------------

func groupOrder(ctx context.Context, tx *sql.Tx) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM den_groups ORDER BY position, id`)
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

func (d *Den) publishGroupOrder(order []int64) error {
	ids := make([]string, len(order))
	for i, id := range order {
		ids[i] = denproto.FormatID(id)
	}
	return d.Hub.Publish(denproto.EventGroupsReordered, denproto.GroupsReordered{GroupIDs: ids}, Everyone)
}

func (d *Den) group(ctx context.Context, q querier, id int64) (denproto.Group, error) {
	g := denproto.Group{ID: denproto.FormatID(id)}
	err := q.QueryRowContext(ctx, `SELECT name, position FROM den_groups WHERE id = ?`, id).Scan(&g.Name, &g.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return g, denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such group")
	}
	return g, err
}

// CreateGroup adds a channel group.
func (d *Den) CreateGroup(ctx context.Context, s *Session, req denproto.GroupRequest) (denproto.Group, error) {
	if !IsStaff(s.Role) {
		return denproto.Group{}, errStaffOnly
	}
	if req.Name == nil {
		return denproto.Group{}, invalid("a group needs a name")
	}
	name, err := denproto.CleanName(*req.Name, denproto.MaxNameRunes)
	if err != nil {
		return denproto.Group{}, invalid("name: %v", err)
	}
	var g denproto.Group
	var order []int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		ids, err := groupOrder(ctx, tx)
		if err != nil {
			return err
		}
		if len(ids) >= denproto.MaxGroups {
			return invalid("a den has at most %d groups", denproto.MaxGroups)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_groups (name, position) VALUES (?, 0)`, name)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		order = insertAt(ids, id, req.Position)
		if err := renumber(ctx, tx, "den_groups", order); err != nil {
			return err
		}
		g, err = d.group(ctx, tx, id)
		return err
	})
	if err != nil {
		return g, err
	}
	if err := d.Hub.Publish(denproto.EventGroupCreated, g, Everyone); err != nil {
		return g, err
	}
	return g, d.publishGroupOrder(order)
}

// UpdateGroup renames or moves a group.
func (d *Den) UpdateGroup(ctx context.Context, s *Session, id string, req denproto.GroupRequest) (denproto.Group, error) {
	if !IsStaff(s.Role) {
		return denproto.Group{}, errStaffOnly
	}
	gid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Group{}, denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such group")
	}
	if req.Name != nil {
		if *req.Name, err = denproto.CleanName(*req.Name, denproto.MaxNameRunes); err != nil {
			return denproto.Group{}, invalid("name: %v", err)
		}
	}
	var g denproto.Group
	var order []int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if _, err := d.group(ctx, tx, gid); err != nil {
			return err
		}
		if req.Name != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE den_groups SET name = ? WHERE id = ?`, *req.Name, gid); err != nil {
				return err
			}
		}
		if req.Position != nil {
			ids, err := groupOrder(ctx, tx)
			if err != nil {
				return err
			}
			order = insertAt(without(ids, gid), gid, req.Position)
			if err := renumber(ctx, tx, "den_groups", order); err != nil {
				return err
			}
		}
		g, err = d.group(ctx, tx, gid)
		return err
	})
	if err != nil {
		return g, err
	}
	if err := d.Hub.Publish(denproto.EventGroupUpdated, g, Everyone); err != nil {
		return g, err
	}
	if order != nil {
		return g, d.publishGroupOrder(order)
	}
	return g, nil
}

// DeleteGroup removes a group. Its channels move to the end of the
// ungrouped list.
func (d *Den) DeleteGroup(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return errStaffOnly
	}
	gid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such group")
	}
	var groups, ungrouped []int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if _, err := d.group(ctx, tx, gid); err != nil {
			return err
		}
		// Take both lists before the delete: ON DELETE SET NULL ungroups
		// the moved channels with positions that collide with the others.
		moved, err := siblings(ctx, tx, &gid)
		if err != nil {
			return err
		}
		existing, err := siblings(ctx, tx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_groups WHERE id = ?`, gid); err != nil {
			return err
		}
		ungrouped = append(existing, moved...)
		if err := renumber(ctx, tx, "den_channels", ungrouped); err != nil {
			return err
		}
		if groups, err = groupOrder(ctx, tx); err != nil {
			return err
		}
		return renumber(ctx, tx, "den_groups", groups)
	})
	if err != nil {
		return err
	}
	if err := d.Hub.Publish(denproto.EventGroupDeleted, denproto.ChannelDeleted{ID: denproto.FormatID(gid)}, Everyone); err != nil {
		return err
	}
	if err := d.publishReorders(ctx, []reorderedList{{nil, ungrouped}}); err != nil {
		return err
	}
	return d.publishGroupOrder(groups)
}

// Messages --------------------------------------------------------------------

const messageColumns = `id, channel_id, author_id, created_at, revision, edited_at, edited_by, text, reply_to, nonce, key_id`

func (d *Den) scanMessage(scan func(...any) error) (denproto.Message, error) {
	var m denproto.Message
	var id, channel, author int64
	var editedAt, editedBy, replyTo, keyID sql.NullInt64
	var text, nonce []byte
	if err := scan(&id, &channel, &author, &m.CreatedAt, &m.Revision, &editedAt, &editedBy, &text, &replyTo, &nonce, &keyID); err != nil {
		return m, err
	}
	m.ID, m.ChannelID, m.AuthorID = denproto.FormatID(id), denproto.FormatID(channel), denproto.FormatID(author)
	if keyID.Valid {
		// A DM's text is sealed by its members' clients. It goes out as it
		// came, with what opening it takes: its key and its nonce.
		m.Sealed, m.KeyID, m.Nonce = text, denproto.FormatID(keyID.Int64), nonce
	} else {
		plain, err := d.v.Open(text, textAD(id))
		if err != nil {
			return m, err
		}
		m.Text = string(plain)
	}
	if editedAt.Valid {
		m.EditedAt = editedAt.Int64
	}
	if editedBy.Valid {
		m.EditedBy = denproto.FormatID(editedBy.Int64)
	}
	if replyTo.Valid {
		m.ReplyTo = denproto.FormatID(replyTo.Int64)
	}
	return m, nil
}

// queryMessages returns the messages a query selects, with previews of the
// messages they reply to.
func (d *Den) queryMessages(ctx context.Context, query string, args ...any) ([]denproto.Message, error) {
	ms, err := d.scanMessages(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, m := range ms {
		if id, err := denproto.ParseID(m.ReplyTo); err == nil {
			ids = append(ids, id)
		}
	}
	replies, err := d.replies(ctx, ids)
	if err != nil {
		return nil, err
	}
	mids := make([]int64, len(ms))
	for i, m := range ms {
		mids[i], _ = denproto.ParseID(m.ID)
	}
	files, err := d.attachments(ctx, d.db, mids)
	if err != nil {
		return nil, err
	}
	editors, err := messageEditors(ctx, d.db, mids)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		ms[i].Reply = replies[ms[i].ReplyTo]
		ms[i].Editors = editors[mids[i]]
		// A DM message's files are listed inside it, with their keys.
		if ms[i].Sealed == nil {
			ms[i].Attachments = files[mids[i]]
		}
	}
	return ms, nil
}

// messageEditors returns the editors of the messages with the given IDs,
// by message, in the order of their IDs.
func messageEditors(ctx context.Context, q querier, ids []int64) (map[int64][]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, `SELECT message_id, member_id FROM den_message_editors WHERE message_id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`) ORDER BY message_id, member_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var message, member int64
		if err := rows.Scan(&message, &member); err != nil {
			return nil, err
		}
		out[message] = append(out[message], denproto.FormatID(member))
	}
	return out, rows.Err()
}

// checkEditors resolves the members an author names to edit a message with
// them: at most MaxEditors others, each in the den now and able to see the
// channel. It returns them in the order of their IDs.
func (d *Den) checkEditors(ctx context.Context, ids []string, c denproto.Channel, author int64) ([]int64, error) {
	if len(ids) > denproto.MaxEditors {
		return nil, invalid("editors: at most %d members", denproto.MaxEditors)
	}
	out := make([]int64, 0, len(ids))
	for _, s := range ids {
		id, err := denproto.ParseID(s)
		if err != nil || id == author || slices.Contains(out, id) {
			return nil, invalid("editors: each must be another member, once")
		}
		var role string
		err = d.db.QueryRowContext(ctx, `SELECT role FROM den_members WHERE id = ? AND left_at IS NULL AND banned_at IS NULL`, id).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !visible(c, id, IsStaff(role)) {
			return nil, invalid("editors: member %s can't see this channel", s)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return out, nil
}

// setEditors replaces a message's editors within tx.
func setEditors(ctx context.Context, tx *sql.Tx, message int64, editors []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM den_message_editors WHERE message_id = ?`, message); err != nil {
		return err
	}
	for _, e := range editors {
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_message_editors (message_id, member_id) VALUES (?, ?)`, message, e); err != nil {
			return err
		}
	}
	return nil
}

func formatIDs(ids []int64) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = denproto.FormatID(id)
	}
	return out
}

// mayEdit reports whether a member may edit a message: its author, or one
// of the members they named.
func mayEdit(m denproto.Message, member int64) bool {
	id := denproto.FormatID(member)
	return m.AuthorID == id || slices.Contains(m.Editors, id)
}

// replies returns previews of the messages with the given IDs, by ID.
// Deleted messages have none.
func (d *Den) replies(ctx context.Context, ids []int64) (map[string]*denproto.Reply, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	ms, err := d.scanMessages(ctx, `SELECT `+messageColumns+` FROM den_messages WHERE id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*denproto.Reply, len(ms))
	for _, m := range ms {
		out[m.ID] = replyOf(m)
	}
	return out, nil
}

// replyOf previews a message for the replies to it: the start of its
// text, or in a DM, the whole of it sealed, for the client to open.
func replyOf(m denproto.Message) *denproto.Reply {
	if m.Sealed != nil {
		return &denproto.Reply{AuthorID: m.AuthorID, Sealed: m.Sealed, KeyID: m.KeyID, Nonce: m.Nonce, Revision: m.Revision}
	}
	return &denproto.Reply{AuthorID: m.AuthorID, Text: denproto.Excerpt(m.Text)}
}

func (d *Den) scanMessages(ctx context.Context, query string, args ...any) ([]denproto.Message, error) {
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []denproto.Message
	for rows.Next() {
		m, err := d.scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func reverse(ms []denproto.Message) {
	for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
		ms[i], ms[j] = ms[j], ms[i]
	}
}

// historyBudget bounds the messages in a history page, as JSON, so the
// page stays within a response's 1 MiB with room to spare.
const historyBudget = 896 << 10

// fit keeps the messages from the front of ms that fit in budget bytes of
// JSON, at least one, and reports whether it left any out.
func fit(ms []denproto.Message, budget int) ([]denproto.Message, bool, error) {
	used := 0
	for i, m := range ms {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, false, err
		}
		if used += len(b) + 1; used > budget && i > 0 {
			return ms[:i], true, nil
		}
	}
	return ms, false, nil
}

// HistoryQuery selects a page: the newest, or before, after or around an ID.
type HistoryQuery struct {
	Before, After, Around int64
	Limit                 int
}

// History returns a page of a channel's messages, oldest first.
func (d *Den) History(ctx context.Context, s *Session, channelID string, q HistoryQuery) (denproto.History, error) {
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return denproto.History{}, err
	}
	if !hasMessages(c) {
		return denproto.History{Messages: []denproto.Message{}}, nil
	}
	n := q.Limit
	if n == 0 {
		n = denproto.DefaultHistory
	}
	if n < 1 || n > denproto.MaxHistory {
		return denproto.History{}, invalid("limit must be 1 to %d", denproto.MaxHistory)
	}
	// Each side of the anchor gets its share of the page's bytes, and
	// keeps the messages nearest the anchor that fit.
	older := func(below int64, limit, budget int) ([]denproto.Message, bool, error) {
		ms, err := d.queryMessages(ctx, `SELECT `+messageColumns+` FROM den_messages
			WHERE channel_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, cid, below, limit+1)
		if err != nil {
			return nil, false, err
		}
		more := len(ms) > limit
		if more {
			ms = ms[:limit]
		}
		ms, cut, err := fit(ms, budget)
		reverse(ms)
		return ms, more || cut, err
	}
	newer := func(from int64, limit, budget int) ([]denproto.Message, bool, error) {
		ms, err := d.queryMessages(ctx, `SELECT `+messageColumns+` FROM den_messages
			WHERE channel_id = ? AND id >= ? ORDER BY id LIMIT ?`, cid, from, limit+1)
		if err != nil {
			return nil, false, err
		}
		more := len(ms) > limit
		if more {
			ms = ms[:limit]
		}
		ms, cut, err := fit(ms, budget)
		return ms, more || cut, err
	}
	exists := func(where string, id int64) (bool, error) {
		var found bool
		err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_messages WHERE channel_id = ? AND `+where+`)`, cid, id).Scan(&found)
		return found, err
	}
	var h denproto.History
	switch {
	case q.Around > 0:
		var before, after []denproto.Message
		if before, h.HasOlder, err = older(q.Around, n/2, historyBudget/2); err == nil {
			after, h.HasNewer, err = newer(q.Around, n-n/2, historyBudget/2)
		}
		h.Messages = append(before, after...)
	case q.Before > 0:
		if h.Messages, h.HasOlder, err = older(q.Before, n, historyBudget); err == nil {
			h.HasNewer, err = exists("id >= ?", q.Before)
		}
	case q.After > 0:
		if h.Messages, h.HasNewer, err = newer(q.After+1, n, historyBudget); err == nil {
			h.HasOlder, err = exists("id <= ?", q.After)
		}
	default:
		h.Messages, h.HasOlder, err = older(1<<62, n, historyBudget)
	}
	if h.Messages == nil {
		h.Messages = []denproto.Message{}
	}
	return h, err
}

// Send posts a message. A retry with the same nonce within ten minutes
// returns the original instead of posting again.
func (d *Den) Send(ctx context.Context, s *Session, channelID string, req denproto.SendRequest) (denproto.Message, error) {
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return denproto.Message{}, err
	}
	if !hasMessages(c) {
		return denproto.Message{}, invalid("messages go in text channels and DMs")
	}
	if c.Kind == denproto.KindDM {
		if err := d.checkDMOpen(ctx, c, s.MemberID); err != nil {
			return denproto.Message{}, err
		}
	}
	if err := denproto.Size("nonce", req.Nonce, denproto.NonceBytes); err != nil {
		return denproto.Message{}, err
	}
	dm := c.Kind == denproto.KindDM
	most := denproto.MaxAttachments
	if dm {
		most = denproto.MaxDMFiles
	}
	files, err := parseAttachments(req.Attachments, most)
	if err != nil {
		return denproto.Message{}, err
	}
	if err := checkSealedText(dm, req.Text, req.Sealed, req.KeyID); err != nil {
		return denproto.Message{}, err
	}
	if !dm {
		// Links lose their tracking before the text's limits are checked.
		req.Text = denproto.CleanLinks(req.Text)
		if err := denproto.CheckMessageText(req.Text, len(files) > 0); err != nil {
			return denproto.Message{}, invalid("text: %v", err)
		}
	}
	var replyTo *int64
	var reply *denproto.Reply
	if req.ReplyTo != "" {
		r, err := denproto.ParseID(req.ReplyTo)
		if err != nil {
			return denproto.Message{}, invalid("reply_to: no such message")
		}
		replied, err := d.scanMessages(ctx, `SELECT `+messageColumns+` FROM den_messages WHERE id = ? AND channel_id = ?`, r, cid)
		if err != nil {
			return denproto.Message{}, err
		}
		if len(replied) == 0 {
			return denproto.Message{}, invalid("reply_to: no such message in this channel")
		}
		replyTo = &r
		reply = replyOf(replied[0])
	}
	now := d.now()
	existing, err := d.queryMessages(ctx, `SELECT `+messageColumns+` FROM den_messages
		WHERE author_id = ? AND nonce = ? AND created_at > ?`, s.MemberID, []byte(req.Nonce), now.Add(-nonceWindow).UnixMilli())
	if err != nil {
		return denproto.Message{}, err
	}
	if len(existing) > 0 {
		existing[0].Nonce = req.Nonce
		return existing[0], nil
	}
	mentioned, err := d.mentionedMembers(ctx, req.Text, c, s.MemberID)
	if err != nil {
		return denproto.Message{}, err
	}
	editors, err := d.checkEditors(ctx, req.Editors, c, s.MemberID)
	if err != nil {
		return denproto.Message{}, err
	}
	var id int64
	var reopened []int64
	var keyID *int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		text := []byte(req.Sealed)
		if dm {
			// Checked here, so a start-over that retires the key can't slip
			// in between.
			k, err := sendKey(ctx, tx, cid, req.KeyID, s.MemberID)
			if err != nil {
				return err
			}
			keyID = &k
		} else {
			text = []byte{}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_messages (channel_id, author_id, created_at, text, reply_to, nonce, key_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, cid, s.MemberID, now.UnixMilli(), text, replyTo, []byte(req.Nonce), keyID)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if !dm {
			// The sealed text names its row, so it can't be moved to another.
			sealed, err := d.v.Seal([]byte(req.Text), textAD(id))
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE den_messages SET text = ? WHERE id = ?`, sealed, id); err != nil {
				return err
			}
		}
		if err := insertMentions(ctx, tx, id, cid, mentioned); err != nil {
			return err
		}
		if err := setEditors(ctx, tx, id, editors); err != nil {
			return err
		}
		if err := attach(ctx, tx, id, s.MemberID, files, dm, now); err != nil {
			return err
		}
		// A member has read what they wrote.
		if _, err = tx.ExecContext(ctx, `INSERT INTO den_read_states (member_id, channel_id, message_id) VALUES (?, ?, ?)
			ON CONFLICT (member_id, channel_id) DO UPDATE SET message_id = max(message_id, excluded.message_id)`, s.MemberID, cid, id); err != nil {
			return err
		}
		if c.Kind == denproto.KindDM {
			// A new message brings a closed DM back for both members.
			reopened, err = reopen(ctx, tx, cid, 0)
		}
		return err
	})
	if err != nil {
		return denproto.Message{}, err
	}
	m := denproto.Message{
		ID: denproto.FormatID(id), ChannelID: c.ID, AuthorID: denproto.FormatID(s.MemberID),
		CreatedAt: now.UnixMilli(), Revision: 1, Text: req.Text, ReplyTo: req.ReplyTo, Reply: reply, Nonce: req.Nonce,
		Editors: formatIDs(editors),
	}
	if dm {
		m.Sealed, m.KeyID = req.Sealed, denproto.FormatID(*keyID)
	} else {
		attached, err := d.attachments(ctx, d.db, []int64{id})
		if err != nil {
			return denproto.Message{}, err
		}
		m.Attachments = attached[id]
	}
	if err := d.Hub.Publish(denproto.EventMessageCreated, m, audienceOf(c)); err != nil {
		return m, err
	}
	for _, member := range reopened {
		if member != s.MemberID {
			if err := d.publishReadState(ctx, member, cid); err != nil {
				return m, err
			}
		}
	}
	return m, d.publishReadState(ctx, s.MemberID, cid)
}

// checkSealedText checks that a DM message's text comes sealed, with the
// key it's sealed with, and that nothing else does.
func checkSealedText(dm bool, text string, sealed denproto.Bytes, keyID string) error {
	if !dm {
		if sealed != nil || keyID != "" {
			return invalid("sealed: only DM messages come sealed")
		}
		return nil
	}
	if text != "" || len(sealed) == 0 || len(sealed) > denproto.MaxSealed {
		return invalid("sealed: a DM message's text comes sealed, 1 to %d bytes, and text stays empty", denproto.MaxSealed)
	}
	return nil
}

// mentionedMembers resolves a text's mentions to members in the den who can
// see the channel, leaving out the author. In a DM, every message counts
// for the other member, so their unread count is the messages waiting.
func (d *Den) mentionedMembers(ctx context.Context, text string, c denproto.Channel, author int64) ([]int64, error) {
	if c.Kind == denproto.KindDM {
		if other, ok := dmPartner(c, author); ok {
			return []int64{other}, nil
		}
		return nil, nil
	}
	names := denproto.Mentions(text)
	if len(names) == 0 {
		return nil, nil
	}
	args := make([]any, len(names))
	for i, n := range names {
		args[i] = n
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id, role FROM den_members WHERE left_at IS NULL AND username IN (?`+
		strings.Repeat(", ?", len(names)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, err
		}
		if id != author && (!c.StaffOnly || IsStaff(role)) {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func insertMentions(ctx context.Context, tx *sql.Tx, message, channel int64, members []int64) error {
	for _, m := range members {
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_mentions (member_id, channel_id, message_id) VALUES (?, ?, ?)`, m, channel, message); err != nil {
			return err
		}
	}
	return nil
}

func (d *Den) message(ctx context.Context, id string) (denproto.Message, int64, error) {
	mid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Message{}, 0, errMessageNotFound
	}
	ms, err := d.queryMessages(ctx, `SELECT `+messageColumns+` FROM den_messages WHERE id = ?`, mid)
	if err != nil {
		return denproto.Message{}, 0, err
	}
	if len(ms) == 0 {
		return denproto.Message{}, 0, errMessageNotFound
	}
	return ms[0], mid, nil
}

// Edit replaces a message's text, if nobody changed it since revision. Its
// author and the members they named may edit it; only the author changes
// who those are.
func (d *Den) Edit(ctx context.Context, s *Session, id string, req denproto.EditRequest) (denproto.Message, error) {
	d.changes.Lock()
	defer d.changes.Unlock()
	m, mid, err := d.message(ctx, id)
	if err != nil {
		return m, err
	}
	c, cid, err := d.visibleChannel(ctx, s, m.ChannelID)
	if err != nil {
		return m, errMessageNotFound
	}
	if !mayEdit(m, s.MemberID) {
		return m, forbidden("only the author, and the members they named, edit a message")
	}
	author, _ := denproto.ParseID(m.AuthorID)
	if req.Editors != nil && author != s.MemberID {
		return m, forbidden("only the author changes who may edit a message")
	}
	dm := c.Kind == denproto.KindDM
	if err := checkSealedText(dm, req.Text, req.Sealed, req.KeyID); err != nil {
		return m, err
	}
	var keyID *int64
	if dm {
		k, err := sendKey(ctx, d.db, cid, req.KeyID, s.MemberID)
		if err != nil {
			return m, err
		}
		keyID = &k
	} else if req.Unedited {
		return m, invalid("unedited: only DM messages' edits say so; the den compares other texts itself")
	} else {
		req.Text = denproto.CleanLinks(req.Text)
		if err := denproto.CheckMessageText(req.Text, len(m.Attachments) > 0); err != nil {
			return m, invalid("text: %v", err)
		}
	}
	if req.Revision != m.Revision {
		return m, &conflict{m}
	}
	var editors []int64
	if req.Editors != nil {
		if editors, err = d.checkEditors(ctx, *req.Editors, c, author); err != nil {
			return m, err
		}
	}
	// In a DM the message counts for the member it was sent to, as when it
	// was sent, and nobody is mentioned by their own edit.
	from := s.MemberID
	if c.Kind == denproto.KindDM {
		from = author
	}
	mentioned, err := d.mentionedMembers(ctx, req.Text, c, from)
	if err != nil {
		return m, err
	}
	mentioned = slices.DeleteFunc(mentioned, func(id int64) bool { return id == s.MemberID })
	sealed := []byte(req.Sealed)
	if !dm {
		if sealed, err = d.v.Seal([]byte(req.Text), textAD(mid)); err != nil {
			return m, err
		}
	}
	now := d.now().UnixMilli()
	// Changing only who may edit leaves the message unmarked. The den can't
	// compare a DM's texts, so the edit says.
	edited := req.Text != m.Text
	if dm {
		edited = !req.Unedited
	}
	err = d.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE den_messages SET text = ?, key_id = ?, revision = revision + 1,
			edited_at = iif(?, ?, edited_at), edited_by = iif(?, ?, edited_by) WHERE id = ? AND revision = ?`,
			sealed, keyID, edited, now, edited, s.MemberID, mid, req.Revision)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errRaced
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_mentions WHERE message_id = ?`, mid); err != nil {
			return err
		}
		if err := insertMentions(ctx, tx, mid, cid, mentioned); err != nil {
			return err
		}
		if req.Editors != nil {
			return setEditors(ctx, tx, mid, editors)
		}
		return nil
	})
	if errors.Is(err, errRaced) {
		current, _, lerr := d.message(ctx, id)
		if lerr != nil {
			return m, lerr
		}
		return current, &conflict{current}
	}
	if err != nil {
		return m, err
	}
	m.Text, m.Revision = req.Text, m.Revision+1
	if dm {
		m.Sealed, m.KeyID = req.Sealed, denproto.FormatID(*keyID)
	}
	if edited {
		m.EditedAt, m.EditedBy = now, denproto.FormatID(s.MemberID)
	}
	if req.Editors != nil {
		m.Editors = formatIDs(editors)
	}
	return m, d.Hub.Publish(denproto.EventMessageUpdated, m, audienceOf(c))
}

// SetTask checks or unchecks task n of a message, for its author or one of
// its editors. A tick needs no revision, so ticks on different tasks never
// conflict, but it names the task's text: if that no longer matches, as
// after an edit moved the lines, the tick is refused as a conflict rather
// than landing on another task. Ticks don't mark a message as edited.
func (d *Den) SetTask(ctx context.Context, s *Session, id string, n int, req denproto.TaskRequest) (denproto.Message, error) {
	d.changes.Lock()
	defer d.changes.Unlock()
	m, mid, err := d.message(ctx, id)
	if err != nil {
		return m, err
	}
	c, _, err := d.visibleChannel(ctx, s, m.ChannelID)
	if err != nil {
		return m, errMessageNotFound
	}
	if !mayEdit(m, s.MemberID) {
		return m, forbidden("only the author, and the members they named, tick a message's tasks")
	}
	if c.Kind == denproto.KindDM {
		return m, invalid("a DM message's tasks tick with an edit: the den can't read its text")
	}
	tasks := denproto.Tasks(m.Text)
	if n < 0 || n >= len(tasks) || tasks[n].Text != req.Text {
		return m, &conflict{m}
	}
	if tasks[n].Checked == req.Checked {
		return m, nil
	}
	text, _ := denproto.SetTask(m.Text, n, req.Checked)
	sealed, err := d.v.Seal([]byte(text), textAD(mid))
	if err != nil {
		return m, err
	}
	res, err := d.db.ExecContext(ctx, `UPDATE den_messages SET text = ?, revision = revision + 1 WHERE id = ? AND revision = ?`,
		sealed, mid, m.Revision)
	if err != nil {
		return m, err
	}
	// Edits and ticks wait their turn, so only a delete gets here first.
	if changed, _ := res.RowsAffected(); changed == 0 {
		return m, errMessageNotFound
	}
	m.Text, m.Revision = text, m.Revision+1
	return m, d.Hub.Publish(denproto.EventMessageUpdated, m, audienceOf(c))
}

var errRaced = errors.New("the message changed during the edit")

// conflict is an edit_conflict carrying the message as it is now.
type conflict struct{ current denproto.Message }

func (c *conflict) Error() string { return "edit_conflict" }

// Current returns the message an edit conflicted with.
func Conflict(err error) (denproto.Message, bool) {
	var c *conflict
	if errors.As(err, &c) {
		return c.current, true
	}
	return denproto.Message{}, false
}

// Delete removes a message for good. Its author may, and so may staff of a
// higher rank than the author, as with removing members: moderators delete
// members' messages, and the owner anyone's.
func (d *Den) Delete(ctx context.Context, s *Session, id string) error {
	m, mid, err := d.message(ctx, id)
	if err != nil {
		return err
	}
	c, _, err := d.visibleChannel(ctx, s, m.ChannelID)
	if err != nil {
		return errMessageNotFound
	}
	if m.AuthorID != denproto.FormatID(s.MemberID) {
		var role string
		if err := d.db.QueryRowContext(ctx, `SELECT role FROM den_members WHERE id = ?`, m.AuthorID).Scan(&role); err != nil {
			return err
		}
		if !IsStaff(s.Role) || denproto.Rank(role) >= denproto.Rank(s.Role) {
			return forbidden("only the author, or staff above them, delete a message")
		}
	}
	files, blobs, err := filesOf(ctx, d.db, `m.id = ?`, mid)
	if err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx, `DELETE FROM den_messages WHERE id = ?`, mid); err != nil {
		return err
	}
	d.files.remove(blobs)
	return d.Hub.Publish(denproto.EventMessageDeleted, denproto.MessageDeleted{ID: m.ID, ChannelID: m.ChannelID, Files: files[mid]}, audienceOf(c))
}

// MarkRead moves a member's read position in a channel forward.
func (d *Den) MarkRead(ctx context.Context, s *Session, channelID string, req denproto.ReadRequest) error {
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return err
	}
	if !hasMessages(c) {
		return invalid("only text channels and DMs have a read position")
	}
	mid, err := denproto.ParseID(req.MessageID)
	if err != nil {
		return invalid("message_id: invalid ID")
	}
	res, err := d.db.ExecContext(ctx, `INSERT INTO den_read_states (member_id, channel_id, message_id) VALUES (?, ?, ?)
		ON CONFLICT (member_id, channel_id) DO UPDATE SET message_id = excluded.message_id
		WHERE excluded.message_id > den_read_states.message_id`, s.MemberID, cid, mid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	return d.publishReadState(ctx, s.MemberID, cid)
}

// publishReadState tells a member's sessions where they are in a channel.
func (d *Den) publishReadState(ctx context.Context, member, channel int64) error {
	st := denproto.ReadState{ChannelID: denproto.FormatID(channel)}
	var pos sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT message_id, closed FROM den_read_states WHERE member_id = ? AND channel_id = ?`, member, channel).
		Scan(&pos, &st.Closed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if pos.Int64 > 0 {
		st.ReadPosition = denproto.FormatID(pos.Int64)
	}
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM den_mentions WHERE member_id = ? AND channel_id = ? AND message_id > ?`,
		member, channel, pos.Int64).Scan(&st.MentionCount); err != nil {
		return err
	}
	return d.Hub.Publish(denproto.EventReadStateUpdated, st, OnlyMember(member))
}
