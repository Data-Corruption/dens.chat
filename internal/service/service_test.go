package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/service"
	"github.com/Data-Corruption/dens.chat/internal/ui"

	"github.com/coder/websocket"
)

// The ports tests give an instance come from below those the system hands
// out by itself, from 32768 on Linux and 49152 on Windows. A port asked for
// with :0 comes from there, and once its listener closes, the system can
// hand it to another test's connection, as a listener of another package
// running at the same time does: a den whose hosting turns off and on
// again, or that restarts, would then find its port taken.
const testPortsFrom, testPortsSpan = 20000, 12000

// freePort finds a TCP port free on loopback.
func freePort(t *testing.T) int {
	t.Helper()
	return findPort(t, func(port int) error {
		ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			ln.Close()
		}
		return err
	})
}

// findPort tries ports among the tests' until one is free.
func findPort(t *testing.T, try func(port int) error) int {
	t.Helper()
	for range 200 {
		if port := testPortsFrom + rand.IntN(testPortsSpan); try(port) == nil {
			return port
		}
	}
	t.Fatal("found no free port")
	return 0
}

// startDevService opens and runs a development instance in temporary
// directories and returns a control client for it. The port freePort finds
// can go to a test of another package, running at the same time, before
// the service binds it, so a start that fails goes again on another port.
func startDevService(t *testing.T) (*app.App, control.Client) {
	t.Helper()
	return startDevServiceWith(t, nil)
}

// startDevServiceWith is startDevService with set changing the options of
// each start.
func startDevServiceWith(t *testing.T, set func(*app.OpenOptions)) (*app.App, control.Client) {
	t.Helper()
	a, err := runDevService(t, set)
	if err != nil {
		t.Logf("starting again on another port: %v", err)
		if a, err = runDevService(t, set); err != nil {
			t.Fatalf("service stopped before ready: %v", err)
		}
	}
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	client := control.Client{Endpoint: a.Layout.ControlEndpoint, Server: host.ControlServer{Account: me}, Timeout: 10 * time.Second}
	return a, client
}

// devHome points a development instance's directories into the test's own.
func devHome(t *testing.T) {
	t.Helper()
	useHome(t, t.TempDir())
}

// useHome points a development instance's directories into dir.
func useHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("LOCALAPPDATA", dir)
}

var devBuild = build.BuildInfo{Name: "dens", Version: "v1.0.0", DefaultLogLevel: "warn", DevMode: true}

func devInstance() string { return fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000) }

// runDevService opens a development instance in temporary directories and
// runs it until the test ends. It returns why the service stopped, if it
// stopped before it was ready, having closed what it opened.
func runDevService(t *testing.T, set func(*app.OpenOptions)) (*app.App, error) {
	t.Helper()
	devHome(t)
	opts := app.OpenOptions{Instance: devInstance(), DevClientPort: freePort(t)}
	if set != nil {
		set(&opts)
	}
	a, stop, err := serveDev(t, opts)
	if err != nil {
		return nil, err
	}
	t.Cleanup(stop)
	return a, nil
}

// serveDev opens a development instance with opts, in the directories the
// environment names, and runs it until stop is called. It returns why the
// service stopped, if it stopped before it was ready, having closed what
// it opened.
func serveDev(t *testing.T, opts app.OpenOptions) (a *app.App, stop func(), err error) {
	t.Helper()
	a = app.New(devBuild)
	if err := a.Open(opts); err != nil {
		t.Fatalf("Open: %v", err)
	}
	pages, err := ui.New()
	if err != nil {
		t.Fatal(err)
	}
	a.UI = pages

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx, a, func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-done:
		cancel()
		_ = a.Close()
		return nil, nil, err
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("service did not become ready")
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			if err := <-done; err != nil {
				t.Errorf("service.Run: %v", err)
			}
			if err := a.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		})
	}
	return a, stop, nil
}

