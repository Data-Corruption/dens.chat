package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"

	"github.com/coder/websocket"
)

// TestPageStreamSaysWhyItClosed checks the causes the log gives for a
// page's stream closing (M11): the page going away or closing it, its
// connection breaking, a ping it doesn't answer, a write it doesn't take,
// falling behind, and the service stopping.
func TestPageStreamSaysWhyItClosed(t *testing.T) {
	quick := pageTimes{ping: time.Minute, pong: time.Minute, write: time.Minute}
	for _, c := range []struct {
		want  string
		times pageTimes
		// page acts as the page, once it has the dens view; feed acts on
		// the service's side, and stop stops the service.
		page func(*websocket.Conn)
		feed func(calls chan<- denclient.CallEvent, stream chan denclient.PageEvent, stop func())
	}{
		{want: "the page went away", times: quick, page: func(c *websocket.Conn) { c.Close(websocket.StatusGoingAway, "") }},
		{want: "the page closed it with 1000", times: quick, page: func(c *websocket.Conn) { c.Close(websocket.StatusNormalClosure, "") }},
		{want: "the connection broke", times: quick, page: func(c *websocket.Conn) { c.CloseNow() }},
		// A page that stops reading answers no ping, and takes no writes
		// once what's in flight fills the connection.
		{want: "a ping went unanswered", times: pageTimes{ping: 50 * time.Millisecond, pong: 100 * time.Millisecond, write: time.Minute}},
		{want: "a write stalled", times: pageTimes{ping: time.Minute, pong: time.Minute, write: 200 * time.Millisecond},
			feed: func(calls chan<- denclient.CallEvent, _ chan denclient.PageEvent, _ func()) {
				offer := &denclient.CallOffer{Version: 1, SDP: strings.Repeat("v", 1<<20)}
				for range 256 {
					select {
					case calls <- denclient.CallEvent{Offer: offer}:
					case <-time.After(time.Second):
						return
					}
				}
			}},
		// A page that reads answers the service's close at once.
		{want: "it fell behind", times: quick, page: func(c *websocket.Conn) { go func() { _, _, _ = c.Read(context.Background()) }() },
			feed: func(_ chan<- denclient.CallEvent, stream chan denclient.PageEvent, _ func()) { close(stream) }},
		{want: "the service is stopping", times: quick, feed: func(_ chan<- denclient.CallEvent, _ chan denclient.PageEvent, stop func()) { stop() }},
	} {
		t.Run(c.want, func(t *testing.T) {
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			calls := make(chan denclient.CallEvent)
			stream := make(chan denclient.PageEvent)
			p := pageServer{
				view:    func(context.Context) (densView, error) { return densView{Epoch: "e"}, nil },
				changes: make(chan struct{}),
				calls:   calls,
				stream:  stream,
				keys:    make(chan shortcuts.Event),
				handle:  func(context.Context, pageMessage) {},
				times:   c.times,
			}
			why := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer ws.CloseNow()
				why <- p.serve(ctx, ws)
			}))
			defer srv.Close()
			ws, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.CloseNow()
			rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, data, err := ws.Read(rctx); err != nil || !strings.Contains(string(data), `"t":"dens"`) {
				t.Fatalf("the first message: %s, %v", data, err)
			}
			if c.page != nil {
				c.page(ws)
			}
			if c.feed != nil {
				go c.feed(calls, stream, stop)
			}
			select {
			case got := <-why:
				if got != c.want {
					t.Errorf("the stream closed as %q", got)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the stream didn't close")
			}
		})
	}
}
