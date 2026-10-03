package denproto

import (
	"net/netip"
	"strings"
	"testing"
)

// sdpLines joins lines as SDP does, with a line break after the last.
func sdpLines(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

var testOffer = sdpLines(
	"v=0", "o=- 1 2 IN IP4 0.0.0.0", "s=-", "t=0 0", "a=ice-lite", "a=fingerprint:sha-256 AA:BB", "a=group:BUNDLE 0 1",
	"m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=setup:actpass", "a=mid:0", "a=ice-ufrag:abcd",
	"a=ice-pwd:efghijklmnopqrstuvwxyz12", "a=rtcp-mux", "a=rtpmap:111 opus/48000/2", "a=recvonly",
	"a=candidate:1 1 udp 2130706431 10.0.0.43 7881 typ host", "a=end-of-candidates",
	"m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=mid:1", "a=rtpmap:111 opus/48000/2", "a=sendonly",
	"a=msid:1002 audio",
)

func TestStripCandidates(t *testing.T) {
	got := StripCandidates(testOffer)
	if strings.Contains(got, "candidate") {
		t.Fatalf("candidates left:\n%s", got)
	}
	want := strings.Replace(testOffer, "a=candidate:1 1 udp 2130706431 10.0.0.43 7881 typ host\r\na=end-of-candidates\r\n", "", 1)
	if got != want {
		t.Fatalf("stripping changed more than the candidates:\n%q\nwant\n%q", got, want)
	}
}

func TestAddCandidates(t *testing.T) {
	addrs := []netip.Addr{netip.MustParseAddr("203.0.113.5"), netip.MustParseAddr("::ffff:192.0.2.1"), netip.MustParseAddr("2001:db8::5")}
	got, err := AddCandidates(StripCandidates(testOffer), addrs, 7881, 7882)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckOffer(got); err != nil {
		t.Fatalf("the written offer doesn't check: %v", err)
	}
	first, second, _ := strings.Cut(got, "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\na=mid:1")
	if strings.Contains(second, "candidate") {
		t.Fatalf("candidates outside the first section:\n%s", got)
	}
	want := []string{
		"a=candidate:1 1 udp 2130706431 203.0.113.5 7881 typ host",
		"a=candidate:3 1 udp 2130706430 192.0.2.1 7881 typ host",
		"a=candidate:5 1 udp 2130706429 2001:db8::5 7881 typ host",
		"a=candidate:2 1 tcp 1671430143 203.0.113.5 7882 typ host tcptype passive",
		"a=candidate:4 1 tcp 1671430142 192.0.2.1 7882 typ host tcptype passive",
		"a=candidate:6 1 tcp 1671430141 2001:db8::5 7882 typ host tcptype passive",
		"a=end-of-candidates",
	}
	if !strings.HasSuffix(first, "a=recvonly\r\n"+strings.Join(want, "\r\n")+"\r\n") {
		t.Fatalf("the first section's candidates:\n%s", first)
	}

	// A first section that's also the last gets them before the SDP's end.
	only := sdpLines("v=0", "o=- 1 2 IN IP4 0.0.0.0", "s=-", "t=0 0", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:0")
	got, err = AddCandidates(only, addrs[:1], 7881, 7882)
	if err != nil || !strings.HasSuffix(got, "a=mid:0\r\n"+want[0]+"\r\n"+want[3]+"\r\na=end-of-candidates\r\n") {
		t.Fatalf("one section: %q, %v", got, err)
	}

	for _, bad := range []struct {
		addrs    []netip.Addr
		udp, tcp int
	}{{nil, 7881, 7882}, {addrs, 0, 7882}, {addrs, 7881, 70000}} {
		if _, err := AddCandidates(testOffer, bad.addrs, bad.udp, bad.tcp); err == nil {
			t.Errorf("wrote candidates for %v, %d, %d", bad.addrs, bad.udp, bad.tcp)
		}
	}
	if _, err := AddCandidates(sdpLines("v=0", "s=-"), addrs, 7881, 7882); err == nil {
		t.Error("wrote candidates into an SDP with no media")
	}
}

func TestCheckOffer(t *testing.T) {
	if err := CheckOffer(testOffer); err != nil {
		t.Fatal(err)
	}
	for name, offer := range map[string]string{
		"video":      strings.Replace(testOffer, "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\na=mid:1", "m=video 9 UDP/TLS/RTP/SAVPF 96\r\nc=IN IP4 0.0.0.0\r\na=mid:1", 1),
		"data":       strings.Replace(testOffer, "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\na=mid:1", "m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\nc=IN IP4 0.0.0.0\r\na=mid:1", 1),
		"no media":   sdpLines("v=0", "o=- 1 2 IN IP4 0.0.0.0", "s=-", "t=0 0"),
		"malformed":  "not an offer",
		"over limit": testOffer + strings.Repeat("a=x\r\n", MaxSDP/5),
	} {
		if CheckOffer(offer) == nil {
			t.Errorf("%s: passed", name)
		}
	}
}
