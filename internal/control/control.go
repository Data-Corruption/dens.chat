// Package control is the protocol between the dens CLI, run by the desktop
// user, and the service. Each connection carries one request: a JSON line
// from the client, then a JSON line back, optionally followed by a raw body
// that runs until the service closes the connection, or by a stream of JSON
// lines both ways, as the shortcut helper keeps (M7).
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
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Operations.
const (
	OpStatus = "status"
	OpPair   = "pair"
	OpBackup = "backup"
	// OpShortcuts opens the stream between the desktop user's shortcut
	// helper and the service (M7).
	OpShortcuts = "shortcuts"
	// OpOwnerInvite makes an invite that hands the den this instance hosts
	// to whoever redeems it (M8).
	OpOwnerInvite = "owner-invite"
	// OpQuit stops a session form's service, which has no service manager
	// to stop it (M9.3).
	OpQuit = "quit"
)

const maxLine = 64 * 1024

// maxConns is how many connections the server serves at once. The rest
// wait to be accepted: in the socket's backlog, or for the pipe's one
// listening instance on Windows.
const maxConns = 64

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
	Version     string `json:"version"`
	Instance    string `json:"instance"`
	ClientURL   string `json:"clientURL"`
	PasswordSet bool   `json:"passwordSet"`
	// Locked is the Flatpak waiting for the local password (M9.3).
	Locked     bool `json:"locked,omitempty"`
	DenEnabled bool `json:"denEnabled"`
	// DenProblem says why the den doesn't serve while hosting is on (M9.1).
	DenProblem      string `json:"denProblem,omitempty"`
	UpdateAvailable string `json:"updateAvailable,omitempty"`
}

// Pair is the result of OpPair: a URL carrying a one-time pairing token.
type Pair struct {
	URL string `json:"url"`
}

// OwnerInvite is the result of OpOwnerInvite: an invite string for the
// page's Join box, and the name of the den it hands on.
type OwnerInvite struct {
	Invite string `json:"invite"`
	Den    string `json:"den"`
}

// Reply is what a handler returns. Body, if set, is streamed after the
// response line and closed afterwards. Stream, if set, takes the connection
// after the response line, until it returns. After, if set, runs once the
// reply has gone, as a stop the reply announces.
type Reply struct {
	Result any
	Body   io.ReadCloser
	Stream func(ctx context.Context, s *Stream) error
	After  func()
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

// Serve accepts connections until ctx is cancelled or ln closes, then
// closes ln. A failed accept is tried again after a pause, as net/http
// does, rather than ending the service over one client (M8).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	slots := make(chan struct{}, maxConns)
	var pause time.Duration
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return nil
		}
		conn, err := ln.Accept()
		if err != nil {
			<-slots
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			pause = min(max(2*pause, 5*time.Millisecond), time.Second)
			s.Log.Warnf("control: accept a connection: %v; trying again in %s", err, pause)
			select {
			case <-time.After(pause):
			case <-ctx.Done():
				return nil
			}
			continue
		}
		pause = 0
		go func() {
			defer func() { <-slots }()
			s.serve(ctx, conn)
		}()
	}
}

func (s *Server) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	// Where the system names the account on a connection as it connects,
	// as Linux does, another account is refused before anything it sent is
	// read. A Windows pipe names its client only from data read from it.
	if host.PeerBeforeRequest && !s.authorize(conn) {
		return
	}
	readTimeout := s.ReadTimeout
	if readTimeout == 0 {
		readTimeout = 10 * time.Second
	}
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	reader := bufio.NewReader(conn)
	line, err := readLine(reader)
	if err != nil {
		s.Log.Debugf("control: read request: %v", err)
		return
	}
	if !host.PeerBeforeRequest && !s.authorize(conn) {
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
	sent := s.reply(conn, resp, reply.Body)
	if reply.After != nil {
		defer reply.After()
	}
	if !sent || !resp.OK || reply.Stream == nil {
		return
	}
	// The stream keeps its own deadlines, line by line.
	_ = conn.SetDeadline(time.Time{})
	stream := &Stream{conn: conn, r: reader}
	if err := reply.Stream(ctx, stream); err != nil && ctx.Err() == nil {
		s.Log.Debugf("control: %s: %v", req.Op, err)
	}
}

