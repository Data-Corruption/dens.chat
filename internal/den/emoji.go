package den

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// The den's emoji (M15.2): images staff add, which members write by name,
// each one of the den's named files, as a sound is (M14.3). An emoji takes
// its file as a profile takes its picture, so retention never removes it,
// and the file counts toward the den's space, not its uploader's. Unlike a
// sound's, its file opens to every member, since every page draws it.
//
// The page of the staff member adding an emoji draws a still one into a
// PNG of at most MaxEmojiSide pixels on its longer side, and the member's
// local service makes an animated one into a GIF of that size, as it makes
// an animated picture (M15.1); the den checks the sides and the size.
// Members write an emoji by its name, which the den never looks for in
// what they write.

// kindEmoji is a named file's kind for an emoji.
const kindEmoji = "emoji"

var (
	errEmojiNotFound        = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such emoji")
	errEmojiNameTaken error = denproto.Errorf(http.StatusConflict, denproto.CodeEmojiNameTaken, "another emoji has that name")
)

const emojiColumns = `n.id, n.name, n.file_id, f.width, f.height, f.animated, n.added_by, n.added_at`

func scanEmoji(scan func(...any) error) (denproto.Emoji, error) {
	var e denproto.Emoji
	var id, file, by int64
	var w, h sql.NullInt64
	if err := scan(&id, &e.Name, &file, &w, &h, &e.Image.Animated, &by, &e.AddedAt); err != nil {
		return e, err
	}
	e.ID, e.Image.ID, e.AddedBy = denproto.FormatID(id), denproto.FormatID(file), denproto.FormatID(by)
	e.Image.Width, e.Image.Height = int(w.Int64), int(h.Int64)
	return e, nil
}

