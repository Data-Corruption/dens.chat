package cache

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// den stands in for a den's file: it counts what the cache asks it for.
type den struct {
	data  []byte
	reads atomic.Int64 // bytes read
	opens atomic.Int64
	fail  atomic.Bool
}

type denSource struct {
	d      *den
	closed atomic.Bool
}

func (s *denSource) ReadAt(p []byte, off int64) (int, error) {
	if s.d.fail.Load() {
		return 0, errors.New("the den is away")
	}
	if off >= int64(len(s.d.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.d.data[off:])
	s.d.reads.Add(int64(n))
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (s *denSource) Size() int64  { return int64(len(s.d.data)) }
func (s *denSource) Close() error { s.closed.Store(true); return nil }

func (d *den) open() (Source, error) {
	if d.fail.Load() {
		return nil, errors.New("the den is away")
	}
	d.opens.Add(1)
	return &denSource{d: d}, nil
}

func newDen(t *testing.T, size int) *den {
	t.Helper()
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return &den{data: b}
}

type fixture struct {
	t   *testing.T
	dir string
	v   *vault.Vault
	log *xlog.Logger
	s   *Store
}

var denA = bytes.Repeat([]byte{0xa}, 32)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	log, err := xlog.New(filepath.Join(dir, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, vault.KeySize)
	rand.Read(key)
	v, err := vault.New(key)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, dir: filepath.Join(dir, "cache"), v: v, log: log}
	t.Cleanup(func() {
		if f.s != nil {
			f.s.Close()
		}
		v.Close()
		log.Close()
	})
	f.reopen()
	return f
}

// reopen opens the cache again, as a restarted service does.
func (f *fixture) reopen() {
	f.t.Helper()
	if f.s != nil {
		f.s.Close()
	}
	s, err := Open(f.dir, f.v, f.log, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	f.s = s
}

func (f *fixture) open(id int64, owner Owner, d *den) *File {
	f.t.Helper()
	h, err := f.s.File(denA, id, false, owner, d.open)
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

// read reads the whole of a handle at its offsets, as a player might, and
// checks it against the den's bytes.
func (f *fixture) readAll(h *File, d *den, step int) {
	f.t.Helper()
	got := make([]byte, len(d.data))
	for off := 0; off < len(got); off += step {
		end := min(off+step, len(got))
		if n, err := h.ReadAt(got[off:end], int64(off)); n != end-off || (err != nil && err != io.EOF) {
			f.t.Fatalf("read at %d: %d, %v", off, n, err)
		}
	}
	if !bytes.Equal(got, d.data) {
		f.t.Fatal("the bytes read differ from the den's")
	}
}

var onMessage = Owner{Message: 10, Channel: 3}

func TestReadsFillTheCacheAndComeBack(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 3*chunkSize+1234)
	h := f.open(1, onMessage, d)
	if h.Size() != int64(len(d.data)) {
		t.Fatalf("size %d", h.Size())
	}
	// A read in the middle fetches from there, not from the start.
	mid := make([]byte, 100)
	if _, err := h.ReadAt(mid, 2*chunkSize+50); err != nil || !bytes.Equal(mid, d.data[2*chunkSize+50:2*chunkSize+150]) {
		t.Fatalf("a read in the middle: %v", err)
	}
	if got := d.reads.Load(); got != chunkSize+1234 {
		t.Fatalf("the den was read for %d bytes, not the two chunks from there to the end", got)
	}
	f.readAll(h, d, 32<<10)
	h.Close()
	held := f.s.Used()
	if held < int64(len(d.data)) || held > int64(len(d.data))+4*(slotHead+sealOverhead) {
		t.Fatalf("the cache holds %d bytes of a %d-byte file", held, len(d.data))
	}

	// Nothing reaches the den once the cache holds the file, even after a
	// restart, and even with the den away.
	d.reads.Store(0)
	opens := d.opens.Load()
	f.reopen()
	d.fail.Store(true)
	h = f.open(1, onMessage, d)
	f.readAll(h, d, 7000)
	h.Close()
	if d.reads.Load() != 0 || d.opens.Load() != opens {
		t.Fatalf("a cached file reached the den: %d bytes, %d opens", d.reads.Load(), d.opens.Load()-opens)
	}
	if f.s.Used() != held {
		t.Fatalf("after a restart the cache counts %d bytes, not %d", f.s.Used(), held)
	}

	// Nothing of the file lies on disk in the clear.
	files, _ := os.ReadDir(filepath.Join(f.dir, filesDir))
	if len(files) != 1 {
		t.Fatalf("%d files on disk", len(files))
	}
	disk, _ := os.ReadFile(filepath.Join(f.dir, filesDir, files[0].Name()))
	if bytes.Contains(disk, d.data[:64]) || bytes.Contains(disk, d.data[2*chunkSize:2*chunkSize+64]) {
		t.Fatal("the cached file holds the den's bytes in the clear")
	}
}

func TestReadersAtOnce(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 40*chunkSize+77)
	var wg sync.WaitGroup
	for r := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := f.s.File(denA, 1, false, onMessage, d.open)
			if err != nil {
				t.Error(err)
				return
			}
			defer h.Close()
			got := make([]byte, len(d.data))
			// Each reader starts somewhere else, as seeks do.
			start := (r * 7 * chunkSize) % len(d.data)
			for off := start; off < len(got); off += 20000 {
				end := min(off+20000, len(got))
				if _, err := h.ReadAt(got[off:end], int64(off)); err != nil && err != io.EOF {
					t.Error(err)
					return
				}
			}
			if !bytes.Equal(got[start:], d.data[start:]) {
				t.Error("a reader read other bytes")
			}
		}()
	}
	wg.Wait()
	// Readers wait for one another's fetches, so each chunk came from the
	// den once.
	if got := d.reads.Load(); got != int64(len(d.data)) {
		t.Fatalf("the den was read for %d bytes of a %d-byte file", got, len(d.data))
	}
	h := f.open(1, onMessage, d)
	d.fail.Store(true)
	f.readAll(h, d, 65536)
	h.Close()
}

