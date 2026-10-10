package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// asanExit is the exit code the native driver runs with under
// AddressSanitizer, so a memory bug is told apart from FFmpeg refusing a
// file.
const asanExit = 99

// fuzzJobTime bounds each input's job, in the module and natively, well
// below the Runner's minutes: a damaged file that makes FFmpeg spin ends
// its job as it would anyway, and a few don't stall the fuzzer. Both jobs
// together stay under the ten seconds Go's fuzzer gives one input before
// it ends the worker as deadlocked, which a loaded machine would reach.
const fuzzJobTime = 4 * time.Second

// FuzzDriver runs damaged files through the driver, as Dens does with what
// members send. In the module, every job ends with an answer or a JobError,
// whatever the file: a trap, a balloon or a loop ends its worker, not the
// Runner. With DENS_FFMPEG_ASAN naming the driver built natively under
// AddressSanitizer (scripts/ffmpeg.sh --asan), each input runs there too,
// where a memory bug the module would only hold, in Dens's C or FFmpeg's,
// fails the run. scripts/ffmpeg.sh --fuzz runs it for a while; go test runs
// the seeds, through the module.
//
// which picks the operation, and for a still, the rest of it picks the
// orientation the host passes and whether it's scaled down, as a photo's
// smaller copy is (stillArgs).
func FuzzDriver(f *testing.F) {
	if raceOn {
		f.Skip("the race detector slows the module some fifty times over")
	}
	ops := []Op{OpProbe, OpStrip, OpStill, OpPoster}
	seeds := map[string][]Op{
		"with-gps.mov": {OpStrip, OpPoster},
		"with-gps.mp4": {OpStrip, OpPoster},
		"rotated.heic": {OpStill},
		"meta.mp4":     {OpProbe, OpStrip},
		"meta.mkv":     {OpStrip},
		"meta.webm":    {OpStrip},
		"meta.mp3":     {OpStrip},
		"meta.m4a":     {OpStrip},
		"meta.flac":    {OpStrip},
		"meta.ogg":     {OpStrip},
		"meta.wav":     {OpStrip},
	}
	for name, want := range seeds {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		for _, op := range want {
			for i := range ops {
				if ops[i] == op {
					f.Add(uint8(i), data)
				}
			}
		}
	}
	// JPEGs and PNGs, which a still takes for smaller copies: a JPEG turned
	// a quarter and scaled, and PNGs with and without transparency.
	still := uint8(2)
	for _, seed := range []struct {
		data  []byte
		which uint8
	}{
		{seedImage(f, "jpeg", false), still + 4*6 + 128},
		{seedImage(f, "png", true), still + 128},
		{seedImage(f, "png", false), still + 4*3},
	} {
		f.Add(seed.which, seed.data)
	}
	asan := os.Getenv("DENS_FFMPEG_ASAN")
	r := TestRunner(nil)
	r.timeout = func(Op, int64) time.Duration { return fuzzJobTime }
	f.Fuzz(func(t *testing.T, which uint8, data []byte) {
		op := ops[int(which)%len(ops)]
		ctx := context.Background()
		in, out := &memFile{data: data}, &memFile{}
		var err error
		switch op {
		case OpProbe:
			_, err = r.Probe(ctx, in)
		case OpStrip:
			_, err = r.Strip(ctx, in, out, "")
		case OpStill:
			side, orientation := stillArgs(which)
			_, err = r.Still(ctx, in, out, side, 3, orientation)
		case OpPoster:
			_, err = r.Poster(ctx, in, out, 640, 5)
		}
		var job *JobError
		if err != nil && !errors.As(err, &job) {
			t.Fatalf("%s: %v", op, err)
		}
		if asan != "" {
			runNative(t, asan, op, which, data)
		}
	})
}

// stillArgs reads a still's maximum side and orientation from the rest of
// a fuzz input's which.
func stillArgs(which uint8) (side, orientation int) {
	if which >= 128 {
		side = 32
	}
	return side, int(which/4) % 9
}

// seedImage makes a small JPEG, or a PNG, with transparency or without.
func seedImage(f *testing.F, kind string, alpha bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			a := uint8(255)
			if alpha {
				a = uint8(x * 4)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 5), B: 128, A: a})
		}
	}
	var buf bytes.Buffer
	var err error
	if kind == "jpeg" {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		f.Fatal(err)
	}
	return buf.Bytes()
}

// runNative runs a job with the native driver under AddressSanitizer, and
// fails on what it reports.
func runNative(t *testing.T, asan string, op Op, which uint8, data []byte) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		t.Fatal(err)
	}
	side, orientation := stillArgs(which)
	args := map[Op][]string{
		OpProbe:  {"probe", in},
		OpStrip:  {"strip", in, out},
		OpStill:  {"still", in, out, strconv.Itoa(side), "3", strconv.Itoa(orientation)},
		OpPoster: {"poster", in, out, "640", "5"},
	}[op]
	ctx, cancel := context.WithTimeout(context.Background(), fuzzJobTime)
	defer cancel()
	cmd := exec.CommandContext(ctx, asan, args...)
	// Allocations past what the module's cap allows fail, as they would in
	// it, rather than ending the run.
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=exitcode=99:detect_leaks=0:allocator_may_return_null=1:"+
		"max_allocation_size_mb=768:soft_rss_limit_mb=2048")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var exit *exec.ExitError
	if err := cmd.Run(); errors.As(err, &exit) && exit.ExitCode() == asanExit {
		report := stderr.Bytes()
		t.Fatalf("AddressSanitizer, on %s:\n%s", op, report[max(0, len(report)-8000):])
	}
}