func TestPairSetPasswordAndBackUp(t *testing.T) {
	a, client := startDevService(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", a.Instance.ClientPort)

	var status control.Status
	if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil {
		t.Fatal(err)
	}
	if status.Version != "v1.0.0" || status.PasswordSet {
		t.Fatalf("status = %+v", status)
	}

	var pair control.Pair
	if err := client.Call(control.Request{Op: control.OpPair}, &pair); err != nil {
		t.Fatal(err)
	}
	// The browser opens the page at localhost (M4.1); scripts, like this
	// test, reach the same listener at 127.0.0.1.
	token := strings.TrimPrefix(pair.URL[strings.Index(pair.URL, "#"):], "#token=")
	if !strings.HasPrefix(pair.URL, fmt.Sprintf("http://localhost:%d/#token=", a.Instance.ClientPort)) || token == "" {
		t.Fatalf("pairing URL %q", pair.URL)
	}

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	post := func(path string, body any) *http.Response {
		t.Helper()
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(data))
		req.Header.Set("Origin", base)
		req.Header.Set("Content-Type", "application/json")
		resp, err := browser.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	expect := func(resp *http.Response, code int, what string) {
		t.Helper()
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != code {
			t.Fatalf("%s: %d %s, want %d", what, resp.StatusCode, body, code)
		}
	}

	expect(post("/api/status", nil), http.StatusMethodNotAllowed, "POST status")
	resp, err := browser.Get(base + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	expect(resp, http.StatusUnauthorized, "status before pairing")
	expect(post("/api/pair", map[string]string{"token": token}), http.StatusOK, "pair")
	expect(post("/api/pair", map[string]string{"token": token}), http.StatusForbidden, "pair again with the same token")
	expect(post("/api/password", map[string]string{"password": "short"}), http.StatusBadRequest, "short password")
	expect(post("/api/password", map[string]string{"password": "correct horse battery"}), http.StatusOK, "set password")
	expect(post("/api/password", map[string]string{"password": "correct horse battery"}), http.StatusConflict, "set password twice")
	expect(post("/api/password/change", map[string]string{"current": "wrong wrong wrong", "next": "new horse battery"}), http.StatusForbidden, "change with wrong password")

	for path, want := range map[string]string{"/api/status": `"passwordSet":true`, "/api/dens": `"dens":[]`, "/": `<div id="app"`} {
		resp, err = browser.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Fatalf("%s after setup: %d %s", path, resp.StatusCode, body)
		}
	}

	discard := func() (io.Writer, error) { return io.Discard, nil }
	if err := client.CallBody(control.Request{Op: control.OpBackup, Password: "nope nope nope"}, nil, discard); err == nil {
		t.Fatal("backup with the wrong password succeeded")
	}
	// Wait out the password limiter from the wrong attempts above.
	time.Sleep(4 * time.Second)
	var archive bytes.Buffer
	var result control.Backup
	if err := client.CallBody(control.Request{Op: control.OpBackup, Password: "correct horse battery"}, &result, func() (io.Writer, error) { return &archive, nil }); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if result.Size < int64(archive.Len()) {
		t.Fatalf("the backup said it takes %d bytes, and took %d", result.Size, archive.Len())
	}
	manifest, err := backup.Extract(&archive, t.TempDir(), backup.ExtractOptions{})
	if err != nil {
		t.Fatalf("extract backup: %v", err)
	}
	if manifest.Version != "v1.0.0" || manifest.Instance != a.Layout.Instance || !strings.HasSuffix(result.Name, ".backup") {
		t.Fatalf("manifest %+v, result %+v", manifest, result)
	}
}

// A browser that loads the page at 127.0.0.1 is sent to localhost, where
// YouTube's player plays (M4.1), with the path and query it asked for. A
// script gets the page where it asked, and so does a browser already at
// localhost.
func TestBrowsersLoadThePageAtLocalhost(t *testing.T) {
	a, _ := startDevService(t)
	port := a.Instance.ClientPort
	noFollow := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	get := func(host, path string, document bool) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
		req.Host = host
		if document {
			req.Header.Set("Sec-Fetch-Dest", "document")
		}
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	for _, c := range []struct {
		host, path string
		document   bool
		code       int
		location   string
	}{
		{fmt.Sprintf("127.0.0.1:%d", port), "/den/abc?x=1", true, http.StatusTemporaryRedirect, fmt.Sprintf("http://localhost:%d/den/abc?x=1", port)},
		{fmt.Sprintf("[::1]:%d", port), "/", true, http.StatusTemporaryRedirect, fmt.Sprintf("http://localhost:%d/", port)},
		{fmt.Sprintf("127.0.0.1:%d", port), "/den//evil.example/", true, http.StatusTemporaryRedirect, fmt.Sprintf("http://localhost:%d/den//evil.example/", port)},
		{fmt.Sprintf("127.0.0.1:%d", port), "//evil.example/", true, http.StatusNotFound, ""},
		{fmt.Sprintf("localhost:%d", port), "/", true, http.StatusOK, ""},
		{fmt.Sprintf("LocalHost:%d", port), "/settings", true, http.StatusOK, ""},
		{fmt.Sprintf("127.0.0.1:%d", port), "/", false, http.StatusOK, ""},
		{fmt.Sprintf("127.0.0.1:%d", port), "/api/status", true, http.StatusUnauthorized, ""},
	} {
		resp := get(c.host, c.path, c.document)
		if resp.StatusCode != c.code || resp.Header.Get("Location") != c.location {
			t.Errorf("%s%s (document %v): %d to %q, want %d to %q", c.host, c.path, c.document, resp.StatusCode, resp.Header.Get("Location"), c.code, c.location)
		}
	}
}

func TestClientListenerRejectsForeignHosts(t *testing.T) {
	a, _ := startDevService(t)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/", a.Instance.ClientPort), nil)
	req.Host = "attacker.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("foreign Host got %d", resp.StatusCode)
	}
}

