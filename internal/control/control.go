// Package control is the protocol between the dens CLI, run by the desktop
// user, and the service. Each connection carries one request: a JSON line
// from the client, then a JSON line back, optionally followed by a raw body
// that runs until the service closes the connection.
//
// The endpoint is a Unix socket or named pipe from internal/platform/host.
// The service answers only the one desktop user recorded at install, by the
// identity the operating system reports for the connection.
package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Operations.
const (
	OpStatus = "status"
	OpPair   = "pair"
	OpBackup = "backup"
)

const maxLine = 64 * 1024

// Request is one client request.
type Request struct {
	Op string `json:"op"`
	// Password is the local password, for operations that re-authenticate.
	Password string `json:"password,omitempty"`
}

// Response is the service's reply line.
type Response struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	// Body is true when a raw body follows the line until end of stream.
	Body bool `json:"body,omitempty"`
}

// Status is the result of OpStatus.
type Status struct {
	Version         string `json:"version"`
	Instance        string `json:"instance"`
	ClientURL       string `json:"clientURL"`
	PasswordSet     bool   `json:"passwordSet"`
	DenEnabled      bool   `json:"denEnabled"`
	UpdateAvailable string `json:"updateAvailable,omitempty"`
}

// Pair is the result of OpPair: a URL carrying a one-time pairing token.
type Pair struct {
	URL string `json:"url"`
}

// Reply is what a handler returns. Body, if set, is streamed after the
// response line and closed afterwards.
type Reply struct {
	Result any
	Body   io.ReadCloser
}

// Handler serves one operation.
type Handler func(ctx context.Context, req Request) (Reply, error)

// UserError is reported to the client verbatim. Any other handler error is
// logged and reported as an internal error.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Errorf returns a UserError.
func Errorf(format string, args ...any) error {
	return &UserError{Msg: fmt.Sprintf(format, args...)}
}

// Server answers control requests.
type Server struct {
	// Allowed is the only account whose requests are served.
	Allowed  host.Identity
	Handlers map[string]Handler
	Log      *xlog.Logger
	// ReadTimeout bounds how long a client may take to send its request.
	ReadTimeout time.Duration
	// WriteTimeout bounds sending a reply, including any body.
	WriteTimeout time.Duration
}

// Serve accepts connections until ctx is cancelled, then closes ln.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept control connection: %w", err)
		}
		go s.serve(ctx, conn)
	}
}

func (s *Server) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	readTimeout := s.ReadTimeout
	if readTimeout == 0 {
		readTimeout = 10 * time.Second
	}
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	line, err := readLine(bufio.NewReader(conn))
	if err != nil {
		s.Log.Debugf("control: read request: %v", err)
		return
	}
	// The peer is identified only after its first message was read, which
	// the Windows pipe requires.
	peer, err := host.PeerIdentity(conn)
	if err != nil {
		s.Log.Warnf("control: identify peer: %v", err)
		s.reply(conn, Response{Error: "cannot identify caller"}, nil)
		return
	}
	if !peer.Equal(s.Allowed) {
		s.Log.Warnf("control: refused account %s", peer.ID)
		s.reply(conn, Response{Error: fmt.Sprintf("this instance only answers %s", s.Allowed.Name)}, nil)
		return
	}
	var req Request
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.reply(conn, Response{Error: "malformed request"}, nil)
		return
	}
	handler := s.Handlers[req.Op]
	if handler == nil {
		s.reply(conn, Response{Error: fmt.Sprintf("unknown operation %q", req.Op)}, nil)
		return
	}
	s.Log.Debugf("control: %s", req.Op)
	reply, err := handler(ctx, req)
	if err != nil {
		var userErr *UserError
		if errors.As(err, &userErr) {
			s.reply(conn, Response{Error: userErr.Msg}, nil)
			return
		}
		s.Log.Errorf("control: %s: %v", req.Op, err)
		s.reply(conn, Response{Error: "internal error; see the service log"}, nil)
		return
	}
	resp := Response{OK: true, Body: reply.Body != nil}
	if reply.Result != nil {
		resp.Result, err = json.Marshal(reply.Result)
		if err != nil {
			s.Log.Errorf("control: %s: encode result: %v", req.Op, err)
			resp = Response{Error: "internal error; see the service log"}
		}
	}
	s.reply(conn, resp, reply.Body)
}

func (s *Server) reply(conn net.Conn, resp Response, body io.ReadCloser) {
	if body != nil {
		defer body.Close()
	}
	writeTimeout := s.WriteTimeout
	if writeTimeout == 0 {
		writeTimeout = 10 * time.Second
	}
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		s.Log.Debugf("control: write reply: %v", err)
		return
	}
	if body != nil && resp.OK {
		if _, err := io.Copy(conn, body); err != nil {
			s.Log.Warnf("control: stream reply body: %v", err)
		}
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, errors.New("request line too long")
		}
		if !isPrefix {
			return line, nil
		}
	}
}

// Client sends control requests to one instance.
type Client struct {
	Endpoint string
	Server   host.ControlServer
	// Timeout bounds connecting and exchanging the request and reply line.
	Timeout time.Duration
}

// Call sends req and decodes the result into result, which may be nil.
func (c Client) Call(req Request, result any) error {
	return c.CallBody(req, result, nil)
}

// CallBody is Call for operations that stream a body, which is copied to
// body until the service closes the connection.
func (c Client) CallBody(req Request, result any, body io.Writer) error {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	conn, err := host.DialControl(c.Endpoint, c.Server, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	reader := bufio.NewReader(conn)
	line, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("read reply: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("decode reply: %w", err)
	}
	if !resp.OK {
		return &UserError{Msg: resp.Error}
	}
	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
	}
	if resp.Body {
		if body == nil {
			return errors.New("service sent a body this call can't take")
		}
		// A body can be large; only an idle connection times out.
		_ = conn.SetDeadline(time.Time{})
		if _, err := io.Copy(body, &idleTimeoutReader{conn: conn, r: reader, idle: timeout}); err != nil {
			return fmt.Errorf("receive body: %w", err)
		}
	}
	return nil
}

// idleTimeoutReader extends the read deadline before every read, so a slow
// but live transfer continues and a stalled one fails.
type idleTimeoutReader struct {
	conn net.Conn
	r    io.Reader
	idle time.Duration
}

func (t *idleTimeoutReader) Read(p []byte) (int, error) {
	_ = t.conn.SetReadDeadline(time.Now().Add(t.idle))
	return t.r.Read(p)
}
