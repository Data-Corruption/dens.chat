package host

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ListenControl listens on the Unix socket at path. Any local user may
// connect (mode 0666); the server authorizes each connection by the UID the
// kernel reports for it (PeerIdentity), as the system D-Bus does. allowed is
// unused here and exists for the Windows pipe, whose DACL also names it.
func ListenControl(path string, _ Identity) (net.Listener, error) {
	if len(path) >= len(unix.RawSockaddrUnix{}.Path) {
		return nil, fmt.Errorf("control socket path %s is longer than a Unix socket path may be", path)
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on control socket %s: %w", path, err)
	}
	// The service's UMask creates the socket 0600.
	if err := os.Chmod(path, 0o666); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("open control socket to local users: %w", err)
	}
	return ln, nil
}

// removeStaleSocket deletes a socket left by an earlier run. The runtime
// directory belongs to the service, so anything else at path is refused.
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect control socket path: %w", err)
	}
	if info.Mode()&fs.ModeSocket == 0 {
		return fmt.Errorf("control socket path %s exists and is not a socket", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("control socket path %s belongs to another user", path)
	}
	return os.Remove(path)
}

// PeerIdentity returns the account on the other end of a control connection.
func PeerIdentity(conn net.Conn) (Identity, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return Identity{}, err
	}
	return Identity{ID: strconv.FormatUint(uint64(cred.Uid), 10)}, nil
}

func peerCred(conn net.Conn) (*unix.Ucred, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("control connection is %T, not a Unix socket", conn)
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	if credErr != nil {
		return nil, fmt.Errorf("read peer credentials: %w", credErr)
	}
	return cred, nil
}

// DialControl connects to the control socket at path and checks that the
// process serving it runs as server.Account, so another local user can't
// stand in for the service.
func DialControl(path string, server ControlServer, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	cred, err := peerCred(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if uid := strconv.FormatUint(uint64(cred.Uid), 10); uid != server.Account.ID {
		_ = conn.Close()
		return nil, fmt.Errorf("control socket is served by UID %s, not the service account %s", uid, server.Account)
	}
	return conn, nil
}