// TestStartEmptiesTemp checks that the service empties the data directory's
// tmp as it starts, so nothing a crash left there outlives it (M8).
func TestStartEmptiesTemp(t *testing.T) {
	devHome(t)
	opts := app.OpenOptions{Instance: devInstance(), DevClientPort: freePort(t)}
	a := app.New(devBuild)
	if err := a.Open(opts); err != nil {
		t.Fatal(err)
	}
	temp := a.Layout.Temp
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"upload-123", "dens-scratch-456", filepath.Join("backup-789", "db.sqlite")} {
		path := filepath.Join(temp, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("left by a crash"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a = app.New(devBuild)
	if err := a.Open(opts); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if entries, err := os.ReadDir(temp); err != nil || len(entries) != 0 {
		t.Fatalf("tmp after a start: %v %v", entries, err)
	}
}

// A session form's service stops when asked over the control endpoint,
// having no service manager to stop it, after answering (M9.3).
func TestSessionServiceQuitsWhenAsked(t *testing.T) {
	devHome(t)
	a := app.New(devBuild)
	if err := a.Open(app.OpenOptions{Instance: devInstance(), DevClientPort: freePort(t)}); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	pages, err := ui.New()
	if err != nil {
		t.Fatal(err)
	}
	a.UI = pages
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- service.Run(context.Background(), a, func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("service stopped before ready: %v", err)
	}
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	client := control.Client{Endpoint: a.Layout.ControlEndpoint, Server: host.ControlServer{Account: me}, Timeout: 10 * time.Second}
	if err := client.Call(control.Request{Op: control.OpQuit}, nil); err != nil {
		t.Fatalf("quit: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the service stopped with %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the service didn't stop when asked")
	}
}

// The page downloads a backup as dens backup writes it, after the local
// password, from a link that works once and soon goes (M9.3).
func TestPageDownloadsABackup(t *testing.T) {
	a, client := startDevService(t)
	b := pair(t, a, client)
	b.expect(http.StatusForbidden, "a backup with a wrong password", "POST", "/api/backup", map[string]string{"password": "wrong wrong wrong"})
	var size struct {
		Size int64 `json:"size"`
	}
	b.expect(http.StatusOK, "a backup's size", "GET", "/api/backup", nil, &size)
	var link struct {
		URL string `json:"url"`
	}
	b.expect(http.StatusOK, "a backup", "POST", "/api/backup", map[string]string{"password": localPassword}, &link)
	req, _ := http.NewRequest("GET", b.base+link.URL, nil)
	resp, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("the download: %d %v, %v", resp.StatusCode, resp.Header, err)
	}
	if size.Size < int64(len(archive)) {
		t.Fatalf("the page said the backup takes %d bytes, and it took %d", size.Size, len(archive))
	}
	dir := t.TempDir()
	manifest, err := backup.Extract(bytes.NewReader(archive), dir, backup.ExtractOptions{})
	if err != nil || manifest.Instance != a.Layout.Instance {
		t.Fatalf("the archive: %+v, %v", manifest, err)
	}
	b.expect(http.StatusGone, "the link again", "GET", link.URL, nil)
}

// TestPageDownloadsTheLogs checks the logs' download from the settings
// (M11): the newest logs, oldest first, as a file the browser keeps no
// copy of, for a paired browser alone; and a page's stream closing goes in
// them, with why.
func TestPageDownloadsTheLogs(t *testing.T) {
	a, client := startDevService(t)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/logs", a.Instance.ClientPort))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an unpaired browser's download: %d", resp.StatusCode)
	}
	b := pair(t, a, client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(b.base, "http")+"/api/events",
		&websocket.DialOptions{HTTPClient: b.http, HTTPHeader: http.Header{"Origin": {b.base}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	ws.Close(websocket.StatusGoingAway, "")
	download := func() (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", b.base+"/api/logs", nil)
		resp, err := b.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, string(body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, logs := download()
		h := resp.Header
		if resp.StatusCode != http.StatusOK || h.Get("Content-Type") != "text/plain; charset=utf-8" || h.Get("Cache-Control") != "no-store" ||
			!strings.HasPrefix(h.Get("Content-Disposition"), `attachment; filename=dens-`+a.Layout.Instance+"-") {
			t.Fatalf("the download: %d %v", resp.StatusCode, h)
		}
		start, closed := strings.Index(logs, "Starting dens v1.0.0"), strings.Index(logs, "stream closed after 0s: the page went away")
		if start >= 0 && closed > start {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the logs don't start the service and then close the page's stream:\n%s", logs)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestLogsOverTheControlEndpoint checks what dens logs asks of the service
// (M11): the newest logs, as the page downloads them, and the level kept as
// the page keeps it, which the settings then show.
func TestLogsOverTheControlEndpoint(t *testing.T) {
	a, client := startDevService(t)
	if err := client.Call(control.Request{Op: control.OpLogs, Level: "loud"}, nil); err == nil || !strings.Contains(err.Error(), "one of debug, info, warn, error, none") {
		t.Fatalf("a level that isn't one: %v", err)
	}
	var kept control.LogLevel
	if err := client.Call(control.Request{Op: control.OpLogs, Level: " INFO "}, &kept); err != nil || kept.Level != "info" {
		t.Fatalf("setting info: %+v, %v", kept, err)
	}
	b := pair(t, a, client)
	var settings struct {
		LogLevel string `json:"logLevel"`
	}
	b.expect(http.StatusOK, "the settings", "GET", "/api/settings", nil, &settings)
	if settings.LogLevel != "info" {
		t.Fatalf("the settings show %q", settings.LogLevel)
	}
	var result control.Backup
	var logs bytes.Buffer
	if err := client.CallBody(control.Request{Op: control.OpLogs}, &result, func() (io.Writer, error) { return &logs, nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Name, "dens-"+a.Layout.Instance+"-") || !strings.HasSuffix(result.Name, "-logs.txt") || result.Size != int64(logs.Len()) ||
		!strings.Contains(logs.String(), "Starting dens v1.0.0") {
		t.Fatalf("the logs: %+v, %d bytes", result, logs.Len())
	}
}
