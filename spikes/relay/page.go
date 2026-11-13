package main

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// The page that plays what arrives: served on loopback as Dens's is, and
// fed each packet over a WebSocket as the probe gets it. The buffer that
// hides late packets is the page's, in its audio worklet, and what the
// worklet measures comes back up the socket into the log.

//go:embed web
var webFiles embed.FS

type Player struct {
	buffer  int
	rate    atomic.Int32
	mu      sync.Mutex
	clients map[*pageClient]struct{}
}

type pageClient struct {
	name    string
	out     chan []byte
	dropped atomic.Int64
}

func newPlayer(buffer int) *Player {
	return &Player{buffer: buffer, clients: map[*pageClient]struct{}{}}
}

// play hands a packet to every page listening. A page that has fallen a
// second behind loses the packet rather than hold the others up.
func (p *Player) play(pkt []byte) {
	p.rate.Store(int32(binary.LittleEndian.Uint32(pkt[4:])))
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.clients {
		select {
		case c.out <- pkt:
		default:
			c.dropped.Add(1)
		}
	}
}

func (p *Player) serve(ctx context.Context, addr string) error {
	files, _ := fs.Sub(webFiles, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(files))
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		rate := int(p.rate.Load())
		if rate == 0 {
			rate = 48000
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"rate": rate, "buffer": p.buffer})
	})
	mux.HandleFunc("/ws", p.socket)
	// localhost is both loopback addresses, as a browser may try either.
	addrs := []string{addr}
	if host, port, err := net.SplitHostPort(addr); err == nil && host == "localhost" {
		addrs = []string{net.JoinHostPort("127.0.0.1", port), net.JoinHostPort("::1", port)}
	}
	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	errs := make(chan error, len(addrs))
	served := 0
	for _, a := range addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			logf("page", "not at %s: %v", a, err)
			continue
		}
		served++
		go func() { errs <- srv.Serve(ln) }()
	}
	if served == 0 {
		return errors.New("no address to serve the page at")
	}
	logf("page", "at http://%s/ and its clicks at http://%s/click.html", addr, addr)
	for ; served > 0; served-- {
		if err := <-errs; err != http.ErrServerClosed {
			return err
		}
	}
	return nil
}

func (p *Player) socket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	c := &pageClient{name: r.URL.Query().Get("name"), out: make(chan []byte, 256)}
	// batch=N: N packets to a message, each under its length, so the page
	// is woken a fraction as often. The first of them waits for the last.
	batch, _ := strconv.Atoi(r.URL.Query().Get("batch"))
	p.mu.Lock()
	p.clients[c] = struct{}{}
	p.mu.Unlock()
	logf("page", "name=%s joined agent=%q", c.name, r.UserAgent())
	defer func() {
		p.mu.Lock()
		delete(p.clients, c)
		p.mu.Unlock()
		logf("page", "name=%s left dropped=%d", c.name, c.dropped.Load())
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if kind == websocket.MessageText {
				logf("page", "name=%s %s", c.name, data)
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case pkt := <-c.out:
			if batch > 1 {
				msg := make([]byte, 0, batch*(4+len(pkt)))
				for n := 1; ; n++ {
					msg = binary.LittleEndian.AppendUint32(msg, uint32(len(pkt)))
					msg = append(msg, pkt...)
					if n == batch {
						break
					}
					select {
					case <-ctx.Done():
						return
					case pkt = <-c.out:
					}
				}
				pkt = msg
			}
			wctx, done := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(wctx, websocket.MessageBinary, pkt)
			done()
			if err != nil {
				return
			}
		}
	}
}
