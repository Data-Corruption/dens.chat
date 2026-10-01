package denclient_test

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	denhttp "github.com/Data-Corruption/dens.chat/internal/platform/http/den"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// The test binary is its own media worker, as ffmpeg.TestWorkerEnv says.
func TestMain(m *testing.M) {
	if os.Getenv(ffmpeg.TestWorkerEnv) == "1" {
		os.Exit(ffmpeg.Work(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// store is one install's database, vault and log, and its media module,
// which its den and client share.
type store struct {
	db      *sql.DB
	v       *vault.Vault
	log     *xlog.Logger
	storage den.Storage
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
	// The service sets the vault up at its first start.
	if err := vaultstore.Init(context.Background(), db, v.CheckValue()); err != nil {
		t.Fatal(err)
	}
	storage := den.Storage{Dir: filepath.Join(dir, "uploads"), Temp: filepath.Join(dir, "tmp"),
		Media: ffmpeg.TestRunner(nil)}
	for _, d := range []string{storage.Dir, storage.Temp} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return store{db, v, log, storage}
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
	d, err := den.Open(context.Background(), h.s.db, h.s.v, h.s.log, h.s.storage)
	if err != nil {
		h.t.Fatal(err)
	}
	d.TokenLifetime = lifetime
	d.PresenceDelay = 50 * time.Millisecond
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
	m, _ := h.clientFrom(t, newStore(t), own)
	return m
}

// clientFrom starts a Manager on an install's store, as the service does
// at every start, and returns a function that stops it.
func (h *denHost) clientFrom(t *testing.T, s store, own denclient.OwnDen) (*denclient.Manager, func()) {
	t.Helper()
	return h.clientVia(t, s, own, h.public.Listener.Addr().String())
}

// clientVia starts a Manager as clientFrom does, which reaches example.com
// at address.
func (h *denHost) clientVia(t *testing.T, s store, own denclient.OwnDen, address string) (*denclient.Manager, func()) {
	t.Helper()
	m := denclient.New(s.db, s.v, s.log, "dens-test", own)
	m.TempDir = t.TempDir()
	m.Media = s.storage.Media
	transport := h.public.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, "example.com:") {
			addr = address
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	m.HTTP = &http.Client{Timeout: 10 * time.Second, Transport: transport}
	m.Transfer = &http.Client{Transport: transport}
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
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return m, stop
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
		_, err = m.Join(context.Background(), invite, req)
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
		joined, err = status(owner.JoinOwn(context.Background(), code, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "correct horse"}))
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
	if _, err := member.Join(context.Background(), invite, denclient.JoinRequest{Username: "bob2", DisplayName: "B", Password: "correct horse"}); err != denclient.ErrAlreadyJoined {
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
		_, err = m.JoinOwn(context.Background(), code, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "correct horse"})
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
	if _, err := m.Preview(context.Background(), forged); !errors.Is(err, denproto.ErrWrongIdentity) {
		t.Fatalf("preview of a den with another identity: %v", err)
	}
}

// An https:// address in front of the plain den listener, which the owner
// can't notice (their own client uses loopback), is explained, caught by
// the address test, and fixed by changing the address.
func TestAddressProblems(t *testing.T) {
	h := startDen(t, newStore(t), time.Hour)
	wrong := strings.Replace(h.loopback.URL, "http:", "https:", 1)
	code, err := h.d.Create(context.Background(), "Misaddressed", wrong)
	if err != nil {
		t.Fatal(err)
	}
	owner := h.client(t, h.own())
	var joined denclient.Status
	for range 100 {
		joined, err = status(owner.JoinOwn(context.Background(), code, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "correct horse"}))
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, owner, "owner connected over loopback", connected)

	invite, _, err := owner.CreateInvite(context.Background(), joined.DenID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	member := h.client(t, noOwnDen)
	_, err = member.Preview(context.Background(), invite)
	if msg := denclient.Explain(err); !strings.Contains(msg, "answers plain HTTP") {
		t.Fatalf("preview through the wrong scheme explained as %q (%v)", msg, err)
	}
	if msg := denclient.Explain(owner.CheckAddress(context.Background(), joined.DenID)); !strings.Contains(msg, "answers plain HTTP") {
		t.Fatalf("the owner's address test explained %q", msg)
	}

	right := h.loopback.URL
	if err := owner.UpdateDen(context.Background(), joined.DenID, denproto.DenUpdateRequest{URL: &right}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, owner, "the new address", func(s denclient.Status) bool { return s.URL == right })
	if err := owner.CheckAddress(context.Background(), joined.DenID); err != nil {
		t.Fatalf("address test after the fix: %v", err)
	}
	invite, _, err = owner.CreateInvite(context.Background(), joined.DenID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	join(t, member, invite, "bob")
}

// chatDen starts a den with an owner (alice) and a member (bob), both
// connected, and a channel.
func chatDen(t *testing.T) (h *denHost, owner, member *denclient.Manager, denID, channelID string) {
	t.Helper()
	h = startDen(t, newStore(t), time.Hour)
	h.d.RelaxLimits() // the tests send faster than members may
	code, err := h.d.Create(context.Background(), "Chat Den", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	owner = h.client(t, h.own())
	var joined denclient.Status
	for range 100 {
		joined, err = status(owner.JoinOwn(context.Background(), code, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "correct horse"}))
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	denID = joined.DenID
	waitFor(t, owner, "owner connected", connected)
	invite, _, err := owner.CreateInvite(context.Background(), denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	member = h.client(t, noOwnDen)
	join(t, member, invite, "bob")
	name := "general"
	if err := owner.Manage(context.Background(), denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*denclient.Manager{owner, member} {
		deadline := time.Now().Add(5 * time.Second)
		for {
			v, err := m.View(denID)
			if err == nil && len(v.Channels) == 1 {
				channelID = v.Channels[0].ID
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the channel never reached a client: %+v %v", v, err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return h, owner, member, denID, channelID
}

func send(t *testing.T, m *denclient.Manager, denID, channelID, text string) denproto.Message {
	t.Helper()
	msg, err := m.Send(context.Background(), denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return msg.Message
}

// status is the status of a den just joined.
func status(j denclient.Joined, err error) (denclient.Status, error) { return j.Status, err }

// collect reads message.created events from a page stream until it has
// want distinct messages, failing on a duplicate.
func collect(t *testing.T, stream <-chan denclient.PageEvent, want int) []string {
	t.Helper()
	var ids []string
	seen := map[string]bool{}
	timeout := time.After(10 * time.Second)
	for len(ids) < want {
		select {
		case e, ok := <-stream:
			if !ok {
				t.Fatal("the page stream closed")
			}
			for _, ev := range e.Events {
				if ev.T != denproto.EventMessageCreated {
					continue
				}
				var m denproto.Message
				json.Unmarshal(ev.D, &m)
				if seen[m.ID] {
					t.Fatalf("message %s arrived twice", m.ID)
				}
				seen[m.ID] = true
				ids = append(ids, m.ID)
			}
		case <-timeout:
			t.Fatalf("got %d of %d messages: %v", len(ids), want, ids)
		}
	}
	return ids
}

// A member whose connection drops catches up on reconnect without gaps or
// duplicates: the den replays what they missed.
func TestChatCatchUp(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	stream, stop := member.Stream()
	defer stop()
	var sent []string
	for i := range 3 {
		sent = append(sent, send(t, owner, denID, channelID, fmt.Sprintf("before %d", i)).ID)
	}
	got := collect(t, stream, 3)

	view, _ := member.View(denID)
	bob, _ := denproto.ParseID(view.Me.ID)
	h.d.CloseMemberSockets(bob, denproto.CloseTooSlow, "too slow")
	for i := range 5 {
		sent = append(sent, send(t, owner, denID, channelID, fmt.Sprintf("while away %d", i)).ID)
	}
	got = append(got, collect(t, stream, 5)...)
	if strings.Join(got, ",") != strings.Join(sent, ",") {
		t.Fatalf("the member got %v, want %v", got, sent)
	}

	page, err := member.History(context.Background(), denID, channelID, denclient.HistoryQuery{Limit: 3})
	if err != nil || len(page.Messages) != 3 || page.Messages[2].ID != sent[7] || !page.HasOlder {
		t.Fatalf("history: %+v %v", page, err)
	}
	older, err := member.History(context.Background(), denID, channelID, denclient.HistoryQuery{Before: page.Messages[0].ID})
	if err != nil || len(older.Messages) != 5 || older.HasOlder {
		t.Fatalf("older: %+v %v", older, err)
	}
	if _, err := member.History(context.Background(), denID, "../den", denclient.HistoryQuery{}); err == nil {
		t.Fatal("a path in place of a channel ID was passed on")
	}
}

func TestChatEditsAndMentions(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	msg := send(t, member, denID, channelID, "first draft")
	edited, err := member.Edit(context.Background(), denID, channelID, msg.ID, denproto.EditRequest{Revision: 1, Text: "second draft"})
	if err != nil || edited.Revision != 2 {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	_, err = member.Edit(context.Background(), denID, channelID, msg.ID, denproto.EditRequest{Revision: 1, Text: "stale"})
	var conflict *denclient.ErrEditConflict
	if !errors.As(err, &conflict) || conflict.Current.Text != "second draft" {
		t.Fatalf("stale edit: %v", err)
	}

	reply, err := owner.Send(context.Background(), denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "a reply", ReplyTo: msg.ID})
	if err != nil || reply.Reply == nil || reply.Reply.Text != "second draft" {
		t.Fatalf("reply: %+v %v", reply, err)
	}
	page, err := member.History(context.Background(), denID, channelID, denclient.HistoryQuery{})
	if err != nil || len(page.Messages) != 2 || page.Messages[1].Reply == nil || page.Messages[1].Reply.AuthorID != msg.AuthorID {
		t.Fatalf("the reply's preview in history: %+v %v", page, err)
	}

	stream, stop := member.Stream()
	defer stop()
	mention := send(t, owner, denID, channelID, "hey @bob")
	// The page gets the counted read state with the event.
	timeout := time.After(5 * time.Second)
	for counted := false; !counted; {
		select {
		case e := <-stream:
			for _, r := range e.Reads {
				counted = counted || (r.ChannelID == channelID && r.MentionCount == 1 && r.LastMessage == mention.ID)
			}
		case <-timeout:
			t.Fatal("the page never got the mention's read state")
		}
	}
	readState := func() denproto.ReadState {
		v, _ := member.View(denID)
		for _, r := range v.ReadStates {
			if r.ChannelID == channelID {
				return r
			}
		}
		return denproto.ReadState{}
	}
	deadline := time.Now().Add(5 * time.Second)
	for readState().MentionCount != 1 || readState().LastMessage != mention.ID {
		if time.Now().After(deadline) {
			t.Fatalf("the mention wasn't counted: %+v", readState())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := member.MarkRead(context.Background(), denID, channelID, mention.ID); err != nil {
		t.Fatal(err)
	}
	for readState().MentionCount != 0 || readState().ReadPosition != mention.ID {
		if time.Now().After(deadline) {
			t.Fatalf("reading didn't clear the mention: %+v", readState())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = h
}

// A den restart resyncs: the page is told to reload, and history still
// pages from the den.
func TestChatResetAfterRestart(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	send(t, owner, denID, channelID, "before the restart")
	stream, stop := member.Stream()
	defer stop()
	h.restart()
	timeout := time.After(10 * time.Second)
	for reset := false; !reset; {
		select {
		case e := <-stream:
			reset = e.Reset
		case <-timeout:
			t.Fatal("no reset after the den restarted")
		}
	}
	page, err := member.History(context.Background(), denID, channelID, denclient.HistoryQuery{})
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("history after the restart: %+v %v", page, err)
	}
}
