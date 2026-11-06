package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Group DMs (M16.3). A group DM is a channel of kind group_dm, its members
// kept in the order they joined, each with the change that took them in,
// from which they get the group's changes, and the group's last message as
// they joined, after which they read. Every change is a member's
// statement, signed, naming the group's members after it and the change
// before it, which the den checks as each member's Dens does and keeps in
// order: it can't add anyone, since no change it made up would check, and
// its own changes are removals of members who left the den.
//
// A group has a key for each membership. The change that makes one, the
// group made or a member added or removed, carries its key; after a member
// leaves, or the den takes one out, the live key retires and the first
// remaining member's Dens to send makes the next, as it does a DM's. The
// group's name is sealed under one of its keys, by that key's ID: a naming
// seals it under the live key, and each new key carries it again.
//
// The den sends nothing of groups to a Dens speaking version 1 of the
// protocol: their channels, messages and keys would close its connection
// as the den breaking the protocol.

var (
	errGroupChanged = denproto.Errorf(http.StatusConflict, denproto.CodeEditConflict, "the group changed since that change was made")
	errNoGroups     = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "group DMs need version 2 of the protocol")
)

// reservedFor is how long a group reserved for its maker waits for its
// first change before it goes.
const reservedFor = time.Hour

// isPrivate reports whether a channel's messages are sealed by its
// members' Dens: a DM's, a group's, or a member's notes (M17.3).
func isPrivate(c denproto.Channel) bool {
	return c.Kind == denproto.KindDM || c.Kind == denproto.KindGroupDM || c.Kind == denproto.KindNotes
}

// group is a group DM as the den keeps it: who reserved it, its members in
// the order they joined, with the change that took each in and their
// group's last message as they joined, its latest change's number and hash,
// and its name.
type group struct {
	id      int64
	madeBy  int64
	members []int64
	joined  map[int64]int
	since   map[int64]int64
	state   denproto.GroupState
	name    *denproto.SealedName
}

// loadGroup reads a group DM. One reserved for its maker has no change yet.
func loadGroup(ctx context.Context, q querier, denID []byte, id int64) (group, error) {
	g := group{id: id, joined: map[int64]int{}, since: map[int64]int64{}}
	var name, kid []byte
	err := q.QueryRowContext(ctx, `SELECT made_by, name, name_kid FROM den_group_dms WHERE channel_id = ?`, id).Scan(&g.madeBy, &name, &kid)
	if errors.Is(err, sql.ErrNoRows) {
		return g, errChannelNotFound
	}
	if err != nil {
		return g, err
	}
	if name != nil {
		g.name = &denproto.SealedName{KID: kid, Sealed: name}
	}
	rows, err := q.QueryContext(ctx, `SELECT member_id, joined, since FROM den_group_members WHERE channel_id = ? ORDER BY position`, id)
	if err != nil {
		return g, err
	}
	for rows.Next() {
		var m, since int64
		var joined int
		if err := rows.Scan(&m, &joined, &since); err != nil {
			rows.Close()
			return g, err
		}
		g.members = append(g.members, m)
		g.joined[m], g.since[m] = joined, since
		g.state.Members = append(g.state.Members, denproto.FormatID(m))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, err
	}
	var statement []byte
	err = q.QueryRowContext(ctx, `SELECT statement FROM den_group_changes WHERE channel_id = ? ORDER BY n DESC LIMIT 1`, id).Scan(&statement)
	if errors.Is(err, sql.ErrNoRows) {
		return g, nil
	}
	if err != nil {
		return g, err
	}
	var last denproto.GroupChange
	if err := json.Unmarshal(statement, &last); err != nil {
		return g, err
	}
	g.state.N = last.N
	g.state.Hash, err = last.Hash(denID, id)
	return g, err
}

