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

// Uploaded is a file the den stored, and whether metadata was taken out
// of it first.
type Uploaded struct {
	denproto.File
	Stripped bool `json:"stripped,omitempty"`
}

// ErrTooLarge is an upload over the den's limit, which the page states.
var ErrTooLarge = errors.New("the file is larger than this den allows")

// refusal explains why a kind of file isn't sent.
func refusal(k media.Kind) error {
	switch k {
	case media.Photo:
		return inputError(errors.New("Dens can't send this kind of photo yet (HEIC, AVIF, TIFF, camera raw or Photoshop), " +
			"because it can't take the location and camera details out of it. Save it as JPEG or PNG and send that."))
	case media.Audio:
		return inputError(errors.New("Dens can't send audio yet, because it can't take out the details audio files carry, " +
			"which can include where they were recorded."))
	}
	return inputError(errors.New("Dens can't send video yet, because it can't take out the details videos carry, " +
		"such as where they were filmed."))
}

// Upload sends a file to a den for a message or a profile to use. Images
// lose their metadata on the way, as they stream; size is the file's
// length. A file for a DM, which channel names, goes sealed. Nothing reads
// body once Upload returns, so the caller can drain what's left of it.
func (m *Manager) Upload(ctx context.Context, denID, channelID, name string, size int64, body io.Reader) (Uploaded, error) {
	c, err := m.find(denID)
	if err != nil {
		return Uploaded{}, err
	}
	if channelID != "" {
		if _, dm := c.isDM(channelID); dm {
			return c.uploadDM(ctx, channelID, name, size, body)
		}
	}
	limits := c.limits()
	if limits.FileSize > 0 && size > limits.FileSize {
		return Uploaded{}, ErrTooLarge
	}
	// The stripping goroutine, or the transport sending the file, may
	// still be reading when the upload fails.
	g := &gate{r: body}
	defer g.shut()
	br := bufio.NewReaderSize(g, media.SniffLen)
	head, err := br.Peek(media.SniffLen)
	if err != nil && !errors.Is(err, io.EOF) {
		return Uploaded{}, err
	}
	kind := media.Sniff(head)
	if kind.Refused() {
		return Uploaded{}, refusal(kind)
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
	return up, nil
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

// OpenedFile is a file from a den, for the page.
type OpenedFile struct {
	io.Reader
	Size int64
	// Kind is what the file turned out to be. Only an image kind may be
	// shown inline.
	Kind  media.Kind
	close func() error
}

func (f *OpenedFile) Close() error {
	if f.close == nil {
		return nil
	}
	return f.close()
}

// OpenFile fetches a file, or its preview, from a den, or from the cache.
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
		return &OpenedFile{Reader: bytes.NewReader(data), Size: int64(len(data)), Kind: kind}, nil
	}
	c.mu.Lock()
	df, sealed := c.dmFiles[fileID]
	c.mu.Unlock()
	var body io.ReadCloser
	var size int64
	if sealed {
		// A DM's file opens with the key its message sealed.
		if body, size, err = c.openDMFile(ctx, fileID, df, thumb); err != nil {
			return nil, err
		}
	} else {
		path := "/api/files/" + fileID
		if thumb {
			path += "/thumb"
		}
		res, err := c.transfer(ctx, path)
		if err != nil {
			return nil, err
		}
		size = res.ContentLength
		if size < 0 || size > denproto.MaxFileSize {
			res.Body.Close()
			return nil, errors.New("the den's answer is malformed")
		}
		body = readCloser{io.LimitReader(res.Body, size), res.Body}
	}
	if size <= cacheFileSize {
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read the file from the den: %w", err)
		}
		if int64(len(data)) != size {
			return nil, errors.New("the den sent less than it said")
		}
		kind := media.Sniff(data[:min(len(data), media.SniffLen)])
		m.files.put(key, data, kind)
		return &OpenedFile{Reader: bytes.NewReader(data), Size: size, Kind: kind}, nil
	}
	br := bufio.NewReaderSize(body, media.SniffLen)
	head, err := br.Peek(media.SniffLen)
	if err != nil && !errors.Is(err, io.EOF) {
		body.Close()
		return nil, err
	}
	return &OpenedFile{Reader: br, Size: size, Kind: media.Sniff(head), close: body.Close}, nil
}

// transfer fetches a file from the den, signing in again once if the den
// no longer accepts the token.
func (c *conn) transfer(ctx context.Context, path string) (*http.Response, error) {
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
