package den

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// The den's soundboard (M14.3): sounds staff add, each one of the den's
// named files, which a member in a call plays for everyone in it (see
// voice.go). A sound takes its file as a profile takes its picture, so
// retention never removes it, and the file counts toward the den's space,
// not its uploader's, since it's the den's.
//
// The page of the staff member adding a sound encodes it as Opus in Ogg,
// and it arrives as any upload does, stripped by the media module. The den
// takes only Opus in Ogg, which it reads back packet by packet, no longer
// than the owner allows, and no larger than that length at the bitrate
// makes; and it sends those packets as they are when the sound plays.

// kindSound is a named file's kind for a sound.
const kindSound = "sound"

var (
	errSoundNotFound        = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such sound")
	errSoundNameTaken error = denproto.Errorf(http.StatusConflict, denproto.CodeSoundNameTaken, "another sound has that name")
)

const soundColumns = `n.id, n.name, n.file_id, f.duration_ms, n.added_by, n.added_at`

func scanSound(scan func(...any) error) (denproto.Sound, error) {
	var s denproto.Sound
	var id, file, by int64
	var ms sql.NullInt64
	if err := scan(&id, &s.Name, &file, &ms, &by, &s.AddedAt); err != nil {
		return s, err
	}
	s.ID, s.FileID, s.AddedBy, s.Duration = denproto.FormatID(id), denproto.FormatID(file), denproto.FormatID(by), ms.Int64
	return s, nil
}