// fillGroup sets a group DM's channel's members, in the order they joined,
// the number of its latest change, and its name, as the den holds them.
func fillGroup(ctx context.Context, q querier, c *denproto.Channel) error {
	id, err := denproto.ParseID(c.ID)
	if err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `SELECT member_id FROM den_group_members WHERE channel_id = ? ORDER BY position`, id)
	if err != nil {
		return err
	}
	c.Members = nil
	for rows.Next() {
		var m int64
		if err := rows.Scan(&m); err != nil {
			rows.Close()
			return err
		}
		c.Members = append(c.Members, denproto.FormatID(m))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var name, kid []byte
	if err := q.QueryRowContext(ctx, `SELECT coalesce((SELECT max(n) FROM den_group_changes WHERE channel_id = ?1), 0), g.name, g.name_kid
		FROM den_group_dms g WHERE g.channel_id = ?1`, id).Scan(&c.Changes, &name, &kid); err != nil {
		return err
	}
	c.SealedName = nil
	if name != nil {
		c.SealedName = &denproto.SealedName{KID: kid, Sealed: name}
	}
	return nil
}

// groupDMs lists the group DMs a member is in.
func groupDMs(ctx context.Context, q querier, member int64) ([]denproto.Channel, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+channelColumns+` FROM den_channels
		WHERE kind = 'group_dm' AND id IN (SELECT channel_id FROM den_group_members WHERE member_id = ?) ORDER BY id`, member)
	if err != nil {
		return nil, err
	}
	var out []denproto.Channel
	for rows.Next() {
		c, err := scanChannel(rows.Scan)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := fillGroup(ctx, q, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// audienceOfGroup is a group DM's members, as an audience.
func audienceOfGroup(c denproto.Channel) Audience {
	set := map[int64]bool{}
	for _, m := range c.Members {
		if id, err := denproto.ParseID(m); err == nil {
			set[id] = true
		}
	}
	return Members(set)
}

// groupSince is where a member's view of a group's messages starts: after
// the group's last message as they joined, and for any other channel,
// from its first.
func groupSince(ctx context.Context, q querier, c denproto.Channel, cid, member int64) (int64, error) {
	if c.Kind != denproto.KindGroupDM {
		return 0, nil
	}
	var since int64
	err := q.QueryRowContext(ctx, `SELECT since FROM den_group_members WHERE channel_id = ? AND member_id = ?`, cid, member).Scan(&since)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errChannelNotFound
	}
	return since, err
}

// checkGroupCount refuses a member in as many group DMs as a member may be.
func checkGroupCount(ctx context.Context, q querier, member int64) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM den_group_members WHERE member_id = ?`, member).Scan(&n); err != nil {
		return err
	}
	if n >= denproto.MaxGroupDMs {
		return invalid("members: member %d is in %d group DMs, as many as a member may be", member, denproto.MaxGroupDMs)
	}
	return nil
}

// ReserveGroup reserves a group DM for the session's member, who makes it
// with its first change: a change, its key and the group's name are all
// bound to the group's ID, which the den gives. Nobody sees a group until
// it's made, and one reserved an hour ago that wasn't goes. A member who
// reserved one and didn't make it gets it again.
func (d *Den) ReserveGroup(ctx context.Context, s *Session) (denproto.GroupCreated, error) {
	if !s.groups() {
		return denproto.GroupCreated{}, errNoGroups
	}
	var id int64
	err := d.tx(ctx, func(tx *sql.Tx) error {
		if err := dropReserved(ctx, tx, d.now()); err != nil {
			return err
		}
		if err := checkGroupCount(ctx, tx, s.MemberID); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `SELECT g.channel_id FROM den_group_dms g WHERE g.made_by = ?
			AND NOT EXISTS (SELECT 1 FROM den_group_changes c WHERE c.channel_id = g.channel_id)`, s.MemberID).Scan(&id)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := d.now().UnixMilli()
		res, err := tx.ExecContext(ctx, `INSERT INTO den_channels (group_id, name, kind, position, created_at) VALUES (NULL, '', 'group_dm', 0, ?)`, now)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO den_group_dms (channel_id, made_by, made_at) VALUES (?, ?, ?)`, id, s.MemberID, now)
		return err
	})
	return denproto.GroupCreated{ID: denproto.FormatID(id)}, err
}

