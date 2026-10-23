package control

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
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
	return startServerOn(t, allowed, handlers, nil)
}

// startServerOn is startServer with the endpoint's listener wrapped, when
// wrap isn't nil.
func startServerOn(t *testing.T, allowed host.Identity, handlers map[string]Handler, wrap func(net.Listener) net.Listener) (Client, func()) {
	t.Helper()
	return startTestServer(t, &Server{Allowed: allowed, Handlers: handlers}, wrap)
}

// startTestServer serves server, given a logger of its own, on a new
// endpoint, its listener wrapped when wrap isn't nil.
func startTestServer(t *testing.T, server *Server, wrap func(net.Listener) net.Listener) (Client, func()) {
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
	if wrap != nil {
		ln = wrap(ln)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server.Log = logger
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
	if err := client.CallBody(Request{Op: OpBackup, Password: "hunter22"}, &result, func() (io.Writer, error) { return &body, nil }); err != nil {
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

// TestServerRefusesOtherAccountsBeforeReading checks that where the system
// names a connection's account as it connects, another account is refused
// before it has sent anything (M8).
func TestServerRefusesOtherAccountsBeforeReading(t *testing.T) {
	if !host.PeerBeforeRequest {
		t.Skip("a Windows pipe names its client only from what it read")
	}
	other := host.Identity{ID: "0", Name: "someone else"}
	if me, _ := host.CurrentUser(); me.ID == "0" {
		other.ID = "65534"
	}
	client, stop := startServer(t, other, map[string]Handler{})
	defer stop()
	conn, err := host.DialControl(client.Endpoint, client.Server, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "only answers") {
		t.Fatalf("a silent connection from another account got %q, %v", line, err)
	}
}

// failingOnce fails its first accept, as the Windows pipe did after a
// client opened it and closed it at once.
type failingOnce struct {
	net.Listener
	failed atomic.Bool
}

func (l *failingOnce) Accept() (net.Conn, error) {
	if !l.failed.Swap(true) {
		return nil, errors.New("the pipe is being closed")
	}
	return l.Listener.Accept()
}

// TestServerAcceptsAgainAfterAFailure checks that a failed accept doesn't
// end the server, which would end the service (M8).
func TestServerAcceptsAgainAfterAFailure(t *testing.T) {
	me, _ := host.CurrentUser()
	ln := &failingOnce{}
	client, stop := startServerOn(t, me, map[string]Handler{
		OpStatus: func(context.Context, Request) (Reply, error) { return Reply{Result: Status{Version: "v1"}}, nil },
	}, func(inner net.Listener) net.Listener {
		ln.Listener = inner
		return ln
	})
	defer stop()
	var status Status
	if err := client.Call(Request{Op: OpStatus}, &status); err != nil || status.Version != "v1" {
		t.Fatalf("after a failed accept: %+v %v", status, err)
	}
	if !ln.failed.Load() {
		t.Fatal("the listener never failed")
	}
}

// TestServerServesAtMostMaxConns checks that the server serves maxConns
// connections at once, and the next once one of them ends.
func TestServerServesAtMostMaxConns(t *testing.T) {
	me, _ := host.CurrentUser()
	entered := make(chan struct{}, maxConns+1)
	release := make(chan struct{})
	client, stop := startServer(t, me, map[string]Handler{
		"wait": func(context.Context, Request) (Reply, error) {
			entered <- struct{}{}
			<-release
			return Reply{}, nil
		},
	})
	defer stop()
	done := make(chan error, maxConns+1)
	for range maxConns {
		go func() { done <- client.Call(Request{Op: "wait"}, nil) }()
	}
	for range maxConns {
		<-entered
	}
	go func() { done <- client.Call(Request{Op: "wait"}, nil) }()
	select {
	case <-entered:
		t.Fatalf("served %d connections at once", maxConns+1)
	case <-time.After(500 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the next connection wasn't served once one ended")
	}
	close(release)
	for range maxConns + 1 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
