// Command densocket is a throwaway M1 spike: a den-shaped WebSocket server
// and client for checking sessions through Caddy, resume after a dropped
// connection or a den restart, and what permessage-deflate saves and costs.
//
//	densocket server  -listen ADDR          event stream with a resume ring
//	densocket probe   -url URL [-ca -connect]    auth, protocol and proxy checks
//	densocket follow  -url URL [-ca -connect -for D]  stay connected, resume, count gaps
//	densocket measure -url URL               bytes on the wire per compression mode
//	densocket connmem -url URL -stats URL -n N   den heap per connection per mode
//	densocket encodings                      offline: batching, short keys, dictionary
package main

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

const (
	token    = "spike-token"
	ringSize = 10000
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: densocket server|probe|follow|measure|connmem [flags]")
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"server": server, "probe": probe, "follow": follow, "measure": measure, "connmem": connmem,
		"encodings": encodings,
	}
	cmd, ok := cmds[os.Args[1]]
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown command", os.Args[1])
		os.Exit(2)
	}
	if err := cmd(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// Corpus ---------------------------------------------------------------------

type message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	AuthorID  string `json:"author_id"`
	CreatedAt int64  `json:"created_at"`
	Text      string `json:"text"`
	ReplyTo   string `json:"reply_to,omitempty"`
}

type event struct {
	T   string          `json:"t"`
	Seq uint64          `json:"seq,omitempty"`
	D   json.RawMessage `json:"d,omitempty"`
}

var words = strings.Fields(`the a to and of i you it is that in was for on
with this have be are not but so just like do what my at we can if they
all get about think know yeah no lol one would there out when up he she
time good really people go now more some how make see back game tonight
server voice channel build update patch fixed broke works weird thing
maybe today tomorrow week later anyone want play stream music movie photo
link check new old first last pretty much actually probably right sure
okay thanks nice cool great bad awful love hate funny wait what why who`)

// corpus builds n deterministic chat messages shaped like M1 traffic: short
// lines, some mentions, links, code and replies, across a few channels.
func corpus(n int) []message {
	r := mrand.New(mrand.NewPCG(1, 2))
	names := []string{"alice", "bob", "carol", "dave", "erin", "frank", "grace", "heidi"}
	out := make([]message, n)
	id := int64(1_000_000)
	ts := int64(1_759_000_000_000)
	for i := range out {
		id += 1 + r.Int64N(3)
		ts += 500 + r.Int64N(30_000)
		var b strings.Builder
		for j := range 3 + r.IntN(25) {
			if j > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(words[r.IntN(len(words))])
		}
		switch k := r.IntN(20); {
		case k == 0:
			b.WriteString(" https://www.youtube.com/watch?v=" + randomID(r, 11))
		case k == 1:
			b.WriteString("\n```go\nfunc main() { fmt.Println(\"hi\") }\n```")
		case k < 4:
			b.WriteString(" @" + names[r.IntN(len(names))])
		}
		m := message{
			ID:        strconv.FormatInt(id, 10),
			ChannelID: strconv.Itoa(10 + r.IntN(5)),
			AuthorID:  strconv.Itoa(100 + r.IntN(30)),
			CreatedAt: ts,
			Text:      b.String(),
		}
		if r.IntN(8) == 0 && i > 0 {
			m.ReplyTo = out[r.IntN(i)].ID
		}
		out[i] = m
	}
	return out
}

func randomID(r *mrand.Rand, n int) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.IntN(len(alphabet))]
	}
	return string(b)
}

// Server ---------------------------------------------------------------------

type den struct {
	epoch  string
	corpus []message

	mu    sync.Mutex
	seq   uint64
	first uint64 // seq of ring[0]
	ring  [][]byte
	subs  map[chan []byte]struct{}
	conns map[*websocket.Conn]struct{}
}

func (d *den) publish(m message) {
	payload, _ := json.Marshal(m)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	frame, _ := json.Marshal(event{T: "message.created", Seq: d.seq, D: payload})
	d.ring = append(d.ring, frame)
	if len(d.ring) > ringSize {
		d.ring = d.ring[1:]
		d.first++
	}
	for ch := range d.subs {
		select {
		case ch <- frame:
		default:
			// A slow consumer loses its subscription; its handler closes
			// the connection and the client resumes.
			delete(d.subs, ch)
			close(ch)
		}
	}
}

