package den

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/coder/websocket"
)

type fixture struct {
	t   *testing.T
	d   *dens.Den
	srv *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	log, err := xlog.New(filepath.Join(dir, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.New(filepath.Join(dir, "db"), log, build.BuildInfo{DefaultLogLevel: "warn"}, database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.New(denproto.Random(vault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	d, err := dens.Open(context.Background(), db, v, log)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, d: d, srv: httptest.NewServer(New(&app.App{Den: d, Log: log}))}
	t.Cleanup(func() {
		f.srv.Listener.Close()
		d.CloseSockets(time.Second)
		f.srv.Close()
		d.Close()
		db.Close()
		v.Close()
		log.Close()
	})
	return f
}

func (f *fixture) post(path string, body any, header http.Header) *http.Response {
	f.t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+path, bytes.NewReader(data))
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	return resp
}

func wantError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	defer resp.Body.Close()
	err := denproto.ReadError(resp)
	if resp.StatusCode != status || !denproto.IsCode(err, code) {
		t.Fatalf("got %d %v, want %d %s", resp.StatusCode, err, status, code)
	}
}

// owner creates the den and joins it, returning a session token.
func (f *fixture) owner() (token denproto.Bytes, key ed25519.PrivateKey) {
	f.t.Helper()
	code, err := f.d.Create(context.Background(), "Den", "https://den.test")
	if err != nil {
		f.t.Fatal(err)
	}
	_, key, _ = ed25519.GenerateKey(nil)
	info, _ := f.d.Info()
	resp, err := f.d.Challenge(denproto.Random(denproto.NonceSize))
	if err != nil {
		f.t.Fatal(err)
	}
	joined, err := f.d.Join(context.Background(), denproto.JoinRequest{
		Invite: code, Username: "alice", DisplayName: "Alice", Verifier: denproto.Random(32),
		PublicKey: denproto.Bytes(key.Public().(ed25519.PublicKey)), DeviceLabel: "test",
		Nonce: resp.Nonce, Proof: denproto.Prove(key, info.ID, resp.Nonce),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return joined.Token, key
}

func (f *fixture) dial(token denproto.Bytes, resume string) (*websocket.Conn, []denproto.Event) {
	f.t.Helper()
	h := http.Header{}
	h.Set(denproto.HeaderVersion, "1")
	h.Set("Authorization", "Bearer "+token.String())
	u := f.srv.URL + "/api/ws"
	if resume != "" {
		u += "?resume=" + resume
	}
	c, _, err := websocket.Dial(context.Background(), u, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		f.t.Fatal(err)
	}
	return c, read(f.t, c)
}

func read(t *testing.T, c *websocket.Conn) []denproto.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events, err := denproto.DecodeFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestVersionAndCreation(t *testing.T) {
	f := newFixture(t)
	resp := f.post("/api/auth/challenge", denproto.ChallengeRequest{ClientNonce: denproto.Random(32)}, http.Header{denproto.HeaderVersion: {"99"}})
	if resp.Header.Get(denproto.HeaderRange) != denproto.RangeHeader() {
		t.Fatalf("range header %q", resp.Header.Get(denproto.HeaderRange))
	}
	wantError(t, resp, http.StatusUpgradeRequired, denproto.CodeProtocolUnsupported)
	wantError(t, f.post("/api/auth/challenge", denproto.ChallengeRequest{ClientNonce: denproto.Random(32)}, nil),
		http.StatusServiceUnavailable, denproto.CodeDenNotCreated)

	f.owner()
	resp = f.post("/api/auth/challenge", denproto.ChallengeRequest{ClientNonce: denproto.Random(32)}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge: %d", resp.StatusCode)
	}
	resp.Body.Close()
	wantError(t, f.post("/api/auth/challenge", map[string]any{"client_nonce": "short"}, nil), http.StatusBadRequest, denproto.CodeMalformed)
	wantError(t, f.post("/api/invites", denproto.InviteCreateRequest{}, nil), http.StatusUnauthorized, denproto.CodeUnauthorized)
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")
	if got := clientIP(r).String(); got != "198.51.100.7" {
		t.Fatalf("through the proxy: %s", got)
	}
	r.RemoteAddr = "192.0.2.1:5000"
	if got := clientIP(r).String(); got != "192.0.2.1" {
		t.Fatalf("a non-loopback peer set X-Forwarded-For and got %s", got)
	}
}

func TestSocketResumeAndClose(t *testing.T) {
	f := newFixture(t)
	token, _ := f.owner()
	c, first := f.dial(token, "")
	if len(first) != 1 || first[0].T != denproto.EventReady {
		t.Fatalf("first frame %+v", first)
	}
	var ready denproto.Ready
	json.Unmarshal(first[0].D, &ready)
	if ready.Me.Username != "alice" || ready.Den.Name != "Den" {
		t.Fatalf("ready %+v", ready)
	}
	c.Close(websocket.StatusNormalClosure, "")

	// Events published while the client is away arrive after "resumed".
	s, _ := f.d.Authenticate(context.Background(), token)
	for i := range 3 {
		name := "Den " + strconv.Itoa(i)
		if _, err := f.d.Update(context.Background(), s, denproto.DenUpdateRequest{Name: &name}); err != nil {
			t.Fatal(err)
		}
	}
	c, events := f.dial(token, ready.Epoch+"."+strconv.FormatUint(ready.Seq, 10))
	if len(events) != 4 || events[0].T != denproto.EventResumed || events[3].Seq != ready.Seq+3 {
		t.Fatalf("resume frame %+v", events)
	}
	c.CloseNow()

	// A stale epoch gets a fresh snapshot instead.
	c, events = f.dial(token, "0000000000000000.1")
	if events[0].T != denproto.EventReady {
		t.Fatalf("stale resume answered %+v", events)
	}

	// Shutting down tells the client the den is restarting.
	go f.d.CloseSockets(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusServiceRestart {
		t.Fatalf("close status %v", err)
	}
}

func TestSocketClosesOnExpiry(t *testing.T) {
	f := newFixture(t)
	f.d.TokenLifetime = 300 * time.Millisecond
	token, _ := f.owner()
	c, _ := f.dial(token, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if websocket.CloseStatus(err) != denproto.CloseSessionExpired {
		t.Fatalf("close status %v", err)
	}
}
