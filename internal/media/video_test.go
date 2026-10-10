package media

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// An iPhone's video, as the module probes and scans one: 1080p at 60
// frames a second, turned a quarter, 21.7 seconds long, with a keyframe
// every second and 192 kbps of sound.
func phoneVideo() (ffmpeg.Stream, ffmpeg.Scanned) {
	v := ffmpeg.Stream{Type: "video", Codec: "hevc", Width: 1080, Height: 1920, Turn: "clock", Decoder: true}
	s := ffmpeg.Scanned{StartUS: 0, EndUS: 21_700_000, Frames: 1229, FPS: 59.94, VideoBytes: 34_000_000,
		AudioBytes: 520_800, AudioStreams: 1}
	for at := int64(0); at < s.EndUS; at += 1_001_667 {
		s.Keyframes = append(s.Keyframes, at)
	}
	return v, s
}

func TestPlanVideoCopy(t *testing.T) {
	v, s := phoneVideo()

	// By default, 720p, as stored, at 0.04 bits a pixel each frame.
	p, err := PlanVideoCopy(v, s, 34_941_781, 0, ByModule, 8)
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 1280 || p.Height != 720 || p.FPS != 30 || p.KBPS != 1105 || p.Estimate < 3_000_000 || p.Estimate > 4_000_000 {
		t.Errorf("the default copy: %+v", p)
	}
	// A video already about that small goes full size.
	if _, err := PlanVideoCopy(v, s, 4_000_000, 0, ByModule, 8); !errors.Is(err, ErrNoSmaller) {
		t.Errorf("a copy that wouldn't be much smaller: %v", err)
	}
	// Over a limit with room, the copy takes its own bitrate, no more.
	if p, err = PlanVideoCopy(v, s, 34_941_781, 25<<20, ByModule, 8); err != nil || p.KBPS != 1105 || p.Width != 1280 {
		t.Errorf("a copy with room: %+v, %v", p, err)
	}

	// Five minutes in 25 MiB: 435 kbps or so, too few for 720p, so 480p.
	s.EndUS = 300_000_000
	s.AudioBytes = 7_200_000
	if p, err = PlanVideoCopy(v, s, 500_000_000, 25<<20, ByModule, 8); err != nil {
		t.Fatal(err)
	}
	if p.Width != 854 || p.Height != 480 || p.KBPS < 400 || p.KBPS > 470 {
		t.Errorf("a long copy: %+v", p)
	}
	if p.Estimate > 25<<20 {
		t.Errorf("a long copy comes to %d bytes", p.Estimate)
	}

	// Twenty minutes don't fit at the floor: about seven do, with their
	// sound.
	s.EndUS = 1_200_000_000
	s.AudioBytes = 28_800_000
	_, err = PlanVideoCopy(v, s, 1<<30, 25<<20, ByModule, 8)
	var long *TooLong
	if !errors.As(err, &long) || long.Longest < 7*time.Minute || long.Longest > 7*time.Minute+30*time.Second {
		t.Errorf("a video too long: %v", err)
	}

	// The module makes no copy of what it doesn't decode.
	v.Decoder = false
	if _, err := PlanVideoCopy(v, s, 1<<30, 25<<20, ByModule, 8); !errors.Is(err, ErrNoCopy) {
		t.Errorf("a video the module doesn't decode: %v", err)
	}
}

func TestLowerVideoPlan(t *testing.T) {
	v, s := phoneVideo()
	p, err := PlanVideoCopy(v, s, 34_941_781, 3<<20, ByModule, 8)
	if err != nil {
		t.Fatal(err)
	}
	// A copy that came out a fifth over the limit aims for the share of it
	// it aimed for at first: its video, without the sound, takes about
	// seven tenths of the bitrate.
	lower, err := LowerVideoPlan(p, s, 3<<20*6/5, 3<<20, 8)
	if err != nil {
		t.Fatal(err)
	}
	if lower.KBPS > p.KBPS*75/100 || lower.KBPS < p.KBPS*65/100 || lower.Width != p.Width || len(lower.Chunks) != len(p.Chunks) {
		t.Errorf("lowered from %d kbps to %+v", p.KBPS, lower)
	}
	for _, c := range lower.Chunks {
		if c.KBPS != lower.KBPS {
			t.Errorf("a chunk at %d kbps", c.KBPS)
		}
	}
	// One far over goes below the floor.
	var long *TooLong
	if _, err := LowerVideoPlan(p, s, 3<<20*5, 3<<20, 8); !errors.As(err, &long) {
		t.Errorf("far over: %v", err)
	}
}

