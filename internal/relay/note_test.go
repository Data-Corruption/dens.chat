package relay

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func aHello(at time.Time, addrs ...string) note {
	id := NewID()
	return note{Kind: kindHello, ID: id[:], Name: "Desk", Offers: []string{offerKeys}, At: at.UnixMilli(),
		Attempt: denproto.Random(attemptSize), Wants: []string{offerKeys},
		Half: &half{Fingerprint: denproto.Random(32), Ufrag: "abcdABCD12345678", Pwd: strings.Repeat("p", 32), Port: 40000, Addrs: addrs}}
}

func decoded(t *testing.T, n note, now time.Time, loopback bool) (note, error) {
	t.Helper()
	b, err := n.encode()
	if err != nil {
		t.Fatal(err)
	}
	return decodeNote(b, now, privateAddr(loopback))
}

// A note decodes as it was written, the largest fits its box, and a hello
// naming any address off the local network, a stale one and malformed ones
// are refused. Offers a newer Dens makes are left out.
func TestNotes(t *testing.T) {
	now := time.Now()
	got, err := decoded(t, aHello(now, "192.168.1.20", "10.0.0.5"), now, false)
	if err != nil || got.Kind != kindHello || got.Half.Port != 40000 || len(got.Half.Addrs) != 2 {
		t.Fatalf("a hello decoded as %+v, %v", got, err)
	}

	largest := aHello(now, "192.168.100.200", "192.168.100.201", "192.168.100.202", "192.168.100.203", "172.31.255.250", "172.31.255.251",
		"10.255.255.250", "10.255.255.251")
	largest.Name = strings.Repeat("<", denproto.MaxLabelRunes)
	largest.Offers = []string{offerKeys, offerSound, "more"}
	if _, err := largest.encode(); err != nil {
		t.Errorf("the largest note doesn't fit its box: %v", err)
	}

	newer := aHello(now, "192.168.1.20")
	newer.Offers, newer.Wants = []string{"screen", offerSound, offerKeys}, []string{"screen", offerSound}
	if got, err := decoded(t, newer, now, false); err != nil || !slices.Equal(got.Offers, []string{offerSound, offerKeys}) ||
		!slices.Equal(got.Wants, []string{offerSound}) {
		t.Errorf("a newer Dens's offers decoded as %v and %v, %v", got.Offers, got.Wants, err)
	}

	for what, n := range map[string]note{
		"a public address":          aHello(now, "192.168.1.20", "8.8.8.8"),
		"an IPv6 address":           aHello(now, "fd00::1"),
		"loopback":                  aHello(now, "127.0.0.1"),
		"a link-local address":      aHello(now, "169.254.10.10"),
		"no address":                aHello(now),
		"nine addresses":            aHello(now, "10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.6", "10.0.0.7", "10.0.0.8", "10.0.0.9"),
		"a time a minute old":       aHello(now.Add(-stale-time.Second), "10.0.0.1"),
		"a time a minute to come":   aHello(now.Add(stale+time.Second), "10.0.0.1"),
		"a kind it doesn't know":    func() note { n := aHello(now, "10.0.0.1"); n.Kind = "goodbye"; return n }(),
		"a short Relay ID":          func() note { n := aHello(now, "10.0.0.1"); n.ID = n.ID[:8]; return n }(),
		"no name":                   func() note { n := aHello(now, "10.0.0.1"); n.Name = " "; return n }(),
		"no attempt":                func() note { n := aHello(now, "10.0.0.1"); n.Attempt = nil; return n }(),
		"no half":                   func() note { n := aHello(now, "10.0.0.1"); n.Half = nil; return n }(),
		"a short fingerprint":       func() note { n := aHello(now, "10.0.0.1"); n.Half.Fingerprint = n.Half.Fingerprint[:20]; return n }(),
		"a short password":          func() note { n := aHello(now, "10.0.0.1"); n.Half.Pwd = "short"; return n }(),
		"a password with a colon":   func() note { n := aHello(now, "10.0.0.1"); n.Half.Pwd = strings.Repeat("p:", 16); return n }(),
		"port 0":                    func() note { n := aHello(now, "10.0.0.1"); n.Half.Port = 0; return n }(),
		"an address that isn't one": aHello(now, "10.0.0.1:80"),
	} {
		if got, err := decoded(t, n, now, false); err == nil {
			t.Errorf("a hello with %s decoded as %+v", what, got)
		}
	}

	// A development instance's links take loopback, and nothing more.
	if _, err := decoded(t, aHello(now, "127.0.0.1"), now, true); err != nil {
		t.Errorf("loopback, where it's allowed: %v", err)
	}
	if _, err := decoded(t, aHello(now, "127.0.0.1", "1.1.1.1"), now, true); err == nil {
		t.Error("a public address beside loopback, where loopback's allowed")
	}

	// who and here need no half.
	id := NewID()
	who := note{Kind: kindWho, ID: id[:], Name: "Laptop", At: now.UnixMilli()}
	if got, err := decoded(t, who, now, false); err != nil || got.Name != "Laptop" {
		t.Errorf("a who decoded as %+v, %v", got, err)
	}
}

func TestRelayIDs(t *testing.T) {
	id := NewID()
	if back, err := ParseID(id.String()); err != nil || back != id {
		t.Errorf("a Relay ID read back as %v, %v", back, err)
	}
	for _, s := range []string{"", "abc", id.String()[:30], id.String() + "00", strings.Repeat("g", 32)} {
		if _, err := ParseID(s); err == nil {
			t.Errorf("%q read as a Relay ID", s)
		}
	}
	if !privateAddr(false)(netip.MustParseAddr("172.16.0.1")) || privateAddr(false)(netip.MustParseAddr("172.32.0.1")) {
		t.Error("private addresses aren't RFC 1918's")
	}
}