func TestASlotCutShortGoes(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 4*chunkSize)
	h := f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	h.Close()
	f.s.Close()
	f.s = nil
	files, _ := os.ReadDir(filepath.Join(f.dir, filesDir))
	path := filepath.Join(f.dir, filesDir, files[0].Name())
	st, _ := os.Stat(path)
	// As if the service stopped halfway through writing the last slot.
	if err := os.Truncate(path, st.Size()-1000); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	d.reads.Store(0)
	h = f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	h.Close()
	if d.reads.Load() != chunkSize {
		t.Fatalf("after a crash the den was read for %d bytes, not the chunk cut short", d.reads.Load())
	}
}

func TestAChunkThatDoesntOpenDropsItsFile(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 2*chunkSize)
	h := f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	h.Close()
	files, _ := os.ReadDir(filepath.Join(f.dir, filesDir))
	path := filepath.Join(f.dir, filesDir, files[0].Name())
	disk, _ := os.ReadFile(path)
	disk[100] ^= 1
	os.WriteFile(path, disk, 0o600)
	f.reopen()

	d.reads.Store(0)
	h = f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	h.Close()
	if d.reads.Load() == 0 {
		t.Fatal("a chunk that doesn't open was read from the cache")
	}
	if f.s.Used() != 0 {
		t.Fatalf("the broken file still counts %d bytes", f.s.Used())
	}
	if files, _ := os.ReadDir(filepath.Join(f.dir, filesDir)); len(files) != 0 {
		t.Fatalf("the broken file is still on disk: %v", files)
	}
}

