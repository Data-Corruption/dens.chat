package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Badges (M17.5): staff attach a badge to an event in a voice channel, a
// picture checked as an emoji's is and a title. The picture is one of the
// den's named files, of kind badge, so it stays the den's whoever added it,
// opening to everyone who sees its event; its name is its file's ID, since
// titles repeat, and the title is sealed with the data key, as the event's
// own text is.
//
// A member earns an event's badge by a minute at a stretch in its call
// while it's live, their call held or not, once. The den times each
// stretch from when the member's call joined the channel, or from the
// event's start if that came later, which the call registry holds in
// memory for as long as the call lasts; it writes only the award, the
// badge, the member and when, so its database holds nothing of who was in
// the call. The den's timer gives each badge as its stretch comes due.
//
// A member's profile carries the badges they earned whose events the asker
// sees, as it carries their bio, each with its event's name and start,
// which say what it was for once the event left the list. A member hides
// any of theirs from their card (M17.6), which only keeps it tidy: the den
// still knows who earned what, and their own copy of their profile still
// carries a hidden one, marked, for them to show again.

// kindBadge is a named file's kind for a badge's picture.
const kindBadge = "badge"

// badgeStretch is how long a member stays in a live event's call to earn
// its badge: with ShortTimes, as a development instance's den takes for
// the browser checks, three seconds.
func (d *Den) badgeStretch() time.Duration {
	if d.ShortTimes {
		return 3 * time.Second
	}
	return denproto.BadgeStretch
}

// badgeChange is what a request asks of an event's badge: nothing, to take
// it off, or a new title, a new picture, or both.
type badgeChange struct {
	asked   bool
	remove  bool
	title   *string
	picture string
}

// cleanBadgeRequest checks what a request asks of an event's badge, which
// staff alone change, and cleans its title.
func cleanBadgeRequest(req denproto.EventRequest, s *Session) (badgeChange, error) {
	var ch badgeChange
	if req.Badge == nil && req.BadgeTitle == nil {
		return ch, nil
	}
	if !IsStaff(s.Role) {
		return ch, forbidden("only moderators and the owner attach badges")
	}
	ch.asked = true
	if req.Badge != nil && *req.Badge == "" {
		if req.BadgeTitle != nil {
			return ch, invalid("a badge taken off takes no title")
		}
		ch.remove = true
		return ch, nil
	}
	if req.BadgeTitle != nil {
		title, err := denproto.CleanBadgeTitle(*req.BadgeTitle)
		if err != nil {
			return ch, invalid("badge_title: %v", err)
		}
		ch.title = &title
	}
	if req.Badge != nil {
		ch.picture = *req.Badge
	}
	return ch, nil
}

// setBadge applies a badge change to an event within tx: had is its badge
// before, if any, and voice whether its place after the change is a voice
// channel, the one place a badge goes. Attaching one takes a picture and a
// title; a picture an upload of the staff member's waiting to be used,
// checked as an emoji is, which becomes one of the den's named files. A
// picture replaced, or a badge taken off, goes, and its awards with it. It
// returns the blobs to remove once tx commits.
func (d *Den) setBadge(ctx context.Context, tx *sql.Tx, event int64, had *badgeRow, ch badgeChange, voice bool, s *Session,
	now time.Time) ([][]byte, error) {
	switch {
	case !ch.asked:
		if had != nil && !voice {
			return nil, invalid("an event with a badge stays in a voice channel")
		}
		return nil, nil
	case ch.remove:
		if had == nil {
			return nil, nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_badges WHERE event_id = ?`, event); err != nil {
			return nil, err
		}
		return deleteNamed(ctx, tx, had.namedID)
	case !voice:
		return nil, invalid("a badge goes on an event in a voice channel")
	case had == nil && (ch.picture == "" || ch.title == nil):
		return nil, invalid("a badge needs a picture and a title")
	}
	var sealed []byte
	if ch.title != nil {
		var err error
		if sealed, err = d.v.Seal([]byte(*ch.title), badgeTitleAD(event)); err != nil {
			return nil, err
		}
	}
	var named int64
	if ch.picture != "" {
		fid, err := checkBadgePicture(ctx, tx, s.MemberID, ch.picture, now)
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_named_files (kind, name, file_id, added_by, added_at) VALUES (?, ?, ?, ?, ?)`,
			kindBadge, strconv.FormatInt(fid, 10), fid, s.MemberID, now.UnixMilli())
		if err != nil {
			return nil, err
		}
		if named, err = res.LastInsertId(); err != nil {
			return nil, err
		}
	}
	if had == nil {
		_, err := tx.ExecContext(ctx, `INSERT INTO den_badges (event_id, named_id, title) VALUES (?, ?, ?)`, event, named, sealed)
		return nil, err
	}
	if sealed != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE den_badges SET title = ? WHERE event_id = ?`, sealed, event); err != nil {
			return nil, err
		}
	}
	if named == 0 {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE den_badges SET named_id = ? WHERE event_id = ?`, named, event); err != nil {
		return nil, err
	}
	return deleteNamed(ctx, tx, had.namedID)
}