// dropReserved removes the groups reserved more than an hour ago that
// nobody made.
func dropReserved(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM den_channels WHERE kind = 'group_dm' AND id IN (SELECT g.channel_id FROM den_group_dms g
		WHERE g.made_at < ? AND NOT EXISTS (SELECT 1 FROM den_group_changes c WHERE c.channel_id = g.channel_id))`, now.Add(-reservedFor).UnixMilli())
	return err
}

// GroupChanges returns a page of a group DM's changes after a number, as
// the session's member may see them: from the change that took them in.
func (d *Den) GroupChanges(ctx context.Context, s *Session, channelID string, after int) (denproto.GroupChanges, error) {
	out := denproto.GroupChanges{Changes: []denproto.GroupChange{}}
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return out, err
	}
	if c.Kind != denproto.KindGroupDM {
		return out, errChannelNotFound
	}
	if after < 0 {
		return out, invalid("after: 0 or more")
	}
	var joined int
	if err := d.db.QueryRowContext(ctx, `SELECT joined FROM den_group_members WHERE channel_id = ? AND member_id = ?`, cid, s.MemberID).Scan(&joined); err != nil {
		return out, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id, made_at, statement FROM den_group_changes WHERE channel_id = ? AND n >= ? ORDER BY n LIMIT ?`,
		cid, max(after+1, joined), denproto.GroupChangesPage+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, at int64
		var statement []byte
		if err := rows.Scan(&id, &at, &statement); err != nil {
			return out, err
		}
		var ch denproto.GroupChange
		if err := json.Unmarshal(statement, &ch); err != nil {
			return out, err
		}
		ch.ID, ch.MadeAt = denproto.FormatID(id), at
		out.Changes = append(out.Changes, ch)
	}
	if len(out.Changes) > denproto.GroupChangesPage {
		out.Changes, out.More = out.Changes[:denproto.GroupChangesPage], true
	}
	return out, rows.Err()
}

// groupDone is a change made to a group DM, for its members to hear of
// once its transaction commits.
type groupDone struct {
	channel int64
	change  denproto.GroupChange
	// before are its members before the change; retired and key the keys
	// it retired and made; gone says nobody's left, so it went, with the
	// blobs of its files.
	before  []int64
	retired []int64
	key     int64
	gone    bool
	blobs   [][]byte
}

