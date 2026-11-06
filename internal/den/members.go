package den

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

var errMemberNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such member")

func forbidden(format string, args ...any) error {
	return denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, format, args...)
}

func bioAD(id int64) []byte { return []byte("den_members.bio:" + strconv.FormatInt(id, 10)) }

// memberColumns selects a member from den_members, with the size of the
// pictures on their profile, and whether each is animated (M15.1), and
// their identity's hash (M16.1).
const memberColumns = `id, username, display_name, role, joined_at, left_at,
	avatar_id, (SELECT width FROM den_files WHERE den_files.id = den_members.avatar_id),
	(SELECT height FROM den_files WHERE den_files.id = den_members.avatar_id),
	(SELECT animated FROM den_files WHERE den_files.id = den_members.avatar_id),
	banner_id, (SELECT width FROM den_files WHERE den_files.id = den_members.banner_id),
	(SELECT height FROM den_files WHERE den_files.id = den_members.banner_id),
	(SELECT animated FROM den_files WHERE den_files.id = den_members.banner_id),
	identity`

func scanMember(scan func(...any) error, extra ...any) (denproto.Member, error) {
	var m denproto.Member
	var id int64
	var left, avatar, aw, ah, banner, bw, bh sql.NullInt64
	var aa, ba sql.NullBool
	var identity []byte
	err := scan(append([]any{&id, &m.Username, &m.DisplayName, &m.Role, &m.JoinedAt, &left, &avatar, &aw, &ah, &aa, &banner, &bw, &bh, &ba,
		&identity}, extra...)...)
	m.ID = denproto.FormatID(id)
	if len(identity) > 0 {
		m.Identity = denproto.Bytes(identity).String()
	}
	if left.Valid {
		m.LeftAt = left.Int64
	}
	if avatar.Valid {
		m.Avatar = denproto.Image{ID: denproto.FormatID(avatar.Int64), Width: int(aw.Int64), Height: int(ah.Int64), Animated: aa.Bool}
	}
	if banner.Valid {
		m.Banner = denproto.Image{ID: denproto.FormatID(banner.Int64), Width: int(bw.Int64), Height: int(bh.Int64), Animated: ba.Bool}
	}
	return m, err
}

