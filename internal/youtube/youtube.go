// Package youtube fetches what the covers of YouTube's players show (M4.1):
// a linked video's title and channel, from YouTube's oEmbed endpoint, and
// its picture, from i.ytimg.com. The page shows both from its own origin,
// so nothing on it contacts YouTube until a member plays a video.
//
// It asks only at addresses it builds from a checked video ID, follows no
// redirects, caps what it reads, and keeps what it found in memory. It
// sends no cookies, referrer or user agent, and its errors never hold a
// video's ID, which comes from a message and so stays out of logs.
package youtube

import (
	"bytes"
	"container/list"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"

	"golang.org/x/time/rate"
)

const (
	oEmbedURL   = "https://www.youtube.com/oembed"
	picturesURL = "https://i.ytimg.com/vi/"

	maxAnswer  = 64 << 10
	maxPicture = 256 << 10
	// The picture asked for is 480 by 360; much more isn't that picture.
	maxSide = 1280

	fetchTimeout = 10 * time.Second
	// A failure is kept for a minute, so a YouTube that was down is asked
	// again soon, but not by every cover on the page.
	keepFound  = time.Hour
	keepFailed = time.Minute
	maxBytes   = 16 << 20
	maxEntries = 1024

	maxTitle   = 200
	maxChannel = 100
)

var (
	// ErrBadID is an ID that isn't a YouTube video's; nothing is fetched.
	ErrBadID = errors.New("not a YouTube video's ID")
	// ErrNotFound is a video, or a picture, YouTube doesn't have.
	ErrNotFound = errors.New("YouTube has no such video")
	// ErrUnavailable is YouTube not answering as it should.
	ErrUnavailable = errors.New("YouTube couldn't be reached")
)

// Video is what a cover shows of a video.
type Video struct {
	Title   string `json:"title,omitempty"`
	Channel string `json:"channel,omitempty"`
	// Playable is false for a video whose owner turned off embedding, or
	// that's private, which oEmbed refuses to describe.
	Playable bool `json:"playable"`
}

// Lookup fetches covers' details and keeps them. Use New.
type Lookup struct {
	oEmbed, pictures string
	client           *http.Client
	limit            *rate.Limiter
	now              func() time.Time

	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List // of *entry, the most recently used first
	bytes   int
	calls   map[string]*call
}

type entry struct {
	key   string
	value any
	err   error
	until time.Time
	size  int
}

// call is a fetch under way, which everyone asking for the same key waits
// on.
type call struct {
	done  chan struct{}
	value any
	err   error
}

// New returns a Lookup that asks YouTube.
func New() *Lookup {
	return newLookup(oEmbedURL, picturesURL, &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: fetchTimeout,
		IdleConnTimeout:       90 * time.Second,
	})
}

