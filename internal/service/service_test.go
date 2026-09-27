package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/service"
	"github.com/Data-Corruption/dens.chat/internal/ui"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startDevService opens and runs a development instance in temporary
// directories and returns a control client for it.
func startDevService(t *testing.T) (*app.App, control.Client) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("LOCALAPPDATA", dir)

	bi := build.BuildInfo{Name: "dens", Version: "v1.0.0", DefaultLogLevel: "warn", DevMode: true}
	a := app.New(bi)
	instance := fmt.Sprintf("t%d", time.Now().UnixNano()%1_000_000)
	if err := a.Open(app.OpenOptions{Instance: instance, DevClientPort: freePort(t)}); err != nil {
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
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("service.Run: %v", err)
		}
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("service stopped before ready: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("service did not become ready")
	}

	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	client := control.Client{Endpoint: a.Layout.ControlEndpoint, Server: host.ControlServer{Account: me}, Timeout: 10 * time.Second}
	return a, client
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
	token := strings.TrimPrefix(pair.URL[strings.Index(pair.URL, "#"):], "#token=")
	if !strings.HasPrefix(pair.URL, base+"/#token=") || token == "" {
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

	resp, err = browser.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), "Dens is running") {
		t.Fatalf("home page after setup:\n%s", page)
	}

	if err := client.CallBody(control.Request{Op: control.OpBackup, Password: "nope nope nope"}, nil, io.Discard); err == nil {
		t.Fatal("backup with the wrong password succeeded")
	}
	// Wait out the password limiter from the wrong attempts above.
	time.Sleep(4 * time.Second)
	var archive bytes.Buffer
	var result service.BackupResult
	if err := client.CallBody(control.Request{Op: control.OpBackup, Password: "correct horse battery"}, &result, &archive); err != nil {
		t.Fatalf("backup: %v", err)
	}
	manifest, err := backup.Extract(&archive, t.TempDir(), backup.DefaultLimits)
	if err != nil {
		t.Fatalf("extract backup: %v", err)
	}
	if manifest.Version != "v1.0.0" || manifest.Instance != a.Layout.Instance || !strings.HasSuffix(result.Name, ".backup") {
		t.Fatalf("manifest %+v, result %+v", manifest, result)
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
