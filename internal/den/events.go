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

// Events (M17.4): the den's events, which staff make, and members too as
// far as the owner's limit allows. An event's name, description and
// place's line of text are what its maker wrote, so the den seals them
// with the data key, as a message's text; its banner is an upload of the
// maker's, checked as a profile's banner is; and its place is a voice
// channel, whose visibility it takes, or a line of text, which everyone
// sees.
//
// The den's timer starts an event at its start, telling the members
// interested in it with event.started, an audience fixed as it's made,
// since the ring replays it, and the rest with event.updated; and ends it
// at its end, forgetting who was interested. An ended event stays, out of
// the list, for its link's card and its badge (M17.5).
//
// d.calendar keeps each event's changes, and their events, in order, the
// timer's among them.

const (
	eventComing = "coming"
	eventLive   = "live"
	eventEnded  = "ended"
)

var errEventNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such event")

func eventAD(id int64) []byte { return []byte("den_events.details:" + strconv.FormatInt(id, 10)) }

// eventDetails are what an event keeps sealed: what its maker, or staff,
// wrote of it.
type eventDetails struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Place       string `json:"place,omitempty"`
}

// eventRow is an event as the den keeps it, with its banner's size, its
// channel's visibility, its maker's role, and how many of those who see it
// are interested.
type eventRow struct {
	id         int64
	details    eventDetails
	banner     denproto.Image
	bannerID   int64
	channel    int64
	staffOnly  bool
	madeBy     int64
	makerRole  string
	startsAt   int64
	endsAt     int64
	state      string
	interested int
}

// view is the event as members see it; mine, where it's given, is the
// member's own mark.
func (e *eventRow) view(mine *bool) denproto.DenEvent {
	v := denproto.DenEvent{ID: denproto.FormatID(e.id), Name: e.details.Name, Description: e.details.Description, Banner: e.banner,
		StartsAt: e.startsAt, EndsAt: e.endsAt, Place: e.details.Place, MadeBy: denproto.FormatID(e.madeBy), Live: e.state == eventLive,
		Interested: e.interested, Mine: mine}
	if e.channel != 0 {
		v.ChannelID = denproto.FormatID(e.channel)
	}
	return v
}

// sees reports whether a member sees the event: one in a staff-only
// channel, staff alone.
func (e *eventRow) sees(staff bool) bool { return !e.staffOnly || staff }

// audience is who sees the event.
func (e *eventRow) audience() Audience {
	if e.staffOnly {
		return Staff
	}
	return Everyone
}

// eventColumns read an event, its banner's size, its channel's visibility,
// its maker's role, and how many are interested of those who see it: in a
// staff-only channel, staff alone.
const eventColumns = `e.id, e.details, e.banner_id, f.width, f.height, f.animated, e.channel_id, coalesce(c.staff_only, 0),
	e.made_by, mk.role, e.starts_at, e.ends_at, e.state,
	(SELECT count(*) FROM den_event_interest i JOIN den_members m ON m.id = i.member_id
		WHERE i.event_id = e.id AND (coalesce(c.staff_only, 0) = 0 OR m.role IN ('moderator', 'owner')))
	FROM den_events e LEFT JOIN den_files f ON f.id = e.banner_id LEFT JOIN den_channels c ON c.id = e.channel_id
	JOIN den_members mk ON mk.id = e.made_by`

func (d *Den) scanEvent(scan func(...any) error) (*eventRow, error) {
	e := &eventRow{}
	var sealed []byte
	var banner, w, h, channel sql.NullInt64
	var animated sql.NullBool
	if err := scan(&e.id, &sealed, &banner, &w, &h, &animated, &channel, &e.staffOnly, &e.madeBy, &e.makerRole,
		&e.startsAt, &e.endsAt, &e.state, &e.interested); err != nil {
		return nil, err
	}
	plain, err := d.v.Open(sealed, eventAD(e.id))
	if err == nil {
		err = json.Unmarshal(plain, &e.details)
	}
	if err != nil {
		return nil, err
	}
	if banner.Valid {
		e.bannerID = banner.Int64
		e.banner = denproto.Image{ID: denproto.FormatID(banner.Int64), Width: int(w.Int64), Height: int(h.Int64), Animated: animated.Bool}
	}
	e.channel = channel.Int64
	return e, nil
}

