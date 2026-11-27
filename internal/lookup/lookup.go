// Package lookup is what the local service's link covers share: YouTube's
// and Steam's cards. It asks a site only at an address its caller built
// from a checked ID, follows no redirects, caps what it reads, sends no
// cookies, referrer or user agent, and keeps what it found in memory,
// fetching each thing once for everyone who asks for it meanwhile. A
// picture must be a JPEG, and text is cleaned as names are. Its errors
// never hold an address, which holds an ID that came from a message and so
// stays out of logs.
package lookup

import (
	"bytes"
	"container/list"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	// Timeout bounds a fetch, from the request to its answer's last byte.
	Timeout = 10 * time.Second
	// A failure is kept for a minute, so a site that was down is asked
	// again soon, but not by every cover on the page.
	keepFailed = time.Minute
	maxBytes   = 16 << 20
	maxEntries = 1024
)

// Transport is the connection a site is asked over.
func Transport() http.RoundTripper {
	return &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: Timeout,
		IdleConnTimeout:       90 * time.Second,
	}
}

// Fetcher asks one site. Its failures wrap unavailable, and an address the
// site says it lacks, notFound.
type Fetcher struct {
	client      *http.Client
	limit       *rate.Limiter
	unavailable error
	notFound    error
}

// NewFetcher returns a Fetcher over transport.
func NewFetcher(transport http.RoundTripper, unavailable, notFound error) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   Timeout,
			// An answer that sends Dens elsewhere is taken as it is, and
			// fails: Dens asks only at the addresses it built.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		// A page asks only for the covers on screen, but a busy channel
		// can show many at once.
		limit:       rate.NewLimiter(8, 16),
		unavailable: unavailable,
		notFound:    notFound,
	}
}

// Ask sends a GET with nothing that identifies the member's browser or
// Dens itself.
func (f *Fetcher) Ask(ctx context.Context, address, accept string) (*http.Response, error) {
	if err := f.limit.Wait(ctx); err != nil {
		return nil, fmt.Errorf("%w: too many asks at once", f.unavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: the request couldn't be made", f.unavailable)
	}
	req.Header.Set("Accept", accept)
	// Go sends a user agent of its own unless the header is there, empty.
	req.Header.Set("User-Agent", "")
	resp, err := f.client.Do(req)
	if err != nil {
		// The error names the address, which holds the ID; say only what
		// went wrong.
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf("%w: it timed out", f.unavailable)
		}
		return nil, fmt.Errorf("%w: the connection failed", f.unavailable)
	}
	return resp, nil
}

// ReadAll reads a body of at most max bytes, and fails on a longer one
// rather than cut it.
func (f *Fetcher) ReadAll(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("%w: the answer broke off", f.unavailable)
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%w: the answer is over %d bytes", f.unavailable, max)
	}
	return b, nil
}

// JPEG fetches a picture of at most maxBytes, and of no side over side,
// and returns it with its metadata taken out. Anything but a JPEG fails.
func (f *Fetcher) JPEG(ctx context.Context, address string, maxBytes int64, side int) ([]byte, error) {
	resp, err := f.Ask(ctx, address, "image/jpeg")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, f.notFound
	default:
		return nil, fmt.Errorf("%w: the picture's host answered %d", f.unavailable, resp.StatusCode)
	}
	body, err := f.ReadAll(resp.Body, maxBytes)
	if err != nil {
		return nil, err
	}
	if media.Sniff(body[:min(len(body), media.SniffLen)]) != media.JPEG {
		return nil, fmt.Errorf("%w: the picture isn't a JPEG", f.unavailable)
	}
	var out bytes.Buffer
	res, err := media.Strip(&out, bytes.NewReader(body), media.JPEG)
	if err != nil {
		return nil, fmt.Errorf("%w: the picture doesn't read as a JPEG", f.unavailable)
	}
	if res.Width < 1 || res.Height < 1 || res.Width > side || res.Height > side {
		return nil, fmt.Errorf("%w: the picture is %d by %d", f.unavailable, res.Width, res.Height)
	}
	return out.Bytes(), nil
}

// Clean makes text from a site fit to show as a name is: without the
// characters that let it disguise itself or reorder the text around it,
// with its spaces collapsed, and cut to max characters.
func Clean(s string, max int) string {
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

// Cache keeps what fetches found, the most recently used first, within a
// budget of entries and bytes. Use NewCache or NewCacheWithLimit.
type Cache struct {
	final    error
	now      func() time.Time
	maxBytes int

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

// NewCache returns a cache with the default 16 MiB byte budget that keeps
// final, an answer that the thing asked about isn't there, as long as a
// value; any other failure it keeps a minute. now tells the time.
func NewCache(final error, now func() time.Time) *Cache {
	return NewCacheWithLimit(final, now, maxBytes)
}

// NewCacheWithLimit returns a cache bounded to limit bytes (or the default
// 16 MiB if limit is not positive).
func NewCacheWithLimit(final error, now func() time.Time, limit int) *Cache {
	if limit <= 0 {
		limit = maxBytes
	}
	return &Cache{
		final:    final,
		now:      now,
		maxBytes: limit,
		entries:  map[string]*list.Element{},
		order:    list.New(),
		calls:    map[string]*call{},
	}
}

// Get returns what's kept under key, or fetches it once for everyone who
// asks meanwhile, and keeps it for keep. fetch returns a value and its
// size. The fetch runs on its own deadline, so a caller who leaves, as a
// page does when a cover scrolls away, fails nobody else.
func (c *Cache) Get(ctx context.Context, key string, keep time.Duration, fetch func(context.Context) (any, int, error)) (any, error) {
	c.mu.Lock()
	if el, ok := c.entries[key]; ok {
		e := el.Value.(*entry)
		if c.now().Before(e.until) {
			c.order.MoveToFront(el)
			c.mu.Unlock()
			return e.value, e.err
		}
		c.drop(el)
	}
	cl, ok := c.calls[key]
	if !ok {
		cl = &call{done: make(chan struct{})}
		c.calls[key] = cl
		go c.run(key, keep, cl, fetch)
	}
	c.mu.Unlock()
	select {
	case <-cl.done:
		return cl.value, cl.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Cache) run(key string, keep time.Duration, cl *call, fetch func(context.Context) (any, int, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	value, size, err := fetch(ctx)
	if err != nil && !errors.Is(err, c.final) {
		keep = keepFailed
	}
	c.mu.Lock()
	delete(c.calls, key)
	c.store(&entry{key: key, value: value, err: err, until: c.now().Add(keep), size: size + len(key) + 64})
	c.mu.Unlock()
	cl.value, cl.err = value, err
	close(cl.done)
}

func (c *Cache) store(e *entry) {
	if el, ok := c.entries[e.key]; ok {
		c.drop(el)
	}
	c.entries[e.key] = c.order.PushFront(e)
	c.bytes += e.size
	for c.bytes > c.maxBytes || c.order.Len() > maxEntries {
		c.drop(c.order.Back())
	}
}

func (c *Cache) drop(el *list.Element) {
	e := c.order.Remove(el).(*entry)
	delete(c.entries, e.key)
	c.bytes -= e.size
}