// emojis lists the den's emoji, by name.
func (d *Den) emojis(ctx context.Context, q querier) ([]denproto.Emoji, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+emojiColumns+` FROM den_named_files n JOIN den_files f ON f.id = n.file_id
		WHERE n.kind = ? ORDER BY n.name, n.id`, kindEmoji)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []denproto.Emoji{}
	for rows.Next() {
		e, err := scanEmoji(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *Den) emoji(ctx context.Context, q querier, id int64) (denproto.Emoji, error) {
	e, err := scanEmoji(q.QueryRowContext(ctx, `SELECT `+emojiColumns+` FROM den_named_files n JOIN den_files f ON f.id = n.file_id
		WHERE n.id = ? AND n.kind = ?`, id, kindEmoji).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return e, errEmojiNotFound
	}
	return e, err
}

// emojiNameTaken reports whether another of the den's emoji than id has a
// name. Names are lowercase, so the table's rule in any case is this one.
func emojiNameTaken(ctx context.Context, q querier, name string, id int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM den_named_files WHERE kind = ? AND name = ? COLLATE NOCASE AND id != ?`,
		kindEmoji, name, id).Scan(&n)
	return n > 0, err
}

// AddEmoji adds an emoji to the den, at staff's word: a name, and one of
// their uploads waiting to be used, which becomes its image. It must be a
// still image or an animated GIF, within the sides and size an emoji has,
// and the den must keep fewer emoji than its owner allows.
func (d *Den) AddEmoji(ctx context.Context, s *Session, req denproto.EmojiRequest) (denproto.Emoji, error) {
	if !IsStaff(s.Role) {
		return denproto.Emoji{}, forbidden("only moderators and the owner add emoji")
	}
	name, err := denproto.NormalizeEmojiName(req.Name)
	if err != nil {
		return denproto.Emoji{}, invalid("name: %v", err)
	}
	fid, err := denproto.ParseID(req.FileID)
	if err != nil {
		return denproto.Emoji{}, invalid("file_id: no such upload waiting to be used")
	}
	info, _ := d.Info()
	limits := denproto.DefaultEmojiLimits
	if info.EmojiLimits != nil {
		limits = *info.EmojiLimits
	}
	if limits.Emojis == 0 {
		return denproto.Emoji{}, invalid("emoji are off in this den")
	}
	now := d.now()
	var emoji denproto.Emoji
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var typ string
		var w, h sql.NullInt64
		var size int64
		var animated bool
		err := tx.QueryRowContext(ctx, `SELECT type, width, height, size, animated FROM den_files
			WHERE id = ? AND uploader_id = ? AND sealed = 0 AND replaces IS NULL AND created_at > ? AND `+unused,
			fid, s.MemberID, now.Add(-uploadExpiry).UnixMilli()).Scan(&typ, &w, &h, &size, &animated)
		if errors.Is(err, sql.ErrNoRows) {
			return invalid("file_id: no such upload waiting to be used")
		}
		if err != nil {
			return err
		}
		if err := checkEmojiImage(typ, w, h, size, animated); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_named_files WHERE kind = ?`, kindEmoji).Scan(&n); err != nil {
			return err
		}
		if n >= limits.Emojis {
			return invalid("the den keeps as many emoji as it allows, %d", limits.Emojis)
		}
		if taken, err := emojiNameTaken(ctx, tx, name, 0); err != nil || taken {
			return cmp.Or(err, errEmojiNameTaken)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_named_files (kind, name, file_id, added_by, added_at) VALUES (?, ?, ?, ?, ?)`,
			kindEmoji, name, fid, s.MemberID, now.UnixMilli())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		emoji, err = d.emoji(ctx, tx, id)
		return err
	})
	if err != nil {
		return denproto.Emoji{}, err
	}
	d.log.Infof("Member %d added emoji %s", s.MemberID, emoji.ID)
	return emoji, d.Hub.Publish(denproto.EventEmojiCreated, emoji, Everyone)
}

// emojiTypes are the still images an emoji may be: those every page draws.
var emojiTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// checkEmojiImage checks an upload as an emoji's image: a still image every
// page draws, or an animated GIF, which only a member's own service makes;
// at most MaxEmojiSide pixels on each side, and of an emoji's size.
func checkEmojiImage(typ string, w, h sql.NullInt64, size int64, animated bool) error {
	if !w.Valid || !h.Valid || !emojiTypes[typ] || animated && typ != "image/gif" {
		return invalid("an emoji is a still image or an animated GIF")
	}
	if w.Int64 < 1 || h.Int64 < 1 || w.Int64 > denproto.MaxEmojiSide || h.Int64 > denproto.MaxEmojiSide {
		return invalid("an emoji is at most %d pixels on each side", denproto.MaxEmojiSide)
	}
	switch {
	case animated && size > denproto.MaxAnimatedEmojiSize:
		return invalid("an animated emoji is at most %d KiB", denproto.MaxAnimatedEmojiSize>>10)
	case !animated && size > denproto.MaxEmojiSize:
		return invalid("an emoji is at most %d KiB", denproto.MaxEmojiSize>>10)
	}
	return nil
}

// RenameEmoji gives one of the den's emoji another name, at staff's word.
// The messages that wrote it by its old name show that name as text.
func (d *Den) RenameEmoji(ctx context.Context, s *Session, id string, req denproto.EmojiUpdate) (denproto.Emoji, error) {
	if !IsStaff(s.Role) {
		return denproto.Emoji{}, forbidden("only moderators and the owner rename emoji")
	}
	eid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Emoji{}, errEmojiNotFound
	}
	name, err := denproto.NormalizeEmojiName(req.Name)
	if err != nil {
		return denproto.Emoji{}, invalid("name: %v", err)
	}
	var emoji denproto.Emoji
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if taken, err := emojiNameTaken(ctx, tx, name, eid); err != nil || taken {
			return cmp.Or(err, errEmojiNameTaken)
		}
		res, err := tx.ExecContext(ctx, `UPDATE den_named_files SET name = ? WHERE id = ? AND kind = ?`, name, eid, kindEmoji)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errEmojiNotFound
		}
		emoji, err = d.emoji(ctx, tx, eid)
		return err
	})
	if err != nil {
		return denproto.Emoji{}, err
	}
	return emoji, d.Hub.Publish(denproto.EventEmojiUpdated, emoji, Everyone)
}

// DeleteEmoji deletes one of the den's emoji, and its image, at staff's
// word. The messages that wrote it show its name as text, and its
// reactions go with it (M15.3): the members' pages drop them as they hear
// it went, and the change log has their messages fetched again by an
// install that was away.
func (d *Den) DeleteEmoji(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner delete emoji")
	}
	eid, err := denproto.ParseID(id)
	if err != nil {
		return errEmojiNotFound
	}
	// Its reactions' messages change, as a reaction changes them.
	d.changes.Lock()
	defer d.changes.Unlock()
	var blobs [][]byte
	err = d.tx(ctx, func(tx *sql.Tx) error {
		reacted, err := reactedWith(ctx, tx, eid)
		if err != nil {
			return err
		}
		var file int64
		err = tx.QueryRowContext(ctx, `DELETE FROM den_named_files WHERE id = ? AND kind = ? RETURNING file_id`, eid, kindEmoji).Scan(&file)
		if errors.Is(err, sql.ErrNoRows) {
			return errEmojiNotFound
		}
		if err != nil {
			return err
		}
		now := d.now()
		for _, m := range reacted {
			if err := logMessage(ctx, tx, m[0], m[1], now); err != nil {
				return err
			}
		}
		blobs, err = deleteFiles(ctx, tx, `id = ?`, file)
		return err
	})
	if err != nil {
		return err
	}
	d.files.remove(blobs)
	d.log.Infof("Member %d deleted emoji %d", s.MemberID, eid)
	return d.Hub.Publish(denproto.EventEmojiDeleted, denproto.EmojiDeleted{ID: denproto.FormatID(eid)}, Everyone)
}
