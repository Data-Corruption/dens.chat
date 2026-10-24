package service_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/install"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// forward stands in for Caddy and the router's forward: the den's public
// address, on loopback here, which reaches the den listener of whichever
// computer it's pointed at.
type forward struct {
	url    string
	target atomic.Value // host:port
}

func startForward(t *testing.T) *forward {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &forward{url: "http://" + ln.Addr().String()}
	f.target.Store("127.0.0.1:1")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(&url.URL{Scheme: "http", Host: f.target.Load().(string)})
			r.Out.Host = r.In.Host
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}
	srv := &http.Server{Handler: proxy}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return f
}

// to points the den's address at a den listener's port.
func (f *forward) to(port int) { f.target.Store(fmt.Sprintf("127.0.0.1:%d", port)) }

// computer is a development instance in a home of its own, standing in for
// a computer, which a restore into it stops and starts again.
type computer struct {
	t      *testing.T
	home   string
	opts   app.OpenOptions
	a      *app.App
	stop   func()
	client control.Client
}

func newComputer(t *testing.T, set func(*app.OpenOptions)) *computer {
	t.Helper()
	c := &computer{t: t, home: t.TempDir(), opts: app.OpenOptions{Instance: devInstance(), DevClientPort: freePort(t)}}
	if set != nil {
		set(&c.opts)
	}
	c.start()
	return c
}

// hosting has a computer host from the start, on ports of its own.
func hosting(t *testing.T) func(*app.OpenOptions) {
	return func(o *app.OpenOptions) {
		o.DevDenPort, o.DevMediaPorts = freePort(t), [2]int{freeUDPPort(t), freePort(t)}
	}
}

func (c *computer) start() {
	c.t.Helper()
	useHome(c.t, c.home)
	a, stop, err := serveDev(c.t, c.opts)
	if err != nil {
		c.t.Fatalf("start: %v", err)
	}
	c.t.Cleanup(stop)
	me, err := host.CurrentUser()
	if err != nil {
		c.t.Fatal(err)
	}
	c.a, c.stop = a, stop
	c.client = control.Client{Endpoint: a.Layout.ControlEndpoint, Server: host.ControlServer{Account: me}, Timeout: 30 * time.Second}
}

// restore restores an archive into the computer's instance, as dens restore
// does, and starts it again; it returns what the restore said.
func (c *computer) restore(archive string) string {
	c.t.Helper()
	c.stop()
	useHome(c.t, c.home)
	l, err := layout.New(devBuild.Name, c.opts.Instance, layout.Development)
	if err != nil {
		c.t.Fatal(err)
	}
	var said bytes.Buffer
	if err := install.RestoreSession(context.Background(), devBuild, l, archive, localPassword, func() error { return nil },
		install.Options{Instance: c.opts.Instance, Out: &said}); err != nil {
		c.t.Fatalf("restore: %v\n%s", err, said.String())
	}
	c.start()
	return said.String()
}

// refusedRestore restores an archive into the computer's instance, as dens
// restore does, which must refuse it, saying want, before it stops the
// service.
func (c *computer) refusedRestore(archive, want string) {
	c.t.Helper()
	useHome(c.t, c.home)
	l, err := layout.New(devBuild.Name, c.opts.Instance, layout.Development)
	if err != nil {
		c.t.Fatal(err)
	}
	err = install.RestoreSession(context.Background(), devBuild, l, archive, localPassword, func() error {
		return errors.New("the restore stopped the service")
	}, install.Options{Instance: c.opts.Instance, Out: io.Discard})
	if err == nil || !strings.Contains(err.Error(), want) {
		c.t.Fatalf("the restore: %v, want it refused with %q", err, want)
	}
}

