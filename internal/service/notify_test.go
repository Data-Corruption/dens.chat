package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/notify"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"

	"github.com/coder/websocket"
)

// pageStream is a page's stream with the service, as the page's events.js
// keeps it.
type pageStream struct {
	t  *testing.T
	ws *websocket.Conn
}

func (b *browser) stream() *pageStream {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(b.base, "http")+"/api/events", &websocket.DialOptions{
		HTTPClient: b.http, HTTPHeader: http.Header{"Origin": {b.base}}})
	if err != nil {
		b.t.Fatal(err)
	}
	ws.SetReadLimit(1 << 20)
	b.t.Cleanup(func() { ws.CloseNow() })
	return &pageStream{t: b.t, ws: ws}
}

func (p *pageStream) send(kind string, d any) {
	p.t.Helper()
	data, _ := json.Marshal(map[string]any{"t": kind, "d": d})
	if err := p.ws.Write(context.Background(), websocket.MessageText, data); err != nil {
		p.t.Fatal(err)
	}
}

// next waits for the next message of kind, skipping the rest.
func (p *pageStream) next(kind string) json.RawMessage {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, data, err := p.ws.Read(ctx)
		if err != nil {
			p.t.Fatalf("waiting for %s: %v", kind, err)
		}
		var msg struct {
			T string          `json:"t"`
			D json.RawMessage `json:"d"`
		}
		if json.Unmarshal(data, &msg) == nil && msg.T == kind {
			return msg.D
		}
	}
}

// quiet fails on a message of kind within a moment.
func (p *pageStream) quiet(kind, why string) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	for {
		_, data, err := p.ws.Read(ctx)
		if err != nil {
			return
		}
		var msg struct {
			T string `json:"t"`
		}
		if json.Unmarshal(data, &msg) == nil && msg.T == kind {
			p.t.Fatalf("%s, yet the page got %s", why, data)
		}
	}
}

// Notifications reach the pages through their streams (M12.3): the status
// as it changes, which the settings keep, and each notification, to a page
// the browser lets notify, unless a page shows its channel; reading the
// channel takes it back. Muting needs a den this install joined.
func TestPagesShowNotifications(t *testing.T) {
	a, client := startDevService(t)
	b := pair(t, a, client)
	hidden, shown := b.stream(), b.stream()
	var st notify.Status
	if err := json.Unmarshal(hidden.next("notifications"), &st); err != nil || st.DMs || st.Shell != nil {
		t.Fatalf("the first status %+v, %v; want every kind off, and no shell", st, err)
	}
	shown.next("notifications")

	b.expect(http.StatusOK, "turn DMs on", "POST", "/api/settings", map[string]any{"notifications": map[string]bool{"dms": true}})
	for _, p := range []*pageStream{hidden, shown} {
		if err := json.Unmarshal(p.next("notifications"), &st); err != nil || !st.DMs || st.Mentions {
			t.Fatalf("after DMs turned on, a page heard %+v, %v", st, err)
		}
	}
	var settings struct {
		Notifications notify.Status `json:"notifications"`
	}
	b.expect(http.StatusOK, "the settings", "GET", "/api/settings", nil, &settings)
	if cfg, err := config.View(a.DB); err != nil || !cfg.Notifications.DMs || !settings.Notifications.DMs {
		t.Fatalf("kept %+v, the settings say %+v, %v", cfg.Notifications, settings.Notifications, err)
	}

	hidden.send("page", map[string]bool{"visible": false, "allowed": true})
	shown.send("page", map[string]bool{"visible": true, "allowed": false})
	shown.send("focus", map[string]string{"den": "d1", "channel": "5"})
	// The service reads the pages' lines in their own time.
	time.Sleep(200 * time.Millisecond)
	a.Notify.Message(notify.Message{Den: "d1", DenName: "Den", Channel: "7", DM: true, ID: "10", Author: "Alice", Text: "hi"})
	var n notify.Notification
	if err := json.Unmarshal(hidden.next("notify"), &n); err != nil || n.Key != "d1/7" || n.Text != "hi" || n.Path != "/den/d1/7#message=10" {
		t.Fatalf("the hidden page was sent %+v, %v", n, err)
	}
	a.Notify.Cleared("d1", "7", "10")
	if got := string(hidden.next("unnotify")); got != `{"keys":["d1/7"]}` {
		t.Fatalf("reading the DM sent %s", got)
	}

	shown.send("focus", map[string]string{"den": "d1", "channel": "7"})
	time.Sleep(200 * time.Millisecond)
	a.Notify.Message(notify.Message{Den: "d1", Channel: "7", DM: true, ID: "11", Author: "Alice", Text: "again"})
	hidden.quiet("notify", "a page shows the DM on screen")

	if code, body := b.do("POST", "/api/dens/d1/notifications", map[string]any{"muted": true}); code != http.StatusNotFound {
		t.Fatalf("muting a den this install hasn't joined: %d %s", code, body)
	}
}
