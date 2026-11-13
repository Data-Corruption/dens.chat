package client_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// TestHelperProbe stands in for the shortcut helper of an installed
// instance, for the den e2e (M20.1): once the service says a call is on,
// as it does while it links to another of the member's installs holding
// one, it holds push to talk, until the file DENS_HELPER_UNTIL names
// exists, then lets go. It runs only when DENS_HELPER_PROBE names the
// instance, as its desktop user.
func TestHelperProbe(t *testing.T) {
	instance := os.Getenv("DENS_HELPER_PROBE")
	if instance == "" {
		t.Skip("the den e2e runs this, against an installed service")
	}
	until := os.Getenv("DENS_HELPER_UNTIL")
	l, err := layout.New("dens", instance, layout.Installed)
	if err != nil {
		t.Fatal(err)
	}
	account, err := host.LookupUser(l.Account)
	if err != nil {
		t.Fatal(err)
	}
	client := control.Client{Endpoint: l.ControlEndpoint, Server: host.ControlServer{ServiceName: l.ServiceName, Account: account}, Timeout: 30 * time.Second}
	s, err := client.Open(control.Request{Op: control.OpShortcuts})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls := make(chan bool, 16)
	go func() {
		defer close(calls)
		for {
			m, err := s.Recv()
			if err != nil {
				return
			}
			var st struct {
				Call bool `json:"call"`
			}
			if m.T == "state" && json.Unmarshal(m.D, &st) == nil {
				calls <- st.Call
			}
		}
	}()
	deadline := time.After(2 * time.Minute)
	for on := false; !on; {
		select {
		case call, open := <-calls:
			if !open {
				t.Fatal("the service closed the helper's stream")
			}
			on = call
		case <-deadline:
			t.Fatal("the service never said a call is on")
		}
	}
	t.Log("the service says a call is on")
	if err := s.Send("source", map[string]any{"covers": []string{"ptt"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send("press", map[string]any{"action": "ptt", "down": true}); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(2 * time.Minute); ; {
		if _, err := os.Stat(until); err == nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("the press was never seen")
		}
		if err := s.Send("ping", nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	_ = s.Send("press", map[string]any{"action": "ptt", "down": false})
}
