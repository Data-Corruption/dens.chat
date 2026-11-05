package den

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Reactions (M15.3): a member's emoji on a message. In a channel the den
// keeps a row for each, checked against Unicode's list or the den's own
// emoji, so a reaction carries no text, and tells those who see the
// channel of each change, one member's one emoji at a time, which applies
// to a set: applying one twice changes nothing, and nobody's upload goes
// on resending a message's whole set on every tap. In a DM the den can't
// read them: each member's set on a message comes sealed by their client,
// and the den keeps it whole against its revision, as an edit is kept, so
// a member's two devices don't drop each other's reactions.
//
// Each change records its message in the change log, as an edit does, so
// an install that was away fetches it again with its reactions, though
// the message's revision stays, so an edit under way meets no conflict.
// Reactions notify nobody. They go with their message, deleted or past the
// den's retention, and with one of the den's emoji as staff delete it;
// staff take one emoji's reactions off a channel's message, those of the
// members they outrank, and a ban that deletes a member's recent messages
// takes the reactions they added in that time too.

// reactionKey names an emoji among a message's reactions.
func reactionKey(emoji string, emojiID int64) string {
	if emoji != "" {
		return "u" + emoji
	}
	return "d" + denproto.FormatID(emojiID)
}

// reactionsOf returns the reactions on the channels' messages with the
// given IDs, by message: each emoji with its members, in the order each
// emoji came, and each member's in the order they reacted.
func reactionsOf(ctx context.Context, q querier, ids []int64) (map[int64][]denproto.Reaction, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, `SELECT message_id, coalesce(emoji, ''), coalesce(emoji_id, 0), member_id FROM den_reactions
		WHERE message_id IN (?`+strings.Repeat(", ?", len(ids)-1)+`) ORDER BY message_id, created_at, rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]denproto.Reaction{}
	at := map[int64]map[string]int{}
	for rows.Next() {
		var message, emojiID, member int64
		var emoji string
		if err := rows.Scan(&message, &emoji, &emojiID, &member); err != nil {
			return nil, err
		}
		index := at[message]
		if index == nil {
			index = map[string]int{}
			at[message] = index
		}
		key := reactionKey(emoji, emojiID)
		i, ok := index[key]
		if !ok {
			r := denproto.Reaction{Emoji: emoji}
			if emoji == "" {
				r.EmojiID = denproto.FormatID(emojiID)
			}
			i = len(out[message])
			index[key] = i
			out[message] = append(out[message], r)
		}
		out[message][i].Members = append(out[message][i].Members, denproto.FormatID(member))
	}
	return out, rows.Err()
}

// dmReactionsOf returns each member's sealed set on the DMs' messages with
// the given IDs, by message, in the order of the members' IDs.
func dmReactionsOf(ctx context.Context, q querier, ids []int64) (map[int64][]denproto.SealedReactions, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, `SELECT message_id, member_id, key_id, revision, sealed FROM den_dm_reactions
		WHERE message_id IN (?`+strings.Repeat(", ?", len(ids)-1)+`) ORDER BY message_id, member_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]denproto.SealedReactions{}
	for rows.Next() {
		var message, member, key int64
		var r denproto.SealedReactions
		var sealed []byte
		if err := rows.Scan(&message, &member, &key, &r.Revision, &sealed); err != nil {
			return nil, err
		}
		r.MemberID, r.KeyID, r.Sealed = denproto.FormatID(member), denproto.FormatID(key), sealed
		out[message] = append(out[message], r)
	}
	return out, rows.Err()
}

// reactable finds the channel of a message a member may see, and its ID
// and the message's: whose reactions they change, or staff take off.
func (d *Den) reactable(ctx context.Context, s *Session, messageID string) (denproto.Channel, int64, int64, error) {
	mid, err := denproto.ParseID(messageID)
	if err != nil {
		return denproto.Channel{}, 0, 0, errMessageNotFound
	}
	var channel int64
	err = d.db.QueryRowContext(ctx, `SELECT channel_id FROM den_messages WHERE id = ?`, mid).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) {
		return denproto.Channel{}, 0, 0, errMessageNotFound
	}
	if err != nil {
		return denproto.Channel{}, 0, 0, err
	}
	c, cid, err := d.visibleChannel(ctx, s, denproto.FormatID(channel))
	if err != nil {
		return denproto.Channel{}, 0, 0, errMessageNotFound
	}
	// A group's member reacts to what they can read, from their joining
	// (M16.3).
	if since, err := groupSince(ctx, d.db, c, cid, s.MemberID); err != nil || mid <= since {
		return denproto.Channel{}, 0, 0, errMessageNotFound
	}
	return c, cid, mid, nil
}

