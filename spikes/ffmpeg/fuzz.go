package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// asanExit is the exit code the ASan driver runs with, so a memory bug is
// told apart from FFmpeg refusing a file.
const asanExit = 99

// fuzz damages the seeds over and over, and strips each damaged copy with the
// module in its worker and with the native driver under AddressSanitizer. It
// counts how each run ended, and keeps every input that wasn't simply stripped
// or refused, with what happened, in out/spikes/ffmpeg/fuzz/.
func fuzz(args []string) int {
	fs := flag.NewFlagSet("fuzz", flag.ExitOnError)
	duration := fs.Duration("duration", time.Hour, "how long to run")
	jobs := fs.Int("jobs", 8, "runs at once")
	fs.Parse(args)
	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ffspike fuzz [-duration D] [-jobs N] ASAN_DRIVER SEED...")
		return 2
	}
	asan := fs.Arg(0)
	var seeds []seed
	for _, path := range fs.Args()[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		ext := strings.ToLower(filepath.Ext(path))
		sd := seed{name: filepath.Base(path), ext: ext, data: data}
		if images[ext] {
			sd.ops = []string{"still"}
		} else if sd.muxer, err = muxerFor(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		} else if videos[ext] {
			sd.ops = []string{"strip", "poster"}
		} else {
			sd.ops = []string{"strip"}
		}
		seeds = append(seeds, sd)
	}
	dir := "out/spikes/ffmpeg/fuzz"
	if err := os.MkdirAll(filepath.Join(dir, "finds"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	var (
		mu     sync.Mutex
		counts = map[string]int{}
		runs   int
	)
	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup
	for w := range *jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(w)))
			work := filepath.Join(dir, fmt.Sprintf("w%d", w))
			os.MkdirAll(work, 0o755)
			for n := 0; time.Now().Before(deadline); n++ {
				s := seeds[rng.IntN(len(seeds))]
				data, how := damage(rng, s.data)
				in := filepath.Join(work, "in"+s.ext)
				if err := os.WriteFile(in, data, 0o644); err != nil {
					return
				}
				op := s.ops[rng.IntN(len(s.ops))]
				ext := s.ext
				if op != "strip" {
					ext = ".img"
				}
				out, nout := filepath.Join(work, "out"+ext), filepath.Join(work, "native"+ext)
				os.Remove(out)
				os.Remove(nout)
				var job, nargs []string
				switch op {
				case "strip":
					job, nargs = []string{"strip", in, out, s.muxer}, []string{"strip", s.muxer, in, nout}
				case "still":
					job, nargs = []string{"still", in, out, "0", "3"}, []string{"still", in, nout, "0", "3"}
				case "poster":
					job, nargs = []string{"poster", in, out, "640", "3"}, []string{"poster", in, nout, "640", "3"}
				}
				module := moduleOutcome(runJob(512, 20*time.Second, job, io.Discard))
				native, report := nativeOutcome(asan, nargs)
				key := op + ": module " + module + ", native " + native
				// Where both strip, the translation must write what the C
				// code does.
				if module == "stripped" && native == "stripped" {
					switch compareFiles(out, nout) {
					case "same":
					case "mp3 tag":
						key += ", outputs differ only in the MP3 VBR tag"
					default:
						key += ", outputs differ"
						module = "stripped differently"
					}
				}
				mu.Lock()
				counts[key]++
				runs++
				id := runs
				mu.Unlock()
				if (module != "stripped" && module != "refused") || (native != "stripped" && native != "refused") {
					name := fmt.Sprintf("%06d-%s-%s", id, strings.ReplaceAll(module, " ", "_"), strings.ReplaceAll(native, " ", "_"))
					os.WriteFile(filepath.Join(dir, "finds", name+s.ext), data, 0o644)
					os.WriteFile(filepath.Join(dir, "finds", name+".txt"),
						[]byte(fmt.Sprintf("seed %s\nop %s\ndamage %s\nmodule %s\nnative %s\n\n%s", s.name, op, how, module, native, report)), 0o644)
				}
			}
		}()
	}
	start := time.Now()
	wg.Wait()

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
	fmt.Printf("%d runs in %s\n", runs, time.Since(start).Round(time.Second))
	for _, k := range keys {
		fmt.Printf("%7d  %s\n", counts[k], k)
	}
	return 0
}

type seed struct {
	name, ext, muxer string
	ops              []string
	data             []byte
}

