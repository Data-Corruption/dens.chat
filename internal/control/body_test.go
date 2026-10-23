package control

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// failingReader fails every read, as a backup's archive does when a file
// it reads goes wrong.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read an upload: input/output error")
}

// slowReader gives n chunks, waiting before each.
type slowReader struct {
	n     int
	pause time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.n == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.pause)
	s.n--
	return copy(p, "chunk;"), nil
}

func bodyHandler(body func() io.Reader) map[string]Handler {
	return map[string]Handler{
		OpBackup: func(context.Context, Request) (Reply, error) {
			return Reply{Result: Backup{Name: "test.backup", Size: 1}, Body: io.NopCloser(body())}, nil
		},
	}
}

func receive(client Client, w io.Writer) error {
	return client.CallBody(Request{Op: OpBackup}, nil, func() (io.Writer, error) { return w, nil })
}

// A body that fails partway ends without its end frame, so the call fails
// rather than handing back the part that came as if it were whole.
func TestBodyCutShortFails(t *testing.T) {
	me, _ := host.CurrentUser()
	client, stop := startServer(t, me, bodyHandler(func() io.Reader {
		return io.MultiReader(strings.NewReader("the first half"), failingReader{})
	}))
	defer stop()
	var body bytes.Buffer
	if err := receive(client, &body); !errors.Is(err, ErrBodyCut) {
		t.Fatalf("a body cut short: %v, with %q", err, body.String())
	}
}

// Each write has its own deadline, so a body that takes longer than one,
// in all, still streams to its end.
func TestLongBodyStreamsToTheEnd(t *testing.T) {
	me, _ := host.CurrentUser()
	client, stop := startTestServer(t, &Server{Allowed: me, WriteTimeout: 200 * time.Millisecond, Handlers: bodyHandler(func() io.Reader {
		return &slowReader{n: 12, pause: 50 * time.Millisecond}
	})}, nil)
	defer stop()
	var body bytes.Buffer
	if err := receive(client, &body); err != nil {
		t.Fatalf("a body longer than one write's deadline: %v", err)
	}
	if body.String() != strings.Repeat("chunk;", 12) {
		t.Fatalf("body = %q", body.String())
	}
}

// A client that stops reading is let go once a write waits past its
// deadline, and when it reads again it learns the body didn't end.
func TestStalledReaderIsLetGo(t *testing.T) {
	me, _ := host.CurrentUser()
	client, stop := startTestServer(t, &Server{Allowed: me, WriteTimeout: 100 * time.Millisecond, Handlers: bodyHandler(func() io.Reader {
		// Far more than the connection's buffers hold.
		return bytes.NewReader(make([]byte, 64<<20))
	})}, nil)
	defer stop()
	err := client.CallBody(Request{Op: OpBackup}, nil, func() (io.Writer, error) {
		time.Sleep(time.Second)
		return io.Discard, nil
	})
	if !errors.Is(err, ErrBodyCut) {
		t.Fatalf("a body whose reader stalled: %v", err)
	}
}

// open's error stops the call before the body is read, as a client
// without room for it does.
func TestOpenRefusesTheBody(t *testing.T) {
	me, _ := host.CurrentUser()
	client, stop := startServer(t, me, bodyHandler(func() io.Reader { return strings.NewReader("archive") }))
	defer stop()
	full := errors.New("no room")
	var result Backup
	err := client.CallBody(Request{Op: OpBackup}, &result, func() (io.Writer, error) {
		if result.Name != "test.backup" {
			t.Errorf("open ran before the result was decoded: %+v", result)
		}
		return nil, full
	})
	if !errors.Is(err, full) {
		t.Fatalf("open's refusal: %v", err)
	}
}

// A service before M10 sent a body raw, to the end of the stream, where one
// cut short looks whole, so the client refuses it before taking any.
func TestRawBodyIsRefused(t *testing.T) {
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	endpoint := testEndpoint(t)
	ln, err := host.ListenControl(endpoint, me)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
			return
		}
		_, _ = conn.Write([]byte(`{"ok":true,"result":{"name":"old.backup"},"body":true}` + "\nraw archive bytes"))
	}()
	client := Client{Endpoint: endpoint, Server: host.ControlServer{Account: me}, Timeout: 5 * time.Second}
	opened := false
	err = client.CallBody(Request{Op: OpBackup}, nil, func() (io.Writer, error) {
		opened = true
		return io.Discard, nil
	})
	if err == nil || opened || !strings.Contains(err.Error(), "older") {
		t.Fatalf("a raw body: %v, opened %v", err, opened)
	}
}
