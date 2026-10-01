package den

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Video and audio: the den strips them again with the media module, from
// what it stored, and keeps that copy. It can't check a container for
// metadata the way it checks an image, since FFmpeg's demuxers skip what
// they don't know, but nothing leaves FFmpeg's muxer that the driver didn't
// copy. A video's preview is its first frame, or, for one the module can't
// decode, an image the uploader's page drew (SetThumb).

// refuse says why the den doesn't take a kind of file, if it doesn't.
func (s *fileStore) refuse(k media.Kind, head []byte) error {
	switch {
	case k.Refused():
		return denproto.Errorf(http.StatusUnsupportedMediaType, denproto.CodeUnsupportedType,
			"Dens doesn't take %s, which can carry metadata it can't remove", media.Name(head))
	case k == media.Photo:
		return denproto.Errorf(http.StatusUnsupportedMediaType, denproto.CodeUnsupportedType,
			"a photo in this format is sent as a JPEG or PNG, which clients turn it into before uploading")
	case k.NeedsMedia() && s.media == nil:
		return denproto.Errorf(http.StatusUnsupportedMediaType, denproto.CodeUnsupportedType,
			"this den can't take video or audio")
	}
	return nil
}

// mediaRefusal says why the media module couldn't take an upload.
func (d *Den) mediaRefusal(err error) error {
	var job *ffmpeg.JobError
	switch {
	case errors.Is(err, ffmpeg.ErrUnreadable):
		return invalid("the file is damaged, or isn't what it starts out as")
	case errors.As(err, &job):
		d.log.Warnf("The media module failed on an upload: %s", job.Reason)
		return invalid("the den's media module failed on the file, which may be damaged")
	}
	return err
}

// restrip strips a video or audio upload again into the copy the den keeps,
// which it returns finished, and describes that copy in f: its type and
// size, a video's size as it shows, its duration, and a video's preview.
func (d *Den) restrip(ctx context.Context, orig *part, blob []byte, l denproto.Limits, f *denproto.File) (stored, thumb *part, thumbSize int64, err error) {
	m := d.files.media
	in, closer, err := orig.openAt(d.v)
	if err != nil {
		return nil, nil, 0, err
	}
	defer closer.Close()
	scratch, err := vault.NewScratch(d.files.temp, partPrefix+"scratch-*")
	if err != nil {
		return nil, nil, 0, err
	}
	defer scratch.Close()
	out := &ffmpeg.Limited{Output: scratch, Max: l.FileSize}
	s, err := media.StripMedia(ctx, m, in, out)
	if out.Over() {
		return nil, nil, 0, tooLarge(l)
	}
	if err != nil {
		return nil, nil, 0, d.mediaRefusal(err)
	}
	f.Type, f.Size, f.Width, f.Height, f.Duration = s.MIME, scratch.Size(), s.Width, s.Height, s.DurationMS
	var th media.Thumb
	if s.Decodes {
		th, err = media.Poster(ctx, m, scratch)
		if ctx.Err() != nil {
			return nil, nil, 0, ctx.Err()
		}
		if err != nil {
			d.log.Infof("A video upload has no preview: %v", err)
		}
	}

	stored, err = d.files.create(d.v, blob, false)
	if err != nil {
		return nil, nil, 0, err
	}
	if _, err := io.Copy(stored, io.NewSectionReader(scratch, 0, f.Size)); err != nil {
		stored.abort()
		return nil, nil, 0, err
	}
	if err := stored.finish(); err != nil {
		stored.abort()
		return nil, nil, 0, err
	}
	if th.Data != nil {
		if thumb, err = d.storeThumb(blob, th); err != nil {
			stored.abort()
			return nil, nil, 0, err
		}
		f.Thumb = &denproto.Thumb{Width: th.Width, Height: th.Height}
	}
	return stored, thumb, int64(len(th.Data)), nil
}

// storeThumb seals a preview into a finished part.
func (d *Den) storeThumb(blob []byte, th media.Thumb) (*part, error) {
	p, err := d.files.create(d.v, blob, true)
	if err != nil {
		return nil, err
	}
	if _, err := p.Write(th.Data); err != nil {
		p.abort()
		return nil, err
	}
	if err := p.finish(); err != nil {
		p.abort()
		return nil, err
	}
	return p, nil
}

