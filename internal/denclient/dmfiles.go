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

// DM files (M1.7). The den can't open a DM's files, so this service does
// what the den does for a channel's: it takes an image's metadata out and
// makes its preview. Then it seals the file and its preview with a key of
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

// uploadDM strips, previews, seals and uploads a file for a DM.
func (c *conn) uploadDM(ctx context.Context, channelID, name string, size int64, body io.Reader) (Uploaded, error) {
	limits := c.limits()
	if limits.FileSize > 0 && vault.SealedSize(max(size, 0)) > limits.FileSize {
		return Uploaded{}, ErrTooLarge
	}
	g := &gate{r: body}
	defer g.shut()
	br := bufio.NewReaderSize(g, media.SniffLen)
	head, err := br.Peek(media.SniffLen)
	if err != nil && !errors.Is(err, io.EOF) {
		return Uploaded{}, err
	}
	kind := media.Sniff(head)
	if kind.Refused() {
		return Uploaded{}, refusal(kind)
	}
	file := denproto.DMFile{Name: denproto.CleanFilename(name), Type: kind.MIME()}
	if file.Type == "" {
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
	sent, err := c.uploadSealed(ctx, pr, vault.SealedSize(file.Size))
	if err != nil {
		return Uploaded{}, err
	}
	file.ID = sent.ID
	if thumb != nil {
		var sealed bytes.Buffer
		sw, err := vault.SealStreamWith(file.Key, &sealed, denproto.DMFileAD(c.j.denID, ch, true))
		if err != nil {
			return Uploaded{}, err
		}
		if _, err := sw.Write(thumb.Data); err != nil {
			return Uploaded{}, err
		}
		if err := sw.Close(); err != nil {
			return Uploaded{}, err
		}
		preview, err := c.uploadSealed(ctx, &sealed, int64(sealed.Len()))
		if err != nil {
			return Uploaded{}, err
		}
		file.Thumb = &denproto.DMThumb{ID: preview.ID, Width: thumb.Width, Height: thumb.Height}
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
	up := Uploaded{File: denproto.File{ID: file.ID, Name: file.Name, Type: file.Type, Size: file.Size,
		Width: file.Width, Height: file.Height, Animated: file.Animated}, Stripped: res.Removed}
	if file.Thumb != nil {
		up.Thumb = &denproto.Thumb{Width: file.Thumb.Width, Height: file.Thumb.Height}
	}
	return up, nil
}

// uploadSealed sends a sealed blob to the den, of the given length.
func (c *conn) uploadSealed(ctx context.Context, body io.Reader, length int64) (denproto.File, error) {
	token, err := c.session(ctx)
	if err != nil {
		return denproto.File{}, err
	}
	a := c.remote()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/api/uploads/sealed", body)
	if err != nil {
		return denproto.File{}, err
	}
	req.ContentLength = length
	a.headers(req.Header, token)
	req.Header.Set("Content-Type", "application/octet-stream")
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
	if len(data) > denproto.MaxBody || json.Unmarshal(data, &f) != nil || denproto.CheckFile(f) != nil || !f.Sealed || f.Size != length {
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

// sentUploads forgets uploads a message sent.
func (c *conn) sentUploads(blobs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range blobs {
		delete(c.uploads, id)
	}
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

// openDMFile fetches a DM file, or its preview, and opens it with the key
// its message sealed.
func (c *conn) openDMFile(ctx context.Context, fileID string, df dmFile, thumb bool) (io.ReadCloser, int64, error) {
	blob := fileID
	if thumb {
		if df.thumb == "" {
			return nil, 0, &denproto.Error{Status: http.StatusNotFound, Code: denproto.CodeNotFound, Message: "no preview"}
		}
		blob = df.thumb
	}
	res, err := c.transfer(ctx, "/api/files/"+blob)
	if err != nil {
		return nil, 0, err
	}
	size, ok := vault.OpenedSize(res.ContentLength)
	if !ok || size > denproto.MaxFileSize {
		res.Body.Close()
		return nil, 0, errors.New("the den's answer is malformed")
	}
	r, err := vault.OpenStreamWith(df.key, io.LimitReader(res.Body, res.ContentLength), denproto.DMFileAD(c.j.denID, df.channel, thumb))
	if err != nil {
		res.Body.Close()
		return nil, 0, err
	}
	return readCloser{r, res.Body}, size, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}
