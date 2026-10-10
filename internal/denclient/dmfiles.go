package denclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// DM files. The den can't open a DM's files, so this service does what
// the den does for a channel's: it takes an image's metadata out and makes
// its preview, or a video's from its first frame. Then it seals the file and its preview with a key of
// the file's own, which goes inside the message that sends them, and the
// den stores two blobs it can't tell from noise.

// uploadExpiry is how long a den keeps an upload waiting for a message.
const uploadExpiry = time.Hour

// dmUpload is a file uploaded for a DM and not sent yet: what the message
// that sends it will seal about it.
type dmUpload struct {
	channel string
	file    denproto.DMFile
	at      time.Time
}

// tempAD names the sealed copy of a DM file this service keeps while it
// makes the file's preview.
var tempAD = []byte("dens-dm-upload")

// uploadDM strips, previews, seals and uploads a file for a DM, or what
// ready holds when it's set, as the media module made it already. One made
// to replace a file of the DM's replaces its preview too, with its own.
func (c *conn) uploadDM(ctx context.Context, channelID, name string, size int64, body io.Reader, replaces string,
	ready *prepared) (Uploaded, error) {
	limits := c.limits()
	if limits.FileSize > 0 && vault.SealedSize(max(size, 0)) > limits.FileSize {
		return Uploaded{}, ErrTooLarge
	}
	p := ready
	var br *bufio.Reader
	var head []byte
	var kind media.Kind
	var err error
	if p == nil {
		g := &gate{r: body}
		defer g.shut()
		br = bufio.NewReaderSize(g, media.SniffLen)
		head, err = br.Peek(media.SniffLen)
		if err != nil && !errors.Is(err, io.EOF) {
			return Uploaded{}, err
		}
		kind = media.Sniff(head)
		if kind.Refused() {
			return Uploaded{}, refusal(head)
		}
		if kind.NeedsMedia() {
			if p, err = c.m.prepare(ctx, kind, head, name, br, max(limits.FileSize, size), dmFit(limits)); err != nil {
				return Uploaded{}, err
			}
			defer p.close()
		}
	}
	if p != nil {
		kind, name, size = p.kind, p.name, p.out.Size()
		br = bufio.NewReaderSize(io.NewSectionReader(p.out, 0, size), media.SniffLen)
		if head, err = br.Peek(media.SniffLen); err != nil && !errors.Is(err, io.EOF) {
			return Uploaded{}, err
		}
	}
	file := denproto.DMFile{Name: denproto.CleanFilename(name), Type: kind.MIME()}
	switch {
	case p != nil && p.stripped.MIME != "":
		file.Type = p.stripped.MIME
	case file.Type == "":
		file.Type = sniffType(head)
	}

	// The file waits on disk while its preview is made, sealed with a key
	// only this function holds.
	tmp, err := os.CreateTemp(c.m.TempDir, "dens-dm-*")
	if err != nil {
		return Uploaded{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	tmpKey := denproto.Random(32)
	defer clear(tmpKey)
	w, err := vault.SealStreamWith(tmpKey, tmp, tempAD)
	if err != nil {
		return Uploaded{}, err
	}
	n := &counter{w: w}
	in := io.LimitReader(br, max(limits.FileSize, size)+1)
	var res media.Result
	if kind.Image() {
		if res, err = media.Strip(n, in, kind); err != nil {
			return Uploaded{}, stripError(err)
		}
	} else if _, err := io.Copy(n, in); err != nil {
		return Uploaded{}, err
	}
	if err := w.Close(); err != nil {
		return Uploaded{}, err
	}
	if limits.FileSize > 0 && vault.SealedSize(n.n) > limits.FileSize {
		return Uploaded{}, ErrTooLarge
	}
	file.Size = n.n
	reopen := func() (io.Reader, error) {
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return vault.OpenStreamWith(tmpKey, bufio.NewReader(tmp), tempAD)
	}

	var thumb *media.Thumb
	if kind.Image() {
		file.Width, file.Height, file.Animated = res.Width, res.Height, res.Animated
		if media.CanPreview(res) {
			r, err := reopen()
			if err != nil {
				return Uploaded{}, err
			}
			th, err := media.Thumbnail(r, kind, res)
			switch {
			case err == nil:
				thumb = &th
			case errors.Is(err, media.ErrMalformed), errors.Is(err, media.ErrTooBig):
				// It goes without a preview, as the den would send it.
			default:
				return Uploaded{}, err
			}
		}
	}
	if p != nil && p.stripped.MIME != "" {
		file.Width, file.Height, file.Duration = p.stripped.Width, p.stripped.Height, p.stripped.DurationMS
		switch {
		case p.poster != nil:
			// A video's copy takes the preview made from its full size
			// (M5.4).
			thumb = p.poster
		case p.stripped.Decodes:
			th, err := media.Poster(ctx, c.m.Media, p.out)
			switch {
			case err == nil:
				thumb = &th
			case ctx.Err() != nil:
				return Uploaded{}, ctx.Err()
			default:
				// The page draws one instead, where the browser plays it.
				c.m.log.Infof("A DM video has no preview: %v", err)
			}
		}
	}

	file.Key = denproto.Random(32)
	ch, _ := denproto.ParseID(channelID)
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		r, err := reopen()
		if err == nil {
			var sw io.WriteCloser
			if sw, err = vault.SealStreamWith(file.Key, pw, denproto.DMFileAD(c.j.denID, ch, false)); err == nil {
				if _, err = io.Copy(sw, r); err == nil {
					err = sw.Close()
				}
			}
		}
		pw.CloseWithError(err)
	}()
	var oldThumb string
	if replaces != "" {
		c.mu.Lock()
		oldThumb = c.dmFiles[replaces].thumb
		c.mu.Unlock()
	}
	var sealed io.Reader = pr
	if p != nil && p.sending != nil {
		sealed = &counted{r: pr, total: vault.SealedSize(file.Size), report: p.sending}
	}
	sent, err := c.uploadSealed(ctx, sealed, vault.SealedSize(file.Size), replaces)
	if err != nil {
		return Uploaded{}, err
	}
	file.ID = sent.ID
	if thumb != nil {
		if file.Thumb, err = c.uploadDMThumb(ctx, file.Key, ch, *thumb, oldThumb); err != nil {
			return Uploaded{}, err
		}
	}

	now := time.Now()
	c.mu.Lock()
	for id, u := range c.uploads {
		if now.Sub(u.at) > uploadExpiry {
			delete(c.uploads, id)
		}
	}
	c.uploads[file.ID] = dmUpload{channel: channelID, file: file, at: now}
	c.dmFiles[file.ID] = dmFile{channel: ch, key: file.Key}
	if file.Thumb != nil {
		c.dmFiles[file.ID] = dmFile{channel: ch, key: file.Key, thumb: file.Thumb.ID}
	}
	c.mu.Unlock()
	up := dmUploaded(file)
	up.Stripped = res.Removed
	if p != nil {
		up.Stripped, up.Converted = true, p.converted
	}
	return up, nil
}

// dmFit is the largest file that fits a den's limit sealed, as a DM's file
// goes.
func dmFit(limits denproto.Limits) int64 {
	return limits.FileSize - (vault.SealedSize(limits.FileSize) - limits.FileSize)
}

// dmUploaded describes a DM file to the page as a den describes a
// channel's.
func dmUploaded(file denproto.DMFile) Uploaded {
	up := Uploaded{File: denproto.File{ID: file.ID, Name: file.Name, Type: file.Type, Size: file.Size,
		Width: file.Width, Height: file.Height, Animated: file.Animated, Duration: file.Duration}}
	if file.Thumb != nil {
		up.Thumb = &denproto.Thumb{Width: file.Thumb.Width, Height: file.Thumb.Height}
	}
	return up
}

// uploadDMThumb seals a DM file's preview with the file's key and uploads
// it, made to replace the preview replaces names when it's set.
func (c *conn) uploadDMThumb(ctx context.Context, key denproto.Bytes, channel int64, th media.Thumb, replaces string) (*denproto.DMThumb, error) {
	var sealed bytes.Buffer
	sw, err := vault.SealStreamWith(key, &sealed, denproto.DMFileAD(c.j.denID, channel, true))
	if err != nil {
		return nil, err
	}
	if _, err := sw.Write(th.Data); err != nil {
		return nil, err
	}
	if err := sw.Close(); err != nil {
		return nil, err
	}
	preview, err := c.uploadSealed(ctx, &sealed, int64(sealed.Len()), replaces)
	if err != nil {
		return nil, err
	}
	return &denproto.DMThumb{ID: preview.ID, Width: th.Width, Height: th.Height}, nil
}

// uploadSealed sends a sealed blob to the den, of the given length, made
// to replace the blob replaces names when it's set.
func (c *conn) uploadSealed(ctx context.Context, body io.Reader, length int64, replaces string) (denproto.File, error) {
	f, err := c.post(ctx, "/api/uploads/sealed", body, length, replaces)
	if err != nil {
		return denproto.File{}, err
	}
	if !f.Sealed || f.Size != length {
		return denproto.File{}, errors.New("the den's answer is malformed")
	}
	return f, nil
}

// post sends bytes to the den, of the given length, and reads back the
// file they made or changed. replaces names the file an upload is made to
// take the place of, if any.
func (c *conn) post(ctx context.Context, path string, body io.Reader, length int64, replaces string) (denproto.File, error) {
	token, err := c.session(ctx)
	if err != nil {
		return denproto.File{}, err
	}
	a := c.remote()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, body)
	if err != nil {
		return denproto.File{}, err
	}
	req.ContentLength = length
	a.headers(req.Header, token)
	req.Header.Set("Content-Type", "application/octet-stream")
	if replaces != "" {
		req.Header.Set(denproto.HeaderReplaces, replaces)
	}
	res, err := c.m.Transfer.Do(req)
	if err != nil {
		return denproto.File{}, fmt.Errorf("reach the den: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return denproto.File{}, denproto.ReadError(res)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, denproto.MaxBody+1))
	if err != nil {
		return denproto.File{}, fmt.Errorf("read the den's response: %w", err)
	}
	var f denproto.File
	if len(data) > denproto.MaxBody || json.Unmarshal(data, &f) != nil || denproto.CheckFile(f) != nil {
		return denproto.File{}, errors.New("the den's answer is malformed")
	}
	return f, nil
}

// takeUploads finds the files a DM message sends, as uploaded for that DM:
// what it seals about them, and every blob it puts on the message.
func (c *conn) takeUploads(channelID string, ids []string) ([]denproto.DMFile, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var files []denproto.DMFile
	var blobs []string
	for _, id := range ids {
		u, ok := c.uploads[id]
		if !ok || u.channel != channelID {
			return nil, nil, inputError(errors.New("a file isn't ready to send in this DM any more; add it again"))
		}
		files = append(files, u.file)
		blobs = append(blobs, u.file.ID)
		if u.file.Thumb != nil {
			blobs = append(blobs, u.file.Thumb.ID)
		}
	}
	return files, blobs, nil
}

// sentUploads forgets uploads a message sent, and the versions kept of
// its photos.
func (c *conn) sentUploads(blobs []string) {
	c.mu.Lock()
	for _, id := range blobs {
		delete(c.uploads, id)
	}
	c.mu.Unlock()
	c.dropKept(blobs)
}

// sniffType names a file that isn't an image, as a den does, for the page
// to pick an icon.
func sniffType(head []byte) string {
	t, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if err != nil || denproto.CheckMediaType(t) != nil {
		return "application/octet-stream"
	}
	return t
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
