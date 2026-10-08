package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/sfu"

	"github.com/coder/websocket"
)

// TestVoiceProbe stands in for a page in a call, for the den e2e: it joins
// a voice channel through an install's page socket, answers the den's
// offers as the page does, with Pion in the browser's place, and passes
// once it has heard another member, and whatever DENS_PROBE_THEN asks for
// after. It runs only when DENS_VOICE_PROBE names the install's client
// listener; the harness sets the rest:
//
//	DENS_VOICE_PROBE    the client listener, such as http://127.0.0.1:8484
//	DENS_PROBE_COOKIE   the paired browser's session cookie, as name=value
//	DENS_PROBE_DEN      the den's ID
//	DENS_PROBE_CHANNEL  the voice channel's ID
//	DENS_PROBE_HEAR     the ID of the member to hear
//	DENS_PROBE_NETWORK  udp or tcp, the one path the call may take: tcp as on
//	                    a network that blocks UDP
//	DENS_PROBE_READY    a file to create once the member is heard, which the
//	                    harness waits for before it acts (M3)
//	DENS_PROBE_THEN     after that: "ride", the den's connection drops, as
//	                    when its proxy restarts, and the call is taken back
//	                    and heard again without a new call; "restart", the
//	                    install's service restarts, as on an update, closing
//	                    the page's socket too, and the call is taken back the
//	                    same way once the socket is dialed again; "silence",
//	                    the member goes quiet, as staff muted them; or
//	                    "ended:REASON", the call ends for that reason
//	DENS_PROBE_STAY     seconds to stay in the call at the end, 5 if unset
func TestVoiceProbe(t *testing.T) {
	base := os.Getenv("DENS_VOICE_PROBE")
	if base == "" {
		t.Skip("the den e2e runs this, against an installed service")
	}
	den, channel, hear := os.Getenv("DENS_PROBE_DEN"), os.Getenv("DENS_PROBE_CHANNEL"), os.Getenv("DENS_PROBE_HEAR")
	network := os.Getenv("DENS_PROBE_NETWORK")
	if network != "udp" && network != "tcp" {
		t.Fatalf("DENS_PROBE_NETWORK is %q, not udp or tcp", network)
	}
	then := os.Getenv("DENS_PROBE_THEN")
	stay := 5 * time.Second
	if v := os.Getenv("DENS_PROBE_STAY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("DENS_PROBE_STAY is %q", v)
		}
		stay = time.Duration(n) * time.Second
	}
	caller, err := sfu.NewTestCaller(sfu.CallerOptions{UDP: network == "udp", TCP: network == "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dial := func() (*websocket.Conn, error) {
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/api/events", &websocket.DialOptions{
			HTTPHeader: http.Header{"Cookie": {os.Getenv("DENS_PROBE_COOKIE")}, "Origin": {base}}})
		if err != nil {
			return nil, err
		}
		ws.SetReadLimit(1 << 20)
		return ws, nil
	}
	first, err := dial()
	if err != nil {
		t.Fatal(err)
	}
	// The page's socket, which "restart" dials again.
	var wsMu sync.Mutex
	ws := first
	socket := func() *websocket.Conn {
		wsMu.Lock()
		defer wsMu.Unlock()
		return ws
	}
	defer func() { socket().CloseNow() }()
	send := func(kind string, d any) error {
		data, err := json.Marshal(map[string]any{"t": kind, "d": d})
		if err != nil {
			return err
		}
		return socket().Write(ctx, websocket.MessageText, data)
	}
	if err := send("voice.join", map[string]any{"den": den, "channel": channel}); err != nil {
		t.Fatal(err)
	}
	// The reader answers offers as the page does, the same answer again to
	// an offer the den sends again after a resume, and passes on the rest.
	// Once closing is set, the socket closing is expected, and says so on
	// closed.
	failed := make(chan error, 1)
	var closing atomic.Bool
	closed := make(chan struct{}, 1)
	ended := make(chan string, 4)
	resumed := make(chan struct{}, 4)
	// back signals the den's connection returning after it dropped: the
	// service says the call's connection dropped before it marks the den
	// offline, so a "connected" counts only after a state that isn't.
	back := make(chan struct{}, 1)
	var mu sync.Mutex
	version, firstAfterResume := 0, 0
	answer := ""
	away, offline := false, false
	read := func(ws *websocket.Conn) {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				if closing.Load() {
					closed <- struct{}{}
				} else {
					failed <- err
				}
				return
			}
			var msg struct {
				T string          `json:"t"`
				D json.RawMessage `json:"d"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			if msg.T == "dens" {
				var d struct {
					Dens []struct {
						ID    string `json:"den_id"`
						State string `json:"state"`
					} `json:"dens"`
				}
				if json.Unmarshal(msg.D, &d) == nil {
					for _, x := range d.Dens {
						mu.Lock()
						switch {
						case x.ID != den || !away:
						case x.State != "connected":
							offline = true
						case offline:
							away, offline = false, false
							select {
							case back <- struct{}{}:
							default:
							}
						}
						mu.Unlock()
					}
				}
				continue
			}
			var e denclient.CallEvent
			if msg.T != "call" || json.Unmarshal(msg.D, &e) != nil {
				continue
			}
			switch {
			case e.Ended != "":
				if e.Ended == denclient.CallDisconnected {
					mu.Lock()
					away = true
					mu.Unlock()
				}
				ended <- e.Ended
			case e.Resumed:
				resumed <- struct{}{}
			case e.Offer != nil:
				mu.Lock()
				if firstAfterResume < 0 {
					firstAfterResume = e.Offer.Version
				}
				if e.Offer.Version != version {
					a, err := caller.Answer(e.Offer.SDP)
					if err != nil {
						mu.Unlock()
						failed <- err
						return
					}
					version, answer = e.Offer.Version, a
				}
				reply := answer
				mu.Unlock()
				if err := send("voice.answer", map[string]any{"den": den, "version": e.Offer.Version, "sdp": reply}); err != nil {
					failed <- err
					return
				}
			}
		}
	}
	go read(first)
	wait := func(what string, timeout time.Duration, done <-chan error) {
		t.Helper()
		select {
		case err := <-failed:
			t.Fatal(err)
		case reason := <-ended:
			t.Fatalf("waiting for %s, the call ended: %s", what, reason)
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(timeout):
			t.Fatalf("timed out waiting for %s", what)
		}
	}
	heard := func(n int) <-chan error {
		c := make(chan error, 1)
		go func() { c <- caller.WaitHeard(hear, n, 60*time.Second) }()
		return c
	}
	wait("member "+hear, 70*time.Second, heard(100))
	path := caller.Path()
	if path != network {
		t.Fatalf("the call went over %q, not %s", path, network)
	}
	t.Logf("heard member %s over %s", hear, path)
	closing.Store(then == "restart")
	if ready := os.Getenv("DENS_PROBE_READY"); ready != "" {
		if err := os.WriteFile(ready, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// resume takes the held call back, as the page does once the den's
	// connection is back, and hears the member on the same call.
	resume := func(what string) {
		t.Helper()
		mu.Lock()
		before := version
		firstAfterResume = -1
		mu.Unlock()
		if err := send("voice.join", map[string]any{"den": den, "channel": channel, "resume": true}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-resumed:
		case reason := <-ended:
			t.Fatalf("resuming ended the call: %s", reason)
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(15 * time.Second):
			t.Fatal("the call wasn't resumed")
		}
		wait("member "+hear+" after resuming", 30*time.Second, heard(100))
		mu.Lock()
		after := firstAfterResume
		mu.Unlock()
		// A new call would start its offers over at 1.
		if after > 0 && after < before {
			t.Fatalf("an offer after resuming has version %d, below %d", after, before)
		}
		t.Logf("rode out %s, offers at version %d", what, before)
	}

	switch {
	case then == "ride":
		// The install's connection to the den drops, and the den holds the
		// call, whose media keeps coming.
		select {
		case reason := <-ended:
			if reason != denclient.CallDisconnected {
				t.Fatalf("the call ended %q, not disconnected", reason)
			}
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(60 * time.Second):
			t.Fatal("the den's connection didn't drop")
		}
		wait("member "+hear+" while the den was away", 30*time.Second, heard(50))
		select {
		case <-back:
		case <-time.After(60 * time.Second):
			t.Fatal("the den's connection didn't come back")
		}
		resume("the den's connection dropping")
	case then == "restart":
		// The service stops, closing the page's socket, and its connection
		// to the den drops with it: the den holds the call, whose media
		// keeps coming. The service may say so before the socket closes.
		select {
		case <-closed:
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(60 * time.Second):
			t.Fatal("the service didn't stop")
		}
		for len(ended) > 0 {
			if reason := <-ended; reason != denclient.CallDisconnected {
				t.Fatalf("the call ended %q as the service stopped", reason)
			}
		}
		select {
		case <-back:
		default:
		}
		wait("member "+hear+" while the service was away", 30*time.Second, heard(50))
		var again *websocket.Conn
		for deadline := time.Now().Add(60 * time.Second); ; {
			if again, err = dial(); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the service didn't come back: %v", err)
			}
			time.Sleep(250 * time.Millisecond)
		}
		// The service is new, so the first "connected" it says is news.
		mu.Lock()
		away, offline = true, true
		mu.Unlock()
		closing.Store(false)
		wsMu.Lock()
		ws = again
		wsMu.Unlock()
		go read(again)
		select {
		case <-back:
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(60 * time.Second):
			t.Fatal("the service didn't reach the den again")
		}
		resume("the service restarting")
	case then == "silence":
		deadline := time.Now().Add(60 * time.Second)
		last, since := caller.Heard(hear), time.Now()
		for time.Since(since) < 2*time.Second {
			if time.Now().After(deadline) {
				t.Fatalf("member %s never went quiet", hear)
			}
			time.Sleep(250 * time.Millisecond)
			if n := caller.Heard(hear); n != last {
				last, since = n, time.Now()
			}
		}
		t.Logf("member %s went quiet", hear)
	case strings.HasPrefix(then, "ended:"):
		want := strings.TrimPrefix(then, "ended:")
		select {
		case reason := <-ended:
			if reason != want {
				t.Fatalf("the call ended %q, not %q", reason, want)
			}
			t.Logf("the call ended: %s", reason)
			return
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(60 * time.Second):
			t.Fatalf("the call didn't end %s", want)
		}
	case then != "":
		t.Fatalf("DENS_PROBE_THEN is %q", then)
	}
	// The other side may still be waiting to hear this one.
	time.Sleep(stay)
	_ = send("voice.leave", map[string]string{"den": den})
	socket().Close(websocket.StatusNormalClosure, "")
}