// ChangeGroup makes a change to a group DM: its first, which makes it, by
// the member who reserved it; a member added by any member, or removed by
// the group's first; a member leaving; or a new name. The change must
// follow the group's latest, by its rules, and be signed by the session's
// member's identity as they publish it now; one made against another of
// its changes gets edit_conflict, and the client makes it again. A change
// that makes a membership carries its key, for the members after it, which
// the den checks as it does a DM's; a member leaving retires the live key,
// and a naming is sealed under it. A group nobody's left in goes.
func (d *Den) ChangeGroup(ctx context.Context, s *Session, channelID string, req denproto.GroupChangeRequest) (denproto.GroupChange, error) {
	if !s.groups() {
		return denproto.GroupChange{}, errNoGroups
	}
	cid, err := denproto.ParseID(channelID)
	if err != nil {
		return denproto.GroupChange{}, errChannelNotFound
	}
	ch := req.Change
	ch.ID, ch.MadeAt = "", 0
	if ch.By != denproto.FormatID(s.MemberID) {
		return denproto.GroupChange{}, invalid("by: a change is made by the member who sends it")
	}
	if ch.Kind == denproto.ChangeDeparted {
		return denproto.GroupChange{}, invalid("kind: only the den takes out members who left it")
	}
	info, _ := d.Info()
	d.keys.Lock()
	defer d.keys.Unlock()
	now := d.now()
	var done groupDone
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var kind string
		err := tx.QueryRowContext(ctx, `SELECT kind FROM den_channels WHERE id = ?`, cid).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			return errChannelNotFound
		}
		if err != nil {
			return err
		}
		if kind != denproto.KindGroupDM {
			return errChannelNotFound
		}
		g, err := loadGroup(ctx, tx, info.ID, cid)
		if err != nil {
			return err
		}
		var prev *denproto.GroupState
		switch {
		case g.state.N == 0:
			// Reserved: its maker alone makes it, and nobody else knows it.
			if g.madeBy != s.MemberID {
				return errChannelNotFound
			}
			if ch.Kind != denproto.ChangeMade {
				return invalid("kind: a group is made by its first change")
			}
		case !slices.Contains(g.members, s.MemberID):
			return errChannelNotFound
		case ch.N != g.state.N+1 || !denproto.Equal(ch.Prev, g.state.Hash):
			return errGroupChanged
		case g.state.N >= denproto.MaxGroupChanges:
			return invalid("a group changes at most %d times; start another", denproto.MaxGroupChanges)
		default:
			prev = &g.state
		}
		if _, err := denproto.CheckChange(info.ID, cid, prev, ch); err != nil {
			return invalid("change: %v", err)
		}
		p, ok, err := publishedIdentity(ctx, tx, s.MemberID)
		if err != nil {
			return err
		}
		if !ok || !denproto.VerifyChange(info.ID, cid, ch, p.hash) {
			return invalid("signature: it doesn't check against your identity")
		}
		after := make([]int64, len(ch.After))
		for i, m := range ch.After {
			after[i], _ = denproto.ParseID(m)
		}
		newcomers := map[int64]bool{}
		for _, m := range after {
			if !slices.Contains(g.members, m) {
				newcomers[m] = true
			}
		}
		for m := range newcomers {
			var left sql.NullInt64
			err := tx.QueryRowContext(ctx, `SELECT left_at FROM den_members WHERE id = ?`, m).Scan(&left)
			if errors.Is(err, sql.ErrNoRows) || left.Valid {
				return invalid("members: member %d isn't in the den", m)
			}
			if err != nil {
				return err
			}
			if err := checkGroupCount(ctx, tx, m); err != nil {
				return err
			}
		}
		makes := ch.Kind == denproto.ChangeMade || ch.Kind == denproto.ChangeAdded || ch.Kind == denproto.ChangeRemoved
		if makes != (req.Key != nil) {
			return invalid("key: a change that makes a membership carries its key, and others none")
		}
		done = groupDone{channel: cid, before: g.members}
		ms := now.UnixMilli()
		switch {
		case req.Key != nil:
			if done.key, done.retired, err = putKey(ctx, tx, info.ID, cid, s.MemberID, after, *req.Key, ms, true, true); err != nil {
				return err
			}
		case ch.Kind == denproto.ChangeLeft:
			if done.retired, err = retireKeys(ctx, tx, s.MemberID, ms, denproto.RetiredReplaced, `k.channel_id = ?`, cid); err != nil {
				return err
			}
		case ch.Kind == denproto.ChangeNamed:
			var statement []byte
			err := tx.QueryRowContext(ctx, `SELECT statement FROM den_dm_keys WHERE channel_id = ? AND retired IS NULL`, cid).Scan(&statement)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			var live keptStatement
			if statement == nil || json.Unmarshal(statement, &live) != nil || !denproto.Equal(live.KID, ch.Name.KID) {
				return invalid("name: a group's name is sealed under its live key")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE den_group_dms SET name = ?, name_kid = ? WHERE channel_id = ?`,
				[]byte(ch.Name.Sealed), []byte(ch.Name.KID), cid); err != nil {
				return err
			}
		}
		if ch.Kind == denproto.ChangeMade && ch.Name != nil {
			if !denproto.Equal(ch.Name.KID, req.Key.KID) {
				return invalid("name: a group's name is sealed under its key")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE den_group_dms SET name = ?, name_kid = ? WHERE channel_id = ?`,
				[]byte(ch.Name.Sealed), []byte(ch.Name.KID), cid); err != nil {
				return err
			}
		}
		if done.change, err = insertChange(ctx, tx, cid, s.MemberID, ch, now); err != nil {
			return err
		}
		if err := setMembers(ctx, tx, g, ch, after); err != nil {
			return err
		}
		if len(after) == 0 {
			done.gone = true
			if _, done.blobs, err = filesOf(ctx, tx, `m.channel_id = ?`, cid); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `DELETE FROM den_channels WHERE id = ?`, cid)
			return err
		}
		return nil
	})
	if err != nil {
		return denproto.GroupChange{}, err
	}
	d.log.Infof("Member %d made change %d (%s) to group DM %d", s.MemberID, done.change.N, done.change.Kind, cid)
	return done.change, d.publishGroup(ctx, done)
}