func TestAnotherKeyOrFormatEmptiesTheCache(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, chunkSize)
	h := f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	h.Close()
	if err := f.s.SetSynced(denA, Synced{Epoch: []byte("epoch"), Version: 9}); err != nil {
		t.Fatal(err)
	}
	f.s.Close()
	f.s = nil

	key := make([]byte, vault.KeySize)
	rand.Read(key)
	other, _ := vault.New(key)
	defer other.Close()
	s, err := Open(f.dir, other, f.log, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Synced(denA); ok || s.Used() != 0 {
		t.Fatalf("a cache under another key was read: used %d", s.Used())
	}
	if files, _ := os.ReadDir(filepath.Join(f.dir, filesDir)); len(files) != 0 {
		t.Fatalf("the files of a cache under another key stayed: %v", files)
	}
	h, _ = s.File(denA, 1, false, onMessage, d.open)
	h.ReadAt(make([]byte, 10), 0)
	h.Close()
	if _, err := s.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(f.dir, other, f.log, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Used() != 0 {
		t.Fatalf("a cache of another format was read: used %d", s.Used())
	}
}

func TestEvictionLetsGoOfWhatWasUsedLongestAgo(t *testing.T) {
	f := newFixture(t)
	clock := time.UnixMilli(1_759_000_000_000)
	f.s.now = func() time.Time { return clock }
	f.s.limit = 8 * chunkSize * 4 // files up to 8 chunks are kept
	dens := map[int64]*den{}
	for id := int64(1); id <= 5; id++ {
		dens[id] = newDen(t, 7*chunkSize)
		clock = clock.Add(time.Minute)
		h := f.open(id, onMessage, dens[id])
		f.readAll(h, dens[id], chunkSize)
		h.Close()
	}
	// Five files of seven chunks don't fit 32: the first went.
	if f.s.Used() > f.s.limit {
		t.Fatalf("the cache holds %d of %d", f.s.Used(), f.s.limit)
	}
	held := func(id int64) bool {
		var n int
		f.s.db.QueryRow(`SELECT count(*) FROM files WHERE id = ?`, id).Scan(&n)
		return n == 1
	}
	if held(1) || !held(2) || !held(5) {
		t.Fatalf("held: 1 %v, 2 %v, 5 %v", held(1), held(2), held(5))
	}

	// A file being read stays, and the next oldest goes in its place; once
	// its reader closes, it's eligible like any other.
	clock = clock.Add(time.Minute)
	reading := f.open(2, onMessage, dens[2])
	f.readAll(reading, dens[2], chunkSize)
	f.s.mu.Lock()
	f.s.db.Exec(`UPDATE files SET used_at = 0 WHERE id = 2`)
	f.s.mu.Unlock()
	d6 := newDen(t, 7*chunkSize)
	clock = clock.Add(time.Minute)
	h := f.open(6, onMessage, d6)
	f.readAll(h, d6, chunkSize)
	h.Close()
	if !held(2) || held(3) {
		t.Fatalf("with 2 being read: 2 %v, 3 %v", held(2), held(3))
	}
	f.readAll(reading, dens[2], 3000)
	reading.Close()

	// Lowering the size lets go of more, oldest first.
	if err := f.s.SetLimit(MinLimit); err != nil {
		t.Fatal(err)
	}
	f.s.mu.Lock()
	f.s.limit = 2 * 7 * (chunkSize + slotHead + sealOverhead)
	err := f.s.evictLocked()
	f.s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !held(5) || !held(6) || held(4) || held(2) {
		t.Fatalf("after shrinking: 2 %v, 4 %v, 5 %v, 6 %v", held(2), held(4), held(5), held(6))
	}
	if files, _ := os.ReadDir(filepath.Join(f.dir, filesDir)); len(files) != 2 {
		t.Fatalf("%d files on disk, not 2", len(files))
	}
}

func TestWhatTheCacheDoesntKeep(t *testing.T) {
	f := newFixture(t)
	f.s.limit = 4 * 4 * chunkSize
	large := newDen(t, 4*chunkSize+1)
	h := f.open(1, onMessage, large)
	f.readAll(h, large, chunkSize)
	h.Close()
	// Nor a file it can't tell when to drop, on nothing it follows.
	small := newDen(t, 1000)
	h = f.open(2, Owner{}, small)
	f.readAll(h, small, 100)
	h.Close()
	if f.s.Used() != 0 {
		t.Fatalf("the cache keeps %d bytes of files it shouldn't", f.s.Used())
	}
	large.reads.Store(0)
	h = f.open(1, onMessage, large)
	f.readAll(h, large, chunkSize)
	h.Close()
	if large.reads.Load() != int64(len(large.data)) {
		t.Fatal("a file over a quarter of the cache came from it")
	}
}

func TestDrops(t *testing.T) {
	f := newFixture(t)
	type file struct {
		id    int64
		owner Owner
	}
	files := []file{
		{1, Owner{Message: 100, Channel: 7}}, {2, Owner{Message: 100, Channel: 7}}, {3, Owner{Message: 200, Channel: 7}},
		{4, Owner{Message: 300, Channel: 8}}, {5, Owner{Message: 400, Channel: 9}}, {6, Owner{Member: 50}},
	}
	keep := func() {
		for _, x := range files {
			for _, thumb := range []bool{false, true} {
				d := newDen(t, 3000)
				h, err := f.s.File(denA, x.id, thumb, x.owner, d.open)
				if err != nil {
					t.Fatal(err)
				}
				h.ReadAt(make([]byte, 10), 0)
				h.Close()
			}
		}
	}
	held := func() []int64 {
		rows, _ := f.s.db.Query(`SELECT DISTINCT id FROM files ORDER BY id`)
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var n int64
			rows.Scan(&n)
			out = append(out, n)
		}
		return out
	}
	keep()
	if n, _ := f.s.Newest(denA); n != 400 {
		t.Fatalf("the newest message with a file: %d", n)
	}
	if p, _ := f.s.Pictures(denA); len(p) != 1 || p[6] != 50 {
		t.Fatalf("pictures: %v", p)
	}
	steps := []struct {
		drop func() error
		want []int64
	}{
		{func() error { return f.s.DropMessage(denA, 100, 2) }, []int64{2, 3, 4, 5, 6}},
		{func() error { return f.s.DropFiles(denA, 6) }, []int64{2, 3, 4, 5}},
		{func() error { return f.s.DropChannel(denA, 8) }, []int64{2, 3, 5}},
		{func() error { return f.s.DropThrough(denA, 200) }, []int64{5}},
		{func() error { return f.s.DropChannelsExcept(denA, []int64{7}) }, nil},
	}
	for i, step := range steps {
		if err := step.drop(); err != nil {
			t.Fatal(err)
		}
		if got := held(); !slices.Equal(got, step.want) {
			t.Fatalf("step %d: held %v, want %v", i, got, step.want)
		}
	}
	keep()
	if err := f.s.DropMessage(denA, 400); err != nil {
		t.Fatal(err)
	}
	if got := held(); !slices.Equal(got, []int64{1, 2, 3, 4, 6}) {
		t.Fatalf("after dropping a message's files: %v", got)
	}
	if err := f.s.DropDen(denA); err != nil {
		t.Fatal(err)
	}
	if got := held(); len(got) != 0 || f.s.Used() != 0 {
		t.Fatalf("after dropping the den: %v, %d bytes", got, f.s.Used())
	}
	keep()
	if err := f.s.SetSynced(denA, Synced{Epoch: []byte("e"), Version: 3}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(held()) != 0 || f.s.Used() != 0 {
		t.Fatal("clearing left something")
	}
	if sy, ok, _ := f.s.Synced(denA); !ok || sy.Version != 3 {
		t.Fatalf("clearing forgot where the den's changes stood: %+v", sy)
	}
	if files, _ := os.ReadDir(filepath.Join(f.dir, filesDir)); len(files) != 0 {
		t.Fatalf("clearing left files on disk: %v", files)
	}
}

func TestADroppedFileBeingReadGoesWhenItsReaderCloses(t *testing.T) {
	f := newFixture(t)
	d := newDen(t, 5*chunkSize)
	h := f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	if err := f.s.DropMessage(denA, onMessage.Message); err != nil {
		t.Fatal(err)
	}
	// The reader goes on reading what it has.
	f.readAll(h, d, 9999)
	if files, _ := os.ReadDir(filepath.Join(f.dir, filesDir)); len(files) != 1 {
		t.Fatalf("%d files on disk while the reader reads", len(files))
	}
	h.Close()
	if files, _ := os.ReadDir(filepath.Join(f.dir, filesDir)); len(files) != 0 || f.s.Used() != 0 {
		t.Fatalf("after the reader closed: %v on disk, %d bytes", files, f.s.Used())
	}
	// It comes from the den again, and the cache keeps it again.
	d.reads.Store(0)
	h = f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	h.Close()
	if d.reads.Load() != int64(len(d.data)) || f.s.Used() == 0 {
		t.Fatalf("a dropped file: read %d from the den, %d cached", d.reads.Load(), f.s.Used())
	}
}

func TestSynced(t *testing.T) {
	f := newFixture(t)
	if _, ok, err := f.s.Synced(denA); ok || err != nil {
		t.Fatalf("a den never synced: %v %v", ok, err)
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(1000))
	want := Synced{Epoch: bytes.Repeat([]byte{7}, 16), Version: n.Int64()}
	if err := f.s.SetSynced(denA, want); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	got, ok, err := f.s.Synced(denA)
	if !ok || err != nil || !bytes.Equal(got.Epoch, want.Epoch) || got.Version != want.Version {
		t.Fatalf("synced %+v %v %v, want %+v", got, ok, err, want)
	}
}