// Member returns a member by ID, without their bio.
func (d *Den) Member(ctx context.Context, id int64) (denproto.Member, error) {
	m, err := scanMember(d.db.QueryRowContext(ctx, `SELECT `+memberColumns+` FROM den_members WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return m, errMemberNotFound
	}
	return m, err
}

// Profile returns a member with their bio. Any member may look at anyone
// who has been in the den, so old messages can show who wrote them.
func (d *Den) Profile(ctx context.Context, id string) (denproto.Member, error) {
	mid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Member{}, errMemberNotFound
	}
	var sealed []byte
	m, err := scanMember(d.db.QueryRowContext(ctx, `SELECT `+memberColumns+`, bio FROM den_members WHERE id = ?`, mid).Scan, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return m, errMemberNotFound
	}
	if err != nil {
		return m, err
	}
	if len(sealed) > 0 {
		bio, err := d.v.Open(sealed, bioAD(mid))
		if err != nil {
			return m, err
		}
		m.Bio = string(bio)
	}
	return m, nil
}

// UpdateProfile changes the member's own display name, bio, avatar or
// banner. A picture is an upload of theirs waiting to be used; the one it
// replaces is deleted.
func (d *Den) UpdateProfile(ctx context.Context, s *Session, req denproto.ProfileRequest) (denproto.Member, error) {
	var sets []string
	var args []any
	if req.DisplayName != nil {
		name, err := denproto.CleanName(*req.DisplayName, denproto.MaxNameRunes)
		if err != nil {
			return denproto.Member{}, invalid("display_name: %v", err)
		}
		sets, args = append(sets, "display_name = ?"), append(args, name)
	}
	if req.Bio != nil {
		bio := denproto.CleanLinks(*req.Bio)
		if err := denproto.CheckBio(bio); err != nil {
			return denproto.Member{}, invalid("bio: %v", err)
		}
		if bio == "" {
			// NULL itself: a nil slice can be stored as an empty blob.
			sets = append(sets, "bio = NULL")
		} else {
			sealed, err := d.v.Seal([]byte(bio), bioAD(s.MemberID))
			if err != nil {
				return denproto.Member{}, err
			}
			sets, args = append(sets, "bio = ?"), append(args, sealed)
		}
	}
	var replaced [][]byte
	now := d.now()
	err := d.tx(ctx, func(tx *sql.Tx) error {
		for _, pic := range []struct {
			id     *string
			column string
			banner bool
		}{{req.Avatar, "avatar_id", false}, {req.Banner, "banner_id", true}} {
			if pic.id == nil {
				continue
			}
			var next any // NULL clears it
			if *pic.id != "" {
				fid, err := checkPicture(ctx, tx, s.MemberID, *pic.id, pic.banner, now)
				if err != nil {
					return err
				}
				next = fid
			}
			var old sql.NullInt64
			if err := tx.QueryRowContext(ctx, `SELECT `+pic.column+` FROM den_members WHERE id = ?`, s.MemberID).Scan(&old); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE den_members SET `+pic.column+` = ? WHERE id = ?`, next, s.MemberID); err != nil {
				return err
			}
			if old.Valid {
				blobs, err := deleteFiles(ctx, tx, `id = ?`, old.Int64)
				if err != nil {
					return err
				}
				replaced = append(replaced, blobs...)
			}
		}
		if len(sets) > 0 {
			if _, err := tx.ExecContext(ctx, "UPDATE den_members SET "+strings.Join(sets, ", ")+" WHERE id = ?", append(args, s.MemberID)...); err != nil {
				return err
			}
		}
		if len(sets) == 0 && req.Avatar == nil && req.Banner == nil {
			return nil
		}
		return logMember(ctx, tx, s.MemberID, now)
	})
	if err != nil {
		return denproto.Member{}, err
	}
	d.files.remove(replaced)
	m, err := d.Profile(ctx, denproto.FormatID(s.MemberID))
	if err != nil || (len(sets) == 0 && req.Avatar == nil && req.Banner == nil) {
		return m, err
	}
	// Bios travel only on request; an update tells clients to fetch again.
	public := m
	public.Bio = ""
	return m, d.Hub.Publish(denproto.EventMemberUpdated, public, Everyone)
}

