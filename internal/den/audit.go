package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// The audit log: staff's acts in the den, a line each, which moderators
// and the owner read in Manage. A line is written only where its act
// changed something, in the act's transaction where it has one, so the two
// commit together, and a call's act, held in memory, writes its line just
// after; the den's timer sweeps it once denproto.AuditKept passes. A
// member's own acts write none, nor does anything in a DM, where staff
// have no powers.

func auditAD(id int64) []byte { return []byte("den_audit.details:" + strconv.FormatInt(id, 10)) }

// auditLine is a line to write: its kind, the member who acted, 0 for the
// den's machine, the member it was on, 0 for none, and the rest.
type auditLine struct {
	kind    string
	by      int64
	member  int64
	details denproto.AuditDetails
}

// execer runs statements: a *sql.DB or a *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// audit writes a line within q, its details sealed with the data key under
// its ID. The act's caller has the timer plan for it once q commits, with
// audited.
func (d *Den) audit(ctx context.Context, q execer, l auditLine) error {
	res, err := q.ExecContext(ctx, `INSERT INTO den_audit (at, kind, done_by, member_id, details) VALUES (?, ?, ?, ?, x'')`,
		d.now().UnixMilli(), l.kind, nullID(l.by), nullID(l.member))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(l.details)
	if err != nil {
		return err
	}
	sealed, err := d.v.Seal(plain, auditAD(id))
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `UPDATE den_audit SET details = ? WHERE id = ?`, sealed, id)
	return err
}

// audited has the timer go by when a line just written goes, which an
// idle timer otherwise never would.
func (d *Den) audited() { d.dueAt(d.now().Add(denproto.AuditKept).UnixMilli()) }

// auditNow writes a line of its own, for an act that writes nothing else.
func (d *Den) auditNow(ctx context.Context, l auditLine) error {
	if err := d.tx(ctx, func(tx *sql.Tx) error { return d.audit(ctx, tx, l) }); err != nil {
		return err
	}
	d.audited()
	return nil
}

// auditCall writes the line of staff's act on a member's call: in the
// call's channel, if they were in one, and for a move the channel it
// went to.
func (d *Den) auditCall(ctx context.Context, kind string, s *Session, member, channel int64, to string) error {
	l := auditLine{kind: kind, by: s.MemberID, member: member}
	if channel != 0 {
		// A channel deleted since the act leaves its line without one.
		c, err := d.channel(ctx, d.db, channel)
		switch {
		case err == nil:
			l.details.ChannelID, l.details.Channel = c.ID, c.Name
			if to != "" {
				l.details.Changes = []denproto.AuditChange{{Field: "channel", From: c.Name, To: to}}
			}
		case !errors.Is(err, errChannelNotFound):
			return err
		}
	}
	return d.auditNow(ctx, l)
}

// namedLine is the line of a change to one of the den's named files, an
// emoji or a sound: by its ID and its name, and a rename's name before.
func namedLine(kind string, s *Session, id int64, name, was string) auditLine {
	l := auditLine{kind: kind, by: s.MemberID, details: denproto.AuditDetails{Ref: denproto.FormatID(id), Name: name}}
	if was != "" {
		l.details.Changes = []denproto.AuditChange{{Field: "name", From: was, To: name}}
	}
	return l
}

// auditValueMax matches denproto's maxAuditValueRunes (200) so comma-separated
// lists of role or channel names always pass CheckAuditLine.
const auditValueMax = 200

