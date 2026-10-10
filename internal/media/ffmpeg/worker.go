package ffmpeg

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg/module"
)

// Job is one operation, as a Runner hands it to its worker.
type Job struct {
	Op      Op     `json:"op"`
	Muxer   string `json:"muxer,omitempty"`
	MaxSide int    `json:"max_side,omitempty"`
	Quality int    `json:"quality,omitempty"`
	// Orientation is a JPEG's EXIF orientation for a still, which FFmpeg
	// doesn't read: 1 to 8, or 0 for the turn the file itself states.
	Orientation int `json:"orientation,omitempty"`
	// A chunk of a video's copy: the frames shown from StartUS up to EndUS,
	// at most FPS a second, at KBPS (M5.4).
	StartUS int64 `json:"start_us,omitempty"`
	EndUS   int64 `json:"end_us,omitempty"`
	FPS     int   `json:"fps,omitempty"`
	KBPS    int   `json:"kbps,omitempty"`
	// Width and Height are a video copy's, which Mux writes.
	Width    int `json:"width,omitempty"`
	Height   int `json:"height,omitempty"`
	MemoryMB int `json:"memory_mb"`
	// Deadline is when the worker gives up on its own, in Unix
	// milliseconds, so one whose Runner died can't run on.
	Deadline int64 `json:"deadline"`
}

// The worker's own limits. A decoder recursing without end hits the stack
// limit, not Go's 1 GB default, and ends the worker.
const (
	workerMaxStack = 64 << 20
	exitDeadline   = 3
	avLogWarning   = 24
)

