package host

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

// The capture opens the default output's loopback through WASAPI, says
// it reads, hands over 16-bit stereo at the output's rate, whatever plays,
// and returns as its context ends. A computer with no output, as a CI
// runner may be, says so, and the test stops there.
func TestCaptureSoundOpensTheLoopback(t *testing.T) {
	var mu sync.Mutex
	var frames, odd int
	var rates []int
	var problems []string
	take := func(pcm []int16, rate int) {
		mu.Lock()
		defer mu.Unlock()
		frames += len(pcm) / 2
		odd += len(pcm) % 2
		if !slices.Contains(rates, rate) {
			rates = append(rates, rate)
		}
	}
	problem := func(why string) {
		mu.Lock()
		defer mu.Unlock()
		problems = append(problems, why)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := CaptureSound(ctx, take, problem); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("the capture returned %s after it started, its context ending at 2 s", d)
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.Contains(problems, "this computer has no sound output") {
		t.Skip("this computer has no sound output")
	}
	if len(problems) == 0 || problems[0] != "" {
		t.Fatalf("the capture never said it reads: %q", problems)
	}
	t.Logf("read %d frames at %v, nothing playing perhaps", frames, rates)
	for _, r := range rates {
		if r < 8000 || r > 192000 {
			t.Errorf("a rate of %d", r)
		}
	}
	if odd != 0 {
		t.Errorf("%d pieces of half a frame", odd)
	}
}
