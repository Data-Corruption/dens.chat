package den

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Uploads: the files members attach to messages and put on their
// profiles.
//
// Clients take metadata out of images before uploading. The den checks
// that nothing is left and refuses a file that still has some, so no
// location reaches its disk; then it makes the image's preview from what
// it stored. Video and audio it strips again itself (media.go). Files are
// sealed with the data key, in chunks, under random names only their rows
// know, and their names are sealed like message text.
const (
	// uploadExpiry is how long an upload waits for a message or a profile
	// to use it.
	uploadExpiry = time.Hour
	// freeSpaceFloor is the room uploads leave on the den's disk, which
	// the database and the system need too.
	freeSpaceFloor = 1 << 30
	partPrefix     = "upload-"
	thumbSuffix    = ".thumb"
)

var errFileNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such file")

// fileStore keeps uploads on disk.
type fileStore struct {
	dir, temp string
	// decode lets one image decode at a time: a large photo takes
	// hundreds of megabytes while its preview is made.
	decode chan struct{}
	// freeSpace is host.FreeSpace; tests replace it.
	freeSpace func(string) (uint64, error)
	media     *ffmpeg.Runner

	mu    sync.Mutex
	sweep *time.Timer
	next  time.Time
}

func newFileStore(s Storage) *fileStore {
	return &fileStore{dir: s.Dir, temp: s.Temp, decode: make(chan struct{}, 1), freeSpace: host.FreeSpace, media: s.Media}
}

func blobName(blob []byte, thumb bool) string {
	if thumb {
		return hex.EncodeToString(blob) + thumbSuffix
	}
	return hex.EncodeToString(blob)
}

// blobAD binds a sealed file to its name, so one can't stand in for
// another.
func blobAD(blob []byte, thumb bool) []byte {
	return []byte("den_files.blob:" + blobName(blob, thumb))
}

func fileNameAD(id int64) []byte { return []byte("den_files.name:" + strconv.FormatInt(id, 10)) }

// part is a file being stored: sealed into a temporary file, and moved
// into place once the whole upload checks out.
type part struct {
	f     *os.File
	seal  io.WriteCloser
	blob  []byte
	thumb bool
	dest  string
	moved bool // renamed into place
	kept  bool // its upload is stored, so it stays
}

func (s *fileStore) create(v *vault.Vault, blob []byte, thumb bool) (*part, error) {
	f, err := os.CreateTemp(s.temp, partPrefix+"*")
	if err != nil {
		return nil, err
	}
	w, err := v.SealStream(f, blobAD(blob, thumb))
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return &part{f: f, seal: w, blob: blob, thumb: thumb, dest: filepath.Join(s.dir, blobName(blob, thumb))}, nil
}

func (p *part) Write(b []byte) (int, error) { return p.seal.Write(b) }

// finish seals the last chunk and puts the file on disk.
func (p *part) finish() error {
	if err := p.seal.Close(); err != nil {
		return err
	}
	if err := p.f.Sync(); err != nil {
		return err
	}
	return p.f.Close()
}

// open reads a finished part back.
func (p *part) open(v *vault.Vault) (io.ReadCloser, error) {
	f, err := os.Open(p.f.Name())
	if err != nil {
		return nil, err
	}
	return readCloser{v.OpenStream(f, blobAD(p.blob, p.thumb)), f}, nil
}

// openAt reads a finished part back at any offset; closer closes it.
func (p *part) openAt(v *vault.Vault) (*vault.StreamReaderAt, io.Closer, error) {
	return openSealedAt(v, p.f.Name(), blobAD(p.blob, p.thumb))
}

func openSealedAt(v *vault.Vault, name string, ad []byte) (*vault.StreamReaderAt, io.Closer, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	r, err := v.OpenStreamAt(f, st.Size(), ad)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return r, f, nil
}

// move puts a finished part where stored files live.
func (p *part) move() error {
	if err := os.Rename(p.f.Name(), p.dest); err != nil {
		return err
	}
	p.moved = true
	return nil
}