// messageThere reports whether a message is still there within tx, which
// a delete may have taken since its channel was found.
func messageThere(ctx context.Context, tx *sql.Tx, id int64) error {
	var there bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_messages WHERE id = ?)`, id).Scan(&there); err != nil {
		return err
	}
	if !there {
		return errMessageNotFound
	}
	return nil
}

// reactionArgs are what names an emoji in the reactions' table: one of
// Unicode's, or one of the den's, the other NULL. req has been checked.
func reactionArgs(req denproto.ReactionRequest) (emoji, emojiID any) {
	if req.Emoji != "" {
		return req.Emoji, nil
	}
	id, _ := denproto.ParseID(req.EmojiID)
	return nil, id
}

// React adds the member's reaction to a channel's message: one of
// Unicode's emoji, or one of the den's while emoji are on. A message holds
// at most MaxReactionEmoji different emoji, and MaxReactions in all; the
// same reaction again changes nothing.
func (d *Den) React(ctx context.Context, s *Session, messageID string, req denproto.ReactionRequest) error {
	return d.react(ctx, s, messageID, req, true)
}

// Unreact takes the member's reaction off a channel's message; one they
// haven't made changes nothing.
func (d *Den) Unreact(ctx context.Context, s *Session, messageID string, req denproto.ReactionRequest) error {
	return d.react(ctx, s, messageID, req, false)
}

func (d *Den) react(ctx context.Context, s *Session, messageID string, req denproto.ReactionRequest, add bool) error {
	if err := req.Check(); err != nil {
		return invalid("emoji: %v", err)
	}
	// Reactions wait their turn with edits, so an edit's message carries
	// the reactions as they were, and the events go out in the order the
	// changes were made.
	d.changes.Lock()
	defer d.changes.Unlock()
	c, cid, mid, err := d.reactable(ctx, s, messageID)
	if err != nil {
		return err
	}
	if isPrivate(c) {
		return invalid("a DM's reactions come sealed, as a set each member's client seals")
	}
	emoji, emojiID := reactionArgs(req)
	now := d.now()
	changed := false
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if err := messageThere(ctx, tx, mid); err != nil {
			return err
		}
		if !add {
			res, err := tx.ExecContext(ctx, `DELETE FROM den_reactions WHERE message_id = ? AND member_id = ? AND emoji IS ? AND emoji_id IS ?`,
				mid, s.MemberID, emoji, emojiID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil
			}
			changed = true
			return logMessage(ctx, tx, mid, cid, now)
		}
		if emojiID != nil {
			if err := d.checkDenEmoji(ctx, tx, emojiID.(int64)); err != nil {
				return err
			}
		}
		var mine, there bool
		var kinds, total int
		if err := tx.QueryRowContext(ctx, `SELECT
				EXISTS (SELECT 1 FROM den_reactions WHERE message_id = ?1 AND member_id = ?2 AND emoji IS ?3 AND emoji_id IS ?4),
				EXISTS (SELECT 1 FROM den_reactions WHERE message_id = ?1 AND emoji IS ?3 AND emoji_id IS ?4),
				(SELECT count(*) FROM (SELECT DISTINCT emoji, emoji_id FROM den_reactions WHERE message_id = ?1)),
				(SELECT count(*) FROM den_reactions WHERE message_id = ?1)`,
			mid, s.MemberID, emoji, emojiID).Scan(&mine, &there, &kinds, &total); err != nil {
			return err
		}
		switch {
		case mine:
			return nil
		case !there && kinds >= denproto.MaxReactionEmoji:
			return invalid("a message holds at most %d different emoji", denproto.MaxReactionEmoji)
		case total >= denproto.MaxReactions:
			return invalid("a message holds at most %d reactions", denproto.MaxReactions)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_reactions (message_id, member_id, emoji, emoji_id, created_at) VALUES (?, ?, ?, ?, ?)`,
			mid, s.MemberID, emoji, emojiID, now.UnixMilli()); err != nil {
			return err
		}
		changed = true
		return logMessage(ctx, tx, mid, cid, now)
	})
	if err != nil || !changed {
		return err
	}
	t := denproto.EventReactionRemoved
	if add {
		t = denproto.EventReactionAdded
	}
	return d.Hub.Publish(t, denproto.ReactionChange{MessageID: denproto.FormatID(mid), ChannelID: c.ID,
		MemberID: denproto.FormatID(s.MemberID), Emoji: req.Emoji, EmojiID: req.EmojiID}, audienceOf(c))
}

