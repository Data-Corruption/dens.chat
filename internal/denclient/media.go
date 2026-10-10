package denclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Video, audio, and photos browsers can't show. Video and audio lose their
// metadata on this machine, in the media module (internal/media/ffmpeg),
// which copies their streams into a new container; a HEIC, TIFF, JPEG 2000
// or Photoshop photo becomes a JPEG, or a PNG with transparency, which then
// goes as any image. The file waits in scratch files sealed with keys that
// live only for the job, so nothing of it lies on disk in the clear.

// scratchPattern names this service's scratch files.
const scratchPattern = "dens-scratch-*"

// refusal explains why a file isn't sent: head is its start.
func refusal(head []byte) error {
	name := media.Name(head)
	if name == "" {
		name = "this kind of file"
	}
	return inputError(fmt.Errorf("Dens can't send %s, because it can't take out the details it may carry, "+
		"such as where it was made. Save it in a common format, such as JPEG or MP4, and send that.", name))
}

// mediaError explains why the media module couldn't take a file.
func (m *Manager) mediaError(err error) error {
	var job *ffmpeg.JobError
	switch {
	case errors.Is(err, ffmpeg.ErrUnreadable):
		return inputError(errors.New("This file is damaged, or in a form Dens can't read, so it can't be sent."))
	case errors.As(err, &job):
		m.log.Warnf("The media module failed on a file to send: %s", job.Reason)
		return inputError(errors.New("Dens couldn't take the details out of this file, so it wasn't sent. It may be damaged."))
	}
	return err
}

// prepared is what the media module made of a file to send.
type prepared struct {
	// out is the copy to send, which kind and name describe: a converted
	// photo is a JPEG or PNG named for it.
	out       *vault.Scratch
	kind      media.Kind
	name      string
	converted bool
	// stripped describes a video or audio copy; its MIME is empty for a
	// photo.
	stripped media.Stripped
}

func (p *prepared) close() { _ = p.out.Close() }

// prepare runs a file of a kind that needs it through the media module:
// body is the whole file, of at most limit bytes, and the copy it makes
// may take at most fit, or the largest file a den takes when fit is 0.
func (m *Manager) prepare(ctx context.Context, kind media.Kind, head []byte, name string, body io.Reader, limit, fit int64) (*prepared, error) {
	if m.Media == nil {
		return nil, inputError(errors.New("Dens can't send video, audio or this kind of photo here."))
	}
	if fit <= 0 {
		fit = denproto.MaxFileSize
	}
	in, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	n, err := io.Copy(io.NewOffsetWriter(in, 0), io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, ErrTooLarge
	}
	out, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	p := &prepared{out: out, kind: kind, name: name}
	if err := m.convert(ctx, p, head, in, &ffmpeg.Limited{Output: out, Max: fit}); err != nil {
		out.Close()
		return nil, err
	}
	return p, nil
}

