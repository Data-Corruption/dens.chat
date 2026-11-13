package shortcuts

import (
	"context"
	"encoding/binary"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// cutter is a soundCut on a clock the test moves, keeping its pieces.
type cutter struct {
	*soundCut
	at     time.Duration
	pieces [][]byte
}

func newCutter() *cutter {
	c := &cutter{}
	c.soundCut = &soundCut{now: func() time.Duration { return c.at }, out: func(p []byte) { c.pieces = append(c.pieces, p) }}
	return c
}

// frames reads a piece's frames back, the left channel's.
func frames(piece []byte) []int16 {
	var out []int16
	for i := 4; i+4 <= len(piece); i += 4 {
		out = append(out, int16(binary.LittleEndian.Uint16(piece[i:])))
	}
	return out
}

func ramp(from, n int) []int16 {
	pcm := make([]int16, 2*n)
	for i := range n {
		pcm[2*i], pcm[2*i+1] = int16(from+i), int16(from+i)
	}
	return pcm
}

// What the system gives is cut into pieces of 240 frames, each naming its
// rate, whatever sizes it came in; a system that gives nothing for 60 ms
// gets silence in its place, up to 10 ms ago, and evenly from then until
// it gives again; and one that stood still for over a second has that
// time skipped, not filled.
func TestTheSoundIsCutIntoPieces(t *testing.T) {
	c := newCutter()
	c.data(ramp(0, 100), 48000)
	c.at += 10 * time.Millisecond
	c.data(ramp(100, 500), 48000)
	if len(c.pieces) != 2 {
		t.Fatalf("600 frames made %d pieces", len(c.pieces))
	}
	for i, p := range c.pieces {
		if len(p) != PieceSize || PieceRate(p) != 48000 {
			t.Fatalf("piece %d: %d bytes at %d Hz", i, len(p), PieceRate(p))
		}
		got := frames(p)
		if got[0] != int16(240*i) || got[239] != int16(240*i+239) {
			t.Errorf("piece %d runs %d to %d", i, got[0], got[239])
		}
	}

	// Nothing for 100 ms: 90 ms of silence, then the system's sound after
	// it.
	c.pieces = nil
	c.at += 100 * time.Millisecond
	c.fill()
	filled := 0
	for _, p := range c.pieces {
		for _, v := range frames(p) {
			if v == 0 {
				filled++
			}
		}
	}
	if owed := int((c.at.Seconds()-0.01)*48000) - 600; filled < owed-240 || filled > owed+240 {
		t.Errorf("filled %d frames of silence where %d were owed", filled, owed)
	}

	// Still nothing: each 10 ms more is filled as it passes, not once 60
	// more are owed, and no longer once the system gives again.
	for i := range 5 {
		c.pieces = nil
		c.at += 10 * time.Millisecond
		c.fill()
		if len(c.pieces) != 2 {
			t.Errorf("10 ms more of nothing, the %d. time, made %d pieces of silence", i+1, len(c.pieces))
		}
	}
	c.data(ramp(0, 480), 48000)
	c.pieces = nil
	c.at += 30 * time.Millisecond
	c.fill()
	if len(c.pieces) != 0 {
		t.Errorf("30 ms after the system gave, %d pieces were filled", len(c.pieces))
	}

	// A stall of two seconds is skipped.
	c.pieces = nil
	c.at += 2 * time.Second
	c.fill()
	if len(c.pieces) != 0 {
		t.Errorf("a stall of two seconds was filled with %d pieces", len(c.pieces))
	}

	// Another rate starts again, its pieces naming it.
	c.data(ramp(0, 480), 44100)
	if len(c.pieces) != 2 || PieceRate(c.pieces[1]) != 44100 {
		t.Errorf("44.1 kHz made %d pieces", len(c.pieces))
	}
	if PieceRate(make([]byte, 10)) != 0 || PieceRate(binary.LittleEndian.AppendUint32(nil, 1)) != 0 {
		t.Error("a malformed piece names a rate")
	}
}

// fakeCapture stands in for the system's sound: a ramp at 48 kHz, 10 ms at
// a time, which pauses as the test says, and what keeps it from reading,
// "" for nothing, reported each time.
type fakeCapture struct {
	mu      sync.Mutex
	running int
	paused  bool
	problem string
	starts  int
}

func (f *fakeCapture) capture(ctx context.Context, take func([]int16, int), problem func(string)) error {
	f.mu.Lock()
	f.running++
	f.starts++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.running--
		f.mu.Unlock()
	}()
	problem("")
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		f.mu.Lock()
		paused, why := f.paused, f.problem
		f.mu.Unlock()
		problem(why)
		if paused {
			continue
		}
		take(ramp(n%1000+1, 480), 48000)
		n += 480
	}
}