// subscribe registers a live subscriber and returns what to send first: a
// resumed frame and the missed events, or a ready frame when the resume
// point is unknown (another den process, or older than the ring).
func (d *den) subscribe(resume string, info map[string]any) (first [][]byte, ch chan []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch = make(chan []byte, 256)
	d.subs[ch] = struct{}{}
	if epoch, seqText, ok := strings.Cut(resume, "."); ok && epoch == d.epoch {
		if s, err := strconv.ParseUint(seqText, 10, 64); err == nil && s+1 >= d.first && s <= d.seq {
			resumed, _ := json.Marshal(event{T: "resumed"})
			first = append(first, resumed)
			return append(first, d.ring[s+1-d.first:]...), ch
		}
	}
	info["epoch"], info["seq"] = d.epoch, d.seq
	payload, _ := json.Marshal(info)
	ready, _ := json.Marshal(event{T: "ready", D: payload})
	return [][]byte{ready}, ch
}

func (d *den) unsubscribe(ch chan []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.subs[ch]; ok {
		delete(d.subs, ch)
		close(ch)
	}
}

func (d *den) track(c *websocket.Conn, add bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if add {
		d.conns[c] = struct{}{}
	} else {
		delete(d.conns, c)
	}
}

func compressionMode(name string) websocket.CompressionMode {
	switch name {
	case "notakeover":
		return websocket.CompressionNoContextTakeover
	case "takeover":
		return websocket.CompressionContextTakeover
	default:
		return websocket.CompressionDisabled
	}
}