// SetRole makes a member a moderator or a member again. Only the owner
// may, and the owner's own role changes only when the den changes hands.
func (d *Den) SetRole(ctx context.Context, s *Session, id string, req denproto.RoleRequest) (denproto.Member, error) {
	if s.Role != denproto.RoleOwner {
		return denproto.Member{}, forbidden("only the owner changes roles")
	}
	if req.Role != denproto.RoleMember && req.Role != denproto.RoleModerator {
		return denproto.Member{}, invalid("role must be member or moderator")
	}
	mid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Member{}, errMemberNotFound
	}
	if mid == s.MemberID {
		return denproto.Member{}, invalid("the owner stays the owner until the den changes hands")
	}
	err = d.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE den_members SET role = ? WHERE id = ? AND left_at IS NULL AND role != 'owner'`, req.Role, mid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errMemberNotFound
		}
		return logMember(ctx, tx, mid, d.now())
	})
	if err != nil {
		return denproto.Member{}, err
	}
	m, err := d.Member(ctx, mid)
	if err != nil {
		return m, err
	}
	if err := d.Hub.Publish(denproto.EventMemberUpdated, m, Everyone); err != nil {
		return m, err
	}
	// Staff-only channels come or go with the role, so the member's sockets
	// start over from a snapshot of what they can see now.
	d.Hub.SetStaff(mid, IsStaff(req.Role))
	d.roleChanged(mid, IsStaff(req.Role))
	return m, nil
}

// depart takes a member out of the den within tx. Their row stays, so
// their messages keep a name and a returning member can reclaim it, but
// they lose their role, devices and sessions, recovery codes, read state,
// any invites they made, the pictures on their profile, the uploads they
// hadn't sent, and their identity and record (M16.1), which they publish
// again if they come back. The sounds and emoji they added are the den's, and
// stay (M14.3, M15.2).
// They leave their group DMs too, each by a change of the den's own
// (M16.3), their notes go, with their files (M17.3), and their events to
// come, with their banners, and their marks on the rest (M17.4); their
// live events go on to their end, banners and all.
// It returns what's to be told once tx commits: those files' blobs, to
// remove, the groups' changes, to tell their members of, and the events.
func (d *Den) depart(ctx context.Context, tx *sql.Tx, denID []byte, id, now int64) (departure, error) {
	var out departure
	for _, stmt := range []string{
		`UPDATE den_members SET left_at = ?1, role = 'member', identity = NULL WHERE id = ?2`,
		`DELETE FROM den_identities WHERE member_id = ?2`,
		`DELETE FROM den_records WHERE member_id = ?2`,
		`DELETE FROM den_devices WHERE member_id = ?2`, // and, through them, sessions
		`DELETE FROM den_recovery_codes WHERE member_id = ?2`,
		`DELETE FROM den_read_states WHERE member_id = ?2`,
		`DELETE FROM den_mentions WHERE member_id = ?2`,
		`DELETE FROM den_invites WHERE created_by = ?2 AND role = 'member'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, now, id); err != nil {
			return out, err
		}
	}
	if err := logMember(ctx, tx, id, time.UnixMilli(now)); err != nil {
		return out, err
	}
	var err error
	if out.groups, err = departGroups(ctx, tx, denID, id, time.UnixMilli(now)); err != nil {
		return out, err
	}
	notes, err := deleteNotes(ctx, tx, id)
	if err != nil {
		return out, err
	}
	var banners [][]byte
	if out.events, banners, err = d.departEvents(ctx, tx, id); err != nil {
		return out, err
	}
	blobs, err := deleteFiles(ctx, tx, `uploader_id = ? AND message_id IS NULL AND NOT `+named+` AND NOT `+eventBanner, id)
	out.blobs = slices.Concat(blobs, notes, banners)
	return out, err
}

// departure is what a member's leaving takes, to tell once it commits.
type departure struct {
	blobs  [][]byte
	groups []groupDone
	events eventsGone
}

// publishDeparted tells the members of a member's groups that the den
// took them out, as they left the den, and everyone who saw them of the
// events their leaving canceled and the counts it changed (M17.4).
func (d *Den) publishDeparted(ctx context.Context, gone departure) error {
	var errs []error
	for _, g := range gone.groups {
		errs = append(errs, d.publishGroup(ctx, g))
	}
	errs = append(errs, d.publishGone(ctx, gone.events))
	return errors.Join(errs...)
}