// auditNames joins names with ", ", sorted case-insensitively, bounded to
// auditValueMax runes with a " (+N more)" suffix when truncated.
func auditNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	sorted := slices.Clone(names)
	slices.SortFunc(sorted, func(a, b string) int {
		if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	full := strings.Join(sorted, ", ")
	if utf8.RuneCountInString(full) <= auditValueMax {
		return full
	}
	var b strings.Builder
	used := 0
	for i, name := range sorted {
		rem := len(sorted) - i
		suffix := fmt.Sprintf(" (+%d more)", rem)
		add := name
		if i > 0 {
			add = ", " + name
		}
		nextRem := len(sorted) - i - 1
		nextSuffix := ""
		if nextRem > 0 {
			nextSuffix = fmt.Sprintf(" (+%d more)", nextRem)
		}
		if used+utf8.RuneCountInString(add)+utf8.RuneCountInString(nextSuffix) > auditValueMax {
			if i == 0 {
				return fmt.Sprintf("%d items", len(sorted))
			}
			b.WriteString(suffix)
			return b.String()
		}
		b.WriteString(add)
		used += utf8.RuneCountInString(add)
	}
	return b.String()
}

// channelChanges lists what an update changed of a channel within q: its
// name, description, visibility, bitrate or group, by its name.
func channelChanges(ctx context.Context, q querier, before, after denproto.Channel) ([]denproto.AuditChange, error) {
	var out []denproto.AuditChange
	if before.Name != after.Name {
		out = append(out, denproto.AuditChange{Field: "name", From: before.Name, To: after.Name})
	}
	if before.Description != after.Description {
		out = append(out, denproto.AuditChange{Field: "description"})
	}
	if before.StaffOnly != after.StaffOnly {
		out = append(out, denproto.AuditChange{Field: "staff_only", From: fmt.Sprint(before.StaffOnly), To: fmt.Sprint(after.StaffOnly)})
	}
	if !slices.Equal(before.RoleIDs, after.RoleIDs) {
		var ids []int64
		for _, s := range slices.Concat(before.RoleIDs, after.RoleIDs) {
			if id, err := denproto.ParseID(s); err == nil && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		names, err := roleNamesMap(ctx, q, ids)
		if err != nil {
			return nil, err
		}
		label := func(list []string) string {
			ns := make([]string, 0, len(list))
			for _, s := range list {
				if id, err := denproto.ParseID(s); err == nil && names[id] != "" {
					ns = append(ns, names[id])
				}
			}
			return auditNames(ns)
		}
		out = append(out, denproto.AuditChange{Field: "roles", From: label(before.RoleIDs), To: label(after.RoleIDs)})
	}
	if before.Bitrate != after.Bitrate {
		out = append(out, denproto.AuditChange{Field: "bitrate", From: fmt.Sprint(before.Bitrate), To: fmt.Sprint(after.Bitrate)})
	}
	from, to := "", ""
	if before.GroupID != nil {
		from = *before.GroupID
	}
	if after.GroupID != nil {
		to = *after.GroupID
	}
	if from != to {
		names := map[string]string{"": ""}
		for _, id := range []string{from, to} {
			if id == "" {
				continue
			}
			var name string
			gid, _ := denproto.ParseID(id)
			if err := q.QueryRowContext(ctx, `SELECT name FROM den_groups WHERE id = ?`, gid).Scan(&name); err != nil {
				return nil, err
			}
			names[id] = name
		}
		out = append(out, denproto.AuditChange{Field: "group", From: names[from], To: names[to]})
	}
	return out, nil
}

// eventLine is the line of staff's act on another member's event, named
// as it was then.
func eventLine(kind string, s *Session, e *eventRow, changes []denproto.AuditChange) auditLine {
	return auditLine{kind: kind, by: s.MemberID, member: e.madeBy,
		details: denproto.AuditDetails{Ref: denproto.FormatID(e.id), Name: e.details.Name, Changes: changes}}
}

// eventChanges lists what an update changed of an event within q: its
// name, place or channel, times, and whether its description, banner or
// badge changed.
func (d *Den) eventChanges(ctx context.Context, q querier, before, after *eventRow) ([]denproto.AuditChange, error) {
	var out []denproto.AuditChange
	add := func(field, from, to string) {
		if from != to {
			out = append(out, denproto.AuditChange{Field: field, From: from, To: to})
		}
	}
	add("name", before.details.Name, after.details.Name)
	if before.details.Description != after.details.Description {
		out = append(out, denproto.AuditChange{Field: "description"})
	}
	add("place", before.details.Place, after.details.Place)
	if before.channel != after.channel {
		names := map[int64]string{0: ""}
		for _, id := range []int64{before.channel, after.channel} {
			if id == 0 {
				continue
			}
			c, err := d.channel(ctx, q, id)
			if err != nil {
				return nil, err
			}
			names[id] = c.Name
		}
		out = append(out, denproto.AuditChange{Field: "channel", From: names[before.channel], To: names[after.channel]})
	}
	add("starts_at", fmt.Sprint(before.startsAt), fmt.Sprint(after.startsAt))
	add("ends_at", fmt.Sprint(before.endsAt), fmt.Sprint(after.endsAt))
	if before.bannerID != after.bannerID {
		out = append(out, denproto.AuditChange{Field: "banner"})
	}
	if (before.badge == nil) != (after.badge == nil) || (before.badge != nil && (before.badge.title != after.badge.title || before.badge.namedID != after.badge.namedID)) {
		out = append(out, denproto.AuditChange{Field: "badge"})
	}
	return out, nil
}

// denChanges lists what an update changed of the den's settings, each by
// its column's name.
func denChanges(before, after denproto.Den) []denproto.AuditChange {
	emojis := func(d denproto.Den) int {
		if d.EmojiLimits == nil {
			return denproto.DefaultEmojiLimits.Emojis
		}
		return d.EmojiLimits.Emojis
	}
	events := func(d denproto.Den) int {
		if d.EventLimits == nil {
			return denproto.DefaultEventLimits.MemberEvents
		}
		return d.EventLimits.MemberEvents
	}
	var out []denproto.AuditChange
	for _, f := range []struct {
		field    string
		from, to any
	}{
		{"name", before.Name, after.Name},
		{"url", before.URL, after.URL},
		{"file_size", before.Limits.FileSize, after.Limits.FileSize},
		{"member_storage", before.Limits.MemberStorage, after.Limits.MemberStorage},
		{"den_storage", before.Limits.DenStorage, after.Limits.DenStorage},
		{"call_members", before.CallLimits.Members, after.CallLimits.Members},
		{"den_callers", before.CallLimits.Callers, after.CallLimits.Callers},
		{"shares", before.CallLimits.Shares, after.CallLimits.Shares},
		{"share_viewers", before.CallLimits.ShareViewers, after.CallLimits.ShareViewers},
		{"share_bitrate", before.CallLimits.ShareBitrate, after.CallLimits.ShareBitrate},
		{"share_height", before.CallLimits.ShareHeight, after.CallLimits.ShareHeight},
		{"share_fps", before.CallLimits.ShareFPS, after.CallLimits.ShareFPS},
		{"retention", before.Retention, after.Retention},
		{"sounds", before.SoundLimits.Sounds, after.SoundLimits.Sounds},
		{"sound_longest", before.SoundLimits.Longest, after.SoundLimits.Longest},
		{"sound_wait", before.SoundLimits.Wait, after.SoundLimits.Wait},
		{"emojis", emojis(before), emojis(after)},
		{"member_events", events(before), events(after)},
	} {
		if from, to := fmt.Sprint(f.from), fmt.Sprint(f.to); from != to {
			out = append(out, denproto.AuditChange{Field: f.field, From: from, To: to})
		}
	}
	return out
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// AuditLog answers staff with a page of the log, newest first, from
// before the line the page before ended at, or from the newest.
func (d *Den) AuditLog(ctx context.Context, s *Session, before string) (denproto.AuditLog, error) {
	log := denproto.AuditLog{Lines: []denproto.AuditLine{}}
	if !IsStaff(s.Role) {
		return log, forbidden("only moderators and the owner read the audit log")
	}
	from := int64(-1)
	if before != "" {
		var err error
		if from, err = denproto.ParseID(before); err != nil {
			return log, invalid("before: not a line of the log")
		}
	}
	// A line past its time stays out though the timer, delayed as a
	// sleeping host delays it, hasn't swept it yet.
	rows, err := d.db.QueryContext(ctx, `SELECT id, at, kind, done_by, member_id, details FROM den_audit
		WHERE (? < 0 OR id < ?) AND at > ? ORDER BY id DESC LIMIT ?`,
		from, from, d.now().Add(-denproto.AuditKept).UnixMilli(), denproto.AuditPage+1)
	if err != nil {
		return log, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var by, member sql.NullInt64
		var sealed []byte
		var l denproto.AuditLine
		if err := rows.Scan(&id, &l.At, &l.Kind, &by, &member, &sealed); err != nil {
			return log, err
		}
		plain, err := d.v.Open(sealed, auditAD(id))
		if err != nil {
			return log, err
		}
		if err := json.Unmarshal(plain, &l.AuditDetails); err != nil {
			return log, err
		}
		l.ID = denproto.FormatID(id)
		if by.Valid {
			l.By = denproto.FormatID(by.Int64)
		}
		if member.Valid {
			l.MemberID = denproto.FormatID(member.Int64)
		}
		log.Lines = append(log.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return log, err
	}
	if len(log.Lines) > denproto.AuditPage {
		log.Lines = log.Lines[:denproto.AuditPage]
		log.Next = log.Lines[denproto.AuditPage-1].ID
	}
	return log, nil
}

// sweepAudit deletes the lines whose time passed.
func (d *Den) sweepAudit(ctx context.Context) error {
	res, err := d.db.ExecContext(ctx, `DELETE FROM den_audit WHERE at <= ?`, d.now().Add(-denproto.AuditKept).UnixMilli())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		d.log.Infof("Swept %d lines of the audit log", n)
	}
	return nil
}

// MembersSeen answers staff with when each member in the den was last
// seen, the latest of their devices, by its day in UTC: enough to find who
// hasn't been on in a long while, and less than presence says.
func (d *Den) MembersSeen(ctx context.Context, s *Session) (denproto.SeenList, error) {
	list := denproto.SeenList{Seen: []denproto.Seen{}}
	if !IsStaff(s.Role) {
		return list, forbidden("only moderators and the owner see when members were last seen")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT d.member_id, max(d.last_seen_at) FROM den_devices d
		JOIN den_members m ON m.id = d.member_id WHERE m.left_at IS NULL GROUP BY d.member_id ORDER BY d.member_id`)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	day := denproto.SeenDay.Milliseconds()
	for rows.Next() {
		var member, at int64
		if err := rows.Scan(&member, &at); err != nil {
			return list, err
		}
		list.Seen = append(list.Seen, denproto.Seen{MemberID: denproto.FormatID(member), Day: at - at%day})
	}
	return list, rows.Err()
}
