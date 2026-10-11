package host

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// x11Conn is the least of an X11 client the shortcut helper needs: a
// connection to the local display, which asks for the keyboard's state
// (QueryKeymap) and nothing else.
type x11Conn struct {
	mu   sync.Mutex
	conn net.Conn
}

// dialX11 connects to a local display ("[unix]:N[.S]"), with the cookie
// the X authority file holds for it, if any.
func dialX11(display string) (*x11Conn, error) {
	host, rest, ok := strings.Cut(display, ":")
	if !ok || (host != "" && host != "unix") {
		return nil, fmt.Errorf("display %q isn't a local one", display)
	}
	number, _, _ := strings.Cut(rest, ".")
	if number == "" {
		return nil, fmt.Errorf("display %q has no number", display)
	}
	var conn net.Conn
	var err error
	for _, addr := range []string{"@/tmp/.X11-unix/X" + number, "/tmp/.X11-unix/X" + number} {
		if conn, err = net.DialTimeout("unix", addr, 2*time.Second); err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	name, data := xauthCookie(number)
	setup := []byte{'l', 0, 11, 0, 0, 0}
	setup = binary.LittleEndian.AppendUint16(setup, uint16(len(name)))
	setup = binary.LittleEndian.AppendUint16(setup, uint16(len(data)))
	setup = append(setup, 0, 0)
	setup = append(setup, padded(name)...)
	setup = append(setup, padded(data)...)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(setup); err != nil {
		conn.Close()
		return nil, err
	}
	var head [8]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		conn.Close()
		return nil, err
	}
	rest8 := make([]byte, 4*int(binary.LittleEndian.Uint16(head[6:])))
	if _, err := io.ReadFull(conn, rest8); err != nil {
		conn.Close()
		return nil, err
	}
	if head[0] != 1 {
		conn.Close()
		reason := ""
		if head[0] == 0 && int(head[1]) <= len(rest8) {
			reason = ": " + string(rest8[:head[1]])
		}
		return nil, fmt.Errorf("the X server refused the connection%s", reason)
	}
	_ = conn.SetDeadline(time.Time{})
	return &x11Conn{conn: conn}, nil
}

// keymap asks which keys are down: a bit for each of the 256 keycodes.
func (x *x11Conn) keymap() ([32]byte, error) {
	var keys [32]byte
	x.mu.Lock()
	defer x.mu.Unlock()
	_ = x.conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := x.conn.Write([]byte{44, 0, 1, 0}); err != nil { // QueryKeymap
		return keys, err
	}
	// The server may send events every client gets, as MappingNotify,
	// before the reply.
	for {
		var packet [32]byte
		if _, err := io.ReadFull(x.conn, packet[:]); err != nil {
			return keys, err
		}
		switch packet[0] {
		case 0:
			return keys, fmt.Errorf("the X server answered error %d", packet[1])
		case 1:
			copy(keys[:24], packet[8:])
			_, err := io.ReadFull(x.conn, keys[24:])
			return keys, err
		case 35: // GenericEvent, which runs past 32 bytes
			if _, err := io.CopyN(io.Discard, x.conn, 4*int64(binary.LittleEndian.Uint32(packet[4:]))); err != nil {
				return keys, err
			}
		}
	}
}

func (x *x11Conn) close() error { return x.conn.Close() }

// xauthCookie finds the MIT-MAGIC-COOKIE-1 the X authority file holds for
// display number on this host, or none.
func xauthCookie(number string) (name, data []byte) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil
		}
		path = filepath.Join(home, ".Xauthority")
	}
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	hostname, _ := os.Hostname()
	const familyLocal, familyWild = 256, 65535
	r := bytes.NewReader(file)
	field := func() ([]byte, bool) {
		var n uint16
		if binary.Read(r, binary.BigEndian, &n) != nil {
			return nil, false
		}
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		return b, err == nil
	}
	for {
		var family uint16
		if binary.Read(r, binary.BigEndian, &family) != nil {
			return nil, nil
		}
		addr, ok1 := field()
		num, ok2 := field()
		authName, ok3 := field()
		authData, ok4 := field()
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, nil
		}
		hostMatches := family == familyWild || (family == familyLocal && string(addr) == hostname)
		if hostMatches && (len(num) == 0 || string(num) == number) && string(authName) == "MIT-MAGIC-COOKIE-1" {
			return authName, authData
		}
	}
}

func padded(b []byte) []byte {
	return append(b, make([]byte, (4-len(b)%4)%4)...)
}

var errNoX11 = errors.New("no X11 display")
