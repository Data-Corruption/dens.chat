package control

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func testEndpoint(t *testing.T) string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\dens-control-test-` + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	}
	return filepath.Join(t.TempDir(), "control.sock")
}

func startServer(t *testing.T, allowed host.Identity, handlers map[string]Handler) (Client, func()) {
	t.Helper()
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	logger, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := testEndpoint(t)
	ln, err := host.ListenControl(endpoint, me)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{Allowed: allowed, Handlers: handlers, Log: logger}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln) }()
	stop := func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
		logger.Close()
	}
	return Client{Endpoint: endpoint, Server: host.ControlServer{Account: me}, Timeout: 5 * time.Second}, stop
}

func TestCallRoundTrip(t *testing.T) {
	me, _ := host.CurrentUser()
	client, stop := startServer(t, me, map[string]Handler{
		OpStatus: func(_ context.Context, req Request) (Reply, error) {
			return Reply{Result: Status{Version: "v1.2.3", Instance: "main"}}, nil
		},
		OpBackup: func(_ context.Context, req Request) (Reply, error) {
			if req.Password != "hunter22" {
				return Reply{}, Errorf("wrong password")
			}
			return Reply{Result: map[string]string{"name": "backup.tar.gz"}, Body: io.NopCloser(strings.NewReader("archive bytes"))}, nil
		},
		"broken": func(context.Context, Request) (Reply, error) {
			return Reply{}, errors.New("secret internal detail")
		},
	})
	defer stop()

	var status Status
	if err := client.Call(Request{Op: OpStatus}, &status); err != nil {
		t.Fatal(err)
	}
	if status.Version != "v1.2.3" || status.Instance != "main" {
		t.Fatalf("status = %+v", status)
	}

	var body bytes.Buffer
	var result map[string]string
	if err := client.CallBody(Request{Op: OpBackup, Password: "hunter22"}, &result, &body); err != nil {
		t.Fatal(err)
	}
	if body.String() != "archive bytes" || result["name"] != "backup.tar.gz" {
		t.Fatalf("body %q, result %v", body.String(), result)
	}

	err := client.Call(Request{Op: OpBackup, Password: "nope"}, nil)
	var userErr *UserError
	if !errors.As(err, &userErr) || userErr.Msg != "wrong password" {
		t.Fatalf("user error = %v", err)
	}
	err = client.Call(Request{Op: "broken"}, nil)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("internal error leaked or missing: %v", err)
	}
	if err := client.Call(Request{Op: "nope"}, nil); err == nil {
		t.Fatal("unknown operation answered")
	}
}

func TestServerRefusesOtherAccounts(t *testing.T) {
	other := host.Identity{ID: "S-1-5-19", Name: "someone else"}
	if runtime.GOOS != "windows" {
		other.ID = "0"
		if me, _ := host.CurrentUser(); me.ID == "0" {
			other.ID = "65534"
		}
	}
	called := false
	client, stop := startServer(t, other, map[string]Handler{
		OpStatus: func(context.Context, Request) (Reply, error) {
			called = true
			return Reply{}, nil
		},
	})
	defer stop()
	err := client.Call(Request{Op: OpStatus}, nil)
	if err == nil || !strings.Contains(err.Error(), "only answers") {
		t.Fatalf("call from another account = %v", err)
	}
	if called {
		t.Fatal("handler ran for another account")
	}
}
