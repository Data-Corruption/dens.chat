package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Runner runs jobs, each in a worker process of its own: one at a time, but
// for the chunks of videos' copies, which run side by side, as many as half
// the computer's cores (M5.4).
type Runner struct {
	command []string
	start   func(*exec.Cmd) error
	log     func(string)
	slot    chan struct{}
	chunks  chan struct{}
	timeout func(Job, int64) time.Duration

	// MemoryMB caps the module's memory in a worker.
	MemoryMB int
}

// WorkerCommand is the hidden command that runs Work, which a Runner's
// command names after the binary.
const WorkerCommand = "media-worker"

// TestWorkerEnv tells a test binary to run Work instead of its tests: a
// package whose tests run media jobs checks it in TestMain, and gets a Runner
// of itself from TestRunner.
const TestWorkerEnv = "DENS_MEDIA_TEST_WORKER"

// TestRunner returns a Runner whose workers are this program, with args,
// and TestWorkerEnv set for them alone: a test binary's TestMain runs Work
// when it sees it. log gets FFmpeg's warnings.
func TestRunner(log func(string), args ...string) *Runner {
	// os.Args[0] may be a bare name, which exec won't look up in the
	// current directory.
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	return NewRunner(append([]string{exe}, args...), func(cmd *exec.Cmd) error {
		cmd.Env = append(os.Environ(), TestWorkerEnv+"=1")
		return cmd.Start()
	}, log)
}

// DefaultMemoryMB fits a 48-megapixel photo's still.
const DefaultMemoryMB = 768

// NewRunner returns a Runner whose workers run command, its first element
// the binary, started by start, which may lower their priority. log gets
// FFmpeg's warnings, which name no file.
func NewRunner(command []string, start func(*exec.Cmd) error, log func(string)) *Runner {
	if start == nil {
		start = (*exec.Cmd).Start
	}
	if log == nil {
		log = func(string) {}
	}
	return &Runner{command: command, start: start, log: log, slot: make(chan struct{}, 1),
		chunks: make(chan struct{}, max(1, runtime.NumCPU()/2)), timeout: timeoutFor, MemoryMB: DefaultMemoryMB}
}

// Workers is how many chunks of videos' copies run at once.
func (r *Runner) Workers() int { return cap(r.chunks) }

// Probe describes a file.
func (r *Runner) Probe(ctx context.Context, in Input) (Probed, error) {
	var p Probed
	return p, r.run(ctx, Job{Op: OpProbe}, files{in: in}, &p)
}

// Strip writes a copy of in's video and audio, without anything else it
// carried, to out, in the container muxer names, or the one the driver picks
// when it's empty.
func (r *Runner) Strip(ctx context.Context, in Input, out Output, muxer string) (Stripped, error) {
	var s Stripped
	return s, r.run(ctx, Job{Op: OpStrip, Muxer: muxer}, files{in: in, out: out}, &s)
}

// Still writes in, an image, to out as a JPEG, or a PNG when it has
// transparency or is one, upright, fitting maxSide when that's above 0, at
// JPEG quality quality (2 is best). orientation is a JPEG's EXIF
// orientation, from 1 to 8, which FFmpeg doesn't read; 0 turns the image as
// the file itself says, as a HEIC does.
func (r *Runner) Still(ctx context.Context, in Input, out Output, maxSide, quality, orientation int) (Image, error) {
	var i Image
	return i, r.run(ctx, Job{Op: OpStill, MaxSide: maxSide, Quality: quality, Orientation: orientation}, files{in: in, out: out}, &i)
}

// Poster writes in's first video frame to out as a JPEG, upright, fitting
// maxSide.
func (r *Runner) Poster(ctx context.Context, in Input, out Output, maxSide, quality int) (Image, error) {
	var i Image
	return i, r.run(ctx, Job{Op: OpPoster, MaxSide: maxSide, Quality: quality}, files{in: in, out: out}, &i)
}