// queryEvents reads the events a condition on den_events (aliased e)
// selects, in the order the list shows them: live first, then by start.
func (d *Den) queryEvents(ctx context.Context, q querier, where string, args ...any) ([]*eventRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+eventColumns+` WHERE `+where+`
		ORDER BY e.state = 'live' DESC, e.starts_at, e.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*eventRow
	for rows.Next() {
		e, err := d.scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// event reads one event.
func (d *Den) event(ctx context.Context, q querier, id int64) (*eventRow, error) {
	events, err := d.queryEvents(ctx, q, `e.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, errEventNotFound
	}
	return events[0], nil
}

// visibleEvent reads an event to come or live that the session sees.
func (d *Den) visibleEvent(ctx context.Context, q querier, s *Session, id string) (*eventRow, error) {
	eid, err := denproto.ParseID(id)
	if err != nil {
		return nil, errEventNotFound
	}
	e, err := d.event(ctx, q, eid)
	if err != nil {
		return nil, err
	}
	if e.state == eventEnded || !e.sees(IsStaff(s.Role)) {
		return nil, errEventNotFound
	}
	return e, nil
}

// eventsFor lists the events to come and live a member sees, each with
// their own mark, for their snapshot.
func (d *Den) eventsFor(ctx context.Context, q querier, member int64, staff bool) ([]denproto.DenEvent, error) {
	events, err := d.queryEvents(ctx, q, `e.state != 'ended' AND (coalesce(c.staff_only, 0) = 0 OR ?)`, staff)
	if err != nil {
		return nil, err
	}
	mine, err := memberInterest(ctx, q, member)
	if err != nil {
		return nil, err
	}
	out := make([]denproto.DenEvent, 0, len(events))
	for _, e := range events {
		marked := mine[e.id]
		out = append(out, e.view(&marked))
	}
	return out, nil
}

// memberInterest is the events a member marked Interested.
func memberInterest(ctx context.Context, q querier, member int64) (map[int64]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT event_id FROM den_event_interest WHERE member_id = ?`, member)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// interested is the members interested in an event.
func interested(ctx context.Context, q querier, event int64) (map[int64]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT member_id FROM den_event_interest WHERE event_id = ?`, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// minEventLength is the shortest an event lasts: with ShortTimes, as a
// development instance's den takes for the browser checks, a second.
func (d *Den) minEventLength() time.Duration {
	if d.ShortTimes {
		return time.Second
	}
	return denproto.MinEventLength
}

// checkEventTimes checks an event's times as a change leaves them, as of
// now. A start that changes is up to EventStartSlack past, which counts as
// now, and at most MaxEventAhead ahead; a live event's start doesn't
// change, and its end is never before now. Either way it lasts within the
// limits.
func (d *Den) checkEventTimes(starts, ends int64, startChanged, live bool, now time.Time) error {
	if startChanged {
		if live {
			return invalid("starts_at: a live event's start stays where it was")
		}
		if starts < now.Add(-denproto.EventStartSlack).UnixMilli() {
			return invalid("starts_at: an event starts now or later")
		}
		if starts > now.Add(denproto.MaxEventAhead).UnixMilli() {
			return invalid("starts_at: an event starts within a year")
		}
	}
	if live && ends < now.UnixMilli() {
		return invalid("ends_at: a live event's end is now or later; End now ends it")
	}
	length := time.Duration(ends-starts) * time.Millisecond
	if length < d.minEventLength() || length > denproto.MaxEventLength {
		return invalid("ends_at: an event lasts 5 minutes to a week")
	}
	return nil
}

// cleanEventFields checks and cleans what a request gives an event's name,
// description and place's line of text, in place.
func cleanEventFields(req *denproto.EventRequest) error {
	if req.Name != nil {
		name, err := denproto.CleanEventName(*req.Name)
		if err != nil {
			return invalid("name: %v", err)
		}
		req.Name = &name
	}
	if req.Description != nil {
		description := strings.TrimSpace(denproto.CleanLinks(*req.Description))
		if err := denproto.CheckEventDescription(description); err != nil {
			return invalid("description: %v", err)
		}
		req.Description = &description
	}
	if req.ChannelID != nil && req.Place != nil {
		return invalid("an event's place is a voice channel or a line of text")
	}
	if req.Place != nil {
		place, err := denproto.CleanEventPlace(*req.Place)
		if err != nil {
			return invalid("place: %v", err)
		}
		req.Place = &place
	}
	return nil
}

// eventChannel checks a voice channel an event goes in: one the member
// sees.
func (d *Den) eventChannel(ctx context.Context, s *Session, id string) (denproto.Channel, int64, error) {
	cid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Channel{}, 0, invalid("channel_id: no such voice channel")
	}
	c, err := d.channel(ctx, d.db, cid)
	if errors.Is(err, errChannelNotFound) || err == nil && (c.Kind != denproto.KindVoice || !sees(c, s)) {
		return c, 0, invalid("channel_id: no such voice channel")
	}
	return c, cid, err
}

