package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/sfu"

	"github.com/coder/websocket"
)

// TestVoiceProbe stands in for a page in a call, for the den e2e: it joins
// a voice channel through an install's page socket, answers the den's
// offers as the page does, with Pion in the browser's place, and passes
// once it has heard another member. It runs only when DENS_VOICE_PROBE
// names the install's client listener; the harness sets the rest:
//
//	DENS_VOICE_PROBE    the client listener, such as http://127.0.0.1:8484
//	DENS_PROBE_COOKIE   the paired browser's session cookie, as name=value
//	DENS_PROBE_DEN      the den's ID
//	DENS_PROBE_CHANNEL  the voice channel's ID
//	DENS_PROBE_HEAR     the ID of the member to hear
//	DENS_PROBE_NETWORK  udp or tcp, the one path the call may take: tcp as on
//	                    a network that blocks UDP
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
	caller, err := sfu.NewTestCaller(sfu.CallerOptions{UDP: network == "udp", TCP: network == "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/api/events", &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {os.Getenv("DENS_PROBE_COOKIE")}, "Origin": {base}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(1 << 20)
	send := func(kind string, d any) error {
		data, err := json.Marshal(map[string]any{"t": kind, "d": d})
		if err != nil {
			return err
		}
		return ws.Write(ctx, websocket.MessageText, data)
	}
	if err := send("voice.join", map[string]string{"den": den, "channel": channel}); err != nil {
		t.Fatal(err)
	}
	failed := make(chan error, 1)
	go func() {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				failed <- err
				return
			}
			var msg struct {
				T string              `json:"t"`
				D denclient.CallEvent `json:"d"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.T != "call" {
				continue
			}
			if msg.D.Ended != "" {
				failed <- fmt.Errorf("the call ended: %s", msg.D.Ended)
				return
			}
			answer, err := caller.Answer(msg.D.Offer.SDP)
			if err == nil {
				err = send("voice.answer", map[string]any{"den": den, "version": msg.D.Offer.Version, "sdp": answer})
			}
			if err != nil {
				failed <- err
				return
			}
		}
	}()
	heard := make(chan error, 1)
	go func() { heard <- caller.WaitHeard(hear, 100, 60*time.Second) }()
	select {
	case err := <-failed:
		t.Fatal(err)
	case err := <-heard:
		if err != nil {
			t.Fatal(err)
		}
	}
	path := caller.Path()
	if path != network {
		t.Fatalf("the call went over %q, not %s", path, network)
	}
	t.Logf("heard member %s over %s", hear, path)
	// The other side may still be waiting to hear this one.
	time.Sleep(5 * time.Second)
	_ = send("voice.leave", map[string]string{"den": den})
	ws.Close(websocket.StatusNormalClosure, "")
}