// Scan reads in's video's packets without decoding them, to plan its smaller
// copy (M5.4).
func (r *Runner) Scan(ctx context.Context, in Input) (Scanned, error) {
	var s Scanned
	return s, r.run(ctx, Job{Op: OpScan}, files{in: in}, &s)
}

// Encode makes a chunk of in's video's smaller copy in AV1, and writes its
// packets to out, for Mux to put together with the other chunks' (M5.4).
// Chunks run side by side, as many as Workers, at the lowest priority, as
// every job does. progress, if it's set, gets the time of each frame as
// it's encoded, in microseconds.
func (r *Runner) Encode(ctx context.Context, in Input, out Output, c Chunk, progress func(us int64)) (Encoded, error) {
	var e Encoded
	job := Job{Op: OpEncode, StartUS: c.StartUS, EndUS: c.EndUS, MaxSide: c.MaxSide, FPS: c.FPS, KBPS: c.KBPS}
	return e, r.run(ctx, job, files{in: in, out: out, progress: progress}, &e)
}

// Mux writes in's video's smaller copy to out as an MP4: the AV1 packets
// Encode wrote of its chunks, one chunk's after another in packets, width ×
// height, with in's sound and turn (M5.4).
func (r *Runner) Mux(ctx context.Context, in, packets Input, out Output, width, height int) (Muxed, error) {
	var m Muxed
	return m, r.run(ctx, Job{Op: OpMux, Width: width, Height: height}, files{in: in, out: out, packets: packets}, &m)
}

// Demux writes in's video packets to out as records, in the order a
// decoder takes them, for the page to decode with WebCodecs and make the
// video's copy itself, and says what decoding them takes (M5.5).
func (r *Runner) Demux(ctx context.Context, in Input, out Output) (Demuxed, error) {
	var d Demuxed
	return d, r.run(ctx, Job{Op: OpDemux}, files{in: in, out: out}, &d)
}

// timeoutFor gives a job time in proportion to its file: stripping runs at
// the disk's pace, several hundred MB/s, and this allows 5. A chunk of a
// video's copy gets a minute for each second it lasts: one runs a few
// times faster than the video plays on a fast desktop, and slower on a
// small laptop, at the lowest priority, behind whatever else is busy.
func timeoutFor(job Job, size int64) time.Duration {
	switch job.Op {
	case OpProbe:
		return 30*time.Second + time.Duration(size/(5<<20))*time.Second
	case OpEncode:
		seconds := min(max(job.EndUS-job.StartUS, 0)/1e6, 6*60*60)
		return 2*time.Minute + time.Duration(seconds)*time.Minute
	}
	return 2*time.Minute + time.Duration(size/(5<<20))*time.Second
}

// files are a job's files, by the numbers the driver knows them by, and
// where its progress goes.
type files struct {
	in       Input
	out      Output
	packets  Input
	progress func(int64)
}

func (f files) get(i byte) Input {
	switch {
	case i == 0:
		return f.in
	case i == 1 && f.out != nil:
		return f.out
	case i == 2 && f.packets != nil:
		return f.packets
	}
	return nil
}

