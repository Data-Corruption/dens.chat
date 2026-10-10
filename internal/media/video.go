package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// Smaller copies of videos (M5.4). A video goes as a smaller copy, as a
// photo does, which the media module makes: AV1 at about 0.04 bits a pixel
// each frame, at most 720p and 30 frames a second, in chunks of a few
// seconds that run side by side, each from a keyframe, put together with
// the video's sound as it is. A video over a den's limit gets the bitrate
// that fits it, stepping down to 480p as it falls, and below a floor it's
// too long for the den.

const (
	// CopyVideoSide is a video copy's longer side at most: 720p, since the
	// module makes 1080p more than twice as slowly.
	CopyVideoSide = 1280
	// copySmallSide is where a copy fitted to a den's limit steps down to
	// when its bitrate runs low: 480p.
	copySmallSide = 854
	// CopyFPS is a copy's frame rate at most.
	CopyFPS = 30
	// copyBPP is a copy's bitrate, in bits a pixel each frame: 1.1 Mbps at
	// 720p and 30 frames a second.
	copyBPP = 0.04
	// stepBPP is the least a fitted copy takes at 720p before it steps
	// down, and floorBPP the least at 480p, below which the video is too
	// long for the den.
	stepBPP  = 0.025
	floorBPP = 0.02
	// minKbps keeps a tiny video's bitrate from coming to nothing.
	minKbps = 16
	// fitShare is the share of a den's limit a fitted copy aims for: libaom
	// keeps within a few percent of its bitrate, and the container's index
	// takes some (containerBytes, besides). The page's encoders, in the
	// video spike, kept within a fifth of theirs (pageShare).
	fitShare       = 0.9
	pageShare      = 0.8
	containerBytes = 64 << 10
)

// CopyBy is who makes a video's copy.
type CopyBy int

const (
	// ByModule is the media module, in chunks side by side, up to 720p.
	ByModule CopyBy = iota
	// ByPage is the page, with WebCodecs, up to 1080p (M5.5).
	ByPage
)

// sides are the longer sides a copy steps down through, as the bitrate
// that fits a den's limit falls, and share the part of the limit it aims
// for.
func (by CopyBy) sides() []int {
	if by == ByPage {
		return []int{PageVideoSide, CopyVideoSide, copySmallSide}
	}
	return []int{CopyVideoSide, copySmallSide}
}

func (by CopyBy) share() float64 {
	if by == ByPage {
		return pageShare
	}
	return fitShare
}

// Chunks last at least minChunk and at most maxChunk. A chunk not at a
// keyframe decodes from the one before, which phones put every second or
// so, so a cut goes to the keyframe nearest it within a quarter of a chunk.
const (
	minChunk  = time.Second
	maxChunk  = 10 * time.Second
	leastPart = 500 * time.Millisecond
)

// VideoPlan is how a video's copy is made, and by whom.
type VideoPlan struct {
	By CopyBy
	// Width and Height are the copy's, as the video stores its frames,
	// before its turn.
	Width, Height int
	FPS, KBPS     int
	// Chunks are the module's; the page makes its copy in one go.
	Chunks []ffmpeg.Chunk
	// Estimate is about how large the copy comes out.
	Estimate int64
}

var (
	// ErrNoSmaller is a video whose copy wouldn't come to three quarters of
	// its full size, which goes full size instead, as a photo's does.
	ErrNoSmaller = errors.New("a copy of the video wouldn't be much smaller")
	// ErrNoCopy is a video the module can't make a copy of: one it doesn't
	// decode, such as VP9 or AV1 in a WebM.
	ErrNoCopy = errors.New("the media module can't make a copy of this video")
)

// TooLong is a video too long for a den's limit even as a smaller copy, of
// which about Longest would fit.
type TooLong struct {
	Longest time.Duration
}

func (e *TooLong) Error() string {
	return fmt.Sprintf("the video is too long to fit; about %v of it would", e.Longest)
}

// turned reports whether a turn swaps a video's width and height.
func turned(turn string) bool {
	switch turn {
	case "transpose", "clock", "cclock", "clock_flip":
		return true
	}
	return false
}

// fitEven fits w × h in side on its longer side, never enlarged, in even
// pixels, as the driver's encode does.
func fitEven(w, h, side int) (int, int) {
	k := 1.0
	if m := max(w, h); m > side {
		k = float64(side) / float64(m)
	}
	even := func(n int) int { return max(2, int(math.Round(float64(n)*k/2))*2) }
	return even(w), even(h)
}