// images get stills, videos get stripped or a poster, and the rest get
// stripped.
var (
	images = map[string]bool{".heic": true, ".heif": true, ".avif": true, ".tif": true, ".tiff": true,
		".dng": true, ".jp2": true, ".j2c": true, ".j2k": true, ".psd": true}
	videos = map[string]bool{".mp4": true, ".mov": true, ".mkv": true, ".webm": true, ".m4v": true}
)

// damage returns a copy of data with one kind of damage, and says which.
// Most of it lands in the first 64 KB, where containers keep their headers.
func damage(rng *rand.Rand, data []byte) ([]byte, string) {
	d := append([]byte(nil), data...)
	at := func() int {
		if rng.IntN(4) > 0 {
			return rng.IntN(min(len(d), 64<<10))
		}
		return rng.IntN(len(d))
	}
	switch rng.IntN(5) {
	case 0:
		n := 1 + rng.IntN(16)
		for range n {
			d[at()] ^= byte(1 + rng.IntN(255))
		}
		return d, fmt.Sprintf("flipped %d bytes", n)
	case 1:
		n := rng.IntN(len(d))
		return d[:n], fmt.Sprintf("cut at %d", n)
	case 2:
		values := []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, 0xfffffff0, 0x10000, 8}
		n := 1 + rng.IntN(4)
		for range n {
			if i := at(); i+4 <= len(d) {
				binary.BigEndian.PutUint32(d[i:], values[rng.IntN(len(values))])
			}
		}
		return d, fmt.Sprintf("set %d 32-bit fields", n)
	case 3:
		i, j := at(), at()
		if i > j {
			i, j = j, i
		}
		j = min(j, i+4096)
		chunk := append([]byte(nil), d[i:j]...)
		k := at()
		return append(d[:k:k], append(chunk, d[k:]...)...), fmt.Sprintf("copied %d bytes from %d to %d", j-i, i, k)
	default:
		i := at()
		n := min(1+rng.IntN(256), len(d)-i)
		for x := range n {
			d[i+x] = byte(rng.IntN(256))
		}
		return d, fmt.Sprintf("randomized %d bytes at %d", n, i)
	}
}

// compareFiles says whether two outputs are the same, differ only as FFmpeg's
// uninitialized read makes MP3s differ, or differ otherwise.
//
// avpriv_mpegaudio_decode_header leaves bit_rate unset for a free-format
// frame, and the MP3 writer then compares it, so a damaged frame makes it call
// the file "Xing" (variable bitrate) or "Info" (constant) depending on what the
// stack held. That changes the tag and its frame's two-byte checksum.
func compareFiles(a, b string) string {
	x, err := os.ReadFile(a)
	if err != nil {
		return "unreadable"
	}
	y, err := os.ReadFile(b)
	if err != nil {
		return "unreadable"
	}
	if bytes.Equal(x, y) {
		return "same"
	}
	if len(x) != len(y) {
		return "differ"
	}
	tag := func(d []byte) int {
		i := bytes.Index(d[:min(len(d), 256)], []byte("Xing"))
		if i < 0 {
			i = bytes.Index(d[:min(len(d), 256)], []byte("Info"))
		}
		return i
	}
	i := tag(x)
	if i < 0 || tag(y) != i {
		return "differ"
	}
	other := 0
	for k := range x {
		if x[k] != y[k] && (k < i || k >= i+4) {
			other++
		}
	}
	if other <= 2 {
		return "mp3 tag"
	}
	return "differ"
}

func moduleOutcome(r report) string {
	switch {
	case r.OK:
		return "stripped"
	case strings.HasPrefix(r.Error, "ffmpeg:"):
		return "refused"
	case strings.HasPrefix(r.Error, "trap:"):
		return "trapped"
	case strings.HasPrefix(r.Error, "deadline"):
		return "ran past the deadline"
	case strings.HasPrefix(r.Error, "exit status"):
		return "worker died"
	}
	return "other: " + r.Error
}

func nativeOutcome(driver string, args []string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, driver, args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("ASAN_OPTIONS=exitcode=%d:detect_leaks=0:symbolize=1", asanExit))
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return "ran past the deadline", ""
	case err == nil:
		return "stripped", ""
	case errors.As(err, &exit) && exit.ExitCode() == asanExit:
		return "memory bug", string(output)
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return "refused", ""
	}
	return "crashed", string(output)
}
