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

// A file kept under a message as that message sealed it, by its tag,
// answers only a request naming that tag. Under the same message with
// another, as the message seals it since or as the den lists another
// there, it goes, and what the den gives then is kept in its place,
// before a restart and after.
func TestFileHeldAsItsMessageSealedIt(t *testing.T) {
	f := newFixture(t)
	first, second := newDen(t, 3*chunkSize+100), newDen(t, 3*chunkSize+100)
	a, b := Owner{Message: 10, Channel: 7, As: "a"}, Owner{Message: 10, Channel: 7, As: "b"}
	if !bytes.Equal(f.readUnder(a, a, first), first.data) {
		t.Fatal("the first read")
	}
	for range 2 {
		opens := first.opens.Load()
		if !bytes.Equal(f.readUnder(a, a, first), first.data) || first.opens.Load() != opens {
			t.Error("as its message sealed it, the file came from the den")
		}
		opens = second.opens.Load()
		if !bytes.Equal(f.readUnder(b, b, second), second.data) || second.opens.Load() != opens+1 {
			t.Error("as its message seals it since, the file came from the cache")
		}
		if !bytes.Equal(f.readUnder(b, b, second), second.data) || second.opens.Load() != opens+1 {
			t.Error("what the den gave then wasn't kept in its place")
		}
		opens = first.opens.Load()
		if !bytes.Equal(f.readUnder(a, a, first), first.data) || first.opens.Load() != opens+1 {
			t.Error("as it was sealed before, the file came from the cache")
		}
		f.reopen()
	}
}

// A cache of format 4, as v1.15.0 leaves one, gains the tag's column
// where it stands: it keeps a den's state, which holds what a Dens keeps
// for good, where it synced to, and its files, which had no tag, so a
// channel's still answers and a sealed message's is fetched again.
func TestACacheOfFormat4IsKept(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 3*chunkSize+100)
	if !bytes.Equal(f.readUnder(onMessage, onMessage, d), d.data) {
		t.Fatal("the first read")
	}
	state := []byte(`{"kinds":{"7":"dm"}}`)
	if err := f.s.SaveState(denA, state); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetSynced(denA, Synced{Epoch: []byte("epoch"), Version: 9}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`ALTER TABLE files DROP COLUMN sealed_as`, `PRAGMA user_version = 4`} {
		if _, err := f.s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	f.reopen()
	var format int
	if err := f.s.db.QueryRow(`PRAGMA user_version`).Scan(&format); err != nil || format != Format {
		t.Fatalf("the cache's format is %d: %v", format, err)
	}
	if got, err := f.s.State(denA); err != nil || !bytes.Equal(got, state) {
		t.Errorf("the den's state: %q %v", got, err)
	}
	if sy, ok, err := f.s.Synced(denA); err != nil || !ok || sy.Version != 9 {
		t.Errorf("where it synced to: %+v %v %v", sy, ok, err)
	}
	opens := d.opens.Load()
	if !bytes.Equal(f.readUnder(onMessage, onMessage, d), d.data) || d.opens.Load() != opens {
		t.Error("a file kept with no tag came from the den")
	}
	sealed := Owner{Message: onMessage.Message, Channel: onMessage.Channel, As: "a"}
	if !bytes.Equal(f.readUnder(sealed, sealed, d), d.data) || d.opens.Load() != opens+1 {
		t.Error("a sealed message's file came from what was kept with no tag")
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