// insertChange keeps a group's change, by a member or by the den (by 0),
// and returns it with its ID and time.
func insertChange(ctx context.Context, tx *sql.Tx, cid, by int64, ch denproto.GroupChange, now time.Time) (denproto.GroupChange, error) {
	ch.ID, ch.MadeAt = "", 0
	statement, err := json.Marshal(ch)
	if err != nil {
		return ch, err
	}
	var madeBy *int64
	if by != 0 {
		madeBy = &by
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO den_group_changes (channel_id, n, made_by, made_at, statement) VALUES (?, ?, ?, ?, ?)`,
		cid, ch.N, madeBy, now.UnixMilli(), statement)
	if err != nil {
		return ch, err
	}
	id, err := res.LastInsertId()
	ch.ID, ch.MadeAt = denproto.FormatID(id), now.UnixMilli()
	return ch, err
}

// setMembers makes a group's members those after a change: newcomers
// joining at its end, from the group's last message, and those gone
// leaving with their read position and mentions there.
func setMembers(ctx context.Context, tx *sql.Tx, g group, ch denproto.GroupChange, after []int64) error {
	for _, m := range g.members {
		if !slices.Contains(after, m) {
			if err := leaveGroup(ctx, tx, g.id, m); err != nil {
				return err
			}
		}
	}
	var since int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(id), 0) FROM den_messages WHERE channel_id = ?`, g.id).Scan(&since); err != nil {
		return err
	}
	var position int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(position), -1) FROM den_group_members WHERE channel_id = ?`, g.id).Scan(&position); err != nil {
		return err
	}
	for _, m := range after {
		if slices.Contains(g.members, m) {
			continue
		}
		position++
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_group_members (channel_id, member_id, position, joined, since) VALUES (?, ?, ?, ?, ?)`,
			g.id, m, position, ch.N, since); err != nil {
			return err
		}
	}
	return nil
}