// checkEventBanner checks an upload an event's banner takes: the member's
// own, waiting to be used, as a profile's banner is checked. It returns
// the upload's ID.
func checkEventBanner(ctx context.Context, tx *sql.Tx, member int64, id string, now time.Time) (int64, error) {
	fid, err := denproto.ParseID(id)
	if err != nil {
		return 0, invalid("banner: no such upload")
	}
	var typ string
	var w, h sql.NullInt64
	var size int64
	var animated bool
	err = tx.QueryRowContext(ctx, `SELECT type, width, height, size, animated FROM den_files
		WHERE id = ? AND uploader_id = ? AND created_at > ? AND sealed = 0 AND replaces IS NULL AND `+unused,
		fid, member, now.Add(-uploadExpiry).UnixMilli()).Scan(&typ, &w, &h, &size, &animated)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, invalid("banner: no such upload waiting to be used")
	}
	if err != nil {
		return 0, err
	}
	if !w.Valid || !strings.HasPrefix(typ, "image/") || animated && typ != "image/gif" {
		return 0, invalid("banner: a banner is a still image or an animated GIF")
	}
	if err := bannerShape(w.Int64, h.Int64, size, animated); err != nil {
		return 0, err
	}
	return fid, nil
}

// mayChange checks that a member may change or cancel an event: its maker,
// or staff who outrank them, by the rule calls and messages use.
func mayChange(e *eventRow, s *Session) error {
	if e.madeBy == s.MemberID {
		return nil
	}
	if !IsStaff(s.Role) || denproto.Rank(e.makerRole) >= denproto.Rank(s.Role) {
		return forbidden("only its maker, or staff above them, change an event")
	}
	return nil
}

// CreateEvent makes an event, within the owner's limit for members who
// aren't staff and the den's for all, and tells everyone who sees it. The
// answer carries the maker's mark, which making it doesn't set.
func (d *Den) CreateEvent(ctx context.Context, s *Session, req denproto.EventRequest) (denproto.DenEvent, error) {
	none := denproto.DenEvent{}
	info, _ := d.Info()
	staff := IsStaff(s.Role)
	limits := denproto.DefaultEventLimits
	if info.EventLimits != nil {
		limits = *info.EventLimits
	}
	if !staff && limits.MemberEvents == 0 {
		return none, forbidden("only staff make events in this den")
	}
	if err := cleanEventFields(&req); err != nil {
		return none, err
	}
	switch {
	case req.Name == nil:
		return none, invalid("an event needs a name")
	case req.StartsAt == nil || req.EndsAt == nil:
		return none, invalid("an event needs a start and an end")
	case req.ChannelID == nil && req.Place == nil:
		return none, invalid("an event needs a place: a voice channel or a line of text")
	}
	now := d.now()
	if err := d.checkEventTimes(*req.StartsAt, *req.EndsAt, true, false, now); err != nil {
		return none, err
	}
	details := eventDetails{Name: *req.Name}
	if req.Description != nil {
		details.Description = *req.Description
	}
	var channel sql.NullInt64
	if req.ChannelID != nil {
		_, cid, err := d.eventChannel(ctx, s, *req.ChannelID)
		if err != nil {
			return none, err
		}
		channel = sql.NullInt64{Int64: cid, Valid: true}
	} else {
		details.Place = *req.Place
	}
	d.calendar.Lock()
	defer d.calendar.Unlock()
	var e *eventRow
	err := d.tx(ctx, func(tx *sql.Tx) error {
		var all, own int
		if err := tx.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE made_by = ?) FROM den_events WHERE state != 'ended'`,
			s.MemberID).Scan(&all, &own); err != nil {
			return err
		}
		if all >= denproto.MaxEvents {
			return forbidden("a den holds %d events that haven't ended", denproto.MaxEvents)
		}
		if !staff && own >= limits.MemberEvents {
			return forbidden("you have as many events as this den lets each member have")
		}
		var banner sql.NullInt64
		if req.Banner != nil && *req.Banner != "" {
			fid, err := checkEventBanner(ctx, tx, s.MemberID, *req.Banner, now)
			if err != nil {
				return err
			}
			banner = sql.NullInt64{Int64: fid, Valid: true}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_events (details, banner_id, channel_id, made_by, made_at, starts_at, ends_at)
			VALUES (x'', ?, ?, ?, ?, ?, ?)`, banner, channel, s.MemberID, now.UnixMilli(), *req.StartsAt, *req.EndsAt)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := d.sealDetails(ctx, tx, id, details); err != nil {
			return err
		}
		e, err = d.event(ctx, tx, id)
		return err
	})
	if err != nil {
		return none, err
	}
	d.log.Infof("Member %d made event %d", s.MemberID, e.id)
	if err := d.Hub.Publish(denproto.EventEventCreated, e.view(nil), e.audience()); err != nil {
		return none, err
	}
	d.dueAt(e.startsAt)
	mine := false
	return e.view(&mine), nil
}

