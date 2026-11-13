package client

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A page's listening gets the sound batch packets to a message, each under
// its length, a message cut short going once the rest are late; and as
// the listening ends, why, before the socket closes (M20.2).
func TestListeningStreamsBatches(t *testing.T) {
	packets := make(chan []byte)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		streamSound(r.Context(), ws, 4, packets, done, func() string { return "taken" })
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	packet := func(n byte) []byte { return []byte{n, n, n, n, n, n, n, n, n, n} }
	split := func(msg []byte) (out [][]byte) {
		for len(msg) >= 4 {
			n := binary.LittleEndian.Uint32(msg)
			out = append(out, msg[4:4+n])
			msg = msg[4+n:]
		}
		if len(msg) != 0 {
			t.Errorf("%d bytes left over", len(msg))
		}
		return out
	}
	for i := range 6 {
		packets <- packet(byte(i))
	}
	kind, msg, err := ws.Read(ctx)
	if err != nil || kind != websocket.MessageBinary {
		t.Fatalf("read %v, %v", kind, err)
	}
	if got := split(msg); len(got) != 4 || got[0][0] != 0 || got[3][0] != 3 || len(got[3]) != 10 {
		t.Errorf("the first message held %d packets", len(got))
	}
	sent := time.Now()
	_, msg, err = ws.Read(ctx)
	if got := split(msg); err != nil || len(got) != 2 || got[1][0] != 5 {
		t.Errorf("the second message held %d packets, %v", len(got), err)
	}
	if d := time.Since(sent); d > 200*time.Millisecond {
		t.Errorf("two packets waited %s for the rest of their message", d)
	}
	close(done)
	kind, msg, err = ws.Read(ctx)
	var end struct{ T, Why string }
	if err != nil || kind != websocket.MessageText || json.Unmarshal(msg, &end) != nil || end.T != "ended" || end.Why != "taken" {
		t.Errorf("the end came as %q, %v", msg, err)
	}
	if _, _, err := ws.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Errorf("the socket closed with %v", err)
	}
}