func (r *Runner) run(ctx context.Context, job Job, f files, result any) error {
	sem := r.slot
	if job.Op == OpEncode {
		sem = r.chunks
	}
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-sem }()

	timeout := r.timeout(job, f.in.Size())
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	job.MemoryMB = r.MemoryMB
	job.Deadline = time.Now().Add(timeout).UnixMilli()

	cmd := exec.Command(r.command[0], r.command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &tail{max: 4 << 10}
	cmd.Stderr = stderr
	if err := r.start(cmd); err != nil {
		return fmt.Errorf("start the media worker: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill() })
	defer stop()

	d, serveErr := r.serve(newWire(stdout, stdin), job, f)
	if serveErr != nil {
		// A worker the Runner stopped listening to may be stuck writing.
		_ = cmd.Process.Kill()
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	switch {
	case ctx.Err() != nil && serveErr != nil:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &JobError{Err: ErrFailed, Reason: "it ran past its deadline"}
		}
		return ctx.Err()
	case serveErr != nil:
		reason := "the worker died"
		if waitErr != nil {
			reason += ": " + waitErr.Error()
		}
		if line := stderr.fatal(); line != "" {
			reason += ": " + line
		}
		return &JobError{Err: ErrFailed, Reason: reason}
	case d.Failed != "":
		return &JobError{Err: ErrFailed, Reason: d.Failed}
	case d.Unreadable != "":
		return &JobError{Err: ErrUnreadable, Reason: d.Unreadable}
	}
	if err := json.Unmarshal(d.Result, result); err != nil {
		return &JobError{Err: ErrFailed, Reason: "an answer that isn't JSON"}
	}
	return nil
}

// serve sends the job and answers the worker's requests until it's done.
func (r *Runner) serve(c *wire, job Job, f files) (done, error) {
	data, _ := json.Marshal(job)
	c.putU32(uint32(len(data)))
	c.put(data)
	if err := c.flush(); err != nil {
		return done{}, err
	}
	var buf []byte
	for {
		op, err := c.u8()
		if err != nil {
			return done{}, err
		}
		switch op {
		case reqRead:
			i, _ := c.u8()
			off, _ := c.i64()
			n, err := c.u32()
			if err != nil {
				return done{}, err
			}
			if n > maxFrame {
				return done{}, errFrame
			}
			got := int32(-1)
			if in := f.get(i); in != nil && off >= 0 {
				if uint32(cap(buf)) < n {
					buf = make([]byte, n)
				}
				k, err := in.ReadAt(buf[:n], off)
				if err == nil || errors.Is(err, io.EOF) {
					got = int32(k)
				}
			}
			c.putU32(uint32(got))
			if got > 0 {
				c.put(buf[:got])
			}
		case reqWrite:
			i, _ := c.u8()
			off, _ := c.i64()
			n, err := c.u32()
			if err != nil {
				return done{}, err
			}
			if buf, err = c.bytes(n, buf); err != nil {
				return done{}, err
			}
			got := int32(-1)
			if i == 1 && f.out != nil && off >= 0 {
				if k, err := f.out.WriteAt(buf, off); err == nil {
					got = int32(k)
				}
			}
			c.putU32(uint32(got))
		case reqSize:
			i, err := c.u8()
			if err != nil {
				return done{}, err
			}
			size := int64(-1)
			if in := f.get(i); in != nil {
				size = in.Size()
			}
			c.putI64(size)
		case reqLog:
			_, _ = c.i32()
			n, err := c.u32()
			if err != nil {
				return done{}, err
			}
			if buf, err = c.bytes(n, buf); err != nil {
				return done{}, err
			}
			r.log(strings.TrimSpace(string(buf)))
			continue
		case reqProgress:
			at, err := c.i64()
			if err != nil {
				return done{}, err
			}
			if f.progress != nil {
				f.progress(at)
			}
			continue
		case reqDone:
			n, err := c.u32()
			if err != nil {
				return done{}, err
			}
			if buf, err = c.bytes(n, buf); err != nil {
				return done{}, err
			}
			var d done
			if err := json.Unmarshal(buf, &d); err != nil {
				return done{}, errFrame
			}
			return d, nil
		default:
			return done{}, errFrame
		}
		if err := c.flush(); err != nil {
			return done{}, err
		}
	}
}

// tail keeps the end of what a worker writes to standard error, where Go
// reports what ended it, such as a stack overflow.
type tail struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

// fatal returns the line where Go says why it stopped, if any.
func (t *tail) fatal() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, line := range bytes.Split(t.buf, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("fatal error:")) || bytes.HasPrefix(line, []byte("runtime: ")) {
			return string(bytes.TrimSpace(line))
		}
	}
	return ""
}
