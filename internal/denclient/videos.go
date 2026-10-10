package denclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Smaller copies of videos (M5.4). A video added to a message goes as a
// smaller copy, as a photo does, unless its sender sends it full size: this
// service strips it, which is its full size, and makes its copy with the
// media module, in chunks side by side (media.MakeVideoCopy), with a
// preview from the full size, which a den can't make from the copy's AV1.
// Both versions are kept until the message goes, as a photo's are. A copy
// takes a while, so the page follows the upload by a key of its own, for
// how far it has come, and may ask for the video full size instead.

// Progress is how far an upload the page follows has come: Done, from 0 to
// 1, of its Stage, "preparing" (stripping), "copying" (making a video's
// smaller copy) or "sending" (to the den).
type Progress struct {
	Stage string  `json:"stage"`
	Done  float64 `json:"done"`
}

// following is an upload the page follows.
type following struct {
	den string
	mu  sync.Mutex
	p   Progress
	// fullFits says the video's full size fits the den, which the page may
	// then send instead of waiting for its copy; full is closed when it
	// asks.
	fullFits bool
	full     chan struct{}
	once     sync.Once
	// page says the page makes a video's copy itself when it can, copy is
	// the one on offer to it, and socket says a page follows the upload
	// (M5.5). closed is closed when the upload ends.
	page   bool
	copy   *pageCopy
	socket bool
	closed chan struct{}
	// changed wakes the page's socket when the progress changes.
	changed chan struct{}
}

// pageCopies says the page makes a video's copy itself when it can.
func (f *following) pageCopies() bool { return f != nil && f.page }

func (f *following) set(stage string, done float64) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.p = Progress{Stage: stage, Done: min(max(done, 0), 1)}
	f.mu.Unlock()
	select {
	case f.changed <- struct{}{}:
	default:
	}
}

func (f *following) sending(done, total int64) {
	if total > 0 {
		f.set("sending", float64(done)/float64(total))
	}
}

// fits says whether the page may ask for the full size, which it may once
// it's known to fit.
func (f *following) fits(ok bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.fullFits = ok
	f.mu.Unlock()
}

// wantsFull is closed when the page asks for the full size instead.
func (f *following) wantsFull() <-chan struct{} {
	if f == nil {
		return nil
	}
	return f.full
}

// progressKey is what the page names an upload it follows by.
var progressKey = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// follow starts following an upload by the page's key, and returns how to
// stop. page says the page makes a video's copy itself when it can.
func (m *Manager) follow(denID, key string, page bool) (*following, func(), error) {
	if !progressKey.MatchString(key) {
		return nil, nil, inputError(errors.New("an upload's progress key is 8 to 64 letters, digits, - or _"))
	}
	f := &following{den: denID, p: Progress{Stage: "preparing"}, full: make(chan struct{}), page: page, closed: make(chan struct{}),
		changed: make(chan struct{}, 1)}
	m.followMu.Lock()
	defer m.followMu.Unlock()
	if _, ok := m.follows[key]; ok {
		return nil, nil, inputError(errors.New("an upload already goes by that progress key"))
	}
	m.follows[key] = f
	var once sync.Once
	return f, func() {
		once.Do(func() {
			m.followMu.Lock()
			delete(m.follows, key)
			m.followMu.Unlock()
			close(f.closed)
		})
	}, nil
}

func (m *Manager) followed(denID, key string) (*following, error) {
	m.followMu.Lock()
	f, ok := m.follows[key]
	m.followMu.Unlock()
	if !ok || f.den != denID {
		return nil, &denproto.Error{Status: http.StatusNotFound, Code: denproto.CodeNotFound, Message: "no such upload under way"}
	}
	return f, nil
}