// PlanVideoCopy plans the copy of a video whose video stream the module
// probed as v and whose packets it scanned as s, made by by; full is the
// full size's bytes, and workers how many of the module's chunks run at
// once. With fit above 0, the video is over the den's limit, and the copy
// takes the bitrate that fits in fit bytes, or the video is TooLong.
// Otherwise the copy is made at its own bitrate, or not at all when it
// wouldn't come to three quarters of full: ErrNoSmaller. The module makes
// no copy of a video it doesn't decode: ErrNoCopy.
func PlanVideoCopy(v ffmpeg.Stream, s ffmpeg.Scanned, full, fit int64, by CopyBy, workers int) (VideoPlan, error) {
	if by == ByModule && !v.Decoder {
		return VideoPlan{}, ErrNoCopy
	}
	sw, sh := v.Width, v.Height
	if turned(v.Turn) {
		sw, sh = sh, sw
	}
	length := s.EndUS - s.StartUS
	if sw < 1 || sh < 1 || length <= 0 || s.Frames < 1 {
		return VideoPlan{}, ErrNoCopy
	}
	seconds := float64(length) / 1e6
	fps := float64(CopyFPS)
	if s.FPS > 0 {
		fps = min(s.FPS, CopyFPS)
	}
	kbps := func(w, h int, bpp float64) float64 { return bpp * float64(w*h) * fps / 1000 }
	size := func(rate float64) int64 {
		return int64(rate*1000/8*seconds) + s.AudioBytes + containerBytes
	}
	plan := func(w, h int, rate float64) VideoPlan {
		rate = max(rate, minKbps)
		p := VideoPlan{By: by, Width: w, Height: h, FPS: CopyFPS, KBPS: int(rate), Estimate: size(rate)}
		if by == ByModule {
			p.Chunks = planChunks(s, workers, w, h, int(rate))
		}
		return p
	}

	sides := by.sides()
	if fit <= 0 {
		w, h := fitEven(sw, sh, sides[0])
		p := plan(w, h, kbps(w, h, copyBPP))
		if p.Estimate*4 > full*3 {
			return VideoPlan{}, ErrNoSmaller
		}
		return p, nil
	}
	rate := (float64(fit)*by.share() - float64(s.AudioBytes) - containerBytes) * 8 / seconds / 1000
	for i, side := range sides {
		w, h := fitEven(sw, sh, side)
		least := stepBPP
		if i == len(sides)-1 {
			least = floorBPP
		}
		if rate >= kbps(w, h, least) {
			return plan(w, h, min(rate, kbps(w, h, copyBPP))), nil
		}
	}
	// About as long as the floor's bitrate fits, with the sound's.
	w, h := fitEven(sw, sh, copySmallSide)
	bps := kbps(w, h, floorBPP)*1000 + float64(s.AudioBytes)*8/seconds
	longest := time.Duration(max(float64(fit)*by.share()-containerBytes, 0) * 8 / bps * float64(time.Second))
	return VideoPlan{}, &TooLong{Longest: longest.Truncate(time.Second)}
}

// LowerVideoPlan plans a copy again, smaller, for one that came out over its
// limit, at got bytes: its bitrate scaled down to fit, or TooLong below the
// floor at its size.
func LowerVideoPlan(p VideoPlan, s ffmpeg.Scanned, got, fit int64, workers int) (VideoPlan, error) {
	video := float64(got - s.AudioBytes - containerBytes)
	room := float64(fit)*p.By.share() - float64(s.AudioBytes) - containerBytes
	if video <= 0 || room <= 0 {
		return VideoPlan{}, &TooLong{}
	}
	rate := float64(p.KBPS) * room / video
	fps := float64(CopyFPS)
	if s.FPS > 0 {
		fps = min(s.FPS, CopyFPS)
	}
	if rate < floorBPP*float64(p.Width*p.Height)*fps/1000 {
		seconds := float64(s.EndUS-s.StartUS) / 1e6
		return VideoPlan{}, &TooLong{Longest: time.Duration(seconds * room / video * float64(time.Second)).Truncate(time.Second)}
	}
	p.KBPS = max(int(rate), minKbps)
	p.Estimate = int64(float64(p.Estimate) * room / video)
	if p.By == ByModule {
		p.Chunks = planChunks(s, workers, p.Width, p.Height, p.KBPS)
	}
	return p, nil
}

