package host

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pipeClientRights lets the recorded user read and write pipe data. It leaves
// out FILE_APPEND_DATA, which on a pipe is FILE_CREATE_PIPE_INSTANCE: with it
// the user could add server instances of the name and pose as the service.
const pipeClientRights = 0x0012019b

var (
	modadvapi32                    = windows.NewLazySystemDLL("advapi32.dll")
	procImpersonateNamedPipeClient = modadvapi32.NewProc("ImpersonateNamedPipeClient")
)

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

// ListenControl creates the named pipe name. Its DACL grants SYSTEM and this
// process's account full access and allowed only data access, and remote
// clients are rejected. FILE_FLAG_FIRST_PIPE_INSTANCE makes startup fail if
// another process already holds the name, instead of joining its pipe.
func ListenControl(name string, allowed Identity) (net.Listener, error) {
	own, err := processSID()
	if err != nil {
		return nil, err
	}
	if _, err := windows.StringToSid(allowed.ID); err != nil {
		return nil, fmt.Errorf("allowed control user %q is not a SID: %w", allowed.ID, err)
	}
	sddl := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)(A;;%#x;;;%s)", own, pipeClientRights, allowed.ID)
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf("build control pipe security descriptor: %w", err)
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	first, err := createPipeInstance(name, sa, true)
	if err != nil {
		return nil, fmt.Errorf("create control pipe %s: %w", name, err)
	}
	closeEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(first)
		return nil, err
	}
	return &pipeListener{name: name, sa: sa, next: first, closeEvent: closeEvent}, nil
}

func createPipeInstance(name string, sa *windows.SecurityAttributes, first bool) (windows.Handle, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if first {
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	mode := uint32(windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT | windows.PIPE_REJECT_REMOTE_CLIENTS)
	return windows.CreateNamedPipe(path, flags, mode, windows.PIPE_UNLIMITED_INSTANCES, 64*1024, 64*1024, 0, sa)
}

// pipeListener always holds one unconnected instance of the pipe. Accept
// creates the next instance before returning a connected one, so the name
// never goes unheld between connections and can't be claimed by another
// process. Accept must not be called concurrently.
type pipeListener struct {
	name       string
	sa         *windows.SecurityAttributes
	closeEvent windows.Handle
	acceptMu   sync.Mutex

	mu        sync.Mutex
	next      windows.Handle
	accepting bool
	closed    bool
}

func (l *pipeListener) Accept() (net.Conn, error) {
	l.acceptMu.Lock()
	defer l.acceptMu.Unlock()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, net.ErrClosed
	}
	h := l.next
	l.accepting = true
	l.mu.Unlock()

	connectErr := l.connect(h)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.accepting = false
	if l.closed {
		_ = windows.CloseHandle(h)
		return nil, net.ErrClosed
	}
	if connectErr != nil {
		return nil, connectErr
	}
	next, err := createPipeInstance(l.name, l.sa, false)
	if err != nil {
		_ = windows.CloseHandle(h)
		l.next = windows.InvalidHandle
		l.closed = true
		return nil, fmt.Errorf("create next control pipe instance: %w", err)
	}
	l.next = next
	conn, err := newPipeConn(h, l.name, true)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return conn, nil
}

// connect waits for a client on h, or for Close. The connect is overlapped
// so that Close can interrupt it; the operation is always completed before
// connect returns, because the kernel writes into the OVERLAPPED.
func (l *pipeListener) connect(h windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	overlapped := &windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(h, overlapped)
	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		return nil
	case !errors.Is(err, windows.ERROR_IO_PENDING):
		return fmt.Errorf("connect control pipe: %w", err)
	}
	result, err := windows.WaitForMultipleObjects([]windows.Handle{event, l.closeEvent}, false, windows.INFINITE)
	var done uint32
	if result == windows.WAIT_OBJECT_0 {
		return windows.GetOverlappedResult(h, overlapped, &done, false)
	}
	_ = windows.CancelIoEx(h, overlapped)
	_ = windows.GetOverlappedResult(h, overlapped, &done, true)
	if err != nil {
		return err
	}
	return net.ErrClosed
}

