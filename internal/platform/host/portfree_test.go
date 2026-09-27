package host

import (
	"net"
	"testing"
)

// The held sockets bind loopback only, which never makes Windows Firewall
// prompt; PortFree must still see them.
func TestPortFreeSeesHeldPorts(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcpPort := ln.Addr().(*net.TCPAddr).Port
	if err := PortFree("tcp", tcpPort); err == nil {
		t.Fatalf("TCP port %d looked free while a listener held it", tcpPort)
	}
	ln.Close()
	if err := PortFree("tcp", tcpPort); err != nil {
		t.Fatalf("TCP port %d after closing its listener: %v", tcpPort, err)
	}

	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udpPort := conn.LocalAddr().(*net.UDPAddr).Port
	if err := PortFree("udp", udpPort); err == nil {
		t.Fatalf("UDP port %d looked free while a socket held it", udpPort)
	}
	conn.Close()
	if err := PortFree("udp", udpPort); err != nil {
		t.Fatalf("UDP port %d after closing its socket: %v", udpPort, err)
	}
}
