package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// asanExit is the exit code the native driver runs with under
// AddressSanitizer, so a memory bug is told apart from FFmpeg refusing a
// file.
const asanExit = 99

// FuzzDriver runs damaged files through the driver, as Dens does with what
// members send. In the module, every job ends with an answer or a JobError,
// whatever the file: a trap, a balloon or a loop ends its worker, not the
// Runner. With DENS_FFMPEG_ASAN naming the driver built natively under
// AddressSanitizer (scripts/ffmpeg.sh --asan), each input runs there too,
// where a memory bug the module would only hold, in Dens's C or FFmpeg's,
// fails the run. scripts/ffmpeg.sh --fuzz runs it for a while; go test runs
// the seeds, through the module.
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
	asan := os.Getenv("DENS_FFMPEG_ASAN")
	r := TestRunner(nil)
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
			_, err = r.Still(ctx, in, out, 0, 3)
		case OpPoster:
			_, err = r.Poster(ctx, in, out, 640, 5)
		}
		var job *JobError
		if err != nil && !errors.As(err, &job) {
			t.Fatalf("%s: %v", op, err)
		}
		if asan != "" {
			runNative(t, asan, op, data)
		}
	})
}

// runNative runs a job with the native driver under AddressSanitizer, and
// fails on what it reports.
func runNative(t *testing.T, asan string, op Op, data []byte) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		t.Fatal(err)
	}
	args := map[Op][]string{
		OpProbe:  {"probe", in},
		OpStrip:  {"strip", in, out},
		OpStill:  {"still", in, out, "0", "3"},
		OpPoster: {"poster", in, out, "640", "5"},
	}[op]
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
