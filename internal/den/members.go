package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func statusAD(id int64) []byte { return []byte("den_members.status:" + strconv.FormatInt(id, 10)) }

// memberColumns selects a member from den_members, with the size of the
// pictures on their profile, and whether each is animated, their
// identity's hash, their name's style and their status.
const memberColumns = `id, username, display_name, role, joined_at, left_at,
	avatar_id, (SELECT width FROM den_files WHERE den_files.id = den_members.avatar_id),
	(SELECT height FROM den_files WHERE den_files.id = den_members.avatar_id),
	(SELECT animated FROM den_files WHERE den_files.id = den_members.avatar_id),
	banner_id, (SELECT width FROM den_files WHERE den_files.id = den_members.banner_id),
	(SELECT height FROM den_files WHERE den_files.id = den_members.banner_id),
	(SELECT animated FROM den_files WHERE den_files.id = den_members.banner_id),
	identity, name_style, status, status_until`

// scanMember reads a member as memberColumns selects them, with a status
// whose end has passed left out.
func (d *Den) scanMember(scan func(...any) error, extra ...any) (denproto.Member, error) {
	var m denproto.Member
	var id int64
	var left, avatar, aw, ah, banner, bw, bh, until sql.NullInt64
	var aa, ba sql.NullBool
	var identity, status []byte
	var style sql.NullString
	err := scan(append([]any{&id, &m.Username, &m.DisplayName, &m.Role, &m.JoinedAt, &left, &avatar, &aw, &ah, &aa, &banner, &bw, &bh, &ba,
		&identity, &style, &status, &until}, extra...)...)
	if err != nil {
		return m, err
	}
	if style.Valid {
		if err = json.Unmarshal([]byte(style.String), &m.NameStyle); err != nil {
			err = fmt.Errorf("read a member's name style: %w", err)
		}
	}
	if len(status) > 0 && (!until.Valid || until.Int64 > d.now().UnixMilli()) {
		open, serr := d.v.Open(status, statusAD(id))
		if serr != nil {
			return m, errors.Join(err, fmt.Errorf("open a member's status: %w", serr))
		}
		m.Status, m.StatusUntil = string(open), until.Int64
	}
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
	m, err := d.scanMember(d.db.QueryRowContext(ctx, `SELECT `+memberColumns+` FROM den_members WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return m, errMemberNotFound
	}
	return m, err
}

// Profile returns a member with their bio, and the badges they earned
// whose events the session sees, but those they hid, which their
// own profile alone carries, marked. Any member may look at anyone
// who has been in the den, so old messages can show who wrote them.
func (d *Den) Profile(ctx context.Context, s *Session, id string) (denproto.MemberProfile, error) {
	m, err := d.profileOf(ctx, id)
	if err != nil {
		return denproto.MemberProfile{}, err
	}
	mid, _ := denproto.ParseID(m.ID)
	staff := IsStaff(s.Role)
	rids, err := memberRoleIDs(ctx, d.db, mid)
	if err != nil {
		return denproto.MemberProfile{}, err
	}
	if len(rids) > 0 {
		allowed, err := d.visibleRoleSet(ctx, d.db, s.MemberID, staff)
		if err != nil {
			return denproto.MemberProfile{}, err
		}
		m.RoleIDs = filterRoleIDs(formatIDs(rids), allowed)
	}
	p := denproto.MemberProfile{Member: m}
	p.Badges, err = d.profileBadges(ctx, mid, s.MemberID, staff, mid == s.MemberID)
	return p, err
}

// profileOf returns a member with their bio.
func (d *Den) profileOf(ctx context.Context, id string) (denproto.Member, error) {
	mid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Member{}, errMemberNotFound
	}
	var sealed []byte
	m, err := d.scanMember(d.db.QueryRowContext(ctx, `SELECT `+memberColumns+`, bio FROM den_members WHERE id = ?`, mid).Scan, &sealed)
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
// banner, or their name's style. A picture is an upload of theirs
// waiting to be used; the one it replaces is deleted.
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
	if req.NameStyle != nil {
		if err := denproto.CheckNameStyle(*req.NameStyle); err != nil {
			return denproto.Member{}, invalid("name_style: %v", err)
		}
		if req.NameStyle.IsZero() {
			sets = append(sets, "name_style = NULL")
		} else {
			style, err := json.Marshal(req.NameStyle)
			if err != nil {
				return denproto.Member{}, err
			}
			sets, args = append(sets, "name_style = ?"), append(args, string(style))
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
	m, err := d.profileOf(ctx, denproto.FormatID(s.MemberID))
	if err != nil || (len(sets) == 0 && req.Avatar == nil && req.Banner == nil) {
		return m, err
	}
	// Bios travel only on request; an update tells clients to fetch again.
	public := m
	public.Bio = ""
	return m, d.Hub.Publish(denproto.EventMemberUpdated, public, Everyone)
}

// SetStatus sets the member's own status until its end, or with none
// until they clear it, or clears it, and tells everyone.
func (d *Den) SetStatus(ctx context.Context, s *Session, req denproto.StatusRequest) (denproto.Member, error) {
	now := d.now()
	var status []byte
	var until any // NULL for none
	switch {
	case req.Status == "" && req.Until != 0:
		return denproto.Member{}, invalid("until: a status that's cleared has no end")
	case req.Status != "":
		line, err := denproto.CleanStatus(req.Status)
		if err != nil {
			return denproto.Member{}, invalid("status: %v", err)
		}
		if req.Until != 0 {
			if req.Until <= now.UnixMilli() {
				return denproto.Member{}, invalid("until: a status ends after now")
			}
			until = req.Until
		}
		if status, err = d.v.Seal([]byte(line), statusAD(s.MemberID)); err != nil {
			return denproto.Member{}, err
		}
	}
	err := d.tx(ctx, func(tx *sql.Tx) error {
		// NULL itself: a nil slice can be stored as an empty blob.
		var sealed any
		if status != nil {
			sealed = status
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_members SET status = ?, status_until = ? WHERE id = ?`, sealed, until, s.MemberID); err != nil {
			return err
		}
		return logMember(ctx, tx, s.MemberID, now)
	})
	if err != nil {
		return denproto.Member{}, err
	}
	m, err := d.Member(ctx, s.MemberID)
	if err != nil {
		return m, err
	}
	return m, d.Hub.Publish(denproto.EventMemberUpdated, m, Everyone)
}

