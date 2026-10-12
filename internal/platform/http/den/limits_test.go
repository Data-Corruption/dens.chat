package den

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestMessageBudgetAcrossChannels checks that a member's messages spread
// over many channels are held to one budget (M8), while each channel's own
// limit still holds within it.
func TestMessageBudgetAcrossChannels(t *testing.T) {
	f := newFixture(t)
	token, _ := f.owner()
	s := f.session(token)
	send := func(channel denproto.Channel) int {
		return f.call(http.MethodPost, "/api/channels/"+channel.ID+"/messages", token,
			denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Text: "hello"}, nil)
	}
	first, second, third := f.channel(s, "first", denproto.KindText, false), f.channel(s, "second", denproto.KindText, false),
		f.channel(s, "third", denproto.KindText, false)
	for i := range 5 {
		if code := send(first); code != http.StatusCreated {
			t.Fatalf("message %d in the first channel: %d", i+1, code)
		}
	}
	// The channel's own limit refuses a sixth, and that spends nothing of
	// the budget.
	if code := send(first); code != http.StatusTooManyRequests {
		t.Fatalf("a sixth message at once in one channel: %d", code)
	}
	for i := range 5 {
		if code := send(second); code != http.StatusCreated {
			t.Fatalf("message %d in the second channel: %d", i+1, code)
		}
	}
	if code := send(third); code != http.StatusTooManyRequests {
		t.Fatalf("an eleventh message at once, in a third channel: %d", code)
	}
}

// TestSlowBodyIsCutOff checks that a request's body has bodyDeadline to
// arrive (M8), so a client that stops sending doesn't hold its handler.
func TestSlowBodyIsCutOff(t *testing.T) {
	f := newFixture(t)
	f.owner()
	defer func(d time.Duration) { bodyDeadline = d }(bodyDeadline)
	bodyDeadline = 200 * time.Millisecond

	body, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, f.srv.URL+"/api/auth/challenge", body)
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Content-Type", "application/json")
	go func() { _, _ = w.Write([]byte(`{"client_nonce":`)) }() // and nothing more
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("the den didn't answer a stalled body: %v", err)
	}
	wantError(t, resp, http.StatusBadRequest, denproto.CodeMalformed)
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the den waited %s for a body with a %s deadline", took, bodyDeadline)
	}
}
