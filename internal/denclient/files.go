package denclient

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Files go to dens through here, and come back through here.
//
// An image's metadata comes out before it leaves this machine: the den
// refuses an image that still has any, and never learns where a photo was
// taken. Files coming back are checked the same way everything from a den
// is: only a file that really is an image of a kind media reads is ever
// handed to the browser as one.

// Cache sizes: previews and small files stay in memory, since the page
// never keeps them (they'd sit in the browser's cache unencrypted).
const (
	cacheSize     = 64 << 20
	cacheFileSize = 8 << 20
)

// Uploaded is a file the den stored, whether metadata was taken out of it
// first, and whether it's a photo turned into a JPEG or PNG to send. A
// photo uploaded for a message with its two versions describes them
// (versions.go).
type Uploaded struct {
	denproto.File
	Stripped  bool      `json:"stripped,omitempty"`
	Converted bool      `json:"converted,omitempty"`
	Versions  *Versions `json:"versions,omitempty"`
}

// ErrTooLarge is an upload over the den's limit, which the page states.
var ErrTooLarge = errors.New("the file is larger than this den allows")

// Upload sends a file to a den for a message or a profile to use. Images
// lose their metadata on the way, as they stream, and video, audio and
// photos browsers can't show go through the media module first (media.go);
// size is the file's length. A file for a DM, which channel names, goes sealed. Nothing reads
// body once Upload returns, so the caller can drain what's left of it.
func (m *Manager) Upload(ctx context.Context, denID, channelID, name string, size int64, body io.Reader) (Uploaded, error) {
	return m.upload(ctx, denID, channelID, name, size, body, "")
}

// Replace uploads a file, as Upload does, made to take the place of one of
// this member's files in use (M5). The den counts it against the space
// that file frees, so a member at their limit can swap a file for another,
// and it can take only that file's place, which SwapFile puts it in.
func (m *Manager) Replace(ctx context.Context, denID, channelID, fileID, name string, size int64, body io.Reader) (Uploaded, error) {
	if err := checkID("file", fileID); err != nil {
		return Uploaded{}, err
	}
	return m.upload(ctx, denID, channelID, name, size, body, fileID)
}

func (m *Manager) upload(ctx context.Context, denID, channelID, name string, size int64, body io.Reader, replaces string) (Uploaded, error) {
	c, err := m.find(denID)
	if err != nil {
		return Uploaded{}, err
	}
	return m.uploadTo(ctx, c, channelID, name, size, body, replaces)
}

// uploadTo uploads a file to a den, as upload does.
func (m *Manager) uploadTo(ctx context.Context, c *conn, channelID, name string, size int64, body io.Reader, replaces string) (Uploaded, error) {
	return m.uploadFile(ctx, c, channelID, name, size, body, replaces, nil)
}

// uploadReady uploads what the media module made of a file already, as
// uploadTo does once it has: a video's version, kept while it waits to be
// sent (M5.4). The caller keeps the file.
func (m *Manager) uploadReady(ctx context.Context, c *conn, channelID string, p *prepared, replaces string) (Uploaded, error) {
	return m.uploadFile(ctx, c, channelID, p.name, p.out.Size(), nil, replaces, p)
}