func TestPlanChunks(t *testing.T) {
	_, video := phoneVideo()
	check := func(name string, s ffmpeg.Scanned, workers, n int) {
		t.Helper()
		chunks := planChunks(s, workers, 1280, 720, 1105)
		if len(chunks) != n {
			t.Errorf("%s: %d chunks, want %d", name, len(chunks), n)
		}
		target := (s.EndUS - s.StartUS) / int64(max(n, 1))
		for i, c := range chunks {
			if i == 0 && c.StartUS != s.StartUS || i > 0 && c.StartUS != chunks[i-1].EndUS {
				t.Errorf("%s: chunk %d starts at %d", name, i, c.StartUS)
			}
			// A cut goes to a keyframe near it.
			if i > 0 && !slices.Contains(s.Keyframes, c.StartUS) && slices.ContainsFunc(s.Keyframes, func(k int64) bool {
				return abs(k-c.StartUS) <= target/4
			}) {
				t.Errorf("%s: chunk %d starts at %d, beside a keyframe", name, i, c.StartUS)
			}
			if c.EndUS-c.StartUS < leastPart.Microseconds() || c.EndUS-c.StartUS > maxChunk.Microseconds()+time.Second.Microseconds() {
				t.Errorf("%s: chunk %d lasts %d µs", name, i, c.EndUS-c.StartUS)
			}
			if c.MaxSide != 1280 || c.FPS != 30 || c.KBPS != 1105 {
				t.Errorf("%s: chunk %+v", name, c)
			}
		}
		if last := chunks[len(chunks)-1]; last.EndUS <= s.EndUS {
			t.Errorf("%s: the last chunk ends at %d, within the video", name, last.EndUS)
		}
	}
	// Eight workers get one chunk each.
	check("eight workers", video, 8, 8)
	// One worker gets chunks of at most ten seconds.
	check("one worker", video, 1, 3)
	// Without keyframes near, chunks start between them.
	sparse := video
	sparse.Keyframes = []int64{0}
	check("no keyframes", sparse, 8, 8)
	// A video too short for a second for each worker gets a chunk a
	// second.
	short := video
	short.EndUS = 12_000_000
	check("twelve seconds", short, 8, 8)
	short.EndUS = 4_200_000
	check("four seconds", short, 8, 4)
	short.EndUS = 900_000
	check("under a second", short, 8, 1)
	// Five minutes get chunks of ten seconds at most.
	long := video
	long.EndUS = 300_000_000
	check("five minutes", long, 8, 32)
}

func TestMakeVideoCopy(t *testing.T) {
	if raceOn {
		t.Skip("the race detector slows encoding in the module some fifty times over")
	}
	ctx := context.Background()
	m := ffmpeg.TestRunner(nil)
	full := &ffmpeg.Buffer{}
	if _, err := StripMedia(ctx, m, moduleFile(t, "with-gps.mov"), full); err != nil {
		t.Fatal(err)
	}
	p, err := m.Probe(ctx, full)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := p.Video()
	s, err := m.Scan(ctx, full)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanVideoCopy(v, s, full.Size(), 0, ByModule, 2)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Width != 568 || plan.Height != 320 || len(plan.Chunks) != 2 {
		t.Fatalf("plan: %+v", plan)
	}
	var mu sync.Mutex
	var seen []float64
	var files []*ffmpeg.Buffer
	out := &ffmpeg.Buffer{}
	c, err := MakeVideoCopy(ctx, m, full, v, plan, func() (ChunkFile, error) {
		f := &ffmpeg.Buffer{}
		files = append(files, f)
		return nopCloser{f}, nil
	}, out, func(f float64) {
		mu.Lock()
		seen = append(seen, f)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	// Turned a quarter, as the video is.
	if c.Width != 320 || c.Height != 568 || c.VideoPackets != 120 || c.AudioStreams != 1 || c.DurationMS < 3900 ||
		c.Bytes != out.Size() || c.Bytes*4 > full.Size()*3 {
		t.Errorf("copied %+v, from %d bytes", c, full.Size())
	}
	if len(files) != 2 {
		t.Errorf("%d chunk files", len(files))
	}
	if len(seen) == 0 || seen[len(seen)-1] != 1 || !slices.IsSorted(seen) {
		t.Errorf("progress: %d reports, last %v", len(seen), seen[len(seen)-1:])
	}

	// A chunk the module fails on fails the copy.
	bad := plan
	bad.Chunks = slices.Clone(plan.Chunks)
	bad.Chunks[1].KBPS = 0
	if _, err := MakeVideoCopy(ctx, m, full, v, bad, func() (ChunkFile, error) { return nopCloser{&ffmpeg.Buffer{}}, nil },
		&ffmpeg.Buffer{}, nil); !errors.Is(err, ffmpeg.ErrUnreadable) {
		t.Errorf("a chunk that fails: %v", err)
	}
}

type nopCloser struct{ *ffmpeg.Buffer }

func (nopCloser) Close() error { return nil }
