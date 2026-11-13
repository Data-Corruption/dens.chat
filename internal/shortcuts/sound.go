package shortcuts

import (
	"context"
	"encoding/binary"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
)

// The computer's sound (M20.2). While Relay shares it with another of the
// member's devices, the service's state asks the helper to read it, and
// the helper sends it on a request of its own, control.OpSound, in
// pieces, a frame each: the rate, 4 bytes little-endian, then PieceFrames
// frames of 16-bit stereo, little-endian and interleaved. The helper cuts
// what the system gives into pieces, and fills with silence where it
// gives nothing, as an output with nothing playing does on Windows, so
// the stream never stops for whoever listens. In the Flatpak the request
// is a pipe inside the service, as the helper's stream is.

const (
	// PieceFrames is how many frames a piece holds: 5 ms at 48 kHz.
	PieceFrames = 240
	// PieceSize is a piece's bytes.
	PieceSize = 4 + PieceFrames*4
	// The rates a piece may name.
	minRate, maxRate = 8000, 192000
	// soundIdle is how long the service waits for a piece, which come
	// every 5 ms while the helper reads, before it gives up on the stream.
	soundIdle = 5 * time.Second
	// A device that gave nothing for fillAfter gets silence in its place,
	// up to fillUpTo ago, and again every fillEvery until it gives: in
	// bursts, a small buffer at the other end would run dry between them,
	// and again as the sound came back. One a second behind stood still,
	// as a computer asleep, and its time is skipped, not filled.
	fillAfter = 60 * time.Millisecond
	fillUpTo  = 10 * time.Millisecond
	fillEvery = 10 * time.Millisecond
	// problemAfter is how long a problem reading the sound lasts before
	// the helper says so: one comes for a moment as the default output
	// changes.
	problemAfter = 2 * time.Second
	// soundRetry is how long the helper waits to open the request again.
	soundRetry = time.Second
	// soundQueue bounds the pieces waiting to be sent: live sound queued
	// is sound late.
	soundQueue = 64
)

// PieceRate is the rate a piece names, or 0 for one malformed.
func PieceRate(piece []byte) int {
	if len(piece) != PieceSize {
		return 0
	}
	rate := int(binary.LittleEndian.Uint32(piece))
	if rate < minRate || rate > maxRate {
		return 0
	}
	return rate
}

// soundCut cuts what the system gives into pieces, handing each to out,
// and fills with silence where it gives nothing.
type soundCut struct {
	now func() time.Duration
	out func(piece []byte)

	mu   sync.Mutex
	rate int
	// base is when the stream started on the helper's clock, and total
	// the frames since, the system's and the silence's.
	base    time.Duration
	total   int64
	pending []int16
	// filling is the system giving nothing, since it last did.
	filling bool
}

func (c *soundCut) span(frames int64) time.Duration {
	return time.Duration(frames) * time.Second / time.Duration(c.rate)
}

// data takes what the system gave.
func (c *soundCut) data(pcm []int16, rate int) {
	at := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.filling = false
	if rate != c.rate {
		c.rate, c.base, c.total, c.pending = rate, at, 0, c.pending[:0]
	}
	pcm = pcm[:len(pcm)&^1]
	c.total += int64(len(pcm) / 2)
	// A system that ran ahead of the clock, as after a stall, moves the
	// start back, so its sound is never owed as silence.
	if ahead := c.span(c.total) - (at - c.base); ahead > 200*time.Millisecond {
		c.base = at - c.span(c.total)
	}
	c.pending = append(c.pending, pcm...)
	c.cut()
}

// fill gives silence for what the system owes once it has gone quiet, and
// is called every fillEvery.
func (c *soundCut) fill() {
	at := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate == 0 {
		// Nothing read yet: silence at the usual rate until something is.
		c.rate, c.base = 48000, at
	}
	owed := int64((at-c.base).Seconds()*float64(c.rate)) - c.total
	switch {
	case owed > int64(c.rate):
		c.base = at - c.span(c.total)
	case c.filling || c.span(owed) > fillAfter:
		c.filling = true
		if n := owed - int64(fillUpTo.Seconds()*float64(c.rate)); n > 0 {
			c.total += n
			c.pending = append(c.pending, make([]int16, 2*n)...)
			c.cut()
		}
	}
}