// sealDetails keeps an event's details within tx, sealed with the data key.
func (d *Den) sealDetails(ctx context.Context, tx *sql.Tx, id int64, details eventDetails) error {
	plain, err := json.Marshal(details)
	if err != nil {
		return err
	}
	sealed, err := d.v.Seal(plain, eventAD(id))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE den_events SET details = ? WHERE id = ?`, sealed, id)
	return err
}

// UpdateEvent changes an event to come or live, at its maker's word or
// staff's above them: a live one keeps its start, and its end moves no
// earlier than now. A banner it replaces goes. An event that moves into or
// out of a staff-only channel starts members who aren't staff over from a
// snapshot, as a channel's visibility does.
func (d *Den) UpdateEvent(ctx context.Context, s *Session, id string, req denproto.EventRequest) (denproto.DenEvent, error) {
	none := denproto.DenEvent{}
	if err := cleanEventFields(&req); err != nil {
		return none, err
	}
	var channel sql.NullInt64
	if req.ChannelID != nil {
		_, cid, err := d.eventChannel(ctx, s, *req.ChannelID)
		if err != nil {
			return none, err
		}
		channel = sql.NullInt64{Int64: cid, Valid: true}
	}
	d.calendar.Lock()
	defer d.calendar.Unlock()
	now := d.now()
	var before, after *eventRow
	var blobs [][]byte
	err := d.tx(ctx, func(tx *sql.Tx) error {
		e, err := d.visibleEvent(ctx, tx, s, id)
		if err != nil {
			return err
		}
		if err := mayChange(e, s); err != nil {
			return err
		}
		before = e
		starts, ends := e.startsAt, e.endsAt
		if req.StartsAt != nil {
			starts = *req.StartsAt
		}
		if req.EndsAt != nil {
			ends = *req.EndsAt
		}
		if starts != e.startsAt || ends != e.endsAt {
			if err := d.checkEventTimes(starts, ends, starts != e.startsAt, e.state == eventLive, now); err != nil {
				return err
			}
		}
		details := e.details
		if req.Name != nil {
			details.Name = *req.Name
		}
		if req.Description != nil {
			details.Description = *req.Description
		}
		place := sql.NullInt64{Int64: e.channel, Valid: e.channel != 0}
		switch {
		case req.ChannelID != nil:
			place, details.Place = channel, ""
		case req.Place != nil:
			place, details.Place = sql.NullInt64{}, *req.Place
		}
		banner := sql.NullInt64{Int64: e.bannerID, Valid: e.bannerID != 0}
		if req.Banner != nil {
			banner = sql.NullInt64{}
			if *req.Banner != "" {
				fid, err := checkEventBanner(ctx, tx, s.MemberID, *req.Banner, now)
				if err != nil {
					return err
				}
				banner = sql.NullInt64{Int64: fid, Valid: true}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_events SET banner_id = ?, channel_id = ?, starts_at = ?, ends_at = ? WHERE id = ?`,
			banner, place, starts, ends, e.id); err != nil {
			return err
		}
		if err := d.sealDetails(ctx, tx, e.id, details); err != nil {
			return err
		}
		if e.bannerID != 0 && e.bannerID != banner.Int64 {
			if blobs, err = deleteFiles(ctx, tx, `id = ?`, e.bannerID); err != nil {
				return err
			}
		}
		after, err = d.event(ctx, tx, e.id)
		return err
	})
	if err != nil {
		return none, err
	}
	d.files.remove(blobs)
	if before.staffOnly != after.staffOnly {
		if err := d.Hub.Publish(denproto.EventEventUpdated, after.view(nil), Staff); err != nil {
			return none, err
		}
		d.Hub.RefreshNonStaff()
	} else if err := d.Hub.Publish(denproto.EventEventUpdated, after.view(nil), after.audience()); err != nil {
		return none, err
	}
	if after.startsAt != before.startsAt || after.endsAt != before.endsAt {
		d.planTimer()
	}
	mine, err := markedBy(ctx, d.db, after.id, s.MemberID)
	if err != nil {
		return none, err
	}
	return after.view(&mine), nil
}

