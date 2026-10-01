package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Data-Corruption/dens.chat/spikes/ffmpeg/ffwasm"
)

// WASI errno values the host answers with.
const (
	errnoBadf       = 8
	errnoNosys      = 52
	errnoNotcapable = 76
)

// memory is the module's linear memory. Its capacity is reserved up front, so
// growing never moves it; pages the module never touches cost no RAM.
type memory struct {
	buf  []byte
	max  int64 // pages
	peak int64 // pages
}

func newMemory(maxBytes int64) *memory {
	return &memory{buf: make([]byte, 0, maxBytes), max: maxBytes >> 16}
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
	m.buf = m.buf[:n<<16]
	m.peak = max(m.peak, n)
	return old
}

// exitError is the module calling proc_exit.
type exitError struct{ code int32 }

func (e exitError) Error() string { return fmt.Sprintf("module exited with %d", e.code) }

// host answers the module's imports: Dens's own functions, which reach the
// input and output files, and the WASI calls wasi-libc makes, where anything
// that would open a file, a socket or a process fails.
type host struct {
	mem    *memory
	files  [2]*os.File
	log    io.Writer
	result []byte
	start  time.Time
}

func (h *host) Xmemory() ffwasm.Memory { return h.mem }

func (h *host) bytes(ptr, n int32) []byte {
	return h.mem.buf[uint32(ptr):][:uint32(n)]
}

// Dens's imports.

func (h *host) Xread(file, ptr, n int32) int32 {
	got, err := io.ReadFull(h.files[file], h.bytes(ptr, n))
	if got == 0 && err != nil && !errors.Is(err, io.EOF) {
		return -1
	}
	return int32(got)
}

func (h *host) Xwrite(file, ptr, n int32) int32 {
	got, err := h.files[file].Write(h.bytes(ptr, n))
	if err != nil {
		return -1
	}
	return int32(got)
}

func (h *host) Xseek(file int32, offset int64, whence int32) int64 {
	f := h.files[file]
	if whence == 3 {
		st, err := f.Stat()
		if err != nil {
			return -1
		}
		return st.Size()
	}
	pos, err := f.Seek(offset, int(whence))
	if err != nil {
		return -1
	}
	return pos
}

func (h *host) Xlog(level, ptr, n int32) {
	fmt.Fprintf(h.log, "ffmpeg[%d]: %s", level, h.bytes(ptr, n))
}

func (h *host) Xresult(ptr, n int32) {
	h.result = append([]byte(nil), h.bytes(ptr, n)...)
}

// WASI's imports.

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
		ptr, size := binary.LittleEndian.Uint32(iov), binary.LittleEndian.Uint32(iov[4:])
		h.log.Write(h.bytes(int32(ptr), int32(size)))
		total += size
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

func (h *host) Xfd_read(_, _, _, _ int32) int32             { return errnoBadf }
func (h *host) Xfd_seek(_ int32, _ int64, _, _ int32) int32 { return errnoBadf }
func (h *host) Xfd_close(_ int32) int32                     { return errnoBadf }
func (h *host) Xfd_fdstat_set_flags(_, _ int32) int32       { return errnoNosys }
func (h *host) Xfd_prestat_get(_, _ int32) int32            { return errnoBadf }
func (h *host) Xfd_prestat_dir_name(_, _, _ int32) int32    { return errnoBadf }
func (h *host) Xpoll_oneoff(_, _, _, _ int32) int32         { return errnoNosys }
func (h *host) Xproc_exit(code int32)                       { panic(exitError{code}) }
func (h *host) Xpath_open(_, _, _, _, _ int32, _, _ int64, _, _ int32) int32 {
	return errnoNotcapable
}
