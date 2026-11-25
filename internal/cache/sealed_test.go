package cache

import (
	"bytes"
	"io"
	"testing"
)

// readUnder reads file 1 whole as the page asks for it under under, from
// the cache or from d, which the cache keeps as owner's.
func (f *fixture) readUnder(under, owner Owner, d *den) []byte {
	f.t.Helper()
	h, err := f.s.File(denA, 1, false, under, owner, d.open)
	if err != nil {
		f.t.Fatal(err)
	}
	defer h.Close()
	got, err := io.ReadAll(io.NewSectionReader(h, 0, h.Size()))
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

// A file asked for under a message is held only if the cache kept it
// under that message, in the same channel, before a restart and after:
// under another, in another channel or under none, it comes from the den,
// which the cache doesn't keep there, and stays kept as it was.
func TestFileHeldUnderItsMessage(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 3*chunkSize+100)
	other := newDen(t, 3*chunkSize+100)
	read := f.readUnder
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
