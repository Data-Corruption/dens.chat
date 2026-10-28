package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/wlynxg/anet"
)

// network looks names up as Dens does, through the system's resolver
// (bionic's, which asks netd, Private DNS included) and through Go's own,
// which Android gives no resolv.conf; lists the interfaces as Go and Pion
// do; and makes the connections a member's client makes.
func network(r *reporter, lan string) {
	r.section("network")
	for _, name := range []string{"dens.chat", "releases.dens.chat", "www.youtube-nocookie.com", "localhost"} {
		r.line("lookup "+name, "%s", lookup(net.DefaultResolver, name))
	}
	r.line("Go's own resolver", "%s", lookup(&net.Resolver{PreferGo: true}, "dens.chat"))

	ifaces, err := net.Interfaces()
	if err != nil {
		r.err("net.Interfaces", err)
	} else {
		r.line("net.Interfaces", "%s", ifaceNames(ifaces))
	}
	addrs, err := net.InterfaceAddrs()
	r.line("net.InterfaceAddrs", "%s %s", addrCount(addrs), errOrEmpty(err))
	aifaces, err := anet.Interfaces()
	if err != nil {
		r.err("anet.Interfaces", err)
	} else {
		r.line("anet.Interfaces", "%s", ifaceNames(aifaces))
	}
	aaddrs, err := anet.InterfaceAddrs()
	r.line("anet.InterfaceAddrs", "%s %s", addrCount(aaddrs), errOrEmpty(err))

	for _, dir := range []string{"/system/etc/security/cacerts", "/apex/com.android.conscrypt/cacerts"} {
		entries, err := os.ReadDir(dir)
		r.line("CA certificates", "%s: %d %s", dir, len(entries), errOrEmpty(err))
	}
	start := time.Now()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", "dens.chat:443", &tls.Config{ServerName: "dens.chat"})
	if err != nil {
		r.err("TLS dens.chat", err)
	} else {
		state := conn.ConnectionState()
		r.line("TLS dens.chat", "ok in %v, %s, chain to %s", time.Since(start).Round(time.Millisecond),
			tls.VersionName(state.Version), state.PeerCertificates[len(state.PeerCertificates)-1].Issuer.CommonName)
		conn.Close()
	}
	r.line("UDP to 1.1.1.1:53", "%s", udpDNS("1.1.1.1:53"))

	if lan != "" {
		// Android 17's local network permission: a refused TCP
		// connection times out, and a refused datagram fails at once.
		start := time.Now()
		c, err := net.DialTimeout("tcp", lan, 5*time.Second)
		if err == nil {
			c.Close()
		}
		r.line("TCP to "+lan, "%s after %v", orOK(err), time.Since(start).Round(time.Millisecond))
		u, err := net.Dial("udp", lan)
		if err == nil {
			_, err = u.Write([]byte("aprobe"))
			u.Close()
		}
		r.line("UDP to "+lan, "%s", orOK(err))
	}
}

func lookup(res *net.Resolver, name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	start := time.Now()
	ips, err := res.LookupNetIP(ctx, "ip", name)
	took := time.Since(start).Round(time.Millisecond)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return fmt.Sprintf("error after %v: %s", took, dnsErr.Err)
		}
		return fmt.Sprintf("error after %v: %v", took, err)
	}
	return fmt.Sprintf("%d addresses in %v", len(ips), took)
}

func ifaceNames(ifaces []net.Interface) string {
	var names []string
	for _, i := range ifaces {
		names = append(names, fmt.Sprintf("%s(%v)", i.Name, i.Flags))
	}
	return strings.Join(names, " ")
}

// addrCount says how many addresses of each kind there are, never which.
func addrCount(addrs []net.Addr) string {
	var v4, v6, loop int
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		switch {
		case !ok:
		case ipnet.IP.IsLoopback():
			loop++
		case ipnet.IP.To4() != nil:
			v4++
		default:
			v6++
		}
	}
	return fmt.Sprintf("%d IPv4, %d IPv6, %d loopback", v4, v6, loop)
}

// udpDNS asks a public resolver for dens.chat's address over plain UDP:
// DNS's port is exempt from Android 17's local network permission, and a
// public address needs none anyway.
func udpDNS(server string) string {
	c, err := net.DialTimeout("udp", server, 3*time.Second)
	if err != nil {
		return "error: " + err.Error()
	}
	defer c.Close()
	query := []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0,
		4, 'd', 'e', 'n', 's', 4, 'c', 'h', 'a', 't', 0, 0, 1, 0, 1}
	start := time.Now()
	if _, err := c.Write(query); err != nil {
		return "error: " + err.Error()
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("answered %d bytes in %v", n, time.Since(start).Round(time.Millisecond))
}
