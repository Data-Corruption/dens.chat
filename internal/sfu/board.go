package sfu

import (
	"errors"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// A call's soundboard (M14.3): one more audio track in the call, which
// every member's connection gets as they join, and which the den plays its
// sounds on, one at a time. The den sends a sound's Opus packets as its
// file holds them, at their pace, so everyone hears it at once, and a
// member joining partway hears the rest.

// opusRate is the rate Opus counts samples at, which its RTP timestamps
// count in.
const opusRate = 48000

// ErrPlaying is a sound asked to play while another plays in the call.
var ErrPlaying = errors.New("a sound plays in the call")

// errNoCall is a sound asked to play in a call nobody is in.
var errNoCall = errors.New("nobody is in that call")

// Sound is what the soundboard plays: its Opus packets in order, each with
// how many samples at 48 kHz it holds, until io.EOF or another error, which
// ends it.
type Sound interface {
	Next() (packet []byte, samples int, err error)
	Close() error
}

// board is a call's soundboard: its track, and the sound that plays on it.
// seq, ts and last carry the track's numbering on from one sound to the
// next; only the sound playing touches them, and the SFU's lock hands them
// from one sound to the next.
type board struct {
	track   *webrtc.TrackLocalStaticRTP
	playing *playing // guarded by the SFU's lock
	seq     uint16
	ts      uint32
	last    time.Time // when the last packet went
}

// playing is a sound on its way, which ends once end closes.
type playing struct {
	end  chan struct{}
	once sync.Once
}

func (p *playing) stop() { p.once.Do(func() { close(p.end) }) }

func newBoard() (*board, error) {
	track, err := webrtc.NewTrackLocalStaticRTP(opus, boardTrack, denproto.SoundboardStream)
	if err != nil {
		return nil, err
	}
	return &board{track: track}, nil
}

// stopLocked ends the sound that plays, if any. The caller holds the SFU's
// lock.
func (b *board) stopLocked() {
	if b.playing != nil {
		b.playing.stop()
	}
}

// Play sends a sound's packets on the soundboard of a room's call, the
// first at once and each after it as the one before ends. Once the sound
// ends, on its own or as stop is called, it closes the sound and calls
// done, with no lock held; stop never waits, so a caller may hold its own
// lock, which done may take.
func (s *SFU) Play(room string, sound Sound, done func()) (stop func(), err error) {
	s.mu.Lock()
	b := s.boards[room]
	switch {
	case s.closed:
		err = ErrClosed
	case b == nil:
		err = errNoCall
	case b.playing != nil:
		err = ErrPlaying
	}
	if err != nil {
		s.mu.Unlock()
		_ = sound.Close()
		return nil, err
	}
	p := &playing{end: make(chan struct{})}
	b.playing = p
	s.mu.Unlock()
	go func() {
		b.play(sound, p.end)
		_ = sound.Close()
		s.mu.Lock()
		if b.playing == p {
			b.playing = nil
		}
		s.mu.Unlock()
		done()
	}()
	return p.stop, nil
}

// play sends a sound's packets at their pace until it ends or end closes.
// Its timestamps go on from the last sound's by the time between, so a
// browser's jitter buffer takes the silence as time gone by, and its first
// packet is marked as the start of a burst of sound, as after silence
// suppressed (RFC 7587, 4.2).
func (b *board) play(sound Sound, end <-chan struct{}) {
	if !b.last.IsZero() {
		b.ts += samplesIn(time.Since(b.last))
	}
	due := time.Now()
	wait := time.NewTimer(0)
	defer wait.Stop()
	<-wait.C
	for first := true; ; first = false {
		packet, samples, err := sound.Next()
		if err != nil {
			return
		}
		if d := time.Until(due); d > 0 {
			wait.Reset(d)
			select {
			case <-end:
				return
			case <-wait.C:
			}
		} else {
			select {
			case <-end:
				return
			default:
			}
		}
		b.seq++
		_ = b.track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, Marker: first, PayloadType: opusType,
			SequenceNumber: b.seq, Timestamp: b.ts}, Payload: packet})
		b.ts += uint32(samples)
		b.last = time.Now()
		due = due.Add(time.Duration(samples) * time.Second / opusRate)
	}
}

// samplesIn is how many samples at opusRate pass in d, wrapping as RTP's
// timestamps do. It counts whole seconds apart from the rest, since d in
// nanoseconds times the rate would overflow once a call had gone two days
// between sounds.
func samplesIn(d time.Duration) uint32 {
	return uint32(d/time.Second)*opusRate + uint32(d%time.Second*opusRate/time.Second)
}
