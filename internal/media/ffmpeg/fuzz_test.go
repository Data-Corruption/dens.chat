package ffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
// which picks the operation, and the rest of it the operation's arguments:
// for a still, the orientation the host passes and whether it's scaled
// down, as a photo's smaller copy is (stillArgs), and for a chunk of a
// video's copy, where it starts (chunkArgs). A video's copy is muxed from
// the input split in two: the video, and the packets of its copy
// (splitMux).
func FuzzDriver(f *testing.F) {
	if raceOn {
		f.Skip("the race detector slows the module some fifty times over")
	}
	seeds := map[string][]Op{
		"with-gps.mov": {OpStrip, OpPoster, OpScan, OpEncode},
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
			f.Add(uint8(slices.Index(fuzzOps, op)), data)
		}
	}
	// JPEGs and PNGs, which a still takes for smaller copies: a JPEG turned
	// a quarter and scaled, and PNGs with and without transparency.
	for _, seed := range []struct {
		data              []byte
		orientation, side int
	}{
		{seedImage(f, "jpeg", false), 6, 32},
		{seedImage(f, "png", true), 0, 32},
		{seedImage(f, "png", false), 3, 0},
	} {
		f.Add(stillWhich(seed.orientation, seed.side), seed.data)
	}
	r := TestRunner(nil)
	r.timeout = func(Job, int64) time.Duration { return fuzzJobTime }
	// The phone video and the packets of half a second of its copy, as mux
	// takes them.
	video, err := os.ReadFile(filepath.Join("testdata", "with-gps.mov"))
	if err != nil {
		f.Fatal(err)
	}
	packets := &memFile{}
	if _, err := r.Encode(context.Background(), &memFile{data: video}, packets, chunkArgs(0), nil); err != nil {
		f.Fatal(err)
	}
	f.Add(uint8(slices.Index(fuzzOps, OpMux)), joinMux(video, packets.data))
	asan := os.Getenv("DENS_FFMPEG_ASAN")
	f.Fuzz(func(t *testing.T, which uint8, data []byte) {
		op := fuzzOps[int(which)%len(fuzzOps)]
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
		case OpScan:
			_, err = r.Scan(ctx, in)
		case OpEncode:
			_, err = r.Encode(ctx, in, out, chunkArgs(which), nil)
		case OpMux:
			video, packets := splitMux(data)
			_, err = r.Mux(ctx, &memFile{data: video}, &memFile{data: packets}, out, 64, 36)
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

// fuzzOps are the operations FuzzDriver runs, by which.
var fuzzOps = []Op{OpProbe, OpStrip, OpStill, OpPoster, OpScan, OpEncode, OpMux}

// stillArgs reads a still's maximum side and orientation from the rest of
// a fuzz input's which.
func stillArgs(which uint8) (side, orientation int) {
	if which >= 128 {
		side = 32
	}
	return side, int(which) / len(fuzzOps) % 9
}

// stillWhich is the which of a still with an orientation, and scaled down
// when side is set.
func stillWhich(orientation, side int) uint8 {
	still := slices.Index(fuzzOps, OpStill)
	for which := still; which < 256; which += len(fuzzOps) {
		if s, o := stillArgs(uint8(which)); o == orientation && (s > 0) == (side > 0) {
			return uint8(which)
		}
	}
	panic("no such still")
}

// chunkArgs reads a chunk of a video's copy from the rest of a fuzz input's
// which: half a second starting at one of the first four half seconds,
// small enough to encode in a moment.
func chunkArgs(which uint8) Chunk {
	start := int64(int(which)/len(fuzzOps)%4) * 500000
	return Chunk{StartUS: start, EndUS: start + 500000, MaxSide: 64, FPS: 30, KBPS: 200}
}

// joinMux puts a video and its copy's packets in one fuzz input, which
// splitMux splits again: the video's length, then the two.
func joinMux(video, packets []byte) []byte {
	return slices.Concat(binary.LittleEndian.AppendUint32(nil, uint32(len(video))), video, packets)
}

func splitMux(data []byte) (video, packets []byte) {
	if len(data) < 4 {
		return nil, data
	}
	n := min(int(binary.LittleEndian.Uint32(data)), len(data)-4)
	return data[4 : 4+n], data[4+n:]
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
	in, out, packets := filepath.Join(dir, "in"), filepath.Join(dir, "out"), filepath.Join(dir, "packets")
	video, copied := data, []byte(nil)
	if op == OpMux {
		video, copied = splitMux(data)
	}
	if err := os.WriteFile(in, video, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packets, copied, 0o600); err != nil {
		t.Fatal(err)
	}
	side, orientation := stillArgs(which)
	c := chunkArgs(which)
	itoa := func(n int64) string { return strconv.FormatInt(n, 10) }
	args := map[Op][]string{
		OpProbe:  {"probe", in},
		OpStrip:  {"strip", in, out},
		OpStill:  {"still", in, out, strconv.Itoa(side), "3", strconv.Itoa(orientation)},
		OpPoster: {"poster", in, out, "640", "5"},
		OpScan:   {"scan", in},
		OpEncode: {"encode", in, out, itoa(c.StartUS), itoa(c.EndUS), strconv.Itoa(c.MaxSide), strconv.Itoa(c.FPS), strconv.Itoa(c.KBPS)},
		OpMux:    {"mux", in, packets, out, "64", "36"},
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
