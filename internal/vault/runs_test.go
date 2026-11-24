package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

// countingReader counts the bytes read through it, and where reads began.
type countingReader struct {
	r    io.ReaderAt
	n    int64
	offs []int64
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n += int64(n)
	c.offs = append(c.offs, off)
	return n, err
}

// A stream's runs are sixteen chunks until there would be more than 32,
// then twice as many as it takes, up to a file of the largest size, and
// the digests a stream's writer makes check every byte of it read at any
// offset through CheckRuns.
func TestRunsOfAStream(t *testing.T) {
	for _, c := range []struct {
		chunks    int64
		runs, per int
	}{{1, 1, 16}, {16, 1, 16}, {17, 2, 16}, {512, 32, 16}, {513, 17, 32}, {1024, 32, 32}, {1025, 17, 64}, {16384, 32, 512}} {
		size := SealedSize(c.chunks * StreamChunk)
		per, n := runBytes(size)
		if n != c.runs || per != int64(c.per)*SealedChunk || SumsSize(size) != c.runs*SumSize {
			t.Errorf("%d chunks: %d runs of %d bytes", c.chunks, n, per)
		}
	}
	if _, n := runBytes(SealedSize(0)); n != 1 {
		t.Errorf("an empty stream has %d runs", n)
	}

	key := make([]byte, 32)
	rand.Read(key)
	for _, size := range []int{0, 1, StreamChunk, 16 * StreamChunk, 16*StreamChunk + 1, 40*StreamChunk + 999} {
		plain := make([]byte, size)
		rand.Read(plain)
		var sealed bytes.Buffer
		sums := NewRunSums(SealedSize(int64(size)))
		w, err := SealStreamWith(key, io.MultiWriter(&sealed, sums), []byte("ad"))
		if err != nil {
			t.Fatal(err)
		}
		w.Write(plain)
		w.Close()
		digests, err := sums.Sums()
		if err != nil || len(digests) != SumsSize(int64(sealed.Len())) {
			t.Fatalf("%d bytes: %d bytes of digests, %v", size, len(digests), err)
		}
		checked, err := CheckRuns(bytes.NewReader(sealed.Bytes()), int64(sealed.Len()), digests)
		if err != nil {
			t.Fatal(err)
		}
		r, err := OpenStreamAtWith(key, checked, int64(sealed.Len()), []byte("ad"))
		if err != nil {
			t.Fatal(err)
		}
		for _, off := range []int{size - 1, 0, size / 2, 17*StreamChunk + 5} {
			if off < 0 || off >= size {
				continue
			}
			got := make([]byte, min(5000, size-off))
			if _, err := r.ReadAt(got, int64(off)); err != nil || !bytes.Equal(got, plain[off:off+len(got)]) {
				t.Errorf("%d bytes: at %d: %v", size, off, err)
			}
		}
	}
}

// A read checks only the run it falls in, which it reads whole first,
// beside the header; a run with a byte changed, or digests of another
// stream, fail at that run alone, as ErrRun, which is ErrSealed too.
func TestCheckingRuns(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	plain := make([]byte, 50*StreamChunk)
	rand.Read(plain)
	var sealed bytes.Buffer
	sums := NewRunSums(SealedSize(int64(len(plain))))
	w, _ := SealStreamWith(key, io.MultiWriter(&sealed, sums), nil)
	w.Write(plain)
	w.Close()
	digests, _ := sums.Sums()
	blob := sealed.Bytes()

	src := &countingReader{r: bytes.NewReader(blob)}
	checked, err := CheckRuns(src, int64(len(blob)), digests)
	if err != nil {
		t.Fatal(err)
	}
	r, err := OpenStreamAtWith(key, checked, int64(len(blob)), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 100)
	if _, err := r.ReadAt(got, 35*StreamChunk); err != nil || !bytes.Equal(got, plain[35*StreamChunk:][:100]) {
		t.Fatal(err)
	}
	if run := int64(16 * SealedChunk); src.n != streamHeader+run || src.offs[len(src.offs)-1] != streamHeader+2*run {
		t.Errorf("a read in the third run read %d bytes, from %v", src.n, src.offs)
	}

	changed := bytes.Clone(blob)
	changed[streamHeader+20*SealedChunk] ^= 1
	checked, _ = CheckRuns(bytes.NewReader(changed), int64(len(changed)), digests)
	r, _ = OpenStreamAtWith(key, checked, int64(len(changed)), nil)
	if _, err := r.ReadAt(got, 3*StreamChunk); err != nil {
		t.Errorf("the first run, untouched: %v", err)
	}
	if _, err := r.ReadAt(got, 17*StreamChunk); !errors.Is(err, ErrRun) || !errors.Is(err, ErrSealed) {
		t.Errorf("the changed run: %v", err)
	}
	header := bytes.Clone(blob)
	header[5] ^= 1
	checked, _ = CheckRuns(bytes.NewReader(header), int64(len(header)), digests)
	if r, err := OpenStreamAtWith(key, checked, int64(len(header)), nil); err == nil {
		if _, err := r.ReadAt(got, 40*StreamChunk); !errors.Is(err, ErrRun) {
			t.Errorf("a changed header: %v", err)
		}
	}

	if _, err := CheckRuns(bytes.NewReader(blob), int64(len(blob)), digests[:len(digests)-SumSize]); err == nil {
		t.Error("too few digests were taken")
	}
	if _, err := CheckRuns(bytes.NewReader(blob), int64(len(blob))+1, digests); err == nil {
		t.Error("a size no stream has was taken")
	}
	short := NewRunSums(100)
	short.Write(make([]byte, 50))
	if _, err := short.Sums(); err == nil {
		t.Error("the digests of a stream cut short")
	}
	if _, err := short.Write(make([]byte, 51)); err == nil {
		t.Error("a write past the stream's size")
	}
}
