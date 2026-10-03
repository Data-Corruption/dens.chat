package denclient

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
)

func TestMediaAddrs(t *testing.T) {
	local := []netip.Addr{netip.MustParseAddr("192.168.1.10"), netip.MustParseAddr("fd00::10")}
	parse := func(ips ...string) []netip.Addr {
		var out []netip.Addr
		for _, ip := range ips {
			out = append(out, netip.MustParseAddr(ip))
		}
		return out
	}
	cases := []struct {
		name     string
		base     string
		own      bool
		resolves []netip.Addr
		want     []netip.Addr
	}{
		{"the install's own den", "http://127.0.0.1:8485", true, nil, local},
		{"a name", "https://den.test", false, parse("203.0.113.5", "2001:db8::5"), parse("203.0.113.5", "2001:db8::5")},
		{"split DNS at home", "https://den.test", false, parse("192.168.1.20"), parse("192.168.1.20")},
		{"a name on this machine", "https://den.test", false, parse("127.0.0.1"), local},
		{"an IPv6 name on this machine", "https://den.test", false, parse("::1"), local},
		{"repeats and mapped addresses", "https://den.test", false,
			parse("203.0.113.5", "::ffff:203.0.113.5", "fe80::1", "224.0.0.1"), parse("203.0.113.5")},
		{"an address", "https://198.51.100.7:8443", false, nil, parse("198.51.100.7")},
		{"an IPv6 address", "https://[2001:db8::7]", false, nil, parse("2001:db8::7")},
	}
	for _, c := range cases {
		m := &Manager{
			LookupHost: func(_ context.Context, host string) ([]netip.Addr, error) {
				if host != "den.test" {
					return nil, errors.New("looked up " + host)
				}
				return c.resolves, nil
			},
			LocalAddrs: func() ([]netip.Addr, error) { return local, nil },
		}
		got, err := m.mediaAddrs(context.Background(), c.base, c.own)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}

	m := &Manager{LookupHost: func(context.Context, string) ([]netip.Addr, error) { return parse("fe80::1"), nil }}
	if _, err := m.mediaAddrs(context.Background(), "https://den.test", false); err == nil {
		t.Error("a name with only a link-local address gave somewhere to send media")
	}
}
