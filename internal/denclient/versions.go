package denclient

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Smaller copies (M5). A photo or video added to a message goes as a
// smaller copy unless its sender sends it full size. This service makes
// both versions as it's added, with the media module, uploads the one to
// send, and keeps both, each in a scratch file sealed with a key of its
// own, until the message is sent, the file is taken off it, or the hour a
// den keeps an upload waiting is up. Meanwhile the page compares them, and
// switching uploads the other version and drops the one waiting. A video's
// copy takes a while (videos.go).

// Send says which version of a photo or video a message sends.
type Send string

const (
	// SendSmaller sends a smaller copy, when there's one worth sending: at
	// most three quarters of the full size.
	SendSmaller Send = "smaller"
	// SendFull sends a file full size, unless the den's limit is below it
	// and above its copy.
	SendFull Send = "full"
)

// Versions describes a file's two versions as the page compares them: full
// size, as it goes without a copy, and its smaller copy, and which one the
// upload is.
type Versions struct {
	Sent    Send    `json:"sent"`
	Full    Version `json:"full"`
	Smaller Version `json:"smaller"`
}

// Version is one of a file's versions: its type, its size in bytes, its
// pixels as it shows, a video's frames a second, and whether the den takes
// it, within its size limit as it stood when the file was added, sealed for
// a DM.
type Version struct {
	Type   string  `json:"type"`
	Size   int64   `json:"size"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	FPS    float64 `json:"fps,omitempty"`
	Fits   bool    `json:"fits"`
}

// version is one version of a file, as it goes: a photo's JPEG or PNG,
// stripped, or a video's MP4, which video describes.
type version struct {
	file          *vault.Scratch
	kind          media.Kind
	width, height int
	video         *media.Stripped
	fps           float64
}

func (v *version) size() int64 { return v.file.Size() }

func (v *version) reader() *io.SectionReader { return io.NewSectionReader(v.file, 0, v.file.Size()) }

func (v *version) close() {
	if v != nil {
		_ = v.file.Close()
	}
}

func (v *version) describe(fits bool) Version {
	d := Version{Type: v.kind.MIME(), Size: v.size(), Width: v.width, Height: v.height, Fits: fits}
	if v.video != nil {
		d.Type, d.FPS = v.video.MIME, math.Round(v.fps*100)/100
	}
	return d
}

// kept is a file's two versions, kept while the upload of one waits to be
// sent.
type kept struct {
	// mu is held while the file switches versions.
	mu sync.Mutex
	// id is the upload waiting; the conn's mu guards it.
	id                  string
	channel, replaces   string
	name                string
	full, smaller       *version
	fullFits            bool
	stripped, converted bool
	// poster is a video copy's preview, made from the full size.
	poster *media.Thumb
	sent   Send
	up     Uploaded // the upload waiting, as the page has it
	timer  *time.Timer
	once   sync.Once
}

func (k *kept) describe() *Versions {
	return &Versions{Sent: k.sent, Full: k.full.describe(k.fullFits), Smaller: k.smaller.describe(true)}
}

func (k *kept) close() {
	k.once.Do(func() {
		if k.timer != nil {
			k.timer.Stop()
		}
		k.full.close()
		k.smaller.close()
	})
}

// errNotKept is a switch or a look at a file whose versions are gone:
// sent, taken off, switched meanwhile, or past the hour.
var errNotKept = inputError(errors.New("This file's two versions aren't kept any more. Take it off the message and add it again."))

// UploadVersions uploads a file for a message, as Upload does, and a photo
// or video with its two versions: it sends the one send names, or its
// smaller copy whatever send says when the full size is over the den's
// limit and the copy isn't, and keeps both for the page to compare and
// switch between (OpenVersion, SwitchVersion). A file without a copy worth
// sending, and anything else, goes as Upload sends it. replaces names the
// file it's made to take the place of, if any, and key, if it's set, is
// what the page follows the upload's progress by (UploadProgress), and may
// ask for a video full size by instead of waiting for its copy
// (SendFullSize).
func (m *Manager) UploadVersions(ctx context.Context, denID, channelID, name string, size int64, body io.Reader, send Send,
	replaces, key string) (Uploaded, error) {
	if send != SendSmaller && send != SendFull {
		return Uploaded{}, inputError(errors.New("a file goes smaller or full size"))
	}
	if replaces != "" {
		if err := checkID("file", replaces); err != nil {
			return Uploaded{}, err
		}
	}
	c, err := m.find(denID)
	if err != nil {
		return Uploaded{}, err
	}
	var f *following
	if key != "" {
		var done func()
		if f, done, err = m.follow(denID, key); err != nil {
			return Uploaded{}, err
		}
		defer done()
	}
	g := &gate{r: body}
	defer g.shut()
	br := bufio.NewReaderSize(g, media.SniffLen)
	head, err := br.Peek(media.SniffLen)
	if err != nil && !errors.Is(err, io.EOF) {
		return Uploaded{}, err
	}
	kind := media.Sniff(head)
	if m.Media != nil && kind == media.Video {
		return m.uploadVideo(ctx, c, channelID, name, size, br, send, replaces, f)
	}
	if m.Media == nil || kind != media.JPEG && kind != media.PNG && kind != media.Photo {
		return m.uploadTo(ctx, c, channelID, name, size, br, replaces)
	}
	// Over the den's limit, a photo may still go as its copy, so it's
	// taken up to the largest file any den takes.
	if size > denproto.MaxFileSize {
		return Uploaded{}, ErrTooLarge
	}
	k, err := m.makeVersions(ctx, kind, head, name, io.LimitReader(br, denproto.MaxFileSize+1))
	if err != nil {
		return Uploaded{}, err
	}
	k.channel, k.replaces = channelID, replaces

	limits := c.limits()
	dm := false
	if channelID != "" {
		_, dm = c.isDM(channelID)
	}
	fits := func(v *version) bool {
		n := v.size()
		if dm {
			n = vault.SealedSize(n)
		}
		return limits.FileSize <= 0 || n <= limits.FileSize
	}
	k.fullFits = fits(k.full)
	// A copy goes when it's at most three quarters of the full size, or
	// when it's what fits.
	if k.smaller != nil && (!fits(k.smaller) || k.fullFits && k.smaller.size()*4 > k.full.size()*3) {
		k.smaller.close()
		k.smaller = nil
	}
	if k.smaller == nil {
		defer k.close()
		if !k.fullFits {
			return Uploaded{}, ErrTooLarge
		}
		return m.uploadVersion(ctx, c, k, k.full, f)
	}

	k.sent = send
	if !k.fullFits {
		k.sent = SendSmaller
	}
	v := k.full
	if k.sent == SendSmaller {
		v = k.smaller
	}
	up, err := m.uploadVersion(ctx, c, k, v, f)
	if err != nil {
		k.close()
		return Uploaded{}, err
	}
	up.Versions = k.describe()
	k.up, k.id = up, up.ID
	c.keep(k)
	return up, nil
}

// makeVersions makes a photo's versions from body, which head starts: full
// size, as the photo goes without a copy, and its smaller copy, when the
// media module makes one. A JPEG or PNG full size is the file stripped; a
// photo the module converts is a JPEG or PNG of all its pixels, as it's
// sent without a copy. The copy that can't be made is left out.
func (m *Manager) makeVersions(ctx context.Context, kind media.Kind, head []byte, name string, body io.Reader) (*kept, error) {
	k := &kept{name: name}
	if kind != media.Photo {
		full, err := m.newVersion(kind)
		if err != nil {
			return nil, err
		}
		res, err := media.Strip(io.NewOffsetWriter(full.file, 0), body, kind)
		if err != nil {
			full.close()
			return nil, stripError(err)
		}
		if full.size() > denproto.MaxFileSize {
			full.close()
			return nil, ErrTooLarge
		}
		full.width, full.height = res.Width, res.Height
		k.full, k.stripped = full, res.Removed
		if media.Copyable(kind, res) {
			k.smaller = m.copyOf(ctx, full.file, res.Orientation)
		}
		if err := ctx.Err(); err != nil {
			k.close()
			return nil, err
		}
		return k, nil
	}

	in, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	n, err := io.Copy(io.NewOffsetWriter(in, 0), body)
	if err != nil {
		return nil, err
	}
	if n > denproto.MaxFileSize {
		return nil, ErrTooLarge
	}
	tiff := strings.HasPrefix(string(head), "II*\x00") || strings.HasPrefix(string(head), "MM\x00*")
	if tiff && media.TIFFRaw(in) {
		return nil, inputError(errors.New("Dens can't send a camera raw photo, because it can't take out the details it may carry, " +
			"such as where it was made. Export it as JPEG and send that."))
	}
	if k.full, err = m.still(ctx, in, 0, media.StillQuality, 0); err != nil {
		return nil, m.mediaError(err)
	}
	k.smaller = m.copyOf(ctx, in, 0)
	if err := ctx.Err(); err != nil {
		k.close()
		return nil, err
	}
	ext := ".jpg"
	if k.full.kind == media.PNG {
		ext = ".png"
	}
	k.name = denproto.CleanFilename(strings.TrimSuffix(name, path.Ext(name)) + ext)
	k.stripped, k.converted = true, true
	return k, nil
}

// copyOf makes a photo's smaller copy, or nil when the module can't.
func (m *Manager) copyOf(ctx context.Context, in ffmpeg.Input, orientation int) *version {
	v, err := m.still(ctx, in, media.CopySide, media.CopyQuality, orientation)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Infof("A photo goes without a smaller copy: %v", err)
		}
		return nil
	}
	return v
}

// still makes a still of in with the media module, and checks it as any
// image a member sends: it must be the kind the module says it made, and
// the size, and it's stripped as one.
func (m *Manager) still(ctx context.Context, in ffmpeg.Input, maxSide, quality, orientation int) (*version, error) {
	tmp, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	defer tmp.Close()
	out := &ffmpeg.Limited{Output: tmp, Max: denproto.MaxFileSize}
	img, err := m.Media.Still(ctx, in, out, maxSide, quality, orientation)
	if out.Over() {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	start := make([]byte, media.SniffLen)
	n, _ := tmp.ReadAt(start, 0)
	kind := media.Sniff(start[:n])
	if kind.MIME() != img.MIME() {
		return nil, &ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a still that isn't what it says"}
	}
	v, err := m.newVersion(kind)
	if err != nil {
		return nil, err
	}
	res, err := media.Strip(io.NewOffsetWriter(v.file, 0), io.NewSectionReader(tmp, 0, tmp.Size()), kind)
	if err != nil || res.Width != img.Width || res.Height != img.Height {
		v.close()
		return nil, &ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a still that isn't the image it says"}
	}
	v.width, v.height = res.Width, res.Height
	return v, nil
}

func (m *Manager) newVersion(kind media.Kind) (*version, error) {
	f, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	return &version{file: f, kind: kind}, nil
}

// keep holds a file's versions while its upload waits, for as long as the
// den keeps the upload.
func (c *conn) keep(k *kept) {
	k.timer = time.AfterFunc(uploadExpiry, func() {
		c.mu.Lock()
		if c.kept[k.id] == k {
			delete(c.kept, k.id)
		}
		c.mu.Unlock()
		k.close()
	})
	c.mu.Lock()
	c.kept[k.id] = k
	c.mu.Unlock()
}

func (c *conn) keptFor(uploadID string) *kept {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.kept[uploadID]
}

// dropKept forgets the versions of uploads that are gone: sent or
// dropped.
func (c *conn) dropKept(ids []string) {
	var gone []*kept
	c.mu.Lock()
	for _, id := range ids {
		if k, ok := c.kept[id]; ok {
			delete(c.kept, id)
			gone = append(gone, k)
		}
	}
	c.mu.Unlock()
	for _, k := range gone {
		k.close()
	}
}

// dropAllKept forgets every file's versions, as the den's connection
// ends.
func (c *conn) dropAllKept() {
	c.mu.Lock()
	gone := c.kept
	c.kept = map[string]*kept{}
	c.mu.Unlock()
	for _, k := range gone {
		k.close()
	}
}

// OpenVersion opens a version of a photo or video waiting to be sent, for
// the page to compare.
func (m *Manager) OpenVersion(denID, uploadID string, which Send) (*OpenedFile, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	if err := checkID("upload", uploadID); err != nil {
		return nil, err
	}
	k := c.keptFor(uploadID)
	if k == nil {
		return nil, &denproto.Error{Status: http.StatusNotFound, Code: denproto.CodeNotFound, Message: "no versions kept for that upload"}
	}
	var v *version
	switch which {
	case SendFull:
		v = k.full
	case SendSmaller:
		v = k.smaller
	default:
		return nil, inputError(errors.New("a file's versions are full and smaller"))
	}
	head := make([]byte, media.SniffLen)
	n, _ := v.file.ReadAt(head, 0)
	return &OpenedFile{SectionReader: v.reader(), Kind: v.kind, Head: head[:n]}, nil
}

// SwitchVersion switches a photo or video waiting to be sent to its other
// version: it uploads that one, drops the one waiting, so its space is
// free at once, and returns the new upload, which takes its place in the
// message.
func (m *Manager) SwitchVersion(ctx context.Context, denID, uploadID string, to Send) (Uploaded, error) {
	if to != SendSmaller && to != SendFull {
		return Uploaded{}, inputError(errors.New("a file goes smaller or full size"))
	}
	c, err := m.find(denID)
	if err != nil {
		return Uploaded{}, err
	}
	if err := checkID("upload", uploadID); err != nil {
		return Uploaded{}, err
	}
	k := c.keptFor(uploadID)
	if k == nil {
		return Uploaded{}, errNotKept
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if c.keptFor(uploadID) != k {
		return Uploaded{}, errNotKept
	}
	if k.sent == to {
		return k.up, nil
	}
	v := k.smaller
	if to == SendFull {
		if !k.fullFits {
			return Uploaded{}, ErrTooLarge
		}
		v = k.full
	}
	up, err := m.uploadVersion(ctx, c, k, v, nil)
	if err != nil {
		return Uploaded{}, err
	}
	c.mu.Lock()
	still := c.kept[uploadID] == k
	if still {
		k.sent = to
		up.Versions = k.describe()
		delete(c.kept, uploadID)
		k.id, k.up = up.ID, up
		c.kept[up.ID] = k
	}
	c.mu.Unlock()
	if !still {
		// It was sent, or taken off the message, meanwhile; what it
		// switched to goes too.
		_ = m.DropUpload(ctx, denID, up.ID)
		return Uploaded{}, errNotKept
	}
	if err := m.DropUpload(ctx, denID, uploadID); err != nil {
		// It waits out the hour instead.
		m.log.Infof("Drop the version a file switched from: %v", err)
	}
	return up, nil
}
