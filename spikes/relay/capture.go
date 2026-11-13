package main

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// What every system's capture shares: the log of what the device gave each
// second, and the silence the probe makes when the device gives nothing,
// so the stream never stops for a listener.

type captureOptions struct {
	mic      bool // the default microphone, in place of the output
	source   string
	keepWarm bool
	convert  bool
}

// A device that has given nothing for this long gets silence in its
// place, up to a moment ago.
const (
	fillAfter = 60 * time.Millisecond
	fillUpTo  = 10 * time.Millisecond
)

type capSink struct {
	emit func(Chunk)

	mu    sync.Mutex
	rate  int
	base  time.Duration // the stream's start on this probe's clock
	total int64         // frames since then, the device's and the probe's
	desc  string

	// The second being counted.
	frames, filled, chunks int
	lastData               time.Duration
	maxGap                 time.Duration
	sumSq                  float64
	peak                   int
	gapsOver               int
	seconds                int
}

func newCapSink(emit func(Chunk)) *capSink { return &capSink{emit: emit} }

// event logs something that happened to the capture.
func (s *capSink) event(format string, args ...any) { logf("cap", "event "+format, args...) }

// describe sets what the next second's line says of the device.
func (s *capSink) describe(format string, args ...any) {
	s.mu.Lock()
	s.desc = fmt.Sprintf(format, args...)
	s.mu.Unlock()
}

// data takes what the device gave.
func (s *capSink) data(frames []int16, rate int) {
	at := now()
	s.mu.Lock()
	s.restart(rate, at)
	n := len(frames) / 2
	s.total += int64(n)
	// A device that ran ahead of the clock, as after a stall, moves the
	// start back, so the probe never counts its sound as owed silence.
	if ahead := time.Duration(s.total)*time.Second/time.Duration(rate) - (at - s.base); ahead > 200*time.Millisecond {
		s.base = at - time.Duration(s.total)*time.Second/time.Duration(rate)
	}
	s.frames += n
	s.chunks++
	if s.lastData != 0 {
		gap := at - s.lastData
		s.maxGap = max(s.maxGap, gap)
		if gap > fillAfter {
			s.gapsOver++
		}
	}
	s.lastData = at
	for _, v := range frames {
		a := int(v)
		if a < 0 {
			a = -a
		}
		s.peak = max(s.peak, a)
		s.sumSq += float64(v) * float64(v)
	}
	s.mu.Unlock()
	s.emit(Chunk{Frames: frames, Rate: rate, At: at})
}

func (s *capSink) restart(rate int, at time.Duration) {
	if rate != s.rate {
		s.rate, s.base, s.total = rate, at, 0
	}
}

// run fills and logs until the context ends.
func (s *capSink) run(ctx context.Context) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	next := now() + time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		at := now()
		s.mu.Lock()
		if s.rate == 0 {
			// No device yet: silence at the usual rate until one shows.
			s.restart(48000, at)
		}
		rate := s.rate
		owed := int64((at-s.base).Seconds()*float64(rate)) - s.total
		var fill []int16
		if owed > int64(rate) {
			// The probe itself stood still, as a computer asleep does:
			// that time is skipped, not filled.
			s.base = at - time.Duration(s.total)*time.Second/time.Duration(rate)
		} else if time.Duration(owed)*time.Second/time.Duration(rate) > fillAfter {
			n := owed - int64(fillUpTo.Seconds()*float64(rate))
			fill = make([]int16, n*2)
			s.total += n
			s.filled += int(n)
		}
		var line string
		if at >= next {
			next += time.Second
			s.seconds++
			rms := math.Inf(-1)
			if s.frames > 0 {
				rms = 20 * math.Log10(math.Sqrt(s.sumSq/float64(s.frames*2))/32768)
			}
			line = fmt.Sprintf("rate=%d frames=%d filled=%d chunks=%d gap-max=%s rms=%.1fdB peak=%.3f %s",
				rate, s.frames, s.filled, s.chunks, ms(s.maxGap), rms, float64(s.peak)/32768, s.desc)
			s.frames, s.filled, s.chunks, s.maxGap, s.sumSq, s.peak = 0, 0, 0, 0, 0, 0
		}
		s.mu.Unlock()
		if fill != nil {
			s.emit(Chunk{Frames: fill, Rate: rate, At: at - fillUpTo, Filled: true})
		}
		if line != "" {
			logf("cap", "%s", line)
		}
	}
}

// clickFinder listens for clicks in a stream: sound that starts after a
// third of a second of quiet and turns loud within 10 ms. The click's time
// is its first frame over the quiet level, since a burst takes a few
// frames to rise.
type clickFinder struct {
	name    string
	found   func(at time.Duration)
	quiet   int
	rate    int
	armed   int // frames left for a start to turn loud
	armedAt time.Duration
}

const (
	clickLoud  = 4900 // about 0.15 of full scale
	clickQuiet = 2600 // about 0.08
)

func (f *clickFinder) feed(c Chunk) {
	if c.Rate != f.rate {
		f.rate, f.quiet, f.armed = c.Rate, 0, 0
	}
	n := len(c.Frames) / 2
	for i := 0; i < n; i++ {
		a := int(c.Frames[2*i])
		if a < 0 {
			a = -a
		}
		if b := int(c.Frames[2*i+1]); b > a {
			a = b
		} else if -b > a {
			a = -b
		}
		if a > clickQuiet {
			if f.quiet > f.rate*3/10 {
				f.armed = f.rate / 100
				f.armedAt = c.At - time.Duration(n-i)*time.Second/time.Duration(f.rate)
			}
			f.quiet = 0
		} else {
			f.quiet++
		}
		if f.armed > 0 {
			f.armed--
			if a > clickLoud {
				f.armed = 0
				f.found(f.armedAt)
			}
		}
	}
}

// sourceClicks finds the clicks in what this side sends, and tells the
// other side when each was captured.
func sourceClicks(s *Sound) *clickFinder {
	return &clickFinder{name: "src", found: func(at time.Duration) {
		logf("click", "captured at=%.4f", at.Seconds())
		s.sourceClick(at)
	}}
}

// outputClicks finds the clicks coming out of this side's output, and
// times each from the source's capture of it.
func outputClicks(s *Sound) *clickFinder {
	return &clickFinder{name: "out", found: func(at time.Duration) {
		if d, ok := s.heardClick(at); ok {
			logf("click", "played at=%.4f delay=%s from the source's capture to this output's", at.Seconds(), ms(d))
		} else {
			logf("click", "played at=%.4f with no click from the source to match", at.Seconds())
		}
	}}
}