// Remove takes a member out of the den, and with Ban keeps their username
// from coming back. Staff may remove members of a lower rank: moderators
// remove members, and the owner removes anyone else. Removing someone who
// already left can still ban them.
func (d *Den) Remove(ctx context.Context, s *Session, id string, req denproto.RemoveRequest) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner remove members")
	}
	if !slices.Contains(denproto.DeleteWindows, req.DeleteMessages) {
		return invalid("delete_messages must be one of %v seconds", denproto.DeleteWindows)
	}
	mid, err := denproto.ParseID(id)
	if err != nil {
		return errMemberNotFound
	}
	if mid == s.MemberID {
		return invalid("to leave the den, use /api/me/leave")
	}
	now := d.now().UnixMilli()
	info, _ := d.Info()
	var left bool
	var deleted []deletedMessage
	var readers []reader
	var unreacted []denproto.ReactionChange
	var blobs [][]byte
	var gone departure
	// The reactions a ban takes off wait their turn with edits (M15.3).
	if req.DeleteMessages > 0 {
		d.changes.Lock()
		defer d.changes.Unlock()
	}
	// Their groups' keys retire with their leaving them (M16.3), and their
	// events to come go (M17.4).
	d.keys.Lock()
	defer d.keys.Unlock()
	d.calendar.Lock()
	defer d.calendar.Unlock()
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var role string
		var leftAt, bannedAt, inviteID sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT role, left_at, banned_at, invite_id FROM den_members WHERE id = ?`, mid).
			Scan(&role, &leftAt, &bannedAt, &inviteID)
		if errors.Is(err, sql.ErrNoRows) {
			return errMemberNotFound
		}
		if err != nil {
			return err
		}
		if denproto.Rank(role) >= denproto.Rank(s.Role) {
			return forbidden("only the owner removes moderators, and nobody removes the owner")
		}
		if req.Ban && !bannedAt.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE den_members SET banned_at = ?, banned_by = ? WHERE id = ?`, now, s.MemberID, mid); err != nil {
				return err
			}
		}
		if req.RevokeInvite && inviteID.Valid {
			if _, err := tx.ExecContext(ctx, `DELETE FROM den_invites WHERE id = ? AND role = 'member'`, inviteID.Int64); err != nil {
				return err
			}
		}
		if req.DeleteMessages > 0 {
			// DMs too (M8): the den deletes them without reading them, and
			// a member flooding DMs leaves no other way to clear them.
			since := now - req.DeleteMessages*1000
			if deleted, readers, blobs, err = deleteRecent(ctx, tx, mid, since, time.UnixMilli(now)); err != nil {
				return err
			}
			// And the reactions they added then, in channels (M15.3).
			if unreacted, err = unreactRecent(ctx, tx, mid, since, time.UnixMilli(now)); err != nil {
				return err
			}
		}
		if leftAt.Valid {
			return nil
		}
		left = true
		gone, err = d.depart(ctx, tx, info.ID, mid, now)
		blobs = append(blobs, gone.blobs...)
		return err
	})
	if err != nil {
		return err
	}
	d.files.remove(blobs)
	d.cancelRequests(mid)
	reason := denproto.CloseReasonRemoved
	if req.Ban {
		reason = denproto.CloseReasonBanned
	}
	d.CloseMemberSockets(mid, denproto.CloseRevoked, reason)
	// A call held for a socket that had already closed ends too.
	d.memberGone(mid)
	d.log.Infof("Member %d removed by member %d (ban %t)", mid, s.MemberID, req.Ban)
	if left {
		if err := d.Hub.Publish(denproto.EventMemberLeft, denproto.MemberLeft{ID: denproto.FormatID(mid), LeftAt: now}, Everyone); err != nil {
			return err
		}
	}
	if err := d.publishDeleted(ctx, deleted); err != nil {
		return err
	}
	if err := d.publishReaders(ctx, readers); err != nil {
		return err
	}
	if err := d.publishDeparted(ctx, gone); err != nil {
		return err
	}
	return d.publishUnreacted(ctx, unreacted)
}

type deletedMessage struct {
	id, channel int64
	files       []string
}

// deleteRecent deletes a member's messages, in channels and DMs, from since
// on, and returns what it deleted, whom they counted for, read past them
// where they were all that lay unread (see passDeleted), and the blobs of
// their files to remove once tx commits.
func deleteRecent(ctx context.Context, tx *sql.Tx, member, since int64, now time.Time) ([]deletedMessage, []reader, [][]byte, error) {
	const where = `m.author_id = ? AND m.created_at >= ?`
	files, blobs, err := filesOf(ctx, tx, where, member, since)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id, m.channel_id FROM den_messages m WHERE `+where+` ORDER BY m.id`, member, since)
	if err != nil {
		return nil, nil, nil, err
	}
	var out []deletedMessage
	var ids []int64
	byChannel := map[int64][]int64{}
	for rows.Next() {
		var m deletedMessage
		if err := rows.Scan(&m.id, &m.channel); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		m.files = files[m.id]
		out = append(out, m)
		ids = append(ids, m.id)
		byChannel[m.channel] = append(byChannel[m.channel], m.id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	readers, err := readersOf(ctx, tx, ids)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM den_messages AS m WHERE `+where, member, since); err != nil {
		return nil, nil, nil, err
	}
	for _, m := range out {
		if err := logMessage(ctx, tx, m.id, m.channel, now); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := passDeleted(ctx, tx, readers, byChannel); err != nil {
		return nil, nil, nil, err
	}
	return out, readers, blobs, nil
}