func (c *soundCut) cut() {
	n := 0
	for ; len(c.pending)-n >= 2*PieceFrames; n += 2 * PieceFrames {
		piece := make([]byte, PieceSize)
		binary.LittleEndian.PutUint32(piece, uint32(c.rate))
		for i, v := range c.pending[n : n+2*PieceFrames] {
			binary.LittleEndian.PutUint16(piece[4+2*i:], uint16(v))
		}
		c.out(piece)
	}
	c.pending = append(c.pending[:0], c.pending[n:]...)
}

// ServeSound is the stream of the helper's sound request: each piece goes
// to Relay while it shares the sound. One stream at a time: a newer one
// replaces the last.
func (h *Hub) ServeSound(ctx context.Context, s *control.Stream) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	context.AfterFunc(ctx, func() { _ = s.Close() })
	h.mu.Lock()
	old := h.soundStream
	h.soundStream = s
	h.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	defer func() {
		h.mu.Lock()
		if h.soundStream == s {
			h.soundStream = nil
		}
		h.mu.Unlock()
	}()
	for {
		piece, err := s.RecvFrame(soundIdle)
		if err != nil {
			return err
		}
		if PieceRate(piece) == 0 {
			continue
		}
		h.mu.Lock()
		take := h.sound
		h.mu.Unlock()
		if take != nil {
			take(piece)
		}
	}
}

// soundFor starts reading the computer's sound for an instance as its
// state asks for it, and stops as it no longer does.
func (h *Helper) soundFor(ctx context.Context, r *remote, want bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case want && r.sound == nil && h.OpenSound != nil && h.Capture != nil:
		sctx, cancel := context.WithCancel(ctx)
		r.sound = cancel
		go h.readSound(sctx, r)
	case !want && r.sound != nil:
		r.sound()
		r.sound = nil
	}
}

// readSound reads the computer's sound for an instance until ctx ends,
// sending it on the sound request, which it opens again as it fails, and
// tells the instance what keeps it from reading.
func (h *Helper) readSound(ctx context.Context, r *remote) {
	var mu sync.Mutex
	latest, said, since := "", "", time.Now()
	problem := func(why string) {
		mu.Lock()
		if why != latest {
			latest, since = why, time.Now()
		}
		mu.Unlock()
	}
	// tell says the latest problem once it has lasted, and at once that
	// there's none.
	tell := func() {
		mu.Lock()
		why, ok := latest, latest != said && (latest == "" || time.Since(since) >= problemAfter)
		if ok {
			said = why
		}
		mu.Unlock()
		if ok && r.stream.Send(msgSound, soundNote{Problem: why}) != nil {
			_ = r.stream.Close()
		}
	}
	start := time.Now()
	for ctx.Err() == nil {
		s, err := h.OpenSound(r.name)
		if err != nil {
			problem("the service didn't take the sound")
			tell()
		} else {
			h.sendSound(ctx, s, start, problem, tell)
			_ = s.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(soundRetry):
		}
	}
}

// sendSound reads the sound and sends it on s until ctx ends or s fails.
func (h *Helper) sendSound(ctx context.Context, s *control.Stream, start time.Time, problem func(string), tell func()) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pieces := make(chan []byte, soundQueue)
	cut := &soundCut{now: func() time.Duration { return time.Since(start) }, out: func(p []byte) {
		select {
		case pieces <- p:
		default:
		}
	}}
	go func() {
		if err := h.Capture(ctx, cut.data, problem); err != nil && ctx.Err() == nil {
			problem(err.Error())
		}
	}()
	tick := time.NewTicker(fillEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			cut.fill()
			tell()
		case p := <-pieces:
			if s.SendFrame(p) != nil {
				return
			}
		}
	}
}
