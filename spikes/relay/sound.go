package main

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// Sound goes across uncompressed: 16-bit stereo at the source's rate, in
// packets of 240 frames (5 ms at 48 kHz, 960 bytes), each under a header
// that numbers it and says where it sits in the stream.

// packetFrames is how many frames a packet holds: 240 unless -frames says
// otherwise, to see what fewer, larger packets cost and save.
var packetFrames = 240

const (
	headerSize = 24
	// A sender whose channel holds more than this unsent drops the packet:
	// live sound queued is sound late.
	maxQueued = 32 * 1024
)

// Chunk is sound as a source hands it over: interleaved stereo.
type Chunk struct {
	Frames []int16
	Rate   int
	At     time.Duration // this probe's clock as the chunk's last frame arrived
	Filled bool          // silence the probe made, the device having given nothing
}

type Sound struct {
	player *Player
	local  bool

	mu      sync.Mutex
	link    *Link
	click   func(time.Duration)
	pending []int16
	rate    int
	seq     uint32
	pos     uint64
	tx      txStats
	rx      rxStats
	clicks  []time.Duration // the other side's clicks, on this clock
}

type txStats struct {
	packets, dropped, failed int
	maxQueued                uint64
}

type rxStats struct {
	started            bool
	highest            uint32
	packets            int
	lost, late, dup    int
	last               time.Duration
	maxGap             time.Duration
	base               time.Duration // the shortest transit seen lately
	prevBase           time.Duration
	over5, over10      int
	over20, over50     int
	maxLate            time.Duration
	rate               int
	bytes              int
	totalLost, totalRx int
}

func newSound(p *Player) *Sound { return &Sound{player: p} }

func (s *Sound) attach(l *Link, click func(time.Duration)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.link, s.click = l, click
	s.pending = s.pending[:0]
	s.rx = rxStats{}
}

func (s *Sound) detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.link, s.click = nil, nil
}

// feed takes a source's sound and sends it, cut into packets.
func (s *Sound) feed(c Chunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.link == nil && !(s.local && s.player != nil) {
		return
	}
	if c.Rate != s.rate {
		s.rate = c.Rate
		s.pending = s.pending[:0]
	}
	s.pending = append(s.pending, c.Frames...)
	for len(s.pending) >= packetFrames*2 {
		pkt := make([]byte, headerSize+packetFrames*4)
		binary.LittleEndian.PutUint32(pkt[0:], s.seq)
		binary.LittleEndian.PutUint32(pkt[4:], uint32(s.rate))
		binary.LittleEndian.PutUint64(pkt[8:], s.pos)
		binary.LittleEndian.PutUint64(pkt[16:], uint64(now()))
		for i := 0; i < packetFrames*2; i++ {
			binary.LittleEndian.PutUint16(pkt[headerSize+2*i:], uint16(s.pending[i]))
		}
		s.pending = s.pending[packetFrames*2:]
		s.seq++
		s.pos += uint64(packetFrames)
		if s.link == nil {
			s.player.play(pkt)
			continue
		}
		if s.link.track != nil {
			if err := s.link.sendSound(pkt, s.pos); err != nil {
				s.tx.failed++
				continue
			}
			s.tx.packets++
			continue
		}
		queued := s.link.pcm.BufferedAmount()
		s.tx.maxQueued = max(s.tx.maxQueued, queued)
		if queued > maxQueued {
			s.tx.dropped++
			continue
		}
		if err := s.link.pcm.Send(pkt); err != nil {
			s.tx.failed++
			continue
		}
		s.tx.packets++
	}
}

// sourceClick tells the other side a click went by in what this side
// captured, so it can time the click coming out of its own output.
func (s *Sound) sourceClick(at time.Duration) {
	s.mu.Lock()
	click := s.click
	s.mu.Unlock()
	if click != nil {
		click(at)
	}
}

func (s *Sound) peerClick(at time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clicks = append(s.clicks, at)
	if len(s.clicks) > 8 {
		s.clicks = s.clicks[1:]
	}
}

// heardClick matches a click this side's output played to the latest the
// source captured before it.
func (s *Sound) heardClick(at time.Duration) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.clicks) - 1; i >= 0; i-- {
		if d := at - s.clicks[i]; d > 0 && d < 1500*time.Millisecond {
			return d, true
		}
	}
	return 0, false
}

