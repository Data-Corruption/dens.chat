package den

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Notes to self (M17.3). A member's notes are a channel of kind notes
// naming them alone, made as they first open it, which only they see. Its
// messages come sealed, as a DM's do, under a key their Dens makes from
// their seal, which the den never holds: each names it by the channel's
// ID. Text, files, replies, edits and deletes work as in a DM; reactions,
// polls, mentions, editors and disappearing times don't, and nothing in
// them counts as unread, since everything in them is the member's own.
//
// The den sends nothing of notes to a Dens speaking a version of the
// protocol before 3, which would close its connection on a channel of a
// kind it doesn't know as the den breaking the protocol.

var errNoNotes = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "notes need version 3 of the protocol")

// isNotes reports whether a channel is a member's notes.
func isNotes(c denproto.Channel) bool { return c.Kind == denproto.KindNotes }

// notes lists a member's notes: one channel, once they've opened them, or
// none.
func (d *Den) notes(ctx context.Context, q querier, member int64) ([]denproto.Channel, error) {
	return d.queryChannels(ctx, q, `SELECT `+channelColumns+` FROM den_channels WHERE kind = 'notes' AND notes_of = ?`, member)
}

// OpenNotes returns the session's member's notes, making them the first
// time. Each member has one in the den.
func (d *Den) OpenNotes(ctx context.Context, s *Session) (denproto.Channel, error) {
	if s.Version < denproto.NotesVersion {
		return denproto.Channel{}, errNoNotes
	}
	var c denproto.Channel
	created := false
	err := d.tx(ctx, func(tx *sql.Tx) error {
		var err error
		c, err = scanChannel(tx.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM den_channels
			WHERE kind = 'notes' AND notes_of = ?`, s.MemberID).Scan)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_channels (group_id, name, kind, position, notes_of, created_at)
			VALUES (NULL, '', 'notes', 0, ?, ?)`, s.MemberID, d.now().UnixMilli())
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
	d.log.Infof("Member %d opened their notes", s.MemberID)
	return c, d.Hub.Publish(denproto.EventChannelCreated, c, notesOf(s.MemberID))
}

// deleteNotes deletes a member's notes within tx, as they leave the den:
// nobody else can read them, and they hold nothing for anyone else. It
// returns the blobs of their files, to remove once tx commits.
func deleteNotes(ctx context.Context, tx *sql.Tx, member int64) ([][]byte, error) {
	_, blobs, err := filesOf(ctx, tx, `m.channel_id IN (SELECT id FROM den_channels WHERE kind = 'notes' AND notes_of = ?)`, member)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM den_channels WHERE kind = 'notes' AND notes_of = ?`, member)
	return blobs, err
}

// sealedKey checks the key a sealed message names, as it's sent or edited:
// a DM's or a group's live key, as sendKey checks it, or in notes their
// channel's ID, which names the key the member's Dens makes from their
// seal. It returns the key_id the message keeps.
func sealedKey(ctx context.Context, q querier, c denproto.Channel, channel int64, keyID string, member int64) (int64, error) {
	if isNotes(c) {
		if keyID != c.ID {
			return 0, invalid("key_id: notes name their channel's ID")
		}
		return channel, nil
	}
	return sendKey(ctx, q, channel, keyID, member)
}
