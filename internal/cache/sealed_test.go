package cache

import (
	"bytes"
	"io"
	"testing"
)

// A file asked for under a message is held only if the cache kept it
// under that message, in the same channel, before a restart and after:
// under another, in another channel or under none, it comes from the den,
// which the cache doesn't keep there, and stays kept as it was.
func TestFileHeldUnderItsMessage(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 3*chunkSize+100)
	other := newDen(t, 3*chunkSize+100)
	read := func(under, owner Owner, d *den) []byte {
		t.Helper()
		h, err := f.s.File(denA, 1, false, under, owner, d.open)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()
		got, err := io.ReadAll(io.NewSectionReader(h, 0, h.Size()))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	kept := Owner{Message: 10, Channel: 7}
	if !bytes.Equal(read(kept, kept, d), d.data) {
		t.Fatal("the first read")
	}
	for range 2 {
		for _, under := range []Owner{{Message: 20, Channel: 7}, {Message: 10, Channel: 8}, {}} {
			opens := other.opens.Load()
			if !bytes.Equal(read(under, under, other), other.data) || other.opens.Load() != opens+1 {
				t.Errorf("under %+v, the file came from the cache", under)
			}
		}
		opens := d.opens.Load()
		if !bytes.Equal(read(kept, kept, d), d.data) || d.opens.Load() != opens {
			t.Error("under its message, the file came from the den")
		}
		f.reopen()
	}
}

// A miss fills the cache to the end of its sixteen chunks and no further,
// where a sealed file's run ends, so a read from a run's middle fetches
// what's left of that run alone.
func TestFillEndsWithItsRun(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 40*chunkSize)
	h := f.open(1, Owner{Message: 10, Channel: 7}, d)
	defer h.Close()
	got := make([]byte, 10)
	if _, err := h.ReadAt(got, 5*chunkSize); err != nil || !bytes.Equal(got, d.data[5*chunkSize:][:10]) {
		t.Fatal(err)
	}
	if n := d.reads.Load(); n != 11*chunkSize {
		t.Errorf("a read in chunk 5 fetched %d chunks", n/chunkSize)
	}
	if _, err := h.ReadAt(got, 17*chunkSize); err != nil || !bytes.Equal(got, d.data[17*chunkSize:][:10]) {
		t.Fatal(err)
	}
	if n := d.reads.Load(); n != 11*chunkSize+15*chunkSize {
		t.Errorf("then a read in chunk 17 fetched %d more", n/chunkSize-11)
	}
}