func (l *pipeListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	// A pending Accept owns its handle and closes it once its connect is
	// cancelled; otherwise the idle instance is closed here.
	if !l.accepting && l.next != windows.InvalidHandle {
		_ = windows.CloseHandle(l.next)
	}
	l.next = windows.InvalidHandle
	return windows.SetEvent(l.closeEvent)
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr(l.name) }

// pipeConn is a net.Conn over an overlapped pipe handle. Each read or write
// waits on its own event and on the connection's close event, so deadlines
// and Close interrupt blocked I/O. Server-side Close only closes the handle:
// DisconnectNamedPipe would discard a reply the client hasn't read yet.
type pipeConn struct {
	h          windows.Handle
	name       string
	server     bool
	closeEvent windows.Handle
	closed     atomic.Bool
	closeOnce  sync.Once
	readMu     sync.Mutex
	writeMu    sync.Mutex

	deadlineMu    sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

func newPipeConn(h windows.Handle, name string, server bool) (*pipeConn, error) {
	closeEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, err
	}
	return &pipeConn{h: h, name: name, server: server, closeEvent: closeEvent}, nil
}

func (c *pipeConn) Read(b []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(b) == 0 {
		return 0, nil
	}
	c.deadlineMu.Lock()
	deadline := c.readDeadline
	c.deadlineMu.Unlock()
	n, err := c.io(b, false, deadline)
	if err != nil && isPipeEnd(err) {
		return n, io.EOF
	}
	return n, err
}

func (c *pipeConn) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	written := 0
	for written < len(b) {
		c.deadlineMu.Lock()
		deadline := c.writeDeadline
		c.deadlineMu.Unlock()
		n, err := c.io(b[written:], true, deadline)
		written += n
		if err != nil {
			if isPipeEnd(err) {
				return written, io.ErrClosedPipe
			}
			return written, err
		}
	}
	return written, nil
}

func (c *pipeConn) io(b []byte, write bool, deadline time.Time) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	overlapped := &windows.Overlapped{HEvent: event}
	var done uint32
	if write {
		err = windows.WriteFile(c.h, b, &done, overlapped)
	} else {
		err = windows.ReadFile(c.h, b, &done, overlapped)
	}
	if err == nil {
		return int(done), nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return int(done), err
	}

	timeout := uint32(windows.INFINITE)
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			remaining = 0
		}
		timeout = uint32(remaining.Milliseconds())
	}
	result, waitErr := windows.WaitForMultipleObjects([]windows.Handle{event, c.closeEvent}, false, timeout)
	if result == windows.WAIT_OBJECT_0 {
		err := windows.GetOverlappedResult(c.h, overlapped, &done, false)
		return int(done), err
	}
	_ = windows.CancelIoEx(c.h, overlapped)
	completeErr := windows.GetOverlappedResult(c.h, overlapped, &done, true)
	if completeErr == nil && done > 0 {
		// The operation finished before the cancel reached it.
		return int(done), nil
	}
	switch {
	case result == windows.WAIT_OBJECT_0+1:
		return int(done), net.ErrClosed
	case result == uint32(windows.WAIT_TIMEOUT):
		return int(done), os.ErrDeadlineExceeded
	case waitErr != nil:
		return int(done), waitErr
	default:
		return int(done), fmt.Errorf("wait for pipe I/O: result %d", result)
	}
}

func isPipeEnd(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) ||
		errors.Is(err, windows.ERROR_NO_DATA)
}

func (c *pipeConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		_ = windows.SetEvent(c.closeEvent)
		// Wait out in-flight I/O before the handle goes away.
		c.readMu.Lock()
		c.writeMu.Lock()
		err = windows.CloseHandle(c.h)
		_ = windows.CloseHandle(c.closeEvent)
		c.writeMu.Unlock()
		c.readMu.Unlock()
	})
	return err
}