func (f *fakeCapture) set(paused bool, problem string) {
	f.mu.Lock()
	f.paused, f.problem = paused, problem
	f.mu.Unlock()
}

func (f *fakeCapture) count() (running, starts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running, f.starts
}

// received keeps what the hub hands Relay.
type received struct {
	mu       sync.Mutex
	pieces   [][]byte
	problems []string
}

func (r *received) take(p []byte) {
	r.mu.Lock()
	r.pieces = append(r.pieces, p)
	r.mu.Unlock()
}

func (r *received) problem(why string) {
	r.mu.Lock()
	r.problems = append(r.problems, why)
	r.mu.Unlock()
}

func (r *received) snapshot() ([][]byte, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.pieces), slices.Clone(r.problems)
}

func waitTrue(t *testing.T, what string, ok func() bool) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// While Relay shares the computer's sound, the helper reads it and sends
// it to the service on a request of its own, in pieces the hub hands
// Relay: the system's sound as it came, and silence where it gave none.
// A problem that lasts reaches Relay, and that it's gone once it is; and
// the helper stops reading as Relay stops sharing. With no helper, Relay
// hears that none is running.
func TestTheHelperSendsTheComputersSound(t *testing.T) {
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub("v1.0.0", "", keysV, testLog(t))
	client := serve(t, hub, me)
	got := &received{}
	hub.SetSound(got.take, got.problem)
	if _, problems := got.snapshot(); !slices.Equal(problems, []string{ErrNoHelper.Error()}) {
		t.Errorf("with no helper, Relay heard %q", problems)
	}

	fake := &fakeCapture{}
	h := helperFor(t, "v1.0.0", client, newFakeKeys())
	h.OpenSound = func(string) (*control.Stream, error) { return client.Open(control.Request{Op: control.OpSound}) }
	h.Capture = fake.capture
	run(t, h)
	waitTrue(t, "a second of sound", func() bool {
		pieces, _ := got.snapshot()
		return len(pieces) >= 200
	})
	pieces, problems := got.snapshot()
	if problems[len(problems)-1] != "" {
		t.Errorf("Relay heard %q once the helper read the sound", problems)
	}
	sound := 0
	for _, p := range pieces {
		if PieceRate(p) != 48000 {
			t.Fatalf("a piece of %d bytes at %d Hz", len(p), PieceRate(p))
		}
		for _, v := range frames(p) {
			if v != 0 {
				sound++
			}
		}
	}
	if sound < 200*240*9/10 {
		t.Errorf("%d frames of sound in %d pieces", sound, len(pieces))
	}

	// Paused, as an output with nothing playing: silence keeps coming.
	fake.set(true, "")
	before := len(pieces)
	time.Sleep(500 * time.Millisecond)
	pieces, _ = got.snapshot()
	if n := len(pieces) - before; n < 50 {
		t.Errorf("%d pieces in half a second of nothing playing", n)
	}

	fake.set(false, "the output went")
	waitTrue(t, "Relay hearing the problem", func() bool {
		_, problems := got.snapshot()
		return problems[len(problems)-1] == "the output went"
	})
	fake.set(false, "")
	waitTrue(t, "Relay hearing the problem's gone", func() bool {
		_, problems := got.snapshot()
		return problems[len(problems)-1] == ""
	})
	hub.SetSound(nil, nil)
	waitTrue(t, "the helper to stop reading", func() bool {
		running, _ := fake.count()
		return running == 0
	})
	hub.SetSound(got.take, got.problem)
	waitTrue(t, "the helper to read again", func() bool {
		running, starts := fake.count()
		return running == 1 && starts == 2
	})
}