// Work is the worker process: it reads a job from in, runs it in the module,
// asking for the job's files over in and out, and reports how it went. It
// returns the process's exit code.
func Work(in io.Reader, out io.Writer) int {
	debug.SetMaxStack(workerMaxStack)
	c := newWire(in, out)
	n, err := c.u32()
	if err != nil {
		return 2
	}
	data, err := c.bytes(n, nil)
	if err != nil {
		return 2
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil || job.MemoryMB <= 0 {
		return 2
	}
	if job.Deadline > 0 {
		time.AfterFunc(time.Until(time.UnixMilli(job.Deadline)), func() { os.Exit(exitDeadline) })
	}
	h := &host{c: c, mem: newMemory(int64(job.MemoryMB) << 20), start: time.Now()}
	d := h.run(job)
	d.ModulePages = h.mem.peak
	answer, _ := json.Marshal(d)
	c.putU8(reqDone)
	c.putU32(uint32(len(answer)))
	c.put(answer)
	if c.flush() != nil {
		return 1
	}
	return 0
}

// run runs a job in a fresh module. A trap in the module is a Go panic,
// which ends the job, never quietly.
func (h *host) run(job Job) (d done) {
	defer func() {
		if r := recover(); r != nil {
			d = done{Failed: fmt.Sprintf("trap: %v", r)}
		}
	}()
	if h.mem.Grow(module.MinPages, h.mem.max) < 0 {
		return done{Failed: "the memory cap is below the module's minimum"}
	}
	m := module.New(h, h, h)
	m.X_initialize()
	m.Xdm_init(avLogWarning)
	var ret int32
	switch job.Op {
	case OpProbe:
		ret = m.Xdm_probe()
	case OpStrip:
		muxer := m.Xmalloc(int32(len(job.Muxer) + 1))
		if muxer == 0 {
			return done{Failed: "out of memory"}
		}
		copy(h.bytes(muxer, int32(len(job.Muxer))+1), job.Muxer+"\x00")
		ret = m.Xdm_strip(muxer)
	case OpStill:
		ret = m.Xdm_still(int32(job.MaxSide), int32(job.Quality), int32(job.Orientation))
	case OpPoster:
		ret = m.Xdm_poster(int32(job.MaxSide), int32(job.Quality))
	case OpScan:
		ret = m.Xdm_scan()
	case OpEncode:
		ret = m.Xdm_encode(job.StartUS, job.EndUS, int32(job.MaxSide), int32(job.FPS), int32(job.KBPS))
	case OpMux:
		ret = m.Xdm_mux(int32(job.Width), int32(job.Height))
	default:
		return done{Failed: fmt.Sprintf("no operation %q", job.Op)}
	}
	if ret < 0 {
		buf := m.Xmalloc(256)
		if buf == 0 {
			return done{Unreadable: fmt.Sprintf("error %d", ret)}
		}
		m.Xdm_error(ret, buf, 256)
		msg, _, _ := bytes.Cut(h.bytes(buf, 256), []byte{0})
		return done{Unreadable: string(msg)}
	}
	return done{Result: h.result}
}

// memory is the module's linear memory. All of it up to the cap is reserved
// up front, so growing never moves it, and pages the module never touches
// cost no RAM. The module sees buf, whose capacity is its length: the
// translation checks bulk operations (memory.fill, memory.copy and
// memory.init) against capacity, and they must trap past the end rather
// than write into pages it may grow into later.
type memory struct {
	reserved []byte
	buf      []byte
	max      int64 // pages
	peak     int64 // pages
}

func newMemory(maxBytes int64) *memory {
	r := make([]byte, maxBytes)
	return &memory{reserved: r, buf: r[:0:0], max: maxBytes >> 16}
}

func (m *memory) Slice() *[]byte { return &m.buf }

func (m *memory) Grow(delta, limit int64) int64 {
	old := int64(len(m.buf)) >> 16
	if delta == 0 {
		return old
	}
	n := old + delta
	if n > min(limit, m.max) || n < old {
		return -1
	}
	m.buf = m.reserved[: n<<16 : n<<16]
	m.peak = max(m.peak, n)
	return old
}

// host answers the module's imports: Dens's own, whose reads and writes go to
// the Runner, and the WASI calls wasi-libc makes, of which anything that
// would open a file, list a directory or reach a socket fails.
type host struct {
	c      *wire
	mem    *memory
	result []byte
	start  time.Time
}

func (h *host) Xmemory() module.Memory { return h.mem }

func (h *host) bytes(ptr, n int32) []byte {
	return h.mem.buf[uint32(ptr):][:uint32(n)]
}

// Dens's imports. A Runner that stops answering ends the job: the module's
// read or write fails, and FFmpeg gives up.

// Xread asks for at most a frame's worth: FFmpeg takes a short read as
// such, and asks again.
func (h *host) Xread(file, ptr, n int32, off int64) int32 {
	n = min(n, maxFrame)
	buf := h.bytes(ptr, n)
	h.c.putU8(reqRead)
	h.c.putU8(byte(file))
	h.c.putI64(off)
	h.c.putU32(uint32(n))
	if h.c.flush() != nil {
		return -1
	}
	got, err := h.c.i32()
	if err != nil || got > n {
		return -1
	}
	if got > 0 {
		if _, err := io.ReadFull(h.c.r, buf[:got]); err != nil {
			return -1
		}
	}
	return got
}

// Xwrite sends a large write a frame at a time: a still's JPEG is written
// whole.
func (h *host) Xwrite(file, ptr, n int32, off int64) int32 {
	for done := int32(0); done < n; {
		k := min(n-done, maxFrame)
		h.c.putU8(reqWrite)
		h.c.putU8(byte(file))
		h.c.putI64(off + int64(done))
		h.c.putU32(uint32(k))
		h.c.put(h.bytes(ptr+done, k))
		if h.c.flush() != nil {
			return -1
		}
		if got, err := h.c.i32(); err != nil || got != k {
			return -1
		}
		done += k
	}
	return n
}

func (h *host) Xsize(file int32) int64 {
	h.c.putU8(reqSize)
	h.c.putU8(byte(file))
	if h.c.flush() != nil {
		return -1
	}
	size, err := h.c.i64()
	if err != nil {
		return -1
	}
	return size
}

func (h *host) Xlog(level, ptr, n int32) {
	h.c.putU8(reqLog)
	h.c.putU32(uint32(level))
	h.c.putU32(uint32(n))
	h.c.put(h.bytes(ptr, n))
}

func (h *host) Xresult(ptr, n int32) {
	h.result = append([]byte(nil), h.bytes(ptr, n)...)
}

// Xprogress goes out with the next request, as a log line does: the module
// reads and writes often while it encodes.
func (h *host) Xprogress(done int64) {
	h.c.putU8(reqProgress)
	h.c.putI64(done)
}

// WASI's imports.

// WASI errno values the host answers with.
const (
	errnoBadf       = 8
	errnoNosys      = 52
	errnoNotcapable = 76
)

func (h *host) put32(ptr int32, v uint32) { binary.LittleEndian.PutUint32(h.bytes(ptr, 4), v) }
func (h *host) put64(ptr int32, v uint64) { binary.LittleEndian.PutUint64(h.bytes(ptr, 8), v) }

func (h *host) Xclock_time_get(id int32, _ int64, ptr int32) int32 {
	if id == 0 {
		h.put64(ptr, uint64(time.Now().UnixNano()))
	} else {
		h.put64(ptr, uint64(time.Since(h.start)))
	}
	return 0
}

func (h *host) Xenviron_get(_, _ int32) int32 { return 0 }

func (h *host) Xenviron_sizes_get(count, size int32) int32 {
	h.put32(count, 0)
	h.put32(size, 0)
	return 0
}

// Xfd_write takes what wasi-libc writes to standard output or error, which
// FFmpeg does only through its log, and drops it: the log comes through Xlog.
func (h *host) Xfd_write(fd, iovs, n, written int32) int32 {
	if fd != 1 && fd != 2 {
		return errnoBadf
	}
	total := uint32(0)
	for i := range n {
		iov := h.bytes(iovs+8*i, 8)
		total += binary.LittleEndian.Uint32(iov[4:])
	}
	h.put32(written, total)
	return 0
}

func (h *host) Xfd_fdstat_get(fd, ptr int32) int32 {
	if fd < 0 || fd > 2 {
		return errnoBadf
	}
	clear(h.bytes(ptr, 24))
	h.bytes(ptr, 1)[0] = 2 // a character device
	return 0
}

func (h *host) Xfd_read(_, _, _, _ int32) int32                   { return errnoBadf }
func (h *host) Xfd_seek(_ int32, _ int64, _, _ int32) int32       { return errnoBadf }
func (h *host) Xfd_close(_ int32) int32                           { return errnoBadf }
func (h *host) Xfd_fdstat_set_flags(_, _ int32) int32             { return errnoNosys }
func (h *host) Xfd_prestat_get(_, _ int32) int32                  { return errnoBadf }
func (h *host) Xfd_prestat_dir_name(_, _, _ int32) int32          { return errnoBadf }
func (h *host) Xfd_readdir(_, _, _ int32, _ int64, _ int32) int32 { return errnoBadf }
func (h *host) Xpath_filestat_get(_, _, _, _, _ int32) int32      { return errnoNotcapable }
func (h *host) Xpoll_oneoff(_, _, _, _ int32) int32               { return errnoNosys }
func (h *host) Xproc_exit(code int32)                             { panic(exitError{code}) }
func (h *host) Xpath_open(_, _, _, _, _ int32, _, _ int64, _, _ int32) int32 {
	return errnoNotcapable
}

// exitError is the module calling proc_exit.
type exitError struct{ code int32 }

func (e exitError) Error() string { return fmt.Sprintf("the module exited with %d", e.code) }