func newLookup(oEmbed, pictures string, transport http.RoundTripper) *Lookup {
	return &Lookup{
		oEmbed:   oEmbed,
		pictures: pictures,
		client: &http.Client{
			Transport: transport,
			Timeout:   fetchTimeout,
			// An answer that sends Dens elsewhere is taken as it is, and
			// fails: Dens asks only at the addresses it built.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		// A page asks only for the covers on screen, but a busy channel
		// can show many at once.
		limit:   rate.NewLimiter(8, 16),
		now:     time.Now,
		entries: map[string]*list.Element{},
		order:   list.New(),
		calls:   map[string]*call{},
	}
}

// Video returns what a cover shows of the video id.
func (l *Lookup) Video(ctx context.Context, id string) (Video, error) {
	if !denproto.VideoID(id) {
		return Video{}, ErrBadID
	}
	v, err := l.get(ctx, "video "+id, func(ctx context.Context) (any, int, error) { return l.fetchVideo(ctx, id) })
	if err != nil {
		return Video{}, err
	}
	return v.(Video), nil
}

// Picture returns the video id's picture: a JPEG, with its metadata taken
// out.
func (l *Lookup) Picture(ctx context.Context, id string) ([]byte, error) {
	if !denproto.VideoID(id) {
		return nil, ErrBadID
	}
	v, err := l.get(ctx, "picture "+id, func(ctx context.Context) (any, int, error) { return l.fetchPicture(ctx, id) })
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// get returns what's kept under key, or fetches it once for everyone who
// asks meanwhile. The fetch runs on its own deadline, so a caller who
// leaves, as a page does when a cover scrolls away, fails nobody else.
func (l *Lookup) get(ctx context.Context, key string, fetch func(context.Context) (any, int, error)) (any, error) {
	l.mu.Lock()
	if el, ok := l.entries[key]; ok {
		e := el.Value.(*entry)
		if l.now().Before(e.until) {
			l.order.MoveToFront(el)
			l.mu.Unlock()
			return e.value, e.err
		}
		l.drop(el)
	}
	c, ok := l.calls[key]
	if !ok {
		c = &call{done: make(chan struct{})}
		l.calls[key] = c
		go l.run(key, c, fetch)
	}
	l.mu.Unlock()
	select {
	case <-c.done:
		return c.value, c.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *Lookup) run(key string, c *call, fetch func(context.Context) (any, int, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	value, size, err := fetch(ctx)
	keep := keepFound
	if err != nil && !errors.Is(err, ErrNotFound) {
		keep = keepFailed
	}
	l.mu.Lock()
	delete(l.calls, key)
	l.store(&entry{key: key, value: value, err: err, until: l.now().Add(keep), size: size + len(key) + 64})
	l.mu.Unlock()
	c.value, c.err = value, err
	close(c.done)
}

func (l *Lookup) store(e *entry) {
	if el, ok := l.entries[e.key]; ok {
		l.drop(el)
	}
	l.entries[e.key] = l.order.PushFront(e)
	l.bytes += e.size
	for l.bytes > maxBytes || l.order.Len() > maxEntries {
		l.drop(l.order.Back())
	}
}

func (l *Lookup) drop(el *list.Element) {
	e := l.order.Remove(el).(*entry)
	delete(l.entries, e.key)
	l.bytes -= e.size
}

func (l *Lookup) fetchVideo(ctx context.Context, id string) (any, int, error) {
	q := url.Values{"url": {"https://www.youtube.com/watch?v=" + id}, "format": {"json"}}
	resp, err := l.ask(ctx, l.oEmbed+"?"+q.Encode(), "application/json")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// oEmbed refuses a video whose owner turned off embedding, and a
		// private one: either plays only on YouTube, if anywhere.
		return Video{}, 0, nil
	case http.StatusNotFound, http.StatusBadRequest:
		return nil, 0, ErrNotFound
	default:
		return nil, 0, fmt.Errorf("%w: oEmbed answered %d", ErrUnavailable, resp.StatusCode)
	}
	if t, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); t != "application/json" {
		return nil, 0, fmt.Errorf("%w: oEmbed's answer isn't JSON", ErrUnavailable)
	}
	body, err := readAll(resp.Body, maxAnswer)
	if err != nil {
		return nil, 0, err
	}
	// The answer also offers a ready-made player, as HTML; nothing but
	// these fields is read.
	var answer struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Author string `json:"author_name"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.Type != "video" {
		return nil, 0, fmt.Errorf("%w: oEmbed's answer doesn't describe a video", ErrUnavailable)
	}
	v := Video{Title: clean(answer.Title, maxTitle), Channel: clean(answer.Author, maxChannel), Playable: true}
	return v, len(v.Title) + len(v.Channel), nil
}

func (l *Lookup) fetchPicture(ctx context.Context, id string) (any, int, error) {
	resp, err := l.ask(ctx, l.pictures+id+"/hqdefault.jpg", "image/jpeg")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, 0, ErrNotFound
	default:
		return nil, 0, fmt.Errorf("%w: the picture's host answered %d", ErrUnavailable, resp.StatusCode)
	}
	body, err := readAll(resp.Body, maxPicture)
	if err != nil {
		return nil, 0, err
	}
	if media.Sniff(body[:min(len(body), media.SniffLen)]) != media.JPEG {
		return nil, 0, fmt.Errorf("%w: the picture isn't a JPEG", ErrUnavailable)
	}
	var out bytes.Buffer
	res, err := media.Strip(&out, bytes.NewReader(body), media.JPEG)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: the picture doesn't read as a JPEG", ErrUnavailable)
	}
	if res.Width < 1 || res.Height < 1 || res.Width > maxSide || res.Height > maxSide {
		return nil, 0, fmt.Errorf("%w: the picture is %d by %d", ErrUnavailable, res.Width, res.Height)
	}
	return out.Bytes(), out.Len(), nil
}

// ask sends a GET with nothing that identifies the member's browser or
// Dens itself.
func (l *Lookup) ask(ctx context.Context, address, accept string) (*http.Response, error) {
	if err := l.limit.Wait(ctx); err != nil {
		return nil, fmt.Errorf("%w: too many asks at once", ErrUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: the request couldn't be made", ErrUnavailable)
	}
	req.Header.Set("Accept", accept)
	// Go sends a user agent of its own unless the header is there, empty.
	req.Header.Set("User-Agent", "")
	resp, err := l.client.Do(req)
	if err != nil {
		// The error names the address, which holds the ID; say only what
		// went wrong.
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf("%w: it timed out", ErrUnavailable)
		}
		return nil, fmt.Errorf("%w: the connection failed", ErrUnavailable)
	}
	return resp, nil
}

// readAll reads a body of at most max bytes, and fails on a longer one
// rather than cut it.
func readAll(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("%w: the answer broke off", ErrUnavailable)
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%w: the answer is over %d bytes", ErrUnavailable, max)
	}
	return b, nil
}

// clean makes text from YouTube fit to show as a name is: without the
// characters that let it disguise itself or reorder the text around it,
// with its spaces collapsed, and cut to max characters.
func clean(s string, max int) string {
	// A line break or tab parts words, which CleanName, dropping it as a
	// control character, would join.
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	name, err := denproto.CleanName(s, utf8.RuneCountInString(s))
	if err != nil {
		return ""
	}
	if r := []rune(name); len(r) > max {
		name = strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return name
}