// deleteNamed deletes one of the den's named files within tx, with its
// file, and returns the file's blobs to remove once tx commits.
func deleteNamed(ctx context.Context, tx *sql.Tx, id int64) ([][]byte, error) {
	var file int64
	if err := tx.QueryRowContext(ctx, `DELETE FROM den_named_files WHERE id = ? RETURNING file_id`, id).Scan(&file); err != nil {
		return nil, err
	}
	return deleteFiles(ctx, tx, `id = ?`, file)
}

// checkBadgePicture checks an upload a badge's picture takes: the staff
// member's own, waiting to be used, and an emoji's image. It returns the
// upload's ID.
func checkBadgePicture(ctx context.Context, tx *sql.Tx, member int64, id string, now time.Time) (int64, error) {
	fid, err := denproto.ParseID(id)
	if err != nil {
		return 0, invalid("badge: no such upload waiting to be used")
	}
	var typ string
	var w, h sql.NullInt64
	var size int64
	var animated bool
	err = tx.QueryRowContext(ctx, `SELECT type, width, height, size, animated FROM den_files
		WHERE id = ? AND uploader_id = ? AND sealed = 0 AND replaces IS NULL AND created_at > ? AND `+unused,
		fid, member, now.Add(-uploadExpiry).UnixMilli()).Scan(&typ, &w, &h, &size, &animated)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, invalid("badge: no such upload waiting to be used")
	}
	if err != nil {
		return 0, err
	}
	if err := checkEmojiImage(typ, w, h, size, animated); err != nil {
		return 0, err
	}
	return fid, nil
}

