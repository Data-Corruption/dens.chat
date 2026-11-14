package client

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/relay"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// Relay (M20): the member's other installs, as they said they're here,
// and from M20.2 another device's sound, played by the page that listens,
// and this computer's, which the member stops sending.
func (rt *router) mountRelay(r chi.Router) {
	r.Get("/api/relay/devices", rt.handleRelayDevices)
	r.Get("/api/relay/listen", rt.handleRelayListen)
	r.Post("/api/relay/stop", rt.handleRelayStop)
}

// handleRelayDevices lists the member's other installs that said they're
// here (M20), and asks them again; and says whether the member refused
// this install its network, where the system asks them, as Android does.
func (rt *router) handleRelayDevices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"devices": rt.a.Relay.Devices(), "refused": rt.a.Relay.Refused()})
}

// How the sound reaches the page: binary messages of several packets, so
// the page wakes a fraction as often, each packet its length, 4 bytes
// little-endian, then the packet. A message waits for its last packet at
// most a packet's time past them all, so one cut short by a gap goes as
// it is.
const (
	defaultBatch = 4
	mostBatch    = 16
	packetTime   = 5 * time.Millisecond
	soundWrite   = 5 * time.Second
)

// handleRelayListen is a page's listening to another of the member's
// devices' sound, by its Relay ID (M20.2), which lasts while the socket
// does: the packets, batch to a message, then as it ends a text message
// saying why, before the socket closes. The page closing the socket stops
// it, and a newer listening takes over from it.
func (rt *router) handleRelayListen(w http.ResponseWriter, r *http.Request) {
	batch, err := strconv.Atoi(r.URL.Query().Get("batch"))
	if err != nil || batch < 1 || batch > mostBatch {
		batch = defaultBatch
	}
	// Accept checks the Origin against the Host, which the Host guard has
	// already pinned to this listener, so other sites can't open it.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer c.CloseNow()
	l, err := rt.a.Relay.Listen(r.URL.Query().Get("device"))
	if err != nil {
		soundEnded(r.Context(), c, relay.WhyUnshared)
		return
	}
	defer l.Stop()
	streamSound(r.Context(), c, batch, l.Packets(), l.Done(), l.Why)
}

// streamSound writes the packets to the page's socket, batch to a
// message, until done, when it says why and closes the socket, or until
// the page closes it or ctx ends.
func streamSound(ctx context.Context, c *websocket.Conn, batch int, packets <-chan []byte, done <-chan struct{}, why func() string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The page sends nothing; its closing ends the read.
	c.SetReadLimit(1024)
	go func() {
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	var msg []byte
	n := 0
	wait := time.NewTimer(time.Hour)
	wait.Stop()
	defer wait.Stop()
	send := func() bool {
		wctx, wcancel := context.WithTimeout(ctx, soundWrite)
		defer wcancel()
		err := c.Write(wctx, websocket.MessageBinary, msg)
		msg, n = msg[:0], 0
		return err == nil
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			if n > 0 && !send() {
				return
			}
			soundEnded(ctx, c, why())
			return
		case pkt := <-packets:
			msg = binary.LittleEndian.AppendUint32(msg, uint32(len(pkt)))
			msg = append(msg, pkt...)
			if n++; n == 1 {
				wait.Reset(time.Duration(batch+1) * packetTime)
			}
			if n >= batch {
				wait.Stop()
				if !send() {
					return
				}
			}
		case <-wait.C:
			if n > 0 && !send() {
				return
			}
		}
	}
}

// soundEnded tells the page why its listening ended, and closes the
// socket.
func soundEnded(ctx context.Context, c *websocket.Conn, why string) {
	b, _ := json.Marshal(map[string]string{"t": "ended", "why": why})
	wctx, cancel := context.WithTimeout(ctx, soundWrite)
	defer cancel()
	if c.Write(wctx, websocket.MessageText, b) == nil {
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
}

// handleRelayStop stops this computer's sound going to the device that
// listens, which is told (M20.2).
func (rt *router) handleRelayStop(w http.ResponseWriter, _ *http.Request) {
	rt.a.Relay.StopSharing()
	writeJSON(w, map[string]bool{"ok": true})
}
