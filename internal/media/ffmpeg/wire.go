package ffmpeg

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
)

// The wire between a Runner and its worker, little-endian. The Runner sends
// the job first, as a length and its JSON. Then the worker asks, and the
// Runner answers, one request at a time:
//
//	'r' file u8, offset i64, n u32           → n' i32, then n' bytes when n' > 0
//	'w' file u8, offset i64, n u32, n bytes  → n' i32
//	's' file u8                              → size i64, or -1
//	'l' level i32, n u32, n bytes            (no answer)
//	'p' done i64                             (how far the job is, no answer)
//	'd' n u32, n bytes of JSON               (the job's end, no answer)
//
// The files are numbered as the driver numbers them: 0 the job's input, 1
// its output, and 2 a video copy's packets.
const (
	reqRead     = 'r'
	reqWrite    = 'w'
	reqSize     = 's'
	reqLog      = 'l'
	reqProgress = 'p'
	reqDone     = 'd'
)

// maxFrame bounds a frame's payload: the module reads and writes through
// 64 KiB buffers, and a job or its answer is small JSON.
const maxFrame = 1 << 20

var errFrame = errors.New("malformed frame from the media worker")

// done is the worker's last word on a job.
type done struct {
	Result []byte `json:"result,omitempty"`
	// Unreadable is FFmpeg's error, for a file it couldn't read.
	Unreadable string `json:"unreadable,omitempty"`
	// Failed is what ended the job otherwise, such as a trap.
	Failed string `json:"failed,omitempty"`
	// ModulePages is the module's memory at its largest.
	ModulePages int64 `json:"module_pages"`
}

type wire struct {
	r *bufio.Reader
	w *bufio.Writer
}

func newWire(r io.Reader, w io.Writer) *wire {
	return &wire{r: bufio.NewReaderSize(r, 128<<10), w: bufio.NewWriterSize(w, 128<<10)}
}

func (c *wire) u8() (byte, error) { return c.r.ReadByte() }

func (c *wire) u32() (uint32, error) {
	var b [4]byte
	_, err := io.ReadFull(c.r, b[:])
	return binary.LittleEndian.Uint32(b[:]), err
}

func (c *wire) i32() (int32, error) {
	n, err := c.u32()
	return int32(n), err
}

func (c *wire) i64() (int64, error) {
	var b [8]byte
	_, err := io.ReadFull(c.r, b[:])
	return int64(binary.LittleEndian.Uint64(b[:])), err
}

// bytes reads a payload of n bytes into buf, which it grows as needed.
func (c *wire) bytes(n uint32, buf []byte) ([]byte, error) {
	if n > maxFrame {
		return nil, errFrame
	}
	if uint32(cap(buf)) < n {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	_, err := io.ReadFull(c.r, buf)
	return buf, err
}

func (c *wire) putU8(v byte) { _ = c.w.WriteByte(v) }

func (c *wire) putU32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	_, _ = c.w.Write(b[:])
}

func (c *wire) putI64(v int64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	_, _ = c.w.Write(b[:])
}

func (c *wire) put(p []byte) { _, _ = c.w.Write(p) }

func (c *wire) flush() error { return c.w.Flush() }
