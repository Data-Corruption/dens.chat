package cache

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// A cached file is a run of slots, each a chunk's index and the chunk
// sealed with the data key, written in the order the chunks arrive rather
// than at their place in a sparse file, which Windows would allocate in
// full. A chunk is the 64 KiB a den seals its uploads in, so a fetch
// decrypts exactly the den's own chunks.
const (
	chunkSize = vault.StreamChunk
	// slotHead is a slot's chunk index, and sealOverhead what sealing
	// adds to a chunk: a version, a nonce and a tag.
	slotHead     = 4
	sealOverhead = 1 + 24 + 16
	// fillChunks is how many chunks a miss fetches at most, a megabyte,
	// so a video playing through costs one request and one row update for
	// each.
	fillChunks = 16
)

// A Source fetches a file's bytes from its den, for the chunks the cache
// lacks.
type Source interface {
	io.ReaderAt
	Size() int64
	Close() error
}

// Owner is what a cached file is on: a message, in its channel, or a
// member's picture. It says which files go when the message changes or
// goes, the channel goes, or the picture is replaced.
type Owner struct {
	Message, Channel int64
	Member           int64
}

func (o Owner) known() bool { return o.Message != 0 || o.Member != 0 }

func nullable(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

type key struct {
	den   int64
	id    int64
	thumb bool
}

// entry is a cached file in memory: where its chunks are in its file on
// disk, which is open while handles read it.
type entry struct {
	key    key
	denID  []byte
	name   string
	size   int64
	chunks int

	// Guarded by Store.mu: the handles reading it, whether its row went
	// while they did, in which case its file goes as the last one closes,
	// and the bytes it holds on disk.
	refs   int
	doomed bool
	held   int64

	// fill lets one handle at a time fetch the chunks it lacks.
	fill sync.Mutex
	// mu guards the rest.
	mu    sync.Mutex
	f     *os.File
	slots []int64 // each chunk's slot's offset plus one, or 0 without it
	end   int64   // where the next slot goes
	// broken is set once a chunk doesn't open: the file has left the
	// cache, and its handles read from the den.
	broken bool
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, filesDir, name) }

func plainLen(size int64, i int) int64 {
	return min(chunkSize, size-int64(i)*chunkSize)
}

func slotLen(size int64, i int) int64 { return slotHead + sealOverhead + plainLen(size, i) }

// chunkAD binds a chunk to its den, its file, the file's size and its
// place, so no chunk opens anywhere else.
func chunkAD(e *entry, i int) []byte {
	ad := make([]byte, 0, 64+len(e.denID))
	ad = append(ad, "dens-cache-chunk-v1"...)
	ad = append(ad, e.denID...)
	ad = binary.BigEndian.AppendUint64(ad, uint64(e.key.id))
	thumb := byte(0)
	if e.key.thumb {
		thumb = 1
	}
	ad = append(ad, thumb)
	ad = binary.BigEndian.AppendUint64(ad, uint64(e.size))
	return binary.BigEndian.AppendUint32(ad, uint32(i))
}

func newEntry(k key, denID []byte, name string, size int64) *entry {
	n := int((size + chunkSize - 1) / chunkSize)
	return &entry{key: k, denID: bytes.Clone(denID), name: name, size: size, chunks: n, slots: make([]int64, n)}
}

// load finds the slots a file holds by reading their heads, and cuts off
// a slot cut short, as by a crash while it was written. It returns the
// bytes the file holds.
func (e *entry) load(path string) (int64, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return 0, err
	}
	var off int64
	var head [slotHead]byte
	for off+slotHead <= st.Size() {
		if _, err := f.ReadAt(head[:], off); err != nil {
			f.Close()
			return 0, err
		}
		i := int(binary.BigEndian.Uint32(head[:]))
		if i >= e.chunks || e.slots[i] != 0 || off+slotLen(e.size, i) > st.Size() {
			break
		}
		e.slots[i] = off + 1
		off += slotLen(e.size, i)
	}
	if off < st.Size() {
		if err := f.Truncate(off); err != nil {
			f.Close()
			return 0, err
		}
	}
	e.f, e.end = f, off
	return off, nil
}

// File is a handle on a file the page reads: from the cache where it holds
// the chunks, and from the den, which fills the cache, where it doesn't.
type File struct {
	s    *Store
	e    *entry // nil for a file the cache doesn't keep
	size int64

	// open reaches the den for the chunks the cache lacks, on the first
	// miss; src is what it opened.
	mu      sync.Mutex
	open    func() (Source, error)
	src     Source
	srcErr  error
	last    int    // the chunk in buf, or -1
	buf     []byte // the last chunk read, as most reads take less than one
	closeMu sync.Once
}

