package host

import (
	"fmt"
	"net"
	"strconv"
)

// PortFree returns an error if another socket holds port on any address:
// a TCP listener, or a bound UDP socket. network is "tcp" or "udp".
func PortFree(network string, port int) error {
	address := net.JoinHostPort("", strconv.Itoa(port))
	switch network {
	case "tcp":
		ln, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		return ln.Close()
	case "udp":
		conn, err := net.ListenPacket("udp", address)
		if err != nil {
			return err
		}
		return conn.Close()
	default:
		return fmt.Errorf("unsupported network %q", network)
	}
}