// authorize checks the account on the other end of conn against the one
// the server answers, and tells any other why it's refused.
func (s *Server) authorize(conn net.Conn) bool {
	peer, err := host.PeerIdentity(conn)
	if err != nil {
		s.Log.Warnf("control: identify peer: %v", err)
		s.reply(conn, Response{Error: "cannot identify caller"}, nil)
		return false
	}
	if !peer.Equal(s.Allowed) {
		s.Log.Warnf("control: refused account %s", peer.ID)
		s.reply(conn, Response{Error: fmt.Sprintf("this instance only answers %s", s.Allowed.Name)}, nil)
		return false
	}
	return true
}

// reply writes the response line and any body, and reports whether the
// line went.
func (s *Server) reply(conn net.Conn, resp Response, body io.ReadCloser) bool {
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
		return false
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		s.Log.Debugf("control: write reply: %v", err)
		return false
	}
	if body != nil && resp.OK {
		if _, err := io.Copy(conn, body); err != nil {
			s.Log.Warnf("control: stream reply body: %v", err)
		}
	}
	return true
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
	resp, reader, err := exchange(conn, req, timeout)
	if err != nil {
		return err
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

// Open sends req and, once the service accepts it, returns the stream the
// reply opens.
func (c Client) Open(req Request) (*Stream, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	conn, err := host.DialControl(c.Endpoint, c.Server, timeout)
	if err != nil {
		return nil, err
	}
	resp, reader, err := exchange(conn, req, timeout)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !resp.OK {
		_ = conn.Close()
		return nil, &UserError{Msg: resp.Error}
	}
	_ = conn.SetDeadline(time.Time{})
	return &Stream{conn: conn, r: reader}, nil
}

// exchange sends req and reads the service's reply line, with timeout for
// both.
func exchange(conn net.Conn, req Request, timeout time.Duration) (Response, *bufio.Reader, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	reader := bufio.NewReader(conn)
	if _, err := conn.Write(append(data, '\n')); err != nil {
		// A service refusing this account answers before it reads the
		// request, and may have closed before the request went. Its answer
		// is still there to read.
		if resp, readErr := readResponse(reader); readErr == nil && !resp.OK {
			return resp, reader, nil
		}
		return Response{}, nil, fmt.Errorf("send request: %w", err)
	}
	resp, err := readResponse(reader)
	return resp, reader, err
}

func readResponse(r *bufio.Reader) (Response, error) {
	line, err := readLine(r)
	if err != nil {
		return Response{}, fmt.Errorf("read reply: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("decode reply: %w", err)
	}
	return resp, nil
}

// Each side of a stream sends a line at least every StreamPing, a ping if
// it has nothing else to say, and gives up on the other after StreamIdle
// without one. Tests shorten them.
var (
	StreamPing = 15 * time.Second
	StreamIdle = 45 * time.Second
)

// Message is one line of a stream: its type, and what it carries.
type Message struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d,omitempty"`
}

// MessagePing is the line that only says a side is still there.
const MessagePing = "ping"

// Stream carries JSON lines both ways on a control connection, once its
// request is answered. Send is safe from several goroutines; Recv is for one.
type Stream struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
	once sync.Once
}

// NewStream is a stream on conn with no request before it, as the
// Flatpak's shortcut helper keeps with its hub over a pipe, inside the
// service (M9.3).
func NewStream(conn net.Conn) *Stream {
	return &Stream{conn: conn, r: bufio.NewReader(conn)}
}

// Send writes a message of type t carrying d, which may be nil.
func (s *Stream) Send(t string, d any) error {
	msg := Message{T: t}
	if d != nil {
		data, err := json.Marshal(d)
		if err != nil {
			return err
		}
		msg.D = data
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = s.conn.Write(append(line, '\n'))
	return err
}

// Recv reads the next message, failing once the other side has been quiet
// for StreamIdle.
func (s *Stream) Recv() (Message, error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(StreamIdle))
	line, err := readLine(s.r)
	if err != nil {
		return Message{}, err
	}
	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return Message{}, fmt.Errorf("decode message: %w", err)
	}
	return msg, nil
}

// KeepAlive pings every StreamPing until ctx ends or a ping fails, which
// closes the stream.
func (s *Stream) KeepAlive(ctx context.Context) {
	ticker := time.NewTicker(StreamPing)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.Send(MessagePing, nil) != nil {
				_ = s.Close()
				return
			}
		}
	}
}

// Close closes the connection; Recv then fails.
func (s *Stream) Close() error {
	var err error
	s.once.Do(func() { err = s.conn.Close() })
	return err
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