func (d *den) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+token {
		http.Error(w, `{"error":{"code":"unauthorized"}}`, http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Dens-Protocol") != "1" {
		w.Header().Set("Dens-Protocol-Range", "1-1")
		http.Error(w, `{"error":{"code":"protocol_unsupported"}}`, http.StatusUpgradeRequired)
		return
	}
	q := r.URL.Query()
	opts := &websocket.AcceptOptions{CompressionMode: compressionMode(q.Get("compress"))}
	if t := q.Get("threshold"); t != "" {
		opts.CompressionThreshold, _ = strconv.Atoi(t)
	}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	d.track(c, true)
	defer d.track(c, false)
	ctx := c.CloseRead(context.Background())

	if q.Get("mode") == "corpus" {
		for _, m := range d.corpus {
			payload, _ := json.Marshal(m)
			frame, _ := json.Marshal(event{T: "message.created", D: payload})
			if c.Write(ctx, websocket.MessageText, frame) != nil {
				return
			}
		}
		c.Close(websocket.StatusNormalClosure, "")
		return
	}

	info := map[string]any{"remote": r.RemoteAddr, "forwarded_for": r.Header.Get("X-Forwarded-For")}
	first, ch := d.subscribe(q.Get("resume"), info)
	defer d.unsubscribe(ch)
	for _, frame := range first {
		if c.Write(ctx, websocket.MessageText, frame) != nil {
			return
		}
	}
	for {
		select {
		case frame, ok := <-ch:
			if !ok {
				c.Close(4008, "slow consumer")
				return
			}
			if c.Write(ctx, websocket.MessageText, frame) != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func server(args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:8485", "address")
	tick := fs.Duration("tick", 100*time.Millisecond, "interval between live events")
	fs.Parse(args)

	epoch := make([]byte, 8)
	rand.Read(epoch)
	d := &den{
		epoch: hex.EncodeToString(epoch), corpus: corpus(500), first: 1,
		subs: map[chan []byte]struct{}{}, conns: map[*websocket.Conn]struct{}{},
	}
	go func() {
		for i := 0; ; i++ {
			time.Sleep(*tick)
			d.publish(d.corpus[i%len(d.corpus)])
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", d.handle)
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		runtime.GC()
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		d.mu.Lock()
		n := len(d.conns)
		d.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]uint64{"heap_inuse": ms.HeapInuse, "conns": uint64(n)})
	})
	srv := &http.Server{Addr: *listen, Handler: mux}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	fmt.Printf("den epoch %s listening on %s\n", d.epoch, *listen)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	done := make(chan struct{})
	go func() {
		<-stop
		// Stop accepting first: a client told to reconnect must not reach
		// this process again. Hijacked WebSockets outlive srv.Close, so tell
		// each one the den is restarting, then give the close frames time.
		srv.Close()
		d.mu.Lock()
		for c := range d.conns {
			go c.Close(websocket.StatusServiceRestart, "den restarting")
		}
		d.mu.Unlock()
		time.Sleep(300 * time.Millisecond)
		close(done)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-done
	return nil
}

// Client ---------------------------------------------------------------------

type countingConn struct {
	net.Conn
	n *atomic.Int64
}

func (c countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// httpClient trusts caFile (when set), dials connect instead of the URL's
// host (when set), and counts the bytes it reads.
func httpClient(caFile, connect string, counter *atomic.Int64) (*http.Client, error) {
	tr := &http.Transport{}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificates in " + caFile)
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	}
	var d net.Dialer
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if connect != "" {
			addr = connect
		}
		c, err := d.DialContext(ctx, network, addr)
		if err != nil || counter == nil {
			return c, err
		}
		return countingConn{c, counter}, nil
	}
	return &http.Client{Transport: tr}, nil
}

func dialOpts(client *http.Client, mode websocket.CompressionMode) *websocket.DialOptions {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("Dens-Protocol", "1")
	return &websocket.DialOptions{HTTPClient: client, HTTPHeader: h, CompressionMode: mode}
}

func probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	url := fs.String("url", "", "WebSocket URL")
	ca := fs.String("ca", "", "CA certificate to trust")
	connect := fs.String("connect", "", "address to dial instead of the URL host")
	fs.Parse(args)
	client, err := httpClient(*ca, *connect, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	check := func(name string, header func(http.Header), want int) {
		opts := dialOpts(client, websocket.CompressionDisabled)
		header(opts.HTTPHeader)
		c, resp, err := websocket.Dial(ctx, *url, opts)
		got := 0
		if resp != nil {
			got = resp.StatusCode
		}
		if c != nil {
			c.CloseNow()
		}
		result := "PASS"
		if got != want {
			result = "FAIL"
		}
		fmt.Printf("%s  %s: status %d (want %d) %v\n", result, name, got, want, errOrEmpty(err))
	}
	check("no token", func(h http.Header) { h.Del("Authorization") }, http.StatusUnauthorized)
	check("wrong protocol", func(h http.Header) { h.Set("Dens-Protocol", "99") }, http.StatusUpgradeRequired)

	c, _, err := websocket.Dial(ctx, *url, dialOpts(client, websocket.CompressionDisabled))
	if err != nil {
		return fmt.Errorf("authenticated dial: %w", err)
	}
	defer c.CloseNow()
	_, frame, err := c.Read(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("PASS  authenticated upgrade; first frame %s\n", frame)
	return nil
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return "(" + err.Error() + ")"
}

func follow(args []string) error {
	fs := flag.NewFlagSet("follow", flag.ExitOnError)
	url := fs.String("url", "", "WebSocket URL")
	ca := fs.String("ca", "", "CA certificate to trust")
	connect := fs.String("connect", "", "address to dial instead of the URL host")
	dur := fs.Duration("for", 40*time.Second, "how long to follow")
	fs.Parse(args)
	client, err := httpClient(*ca, *connect, nil)
	if err != nil {
		return err
	}
	start := time.Now()
	deadline := start.Add(*dur)
	logf := func(format string, a ...any) {
		fmt.Printf("%6.2fs  %s\n", time.Since(start).Seconds(), fmt.Sprintf(format, a...))
	}

	var epoch string
	var last uint64
	var connects, readies, resumes, gaps, events int
	backoff := 250 * time.Millisecond
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		u := *url
		if epoch != "" {
			u += fmt.Sprintf("?resume=%s.%d", epoch, last)
		}
		c, _, err := websocket.Dial(ctx, u, dialOpts(client, websocket.CompressionDisabled))
		if err != nil {
			cancel()
			if time.Now().After(deadline) {
				break
			}
			logf("dial failed, retrying in %v: %v", backoff, err)
			time.Sleep(backoff + time.Duration(mrand.Int64N(int64(backoff/4))))
			backoff = min(backoff*2, 4*time.Second)
			continue
		}
		connects++
		backoff = 250 * time.Millisecond
		go func() {
			for ctx.Err() == nil {
				time.Sleep(10 * time.Second)
				pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
				c.Ping(pctx)
				pcancel()
			}
		}()
		for {
			_, frame, err := c.Read(ctx)
			if err != nil {
				if ctx.Err() == nil {
					logf("disconnected: close status %d: %v", websocket.CloseStatus(err), err)
				}
				break
			}
			var e event
			if json.Unmarshal(frame, &e) != nil {
				continue
			}
			switch e.T {
			case "ready":
				var info struct {
					Epoch        string `json:"epoch"`
					Seq          uint64 `json:"seq"`
					ForwardedFor string `json:"forwarded_for"`
				}
				json.Unmarshal(e.D, &info)
				if epoch != "" {
					logf("ready: new den epoch %s at seq %d (resync; the den restarted)", info.Epoch, info.Seq)
				} else {
					logf("ready: epoch %s at seq %d; den saw X-Forwarded-For %q", info.Epoch, info.Seq, info.ForwardedFor)
				}
				readies++
				epoch, last = info.Epoch, info.Seq
			case "resumed":
				resumes++
				logf("resumed from seq %d", last)
			case "message.created":
				if e.Seq != last+1 {
					gaps++
					logf("GAP: got seq %d after %d", e.Seq, last)
				}
				last = e.Seq
				events++
			}
		}
		c.CloseNow()
		cancel()
	}
	fmt.Printf("summary: connects=%d readies=%d resumes=%d gaps=%d events=%d last_seq=%d\n",
		connects, readies, resumes, gaps, events, last)
	if gaps > 0 {
		return errors.New("events were lost")
	}
	return nil
}

func measure(args []string) error {
	fs := flag.NewFlagSet("measure", flag.ExitOnError)
	url := fs.String("url", "", "WebSocket URL")
	ca := fs.String("ca", "", "CA certificate to trust")
	connect := fs.String("connect", "", "address to dial instead of the URL host")
	fs.Parse(args)

	modes := []struct{ name, query string }{
		{"none", "compress=none"},
		{"per-message, library default (skips < 512 B)", "compress=notakeover"},
		{"per-message, every message", "compress=notakeover&threshold=1"},
		{"context takeover", "compress=takeover"},
	}
	fmt.Printf("%-48s %10s %10s %8s  %s\n", "mode", "payload", "wire", "ratio", "negotiated")
	for _, m := range modes {
		var counter atomic.Int64
		client, err := httpClient(*ca, *connect, &counter)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		clientMode := compressionMode(strings.TrimPrefix(strings.Split(m.query, "&")[0], "compress="))
		c, resp, err := websocket.Dial(ctx, *url+"?mode=corpus&"+m.query, dialOpts(client, clientMode))
		if err != nil {
			cancel()
			return fmt.Errorf("%s: %w", m.name, err)
		}
		handshake := counter.Load()
		var payload int64
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				break
			}
			payload += int64(len(b))
		}
		c.CloseNow()
		cancel()
		wire := counter.Load() - handshake
		fmt.Printf("%-48s %10d %10d %7.0f%%  %s\n", m.name, payload, wire,
			100*float64(wire)/float64(payload), resp.Header.Get("Sec-WebSocket-Extensions"))
	}

	// A history page is one HTTP response; gzip sees the whole page at once.
	page, _ := json.Marshal(map[string]any{"messages": corpus(50), "has_older": true, "has_newer": false})
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(page)
	w.Close()
	fmt.Printf("history page of 50 messages: %d bytes, %d gzipped (%.0f%%)\n",
		len(page), gz.Len(), 100*float64(gz.Len())/float64(len(page)))
	return nil
}