// leaveGroup takes a member out of a group, with their read position and
// mentions there. Their messages stay, as a DM's do once its member left.
func leaveGroup(ctx context.Context, tx *sql.Tx, cid, member int64) error {
	for _, stmt := range []string{
		`DELETE FROM den_group_members WHERE channel_id = ? AND member_id = ?`,
		`DELETE FROM den_read_states WHERE channel_id = ? AND member_id = ?`,
		`DELETE FROM den_mentions WHERE channel_id = ? AND member_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, cid, member); err != nil {
			return err
		}
	}
	return nil
}

// departGroups takes a member who leaves the den out of the groups they're
// in, each by a change of the den's own, which takes nobody in, retiring
// each group's live key, which the first remaining member's Dens to send
// replaces. A group nobody's left in goes, with its messages.
func departGroups(ctx context.Context, tx *sql.Tx, denID []byte, member int64, now time.Time) ([]groupDone, error) {
	// One they reserved and didn't make goes.
	if _, err := tx.ExecContext(ctx, `DELETE FROM den_channels WHERE kind = 'group_dm' AND id IN (SELECT g.channel_id FROM den_group_dms g
		WHERE g.made_by = ? AND NOT EXISTS (SELECT 1 FROM den_group_changes c WHERE c.channel_id = g.channel_id))`, member); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT channel_id FROM den_group_members WHERE member_id = ? ORDER BY channel_id`, member)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []groupDone
	for _, cid := range ids {
		g, err := loadGroup(ctx, tx, denID, cid)
		if err != nil {
			return nil, err
		}
		after := slices.DeleteFunc(slices.Clone(g.members), func(m int64) bool { return m == member })
		ch := denproto.GroupChange{N: g.state.N + 1, Prev: g.state.Hash, Kind: denproto.ChangeDeparted,
			Subjects: []string{denproto.FormatID(member)}, After: formatIDs(after)}
		if ch.After == nil {
			ch.After = []string{}
		}
		if _, err := denproto.CheckChange(denID, cid, &g.state, ch); err != nil {
			return nil, err
		}
		done := groupDone{channel: cid, before: g.members}
		if done.change, err = insertChange(ctx, tx, cid, 0, ch, now); err != nil {
			return nil, err
		}
		if done.retired, err = retireKeys(ctx, tx, member, now.UnixMilli(), denproto.RetiredReplaced, `k.channel_id = ?`, cid); err != nil {
			return nil, err
		}
		if err := setMembers(ctx, tx, g, ch, after); err != nil {
			return nil, err
		}
		if len(after) == 0 {
			done.gone = true
			if _, done.blobs, err = filesOf(ctx, tx, `m.channel_id = ?`, cid); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM den_channels WHERE id = ?`, cid); err != nil {
				return nil, err
			}
		}
		out = append(out, done)
	}
	return out, nil
}

// publishGroup tells a group's members of a change once it committed: the
// change itself to those after it, the group to those who joined as
// created and to the rest as updated, its going to those who left, and its
// keys as they now stand. A group that went takes its files with it.
func (d *Den) publishGroup(ctx context.Context, done groupDone) error {
	gone := func(members []int64) error {
		for _, m := range members {
			if err := d.Hub.Publish(denproto.EventChannelDeleted, denproto.ChannelDeleted{ID: denproto.FormatID(done.channel)},
				OnlyMember(m).aboutGroups()); err != nil {
				return err
			}
		}
		return nil
	}
	if done.gone {
		d.files.remove(done.blobs)
		return gone(done.before)
	}
	c, err := d.channel(ctx, d.db, done.channel)
	if err != nil {
		return err
	}
	now := audienceOfGroup(c)
	var left, joined []int64
	for _, m := range done.before {
		if !now.set[m] {
			left = append(left, m)
		}
	}
	for m := range now.set {
		if !slices.Contains(done.before, m) {
			joined = append(joined, m)
		}
	}
	slices.Sort(joined)
	errs := []error{gone(left)}
	for _, m := range joined {
		errs = append(errs, d.Hub.Publish(denproto.EventChannelCreated, c, OnlyMember(m).aboutGroups()))
	}
	errs = append(errs, d.Hub.Publish(denproto.EventGroupChange, denproto.GroupChanged{ChannelID: c.ID, Change: done.change}, now))
	stayed := map[int64]bool{}
	for m := range now.set {
		if !slices.Contains(joined, m) {
			stayed[m] = true
		}
	}
	if len(stayed) > 0 {
		errs = append(errs, d.Hub.Publish(denproto.EventChannelUpdated, c, Members(stayed)))
	}
	keys := done.retired
	if done.key != 0 {
		keys = append(keys, done.key)
	}
	errs = append(errs, d.publishKeys(ctx, keys...))
	return errors.Join(errs...)
}
