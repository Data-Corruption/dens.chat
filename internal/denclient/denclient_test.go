package denclient_test

import (
	"context"
	"crypto/tls"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	denhttp "github.com/Data-Corruption/dens.chat/internal/platform/http/den"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// store is one install's database, vault and log.
type store struct {
	db  *sql.DB
	v   *vault.Vault
	log *xlog.Logger
}

func newStore(t *testing.T) store {
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
	t.Cleanup(func() { db.Close(); v.Close(); log.Close() })
	return store{db, v, log}
}

// denHost runs a den the way an install does: a public TLS address (Caddy's
// part, here https://example.com, which httptest's certificate covers) and
// a plain loopback listener for the owner's own client.
type denHost struct {
	t        *testing.T
	s        store
	d        *den.Den
	public   *httptest.Server
	loopback *httptest.Server
}

func startDen(t *testing.T, s store, lifetime time.Duration) *denHost {
	t.Helper()
	h := &denHost{t: t, s: s}
	h.open(lifetime, nil, "", "")
	t.Cleanup(h.stop)
	return h
}

// open starts the den's servers, on the given addresses to simulate a
// restart, or on fresh ones.
func (h *denHost) open(lifetime time.Duration, tlsConfig *tls.Config, publicAddr, loopbackAddr string) {
	h.t.Helper()
	d, err := den.Open(context.Background(), h.s.db, h.s.v, h.s.log)
	if err != nil {
		h.t.Fatal(err)
	}
	d.TokenLifetime = lifetime
	h.d = d
	handler := denhttp.New(&app.App{Den: d, Log: h.s.log})
	h.public = httptest.NewUnstartedServer(handler)
	h.loopback = httptest.NewUnstartedServer(handler)
	if publicAddr != "" {
		h.public.Listener.Close()
		h.public.Listener = listen(h.t, publicAddr)
		h.loopback.Listener.Close()
		h.loopback.Listener = listen(h.t, loopbackAddr)
		h.public.TLS = tlsConfig
	}
	h.public.StartTLS()
	h.loopback.Start()
}

func listen(t *testing.T, addr string) net.Listener {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *denHost) stop() {
	if h.public == nil {
		return
	}
	// The order of a real shutdown: close the listeners, then tell sockets
	// the den is restarting.
	h.public.Listener.Close()
	h.loopback.Listener.Close()
	h.d.CloseSockets(2 * time.Second)
	h.public.Close()
	h.loopback.Close()
	h.d.Close()
	h.public = nil
}

// restart replaces the den process on the same addresses: a new epoch.
func (h *denHost) restart() {
	h.t.Helper()
	tlsConfig := h.public.TLS
	publicAddr, loopbackAddr := h.public.Listener.Addr().String(), h.loopback.Listener.Addr().String()
	lifetime := h.d.TokenLifetime
	h.stop()
	h.open(lifetime, tlsConfig, publicAddr, loopbackAddr)
}

func (h *denHost) own() denclient.OwnDen {
	return func() (denproto.Den, string, bool) {
		info, ok := h.d.Info()
		return info, h.loopback.URL, ok
	}
}

// client starts a Manager whose HTTP client trusts the test certificate and
// reaches example.com at the den's public server.
func (h *denHost) client(t *testing.T, own denclient.OwnDen) *denclient.Manager {
	t.Helper()
	s := newStore(t)
	m := denclient.New(s.db, s.v, s.log, "dens-test", own)
	public := h.public
	transport := public.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, "example.com:") {
			addr = public.Listener.Addr().String()
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	m.HTTP = &http.Client{Timeout: 10 * time.Second, Transport: transport}
	m.MinTokenLife = 200 * time.Millisecond
	m.RenewLead = 600 * time.Millisecond
	m.RenewRetry = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := m.Run(ctx); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
	return m
}

func noOwnDen() (denproto.Den, string, bool) { return denproto.Den{}, "", false }

// waitFor polls until cond holds for the manager's only den.
func waitFor(t *testing.T, m *denclient.Manager, what string, cond func(denclient.Status) bool) denclient.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		list := m.Statuses()
		if len(list) == 1 && cond(list[0]) {
			return list[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", what, list)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func connected(s denclient.Status) bool { return s.State == denclient.StateConnected }

func join(t *testing.T, m *denclient.Manager, invite, username string) denclient.Status {
	t.Helper()
	req := denclient.JoinRequest{Username: username, DisplayName: strings.ToUpper(username[:1]) + username[1:], Password: "correct horse"}
	var err error
	for range 100 { // Run may not have started yet
		_, _, err = m.Join(context.Background(), invite, req)
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	return waitFor(t, m, username+" connected", connected)
}

func TestJoinConnectAndRestart(t *testing.T) {
	h := startDen(t, newStore(t), time.Hour)
	code, err := h.d.Create(context.Background(), "Test Den", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}

	owner := h.client(t, h.own())
	var joined denclient.Status
	for range 100 {
		joined, _, err = owner.JoinOwn(context.Background(), code, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "correct horse"})
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !joined.Own || joined.Role != denproto.RoleOwner || joined.URL != "https://example.com" {
		t.Fatalf("owner status %+v", joined)
	}
	waitFor(t, owner, "owner connected", connected)

	invite, _, err := owner.CreateInvite(context.Background(), joined.DenID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	member := h.client(t, noOwnDen)
	info, err := member.Preview(context.Background(), invite)
	if err != nil || info.Name != "Test Den" {
		t.Fatalf("preview %+v, %v", info, err)
	}
	status := join(t, member, invite, "bob")
	if status.Own || status.Role != denproto.RoleMember || status.Name != "Test Den" {
		t.Fatalf("member status %+v", status)
	}
	if _, _, err := member.Join(context.Background(), invite, denclient.JoinRequest{Username: "bob2", DisplayName: "B", Password: "correct horse"}); err != denclient.ErrAlreadyJoined {
		t.Fatalf("second join: %v", err)
	}

	name := "Renamed Den"
	if err := owner.UpdateDen(context.Background(), joined.DenID, denproto.DenUpdateRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, member, "the rename to arrive", func(s denclient.Status) bool { return s.Name == name })

	epoch := h.d.Hub.Epoch()
	h.restart()
	if h.d.Hub.Epoch() == epoch {
		t.Fatal("the restarted den kept its epoch")
	}
	waitFor(t, member, "reconnect after a den restart", func(s denclient.Status) bool {
		return connected(s) && s.Since > status.Since
	})
	name = "After Restart"
	if err := owner.UpdateDen(context.Background(), joined.DenID, denproto.DenUpdateRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, member, "events after the restart", func(s denclient.Status) bool { return s.Name == name })
}

func TestRenewalKeepsTheSocket(t *testing.T) {
	h := startDen(t, newStore(t), 1500*time.Millisecond)
	code, err := h.d.Create(context.Background(), "Short Tokens", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	m := h.client(t, h.own())
	for range 100 {
		_, _, err = m.JoinOwn(context.Background(), code, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "correct horse"})
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	first := waitFor(t, m, "connected", connected)
	// Three token lifetimes: without renewal the den would close the socket
	// with 4001, and the status would change.
	time.Sleep(4500 * time.Millisecond)
	now := m.Statuses()[0]
	if !connected(now) || now.Since != first.Since {
		t.Fatalf("the socket didn't survive renewal: %+v, first connected %+v", now, first)
	}
}

func TestPinnedIdentityMismatch(t *testing.T) {
	h := startDen(t, newStore(t), time.Hour)
	if _, err := h.d.Create(context.Background(), "Real Den", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	m := h.client(t, noOwnDen)
	forged := denproto.EncodeInvite(denproto.Random(denproto.IDSize), denproto.Random(denproto.InviteCodeSize), "https://example.com")
	if _, err := m.Preview(context.Background(), forged); err == nil || !strings.Contains(err.Error(), "identity key") {
		t.Fatalf("preview of a den with another identity: %v", err)
	}
}