// uploadFile uploads body, or what ready holds when it's set.
func (m *Manager) uploadFile(ctx context.Context, c *conn, channelID, name string, size int64, body io.Reader, replaces string,
	ready *prepared) (Uploaded, error) {
	if channelID != "" {
		if _, dm := c.isDM(channelID); dm {
			return c.uploadDM(ctx, channelID, name, size, body, replaces, ready)
		}
	}
	limits := c.limits()
	if limits.FileSize > 0 && size > limits.FileSize {
		return Uploaded{}, ErrTooLarge
	}
	p := ready
	var br *bufio.Reader
	var kind media.Kind
	if p == nil {
		// The stripping goroutine, or the transport sending the file, may
		// still be reading when the upload fails.
		g := &gate{r: body}
		defer g.shut()
		br = bufio.NewReaderSize(g, media.SniffLen)
		head, err := br.Peek(media.SniffLen)
		if err != nil && !errors.Is(err, io.EOF) {
			return Uploaded{}, err
		}
		kind = media.Sniff(head)
		if kind.Refused() {
			return Uploaded{}, refusal(head)
		}
		if kind.NeedsMedia() {
			if p, err = m.prepare(ctx, kind, head, name, br, max(limits.FileSize, size), limits.FileSize); err != nil {
				return Uploaded{}, err
			}
			defer p.close()
		}
	}
	if p != nil {
		kind, name, size = p.kind, p.name, p.out.Size()
		br = bufio.NewReaderSize(io.NewSectionReader(p.out, 0, size), media.SniffLen)
	}

	var send io.Reader = br
	length := size
	var stripped chan strip
	switch kind {
	case media.WebP:
		// A WebP states its size up front, so it's rewritten whole.
		var out bytes.Buffer
		res, err := media.Strip(&out, io.LimitReader(br, max(limits.FileSize, size)+1), kind)
		if err != nil {
			return Uploaded{}, stripError(err)
		}
		stripped = make(chan strip, 1)
		stripped <- strip{res: res}
		send, length = &out, int64(out.Len())
	case media.JPEG, media.PNG, media.GIF:
		pr, pw := io.Pipe()
		defer pr.Close()
		stripped = make(chan strip, 1)
		go func() {
			res, err := media.Strip(pw, br, kind)
			// Sent before the pipe closes, so a failed upload finds it.
			stripped <- strip{res, err}
			pw.CloseWithError(err)
		}()
		send, length = pr, -1
	}

	if p != nil && p.sending != nil {
		send = &counted{r: send, total: length, report: p.sending}
	}
	token, err := c.session(ctx)
	if err != nil {
		return Uploaded{}, err
	}
	a := c.remote()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/api/uploads", send)
	if err != nil {
		return Uploaded{}, err
	}
	req.ContentLength = length
	a.headers(req.Header, token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(denproto.HeaderFilename, url.PathEscape(denproto.CleanFilename(name)))
	if replaces != "" {
		req.Header.Set(denproto.HeaderReplaces, replaces)
	}
	res, err := m.Transfer.Do(req)
	if err != nil {
		select {
		case s := <-stripped:
			if s.err != nil && !errors.Is(s.err, io.ErrClosedPipe) {
				return Uploaded{}, stripError(s.err)
			}
		default:
		}
		return Uploaded{}, fmt.Errorf("reach the den: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return Uploaded{}, denproto.ReadError(res)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, denproto.MaxBody+1))
	if err != nil {
		return Uploaded{}, fmt.Errorf("read the den's response: %w", err)
	}
	var up Uploaded
	if len(data) > denproto.MaxBody || json.Unmarshal(data, &up.File) != nil || denproto.CheckFile(up.File) != nil {
		return Uploaded{}, errors.New("the den's answer is malformed")
	}
	if stripped != nil {
		// The den read the whole file, so stripping is done.
		up.Stripped = (<-stripped).res.Removed
	}
	if p != nil {
		// A video's or audio's container is written anew, with nothing
		// of the old one's but its streams, and a converted photo keeps
		// only its pixels and colors.
		up.Stripped, up.Converted = true, p.converted
		// A video's copy, in AV1, which the den can't make a preview of,
		// takes the one made from its full size (M5.4).
		if p.poster != nil && up.Thumb == nil {
			up = c.giveThumb(ctx, up, *p.poster)
		}
	}
	return up, nil
}

// giveThumb gives an upload of a video the preview this service made of
// it, as SetThumb gives it the page's. One the den doesn't take leaves the
// video for the page to draw one, as it does for a WebM.
func (c *conn) giveThumb(ctx context.Context, up Uploaded, th media.Thumb) Uploaded {
	f, err := c.post(ctx, "/api/uploads/"+up.ID+"/thumb", bytes.NewReader(th.Data), int64(len(th.Data)), "")
	if err == nil && (f.ID != up.ID || f.Thumb == nil) {
		err = errors.New("the den's answer is malformed")
	}
	if err != nil {
		if ctx.Err() == nil {
			c.m.log.Infof("A video's copy goes without its preview: %v", err)
		}
		return up
	}
	up.Thumb = f.Thumb
	return up
}

// counted reports how much of a body of total bytes was read, as an upload
// goes (M5.4).
type counted struct {
	r      io.Reader
	n      int64
	total  int64
	report func(done, total int64)
}

func (c *counted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	c.report(c.n, c.total)
	return n, err
}

// strip is how stripping an upload went.
type strip struct {
	res media.Result
	err error
}

// gate passes reads through until it's shut, and then refuses them. Shut
// waits for a read in progress, so after it nothing is reading the body.
type gate struct {
	mu     sync.Mutex
	r      io.Reader
	closed bool
}

var errUploadOver = errors.New("the upload is over")

func (g *gate) Read(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return 0, errUploadOver
	}
	return g.r.Read(p)
}

func (g *gate) shut() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

func stripError(err error) error {
	if errors.Is(err, media.ErrMalformed) {
		return inputError(errors.New("This file is damaged, or isn't the kind of image it starts out as, so it can't be sent."))
	}
	return err
}

// OpenedFile is a file from a den, for the page, to read anywhere in, as a
// player seeking through a video does.
type OpenedFile struct {
	*io.SectionReader
	// Kind is what the file turned out to be, and Head its first bytes.
	// Only an image kind, or video or audio PlayType names, shows inline.
	Kind  media.Kind
	Head  []byte
	close func() error
}

// PlayType is the type a video or audio file plays as, when it's in a
// container the media module writes; "" otherwise.
func (f *OpenedFile) PlayType() string {
	if f.Kind != media.Video && f.Kind != media.Audio {
		return ""
	}
	return media.PlayType(f.Head)
}

func (f *OpenedFile) Close() error {
	if f.close == nil {
		return nil
	}
	return f.close()
}

func inMemory(data []byte, kind media.Kind) *OpenedFile {
	return &OpenedFile{SectionReader: io.NewSectionReader(bytes.NewReader(data), 0, int64(len(data))), Kind: kind,
		Head: data[:min(len(data), media.SniffLen)]}
}

// OpenFile opens a file, or its preview, from a den, or from the cache. A
// DM's file opens with the key its message sealed, chunk by chunk as it's
// read.
func (m *Manager) OpenFile(ctx context.Context, denID, fileID string, thumb bool) (*OpenedFile, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	if err := checkID("file", fileID); err != nil {
		return nil, err
	}
	key := cacheKey{den: denID, file: fileID, thumb: thumb}
	if data, kind, ok := m.files.get(key); ok {
		return inMemory(data, kind), nil
	}
	c.mu.Lock()
	df, sealed := c.dmFiles[fileID]
	c.mu.Unlock()
	path := "/api/files/" + fileID
	switch {
	case sealed && thumb:
		// A DM file's preview is a blob of its own.
		if df.thumb == "" {
			return nil, &denproto.Error{Status: http.StatusNotFound, Code: denproto.CodeNotFound, Message: "no preview"}
		}
		path = "/api/files/" + df.thumb
	case thumb:
		path += "/thumb"
	}
	rf, err := c.openRemote(ctx, path)
	if err != nil {
		return nil, err
	}
	var r interface {
		io.ReaderAt
		Size() int64
	} = rf
	if sealed {
		if r, err = vault.OpenStreamAtWith(df.key, rf, rf.size, denproto.DMFileAD(c.j.denID, df.channel, thumb)); err != nil {
			rf.Close()
			return nil, err
		}
	}
	size := r.Size()
	if size > denproto.MaxFileSize {
		rf.Close()
		return nil, errors.New("the den's answer is malformed")
	}
	if size <= cacheFileSize {
		defer rf.Close()
		data := make([]byte, size)
		if _, err := r.ReadAt(data, 0); err != nil && size > 0 {
			return nil, err
		}
		kind := media.Sniff(data[:min(len(data), media.SniffLen)])
		m.files.put(key, data, kind)
		return inMemory(data, kind), nil
	}
	head := make([]byte, media.SniffLen)
	if _, err := r.ReadAt(head, 0); err != nil {
		rf.Close()
		return nil, err
	}
	return &OpenedFile{SectionReader: io.NewSectionReader(r, 0, size), Kind: media.Sniff(head), Head: head, close: rf.Close}, nil
}

// transfer fetches a file from the den, whole, or from byte from on when
// from isn't negative, signing in again once if the den no longer accepts
// the token.
func (c *conn) transfer(ctx context.Context, path string, from int64) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		token, err := c.session(ctx)
		if err != nil {
			return nil, err
		}
		a := c.remote()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path, nil)
		if err != nil {
			return nil, err
		}
		a.headers(req.Header, token)
		if from >= 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
		}
		res, err := c.m.Transfer.Do(req)
		if err != nil {
			return nil, fmt.Errorf("reach the den: %w", err)
		}
		if res.StatusCode < 300 {
			return res, nil
		}
		err = denproto.ReadError(res)
		res.Body.Close()
		if attempt == 0 && denproto.IsCode(err, denproto.CodeUnauthorized) {
			c.dropToken()
			continue
		}
		return nil, err
	}
}