// call makes a control request, waiting out the local password's limiter,
// which lets three checks through at once, then one every 2 seconds.
func (c *computer) call(req control.Request, out any) error {
	c.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := c.client.Call(req, out)
		if err == nil || !strings.Contains(err.Error(), app.ErrTooManyAttempts.Error()) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// export writes the den's export to path, as dens den export does.
func (c *computer) export(path, password string) error {
	c.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var archive bytes.Buffer
		err := c.client.CallBody(control.Request{Op: control.OpExport, Password: password}, &control.Backup{},
			func() (io.Writer, error) { return &archive, nil })
		if err == nil {
			return os.WriteFile(path, archive.Bytes(), 0o600)
		}
		if !strings.Contains(err.Error(), app.ErrTooManyAttempts.Error()) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// refused makes a control request that must fail saying want.
func (c *computer) refused(what string, req control.Request, want string) {
	c.t.Helper()
	err := c.call(req, nil)
	if err == nil || !strings.Contains(err.Error(), want) {
		c.t.Fatalf("%s: %v, want it refused with %q", what, err, want)
	}
}

func (c *computer) status() control.Status {
	c.t.Helper()
	var s control.Status
	if err := c.client.Call(control.Request{Op: control.OpStatus}, &s); err != nil {
		c.t.Fatal(err)
	}
	return s
}

// waitDen waits until the computer's only den is as cond says.
func waitDen(t *testing.T, m *denclient.Manager, what string, cond func(denclient.Status) bool) denclient.Status {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		list := m.Statuses()
		if len(list) == 1 && cond(list[0]) {
			return list[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", what, list)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// viewOf waits until a den's view on a computer is as cond says.
func viewOf(t *testing.T, m *denclient.Manager, denID, what string, cond func(denclient.View) bool) denclient.View {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		v, err := m.View(denID)
		if err == nil && cond(v) {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v %v", what, v, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// expectPassword makes a request that checks the local password, as expect
// does, waiting out the password's limiter.
func (b *browser) expectPassword(code int, what, method, path string, body any, out ...any) {
	b.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got, data := b.do(method, path, body)
		if got == http.StatusTooManyRequests && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if got != code {
			b.t.Fatalf("%s: %d %s, want %d", what, got, data, code)
		}
		if len(out) > 0 {
			if err := json.Unmarshal(data, out[0]); err != nil {
				b.t.Fatalf("%s: decode %s: %v", what, data, err)
			}
		}
		return
	}
}

// texts are a channel's messages, oldest first.
func texts(t *testing.T, m *denclient.Manager, denID, channel string) []string {
	t.Helper()
	page, err := m.History(context.Background(), denID, channel, denclient.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, msg := range page.Messages {
		out = append(out, msg.Text)
	}
	return out
}

func send(t *testing.T, m *denclient.Manager, denID, channel, text string, files ...string) {
	t.Helper()
	if _, err := m.Send(context.Background(), denID, channel, denproto.SendRequest{Nonce: denproto.Random(16), Text: text, Attachments: files}); err != nil {
		t.Fatal(err)
	}
}

// checkDM has two members compare check digits, so their DM takes
// messages: a starts its key, and each types what the other's screen shows.
func checkDM(t *testing.T, a, b *denclient.Manager, denID, dm string) {
	t.Helper()
	ctx := context.Background()
	for _, m := range []*denclient.Manager{a, b} {
		viewOf(t, m, denID, "the DM", func(v denclient.View) bool {
			return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == dm })
		})
	}
	if err := a.StartDMKey(ctx, denID, dm, false); err != nil {
		t.Fatal(err)
	}
	state := func(m *denclient.Manager, match func(denclient.DMCheck) bool) denclient.DMCheck {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			st, err := m.CheckState(ctx, denID, dm)
			if err == nil && match(st) {
				return st
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting on the DM's check: %+v %v", st, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	shown := func(st denclient.DMCheck) bool { return st.Half != "" }
	x, y := state(a, shown), state(b, shown)
	if err := a.CheckDM(ctx, denID, dm, y.Half); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckDM(ctx, denID, dm, x.Half); err != nil {
		t.Fatal(err)
	}
	both := func(st denclient.DMCheck) bool { return st.Checked && st.Partner }
	state(a, both)
	state(b, both)
}

// dump lists a table's rows as text, in order.
func dump(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%q", values))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

// files lists the names and contents of the files in dir.
func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(data)
	}
	return out
}

// A den moves from the computer it was made on to another (M10). The
// computer it's on keeps its copy until another serves the den: hosting
// off, an export since hosting was last on, and the den answering at its
// address as itself. The export holds the den and none of the owner's
// client; restored on a server, it's hosted from the command line, holds no
// account, and offers nothing to finish setting up. Its member reconnects
// by themselves, and once the computer it left removes its copy, the
// owner's Dens there reaches the den at its address as the owner, with its
// history and DMs, without a restart.
func TestDenMovesToAnotherComputer(t *testing.T) {
	ctx := context.Background()
	address := startForward(t)

	pc := newComputer(t, hosting(t))
	address.to(pc.opts.DevDenPort)
	owner := pair(t, pc.a, pc.client)
	owner.expect(http.StatusOK, "create the den", "POST", "/api/den", map[string]string{
		"name": "Moving Den", "url": address.url, "username": "alice", "display_name": "Alice", "password": "den password"})
	owner.waitOwnState("connected")
	alice := pc.a.Dens
	denID := alice.Statuses()[0].DenID
	invite, _, err := alice.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	friend := newComputer(t, nil)
	carol := friend.a.Dens
	for range 100 { // the den connections start just after the service does
		if _, err = carol.Join(ctx, invite, denclient.JoinRequest{Username: "carol", DisplayName: "Carol", Password: "den password"}); !errors.Is(err, denclient.ErrNotStarted) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	waitDen(t, carol, "the member connected", connected)

	name := "general"
	if err := alice.Manage(ctx, denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	var channel string
	for _, m := range []*denclient.Manager{alice, carol} {
		channel = viewOf(t, m, denID, "the channel", func(v denclient.View) bool { return len(v.Channels) == 1 }).Channels[0].ID
	}
	send(t, alice, denID, channel, "from the owner, before the move")
	notes := []byte("notes the den keeps")
	file, err := carol.Upload(ctx, denID, channel, "notes.txt", int64(len(notes)), bytes.NewReader(notes))
	if err != nil {
		t.Fatal(err)
	}
	send(t, carol, denID, channel, "from the member, before the move", file.ID)
	carolID := viewOf(t, carol, denID, "the member's view", func(denclient.View) bool { return true }).Me.ID
	dm, err := alice.OpenDM(ctx, denID, carolID)
	if err != nil {
		t.Fatal(err)
	}
	checkDM(t, alice, carol, denID, dm.ID)
	send(t, alice, denID, dm.ID, "a private word, before the move")
	before := []string{"from the owner, before the move", "from the member, before the move"}

	// Hosting on: the den's computer still serves it, and nothing that
	// reaches it after an export's snapshot would be in it.
	pc.refused("removing with hosting on", control.Request{Op: control.OpRemoveDen, Password: localPassword}, "hosting is on")
	if err := pc.export(filepath.Join(t.TempDir(), "x.backup"), localPassword); err == nil || !strings.Contains(err.Error(), "hosting off") {
		t.Fatalf("an export with hosting on: %v", err)
	}
	if err := pc.call(control.Request{Op: control.OpHosting, On: false}, nil); err != nil {
		t.Fatal(err)
	}
	owner.waitOwnState("offline")
	pc.refused("removing with no export", control.Request{Op: control.OpRemoveDen, Password: localPassword}, "hasn't been exported")
	if err := pc.export(filepath.Join(t.TempDir(), "x.backup"), "wrong wrong wrong"); err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Fatalf("an export with the wrong password: %v", err)
	}

	// An export is recorded, until hosting goes on again.
	if err := pc.export(filepath.Join(t.TempDir(), "first.backup"), localPassword); err != nil {
		t.Fatal(err)
	}
	if s := pc.status(); s.DenExported.IsZero() || s.Den != "Moving Den" {
		t.Fatalf("the status after an export: %+v", s)
	}
	if err := pc.call(control.Request{Op: control.OpHosting, On: true, Password: localPassword}, nil); err != nil {
		t.Fatal(err)
	}
	if s := pc.status(); !s.DenExported.IsZero() {
		t.Fatalf("turning hosting on kept the export: %+v", s)
	}

	// The page's Move turns hosting off, and downloads the export.
	var link struct {
		URL string `json:"url"`
	}
	owner.expectPassword(http.StatusOK, "move the den", "POST", "/api/den/export", map[string]string{"password": localPassword}, &link)
	if h := owner.hosting(); h.Enabled || h.Serving {
		t.Fatalf("moving the den left hosting %+v", h)
	}
	resp, err := owner.http.Get(owner.base + link.URL)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), "-den-") {
		t.Fatalf("the export's download: %d %v, %v", resp.StatusCode, resp.Header, err)
	}
	exported := filepath.Join(t.TempDir(), "den.backup")
	if err := os.WriteFile(exported, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if h := owner.hosting(); h.Exported == 0 || h.Den != "Moving Den" || h.URL != address.url {
		t.Fatalf("hosting after the export: %+v", h)
	}
	if v := owner.dens(); v.Hosting.Exported == 0 || !v.Hosting.Owned {
		t.Fatalf("the dens view after the export: %+v", v.Hosting)
	}

	// It holds the den as the instance has it, and none of the client.
	out := t.TempDir()
	manifest, err := backup.Extract(bytes.NewReader(archive), out, backup.ExtractOptions{})
	if err != nil || !manifest.DenExport || manifest.Den != "Moving Den" {
		t.Fatalf("the export: %+v, %v", manifest, err)
	}
	snapshot, err := database.OpenReadOnly(backup.DatabasePath(out))
	if err != nil {
		t.Fatal(err)
	}
	tables, err := database.Tables(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		switch copied := dump(t, snapshot, table); {
		case database.IsDenTable(table):
			if live := dump(t, pc.a.DB, table); live != copied {
				t.Errorf("the export's %s differs from the instance's:\n%s\n%s", table, copied, live)
			}
		case table == "vault" || table == "config":
		case copied != "":
			t.Errorf("the export's %s holds\n%s", table, copied)
		}
	}
	var seal []byte
	if err := snapshot.QueryRow(`SELECT dm_seal FROM vault`).Scan(&seal); err != nil || seal != nil {
		t.Errorf("the export keeps the vault's DM seal: %v", err)
	}
	snapshot.Close()
	uploads := files(t, pc.a.Layout.Uploads)
	if got := files(t, backup.UploadsPath(out)); len(uploads) == 0 || !maps.Equal(got, uploads) {
		t.Errorf("the export's uploads are %d files, and the instance's %d", len(got), len(uploads))
	}

	// The den's address leads nowhere yet, and then to a den of another's.
	pc.refused("removing while the den doesn't answer", control.Request{Op: control.OpRemoveDen, Password: localPassword}, "doesn't answer at "+address.url)
	other := newComputer(t, hosting(t))
	otherPage := pair(t, other.a, other.client)
	otherPage.expect(http.StatusOK, "create another den", "POST", "/api/den", map[string]string{
		"name": "Other Den", "url": address.url, "username": "dave", "display_name": "Dave", "password": "den password"})
	address.to(other.opts.DevDenPort)
	pc.refused("removing while another den answers", control.Request{Op: control.OpRemoveDen, Password: localPassword}, "can't prove it is this den")

	// A computer whose Dens is in use doesn't take the export, which would
	// replace its den and its account there with the export's den alone.
	other.refusedRestore(exported, "instance "+other.opts.Instance+" hosts the den Other Den and has an account in 1 den")

	// The server restores the export, which holds no account and nothing
	// to set up, and hosts it from the command line.
	server := newComputer(t, hosting(t))
	address.to(server.opts.DevDenPort)
	said := server.restore(exported)
	for _, want := range []string{"The export of the den Moving Den", "only holds the den", "den hosting on"} {
		if !strings.Contains(said, want) {
			t.Errorf("the restore said:\n%s\nwhich doesn't say %q", said, want)
		}
	}
	serverPage := pairOnly(t, server.a, server.client)
	if v := serverPage.dens(); len(v.Dens) != 0 || v.Hosting.Enabled || !v.Hosting.Created || !v.Hosting.Owned {
		t.Fatalf("the server's dens view: %+v", v)
	}
	var h control.Hosting
	if err := server.call(control.Request{Op: control.OpHosting, On: true, Password: localPassword}, &h); err != nil {
		t.Fatal(err)
	}
	if !h.On || !h.Serving || h.Den != "Moving Den" || h.URL != address.url || h.DenPort != server.opts.DevDenPort {
		t.Fatalf("hosting on the server: %+v", h)
	}
	waitDen(t, carol, "the member back", connected)
	if got := texts(t, carol, denID, channel); !slices.Equal(got, before) {
		t.Fatalf("the member's history on the server: %q", got)
	}
	if len(server.a.Dens.Statuses()) != 0 {
		t.Fatal("the server joined a den")
	}

	// The computer the den left removes its copy, and reaches it at its
	// address, as the owner.
	pc.refused("removing with the wrong password", control.Request{Op: control.OpRemoveDen, Password: "wrong wrong wrong"}, "wrong password")
	if _, ok := pc.a.Den.Info(); !ok {
		t.Fatal("a wrong password removed the den")
	}
	var removed control.RemovedDen
	if err := pc.call(control.Request{Op: control.OpRemoveDen, Password: localPassword}, &removed); err != nil {
		t.Fatal(err)
	}
	if removed.Den != "Moving Den" || removed.URL != address.url {
		t.Fatalf("removed %+v", removed)
	}
	if _, _, ok := pc.a.OwnDen(); ok {
		t.Fatal("the computer still has its own den")
	}
	for _, table := range tables {
		if database.IsDenTable(table) {
			if rows := dump(t, pc.a.DB, table); rows != "" {
				t.Errorf("the computer's %s keeps\n%s", table, rows)
			}
		}
	}
	if left := files(t, pc.a.Layout.Uploads); len(left) != 0 {
		t.Errorf("the computer keeps %d uploads", len(left))
	}
	if h := owner.hosting(); h.Exported != 0 || h.Den != "" {
		t.Errorf("hosting after the removal: %+v", h)
	}
	st := waitDen(t, alice, "the owner at the den's address", func(s denclient.Status) bool {
		return connected(s) && !s.Own
	})
	if st.Role != denproto.RoleOwner || st.URL != address.url || st.DenID != denID {
		t.Fatalf("the owner's den now: %+v", st)
	}
	if got := texts(t, alice, denID, channel); !slices.Equal(got, before) {
		t.Fatalf("the owner's history at the den's address: %q", got)
	}
	if got := texts(t, alice, denID, dm.ID); !slices.Equal(got, []string{"a private word, before the move"}) {
		t.Fatalf("the owner's DM at the den's address: %q", got)
	}
	send(t, alice, denID, channel, "from the owner, after the move")
	deadline := time.Now().Add(10 * time.Second)
	for got := texts(t, carol, denID, channel); !slices.Contains(got, "from the owner, after the move"); got = texts(t, carol, denID, channel) {
		if time.Now().After(deadline) {
			t.Fatalf("the member's history after the move: %q", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func connected(s denclient.Status) bool { return s.State == denclient.StateConnected }

// An export that fails partway, as when its download stops, holds nothing
// another computer could serve, so the record of it goes with it (M10).
func TestAFailedExportIsForgotten(t *testing.T) {
	pc := newComputer(t, hosting(t))
	owner := pair(t, pc.a, pc.client)
	owner.expect(http.StatusOK, "create the den", "POST", "/api/den", map[string]string{
		"name": "Den", "url": "https://den.test", "username": "alice", "display_name": "Alice", "password": "den password"})
	if err := pc.call(control.Request{Op: control.OpHosting, On: false}, nil); err != nil {
		t.Fatal(err)
	}
	archive, err := pc.a.ExportDen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pc.status().DenExported.IsZero() {
		t.Fatal("the export wasn't recorded as it started")
	}
	if _, err := io.ReadFull(archive, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	archive.Close()
	deadline := time.Now().Add(10 * time.Second)
	for !pc.status().DenExported.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("the failed export stayed recorded")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