func (c *pipeConn) LocalAddr() net.Addr  { return pipeAddr(c.name) }
func (c *pipeConn) RemoteAddr() net.Addr { return pipeAddr(c.name) }

func (c *pipeConn) SetDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline, c.writeDeadline = t, t
	c.deadlineMu.Unlock()
	return nil
}

func (c *pipeConn) SetReadDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.deadlineMu.Unlock()
	return nil
}

func (c *pipeConn) SetWriteDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.writeDeadline = t
	c.deadlineMu.Unlock()
	return nil
}

// PeerIdentity returns the account of the client on a server-side control
// connection. The pipe identifies the client from the last data read, so
// call it only after reading the first request.
func PeerIdentity(conn net.Conn) (Identity, error) {
	c, ok := conn.(*pipeConn)
	if !ok || !c.server {
		return Identity{}, fmt.Errorf("control connection is %T, not a server pipe", conn)
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	sid, err := impersonatedSID(c.h)
	if err != nil {
		return Identity{}, err
	}
	return Identity{ID: sid}, nil
}

// impersonatedSID impersonates the pipe client just long enough to read its
// SID. Without SeImpersonatePrivilege the impersonation is at identification
// level, which is all this needs.
func impersonatedSID(h windows.Handle) (string, error) {
	runtime.LockOSThread()
	if r, _, callErr := procImpersonateNamedPipeClient.Call(uintptr(h)); r == 0 {
		runtime.UnlockOSThread()
		return "", fmt.Errorf("impersonate control client: %w", callErr)
	}
	sid, err := threadSID()
	if revertErr := windows.RevertToSelf(); revertErr != nil {
		// Leave the thread locked: the runtime discards it with the goroutine
		// rather than reusing a thread that is still impersonating.
		return "", fmt.Errorf("revert impersonation: %w", revertErr)
	}
	runtime.UnlockOSThread()
	return sid, err
}

func threadSID() (string, error) {
	thread, err := windows.GetCurrentThread()
	if err != nil {
		return "", err
	}
	var token windows.Token
	if err := windows.OpenThreadToken(thread, windows.TOKEN_QUERY, true, &token); err != nil {
		return "", fmt.Errorf("open impersonation token: %w", err)
	}
	defer token.Close()
	sid, err := tokenSID(token)
	if err != nil {
		return "", err
	}
	return sid.String(), nil
}

// DialControl opens the control pipe name and checks who serves it before
// trusting it: the process the SCM reports for server.ServiceName, or for a
// development instance a process running as server.Account.
func DialControl(name string, server ControlServer, timeout time.Duration) (net.Conn, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	var h windows.Handle
	for {
		h, err = windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			break
		}
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil, fmt.Errorf("%w: %s", os.ErrNotExist, name)
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) || time.Now().After(deadline) {
			return nil, fmt.Errorf("open %s: %w", name, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var serverPID uint32
	if err := windows.GetNamedPipeServerProcessId(h, &serverPID); err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("identify control pipe server: %w", err)
	}
	if err := checkPipeServer(serverPID, server); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	conn, err := newPipeConn(h, name, false)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return conn, nil
}

func checkPipeServer(pid uint32, server ControlServer) error {
	if server.ServiceName != "" {
		state, err := ServiceStatus(server.ServiceName)
		if err != nil {
			return fmt.Errorf("query service %s: %w", server.ServiceName, err)
		}
		if !state.Running || uint32(state.PID) != pid {
			return fmt.Errorf("control pipe is served by process %d, not service %s (process %d)", pid, server.ServiceName, state.PID)
		}
		return nil
	}
	owner, err := processOwner(pid)
	if err != nil {
		return err
	}
	if !owner.Equal(server.Account) {
		return fmt.Errorf("control pipe is served by %s, not %s", owner, server.Account)
	}
	return nil
}