// SetThumb gives a video upload the preview its uploader's page made, for a
// video the den can't make one for, as WebM and AV1, whose frames the
// module doesn't decode. The page draws the first frame the browser plays,
// at the video's size, and sends it as a JPEG or PNG without metadata. The
// den makes its own preview from that image, so it keeps nothing the page
// made as it was. The upload must be the member's own, waiting to be used,
// and a video without a preview; size is the request's length, or -1.
func (d *Den) SetThumb(ctx context.Context, s *Session, id string, size int64, body io.Reader) (denproto.File, error) {
	fid, err := denproto.ParseID(id)
	if err != nil {
		return denproto.File{}, errFileNotFound
	}
	pending := func(q querier) (blob []byte, w, h int64, err error) {
		var typ string
		var width, height sql.NullInt64
		err = q.QueryRowContext(ctx, `SELECT blob, type, width, height FROM den_files
			WHERE id = ? AND uploader_id = ? AND sealed = 0 AND thumb_size IS NULL AND created_at > ? AND `+unused,
			fid, s.MemberID, d.now().Add(-uploadExpiry).UnixMilli()).Scan(&blob, &typ, &width, &height)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, 0, invalid("no such upload waiting for a preview")
		}
		if err != nil {
			return nil, 0, 0, err
		}
		if !strings.HasPrefix(typ, "video/") || !width.Valid || !height.Valid {
			return nil, 0, 0, invalid("only a video takes a preview")
		}
		return blob, width.Int64, height.Int64, nil
	}
	blob, vw, vh, err := pending(d.db)
	if err != nil {
		return denproto.File{}, err
	}
	if size > denproto.MaxPreviewUpload {
		return denproto.File{}, invalid("a video's preview is at most %d bytes", denproto.MaxPreviewUpload)
	}
	data, err := io.ReadAll(io.LimitReader(body, denproto.MaxPreviewUpload+1))
	if err != nil {
		return denproto.File{}, err
	}
	if len(data) > denproto.MaxPreviewUpload {
		return denproto.File{}, invalid("a video's preview is at most %d bytes", denproto.MaxPreviewUpload)
	}
	kind := media.Sniff(data)
	if kind != media.JPEG && kind != media.PNG {
		return denproto.File{}, invalid("a video's preview is a JPEG or PNG")
	}
	res, err := media.Verify(io.Discard, bytes.NewReader(data), kind)
	switch {
	case errors.Is(err, media.ErrMetadata):
		return denproto.File{}, invalid("the preview still carries metadata, which clients take out before uploading")
	case err != nil:
		return denproto.File{}, invalid("the preview is damaged, or isn't what it starts out as")
	}
	if !denproto.PreviewShape(res.Width, res.Height, int(vw), int(vh)) {
		return denproto.File{}, invalid("a video's preview has the video's shape")
	}

	select {
	case d.files.decode <- struct{}{}:
	case <-ctx.Done():
		return denproto.File{}, ctx.Err()
	}
	th, err := media.Thumbnail(bytes.NewReader(data), kind, res)
	<-d.files.decode
	if err != nil {
		return denproto.File{}, invalid("the preview can't be read: %v", err)
	}
	thumb, err := d.storeThumb(blob, th)
	if err != nil {
		return denproto.File{}, err
	}
	defer thumb.abort()

	info, _ := d.Info()
	var f denproto.File
	err = d.tx(ctx, func(tx *sql.Tx) error {
		// The upload may have been used, or given a preview, meanwhile.
		if _, _, _, err := pending(tx); err != nil {
			return err
		}
		st, err := usage(ctx, tx, s.MemberID)
		if err != nil {
			return err
		}
		if err := checkSpace(info.Limits, st, int64(len(th.Data))); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_files SET thumb_width = ?, thumb_height = ?, thumb_size = ? WHERE id = ?`,
			th.Width, th.Height, len(th.Data), fid); err != nil {
			return err
		}
		if f, _, err = d.scanFile(tx.QueryRowContext(ctx, `SELECT `+fileColumns+` FROM den_files WHERE id = ?`, fid).Scan); err != nil {
			return err
		}
		return thumb.move()
	})
	if err != nil {
		return denproto.File{}, err
	}
	thumb.kept = true
	return f, nil
}
