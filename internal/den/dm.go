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
	if err != nil || !created {
		return c, err
	}
	return c, d.Hub.Publish(denproto.EventChannelCreated, c, Pair(low, high))
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