func (d *Den) publishDeleted(ctx context.Context, deleted []deletedMessage) error {
	audiences := map[int64]Audience{}
	for _, m := range deleted {
		audience, ok := audiences[m.channel]
		if !ok {
			c, err := d.channel(ctx, d.db, m.channel)
			if err != nil {
				return err
			}
			audience = audienceOf(c)
			audiences[m.channel] = audience
		}
		event := denproto.MessageDeleted{ID: denproto.FormatID(m.id), ChannelID: denproto.FormatID(m.channel), Files: m.files}
		if err := d.Hub.Publish(denproto.EventMessageDeleted, event, audience); err != nil {
			return err
		}
	}
	return nil
}

// Leave takes the member out of the den at their own request. The owner
// can't leave: the den would have nobody to run it.
func (d *Den) Leave(ctx context.Context, s *Session) error {
	if s.Role == denproto.RoleOwner {
		return forbidden("the owner can't leave the den")
	}
	now := d.now().UnixMilli()
	info, _ := d.Info()
	var gone departure
	d.keys.Lock()
	defer d.keys.Unlock()
	d.calendar.Lock()
	defer d.calendar.Unlock()
	err := d.tx(ctx, func(tx *sql.Tx) (err error) {
		gone, err = d.depart(ctx, tx, info.ID, s.MemberID, now)
		return err
	})
	if err != nil {
		return err
	}
	d.files.remove(gone.blobs)
	d.cancelRequests(s.MemberID)
	d.CloseMemberSockets(s.MemberID, denproto.CloseRevoked, denproto.CloseReasonLeft)
	d.memberGone(s.MemberID)
	d.log.Infof("Member %d left", s.MemberID)
	return errors.Join(d.Hub.Publish(denproto.EventMemberLeft, denproto.MemberLeft{ID: denproto.FormatID(s.MemberID), LeftAt: now}, Everyone),
		d.publishDeparted(ctx, gone))
}

// Bans lists the banned members, most recent first.
func (d *Den) Bans(ctx context.Context, s *Session) (denproto.BanList, error) {
	list := denproto.BanList{Bans: []denproto.Ban{}}
	if !IsStaff(s.Role) {
		return list, forbidden("only moderators and the owner see bans")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT `+memberColumns+`, banned_at, banned_by FROM den_members
		WHERE banned_at IS NOT NULL ORDER BY banned_at DESC, id`)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var b denproto.Ban
		var by sql.NullInt64
		if b.Member, err = scanMember(rows.Scan, &b.BannedAt, &by); err != nil {
			return list, err
		}
		if by.Valid {
			b.BannedBy = denproto.FormatID(by.Int64)
		}
		list.Bans = append(list.Bans, b)
	}
	return list, rows.Err()
}

// Unban lets a banned member's username come back. They stay out of the
// den until someone invites them again. Staff lift a ban placed by someone
// of their rank or lower, as that member's role is now, so a ban the owner
// placed only the owner lifts (M8).
func (d *Den) Unban(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner lift bans")
	}
	mid, err := denproto.ParseID(id)
	if err != nil {
		return errMemberNotFound
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		// NULL when nobody placed it, or whoever did is gone from the table.
		var placer sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT b.role FROM den_members m LEFT JOIN den_members b ON b.id = m.banned_by
			WHERE m.id = ? AND m.banned_at IS NOT NULL`, mid).Scan(&placer)
		if errors.Is(err, sql.ErrNoRows) {
			return denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such ban")
		}
		if err != nil {
			return err
		}
		if placer.Valid && denproto.Rank(placer.String) > denproto.Rank(s.Role) {
			return forbidden("only staff of the rank of whoever placed a ban, or higher, lift it")
		}
		_, err = tx.ExecContext(ctx, `UPDATE den_members SET banned_at = NULL, banned_by = NULL WHERE id = ?`, mid)
		return err
	})
}