// pruneStatuses removes the statuses whose end has passed. It tells
// nobody: every record that carried one carried its end.
func (d *Den) pruneStatuses(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `UPDATE den_members SET status = NULL, status_until = NULL WHERE status_until <= ?`, d.now().UnixMilli())
	return err
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
	var was string
	err = d.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT role FROM den_members WHERE id = ? AND left_at IS NULL AND role != 'owner'`, mid).Scan(&was)
		if errors.Is(err, sql.ErrNoRows) {
			return errMemberNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_members SET role = ? WHERE id = ?`, req.Role, mid); err != nil {
			return err
		}
		if err := logMember(ctx, tx, mid, d.now()); err != nil || was == req.Role {
			return err
		}
		return d.audit(ctx, tx, auditLine{kind: denproto.AuditRoleChanged, by: s.MemberID, member: mid,
			details: denproto.AuditDetails{Changes: []denproto.AuditChange{{Field: "role", From: was, To: req.Role}}}})
	})
	if err != nil {
		return denproto.Member{}, err
	}
	if was != req.Role {
		d.audited()
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
// hadn't sent, and their identity and record, which they publish
// again if they come back. The sounds and emoji they added are the den's, and
// stay. Their notes go, with their files, and their events to come, with
// their banners, and their marks on the rest; their live events go on to
// their end, banners and all.
// It returns what's to be told once tx commits: those files' blobs, to
// remove, and the events.
func (d *Den) depart(ctx context.Context, tx *sql.Tx, id, now int64) (departure, error) {
	var out departure
	for _, stmt := range []string{
		`UPDATE den_members SET left_at = ?1, role = 'member', identity = NULL, status = NULL, status_until = NULL WHERE id = ?2`,
		`DELETE FROM den_identities WHERE member_id = ?2`,
		`DELETE FROM den_records WHERE member_id = ?2`,
		`DELETE FROM den_devices WHERE member_id = ?2`, // and, through them, sessions
		`DELETE FROM den_recovery_codes WHERE member_id = ?2`,
		`DELETE FROM den_read_states WHERE member_id = ?2`,
		`DELETE FROM den_mentions WHERE member_id = ?2`,
		`DELETE FROM den_member_roles WHERE member_id = ?2`,
		`DELETE FROM den_invites WHERE created_by = ?2 AND role = 'member'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, now, id); err != nil {
			return out, err
		}
	}
	if err := logMember(ctx, tx, id, time.UnixMilli(now)); err != nil {
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
	events eventsGone
}

// publishDeparted tells everyone who saw a member who left of the events
// their leaving canceled and the counts it changed.
func (d *Den) publishDeparted(ctx context.Context, gone departure) error {
	return d.publishGone(ctx, gone.events)
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
	var readers []reader
	var unreacted []denproto.ReactionChange
	var blobs [][]byte
	var gone departure
	var audited bool
	// The reactions a ban takes off wait their turn with edits.
	if req.DeleteMessages > 0 {
		d.changes.Lock()
		defer d.changes.Unlock()
	}
	// Their events to come go.
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
		line := auditLine{kind: denproto.AuditMemberRemoved, by: s.MemberID, member: mid,
			details: denproto.AuditDetails{DeleteMessages: req.DeleteMessages}}
		if req.Ban && !bannedAt.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE den_members SET banned_at = ?, banned_by = ? WHERE id = ?`, now, s.MemberID, mid); err != nil {
				return err
			}
			line.details.Ban = true
		}
		if req.RevokeInvite && inviteID.Valid {
			res, err := tx.ExecContext(ctx, `DELETE FROM den_invites WHERE id = ? AND role = 'member'`, inviteID.Int64)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			line.details.RevokeInvite = n > 0
		}
		// For a member who left, only these change anything.
		if !leftAt.Valid || line.details.Ban || line.details.DeleteMessages > 0 || line.details.RevokeInvite {
			if err := d.audit(ctx, tx, line); err != nil {
				return err
			}
			audited = true
		}
		if req.DeleteMessages > 0 {
			// DMs too: the den deletes them without reading them, and
			// a member flooding DMs leaves no other way to clear them.
			since := now - req.DeleteMessages*1000
			if deleted, readers, blobs, err = deleteRecent(ctx, tx, mid, since, time.UnixMilli(now)); err != nil {
				return err
			}
			// And the reactions they added then, in channels.
			if unreacted, err = unreactRecent(ctx, tx, mid, since, time.UnixMilli(now)); err != nil {
				return err
			}
		}
		if leftAt.Valid {
			return nil
		}
		left = true
		gone, err = d.depart(ctx, tx, mid, now)
		blobs = append(blobs, gone.blobs...)
		return err
	})
	if err != nil {
		return err
	}
	if audited {
		d.audited()
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
	last        *string
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
	lastByChannel := map[int64]*string{}
	for ch := range byChannel {
		last, err := channelLastMessage(ctx, tx, ch)
		if err != nil {
			return nil, nil, nil, err
		}
		lastByChannel[ch] = last
	}
	for i, m := range out {
		out[i].last = lastByChannel[m.channel]
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
			audience = d.audienceOf(c)
			audiences[m.channel] = audience
		}
		event := denproto.MessageDeleted{ID: denproto.FormatID(m.id), ChannelID: denproto.FormatID(m.channel), Files: m.files, LastMessage: m.last}
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
	var gone departure
	d.calendar.Lock()
	defer d.calendar.Unlock()
	err := d.tx(ctx, func(tx *sql.Tx) (err error) {
		gone, err = d.depart(ctx, tx, s.MemberID, now)
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
		if b.Member, err = d.scanMember(rows.Scan, &b.BannedAt, &by); err != nil {
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
// placed only the owner lifts.
func (d *Den) Unban(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner lift bans")
	}
	mid, err := denproto.ParseID(id)
	if err != nil {
		return errMemberNotFound
	}
	err = d.tx(ctx, func(tx *sql.Tx) error {
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
		if _, err = tx.ExecContext(ctx, `UPDATE den_members SET banned_at = NULL, banned_by = NULL WHERE id = ?`, mid); err != nil {
			return err
		}
		return d.audit(ctx, tx, auditLine{kind: denproto.AuditBanLifted, by: s.MemberID, member: mid})
	})
	if err != nil {
		return err
	}
	d.audited()
	return nil
}
