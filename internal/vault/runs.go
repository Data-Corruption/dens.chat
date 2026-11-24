package vault

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"sync"
)

// A sealed stream's runs bind its bytes to whoever holds their digests,
// as a message binds a file it names: a run is RunChunks chunks, a
// megabyte, doubling until the stream has at most MaxRuns, and its digest
// is SHA-256 of the stream's header followed by the run's sealed chunks. A
// reader fetches a run whole and checks it before opening any of it, so a
// file still reads from any offset, a run at a time.
const (
	RunChunks = 16
	MaxRuns   = 32
	SumSize   = sha256.Size
)

// ErrRun is a run of a sealed stream that doesn't match its digest.
var ErrRun = fmt.Errorf("%w: a run doesn't match its digest", ErrSealed)

// runBytes is how many sealed bytes each run of a stream of sealedSize
// bytes holds, and how many runs it has.
func runBytes(sealedSize int64) (int64, int) {
	chunks := max((sealedSize-streamHeader+SealedChunk-1)/SealedChunk, 1)
	per := int64(RunChunks)
	for (chunks+per-1)/per > MaxRuns {
		per *= 2
	}
	return per * SealedChunk, int((chunks + per - 1) / per)
}

// SumsSize is how long the digests of a stream of sealedSize bytes are.
func SumsSize(sealedSize int64) int {
	_, n := runBytes(sealedSize)
	return n * SumSize
}

// RunSums digests a sealed stream of a known size as it's written, header
// first.
type RunSums struct {
	size, per int64
	n         int64 // bytes written
	header    []byte
	h         hash.Hash
	sums      []byte
}

// NewRunSums digests a stream of sealedSize bytes.
func NewRunSums(sealedSize int64) *RunSums {
	per, _ := runBytes(sealedSize)
	return &RunSums{size: sealedSize, per: per, header: make([]byte, 0, streamHeader)}
}

func (r *RunSums) Write(p []byte) (int, error) {
	if r.n+int64(len(p)) > r.size {
		return 0, errors.New("more written than the stream's size")
	}
	written := len(p)
	for len(p) > 0 {
		if len(r.header) < streamHeader {
			k := copy(r.header[len(r.header):streamHeader], p)
			r.header = r.header[:len(r.header)+k]
			p, r.n = p[k:], r.n+int64(k)
			continue
		}
		if r.h == nil {
			r.h = sha256.New()
			r.h.Write(r.header)
		}
		end := min(streamHeader+((r.n-streamHeader)/r.per+1)*r.per, r.size)
		k := min(int64(len(p)), end-r.n)
		r.h.Write(p[:k])
		p, r.n = p[k:], r.n+k
		if r.n == end {
			r.sums, r.h = r.h.Sum(r.sums), nil
		}
	}
	return written, nil
}

// Sums returns the digests, once the whole stream was written.
func (r *RunSums) Sums() ([]byte, error) {
	if r.n != r.size {
		return nil, errors.New("the stream was cut short")
	}
	return r.sums, nil
}

// CheckRuns reads a sealed stream of sealedSize bytes at r, a run at a
// time: each is read whole and checked against its digest in sums before
// any of it is given out, and the last kept for the reads after it. A run
// that doesn't match fails with ErrRun. The header is given out as read,
// since nothing opens under it but a run that matched it.
func CheckRuns(r io.ReaderAt, sealedSize int64, sums []byte) (io.ReaderAt, error) {
	if _, ok := OpenedSize(sealedSize); !ok || len(sums) != SumsSize(sealedSize) {
		return nil, ErrSealed
	}
	per, _ := runBytes(sealedSize)
	return &checkedRuns{r: r, size: sealedSize, per: per, sums: sums, run: -1}, nil
}

type checkedRuns struct {
	r    io.ReaderAt
	size int64
	per  int64
	sums []byte

	mu     sync.Mutex
	header []byte
	run    int64 // the run buf holds, or -1
	buf    []byte
}

func (c *checkedRuns) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for len(p) > 0 {
		if off >= c.size {
			return n, io.EOF
		}
		if err := c.readHeader(); err != nil {
			return n, err
		}
		var k int
		if off < streamHeader {
			k = copy(p, c.header[off:])
		} else {
			run := (off - streamHeader) / c.per
			if err := c.load(run); err != nil {
				return n, err
			}
			k = copy(p, c.buf[off-streamHeader-run*c.per:])
		}
		p, off, n = p[k:], off+int64(k), n+k
	}
	return n, nil
}

func (c *checkedRuns) readHeader() error {
	if c.header != nil {
		return nil
	}
	h := make([]byte, streamHeader)
	if _, err := c.r.ReadAt(h, 0); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	c.header = h
	return nil
}

// load reads run whole into c.buf and checks it.
func (c *checkedRuns) load(run int64) error {
	if c.run == run {
		return nil
	}
	c.run = -1
	at := streamHeader + run*c.per
	length := min(c.per, c.size-at)
	if int64(cap(c.buf)) < length {
		c.buf = make([]byte, length)
	}
	buf := c.buf[:length]
	if _, err := c.r.ReadAt(buf, at); err != nil && !(errors.Is(err, io.EOF) && at+length == c.size) {
		return sealedErr(err)
	}
	h := sha256.New()
	h.Write(c.header)
	h.Write(buf)
	if !bytes.Equal(h.Sum(nil), c.sums[run*SumSize:(run+1)*SumSize]) {
		return ErrRun
	}
	c.buf, c.run = buf, run
	return nil
}
