package den

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Disappearing messages (M16.4). Any member of a DM or a group sets how
// long its messages stay after they're sent, which applies to those sent
// after: each keeps when it goes (expires_at), which an edit leaves as it
// was. The den's timer deletes each as its time comes, as its author's
// delete would: its row, with its files and reactions, secure_delete
// overwriting them, message.deleted to its members, and the change log,
// so an install that was away drops it as it comes back. Retention takes
// what passes its period first.
//
// The den can't read a DM, but it must know the time to delete by it, so
// it keeps it in the clear, with who set it and when, for the line in the
// conversation that says so: its word on that can't be checked, as a
// group's changes are, but it deletes the messages either way.

// SetExpiry sets how long a DM's or a group's messages stay after they're
// sent, for those sent from now on, or with 0 keeps them. Any of its
// members sets it, while there's someone to write to; the same again
// changes nothing and tells nobody.
func (d *Den) SetExpiry(ctx context.Context, s *Session, channelID string, req denproto.ExpiryRequest) (denproto.Channel, error) {
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return c, err
	}
	if !isPrivate(c) || isNotes(c) {
		return c, invalid("only a DM's or a group's messages disappear")
	}
	if err := denproto.CheckExpiresAfter(req.ExpiresAfter, d.ShortTimes); err != nil {
		return c, invalid("%v", err)
	}
	if c.Kind == denproto.KindDM {
		if err := d.checkDMOpen(ctx, c, s.MemberID); err != nil {
			return c, err
		}
	}
	// Two members setting it at once tell everyone in the order the den
	// kept them.
	d.changes.Lock()
	defer d.changes.Unlock()
	res, err := d.db.ExecContext(ctx, `UPDATE den_channels SET expires_after = ?, expires_by = ?, expires_set_at = ?
		WHERE id = ? AND expires_after != ?`, req.ExpiresAfter, s.MemberID, d.now().UnixMilli(), cid, req.ExpiresAfter)
	if err != nil {
		return c, err
	}
	if c, err = d.channel(ctx, d.db, cid); err != nil {
		return c, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, nil
	}
	d.log.Infof("Member %d set channel %d's messages to disappear after %d seconds", s.MemberID, cid, req.ExpiresAfter)
	return c, d.Hub.Publish(denproto.EventChannelUpdated, c, audienceOf(c))
}

// disappeared is a message the timer deleted: its ID, its channel and its
// files.
type disappeared struct {
	id, channel int64
	files       []string
}

// deleteDue deletes the messages whose time came, a batch at a time, and
// stops between batches once the timer does.
func (d *Den) deleteDue(ctx context.Context) error {
	for {
		n, err := d.deleteBatch(ctx)
		if err != nil || n < expireBatch {
			return err
		}
		d.timer.mu.Lock()
		closed := d.timer.closed
		d.timer.mu.Unlock()
		if closed {
			return nil
		}
	}
}

// deleteBatch deletes up to expireBatch of the messages whose time came, as
// their authors' deletes would, and tells each one's channel, and each
// member a message counted for their read state as it now is (see
// passDeleted). It returns how many it deleted.
func (d *Den) deleteBatch(ctx context.Context) (int, error) {
	d.changes.Lock()
	defer d.changes.Unlock()
	now := d.now()
	var gone []disappeared
	var blobs [][]byte
	var readers []reader
	err := d.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, channel_id FROM den_messages WHERE expires_at <= ? ORDER BY expires_at, id LIMIT ?`,
			now.UnixMilli(), expireBatch)
		if err != nil {
			return err
		}
		for rows.Next() {
			var g disappeared
			if err := rows.Scan(&g.id, &g.channel); err != nil {
				rows.Close()
				return err
			}
			gone = append(gone, g)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(gone) == 0 {
			return err
		}
		args := make([]any, len(gone))
		for i, g := range gone {
			args[i] = g.id
		}
		in := `(?` + strings.Repeat(", ?", len(gone)-1) + `)`
		files, b, err := filesOf(ctx, tx, `m.id IN `+in, args...)
		if err != nil {
			return err
		}
		blobs = b
		for i := range gone {
			gone[i].files = files[gone[i].id]
		}
		ids := make([]int64, len(gone))
		for i, g := range gone {
			ids[i] = g.id
		}
		if readers, err = readersOf(ctx, tx, ids); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_messages WHERE id IN `+in, args...); err != nil {
			return err
		}
		byChannel := map[int64][]int64{}
		for _, g := range gone {
			if err := logMessage(ctx, tx, g.id, g.channel, now); err != nil {
				return err
			}
			byChannel[g.channel] = append(byChannel[g.channel], g.id)
		}
		return passDeleted(ctx, tx, readers, byChannel)
	})
	if err != nil || len(gone) == 0 {
		return 0, err
	}
	d.files.remove(blobs)
	d.log.Debugf("Deleted %d disappearing messages, with %d files", len(gone), len(blobs))
	var errs []error
	channels := map[int64]*denproto.Channel{}
	for _, g := range gone {
		c, ok := channels[g.channel]
		if !ok {
			found, err := d.channel(ctx, d.db, g.channel)
			if err != nil && !errors.Is(err, errChannelNotFound) {
				errs = append(errs, err)
			}
			if err == nil {
				c = &found
			}
			channels[g.channel] = c
		}
		if c == nil {
			continue
		}
		errs = append(errs, d.Hub.Publish(denproto.EventMessageDeleted,
			denproto.MessageDeleted{ID: denproto.FormatID(g.id), ChannelID: c.ID, Files: g.files}, audienceOf(*c)))
	}
	errs = append(errs, d.publishReaders(ctx, readers))
	return len(gone), errors.Join(errs...)
}
