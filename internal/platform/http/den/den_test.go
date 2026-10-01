package den

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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
	storage := dens.Storage{Dir: filepath.Join(dir, "uploads"), Temp: filepath.Join(dir, "tmp")}
	for _, d := range []string{storage.Dir, storage.Temp} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	d, err := dens.Open(context.Background(), db, v, log, storage)
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
		Nonce: resp.Nonce, Proof: denproto.Prove(key, info.ID, resp.Nonce), SealCheck: denproto.Random(denproto.SealCheckSize),
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
	// As much as a client reads, and no more.
	c.SetReadLimit(denproto.MaxBody)
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
	// Presence isn't replayed, so who's online comes right after "resumed".
	c, events := f.dial(token, ready.Epoch+"."+strconv.FormatUint(ready.Seq, 10))
	if len(events) != 5 || events[0].T != denproto.EventResumed || events[1].T != denproto.EventPresence || events[4].Seq != ready.Seq+3 {
		t.Fatalf("resume frame %+v", events)
	}
	var online denproto.Presence
	if json.Unmarshal(events[1].D, &online); !online.Full || len(online.Online) != 1 {
		t.Fatalf("presence on resume %+v", online)
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

// member joins a second member with an invite from the owner, and returns
// their token.
func (f *fixture) member(ownerToken denproto.Bytes, username string) denproto.Bytes {
	f.t.Helper()
	owner, err := f.d.Authenticate(context.Background(), ownerToken)
	if err != nil {
		f.t.Fatal(err)
	}
	inv, err := f.d.CreateInvite(context.Background(), owner, denproto.InviteCreateRequest{})
	if err != nil {
		f.t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	info, _ := f.d.Info()
	resp, err := f.d.Challenge(denproto.Random(denproto.NonceSize))
	if err != nil {
		f.t.Fatal(err)
	}
	joined, err := f.d.Join(context.Background(), denproto.JoinRequest{
		Invite: inv.Code, Username: username, DisplayName: username, Verifier: denproto.Random(32),
		PublicKey: denproto.Bytes(key.Public().(ed25519.PublicKey)), DeviceLabel: "test",
		Nonce: resp.Nonce, Proof: denproto.Prove(key, info.ID, resp.Nonce), SealCheck: denproto.Random(denproto.SealCheckSize),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return joined.Token
}

// waitPresence reads frames until a presence event matches.
func waitPresence(t *testing.T, c *websocket.Conn, match func(denproto.Presence) bool) {
	t.Helper()
	for {
		for _, e := range read(t, c) {
			var p denproto.Presence
			if e.T == denproto.EventPresence && json.Unmarshal(e.D, &p) == nil && match(p) {
				return
			}
		}
	}
}

func TestPresence(t *testing.T) {
	f := newFixture(t)
	f.d.PresenceDelay = 50 * time.Millisecond
	alice, _ := f.owner()
	bob := f.member(alice, "bob")
	c, first := f.dial(alice, "")
	var ready denproto.Ready
	if json.Unmarshal(first[0].D, &ready); !slices.Contains(ready.Online, ready.Me.ID) {
		t.Fatalf("alice isn't online in her own snapshot: %v", ready.Online)
	}
	defer c.CloseNow()
	b, first := f.dial(bob, "")
	json.Unmarshal(first[0].D, &ready)
	bobID := ready.Me.ID
	waitPresence(t, c, func(p denproto.Presence) bool { return slices.Contains(p.Online, bobID) })
	b.Close(websocket.StatusNormalClosure, "")
	waitPresence(t, c, func(p denproto.Presence) bool { return slices.Contains(p.Offline, bobID) })
}

// upload sends a file's bytes as a client does, with the given length, or
// chunked when it's -1.
func (f *fixture) upload(token denproto.Bytes, name string, body io.Reader, length int64) *http.Response {
	f.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/api/uploads", body)
	req.ContentLength = length
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Authorization", "Bearer "+token.String())
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(denproto.HeaderFilename, url.PathEscape(name))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	return resp
}

func TestUploadsOverHTTP(t *testing.T) {
	f := newFixture(t)
	token, _ := f.owner()
	text := []byte("notes, of a sort\n")
	resp := f.upload(token, "ünïcode notes.txt", bytes.NewReader(text), int64(len(text)))
	var file denproto.File
	json.NewDecoder(resp.Body).Decode(&file)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || file.Name != "ünïcode notes.txt" || file.Type != "text/plain" || file.Size != int64(len(text)) {
		t.Fatalf("%d %+v", resp.StatusCode, file)
	}

	req, _ := http.NewRequest(http.MethodGet, f.srv.URL+"/api/files/"+file.ID, nil)
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Authorization", "Bearer "+token.String())
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(body, text) || got.Header.Get("Content-Type") != "application/octet-stream" ||
		got.Header.Get("Content-Disposition") != "attachment" || got.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("serving: %q %v", body, got.Header)
	}
	req.URL.Path += "/thumb"
	got, _ = http.DefaultClient.Do(req)
	wantError(t, got, http.StatusNotFound, denproto.CodeNotFound)

	// A chunked body is refused once it passes the limit. (A stated length
	// over it is refused before the body is read, which the den's own tests
	// cover: over HTTP the client may still be sending when the den hangs
	// up.)
	resp = f.upload(token, "big", io.LimitReader(zeros{}, denproto.DefaultFileSize+1), -1)
	wantError(t, resp, http.StatusRequestEntityTooLarge, denproto.CodeTooLarge)

	resp = f.upload(token, "x", bytes.NewReader(text), int64(len(text)))
	resp.Body.Close()
	req, _ = http.NewRequest(http.MethodPost, f.srv.URL+"/api/uploads", bytes.NewReader(text))
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Authorization", "Bearer "+token.String())
	req.Header.Set(denproto.HeaderFilename, "%zz")
	resp, _ = http.DefaultClient.Do(req)
	wantError(t, resp, http.StatusBadRequest, denproto.CodeInvalidField)
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// Revoking a device closes its sockets at once, and only its.
// Signing a device out, by revoking its key or by changing the password,
// closes its sockets at once with the reason, and the member's other
// devices hear it's gone.
func TestSigningOutClosesThatDevicesSockets(t *testing.T) {
	for _, tc := range []struct {
		reason  string
		signOut func(d *dens.Den, s *dens.Session, laptop []byte, verifier denproto.Bytes) error
	}{
		{denproto.CloseReasonKeyRevoked, func(d *dens.Den, s *dens.Session, laptop []byte, _ denproto.Bytes) error {
			return d.RevokeDevice(context.Background(), s, laptop)
		}},
		{denproto.CloseReasonPasswordChanged, func(d *dens.Den, s *dens.Session, _ []byte, verifier denproto.Bytes) error {
			_, err := d.ChangePassword(context.Background(), s, denproto.PasswordChangeRequest{Verifier: verifier, NewVerifier: verifier})
			return err
		}},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			code, err := f.d.Create(ctx, "Den", "https://den.test")
			if err != nil {
				t.Fatal(err)
			}
			info, _ := f.d.Info()
			verifier := denproto.Verifier("password", info.ID, "alice")
			signIn := func(key ed25519.PrivateKey) denproto.Bytes {
				resp, err := f.d.Challenge(denproto.Random(denproto.NonceSize))
				if err != nil {
					t.Fatal(err)
				}
				pub := denproto.Bytes(key.Public().(ed25519.PublicKey))
				proof := denproto.Prove(key, info.ID, resp.Nonce)
				joined, err := f.d.Join(ctx, denproto.JoinRequest{Invite: code, Username: "alice", DisplayName: "Alice", Verifier: verifier,
					PublicKey: pub, DeviceLabel: "desktop", Nonce: resp.Nonce, Proof: proof, SealCheck: denproto.Random(denproto.SealCheckSize)})
				if err != nil {
					t.Fatal(err)
				}
				return joined.Token
			}
			_, desktopKey, _ := ed25519.GenerateKey(nil)
			_, laptopKey, _ := ed25519.GenerateKey(nil)
			desktopToken := signIn(desktopKey)
			laptopToken := f.approve(desktopToken, "alice", verifier, laptopKey)
			desktop, _ := f.dial(desktopToken, "")
			laptop, _ := f.dial(laptopToken, "")
			s, _ := f.d.Authenticate(ctx, desktopToken)
			if err := tc.signOut(f.d, s, denproto.ID(laptopKey.Public().(ed25519.PublicKey)), verifier); err != nil {
				t.Fatal(err)
			}
			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			for {
				_, _, err := laptop.Read(rctx)
				if err == nil {
					continue // the event about its own removal can come first
				}
				var ce websocket.CloseError
				if !errors.As(err, &ce) || ce.Code != denproto.CloseRevoked || ce.Reason != tc.reason {
					t.Fatalf("the laptop's socket ended with %v", err)
				}
				break
			}
			// The desktop hears that the laptop is gone, and stays connected.
			events := read(t, desktop)
			if len(events) != 1 || events[0].T != denproto.EventDeviceRemoved {
				t.Fatalf("the desktop got %+v", events)
			}
		})
	}
}
