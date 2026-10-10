// Command bench runs the spike's transcoder in the translated module
// (spikes/video/aomwasm, which build.sh generates), as the media module's
// worker would, and reports how fast and in how much memory. It runs in
// this process, with the worker's host functions, but reads and writes its
// files directly, since the RPC a worker adds costs little beside encoding.
//
//	go run ./video/bench -in IN -out OUT [-side 1920] [-fps 30] [-kbps 2500] [-speed 8] [-frames 0] [-tenbit]
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"time"

	"github.com/Data-Corruption/dens.chat/spikes/video/aomwasm"
)

func main() {
	in := flag.String("in", "", "the video")
	out := flag.String("out", "", "the copy, as Matroska")
	side := flag.Int("side", 1920, "the copy's longer side at most")
	fps := flag.Int("fps", 30, "frames a second at most")
	kbps := flag.Int("kbps", 2500, "the copy's bitrate")
	speed := flag.Int("speed", 8, "libaom's cpu-used, or -1 to only decode and scale")
	frames := flag.Int("frames", 0, "the most frames to encode, 0 for all")
	tenbit := flag.Bool("tenbit", false, "a ten-bit copy")
	memMB := flag.Int("memory", 1024, "the module's memory cap, in MiB")
	flag.Parse()
	debug.SetMaxStack(64 << 20)
	src, err := os.Open(*in)
	if err != nil {
		log.Fatal(err)
	}
	dst, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	h := &host{files: [2]*os.File{src, dst}, mem: newMemory(int64(*memMB) << 20), start: time.Now()}
	if h.mem.Grow(aomwasm.MinPages, h.mem.max) < 0 {
		log.Fatal("the memory cap is below the module's minimum")
	}
	m := aomwasm.New(h, h, h)
	m.X_initialize()
	m.Xdm_init(24)
	ten := int32(0)
	if *tenbit {
		ten = 1
	}
	start := time.Now()
	ret := m.Xvs_transcode(int32(*side), int32(*fps), int32(*kbps), int32(*speed), int32(*frames), ten)
	took := time.Since(start)
	if ret < 0 {
		buf := m.Xmalloc(256)
		m.Xdm_error(ret, buf, 256)
		msg, _, _ := bytes.Cut(h.bytes(buf, 256), []byte{0})
		log.Fatalf("transcode: %s", msg)
	}
	var res map[string]any
	if err := json.Unmarshal(h.result, &res); err != nil {
		log.Fatalf("result %q: %v", h.result, err)
	}
	kept, _ := res["kept"].(float64)
	res["seconds"] = took.Seconds()
	res["fps"] = kept / took.Seconds()
	res["peak_mib"] = h.mem.peak * 64 / 1024
	data, _ := json.Marshal(res)
	fmt.Println(string(data))
}

// memory is the module's linear memory, reserved up to its cap, as the
// worker's is.
type memory struct {
	reserved []byte
	buf      []byte
	max      int64
	peak     int64
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

// host answers the module's imports: its two files directly, and the WASI
// calls wasi-libc makes as the worker answers them.
type host struct {
	files  [2]*os.File
	mem    *memory
	result []byte
	start  time.Time
}

func (h *host) Xmemory() aomwasm.Memory { return h.mem }

func (h *host) bytes(ptr, n int32) []byte { return h.mem.buf[uint32(ptr):][:uint32(n)] }

func (h *host) Xread(file, ptr, n int32, off int64) int32 {
	if file < 0 || file > 1 {
		return -1
	}
	k, err := h.files[file].ReadAt(h.bytes(ptr, n), off)
	if err != nil && !errors.Is(err, io.EOF) {
		return -1
	}
	return int32(k)
}

func (h *host) Xwrite(file, ptr, n int32, off int64) int32 {
	if file != 1 {
		return -1
	}
	k, err := h.files[1].WriteAt(h.bytes(ptr, n), off)
	if err != nil {
		return -1
	}
	return int32(k)
}

func (h *host) Xsize(file int32) int64 {
	if file < 0 || file > 1 {
		return -1
	}
	st, err := h.files[file].Stat()
	if err != nil {
		return -1
	}
	return st.Size()
}

func (h *host) Xlog(level, ptr, n int32) { fmt.Fprint(os.Stderr, string(h.bytes(ptr, n))) }

func (h *host) Xresult(ptr, n int32) { h.result = append([]byte(nil), h.bytes(ptr, n)...) }

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
	h.bytes(ptr, 1)[0] = 2
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
func (h *host) Xproc_exit(code int32)                             { panic(fmt.Sprintf("proc_exit(%d)", code)) }
func (h *host) Xpath_open(_, _, _, _, _ int32, _, _ int64, _, _ int32) int32 {
	return errnoNotcapable
}