// Size is the file's size.
func (h *File) Size() int64 { return h.size }

// File opens a file or its preview from a den, at the cache. A file the
// cache holds, whole or in part, reads from it, and open reaches the den
// only for the chunks it lacks. A file it doesn't hold yet comes from the
// den, and is kept as it's read if it's on something the cache can
// follow, owner, and fits a quarter of the cache.
func (s *Store) File(denID []byte, id int64, thumb bool, owner Owner, open func() (Source, error)) (*File, error) {
	s.mu.Lock()
	den, err := s.denLocked(denID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	k := key{den: den, id: id, thumb: thumb}
	e, err := s.takeLocked(k, denID, owner)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if e != nil {
		return &File{s: s, e: e, size: e.size, open: open, last: -1}, nil
	}

	src, err := open()
	if err != nil {
		return nil, err
	}
	h := &File{s: s, size: src.Size(), src: src, last: -1}
	if !owner.known() || h.size == 0 || h.size > s.Limit()/4 {
		return h, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return h, nil
	}
	// Another handle may have started keeping it meanwhile.
	if e, err := s.takeLocked(k, denID, owner); err != nil || e != nil {
		if err != nil {
			src.Close()
			return nil, err
		}
		if e.size != h.size {
			s.releaseLocked(e)
			return h, nil
		}
		h.e = e
		return h, nil
	}
	e, err = s.addLocked(k, denID, h.size, owner)
	if err != nil {
		s.log.Warnf("keep a file in the cache: %v", err)
		return h, nil
	}
	h.e = e
	return h, nil
}

// takeLocked returns the cached file k, with a handle counted on it, or
// nil if the cache doesn't hold it. The caller holds s.mu.
func (s *Store) takeLocked(k key, denID []byte, owner Owner) (*entry, error) {
	if s.closed {
		return nil, nil
	}
	e := s.entries[k]
	if e == nil {
		var name string
		var size, held int64
		var message, member sql.NullInt64
		err := s.db.QueryRow(`SELECT name, size, held, message, member FROM files WHERE den = ? AND id = ? AND thumb = ?`,
			k.den, k.id, k.thumb).Scan(&name, &size, &held, &message, &member)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		e = newEntry(k, denID, name, size)
		got, err := e.load(s.path(name))
		if err != nil {
			s.log.Warnf("read a cached file: %v", err)
			return nil, s.removeLocked(name, held)
		}
		e.held = got
		if got != held {
			s.used += got - held
			if _, err := s.db.Exec(`UPDATE files SET held = ? WHERE name = ?`, got, name); err != nil {
				e.f.Close()
				return nil, err
			}
		}
		s.entries[k] = e
		if owner.known() && !message.Valid && !member.Valid {
			s.setOwnerLocked(k, owner)
		}
	} else if e.refs == 0 {
		s.idle = slices.DeleteFunc(s.idle, func(x *entry) bool { return x == e })
		e.mu.Lock()
		if e.f == nil {
			f, err := os.OpenFile(s.path(e.name), os.O_RDWR, 0)
			if err != nil {
				e.mu.Unlock()
				delete(s.entries, k)
				return nil, s.removeLocked(e.name, e.held)
			}
			e.f = f
		}
		e.mu.Unlock()
	}
	e.refs++
	if _, err := s.db.Exec(`UPDATE files SET used_at = ? WHERE name = ?`, s.now().UnixMilli(), e.name); err != nil {
		s.releaseLocked(e)
		return nil, err
	}
	return e, nil
}

func (s *Store) setOwnerLocked(k key, owner Owner) {
	if _, err := s.db.Exec(`UPDATE files SET message = ?, channel = ?, member = ? WHERE den = ? AND id = ? AND thumb = ?`,
		nullable(owner.Message), nullable(owner.Channel), nullable(owner.Member), k.den, k.id, k.thumb); err != nil {
		s.log.Warnf("note what a cached file is on: %v", err)
	}
}

// addLocked starts keeping a file the cache doesn't hold yet, with a
// handle counted on it. The caller holds s.mu.
func (s *Store) addLocked(k key, denID []byte, size int64, owner Owner) (*entry, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	name := hex.EncodeToString(b)
	f, err := os.OpenFile(s.path(name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec(`INSERT INTO files (den, id, thumb, name, size, message, channel, member, used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, k.den, k.id, k.thumb, name, size,
		nullable(owner.Message), nullable(owner.Channel), nullable(owner.Member), s.now().UnixMilli())
	if err != nil {
		f.Close()
		os.Remove(s.path(name))
		return nil, err
	}
	e := newEntry(k, denID, name, size)
	e.f, e.refs = f, 1
	s.entries[k] = e
	return e, nil
}

// removeLocked forgets a cached file no handle reads, by the name of its
// file, which no other entry has. The caller holds s.mu.
func (s *Store) removeLocked(name string, held int64) error {
	res, err := s.db.Exec(`DELETE FROM files WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.used -= held
	}
	if err := os.Remove(s.path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Warnf("remove a cached file: %v", err)
	}
	return nil
}

// releaseLocked counts a handle off an entry. The last one closes its
// file, and removes it if its row went meanwhile; an entry that stays is
// kept idle, with where its chunks are, until others push it out. The
// caller holds s.mu.
func (s *Store) releaseLocked(e *entry) {
	e.refs--
	if e.refs > 0 {
		return
	}
	e.mu.Lock()
	if e.f != nil {
		e.f.Close()
		e.f = nil
	}
	e.mu.Unlock()
	if e.doomed {
		if err := os.Remove(s.path(e.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warnf("remove a cached file: %v", err)
		}
		return
	}
	s.idle = append(s.idle, e)
	if len(s.idle) > idleEntries {
		delete(s.entries, s.idle[0].key)
		s.idle = s.idle[1:]
	}
}

// ReadAt reads the file at off, from the chunks the cache holds and, for
// the rest, from the den.
func (h *File) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= h.size {
		return 0, io.EOF
	}
	if h.e == nil {
		return h.src.ReadAt(p, off)
	}
	n := 0
	for n < len(p) && off < h.size {
		i := int(off / chunkSize)
		data, err := h.chunk(i)
		if err != nil {
			return n, err
		}
		k := copy(p[n:], data[off-int64(i)*chunkSize:])
		n += k
		off += int64(k)
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// chunk returns chunk i's bytes: from the cache, or from the den, which
// fills the cache with it and the chunks after it that it lacks.
func (h *File) chunk(i int) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last == i {
		return h.buf, nil
	}
	e := h.e
	if data, ok, err := h.cached(i); err != nil || ok {
		return h.keep(i, data), err
	}
	e.fill.Lock()
	defer e.fill.Unlock()
	// Another handle may have fetched it while this one waited.
	if data, ok, err := h.cached(i); err != nil || ok {
		return h.keep(i, data), err
	}
	src, err := h.source()
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	broken := e.broken
	n := 1
	for !broken && i+n < e.chunks && n < fillChunks && e.slots[i+n] == 0 {
		n++
	}
	e.mu.Unlock()
	from := int64(i) * chunkSize
	span := make([]byte, min(int64(n)*chunkSize, e.size-from))
	if _, err := src.ReadAt(span, from); err != nil && !(errors.Is(err, io.EOF) && from+int64(len(span)) == e.size) {
		return nil, err
	}
	if !broken {
		h.s.store(e, i, span)
	}
	return h.keep(i, span[:plainLen(e.size, i)]), nil
}

func (h *File) keep(i int, data []byte) []byte {
	if data != nil {
		h.last, h.buf = i, data
	}
	return data
}

// cached reads chunk i from the cache, and reports whether it holds it. A
// chunk that doesn't open takes the file out of the cache, and its handles
// read from the den from then on.
func (h *File) cached(i int) ([]byte, bool, error) {
	e := h.e
	e.mu.Lock()
	if e.broken || e.slots[i] == 0 {
		e.mu.Unlock()
		return nil, false, nil
	}
	at, f := e.slots[i]-1, e.f
	e.mu.Unlock()
	slot := make([]byte, slotLen(e.size, i))
	if _, err := f.ReadAt(slot, at); err != nil {
		return nil, false, h.s.broke(e, err)
	}
	data, err := h.s.v.Open(slot[slotHead:], chunkAD(e, i))
	if err != nil || binary.BigEndian.Uint32(slot) != uint32(i) {
		return nil, false, h.s.broke(e, errors.New("a chunk doesn't open"))
	}
	return data, true, nil
}

// broke takes a file whose chunk couldn't be read out of the cache, and
// leaves its handles reading from the den.
func (s *Store) broke(e *entry, cause error) error {
	s.log.Warnf("Dropped a cached file: %v", cause)
	e.mu.Lock()
	e.broken = true
	e.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropEntriesLocked([]droppedFile{{key: e.key, name: e.name, held: e.held}})
}

// store seals the chunks span holds from chunk i on, the ones the cache
// lacks, and appends them to the file. A write that fails leaves the cache
// as it was, short of what it couldn't write: the page has its bytes
// either way.
func (s *Store) store(e *entry, i int, span []byte) {
	s.mu.Lock()
	doomed := e.doomed
	s.mu.Unlock()
	if doomed {
		return
	}
	var added int64
	for j := 0; int64(j)*chunkSize < int64(len(span)); j++ {
		c := i + j
		e.mu.Lock()
		have, at, f := e.slots[c] != 0 || e.broken, e.end, e.f
		e.mu.Unlock()
		if have || f == nil {
			continue
		}
		plain := span[int64(j)*chunkSize : min(int64(j+1)*chunkSize, int64(len(span)))]
		sealed, err := s.v.Seal(plain, chunkAD(e, c))
		if err != nil {
			s.log.Warnf("seal a chunk for the cache: %v", err)
			return
		}
		slot := binary.BigEndian.AppendUint32(make([]byte, 0, slotHead+len(sealed)), uint32(c))
		slot = append(slot, sealed...)
		if _, err := f.WriteAt(slot, at); err != nil {
			s.log.Warnf("write a chunk to the cache: %v", err)
			break
		}
		e.mu.Lock()
		e.slots[c], e.end = at+1, at+int64(len(slot))
		e.mu.Unlock()
		added += int64(len(slot))
	}
	if added == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.doomed {
		return
	}
	e.held += added
	s.used += added
	if _, err := s.db.Exec(`UPDATE files SET held = ? WHERE name = ?`, e.held, e.name); err != nil {
		s.log.Warnf("count a cached file's chunks: %v", err)
	}
	if err := s.evictLocked(); err != nil {
		s.log.Warnf("make room in the cache: %v", err)
	}
}

// source reaches the den for the file, once. The caller holds h.mu.
func (h *File) source() (Source, error) {
	if h.src == nil && h.srcErr == nil {
		h.src, h.srcErr = h.open()
		if h.srcErr == nil && h.src.Size() != h.size {
			// A file's bytes never change under its ID.
			h.src.Close()
			h.src, h.srcErr = nil, errors.New("the den's answer is malformed")
		}
	}
	return h.src, h.srcErr
}

// Close ends the handle.
func (h *File) Close() error {
	var err error
	h.closeMu.Do(func() {
		h.mu.Lock()
		if h.src != nil {
			err = h.src.Close()
		}
		h.mu.Unlock()
		if h.e != nil {
			h.s.mu.Lock()
			h.s.releaseLocked(h.e)
			h.s.mu.Unlock()
		}
	})
	return err
}

// evictLocked lets go of what was used longest ago until the cache fits
// its size: a file, skipping those being read, or a channel's messages,
// whichever was used first. The caller holds s.mu.
func (s *Store) evictLocked() error {
	for s.used > s.limit {
		var file *droppedFile
		var fileUsed int64
		rows, err := s.db.Query(`SELECT den, id, thumb, name, held, used_at FROM files ORDER BY used_at LIMIT 64`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var d droppedFile
			var used int64
			if err := rows.Scan(&d.key.den, &d.key.id, &d.key.thumb, &d.name, &d.held, &used); err != nil {
				rows.Close()
				return err
			}
			if e := s.entries[d.key]; e != nil && e.refs > 0 {
				continue
			}
			file, fileUsed = &d, used
			break
		}
		rows.Close()
		var den, channel, chanUsed int64
		err = s.db.QueryRow(`SELECT den, channel, used_at FROM channels ORDER BY used_at LIMIT 1`).Scan(&den, &channel, &chanUsed)
		hasChannel := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		switch {
		case hasChannel && (file == nil || chanUsed <= fileUsed):
			if err := s.dropMessagesLocked(den, `channel = ?`, channel); err != nil {
				return err
			}
		case file != nil:
			if err := s.dropEntriesLocked([]droppedFile{*file}); err != nil {
				return err
			}
		default:
			return nil // everything left is being read
		}
	}
	return nil
}

type droppedFile struct {
	key  key
	name string
	held int64
}

// dropLocked forgets the cached files a condition on the files table
// selects. The caller holds s.mu.
func (s *Store) dropLocked(where string, args ...any) error {
	rows, err := s.db.Query(`SELECT den, id, thumb, name, held FROM files WHERE `+where, args...)
	if err != nil {
		return err
	}
	var out []droppedFile
	for rows.Next() {
		var d droppedFile
		if err := rows.Scan(&d.key.den, &d.key.id, &d.key.thumb, &d.name, &d.held); err != nil {
			rows.Close()
			return err
		}
		out = append(out, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return s.dropEntriesLocked(out)
}

// dropEntriesLocked forgets cached files. One a handle reads leaves the
// cache at once, and its file goes when the last handle closes, since
// Windows won't delete an open file. The caller holds s.mu.
func (s *Store) dropEntriesLocked(files []droppedFile) error {
	for _, d := range files {
		e := s.entries[d.key]
		if e != nil && e.name != d.name {
			e = nil // an entry since started again under another name
		}
		if e == nil || e.refs == 0 {
			if e != nil {
				delete(s.entries, d.key)
				s.idle = slices.DeleteFunc(s.idle, func(x *entry) bool { return x == e })
			}
			if err := s.removeLocked(d.name, d.held); err != nil {
				return err
			}
			continue
		}
		if _, err := s.db.Exec(`DELETE FROM files WHERE name = ?`, d.name); err != nil {
			return err
		}
		s.used -= e.held
		e.doomed, e.held = true, 0
		delete(s.entries, d.key)
	}
	return nil
}

// denNumber returns a den's number in the cache, and false if the cache
// holds nothing of it. The caller holds s.mu.
func (s *Store) denNumberLocked(denID []byte) (int64, bool, error) {
	if n, ok := s.dens[string(denID)]; ok {
		return n, true, nil
	}
	var n int64
	err := s.db.QueryRow(`SELECT den FROM dens WHERE den_id = ?`, denID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	s.dens[string(denID)] = n
	return n, true, nil
}

// drop forgets a den's cached files that a condition selects.
func (s *Store) drop(denID []byte, where string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return err
	}
	return s.dropLocked(`den = ? AND (`+where+`)`, append([]any{den}, args...)...)
}

// DropFiles forgets files of a den, and their previews.
func (s *Store) DropFiles(denID []byte, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	return s.drop(denID, `id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, anys(ids)...)
}

// DropChannel forgets a channel's messages, and the files on them.
func (s *Store) DropChannel(denID []byte, channel int64) error {
	if err := s.drop(denID, `channel = ?`, channel); err != nil {
		return err
	}
	return s.dropMessages(denID, `channel = ?`, channel)
}

// DropChannelsExcept forgets the messages, and the files on them, of
// channels other than these, as after a ready that no longer lists the
// rest.
func (s *Store) DropChannelsExcept(denID []byte, channels []int64) error {
	if len(channels) == 0 {
		if err := s.drop(denID, `channel IS NOT NULL`); err != nil {
			return err
		}
		return s.dropMessages(denID, "")
	}
	in := `channel NOT IN ` + channelsIn(channels)
	if err := s.drop(denID, `channel IS NOT NULL AND `+in, anys(channels)...); err != nil {
		return err
	}
	return s.dropMessages(denID, in, anys(channels)...)
}

// dropMessages forgets a den's messages in the channels a condition
// selects, or all of them with none.
func (s *Store) dropMessages(denID []byte, where string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return err
	}
	return s.dropMessagesLocked(den, where, args...)
}

// DropThrough forgets the files on messages up to one, as a retention
// pass deletes them.
func (s *Store) DropThrough(denID []byte, message int64) error {
	return s.drop(denID, `message <= ?`, message)
}

// DropMessage forgets the files on a message other than those it still
// has, keep, or all of them, as when it's deleted.
func (s *Store) DropMessage(denID []byte, message int64, keep ...int64) error {
	if len(keep) == 0 {
		return s.drop(denID, `message = ?`, message)
	}
	return s.drop(denID, `message = ? AND id NOT IN (?`+strings.Repeat(", ?", len(keep)-1)+`)`, append([]any{message}, anys(keep)...)...)
}

// Pictures returns the members' pictures the cache holds of a den: each
// file's member.
func (s *Store) Pictures(denID []byte) (map[int64]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int64]int64{}
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return out, err
	}
	rows, err := s.db.Query(`SELECT id, member FROM files WHERE den = ? AND member IS NOT NULL`, den)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, member int64
		if err := rows.Scan(&id, &member); err != nil {
			return nil, err
		}
		out[id] = member
	}
	return out, rows.Err()
}

// Holds reports whether the cache holds anything of a den.
func (s *Store) Holds(denID []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	den, ok, err := s.denNumberLocked(denID)
	if err != nil || !ok {
		return false, err
	}
	var any bool
	err = s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM files WHERE den = ?)`, den).Scan(&any)
	return any, err
}

func anys(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
