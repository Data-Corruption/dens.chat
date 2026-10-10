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
	"strings"
	"sync"
	"time"
)

// Runner runs jobs, one at a time, each in a worker process of its own.
type Runner struct {
	command []string
	start   func(*exec.Cmd) error
	log     func(string)
	slot    chan struct{}
	timeout func(Op, int64) time.Duration

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
	return &Runner{command: command, start: start, log: log, slot: make(chan struct{}, 1), timeout: timeoutFor,
		MemoryMB: DefaultMemoryMB}
}

// Probe describes a file.
func (r *Runner) Probe(ctx context.Context, in Input) (Probed, error) {
	var p Probed
	return p, r.run(ctx, Job{Op: OpProbe}, in, nil, &p)
}

// Strip writes a copy of in's video and audio, without anything else it
// carried, to out, in the container muxer names, or the one the driver picks
// when it's empty.
func (r *Runner) Strip(ctx context.Context, in Input, out Output, muxer string) (Stripped, error) {
	var s Stripped
	return s, r.run(ctx, Job{Op: OpStrip, Muxer: muxer}, in, out, &s)
}

// Still writes in, an image, to out as a JPEG, or a PNG when it has
// transparency or is one, upright, fitting maxSide when that's above 0, at
// JPEG quality quality (2 is best). orientation is a JPEG's EXIF
// orientation, from 1 to 8, which FFmpeg doesn't read; 0 turns the image as
// the file itself says, as a HEIC does.
func (r *Runner) Still(ctx context.Context, in Input, out Output, maxSide, quality, orientation int) (Image, error) {
	var i Image
	return i, r.run(ctx, Job{Op: OpStill, MaxSide: maxSide, Quality: quality, Orientation: orientation}, in, out, &i)
}

// Poster writes in's first video frame to out as a JPEG, upright, fitting
// maxSide.
func (r *Runner) Poster(ctx context.Context, in Input, out Output, maxSide, quality int) (Image, error) {
	var i Image
	return i, r.run(ctx, Job{Op: OpPoster, MaxSide: maxSide, Quality: quality}, in, out, &i)
}

// timeoutFor gives a job time in proportion to its file: stripping runs at
// the disk's pace, several hundred MB/s, and this allows 5.
func timeoutFor(op Op, size int64) time.Duration {
	base := 2 * time.Minute
	if op == OpProbe {
		base = 30 * time.Second
	}
	return base + time.Duration(size/(5<<20))*time.Second
}

func (r *Runner) run(ctx context.Context, job Job, in Input, out Output, result any) error {
	select {
	case r.slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-r.slot }()

	timeout := r.timeout(job.Op, in.Size())
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

	d, serveErr := r.serve(newWire(stdout, stdin), job, in, out)
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
func (r *Runner) serve(c *wire, job Job, in Input, out Output) (done, error) {
	data, _ := json.Marshal(job)
	c.putU32(uint32(len(data)))
	c.put(data)
	if err := c.flush(); err != nil {
		return done{}, err
	}
	file := func(i byte) Input {
		switch {
		case i == 0:
			return in
		case i == 1 && out != nil:
			return out
		}
		return nil
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
			if f := file(i); f != nil && off >= 0 {
				if uint32(cap(buf)) < n {
					buf = make([]byte, n)
				}
				k, err := f.ReadAt(buf[:n], off)
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
			if i == 1 && out != nil && off >= 0 {
				if k, err := out.WriteAt(buf, off); err == nil {
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
			if f := file(i); f != nil {
				size = f.Size()
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