// markedBy reports whether a member marked an event Interested.
func markedBy(ctx context.Context, q querier, event, member int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM den_event_interest WHERE event_id = ? AND member_id = ?`, event, member).Scan(&n)
	return n > 0, err
}

// CancelEvent deletes an event to come, with its banner, at its maker's
// word or staff's above them, and tells everyone who saw it. One that
// started can only be ended.
func (d *Den) CancelEvent(ctx context.Context, s *Session, id string) error {
	d.calendar.Lock()
	defer d.calendar.Unlock()
	var gone *eventRow
	var blobs [][]byte
	err := d.tx(ctx, func(tx *sql.Tx) error {
		e, err := d.visibleEvent(ctx, tx, s, id)
		if err != nil {
			return err
		}
		if err := mayChange(e, s); err != nil {
			return err
		}
		if e.state != eventComing {
			return forbidden("an event that started can only be ended")
		}
		gone = e
		blobs, err = deleteEvents(ctx, tx, `id = ?`, e.id)
		return err
	})
	if err != nil {
		return err
	}
	d.files.remove(blobs)
	d.log.Infof("Member %d canceled event %d", s.MemberID, gone.id)
	if err := d.Hub.Publish(denproto.EventEventCanceled, denproto.EventID{ID: denproto.FormatID(gone.id)}, gone.audience()); err != nil {
		return err
	}
	d.planTimer()
	return nil
}

// deleteEvents deletes the events a condition on den_events selects, with
// their banners, within tx, and returns the banners' blobs to remove once
// it commits.
func deleteEvents(ctx context.Context, tx *sql.Tx, where string, args ...any) ([][]byte, error) {
	rows, err := tx.QueryContext(ctx, `DELETE FROM den_events WHERE `+where+` RETURNING banner_id`, args...)
	if err != nil {
		return nil, err
	}
	var banners []int64
	for rows.Next() {
		var banner sql.NullInt64
		if err := rows.Scan(&banner); err != nil {
			rows.Close()
			return nil, err
		}
		if banner.Valid {
			banners = append(banners, banner.Int64)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var blobs [][]byte
	for _, id := range banners {
		b, err := deleteFiles(ctx, tx, `id = ?`, id)
		if err != nil {
			return nil, err
		}
		blobs = append(blobs, b...)
	}
	return blobs, nil
}

// EndEvent ends a live event now, at its maker's word or staff's above
// them, forgets who was interested, and tells everyone who saw it. One
// that ended already stays as it is.
func (d *Den) EndEvent(ctx context.Context, s *Session, id string) error {
	d.calendar.Lock()
	defer d.calendar.Unlock()
	eid, err := denproto.ParseID(id)
	if err != nil {
		return errEventNotFound
	}
	var ended *eventRow
	err = d.tx(ctx, func(tx *sql.Tx) error {
		e, err := d.event(ctx, tx, eid)
		if err != nil {
			return err
		}
		if !e.sees(IsStaff(s.Role)) {
			return errEventNotFound
		}
		if err := mayChange(e, s); err != nil {
			return err
		}
		switch e.state {
		case eventEnded:
			return nil
		case eventComing:
			return forbidden("an event that hasn't started is canceled, not ended")
		}
		ended = e
		return endEvent(ctx, tx, e, d.now())
	})
	if err != nil || ended == nil {
		return err
	}
	d.log.Infof("Member %d ended event %d", s.MemberID, ended.id)
	if err := d.Hub.Publish(denproto.EventEventEnded, denproto.EventID{ID: denproto.FormatID(ended.id)}, ended.audience()); err != nil {
		return err
	}
	d.planTimer()
	return nil
}

// endEvent ends an event within tx as of now, which is its end if that
// comes sooner, and forgets who was interested in it.
func endEvent(ctx context.Context, tx *sql.Tx, e *eventRow, now time.Time) error {
	ends := min(e.endsAt, max(now.UnixMilli(), e.startsAt))
	if _, err := tx.ExecContext(ctx, `UPDATE den_events SET state = 'ended', ends_at = ? WHERE id = ?`, ends, e.id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM den_event_interest WHERE event_id = ?`, e.id)
	return err
}