// planChunks splits a video's frames into chunks for workers to make side
// by side, cut at the keyframe nearest each cut where one is near. Each
// chunk starts with a keyframe, which costs its bits, so each worker gets
// as few as keep them within maxChunk: a phone's minute-long video goes in
// one round. A video too short for a minChunk each gets one a minChunk.
// The last chunk runs past the video's end, so its last frame, which may
// say it lasts no time, is in it.
func planChunks(s ffmpeg.Scanned, workers, w, h, kbps int) []ffmpeg.Chunk {
	start, end := s.StartUS, s.EndUS
	length := end - start
	workers = max(workers, 1)
	least, most := minChunk.Microseconds(), maxChunk.Microseconds()
	n := max(1, int(length/least))
	if length/int64(workers) >= least {
		n = workers * max(1, int((length+int64(workers)*most-1)/(int64(workers)*most)))
	}
	target := length / int64(n)
	cuts := []int64{start}
	for k := 1; k < n; k++ {
		at := start + length*int64(k)/int64(n)
		if i, _ := slices.BinarySearch(s.Keyframes, at); len(s.Keyframes) > 0 {
			best := int64(-1)
			for _, j := range []int{i - 1, i} {
				if j >= 0 && j < len(s.Keyframes) && abs(s.Keyframes[j]-at) <= target/4 &&
					(best < 0 || abs(s.Keyframes[j]-at) < abs(best-at)) {
					best = s.Keyframes[j]
				}
			}
			if best >= 0 {
				at = best
			}
		}
		if at-cuts[len(cuts)-1] >= leastPart.Microseconds() && end-at >= leastPart.Microseconds() {
			cuts = append(cuts, at)
		}
	}
	chunks := make([]ffmpeg.Chunk, len(cuts))
	for i, c := range cuts {
		next := end + time.Second.Microseconds()
		if i+1 < len(cuts) {
			next = cuts[i+1]
		}
		chunks[i] = ffmpeg.Chunk{StartUS: c, EndUS: next, MaxSide: max(w, h), FPS: CopyFPS, KBPS: kbps}
	}
	return chunks
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// ChunkFile holds a chunk's packets while a copy is made.
type ChunkFile interface {
	ffmpeg.Output
	Close() error
}

// VideoCopy describes a video's copy, made.
type VideoCopy struct {
	ffmpeg.Muxed
	// Width and Height are the copy's as it shows, after its turn.
	Width, Height int
	DurationMS    int64
}

// MakeVideoCopy makes the copy of full, whose video stream the module
// probed as v, as plan says, and writes it to out: its chunks side by side,
// as many as the Runner's workers, each into a file newFile gives, then put
// together with full's sound. progress, if it's set, gets how much of the
// video is encoded, from 0 to 1, as it goes, one call at a time. What the
// module made is checked as any video a member sends: the copy must be the
// MP4 of AV1 it was asked for, at the size planned.
func MakeVideoCopy(ctx context.Context, m *ffmpeg.Runner, full ffmpeg.Input, v ffmpeg.Stream, plan VideoPlan,
	newFile func() (ChunkFile, error), out ffmpeg.Output, progress func(float64)) (VideoCopy, error) {
	if len(plan.Chunks) == 0 {
		return VideoCopy{}, ErrNoCopy
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	files := make([]ChunkFile, len(plan.Chunks))
	defer func() {
		for _, f := range files {
			if f != nil {
				_ = f.Close()
			}
		}
	}()
	for i := range files {
		f, err := newFile()
		if err != nil {
			return VideoCopy{}, err
		}
		files[i] = f
	}

	// How far each chunk is, in microseconds of the video, against how
	// long they all last: the last one's end is past the video's.
	var mu sync.Mutex
	done := make([]int64, len(plan.Chunks))
	lengths := make([]int64, len(plan.Chunks))
	var total int64
	for i, c := range plan.Chunks {
		lengths[i] = c.EndUS - c.StartUS
		if i == len(plan.Chunks)-1 {
			lengths[i] = max(lengths[i]-time.Second.Microseconds(), 1)
		}
		total += lengths[i]
	}
	frame := time.Second.Microseconds() / CopyFPS
	report := func(i int, us int64) {
		mu.Lock()
		defer mu.Unlock()
		done[i] = min(max(us-plan.Chunks[i].StartUS+frame, 0), lengths[i])
		var sum int64
		for _, d := range done {
			sum += d
		}
		if progress != nil {
			progress(float64(sum) / float64(total))
		}
	}

	results := make([]ffmpeg.Encoded, len(plan.Chunks))
	errs := make([]error, len(plan.Chunks))
	var wg sync.WaitGroup
	for i, c := range plan.Chunks {
		wg.Go(func() {
			// A chunk's packets can't run to more than a file a den takes.
			f := &ffmpeg.Limited{Output: files[i], Max: denproto.MaxFileSize}
			results[i], errs[i] = m.Encode(ctx, full, f, c, func(us int64) { report(i, us) })
			if errs[i] != nil {
				cancel()
			}
		})
	}
	wg.Wait()
	// The first chunk to fail stops the others, which then fail for that.
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return VideoCopy{}, err
		}
	}
	if err := errors.Join(errs...); err != nil {
		return VideoCopy{}, err
	}
	packets, frames := 0, 0
	for _, e := range results {
		if e.Width != plan.Width || e.Height != plan.Height {
			return VideoCopy{}, &ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a chunk at another size than planned"}
		}
		packets += e.Packets
		frames += e.Frames
	}
	if frames == 0 {
		return VideoCopy{}, &ffmpeg.JobError{Err: ffmpeg.ErrUnreadable, Reason: "no frames to copy"}
	}
	if progress != nil {
		progress(1)
	}

	parts := make([]ffmpeg.Input, len(files))
	for i, f := range files {
		parts[i] = f
	}
	muxed, err := m.Mux(ctx, full, newConcat(parts), out, plan.Width, plan.Height)
	if err != nil {
		return VideoCopy{}, err
	}
	if muxed.VideoPackets != packets || muxed.Bytes != out.Size() {
		return VideoCopy{}, &ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a copy that isn't what its chunks made"}
	}
	return checkCopy(ctx, m, v, plan, muxed, out)
}