// sounds lists the den's sounds, by name.
func (d *Den) sounds(ctx context.Context, q querier) ([]denproto.Sound, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+soundColumns+` FROM den_named_files n JOIN den_files f ON f.id = n.file_id
		WHERE n.kind = ? ORDER BY n.name COLLATE NOCASE, n.id`, kindSound)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []denproto.Sound{}
	for rows.Next() {
		s, err := scanSound(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *Den) sound(ctx context.Context, q querier, id int64) (denproto.Sound, error) {
	s, err := scanSound(q.QueryRowContext(ctx, `SELECT `+soundColumns+` FROM den_named_files n JOIN den_files f ON f.id = n.file_id
		WHERE n.id = ? AND n.kind = ?`, id, kindSound).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return s, errSoundNotFound
	}
	return s, err
}

// nameTaken reports whether another of the den's sounds than id has a name,
// in any case.
func nameTaken(ctx context.Context, q querier, name string, id int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM den_named_files WHERE kind = ? AND name = ? COLLATE NOCASE AND id != ?`,
		kindSound, name, id).Scan(&n)
	return n > 0, err
}

// AddSound adds a sound to the den's soundboard, at staff's word: a name,
// and one of their uploads waiting to be used, which becomes the sound's
// file. It must be Opus in Ogg, within the den's limits as they are now.
func (d *Den) AddSound(ctx context.Context, s *Session, req denproto.SoundRequest) (denproto.Sound, error) {
	if !IsStaff(s.Role) {
		return denproto.Sound{}, forbidden("only moderators and the owner add sounds")
	}
	name, err := denproto.CleanSoundName(req.Name)
	if err != nil {
		return denproto.Sound{}, invalid("name: %v", err)
	}
	fid, err := denproto.ParseID(req.FileID)
	if err != nil {
		return denproto.Sound{}, invalid("file_id: no such upload waiting to be used")
	}
	info, _ := d.Info()
	limits := info.SoundLimits
	if limits.Sounds == 0 {
		return denproto.Sound{}, invalid("the soundboard is off in this den")
	}
	now := d.now()
	// waiting finds the upload, the member's own and waiting to be used.
	waiting := func(q querier) (blob []byte, typ string, size int64, err error) {
		err = q.QueryRowContext(ctx, `SELECT blob, type, size FROM den_files
			WHERE id = ? AND uploader_id = ? AND sealed = 0 AND replaces IS NULL AND created_at > ? AND `+unused,
			fid, s.MemberID, now.Add(-uploadExpiry).UnixMilli()).Scan(&blob, &typ, &size)
		if errors.Is(err, sql.ErrNoRows) {
			err = invalid("file_id: no such upload waiting to be used")
		}
		return blob, typ, size, err
	}
	blob, typ, size, err := waiting(d.db)
	if err != nil {
		return denproto.Sound{}, err
	}
	if typ != "audio/ogg" {
		return denproto.Sound{}, invalid("a sound is Opus in Ogg, which the page prepares")
	}
	samples, err := d.checkSound(blob, size, limits)
	if err != nil {
		return denproto.Sound{}, err
	}
	var sound denproto.Sound
	err = d.tx(ctx, func(tx *sql.Tx) error {
		// The upload may have been used meanwhile, the soundboard filled,
		// or the name taken.
		if _, _, _, err := waiting(tx); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_named_files WHERE kind = ?`, kindSound).Scan(&n); err != nil {
			return err
		}
		if n >= limits.Sounds {
			return invalid("the soundboard holds as many sounds as this den allows, %d", limits.Sounds)
		}
		if taken, err := nameTaken(ctx, tx, name, 0); err != nil || taken {
			return cmp.Or(err, errSoundNameTaken)
		}
		// The sound's length is what plays, which the file's says too.
		if _, err := tx.ExecContext(ctx, `UPDATE den_files SET duration_ms = ? WHERE id = ?`, max(samples*1000/media.OpusRate, 1), fid); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO den_named_files (kind, name, file_id, added_by, added_at) VALUES (?, ?, ?, ?, ?)`,
			kindSound, name, fid, s.MemberID, now.UnixMilli())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		sound, err = d.sound(ctx, tx, id)
		return err
	})
	if err != nil {
		return denproto.Sound{}, err
	}
	d.log.Infof("Member %d added sound %s", s.MemberID, sound.ID)
	return sound, d.Hub.Publish(denproto.EventSoundCreated, sound, Everyone)
}

// checkSound reads a stored file as a sound, and returns how many samples
// at Opus's rate it plays after its pre-skip: Opus in Ogg, no longer than
// the den's longest, give or take its last packet, which the encoder pads,
// and no larger than its length at the bitrate makes.
func (d *Den) checkSound(blob []byte, size int64, limits denproto.SoundLimits) (int, error) {
	f, err := d.files.open(d.v, blob, false)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	o, err := media.NewOggOpus(f)
	if err != nil {
		return 0, invalid("a sound is Opus in Ogg: %v", err)
	}
	samples, last, err := o.Length()
	if err != nil {
		return 0, invalid("a sound is Opus in Ogg: %v", err)
	}
	if samples <= 0 {
		return 0, invalid("the sound plays nothing")
	}
	if samples-last > limits.Longest*media.OpusRate {
		return 0, invalid("sounds are at most %d seconds long on this den", limits.Longest)
	}
	if ms := int64(samples) * 1000 / media.OpusRate; size > denproto.MaxSoundSize(ms) {
		return 0, invalid("a sound of %d ms is at most %d bytes", ms, denproto.MaxSoundSize(ms))
	}
	return samples, nil
}

// RenameSound gives one of the den's sounds another name, at staff's word.
func (d *Den) RenameSound(ctx context.Context, s *Session, id string, req denproto.SoundUpdate) (denproto.Sound, error) {
	if !IsStaff(s.Role) {
		return denproto.Sound{}, forbidden("only moderators and the owner rename sounds")
	}
	sid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.Sound{}, errSoundNotFound
	}
	name, err := denproto.CleanSoundName(req.Name)
	if err != nil {
		return denproto.Sound{}, invalid("name: %v", err)
	}
	var sound denproto.Sound
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if taken, err := nameTaken(ctx, tx, name, sid); err != nil || taken {
			return cmp.Or(err, errSoundNameTaken)
		}
		res, err := tx.ExecContext(ctx, `UPDATE den_named_files SET name = ? WHERE id = ? AND kind = ?`, name, sid, kindSound)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errSoundNotFound
		}
		sound, err = d.sound(ctx, tx, sid)
		return err
	})
	if err != nil {
		return denproto.Sound{}, err
	}
	return sound, d.Hub.Publish(denproto.EventSoundUpdated, sound, Everyone)
}

// DeleteSound deletes one of the den's sounds, and its file, at staff's
// word. A call it plays in hears the rest of it.
func (d *Den) DeleteSound(ctx context.Context, s *Session, id string) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner delete sounds")
	}
	sid, err := denproto.ParseID(id)
	if err != nil {
		return errSoundNotFound
	}
	var blobs [][]byte
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var file int64
		err := tx.QueryRowContext(ctx, `DELETE FROM den_named_files WHERE id = ? AND kind = ? RETURNING file_id`, sid, kindSound).Scan(&file)
		if errors.Is(err, sql.ErrNoRows) {
			return errSoundNotFound
		}
		if err != nil {
			return err
		}
		blobs, err = deleteFiles(ctx, tx, `id = ?`, file)
		return err
	})
	if err != nil {
		return err
	}
	d.files.remove(blobs)
	d.log.Infof("Member %d deleted sound %d", s.MemberID, sid)
	return d.Hub.Publish(denproto.EventSoundDeleted, denproto.SoundDeleted{ID: denproto.FormatID(sid)}, Everyone)
}

// soundFile is a sound's file, open to play, read as it plays.
type soundFile struct {
	*media.OggOpus
	f StoredFile
}

func (s soundFile) Close() error { return s.f.Close() }

// openSound opens one of the den's sounds to play.
func (d *Den) openSound(ctx context.Context, id string) (soundFile, error) {
	sid, err := denproto.ParseID(id)
	if err != nil {
		return soundFile{}, errSoundNotFound
	}
	var blob []byte
	err = d.db.QueryRowContext(ctx, `SELECT f.blob FROM den_named_files n JOIN den_files f ON f.id = n.file_id
		WHERE n.id = ? AND n.kind = ?`, sid, kindSound).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return soundFile{}, errSoundNotFound
	}
	if err != nil {
		return soundFile{}, err
	}
	f, err := d.files.open(d.v, blob, false)
	if err != nil {
		return soundFile{}, fmt.Errorf("open sound %d: %w", sid, err)
	}
	o, err := media.NewOggOpus(f)
	if err != nil {
		f.Close()
		return soundFile{}, fmt.Errorf("read sound %d: %w", sid, err)
	}
	return soundFile{o, f}, nil
}
