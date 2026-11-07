package den

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// The den's timer: set for whatever comes due next, it ends polls at their
// time (M15.4), deletes disappearing messages at theirs (M16.4), starts and
// ends events at theirs (M17.4), and gives a live event's badge as a
// member's stretch in its call comes due (M17.5). It's set as the den
// starts, which catches up on what came due while it was stopped, as for a
// den restored from a backup, and again as polls are posted and ended,
// messages that disappear are sent, events are made, moved, canceled and
// ended, badges attached, and calls joined.

// dueTimer runs the passes that end polls, delete disappearing messages,
// start and end events, and give badges.
type dueTimer struct {
	// pass lets one pass run at a time, and Stop wait for one under way.
	pass sync.Mutex

	mu      sync.Mutex
	timer   *time.Timer
	at      int64 // when the timer goes, in the den's milliseconds
	started bool
	closed  bool
}

// StartTimer starts ending polls, deleting disappearing messages, and
// starting and ending events at their time, beginning with those whose
// time passed while the den was stopped. Without it, as in tests, they
// come due only when a pass is called. It starts again after StopTimer, as
// hosting does (M9.1).
func (d *Den) StartTimer() {
	t := &d.timer
	t.mu.Lock()
	t.started, t.closed = true, false
	t.mu.Unlock()
	d.planTimer()
}

// StopTimer stops the timer, and waits for a pass under way.
func (d *Den) StopTimer() {
	t := &d.timer
	t.mu.Lock()
	t.closed = true
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.mu.Unlock()
	t.pass.Lock()
	t.pass.Unlock()
}

// planTimer sets the timer for the next poll to end, message to go, event
// to start or end, or badge to give.
func (d *Den) planTimer() {
	t := &d.timer
	t.mu.Lock()
	running := t.started && !t.closed
	t.mu.Unlock()
	// The badges come due by the call registry, whose lock is taken with the
	// timer's let go: a call that starts takes the timer's after the
	// registry's.
	var award int64
	if running {
		award = d.nextAward(context.Background())
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if !t.started || t.closed {
		return
	}
	var next sql.NullInt64
	if err := d.db.QueryRow(`SELECT min(at) FROM (SELECT min(ends_at) AS at FROM den_polls WHERE ended = 0
		UNION ALL SELECT min(expires_at) FROM den_messages WHERE expires_at IS NOT NULL
		UNION ALL SELECT min(starts_at) FROM den_events WHERE state = 'coming'
		UNION ALL SELECT min(ends_at) FROM den_events WHERE state = 'live')`).Scan(&next); err != nil {
		d.log.Errorf("plan the den's timer: %v", err)
		next = sql.NullInt64{Int64: d.now().Add(time.Minute).UnixMilli(), Valid: true}
	}
	if award != 0 && (!next.Valid || award < next.Int64) {
		next = sql.NullInt64{Int64: award, Valid: true}
	}
	if !next.Valid {
		return
	}
	t.at = next.Int64
	t.timer = time.AfterFunc(max(time.UnixMilli(next.Int64).Sub(d.now()), 0), d.runTimer)
}

// dueAt has the timer go by at, a time something just sent comes due,
// where it isn't set to go sooner: a busy group's every message doesn't
// ask the database when the next is due.
func (d *Den) dueAt(at int64) {
	t := &d.timer
	t.mu.Lock()
	sooner := t.timer == nil || at < t.at
	t.mu.Unlock()
	if sooner {
		d.planTimer()
	}
}

// runTimer ends the polls, deletes the messages, gives the badges, and
// ends and starts the events whose time came, then plans the next pass. A
// badge comes before its event's end, so a stretch that came due by then
// counts.
func (d *Den) runTimer() {
	t := &d.timer
	t.pass.Lock()
	defer t.pass.Unlock()
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	ctx := context.Background()
	if err := d.endDue(ctx); err != nil {
		d.log.Errorf("end polls: %v", err)
	}
	if err := d.deleteDue(ctx); err != nil {
		d.log.Errorf("delete disappearing messages: %v", err)
	}
	if err := d.awardDue(ctx); err != nil {
		d.log.Errorf("give badges: %v", err)
	}
	if err := d.endDueEvents(ctx); err != nil {
		d.log.Errorf("end events: %v", err)
	}
	if err := d.startDue(ctx); err != nil {
		d.log.Errorf("start events: %v", err)
	}
	d.planTimer()
}