// SetInterest marks the member Interested in an event to come or live, or
// takes it back, and tells everyone who sees it how many are, and the
// member's own sessions their mark.
func (d *Den) SetInterest(ctx context.Context, s *Session, id string, on bool) (denproto.EventInterest, error) {
	d.calendar.Lock()
	defer d.calendar.Unlock()
	var e *eventRow
	changed := false
	err := d.tx(ctx, func(tx *sql.Tx) error {
		found, err := d.visibleEvent(ctx, tx, s, id)
		if err != nil {
			return err
		}
		var res sql.Result
		if on {
			res, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO den_event_interest (event_id, member_id) VALUES (?, ?)`, found.id, s.MemberID)
		} else {
			res, err = tx.ExecContext(ctx, `DELETE FROM den_event_interest WHERE event_id = ? AND member_id = ?`, found.id, s.MemberID)
		}
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		changed = n > 0
		e, err = d.event(ctx, tx, found.id)
		return err
	})
	if err != nil {
		return denproto.EventInterest{}, err
	}
	answer := denproto.EventInterest{EventID: denproto.FormatID(e.id), Interested: e.interested, Mine: &on}
	if !changed {
		return answer, nil
	}
	if err := d.Hub.Publish(denproto.EventEventInterest, denproto.EventInterest{EventID: answer.EventID, Interested: e.interested},
		e.audience()); err != nil {
		return answer, err
	}
	return answer, d.Hub.Publish(denproto.EventEventInterest, answer, OnlyMember(s.MemberID))
}

// startDue starts every event to come whose start came, telling each
// one's interested members with event.started and the rest who see it
// with event.updated. One whose end came as well, as when the den was
// stopped through all of it, ends without starting.
func (d *Den) startDue(ctx context.Context) error {
	d.calendar.Lock()
	defer d.calendar.Unlock()
	now := d.now()
	type started struct {
		e          *eventRow
		interested map[int64]bool
	}
	var live []started
	var over []*eventRow
	err := d.tx(ctx, func(tx *sql.Tx) error {
		due, err := d.queryEvents(ctx, tx, `e.state = 'coming' AND e.starts_at <= ?`, now.UnixMilli())
		if err != nil {
			return err
		}
		for _, e := range due {
			if e.endsAt <= now.UnixMilli() {
				if err := endEvent(ctx, tx, e, now); err != nil {
					return err
				}
				over = append(over, e)
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE den_events SET state = 'live' WHERE id = ?`, e.id); err != nil {
				return err
			}
			set, err := interested(ctx, tx, e.id)
			if err != nil {
				return err
			}
			e.state = eventLive
			live = append(live, started{e, set})
		}
		return nil
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, st := range live {
		d.log.Infof("Event %d started, with %d interested", st.e.id, st.e.interested)
		v := st.e.view(nil)
		errs = append(errs, d.Hub.Publish(denproto.EventEventStarted, v, Voters(st.interested, st.e.staffOnly)),
			d.Hub.Publish(denproto.EventEventUpdated, v, NonVoters(st.interested, st.e.staffOnly)))
	}
	for _, e := range over {
		d.log.Infof("Event %d passed while the den was stopped", e.id)
		errs = append(errs, d.Hub.Publish(denproto.EventEventEnded, denproto.EventID{ID: denproto.FormatID(e.id)}, e.audience()))
	}
	return errors.Join(errs...)
}

// endDueEvents ends every live event whose end came, forgetting who was
// interested, and tells everyone who saw it.
func (d *Den) endDueEvents(ctx context.Context) error {
	d.calendar.Lock()
	defer d.calendar.Unlock()
	now := d.now()
	var over []*eventRow
	err := d.tx(ctx, func(tx *sql.Tx) error {
		due, err := d.queryEvents(ctx, tx, `e.state = 'live' AND e.ends_at <= ?`, now.UnixMilli())
		if err != nil {
			return err
		}
		for _, e := range due {
			if err := endEvent(ctx, tx, e, now); err != nil {
				return err
			}
		}
		over = due
		return nil
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range over {
		d.log.Infof("Event %d ended", e.id)
		errs = append(errs, d.Hub.Publish(denproto.EventEventEnded, denproto.EventID{ID: denproto.FormatID(e.id)}, e.audience()))
	}
	return errors.Join(errs...)
}

// eventsGone are events a departure or a channel's deletion canceled or
// ended, to tell everyone who saw them.
type eventsGone struct {
	canceled, ended []*eventRow
	// recounted are events whose count of interested members changed.
	recounted []int64
}

// departEvents takes a departing member's events out of the den within
// tx: their events to come go, with their banners, and their marks on the
// rest. Their live events go on to their end.
func (d *Den) departEvents(ctx context.Context, tx *sql.Tx, member int64) (eventsGone, [][]byte, error) {
	var gone eventsGone
	coming, err := d.queryEvents(ctx, tx, `e.state = 'coming' AND e.made_by = ?`, member)
	if err != nil {
		return gone, nil, err
	}
	blobs, err := deleteEvents(ctx, tx, `state = 'coming' AND made_by = ?`, member)
	if err != nil {
		return gone, nil, err
	}
	gone.canceled = coming
	rows, err := tx.QueryContext(ctx, `DELETE FROM den_event_interest WHERE member_id = ? RETURNING event_id`, member)
	if err != nil {
		return gone, nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return gone, nil, err
		}
		gone.recounted = append(gone.recounted, id)
	}
	rows.Close()
	return gone, blobs, rows.Err()
}

// channelEvents takes a voice channel's events out of the den within tx,
// as it's deleted: those to come go, with their banners, and the live one
// ends.
func (d *Den) channelEvents(ctx context.Context, tx *sql.Tx, channel int64) (eventsGone, [][]byte, error) {
	var gone eventsGone
	events, err := d.queryEvents(ctx, tx, `e.state != 'ended' AND e.channel_id = ?`, channel)
	if err != nil {
		return gone, nil, err
	}
	now := d.now()
	for _, e := range events {
		if e.state == eventLive {
			if err := endEvent(ctx, tx, e, now); err != nil {
				return gone, nil, err
			}
			gone.ended = append(gone.ended, e)
		} else {
			gone.canceled = append(gone.canceled, e)
		}
	}
	blobs, err := deleteEvents(ctx, tx, `state = 'coming' AND channel_id = ?`, channel)
	return gone, blobs, err
}

// publishGone tells everyone who saw them of events a departure or a
// channel's deletion took out, and of the counts it changed.
func (d *Den) publishGone(ctx context.Context, gone eventsGone) error {
	var errs []error
	for _, e := range gone.canceled {
		errs = append(errs, d.Hub.Publish(denproto.EventEventCanceled, denproto.EventID{ID: denproto.FormatID(e.id)}, e.audience()))
	}
	for _, e := range gone.ended {
		errs = append(errs, d.Hub.Publish(denproto.EventEventEnded, denproto.EventID{ID: denproto.FormatID(e.id)}, e.audience()))
	}
	slices.Sort(gone.recounted)
	for _, id := range slices.Compact(gone.recounted) {
		e, err := d.event(ctx, d.db, id)
		if errors.Is(err, errEventNotFound) {
			continue
		}
		if err == nil && e.state != eventEnded {
			err = d.Hub.Publish(denproto.EventEventInterest, denproto.EventInterest{EventID: denproto.FormatID(e.id), Interested: e.interested},
				e.audience())
		}
		errs = append(errs, err)
	}
	if len(gone.canceled)+len(gone.ended) > 0 {
		d.planTimer()
	}
	return errors.Join(errs...)
}

// publishChannelEvents tells staff of a voice channel's events as they now
// are, once it turned staff-only or back: how many are interested counts
// only those who see it. Members who aren't staff start over from a
// snapshot, which holds them as they see them.
func (d *Den) publishChannelEvents(ctx context.Context, channel int64) error {
	events, err := d.queryEvents(ctx, d.db, `e.state != 'ended' AND e.channel_id = ?`, channel)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range events {
		errs = append(errs, d.Hub.Publish(denproto.EventEventUpdated, e.view(nil), Staff))
	}
	return errors.Join(errs...)
}