// UploadProgress reports how far an upload the page follows has come.
func (m *Manager) UploadProgress(denID, key string) (Progress, error) {
	f, err := m.followed(denID, key)
	if err != nil {
		return Progress{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.p, nil
}

// SendFullSize has an upload that's making a video's copy stop, and send
// the video full size instead, which it does when that fits the den.
func (m *Manager) SendFullSize(denID, key string) error {
	f, err := m.followed(denID, key)
	if err != nil {
		return err
	}
	f.mu.Lock()
	ok := f.fullFits
	f.mu.Unlock()
	if !ok {
		return ErrTooLarge
	}
	f.once.Do(func() { close(f.full) })
	return nil
}

// uploadVideo uploads a video for a message with its two versions: full
// size, the video stripped, and its smaller copy, made unless the member
// sends it full size and that fits. Over the den's limit, the copy is
// fitted to it. A video the module makes no copy of goes full size when
// that fits, and is refused otherwise.
func (m *Manager) uploadVideo(ctx context.Context, c *conn, channelID, name string, size int64, body io.Reader, send Send,
	replaces string, f *following) (Uploaded, error) {
	if size > denproto.MaxFileSize {
		return Uploaded{}, ErrTooLarge
	}
	full, err := m.stripVideo(ctx, body)
	if err != nil {
		return Uploaded{}, err
	}
	k := &kept{name: name, channel: channelID, replaces: replaces, full: full, stripped: true}
	plain := func() (Uploaded, error) {
		defer k.close()
		return m.uploadVersion(ctx, c, k, k.full, f)
	}
	// Audio in a video's container goes as audio does.
	if full.video.Width == 0 {
		return plain()
	}

	limits := c.limits()
	fit := limits.FileSize
	if forDM(c, channelID) {
		fit = dmFit(limits)
	}
	k.fullFits = fit <= 0 || full.size() <= fit
	f.fits(k.fullFits)
	if k.fullFits && send == SendFull {
		// A copy takes minutes to make, for nothing.
		return plain()
	}
	over := int64(0)
	if !k.fullFits {
		over = fit
	}

	// The copy stops when the page asks for the full size instead.
	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-f.wantsFull():
			cancel()
		case <-copyCtx.Done():
		}
	}()
	k.smaller, k.poster, err = m.videoCopy(copyCtx, full, over, f)
	switch {
	case ctx.Err() != nil:
		k.close()
		return Uploaded{}, ctx.Err()
	case err == nil:
	case !k.fullFits:
		k.close()
		var job *ffmpeg.JobError
		if errors.As(err, &job) {
			m.log.Warnf("The media module failed on a video's copy: %s", job.Reason)
		}
		return Uploaded{}, tooLargeVideo(err, limits.FileSize)
	case errors.Is(err, context.Canceled), errors.Is(err, media.ErrNoSmaller), errors.Is(err, media.ErrNoCopy):
		return plain()
	default:
		m.log.Infof("A video goes without a smaller copy: %v", err)
		return plain()
	}
	// A copy goes when it's at most three quarters of the full size, as a
	// photo's does, or when it's what fits.
	if k.fullFits && k.smaller.size()*4 > full.size()*3 {
		k.smaller.close()
		k.smaller = nil
		return plain()
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

// forDM says whether channelID names one of the den's DMs, whose files go
// sealed.
func forDM(c *conn, channelID string) bool {
	if channelID == "" {
		return false
	}
	_, ok := c.isDM(channelID)
	return ok
}

// stripVideo spools a video into a scratch file and strips it, as uploading
// it full size does, into its full-size version.
func (m *Manager) stripVideo(ctx context.Context, body io.Reader) (*version, error) {
	in, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	n, err := io.Copy(io.NewOffsetWriter(in, 0), io.LimitReader(body, denproto.MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if n > denproto.MaxFileSize {
		return nil, ErrTooLarge
	}
	full, err := m.newVersion(media.Video)
	if err != nil {
		return nil, err
	}
	out := &ffmpeg.Limited{Output: full.file, Max: denproto.MaxFileSize}
	st, err := media.StripMedia(ctx, m.Media, in, out)
	if err != nil {
		full.close()
		if out.Over() {
			return nil, ErrTooLarge
		}
		return nil, m.mediaError(err)
	}
	full.video = &st
	full.width, full.height = st.Width, st.Height
	return full, nil
}

// videoCopy makes the smaller copy of a video's full size, fitted to over
// bytes when the full size is over the den's limit, and the copy's preview
// from the full size. The page makes the copy when it can (M5.5), and the
// module otherwise.
func (m *Manager) videoCopy(ctx context.Context, full *version, over int64, f *following) (*version, *media.Thumb, error) {
	p, err := m.Media.Probe(ctx, full.file)
	if err != nil {
		return nil, nil, err
	}
	v, ok := p.Video()
	if !ok {
		return nil, nil, media.ErrNoCopy
	}
	s, err := m.Media.Scan(ctx, full.file)
	if err != nil {
		return nil, nil, err
	}
	full.fps = s.FPS
	var copied *version
	if f.pageCopies() {
		if copied, err = m.copyByPage(ctx, full, v, s, over, f); err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			m.log.Infof("The page made no copy of a video: %v", err)
		}
	}
	if copied == nil {
		if copied, err = m.copyByModule(ctx, full, v, s, over, f); err != nil {
			return nil, nil, err
		}
	}
	th, err := media.Poster(ctx, m.Media, full.file)
	if err != nil {
		if ctx.Err() != nil {
			copied.close()
			return nil, nil, ctx.Err()
		}
		// The page draws one from the copy instead.
		m.log.Infof("A video's copy has no preview: %v", err)
		return copied, nil, nil
	}
	return copied, &th, nil
}

// copyByModule has the media module make a video's copy, in chunks side by
// side. A copy that comes out over the den's limit is made once more,
// lower.
func (m *Manager) copyByModule(ctx context.Context, full *version, v ffmpeg.Stream, s ffmpeg.Scanned, over int64, f *following) (*version, error) {
	workers := m.Media.Workers()
	plan, err := media.PlanVideoCopy(v, s, full.size(), over, media.ByModule, workers)
	if err != nil {
		return nil, err
	}
	f.set("copying", 0)
	newChunk := func() (media.ChunkFile, error) { return vault.NewScratch(m.TempDir, scratchPattern) }
	for attempt := 0; ; attempt++ {
		out, err := m.newVersion(media.Video)
		if err != nil {
			return nil, err
		}
		limited := &ffmpeg.Limited{Output: out.file, Max: denproto.MaxFileSize}
		c, err := media.MakeVideoCopy(ctx, m.Media, full.file, v, plan, newChunk, limited, func(done float64) {
			f.set("copying", done)
		})
		if err != nil {
			out.close()
			if limited.Over() {
				return nil, ErrTooLarge
			}
			return nil, err
		}
		if over <= 0 || out.size() <= over {
			out.describeCopy(c)
			return out, nil
		}
		got := out.size()
		out.close()
		if attempt > 0 {
			return nil, ErrTooLarge
		}
		if plan, err = media.LowerVideoPlan(plan, s, got, over, workers); err != nil {
			return nil, err
		}
	}
}

// copyByPage has the page make a video's copy (M5.5): the video's packets
// demuxed for it, offered with what decoding them takes and the plan, and
// the AV1 packets it sends back put together with the video's sound. A
// copy that comes out over the den's limit is offered once more, lower.
func (m *Manager) copyByPage(ctx context.Context, full *version, v ffmpeg.Stream, s ffmpeg.Scanned, over int64, f *following) (*version, error) {
	plan, err := media.PlanVideoCopy(v, s, full.size(), over, media.ByPage, 0)
	if err != nil {
		return nil, err
	}
	packets, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	defer packets.Close()
	d, err := m.Media.Demux(ctx, full.file, &ffmpeg.Limited{Output: packets, Max: denproto.MaxFileSize})
	if err != nil {
		return nil, err
	}
	dec, err := media.PageDecoding(d)
	if err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		offer := Offer{ID: attempt, Decoding: dec, Encoding: media.PageEncodingFor(plan, s), Packets: d.Packets, EndUS: s.EndUS}
		got, err := m.pageMakes(ctx, f, offer, packets)
		if err != nil {
			return nil, err
		}
		out, err := m.newVersion(media.Video)
		if err != nil {
			got.Close()
			return nil, err
		}
		limited := &ffmpeg.Limited{Output: out.file, Max: denproto.MaxFileSize}
		c, err := media.MuxPageCopy(ctx, m.Media, full.file, v, plan, got, limited)
		got.Close()
		if err != nil {
			out.close()
			if limited.Over() {
				return nil, ErrTooLarge
			}
			return nil, err
		}
		if over <= 0 || out.size() <= over {
			out.describeCopy(c)
			return out, nil
		}
		size := out.size()
		out.close()
		if attempt > 1 {
			return nil, ErrTooLarge
		}
		if plan, err = media.LowerVideoPlan(plan, s, size, over, 0); err != nil {
			return nil, err
		}
	}
}

// describeCopy says what a version made as a video's copy is: an MP4 of
// AV1, as it shows, at its frame rate.
func (v *version) describeCopy(c media.VideoCopy) {
	v.video = &media.Stripped{MIME: "video/mp4", Width: c.Width, Height: c.Height, DurationMS: c.DurationMS}
	v.width, v.height = c.Width, c.Height
	if c.DurationUS > 0 {
		v.fps = float64(c.VideoPackets) / (float64(c.DurationUS) / 1e6)
	}
}

// tooLargeVideo explains why a video over a den's limit of limit bytes
// isn't sent: it's too long for a copy that fits, or there's no copy.
func tooLargeVideo(err error, limit int64) error {
	var long *media.TooLong
	switch {
	case errors.As(err, &long):
		return inputError(fmt.Errorf("This video is too long for this den's %s limit, even as a smaller copy. %s of it would fit.",
			sizeText(limit), durationText(long.Longest)))
	case errors.Is(err, media.ErrNoCopy):
		return inputError(fmt.Errorf("This video is larger than this den's %s limit, and Dens can't make a smaller copy of it.",
			sizeText(limit)))
	case errors.Is(err, ErrTooLarge):
		return ErrTooLarge
	}
	var job *ffmpeg.JobError
	if errors.As(err, &job) {
		return inputError(fmt.Errorf("This video is larger than this den's %s limit, and Dens couldn't make a smaller copy of it. "+
			"It may be damaged.", sizeText(limit)))
	}
	return err
}

// sizeText writes a size as the page does: 25 MB.
func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.3g GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.3g MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n>>10)
}

// durationText writes about how long something lasts: "About 7 minutes",
// "About 40 seconds", or "Less than a second".
func durationText(d time.Duration) string {
	switch {
	case d >= 2*time.Minute:
		return fmt.Sprintf("About %d minutes", int(d.Minutes()))
	case d >= 2*time.Second:
		return fmt.Sprintf("About %d seconds", int(d.Seconds()))
	}
	return "Less than a second"
}

// uploadVersion uploads one of a file's versions for the message it waits
// for, reporting to f how much has gone: a photo's as any image goes, and a
// video's as the media module made it, with the copy's preview.
func (m *Manager) uploadVersion(ctx context.Context, c *conn, k *kept, v *version, f *following) (Uploaded, error) {
	if v.video == nil {
		up, err := m.uploadTo(ctx, c, k.channel, k.name, v.size(), v.reader(), k.replaces)
		if err == nil {
			up.Stripped, up.Converted = k.stripped, k.converted
		}
		return up, err
	}
	p := &prepared{out: v.file, kind: media.Video, name: k.name, stripped: *v.video}
	if v == k.smaller {
		p.poster = k.poster
	}
	if f != nil {
		p.sending = f.sending
	}
	return m.uploadReady(ctx, c, k.channel, p, k.replaces)
}
