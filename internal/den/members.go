package den

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

var errMemberNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such member")

func forbidden(format string, args ...any) error {
	return denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, format, args...)
}

func bioAD(id int64) []byte { return []byte("den_members.bio:" + strconv.FormatInt(id, 10)) }

// memberColumns selects a member from den_members, with the size of the
// pictures on their profile.
const memberColumns = `id, username, display_name, role, joined_at, left_at,
	avatar_id, (SELECT width FROM den_files WHERE den_files.id = den_members.avatar_id),
	(SELECT height FROM den_files WHERE den_files.id = den_members.avatar_id),
	banner_id, (SELECT width FROM den_files WHERE den_files.id = den_members.banner_id),
	(SELECT height FROM den_files WHERE den_files.id = den_members.banner_id)`

func scanMember(scan func(...any) error, extra ...any) (denproto.Member, error) {
	var m denproto.Member
	var id int64
	var left, avatar, aw, ah, banner, bw, bh sql.NullInt64
	err := scan(append([]any{&id, &m.Username, &m.DisplayName, &m.Role, &m.JoinedAt, &left, &avatar, &aw, &ah, &banner, &bw, &bh}, extra...)...)
	m.ID = denproto.FormatID(id)
	if left.Valid {
		m.LeftAt = left.Int64
	}
	if avatar.Valid {
		m.Avatar = denproto.Image{ID: denproto.FormatID(avatar.Int64), Width: int(aw.Int64), Height: int(ah.Int64)}
	}
	if banner.Valid {
		m.Banner = denproto.Image{ID: denproto.FormatID(banner.Int64), Width: int(bw.Int64), Height: int(bh.Int64)}
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
		if err := denproto.CheckBio(*req.Bio); err != nil {
			return denproto.Member{}, invalid("bio: %v", err)
		}
		if *req.Bio == "" {
			// NULL itself: a nil slice can be stored as an empty blob.
			sets = append(sets, "bio = NULL")
		} else {
			sealed, err := d.v.Seal([]byte(*req.Bio), bioAD(s.MemberID))
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
			_, err := tx.ExecContext(ctx, "UPDATE den_members SET "+strings.Join(sets, ", ")+" WHERE id = ?", append(args, s.MemberID)...)
			return err
		}
		return nil
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
	res, err := d.db.ExecContext(ctx, `UPDATE den_members SET role = ? WHERE id = ? AND left_at IS NULL AND role != 'owner'`, req.Role, mid)
	if err != nil {
		return denproto.Member{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return denproto.Member{}, errMemberNotFound
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
	return m, nil
}

// depart takes a member out of the den within tx. Their row stays, so
// their messages keep a name and a returning member can reclaim it, but
// they lose their role, devices and sessions, recovery codes, read state,
// any invites they made, the pictures on their profile and the uploads
// they hadn't sent. It returns those files' blobs, to remove once tx
// commits.
func depart(ctx context.Context, tx *sql.Tx, id, now int64) ([][]byte, error) {
	for _, stmt := range []string{
		`UPDATE den_members SET left_at = ?1, role = 'member' WHERE id = ?2`,
		`DELETE FROM den_devices WHERE member_id = ?2`, // and, through them, sessions
		`DELETE FROM den_recovery_codes WHERE member_id = ?2`,
		`DELETE FROM den_read_states WHERE member_id = ?2`,
		`DELETE FROM den_mentions WHERE member_id = ?2`,
		`DELETE FROM den_invites WHERE created_by = ?2 AND role = 'member'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, now, id); err != nil {
			return nil, err
		}
	}
	return deleteFiles(ctx, tx, `uploader_id = ? AND message_id IS NULL`, id)
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
	var left bool
	var deleted []deletedMessage
	var blobs [][]byte
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
			// DMs stay: staff can't see them, so they don't clean them up.
			if deleted, blobs, err = deleteRecent(ctx, tx, mid, now-req.DeleteMessages*1000); err != nil {
				return err
			}
		}
		if leftAt.Valid {
			return nil
		}
		left = true
		kept, err := depart(ctx, tx, mid, now)
		blobs = append(blobs, kept...)
		return err
	})
	if err != nil {
		return err
	}
	d.files.remove(blobs)
	reason := denproto.CloseReasonRemoved
	if req.Ban {
		reason = denproto.CloseReasonBanned
	}
	d.CloseMemberSockets(mid, denproto.CloseRevoked, reason)
	d.log.Infof("Member %d removed by member %d (ban %t)", mid, s.MemberID, req.Ban)
	if left {
		if err := d.Hub.Publish(denproto.EventMemberLeft, denproto.MemberLeft{ID: denproto.FormatID(mid), LeftAt: now}, Everyone); err != nil {
			return err
		}
	}
	return d.publishDeleted(ctx, deleted)
}

type deletedMessage struct {
	id, channel int64
	files       []string
}

// deleteRecent deletes a member's messages in channels, not DMs, from since
// on, and returns what it deleted, and the blobs of their files to remove
// once tx commits.
func deleteRecent(ctx context.Context, tx *sql.Tx, member, since int64) ([]deletedMessage, [][]byte, error) {
	const where = `m.author_id = ? AND m.created_at >= ? AND m.channel_id IN (SELECT id FROM den_channels WHERE kind != 'dm')`
	files, blobs, err := filesOf(ctx, tx, where, member, since)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id, m.channel_id FROM den_messages m WHERE `+where, member, since)
	if err != nil {
		return nil, nil, err
	}
	var out []deletedMessage
	for rows.Next() {
		var m deletedMessage
		if err := rows.Scan(&m.id, &m.channel); err != nil {
			rows.Close()
			return nil, nil, err
		}
		m.files = files[m.id]
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM den_messages AS m WHERE `+where, member, since)
	return out, blobs, err
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
	var blobs [][]byte
	err := d.tx(ctx, func(tx *sql.Tx) (err error) {
		blobs, err = depart(ctx, tx, s.MemberID, now)
		return err
	})
	if err != nil {
		return err
	}
	d.files.remove(blobs)
	d.CloseMemberSockets(s.MemberID, denproto.CloseRevoked, denproto.CloseReasonLeft)
	d.log.Infof("Member %d left", s.MemberID)
	return d.Hub.Publish(denproto.EventMemberLeft, denproto.MemberLeft{ID: denproto.FormatID(s.MemberID), LeftAt: now}, Everyone)
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
// den until someone invites them again.
func (d *Den) Unban(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner lift bans")
	}
	mid, err := denproto.ParseID(id)
	if err != nil {
		return errMemberNotFound
	}
	res, err := d.db.ExecContext(ctx, `UPDATE den_members SET banned_at = NULL, banned_by = NULL WHERE id = ? AND banned_at IS NOT NULL`, mid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such ban")
	}
	return nil
}
