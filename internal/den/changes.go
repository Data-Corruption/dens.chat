package den

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// What changed while away (M6): the den logs each change to a message, by
// ID only, so a client that cached what it sends can ask, after a ready,
// what changed since it last synced, rather than fetch it all again. A
// message's text never goes in the log, so deleting one, or a retention
// pass, leaves nothing to find there.

// Kinds of change.
const (
	changeMessage = "message"
	changeMember  = "member"
)

// changeLogAge is how long the den keeps a change.
const changeLogAge = denproto.ChangeLogDays * 24 * time.Hour

// pruneEvery is how often the den forgets changes older than
// changeLogAge.
const pruneEvery = time.Hour

// logChange records a change to a message or a member within tx, at, as
// the den's next version: the thing's earlier change, if it has one, gives
// way to it. channel is a message's.
func logChange(ctx context.Context, tx *sql.Tx, kind string, ref int64, channel *int64, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO den_changes (kind, ref, channel_id, at) VALUES (?, ?, ?, ?)`,
		kind, ref, channel, at.UnixMilli())
	return err
}

// logMessage records a change to a message in a channel.
func logMessage(ctx context.Context, tx *sql.Tx, message, channel int64, at time.Time) error {
	return logChange(ctx, tx, changeMessage, message, &channel, at)
}

// logMember records a change to a member's record (M6.2): what ready
// sends of them, so a client that kept the list gets them again.
func logMember(ctx context.Context, tx *sql.Tx, member int64, at time.Time) error {
	return logChange(ctx, tx, changeMember, member, nil, at)
}

// changedMembers lists the members whose records changed after a version.
func changedMembers(ctx context.Context, q querier, since int64) (map[int64]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT ref FROM den_changes WHERE kind = ? AND id > ?`, changeMember, since)
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