func (m *Manager) convert(ctx context.Context, p *prepared, head []byte, in *vault.Scratch, out *ffmpeg.Limited) error {
	if p.kind == media.Photo {
		tiff := bytes.HasPrefix(head, []byte("II*\x00")) || bytes.HasPrefix(head, []byte("MM\x00*"))
		if tiff && media.TIFFRaw(in) {
			return inputError(errors.New("Dens can't send a camera raw photo, because it can't take out the details it may carry, " +
				"such as where it was made. Export it as JPEG and send that."))
		}
		img, err := m.Media.Still(ctx, in, out, 0, media.StillQuality, 0)
		if out.Over() {
			return ErrTooLarge
		}
		if err != nil {
			return m.mediaError(err)
		}
		// The module reads hostile files, so what it made is checked too:
		// the image path that follows strips and checks it as any image.
		start := make([]byte, media.SniffLen)
		k, _ := p.out.ReadAt(start, 0)
		if p.kind = media.Sniff(start[:k]); p.kind.MIME() != img.MIME() {
			return m.mediaError(&ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a still that isn't what it says"})
		}
		ext := ".jpg"
		if p.kind == media.PNG {
			ext = ".png"
		}
		p.name = denproto.CleanFilename(strings.TrimSuffix(p.name, path.Ext(p.name)) + ext)
		p.converted = true
		return nil
	}

	var err error
	p.stripped, err = media.StripMedia(ctx, m.Media, in, out)
	if out.Over() {
		return ErrTooLarge
	}
	if err != nil {
		return m.mediaError(err)
	}
	return nil
}

// SetThumb gives a video uploaded and not sent yet the preview the page drew
// from its first frame, for a video the media module doesn't decode, such
// as WebM and AV1. The image loses its metadata on the way, as any image
// does, and a DM's is sealed with its file's key.
func (m *Manager) SetThumb(ctx context.Context, denID, fileID string, size int64, body io.Reader) (Uploaded, error) {
	c, err := m.find(denID)
	if err != nil {
		return Uploaded{}, err
	}
	if err := checkID("file", fileID); err != nil {
		return Uploaded{}, err
	}
	if size > denproto.MaxPreviewUpload {
		return Uploaded{}, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(body, denproto.MaxPreviewUpload+1))
	if err != nil {
		return Uploaded{}, err
	}
	if len(data) > denproto.MaxPreviewUpload {
		return Uploaded{}, ErrTooLarge
	}
	kind := media.Sniff(data)
	if kind != media.JPEG && kind != media.PNG {
		return Uploaded{}, inputError(errors.New("A video's preview is a JPEG or PNG."))
	}
	var clean bytes.Buffer
	res, err := media.Strip(&clean, bytes.NewReader(data), kind)
	if err != nil {
		return Uploaded{}, stripError(err)
	}
	c.mu.Lock()
	u, dm := c.uploads[fileID]
	c.mu.Unlock()
	if dm {
		return c.setDMThumb(ctx, fileID, u, clean.Bytes(), kind, res)
	}
	f, err := c.post(ctx, "/api/uploads/"+fileID+"/thumb", &clean, int64(clean.Len()), "")
	if err != nil {
		return Uploaded{}, err
	}
	if f.ID != fileID || f.Thumb == nil {
		return Uploaded{}, errors.New("the den's answer is malformed")
	}
	return Uploaded{File: f}, nil
}

// setDMThumb makes a DM video's preview from what the page drew, as a den
// does for a channel's, and uploads it sealed.
func (c *conn) setDMThumb(ctx context.Context, fileID string, u dmUpload, data []byte, kind media.Kind, res media.Result) (Uploaded, error) {
	f := u.file
	if f.Thumb != nil || !strings.HasPrefix(f.Type, "video/") || f.Width == 0 {
		return Uploaded{}, inputError(errors.New("Only a video without a preview takes one."))
	}
	if !denproto.PreviewShape(res.Width, res.Height, f.Width, f.Height) {
		return Uploaded{}, inputError(errors.New("A video's preview has the video's shape."))
	}
	th, err := media.Thumbnail(bytes.NewReader(data), kind, res)
	if err != nil {
		return Uploaded{}, stripError(err)
	}
	ch, _ := denproto.ParseID(u.channel)
	thumb, err := c.uploadDMThumb(ctx, f.Key, ch, th, "")
	if err != nil {
		return Uploaded{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// The file may have been sent, or given a preview, meanwhile. The
	// preview uploaded for it then waits out the hour unused.
	cur, ok := c.uploads[fileID]
	if !ok || cur.file.Thumb != nil {
		return Uploaded{}, inputError(errors.New("This file was sent, or has a preview, already."))
	}
	cur.file.Thumb = thumb
	c.uploads[fileID] = cur
	c.dmFiles[fileID] = dmFile{channel: ch, key: f.Key, thumb: thumb.ID}
	return dmUploaded(cur.file), nil
}
