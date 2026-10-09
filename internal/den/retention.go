package den

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Retention (M5): with a period set, the den deletes every message older
// than it, with its files, in channels and DMs alike. It deletes the
// oldest first, in the order they were sent, so after a pass every message
// up to the last one it deleted is gone, in every channel, and one event
// tells clients so rather than a delete for each.
const (
	// expireBatch is how many messages one transaction deletes, so a first
	// pass through years of history never holds the database for long.
	expireBatch = 500
	// expireGap is the least time between passes, so a busy den doesn't
	// delete, and tell every client, as each message passes the period.
	expireGap = time.Hour
)

// expiry runs the passes that delete messages past the den's period.
type expiry struct {
	// pass lets one pass run at a time, and Close wait for one under way.
	pass sync.Mutex

	mu      sync.Mutex
	timer   *time.Timer
	started bool
	closed  bool
	last    time.Time // when the last pass ended
}

// retentionPeriod is how long a den keeps a message, for a period of days.
func retentionPeriod(days int) time.Duration { return time.Duration(days) * 24 * time.Hour }

// StartRetention starts deleting the messages that pass the den's
// retention period, beginning with those that passed it while the den was
// stopped. Without it, as in tests, passes run only when called.
func (d *Den) StartRetention() {
	d.expiry.mu.Lock()
	d.expiry.started = true
	d.expiry.mu.Unlock()
	d.planExpiry(true)
}

// planExpiry plans the next pass: at once when now is set, as when the den
// starts or its period changes; otherwise when the oldest message passes
// the period, and no sooner than expireGap after the last pass.
func (d *Den) planExpiry(now bool) {
	info, created := d.Info()
	e := &d.expiry
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	if !e.started || e.closed || !created || info.Retention == 0 {
		return
	}
	var delay time.Duration
	if !now {
		at, err := d.nextExpiry(info.Retention, e.last)
		if err != nil {
			d.log.Errorf("plan deleting old messages: %v", err)
			at = d.now().Add(expireGap)
		}
		delay = max(at.Sub(d.now()), 0)
	}
	e.timer = time.AfterFunc(delay, d.expire)
}

// nextExpiry is when the oldest message passes a period of days, or, with
// none, the soonest a new one could, but no sooner than expireGap after
// the last pass.
func (d *Den) nextExpiry(days int, last time.Time) (time.Time, error) {
	from := d.now()
	var oldest int64
	err := d.db.QueryRow(`SELECT created_at FROM den_messages ORDER BY id LIMIT 1`).Scan(&oldest)
	switch {
	case err == nil:
		from = time.UnixMilli(oldest)
	case !errors.Is(err, sql.ErrNoRows):
		return time.Time{}, err
	}
	at := from.Add(retentionPeriod(days))
	if soonest := last.Add(expireGap); at.Before(soonest) {
		at = soonest
	}
	return at, nil
}

// stopExpiry stops planning passes, and waits for one under way, which
// ends after its batch.
func (d *Den) stopExpiry() {
	e := &d.expiry
	e.mu.Lock()
	e.closed = true
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	e.mu.Unlock()
	e.pass.Lock()
	e.pass.Unlock()
}

func (e *expiry) stopped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// expire deletes the messages past the den's period, oldest first, a batch
// at a time, and tells clients the last one it deleted. Then it plans the
// next pass.
func (d *Den) expire() {
	e := &d.expiry
	e.pass.Lock()
	defer e.pass.Unlock()
	info, created := d.Info()
	if !created || info.Retention == 0 {
		return
	}
	cutoff := d.now().Add(-retentionPeriod(info.Retention)).UnixMilli()
	ctx := context.Background()
	var through, messages int64
	files := 0
	for !e.stopped() {
		last, n, blobs, err := d.expireBatch(ctx, cutoff)
		if err != nil {
			d.log.Errorf("delete old messages: %v", err)
			break
		}
		if n == 0 {
			break
		}
		d.files.remove(blobs)
		through, messages, files = last, messages+n, files+len(blobs)
	}
	if through > 0 {
		d.log.Infof("Retention deleted %d messages and %d files", messages, files)
		event := denproto.MessagesExpired{Through: denproto.FormatID(through)}
		if err := d.Hub.Publish(denproto.EventMessagesExpired, event, Everyone); err != nil {
			d.log.Errorf("announce deleted messages: %v", err)
		}
	}
	e.mu.Lock()
	e.last = d.now()
	e.mu.Unlock()
	d.planExpiry(false)
}

// expireBatch deletes up to expireBatch of the oldest messages while they
// passed the cutoff, and returns the last one it deleted, how many it
// deleted, and the blobs of their files to remove once that commits.
func (d *Den) expireBatch(ctx context.Context, cutoff int64) (through, n int64, blobs [][]byte, err error) {
	err = d.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, created_at FROM den_messages ORDER BY id LIMIT ?`, expireBatch)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, at int64
			if err := rows.Scan(&id, &at); err != nil {
				rows.Close()
				return err
			}
			// Oldest first, in the order they were sent: a message that
			// hasn't passed the period keeps the ones after it, so a clock
			// that once ran back can only make the den delete late.
			if at >= cutoff {
				break
			}
			through, n = id, n+1
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		if _, blobs, err = filesOf(ctx, tx, `m.id <= ?`, through); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM den_messages WHERE id <= ?`, through)
		return err
	})
	if err != nil {
		return 0, 0, nil, err
	}
	return through, n, blobs, nil
}

// RetentionPreview says what a period of days would delete now: the
// messages a pass would take, and the space their files take. Only the
// owner asks, from the den's settings.
func (d *Den) RetentionPreview(ctx context.Context, s *Session, days int) (denproto.RetentionPreview, error) {
	var p denproto.RetentionPreview
	if s.Role != denproto.RoleOwner {
		return p, denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, "only the owner changes the den")
	}
	if days < 1 || days > denproto.MaxRetention {
		return p, invalid("days: 1 to %d", denproto.MaxRetention)
	}
	cutoff := d.now().Add(-retentionPeriod(days)).UnixMilli()
	// A pass stops at the first message that hasn't passed the period.
	var bound sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT id FROM den_messages WHERE created_at >= ? ORDER BY id LIMIT 1`, cutoff).Scan(&bound)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	end := int64(1<<63 - 1)
	if bound.Valid {
		end = bound.Int64
	}
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM den_messages WHERE id < ?`, end).Scan(&p.Messages); err != nil {
		return p, err
	}
	err = d.db.QueryRowContext(ctx, `SELECT coalesce(sum(size + coalesce(thumb_size, 0)), 0) FROM den_files WHERE message_id < ?`, end).
		Scan(&p.Bytes)
	return p, err
}