// checkDenEmoji checks that the den has an emoji to react with, and that
// its emoji are on, within tx.
func (d *Den) checkDenEmoji(ctx context.Context, tx *sql.Tx, id int64) error {
	info, _ := d.Info()
	if info.EmojiLimits != nil && info.EmojiLimits.Emojis == 0 {
		return invalid("emoji are off in this den")
	}
	var has bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM den_named_files WHERE id = ? AND kind = ?)`, id, kindEmoji).Scan(&has); err != nil {
		return err
	}
	if !has {
		return invalid("emoji_id: the den has no such emoji")
	}
	return nil
}

// ClearReactions takes one emoji's reactions off a channel's message, at
// staff's word: those of the members they outrank, as their other powers
// reach. A DM's reactions are its members' alone.
func (d *Den) ClearReactions(ctx context.Context, s *Session, messageID string, req denproto.ReactionRequest) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner take reactions off a message")
	}
	if err := req.Check(); err != nil {
		return invalid("emoji: %v", err)
	}
	d.changes.Lock()
	defer d.changes.Unlock()
	c, cid, mid, err := d.reactable(ctx, s, messageID)
	if err != nil {
		return err
	}
	if isPrivate(c) {
		return forbidden("a DM's reactions are its members' alone")
	}
	emoji, emojiID := reactionArgs(req)
	var cleared []int64
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if err := messageThere(ctx, tx, mid); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT r.member_id, m.role FROM den_reactions r JOIN den_members m ON m.id = r.member_id
			WHERE r.message_id = ? AND r.emoji IS ? AND r.emoji_id IS ? ORDER BY r.created_at, r.rowid`, mid, emoji, emojiID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var member int64
			var role string
			if err := rows.Scan(&member, &role); err != nil {
				rows.Close()
				return err
			}
			if denproto.Rank(role) < denproto.Rank(s.Role) {
				cleared = append(cleared, member)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, member := range cleared {
			if _, err := tx.ExecContext(ctx, `DELETE FROM den_reactions WHERE message_id = ? AND member_id = ? AND emoji IS ? AND emoji_id IS ?`,
				mid, member, emoji, emojiID); err != nil {
				return err
			}
		}
		if len(cleared) == 0 {
			return nil
		}
		return logMessage(ctx, tx, mid, cid, d.now())
	})
	if err != nil || len(cleared) == 0 {
		return err
	}
	d.log.Infof("Member %d took %d reactions off message %d", s.MemberID, len(cleared), mid)
	return d.Hub.Publish(denproto.EventReactionCleared, denproto.ReactionCleared{MessageID: denproto.FormatID(mid), ChannelID: c.ID,
		Emoji: req.Emoji, EmojiID: req.EmojiID, Members: formatIDs(cleared)}, audienceOf(c))
}

// reactionsConflict refuses a DM's set made against a revision other than
// the member's set's, carrying their set as it is now, if they have one.
type reactionsConflict struct{ current *denproto.SealedReactions }

func (c *reactionsConflict) Error() string { return "edit_conflict" }

// ReactionsConflict returns the set a DM's sealed set conflicted with,
// nil when the member had none.
func ReactionsConflict(err error) (*denproto.SealedReactions, bool) {
	var c *reactionsConflict
	if errors.As(err, &c) {
		return c.current, true
	}
	return nil, false
}