// receive counts a packet and hands it to the page.
func (s *Sound) receive(pkt []byte) {
	if len(pkt) < headerSize {
		return
	}
	at := now()
	seq := binary.LittleEndian.Uint32(pkt[0:])
	sent := time.Duration(binary.LittleEndian.Uint64(pkt[16:]))
	s.mu.Lock()
	r := &s.rx
	r.rate = int(binary.LittleEndian.Uint32(pkt[4:]))
	r.packets++
	r.totalRx++
	r.bytes += len(pkt)
	switch {
	case !r.started:
		r.started, r.highest = true, seq
	case seq == r.highest:
		r.dup++
	case int32(seq-r.highest) > 0:
		gap := int(seq - r.highest - 1)
		r.lost += gap
		r.totalLost += gap
		r.highest = seq
	default:
		// One counted as lost came after all.
		r.late++
		r.lost--
		r.totalLost--
	}
	if r.last != 0 {
		r.maxGap = max(r.maxGap, at-r.last)
	}
	r.last = at
	// How late a packet is, against the quickest lately: the two clocks
	// differ by a constant over so short a time.
	transit := at - sent
	if r.base == 0 || transit < r.base {
		r.base = transit
	}
	base := r.base
	if r.prevBase != 0 && r.prevBase < base {
		base = r.prevBase
	}
	lateBy := transit - base
	r.maxLate = max(r.maxLate, lateBy)
	if lateBy > 5*time.Millisecond {
		r.over5++
	}
	if lateBy > 10*time.Millisecond {
		r.over10++
	}
	if lateBy > 20*time.Millisecond {
		r.over20++
	}
	if lateBy > 50*time.Millisecond {
		r.over50++
	}
	s.mu.Unlock()
	if s.player != nil {
		s.player.play(pkt)
	}
}

// report logs the last ten seconds of what was sent and what arrived.
func (s *Sound) report() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tx; t.packets+t.dropped+t.failed > 0 {
		logf("sound", "tx packets=%d dropped=%d failed=%d queued-max=%dB rate=%d", t.packets, t.dropped, t.failed, t.maxQueued, s.rate)
		s.tx = txStats{}
	}
	if r := &s.rx; r.packets > 0 {
		logf("sound", "rx packets=%d lost=%d late=%d dup=%d gap-max=%s over5=%d over10=%d over20=%d over50=%d late-max=%s kbps=%d rate=%d total=%d total-lost=%d",
			r.packets, r.lost, r.late, r.dup, ms(r.maxGap), r.over5, r.over10, r.over20, r.over50, ms(r.maxLate),
			r.bytes*8/10000, r.rate, r.totalRx, r.totalLost)
		r.packets, r.lost, r.late, r.dup, r.bytes = 0, 0, 0, 0, 0
		r.maxGap, r.maxLate = 0, 0
		r.over5, r.over10, r.over20, r.over50 = 0, 0, 0, 0
		r.prevBase, r.base = r.base, 0
	}
}

// tone is a source with no device: a quiet 440 Hz, and a click every two
// seconds, paced by this probe's clock.
func tone(ctx context.Context, emit func(Chunk)) {
	const rate = 48000
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	begin := now()
	var sent int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		want := int64((now() - begin).Seconds() * rate)
		n := int(want - sent)
		if n <= 0 {
			continue
		}
		if n > rate/4 {
			// The probe stalled: skip ahead, as a device would.
			sent = want - rate/100
			n = rate / 100
		}
		frames := make([]int16, n*2)
		for i := 0; i < n; i++ {
			f := sent + int64(i)
			v := 0.05 * math.Sin(2*math.Pi*440*float64(f)/rate)
			if in := f % (2 * rate); in < rate/200 {
				v = 0.8 * math.Sin(2*math.Pi*2000*float64(in)/rate)
			}
			frames[2*i] = int16(v * 32767)
			frames[2*i+1] = frames[2*i]
		}
		sent += int64(n)
		emit(Chunk{Frames: frames, Rate: rate, At: now()})
	}
}