// eventBadges lists the named files that keep the pictures of the badges
// of the events a condition on den_events selects, within tx, before the
// events go.
func eventBadges(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT named_id FROM den_badges WHERE event_id IN (SELECT id FROM den_events WHERE `+where+`)`, args...)
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

// liveBadge is a live event's badge: its call's channel, the event's
// times, and who earned it.
type liveBadge struct {
	event, channel int64
	starts, ends   int64
	earned         map[int64]bool
}

// liveBadges reads the live events with badges in voice channels, and who
// earned each.
func liveBadges(ctx context.Context, q querier) ([]liveBadge, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.id, e.channel_id, e.starts_at, e.ends_at FROM den_events e JOIN den_badges b ON b.event_id = e.id
		WHERE e.state = 'live' AND e.channel_id IS NOT NULL ORDER BY e.id`)
	if err != nil {
		return nil, err
	}
	var out []liveBadge
	for rows.Next() {
		b := liveBadge{earned: map[int64]bool{}}
		if err := rows.Scan(&b.event, &b.channel, &b.starts, &b.ends); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		earned, err := q.QueryContext(ctx, `SELECT member_id FROM den_badge_awards WHERE event_id = ?`, out[i].event)
		if err != nil {
			return nil, err
		}
		for earned.Next() {
			var id int64
			if err := earned.Scan(&id); err != nil {
				earned.Close()
				return nil, err
			}
			out[i].earned[id] = true
		}
		earned.Close()
		if err := earned.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// award is a badge a member's stretch earns them, and when it reaches the
// stretch's length.
type award struct {
	event, member, at int64
}

// stretches lists, for each live badge, the members in its call who
// haven't earned it, and when each one's stretch reaches its length: from
// when their call joined the channel, held since or not, or from the
// event's start if that came later. One that reaches it only after the
// event's end earns nothing.
func (d *Den) stretches(badges []liveBadge) []award {
	stretch := d.badgeStretch().Milliseconds()
	r := &d.calls
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []award
	for _, b := range badges {
		room := r.rooms[b.channel]
		if room == nil {
			continue
		}
		for _, c := range room.members {
			if b.earned[c.member] {
				continue
			}
			if at := max(c.joined.UnixMilli(), b.starts) + stretch; at <= b.ends {
				out = append(out, award{b.event, c.member, at})
			}
		}
	}
	return out
}

// nextAward is when the next badge comes due, as Unix milliseconds, or 0
// for none.
func (d *Den) nextAward(ctx context.Context) int64 {
	badges, err := liveBadges(ctx, d.db)
	if err != nil {
		d.log.Errorf("plan badges: %v", err)
		return 0
	}
	var next int64
	if len(badges) > 0 {
		for _, a := range d.stretches(badges) {
			if next == 0 || a.at < next {
				next = a.at
			}
		}
	}
	return next
}

// awardDue gives the badges whose stretches came due, each once, as of
// when its stretch reached its length.
func (d *Den) awardDue(ctx context.Context) error {
	d.calendar.Lock()
	defer d.calendar.Unlock()
	badges, err := liveBadges(ctx, d.db)
	if err != nil || len(badges) == 0 {
		return err
	}
	now := d.now().UnixMilli()
	var due []award
	for _, a := range d.stretches(badges) {
		if a.at <= now {
			due = append(due, a)
		}
	}
	if len(due) == 0 {
		return nil
	}
	var given []award
	err = d.tx(ctx, func(tx *sql.Tx) error {
		for _, a := range due {
			res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO den_badge_awards (event_id, member_id, earned_at) VALUES (?, ?, ?)`,
				a.event, a.member, a.at)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				given = append(given, a)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, a := range given {
		d.log.Infof("Member %d earned the badge of event %d", a.member, a.event)
	}
	return nil
}

// errBadgeNotFound is a badge the member didn't earn, or whose event they
// don't see.
var errBadgeNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such badge")

// SetBadgeHidden hides a badge the member earned from their profile card,
// or shows it again (M17.6). One whose event they no longer see, as a
// staff-only event's once they aren't staff, is no such badge, as their
// profile leaves it out.
func (d *Den) SetBadgeHidden(ctx context.Context, s *Session, eventID string, hidden bool) error {
	event, err := denproto.ParseID(eventID)
	if err != nil {
		return errBadgeNotFound
	}
	res, err := d.db.ExecContext(ctx, `UPDATE den_badge_awards SET hidden = ? WHERE event_id = ? AND member_id = ?
		AND event_id IN (SELECT e.id FROM den_events e LEFT JOIN den_channels c ON c.id = e.channel_id
			WHERE e.id = ? AND (`+eventStaffOnly+` = 0 OR ?))`, hidden, event, s.MemberID, event, IsStaff(s.Role))
	if err != nil {
		return err
	}
	// SQLite counts the row an update matched, whether or not it changed.
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return errBadgeNotFound
	}
	return nil
}

// profileBadges lists the badges a member earned whose events a viewer,
// staff or not, sees, latest first, as many as a profile carries: those
// the member hid alone for the member's own, each marked.
func (d *Den) profileBadges(ctx context.Context, member int64, staff, own bool) ([]denproto.MemberBadge, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT a.event_id, a.earned_at, a.hidden, b.title, f.id, f.width, f.height, f.animated, e.details, e.starts_at
		FROM den_badge_awards a JOIN den_badges b ON b.event_id = a.event_id
		JOIN den_named_files n ON n.id = b.named_id JOIN den_files f ON f.id = n.file_id
		JOIN den_events e ON e.id = a.event_id LEFT JOIN den_channels c ON c.id = e.channel_id
		WHERE a.member_id = ? AND (a.hidden = 0 OR ?) AND (`+eventStaffOnly+` = 0 OR ?)
		ORDER BY a.earned_at DESC, a.event_id DESC LIMIT ?`, member, own, staff, denproto.MaxProfileBadges)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []denproto.MemberBadge
	for rows.Next() {
		var b denproto.MemberBadge
		var event, file int64
		var w, h sql.NullInt64
		var title, details []byte
		if err := rows.Scan(&event, &b.EarnedAt, &b.Hidden, &title, &file, &w, &h, &b.Image.Animated, &details, &b.StartsAt); err != nil {
			return nil, err
		}
		t, err := d.v.Open(title, badgeTitleAD(event))
		if err != nil {
			return nil, err
		}
		plain, err := d.v.Open(details, eventAD(event))
		if err != nil {
			return nil, err
		}
		var ed eventDetails
		if err := json.Unmarshal(plain, &ed); err != nil {
			return nil, err
		}
		b.EventID, b.Title, b.Event = denproto.FormatID(event), string(t), ed.Name
		b.Image.ID, b.Image.Width, b.Image.Height = denproto.FormatID(file), int(w.Int64), int(h.Int64)
		out = append(out, b)
	}
	return out, rows.Err()
}