// remoteFile reads a file from a den at any offset. A read that carries on
// from the last one, or lands a little past it, reads on in the same
// answer; one elsewhere asks the den for the rest of the file from there.
type remoteFile struct {
	ctx  context.Context
	c    *conn
	path string
	size int64

	mu   sync.Mutex
	body io.ReadCloser // the den's answer from pos on, or nil
	pos  int64
}

// skipAhead is how far past the last read a read may land and still be
// reached by reading on.
const skipAhead = 256 << 10

// openRemote starts fetching a file from the den, which says its size.
func (c *conn) openRemote(ctx context.Context, path string) (*remoteFile, error) {
	res, err := c.transfer(ctx, path, -1)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK || res.ContentLength < 0 || res.ContentLength > vault.SealedSize(denproto.MaxFileSize) {
		res.Body.Close()
		return nil, errors.New("the den's answer is malformed")
	}
	return &remoteFile{ctx: ctx, c: c, path: path, size: res.ContentLength, body: res.Body}, nil
}

func (r *remoteFile) Size() int64 { return r.size }

func (r *remoteFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if off >= r.size {
		return 0, io.EOF
	}
	want := p[:min(int64(len(p)), r.size-off)]
	if r.body != nil && off > r.pos && off-r.pos <= skipAhead {
		if _, err := io.CopyN(io.Discard, r.body, off-r.pos); err != nil {
			r.drop()
		}
		r.pos = off
	}
	if r.body == nil || off != r.pos {
		r.drop()
		res, err := r.c.transfer(r.ctx, r.path, off)
		if err != nil {
			return 0, err
		}
		if res.StatusCode != http.StatusPartialContent || res.ContentLength != r.size-off ||
			res.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", off, r.size-1, r.size) {
			res.Body.Close()
			return 0, errors.New("the den's answer is malformed")
		}
		r.body, r.pos = res.Body, off
	}
	n, err := io.ReadFull(r.body, want)
	r.pos += int64(n)
	if err != nil {
		r.drop()
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return n, fmt.Errorf("read the file from the den: %w", err)
	}
	if len(want) < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *remoteFile) drop() {
	if r.body != nil {
		r.body.Close()
		r.body = nil
	}
}