// SetDMReactions replaces the member's reactions on a DM's message with a
// set their client sealed with the DM's live key, padded to one length:
// made against the revision their set was at, 0 while they had none, as an
// edit is, so their two devices don't drop each other's reactions. The den
// sees that their reactions changed, and when, never what they are.
func (d *Den) SetDMReactions(ctx context.Context, s *Session, messageID string, req denproto.DMReactionsRequest) (denproto.SealedReactions, error) {
	var out denproto.SealedReactions
	if len(req.Sealed) != denproto.SealedReactionsSize {
		return out, invalid("sealed: a set of reactions is sealed to %d bytes", denproto.SealedReactionsSize)
	}
	if req.Revision < 0 {
		return out, invalid("revision: the set's revision, or 0 for none")
	}
	d.changes.Lock()
	defer d.changes.Unlock()
	c, cid, mid, err := d.reactable(ctx, s, messageID)
	if err != nil {
		return out, err
	}
	if !isPrivate(c) {
		return out, invalid("only a DM's reactions come sealed")
	}
	if c.Kind == denproto.KindDM {
		if err := d.checkDMOpen(ctx, c, s.MemberID); err != nil {
			return out, err
		}
	}
	now := d.now()
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if err := messageThere(ctx, tx, mid); err != nil {
			return err
		}
		// Checked here, so a start-over that retires the key can't slip in
		// between, as for a message.
		key, err := sendKey(ctx, tx, cid, req.KeyID, s.MemberID)
		if err != nil {
			return err
		}
		var current *denproto.SealedReactions
		var heldKey int64
		var held denproto.SealedReactions
		var sealed []byte
		err = tx.QueryRowContext(ctx, `SELECT key_id, revision, sealed FROM den_dm_reactions WHERE message_id = ? AND member_id = ?`,
			mid, s.MemberID).Scan(&heldKey, &held.Revision, &sealed)
		switch {
		case err == nil:
			held.MemberID, held.KeyID, held.Sealed = denproto.FormatID(s.MemberID), denproto.FormatID(heldKey), sealed
			current = &held
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if (current == nil && req.Revision != 0) || (current != nil && current.Revision != req.Revision) {
			return &reactionsConflict{current}
		}
		if current == nil {
			// A group's message carries the sets of its members and of those
			// who left it, as many as a message may (M16.3).
			var sets int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_dm_reactions WHERE message_id = ?`, mid).Scan(&sets); err != nil {
				return err
			}
			if sets >= denproto.MaxReactionSets {
				return invalid("a message holds the reactions of at most %d members", denproto.MaxReactionSets)
			}
		}
		out = denproto.SealedReactions{MemberID: denproto.FormatID(s.MemberID), KeyID: denproto.FormatID(key),
			Revision: req.Revision + 1, Sealed: req.Sealed}
		if _, err := tx.ExecContext(ctx, `INSERT INTO den_dm_reactions (message_id, member_id, key_id, revision, sealed, updated_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (message_id, member_id) DO UPDATE SET
			key_id = excluded.key_id, revision = excluded.revision, sealed = excluded.sealed, updated_at = excluded.updated_at`,
			mid, s.MemberID, key, out.Revision, []byte(req.Sealed), now.UnixMilli()); err != nil {
			return err
		}
		return logMessage(ctx, tx, mid, cid, now)
	})
	if err != nil {
		return denproto.SealedReactions{}, err
	}
	return out, d.Hub.Publish(denproto.EventDMReactions, denproto.DMReactions{MessageID: denproto.FormatID(mid), ChannelID: c.ID,
		SealedReactions: out}, audienceOf(c))
}

// reactedWith lists the channels' messages that hold reactions with one of
// the den's emoji, within tx, for the change log as the emoji goes.
func reactedWith(ctx context.Context, tx *sql.Tx, emoji int64) ([][2]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT r.message_id, m.channel_id FROM den_reactions r JOIN den_messages m ON m.id = r.message_id
		WHERE r.emoji_id = ? ORDER BY r.message_id`, emoji)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]int64
	for rows.Next() {
		var m [2]int64
		if err := rows.Scan(&m[0], &m[1]); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// unreactRecent takes off the reactions a member added to channels'
// messages since a time, within tx, as a ban that deletes their recent
// messages does, once it has: those on the messages it deleted went with
// them. It returns the changes to tell. A DM's sets stay, since the den
// can't tell one reaction in them from another.
func unreactRecent(ctx context.Context, tx *sql.Tx, member, since int64, now time.Time) ([]denproto.ReactionChange, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.message_id, m.channel_id, coalesce(r.emoji, ''), coalesce(r.emoji_id, 0) FROM den_reactions r
		JOIN den_messages m ON m.id = r.message_id WHERE r.member_id = ? AND r.created_at >= ? ORDER BY r.created_at, r.rowid`, member, since)
	if err != nil {
		return nil, err
	}
	type reaction struct {
		message, channel, emojiID int64
		emoji                     string
	}
	var found []reaction
	for rows.Next() {
		var r reaction
		if err := rows.Scan(&r.message, &r.channel, &r.emoji, &r.emojiID); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM den_reactions WHERE member_id = ? AND created_at >= ?`, member, since); err != nil {
		return nil, err
	}
	var out []denproto.ReactionChange
	logged := map[int64]bool{}
	for _, r := range found {
		change := denproto.ReactionChange{MessageID: denproto.FormatID(r.message), ChannelID: denproto.FormatID(r.channel),
			MemberID: denproto.FormatID(member), Emoji: r.emoji}
		if r.emoji == "" {
			change.EmojiID = denproto.FormatID(r.emojiID)
		}
		out = append(out, change)
		if !logged[r.message] {
			logged[r.message] = true
			if err := logMessage(ctx, tx, r.message, r.channel, now); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// publishUnreacted tells those who see each channel of the reactions a ban
// took off.
func (d *Den) publishUnreacted(ctx context.Context, changes []denproto.ReactionChange) error {
	audiences := map[string]Audience{}
	for _, change := range changes {
		audience, ok := audiences[change.ChannelID]
		if !ok {
			id, _ := denproto.ParseID(change.ChannelID)
			c, err := d.channel(ctx, d.db, id)
			if err != nil {
				return err
			}
			audience = audienceOf(c)
			audiences[change.ChannelID] = audience
		}
		if err := d.Hub.Publish(denproto.EventReactionRemoved, change, audience); err != nil {
			return err
		}
	}
	return nil
}