func connmem(args []string) error {
	fs := flag.NewFlagSet("connmem", flag.ExitOnError)
	url := fs.String("url", "", "WebSocket URL")
	stats := fs.String("stats", "", "den stats URL")
	n := fs.Int("n", 200, "connections per mode")
	fs.Parse(args)

	heap := func() (uint64, error) {
		resp, err := http.Get(*stats)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		var s struct {
			HeapInuse uint64 `json:"heap_inuse"`
		}
		return s.HeapInuse, json.NewDecoder(resp.Body).Decode(&s)
	}
	client, err := httpClient("", "", nil)
	if err != nil {
		return err
	}
	fmt.Printf("%-32s %14s\n", "mode", "den heap/conn")
	for _, m := range []struct{ name, query string }{
		{"none", "compress=none"},
		{"per-message, every message", "compress=notakeover&threshold=1"},
		{"context takeover", "compress=takeover"},
	} {
		base, err := heap()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		mode := compressionMode(strings.TrimPrefix(strings.Split(m.query, "&")[0], "compress="))
		var conns []*websocket.Conn
		for range *n {
			c, _, err := websocket.Dial(ctx, *url+"?"+m.query, dialOpts(client, mode))
			if err != nil {
				cancel()
				return err
			}
			conns = append(conns, c)
			go func() {
				for {
					if _, _, err := c.Read(ctx); err != nil {
						return
					}
				}
			}()
		}
		// Let the live stream reach every connection a few times.
		time.Sleep(2 * time.Second)
		after, err := heap()
		if err != nil {
			cancel()
			return err
		}
		for _, c := range conns {
			c.CloseNow()
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
		fmt.Printf("%-32s %11.1f KB\n", m.name, float64(int64(after)-int64(base))/float64(*n)/1024)
	}
	return nil
}

// encodings compares, without a network, what per-frame deflate (no context
// takeover, so no per-connection memory) achieves on the corpus with
// batching, shorter keys and a static preset dictionary.
func encodings(args []string) error {
	msgs := corpus(500)
	long := func(m message, seq int) []byte {
		payload, _ := json.Marshal(m)
		b, _ := json.Marshal(event{T: "message.created", Seq: uint64(seq), D: payload})
		return b
	}
	short := func(m message, seq int) []byte {
		b, _ := json.Marshal(map[string]any{"t": "mc", "s": seq, "d": map[string]any{
			"i": m.ID, "c": m.ChannelID, "a": m.AuthorID, "at": m.CreatedAt, "x": m.Text, "r": m.ReplyTo}})
		return b
	}
	// A static dictionary shared by both ends: frame keys and a sample of
	// common chat words. Real traffic would train it on real messages.
	dict := []byte(`{"t":"message.created","seq":,"d":{"id":"","channel_id":"","author_id":"","created_at":,"text":"","reply_to":""}}` +
		strings.Join(words, " ") + " https://www.youtube.com/watch?v=")
	deflate := func(frames [][]byte, batch int, dict []byte) (raw, wire int) {
		for i := 0; i < len(frames); i += batch {
			group := frames[i:min(i+batch, len(frames))]
			frame := group[0]
			if batch > 1 {
				frame = append([]byte("["), bytes.Join(group, []byte(","))...)
				frame = append(frame, ']')
			}
			var buf bytes.Buffer
			var w *flate.Writer
			if dict != nil {
				w, _ = flate.NewWriterDict(&buf, flate.DefaultCompression, dict)
			} else {
				w, _ = flate.NewWriter(&buf, flate.DefaultCompression)
			}
			w.Write(frame)
			w.Flush()
			raw += len(frame)
			wire += buf.Len() - 4 // permessage-deflate drops the sync flush tail
		}
		return raw, wire
	}
	var longFrames, shortFrames [][]byte
	for i, m := range msgs {
		longFrames = append(longFrames, long(m, 1000+i))
		shortFrames = append(shortFrames, short(m, 1000+i))
	}
	base := 0
	for _, f := range longFrames {
		base += len(f)
	}
	fmt.Printf("%-44s %8s %8s\n", "encoding (per-frame deflate, no takeover)", "bytes", "vs raw")
	row := func(name string, n int) { fmt.Printf("%-44s %8d %7.0f%%\n", name, n, 100*float64(n)/float64(base)) }
	row("raw JSON, one event per frame", base)
	for _, c := range []struct {
		name   string
		frames [][]byte
		batch  int
		dict   []byte
	}{
		{"deflate, one event per frame", longFrames, 1, nil},
		{"deflate, 5 events per frame", longFrames, 5, nil},
		{"deflate, 20 events per frame", longFrames, 20, nil},
		{"short keys, raw", shortFrames, 0, nil},
		{"short keys, deflate, one per frame", shortFrames, 1, nil},
		{"dictionary, deflate, one per frame", longFrames, 1, dict},
		{"dictionary, deflate, 5 per frame", longFrames, 5, dict},
	} {
		if c.batch == 0 {
			n := 0
			for _, f := range c.frames {
				n += len(f)
			}
			row(c.name, n)
			continue
		}
		_, wire := deflate(c.frames, c.batch, c.dict)
		row(c.name, wire)
	}
	return nil
}
