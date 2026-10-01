package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	mrand "math/rand/v2"
	"os"
	"testing"
)

func TestStreamReadsAtAnyOffset(t *testing.T) {
	v, _ := testVault(t)
	rng := mrand.New(mrand.NewPCG(1, 2))
	for _, size := range []int{0, 1, StreamChunk - 1, StreamChunk, StreamChunk + 1, 3*StreamChunk + 17, 2 * StreamChunk} {
		plain := make([]byte, size)
		_, _ = rand.Read(plain)
		sealed := sealStream(t, v, plain, []byte("file:1"))
		r, err := v.OpenStreamAt(bytes.NewReader(sealed), int64(len(sealed)), []byte("file:1"))
		if err != nil {
			t.Fatalf("%d bytes: %v", size, err)
		}
		if r.Size() != int64(size) {
			t.Fatalf("%d bytes: size %d", size, r.Size())
		}
		if got, err := io.ReadAll(io.NewSectionReader(r, 0, r.Size())); err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%d bytes: reading it through: %v", size, err)
		}
		for range 50 {
			if size == 0 {
				break
			}
			off := rng.IntN(size)
			n := rng.IntN(size - off + 1)
			got := make([]byte, n)
			if k, err := r.ReadAt(got, int64(off)); k != n || (err != nil && !errors.Is(err, io.EOF)) || !bytes.Equal(got, plain[off:off+n]) {
				t.Fatalf("%d bytes: %d at %d: read %d, %v", size, n, off, k, err)
			}
		}
		if _, err := r.ReadAt(make([]byte, 1), int64(size)); !errors.Is(err, io.EOF) {
			t.Errorf("%d bytes: reading past the end: %v", size, err)
		}
	}
}

func TestStreamReadAtRefusesTampering(t *testing.T) {
	v, _ := testVault(t)
	plain := make([]byte, 2*StreamChunk+100)
	_, _ = rand.Read(plain)
	sealed := sealStream(t, v, plain, []byte("file:1"))
	open := func(b []byte, ad string) (*StreamReaderAt, error) {
		return v.OpenStreamAt(bytes.NewReader(b), int64(len(b)), []byte(ad))
	}

	flipped := append([]byte{}, sealed...)
	flipped[streamHeader+SealedChunk+5] ^= 1
	r, err := open(flipped, "file:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAt(make([]byte, 10), 10); err != nil {
		t.Errorf("the untouched first chunk: %v", err)
	}
	if _, err := r.ReadAt(make([]byte, 10), StreamChunk+10); !errors.Is(err, ErrSealed) {
		t.Errorf("a flipped byte opened: %v", err)
	}

	// Cut after two full chunks: the second, now last, was sealed as not.
	cut := sealed[:streamHeader+2*SealedChunk]
	if r, err := open(cut, "file:1"); err != nil {
		t.Errorf("a cut file of a valid size: %v", err)
	} else if _, err := r.ReadAt(make([]byte, 10), StreamChunk+10); !errors.Is(err, ErrSealed) {
		t.Errorf("a file cut at a chunk opened its new last chunk: %v", err)
	}
	if r, err := open(sealed, "file:2"); err != nil {
		t.Fatal(err)
	} else if _, err := r.ReadAt(make([]byte, 10), 0); !errors.Is(err, ErrSealed) {
		t.Errorf("another file's associated data opened it: %v", err)
	}
	for _, size := range []int{0, streamHeader, streamHeader + 15} {
		if _, err := open(sealed[:size], "file:1"); !errors.Is(err, ErrSealed) {
			t.Errorf("%d bytes opened: %v", size, err)
		}
	}
}

func TestScratchReadsBackWhatWasWritten(t *testing.T) {
	s, err := NewScratch(t.TempDir(), "scratch-*")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rng := mrand.New(mrand.NewPCG(3, 4))
	var want []byte
	for range 400 {
		off := rng.IntN(5 * ScratchBlock)
		p := make([]byte, rng.IntN(3*ScratchBlock/2))
		_, _ = rand.Read(p)
		if _, err := s.WriteAt(p, int64(off)); err != nil {
			t.Fatal(err)
		}
		if end := off + len(p); end > len(want) {
			want = append(want, make([]byte, end-len(want))...)
		}
		copy(want[off:], p)
		if s.Size() != int64(len(want)) {
			t.Fatalf("size %d, want %d", s.Size(), len(want))
		}
		at := rng.IntN(len(want))
		got := make([]byte, rng.IntN(len(want)-at+1))
		if _, err := s.ReadAt(got, int64(at)); err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want[at:at+len(got)]) {
			t.Fatalf("%d bytes at %d differ", len(got), at)
		}
	}
	got, err := io.ReadAll(io.NewSectionReader(s, 0, s.Size()))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("reading it through: %v", err)
	}
}

func TestScratchKeepsNothingInTheClear(t *testing.T) {
	dir := t.TempDir()
	s, err := NewScratch(dir, "scratch-*")
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("a location nobody should see "), 10000)
	if _, err := s.WriteAt(plain, 0); err != nil {
		t.Fatal(err)
	}
	// Read the start back, which seals the block being written.
	if _, err := s.ReadAt(make([]byte, 1), 0); err != nil {
		t.Fatal(err)
	}
	name := s.f.Name()
	disk, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte("a location nobody")) {
		t.Error("the scratch file holds its contents in the clear")
	}
	// Gaps a later write leaves read as zeros.
	if _, err := s.WriteAt([]byte("x"), 9*ScratchBlock); err != nil {
		t.Fatal(err)
	}
	gap := make([]byte, 10)
	if _, err := s.ReadAt(gap, 7*ScratchBlock); err != nil || !bytes.Equal(gap, make([]byte, 10)) {
		t.Errorf("a gap read as %v, %v", gap, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(name); err == nil {
		t.Error("Close left the scratch file")
	}
}