// abort drops a part unless its upload was stored.
func (p *part) abort() {
	if p.kept {
		return
	}
	p.f.Close()
	if p.moved {
		os.Remove(p.dest)
	} else {
		os.Remove(p.f.Name())
	}
}

type readCloser struct {
	io.Reader
	io.Closer
}

// StoredFile is a stored file opened to read anywhere in it, as a player
// seeking through a video does: only the chunks a read covers are opened.
type StoredFile struct {
	*io.SectionReader
	io.Closer
}

// open reads a stored file.
func (s *fileStore) open(v *vault.Vault, blob []byte, thumb bool) (StoredFile, error) {
	r, closer, err := openSealedAt(v, filepath.Join(s.dir, blobName(blob, thumb)), blobAD(blob, thumb))
	if err != nil {
		return StoredFile{}, err
	}
	return StoredFile{io.NewSectionReader(r, 0, r.Size()), closer}, nil
}

// remove deletes stored files and their previews. Their rows are already
// gone, so a file left behind only takes space until the next start.
func (s *fileStore) remove(blobs [][]byte) {
	for _, blob := range blobs {
		_ = os.Remove(filepath.Join(s.dir, blobName(blob, false)))
		_ = os.Remove(filepath.Join(s.dir, blobName(blob, true)))
	}
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func tooLarge(l denproto.Limits) error {
	return denproto.Errorf(http.StatusRequestEntityTooLarge, denproto.CodeTooLarge, "files are at most %d bytes on this den", l.FileSize)
}

// usage is how much space a member's files take, and everyone's.
func usage(ctx context.Context, q querier, member int64) (denproto.Storage, error) {
	var st denproto.Storage
	err := q.QueryRowContext(ctx, `SELECT
		coalesce(sum(size + coalesce(thumb_size, 0)) FILTER (WHERE uploader_id = ?), 0),
		coalesce(sum(size + coalesce(thumb_size, 0)), 0) FROM den_files`, member).Scan(&st.Used, &st.DenUsed)
	return st, err
}

// checkSpace refuses need more bytes past a member's or the den's limit.
func checkSpace(l denproto.Limits, st denproto.Storage, need int64) error {
	if st.Used+need > l.MemberStorage {
		return denproto.Errorf(http.StatusInsufficientStorage, denproto.CodeQuotaExceeded, "the member's files have used their space")
	}
	if st.DenUsed+need > l.DenStorage {
		return denproto.Errorf(http.StatusInsufficientStorage, denproto.CodeDenFull, "the den's files have used its space")
	}
	return nil
}

// Storage reports how much space the member's files take, and everyone's.
func (d *Den) Storage(ctx context.Context, s *Session) (denproto.Storage, error) {
	return usage(ctx, d.db, s.MemberID)
}

// sniffType names a file that isn't an image, for clients to pick an
// icon. It never makes a file show inline: only the image kinds do.
func sniffType(head []byte) string {
	t, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if err != nil || denproto.CheckMediaType(t) != nil {
		return "application/octet-stream"
	}
	return t
}

// Upload stores a file a member sends, for a message or their profile to
// use within the hour. size is the request's length, or -1 when it
// doesn't say; the body is read up to the den's file size limit and no
// further. A sealed upload is a DM's file, which the member's client
// sealed: the den can't tell what it is, and keeps it as it came.
//
// An upload that replaces one of the member's files in use, by its ID,
// counts against the space that file takes, so a member at their limit
// can swap a file for a smaller one (M5). It takes only that file's place,
// and a newer one made for the same file drops it.
func (d *Den) Upload(ctx context.Context, s *Session, name string, size int64, body io.Reader, sealed bool, replaces string) (denproto.File, error) {
	info, _ := d.Info()
	limits := info.Limits
	if size > limits.FileSize {
		return denproto.File{}, tooLarge(limits)
	}
	var replaced sql.NullInt64
	if replaces != "" {
		rid, err := denproto.ParseID(replaces)
		if err != nil {
			return denproto.File{}, invalid("%s: no such file of yours in use", denproto.HeaderReplaces)
		}
		replaced = sql.NullInt64{Int64: rid, Valid: true}
	}
	st, err := usage(ctx, d.db, s.MemberID)
	if err != nil {
		return denproto.File{}, err
	}
	credit, err := replacedSpace(ctx, d.db, s.MemberID, replaced, sealed)
	if err != nil {
		return denproto.File{}, err
	}
	if err := checkSpace(limits, st, max(size, 0)-credit); err != nil {
		return denproto.File{}, err
	}
	if free, err := d.files.freeSpace(d.files.dir); err != nil {
		return denproto.File{}, err
	} else if int64(free) < max(size, 0)+freeSpaceFloor {
		d.log.Warnf("Uploads refused: the disk holding the den's files is nearly full")
		return denproto.File{}, denproto.Errorf(http.StatusInsufficientStorage, denproto.CodeDenFull, "the den's disk is nearly full")
	}

	limited := &io.LimitedReader{R: body, N: limits.FileSize + 1}
	blob := denproto.Random(16)
	orig, err := d.files.create(d.v, blob, false)
	if err != nil {
		return denproto.File{}, err
	}
	defer orig.abort()
	n := &counter{w: orig}
	var f denproto.File
	var kind media.Kind
	var res media.Result
	if sealed {
		f = denproto.File{Type: denproto.SealedType, Sealed: true}
		if _, err := io.Copy(n, limited); err != nil {
			return denproto.File{}, err
		}
		if limited.N == 0 {
			return denproto.File{}, tooLarge(limits)
		}
	} else {
		br := bufio.NewReaderSize(limited, media.SniffLen)
		head, err := br.Peek(media.SniffLen)
		if err != nil && !errors.Is(err, io.EOF) {
			return denproto.File{}, err
		}
		kind = media.Sniff(head)
		if err := d.files.refuse(kind, head); err != nil {
			return denproto.File{}, err
		}
		f = denproto.File{Name: denproto.CleanFilename(name), Type: kind.MIME()}
		if f.Type == "" {
			f.Type = sniffType(head)
		}
		res, err = media.Verify(n, br, kind)
		switch {
		case limited.N == 0:
			return denproto.File{}, tooLarge(limits)
		case errors.Is(err, media.ErrMetadata):
			return denproto.File{}, invalid("the file still carries metadata, which clients take out before uploading")
		case errors.Is(err, media.ErrMalformed):
			return denproto.File{}, invalid("the file is damaged, or isn't what it starts out as")
		case err != nil:
			return denproto.File{}, err
		}
	}
	if err := orig.finish(); err != nil {
		return denproto.File{}, err
	}
	f.Size = n.n

	stored := orig
	var thumb *part
	var thumbSize int64
	switch {
	case sealed:
	case kind.Image():
		f.Width, f.Height, f.Animated = res.Width, res.Height, res.Animated
		if thumb, f.Thumb, thumbSize, err = d.preview(ctx, orig, blob, kind, res); err != nil {
			return denproto.File{}, err
		}
	case kind == media.Video || kind == media.Audio:
		if stored, thumb, thumbSize, err = d.restrip(ctx, orig, blob, limits, &f); err != nil {
			return denproto.File{}, err
		}
		defer stored.abort()
	}
	if thumb != nil {
		defer thumb.abort()
	}

	now := d.now()
	var id int64
	var dropped [][]byte
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if replaced.Valid {
			// A file has one replacement waiting at most.
			if dropped, err = deleteFiles(ctx, tx, `replaces = ? AND uploader_id = ? AND message_id IS NULL`, replaced, s.MemberID); err != nil {
				return err
			}
		}
		// Other uploads may have finished meanwhile, and the file this one
		// replaces may have gone.
		st, err := usage(ctx, tx, s.MemberID)
		if err != nil {
			return err
		}
		credit, err := replacedSpace(ctx, tx, s.MemberID, replaced, sealed)
		if err != nil {
			return err
		}
		if err := checkSpace(limits, st, f.Size+thumbSize-credit); err != nil {
			return err
		}
		var tw, th, ts any
		if f.Thumb != nil {
			tw, th, ts = f.Thumb.Width, f.Thumb.Height, thumbSize
		}
		var w, h, duration any
		if f.Width > 0 {
			w, h = f.Width, f.Height
		}
		if f.Duration > 0 {
			duration = f.Duration
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO den_files
			(blob, uploader_id, name, type, size, width, height, animated, thumb_width, thumb_height, thumb_size, duration_ms,
			sealed, created_at, replaces)
			VALUES (?, ?, x'', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			blob, s.MemberID, f.Type, f.Size, w, h, f.Animated, tw, th, ts, duration, sealed, now.UnixMilli(), replaced)
		if err != nil {
			return err
		}
		if id, err = result.LastInsertId(); err != nil {
			return err
		}
		sealed, err := d.v.Seal([]byte(f.Name), fileNameAD(id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_files SET name = ? WHERE id = ?`, sealed, id); err != nil {
			return err
		}
		// Moved in last, so a failure before leaves only temporary files,
		// and one after, in the commit, leaves files no row names, which
		// the next start clears.
		if err := stored.move(); err != nil {
			return err
		}
		if thumb != nil {
			return thumb.move()
		}
		return nil
	})
	if err != nil {
		return denproto.File{}, err
	}
	stored.kept = true
	if thumb != nil {
		thumb.kept = true
	}
	d.files.remove(dropped)
	f.ID = denproto.FormatID(id)
	d.files.scheduleSweep(d, now.Add(uploadExpiry))
	return f, nil
}

// replacedSpace is the space the file an upload replaces takes, which the
// upload counts against: one of the member's files, in use on a message or
// their profile, sealed or not as the upload is. Without one it's 0.
func replacedSpace(ctx context.Context, q querier, member int64, replaced sql.NullInt64, sealed bool) (int64, error) {
	if !replaced.Valid {
		return 0, nil
	}
	var space int64
	err := q.QueryRowContext(ctx, `SELECT size + coalesce(thumb_size, 0) FROM den_files
		WHERE id = ? AND uploader_id = ? AND sealed = ? AND NOT (`+unused+`)`, replaced.Int64, member, sealed).Scan(&space)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, invalid("%s: no such file of yours in use", denproto.HeaderReplaces)
	}
	return space, err
}

// DropUpload deletes one of the member's uploads waiting to be used, which
// frees its space at once rather than within the hour (M5).
func (d *Den) DropUpload(ctx context.Context, s *Session, id string) error {
	fid, err := denproto.ParseID(id)
	if err != nil {
		return errFileNotFound
	}
	blobs, err := deleteFiles(ctx, d.db, `id = ? AND uploader_id = ? AND `+unused, fid, s.MemberID)
	if err != nil {
		return err
	}
	if len(blobs) == 0 {
		return errFileNotFound
	}
	d.files.remove(blobs)
	return nil
}

// preview makes an image's preview from what was stored, one image at a
// time. An image too large to decode, or one the decoders can't read,
// goes without.
func (d *Den) preview(ctx context.Context, orig *part, blob []byte, kind media.Kind, res media.Result) (*part, *denproto.Thumb, int64, error) {
	if !media.CanPreview(res) {
		return nil, nil, 0, nil
	}
	select {
	case d.files.decode <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, 0, ctx.Err()
	}
	defer func() { <-d.files.decode }()
	r, err := orig.open(d.v)
	if err != nil {
		return nil, nil, 0, err
	}
	th, err := media.Thumbnail(r, kind, res)
	r.Close()
	if errors.Is(err, media.ErrMalformed) || errors.Is(err, media.ErrTooBig) {
		d.log.Infof("An upload has no preview: %v", err)
		return nil, nil, 0, nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	p, err := d.files.create(d.v, blob, true)
	if err != nil {
		return nil, nil, 0, err
	}
	if _, err := p.Write(th.Data); err != nil {
		p.abort()
		return nil, nil, 0, err
	}
	if err := p.finish(); err != nil {
		p.abort()
		return nil, nil, 0, err
	}
	return p, &denproto.Thumb{Width: th.Width, Height: th.Height}, int64(len(th.Data)), nil
}

// OpenFile opens a file, or its preview, for a member who may see it:
// anyone who can see the message it's on, anyone in the den for a picture
// on a profile, and only its uploader while it waits to be used.
func (d *Den) OpenFile(ctx context.Context, s *Session, id string, thumb bool) (StoredFile, error) {
	none := StoredFile{}
	fid, err := denproto.ParseID(id)
	if err != nil {
		return none, errFileNotFound
	}
	var blob []byte
	var size, uploader int64
	var thumbSize, channel sql.NullInt64
	var profile bool
	err = d.db.QueryRowContext(ctx, `SELECT f.blob, f.size, f.thumb_size, f.uploader_id, m.channel_id,
		EXISTS (SELECT 1 FROM den_members WHERE avatar_id = f.id OR banner_id = f.id)
		FROM den_files f LEFT JOIN den_messages m ON m.id = f.message_id WHERE f.id = ?`, fid).
		Scan(&blob, &size, &thumbSize, &uploader, &channel, &profile)
	if errors.Is(err, sql.ErrNoRows) {
		return none, errFileNotFound
	}
	if err != nil {
		return none, err
	}
	switch {
	case channel.Valid:
		c, err := d.channel(ctx, d.db, channel.Int64)
		if err != nil || !visible(c, s.MemberID, IsStaff(s.Role)) {
			return none, errFileNotFound
		}
	case profile, uploader == s.MemberID:
	default:
		return none, errFileNotFound
	}
	if thumb {
		if !thumbSize.Valid {
			return none, errFileNotFound
		}
		size = thumbSize.Int64
	}
	r, err := d.files.open(d.v, blob, thumb)
	if errors.Is(err, fs.ErrNotExist) {
		d.log.Warnf("File %d is missing from the den's storage", fid)
		return none, errFileNotFound
	}
	if err != nil {
		return none, err
	}
	if r.Size() != size {
		r.Close()
		return none, fmt.Errorf("file %d: %w", fid, vault.ErrSealed)
	}
	return r, nil
}

const fileColumns = `id, message_id, name, type, size, width, height, animated, thumb_width, thumb_height, duration_ms`

// scanFile reads a file's description, and the message it's on.
func (d *Den) scanFile(scan func(...any) error) (denproto.File, int64, error) {
	var f denproto.File
	var id int64
	var message, w, h, tw, th, duration sql.NullInt64
	var sealed []byte
	if err := scan(&id, &message, &sealed, &f.Type, &f.Size, &w, &h, &f.Animated, &tw, &th, &duration); err != nil {
		return f, 0, err
	}
	f.Duration = duration.Int64
	name, err := d.v.Open(sealed, fileNameAD(id))
	if err != nil {
		return f, 0, err
	}
	f.ID, f.Name, f.Width, f.Height = denproto.FormatID(id), string(name), int(w.Int64), int(h.Int64)
	if tw.Valid && th.Valid {
		f.Thumb = &denproto.Thumb{Width: int(tw.Int64), Height: int(th.Int64)}
	}
	return f, message.Int64, nil
}

// attachments returns the files on the given messages, by message, in the
// order they were sent.
func (d *Den) attachments(ctx context.Context, q querier, ids []int64) (map[int64][]denproto.File, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, `SELECT `+fileColumns+` FROM den_files WHERE message_id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`) ORDER BY message_id, position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]denproto.File{}
	for rows.Next() {
		f, message, err := d.scanFile(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[message] = append(out[message], f)
	}
	return out, rows.Err()
}

// attach puts pending uploads on a new message within tx. Each must be
// the author's own, unused and not expired, and sealed for a DM's message
// or not for a channel's; one made to replace a file takes only its place.
func attach(ctx context.Context, tx *sql.Tx, message, author int64, files []int64, sealed bool, now time.Time) error {
	for i, id := range files {
		res, err := tx.ExecContext(ctx, `UPDATE den_files SET message_id = ?, position = ?
			WHERE id = ? AND uploader_id = ? AND message_id IS NULL AND created_at > ? AND sealed = ? AND replaces IS NULL
			AND NOT EXISTS (SELECT 1 FROM den_members WHERE avatar_id = den_files.id OR banner_id = den_files.id)`,
			message, i, id, author, now.Add(-uploadExpiry).UnixMilli(), sealed)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			if sealed {
				return invalid("attachments: no such sealed upload waiting to be sent")
			}
			return invalid("attachments: no such upload waiting to be sent")
		}
	}
	return nil
}

// parseAttachments checks a send's list of uploads, at most most of them.
func parseAttachments(ids []string, most int) ([]int64, error) {
	if len(ids) > most {
		return nil, invalid("a message has at most %d files", most)
	}
	out := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, s := range ids {
		id, err := denproto.ParseID(s)
		if err != nil || seen[id] {
			return nil, invalid("attachments: invalid or repeated upload ID")
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// filesOf returns the IDs and blobs of the files on the messages a
// condition on den_messages (aliased m) selects.
func filesOf(ctx context.Context, q querier, where string, args ...any) (map[int64][]string, [][]byte, error) {
	rows, err := q.QueryContext(ctx, `SELECT f.id, f.message_id, f.blob FROM den_files f
		JOIN den_messages m ON m.id = f.message_id WHERE `+where+` ORDER BY f.message_id, f.position`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids := map[int64][]string{}
	var blobs [][]byte
	for rows.Next() {
		var id, message int64
		var blob []byte
		if err := rows.Scan(&id, &message, &blob); err != nil {
			return nil, nil, err
		}
		ids[message] = append(ids[message], denproto.FormatID(id))
		blobs = append(blobs, blob)
	}
	return ids, blobs, rows.Err()
}

// deleteFiles deletes the files a condition on den_files selects, within
// q, and returns their blobs to remove once that commits.
func deleteFiles(ctx context.Context, q querier, where string, args ...any) ([][]byte, error) {
	rows, err := q.QueryContext(ctx, `DELETE FROM den_files WHERE `+where+` RETURNING blob`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var blobs [][]byte
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		blobs = append(blobs, blob)
	}
	return blobs, rows.Err()
}

// unused selects files no message or profile uses.
const unused = `message_id IS NULL AND NOT EXISTS (SELECT 1 FROM den_members WHERE avatar_id = den_files.id OR banner_id = den_files.id)`

// scheduleSweep makes sure pending uploads are swept by at.
func (s *fileStore) scheduleSweep(d *Den, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sweep != nil && !s.next.After(at) {
		return
	}
	if s.sweep != nil {
		s.sweep.Stop()
	}
	s.next = at
	s.sweep = time.AfterFunc(at.Sub(d.now())+time.Second, d.sweepUploads)
}

func (s *fileStore) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sweep != nil {
		s.sweep.Stop()
		s.sweep = nil
	}
}

// sweepUploads deletes uploads nothing used within the hour, and plans the
// next sweep for when the oldest one left expires.
func (d *Den) sweepUploads() {
	d.files.mu.Lock()
	d.files.sweep = nil
	d.files.mu.Unlock()
	ctx := context.Background()
	blobs, err := deleteFiles(ctx, d.db, unused+` AND created_at <= ?`, d.now().Add(-uploadExpiry).UnixMilli())
	if err != nil {
		d.log.Errorf("sweep expired uploads: %v", err)
		return
	}
	d.files.remove(blobs)
	var oldest sql.NullInt64
	if err := d.db.QueryRowContext(ctx, `SELECT min(created_at) FROM den_files WHERE `+unused).Scan(&oldest); err != nil {
		d.log.Errorf("find the oldest upload: %v", err)
		return
	}
	if oldest.Valid {
		d.files.scheduleSweep(d, time.UnixMilli(oldest.Int64).Add(uploadExpiry))
	}
}

// tidyFiles clears what an interrupted upload or delete left behind:
// temporary files, and stored files no row names, which nothing can reach.
// Then it sweeps expired uploads.
func (d *Den) tidyFiles(ctx context.Context) error {
	temps, err := os.ReadDir(d.files.temp)
	if err != nil {
		return err
	}
	for _, e := range temps {
		if strings.HasPrefix(e.Name(), partPrefix) {
			if err := os.Remove(filepath.Join(d.files.temp, e.Name())); err != nil {
				return err
			}
		}
	}
	known := map[string]bool{}
	rows, err := d.db.QueryContext(ctx, `SELECT blob FROM den_files`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			rows.Close()
			return err
		}
		known[hex.EncodeToString(blob)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	stored, err := os.ReadDir(d.files.dir)
	if err != nil {
		return err
	}
	removed := 0
	for _, e := range stored {
		base := strings.TrimSuffix(e.Name(), thumbSuffix)
		if b, err := hex.DecodeString(base); err != nil || len(b) != 16 || known[base] {
			continue
		}
		if err := os.Remove(filepath.Join(d.files.dir, e.Name())); err != nil {
			return err
		}
		removed++
	}
	if removed > 0 {
		d.log.Infof("Removed %d stored files that no upload names", removed)
	}
	d.sweepUploads()
	return nil
}

// checkPicture checks an upload a member puts on their profile: their own,
// waiting to be used, a still image, and the shape the kind of picture
// takes. One made to replace a file must replace the picture it takes the
// place of. It returns the upload's ID.
func checkPicture(ctx context.Context, tx *sql.Tx, member int64, id string, banner bool, now time.Time) (int64, error) {
	fid, err := denproto.ParseID(id)
	if err != nil {
		return 0, invalid("no such upload")
	}
	column := "avatar_id"
	if banner {
		column = "banner_id"
	}
	var typ string
	var w, h sql.NullInt64
	var animated bool
	err = tx.QueryRowContext(ctx, `SELECT type, width, height, animated FROM den_files
		WHERE id = ? AND uploader_id = ? AND created_at > ? AND `+unused+`
		AND (replaces IS NULL OR replaces = (SELECT `+column+` FROM den_members WHERE id = ?))`,
		fid, member, now.Add(-uploadExpiry).UnixMilli(), member).
		Scan(&typ, &w, &h, &animated)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, invalid("no such upload waiting to be used")
	}
	if err != nil {
		return 0, err
	}
	if !w.Valid || animated || !strings.HasPrefix(typ, "image/") {
		return 0, invalid("a profile picture is a still image")
	}
	width, height := w.Int64, h.Int64
	if banner {
		// Three times as wide as high, give or take the rounding of a crop.
		if width < denproto.MinBannerWide || width > denproto.MaxBannerWide || width*100 < height*290 || width*100 > height*310 {
			return 0, invalid("a banner is %d to %d pixels wide and three times as wide as high", denproto.MinBannerWide, denproto.MaxBannerWide)
		}
	} else if width != height || width < denproto.MinAvatarSide || width > denproto.MaxAvatarSide {
		return 0, invalid("an avatar is square, %d to %d pixels", denproto.MinAvatarSide, denproto.MaxAvatarSide)
	}
	return fid, nil
}