// version is the newest change's number, or 0 before the first.
func version(ctx context.Context, q querier) (int64, error) {
	var v int64
	err := q.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = 'den_changes'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// syncState is where the den's changes stand, as ready carries it.
func syncState(ctx context.Context, q querier) (denproto.Sync, error) {
	var s denproto.Sync
	var expired int64
	err := q.QueryRowContext(ctx, `SELECT sync_epoch, changes_floor, expired_through FROM den WHERE id = 1`).
		Scan(&s.Epoch, &s.Floor, &expired)
	if err != nil {
		return s, err
	}
	if expired > 0 {
		s.Expired = denproto.FormatID(expired)
	}
	if s.Version, err = version(ctx, q); err != nil {
		return s, err
	}
	// A floor can't pass the version: pruning sets it to one it read.
	s.Version = max(s.Version, s.Floor)
	return s, nil
}

// errChangesGone refuses changes since a version the den no longer has
// them from.
var errChangesGone = denproto.Errorf(http.StatusGone, denproto.CodeChangesGone,
	"the den no longer has the changes since that version")

// Changes returns a page of what changed since a version: the messages
// edited since, as they are now, and those deleted, in channels the
// member can see now, with IDs up to upto when it's set. The floor and
// the log are read together, so a pass of pruning can't take changes from
// under a version that was still within it.
func (d *Den) Changes(ctx context.Context, s *Session, since, upto int64) (denproto.Changes, error) {
	out := denproto.Changes{Messages: []denproto.Message{}, Deleted: []denproto.MessageDeleted{}}
	if since < 0 || upto < 0 {
		return out, invalid("since and upto are versions and IDs, at least 0")
	}
	staff := IsStaff(s.Role)
	type change struct {
		id, ref, channel int64
	}
	var changes []change
	err := d.readTx(ctx, func(tx *sql.Tx) error {
		state, err := syncState(ctx, tx)
		if err != nil {
			return err
		}
		if since < state.Floor || since > state.Version {
			return errChangesGone
		}
		out.Version = state.Version
		rows, err := tx.QueryContext(ctx, `SELECT id, ref, coalesce(channel_id, 0) FROM den_changes
			WHERE id > ? AND kind = ? ORDER BY id LIMIT ?`, since, changeMessage, denproto.ChangesPage+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c change
			if err := rows.Scan(&c.id, &c.ref, &c.channel); err != nil {
				return err
			}
			changes = append(changes, c)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(changes) > denproto.ChangesPage {
		changes = changes[:denproto.ChangesPage]
		out.Version, out.More = changes[len(changes)-1].id, true
	}

	// Only changes in channels the member can see now count; a channel
	// that's gone is missing from their ready, which drops it whole.
	seen := map[int64]bool{}
	var wanted []int64
	kept := changes[:0]
	for _, c := range changes {
		see, ok := seen[c.channel]
		if !ok {
			ch, err := d.channel(ctx, d.db, c.channel)
			switch {
			case err == nil:
				see = visible(ch, s.MemberID, staff)
			case !errors.Is(err, errChannelNotFound):
				return out, err
			}
			seen[c.channel] = see
		}
		if see && (upto == 0 || c.ref <= upto) {
			kept = append(kept, c)
			wanted = append(wanted, c.ref)
		}
	}
	messages, err := d.messagesByID(ctx, wanted)
	if err != nil {
		return out, err
	}
	used := 0
	for i, c := range kept {
		m, ok := messages[c.ref]
		size := 64
		if ok {
			b, err := json.Marshal(m)
			if err != nil {
				return out, err
			}
			size = len(b) + 1
		}
		// The page stops short of the response's limit, at a change, and
		// the client asks again from the one before.
		if used += size; used > historyBudget && i > 0 {
			out.Version, out.More = kept[i-1].id, true
			break
		}
		if ok {
			out.Messages = append(out.Messages, m)
		} else {
			out.Deleted = append(out.Deleted, denproto.MessageDeleted{ID: denproto.FormatID(c.ref), ChannelID: denproto.FormatID(c.channel)})
		}
	}
	return out, nil
}

// messagesByID returns the messages with the given IDs that still exist,
// as history has them, by ID.
func (d *Den) messagesByID(ctx context.Context, ids []int64) (map[int64]denproto.Message, error) {
	out := map[int64]denproto.Message{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	ms, err := d.queryMessages(ctx, `SELECT `+messageColumns+` FROM den_messages WHERE id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		id, _ := denproto.ParseID(m.ID)
		out[id] = m
	}
	return out, nil
}

// readTx runs fn in a read-only transaction, which sees the database as it
// was when its first read ran, however long fn takes.
func (d *Den) readTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(tx)
}

// NewSyncEpoch starts the den on a new run of versions, as after a restore
// from a backup: the backup's database counts message IDs from where it
// was made, so the restored den can give a new message an ID that a client
// holds for another one, which the backup never had. A client of another
// epoch drops what it cached from the den.
func (d *Den) NewSyncEpoch(ctx context.Context) error {
	if _, created := d.Info(); !created {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `UPDATE den SET sync_epoch = ? WHERE id = 1`, []byte(denproto.Random(denproto.SyncEpochSize)))
	return err
}

// pruner forgets changes older than changeLogAge, hourly.
type pruner struct {
	mu      sync.Mutex
	timer   *time.Timer
	stopped bool
}

// StartPruning forgets changes older than the den keeps them, now and
// hourly. Without it, as in tests, pruning runs only when called.
func (d *Den) StartPruning() {
	d.prune()
}

func (d *Den) prune() {
	if err := d.pruneChanges(context.Background()); err != nil {
		d.log.Errorf("forget old changes: %v", err)
	}
	p := &d.pruner
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped {
		p.timer = time.AfterFunc(pruneEvery, d.prune)
	}
}

func (d *Den) stopPruning() {
	p := &d.pruner
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	if p.timer != nil {
		p.timer.Stop()
	}
}

// pruneChanges forgets the changes older than changeLogAge, and moves the
// floor past them: a client that last synced below it can't tell what it
// missed, and drops what it cached.
func (d *Den) pruneChanges(ctx context.Context) error {
	if _, created := d.Info(); !created {
		return nil
	}
	cutoff := d.now().Add(-changeLogAge).UnixMilli()
	return d.tx(ctx, func(tx *sql.Tx) error {
		// The rows go in order up to the first one still young enough, so a
		// clock that once ran back only keeps changes longer.
		var keep int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM den_changes WHERE at >= ? ORDER BY id LIMIT 1`, cutoff).Scan(&keep)
		var floor int64
		switch {
		case err == nil:
			floor = keep - 1
		case errors.Is(err, sql.ErrNoRows):
			if floor, err = version(ctx, tx); err != nil {
				return err
			}
		default:
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_changes WHERE id <= ?`, floor); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE den SET changes_floor = max(changes_floor, ?) WHERE id = 1`, floor)
		return err
	})
}