func (r *remoteFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drop()
	return nil
}

// Storage reports how much of a den's space this member's files take.
func (m *Manager) Storage(ctx context.Context, denID string) (denproto.Storage, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Storage{}, err
	}
	var st denproto.Storage
	if err := c.call(ctx, http.MethodGet, "/api/me/storage", nil, &st); err != nil {
		return st, err
	}
	if st.Used < 0 || st.DenUsed < st.Used {
		return st, errors.New("the den's answer is malformed")
	}
	return st, nil
}

// limits returns the den's upload limits, as its last snapshot or update
// said.
func (c *conn) limits() denproto.Limits {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return denproto.Limits{}
	}
	return c.den.limits
}

type cacheKey struct {
	den, file string
	thumb     bool
}

type cacheEntry struct {
	key  cacheKey
	data []byte
	kind media.Kind
}

// fileCache keeps recently fetched files in memory, least recently used
// first out.
type fileCache struct {
	mu    sync.Mutex
	size  int64
	order *list.List // front is most recently used
	index map[cacheKey]*list.Element
}

func newFileCache() *fileCache {
	return &fileCache{order: list.New(), index: map[cacheKey]*list.Element{}}
}

func (fc *fileCache) get(k cacheKey) ([]byte, media.Kind, bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	el, ok := fc.index[k]
	if !ok {
		return nil, 0, false
	}
	fc.order.MoveToFront(el)
	e := el.Value.(*cacheEntry)
	return e.data, e.kind, true
}

func (fc *fileCache) put(k cacheKey, data []byte, kind media.Kind) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if el, ok := fc.index[k]; ok {
		fc.removeLocked(el)
	}
	fc.index[k] = fc.order.PushFront(&cacheEntry{key: k, data: data, kind: kind})
	fc.size += int64(len(data))
	for fc.size > cacheSize {
		fc.removeLocked(fc.order.Back())
	}
}

func (fc *fileCache) removeLocked(el *list.Element) {
	e := el.Value.(*cacheEntry)
	fc.order.Remove(el)
	delete(fc.index, e.key)
	fc.size -= int64(len(e.data))
}

// drop forgets files of a den, or all of the den's with no IDs given.
func (fc *fileCache) drop(den string, files ...string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	gone := map[string]bool{}
	for _, f := range files {
		gone[f] = true
	}
	for el := fc.order.Front(); el != nil; {
		next := el.Next()
		if e := el.Value.(*cacheEntry); e.key.den == den && (len(files) == 0 || gone[e.key.file]) {
			fc.removeLocked(el)
		}
		el = next
	}
}
