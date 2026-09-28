package den

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// OpenDM returns the DM between the session's member and another, creating
// it on first use. Each pair of members has one DM. A former member's DM
// still opens, for its history, but a new one can't start with them.
func (d *Den) OpenDM(ctx context.Context, s *Session, req denproto.DMRequest) (denproto.Channel, error) {
	other, err := denproto.ParseID(req.MemberID)
	if err != nil {
		return denproto.Channel{}, errMemberNotFound
	}
	if other == s.MemberID {
		return denproto.Channel{}, invalid("a DM is with someone else")
	}
	low, high := min(s.MemberID, other), max(s.MemberID, other)
	var c denproto.Channel
	created := false
	err = d.tx(ctx, func(tx *sql.Tx) error {
		c, err = scanChannel(tx.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM den_channels
			WHERE kind = 'dm' AND dm_low = ? AND dm_high = ?`, low, high).Scan)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var left sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT left_at FROM den_members WHERE id = ?`, other).Scan(&left)
		if errors.Is(err, sql.ErrNoRows) || left.Valid {
			return errMemberNotFound
		}
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_channels (group_id, name, kind, position, dm_low, dm_high, created_at)
			VALUES (NULL, '', 'dm', 0, ?, ?, ?)`, low, high, d.now().UnixMilli())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		created = true
		c, err = d.channel(ctx, tx, id)
		return err
	})
	if err != nil {
		return c, err
	}
	if !created {
		// Opening a DM the member closed brings it back to their list.
		cid, _ := denproto.ParseID(c.ID)
		reopened, err := reopen(ctx, d.db, cid, s.MemberID)
		if err != nil || len(reopened) == 0 {
			return c, err
		}
		return c, d.publishReadState(ctx, s.MemberID, cid)
	}
	return c, d.Hub.Publish(denproto.EventChannelCreated, c, Pair(low, high))
}

// CloseDM takes a DM out of the member's list, on all their devices,
// until either member sends a message in it or the member opens it again.
func (d *Den) CloseDM(ctx context.Context, s *Session, channelID string) error {
	c, cid, err := d.visibleChannel(ctx, s, channelID)
	if err != nil {
		return err
	}
	if c.Kind != denproto.KindDM {
		return invalid("only DMs close")
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO den_read_states (member_id, channel_id, message_id, closed) VALUES (?, ?, 0, 1)
		ON CONFLICT (member_id, channel_id) DO UPDATE SET closed = 1`, s.MemberID, cid); err != nil {
		return err
	}
	return d.publishReadState(ctx, s.MemberID, cid)
}

// reopen marks a closed DM open again, for one member or, with member 0,
// both, and returns whose it reopened.
func reopen(ctx context.Context, q querier, channel, member int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `UPDATE den_read_states SET closed = 0
		WHERE channel_id = ?1 AND closed = 1 AND (?2 = 0 OR member_id = ?2) RETURNING member_id`, channel, member)
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

// dmPartner returns the other member of a DM.
func dmPartner(c denproto.Channel, member int64) (int64, bool) {
	if len(c.Members) != 2 {
		return 0, false
	}
	me := denproto.FormatID(member)
	for i, id := range c.Members {
		if id == me {
			other, err := denproto.ParseID(c.Members[1-i])
			return other, err == nil
		}
	}
	return 0, false
}

// checkDMOpen refuses a message to a DM whose other member has left.
func (d *Den) checkDMOpen(ctx context.Context, c denproto.Channel, author int64) error {
	other, ok := dmPartner(c, author)
	if !ok {
		return errChannelNotFound
	}
	var left sql.NullInt64
	if err := d.db.QueryRowContext(ctx, `SELECT left_at FROM den_members WHERE id = ?`, other).Scan(&left); err != nil {
		return err
	}
	if left.Valid {
		return forbidden("they're no longer in this den")
	}
	return nil
}