// checkCopy checks a copy put together as any video a member sends: it
// must be the MP4 of AV1 it was asked for, at the size planned, turned as
// the video is, with its sound.
func checkCopy(ctx context.Context, m *ffmpeg.Runner, v ffmpeg.Stream, plan VideoPlan, muxed ffmpeg.Muxed, out ffmpeg.Output) (VideoCopy, error) {
	p, err := m.Probe(ctx, out)
	if err != nil {
		return VideoCopy{}, err
	}
	w, h := plan.Width, plan.Height
	if turned(v.Turn) {
		w, h = h, w
	}
	c, ok := p.Video()
	audio := 0
	for _, st := range p.Streams {
		if st.Type == "audio" {
			audio++
		}
	}
	if !ok || p.Format != "mov,mp4,m4a,3gp,3g2,mj2" || c.Codec != "av1" || c.Turn != v.Turn || c.Width != w || c.Height != h ||
		audio != muxed.AudioStreams || len(p.Streams) != audio+1 {
		return VideoCopy{}, &ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a copy that isn't the video it was asked for"}
	}
	d := VideoCopy{Muxed: muxed, Width: w, Height: h}
	if p.DurationMS > 0 && p.DurationMS <= denproto.MaxDuration {
		d.DurationMS = p.DurationMS
	}
	return d, nil
}

// concat reads finished files one after another, as one.
type concat struct {
	parts         []ffmpeg.Input
	starts, sizes []int64
	size          int64
}

func newConcat(parts []ffmpeg.Input) *concat {
	c := &concat{parts: parts, starts: make([]int64, len(parts)), sizes: make([]int64, len(parts))}
	for i, p := range parts {
		c.starts[i], c.sizes[i] = c.size, p.Size()
		c.size += c.sizes[i]
	}
	return c
}

func (c *concat) Size() int64 { return c.size }

func (c *concat) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	n := 0
	for i, part := range c.parts {
		at := off + int64(n) - c.starts[i]
		if n == len(p) {
			break
		}
		if at >= c.sizes[i] {
			continue
		}
		want := p[n:min(len(p), n+int(c.sizes[i]-at))]
		k, err := part.ReadAt(want, at)
		n += k
		if k < len(want) {
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return n, err
		}
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
