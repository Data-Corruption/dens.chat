package denproto

import (
	"encoding/json"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/pion/sdp/v3"
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

// TestStripAddresses checks that a browser's answer comes out with no
// address in it (M8). testdata/answers.json holds Chromium's and Firefox's,
// each its second in a call, after it had gathered, taken in the browser
// check's container, with a documentation address in place of the
// container's. Firefox's names its host candidates, as it does once the page
// may use the microphone, rather than mDNS names.
func TestStripAddresses(t *testing.T) {
	data, err := os.ReadFile("testdata/answers.json")
	if err != nil {
		t.Fatal(err)
	}
	var answers map[string]string
	if err := json.Unmarshal(data, &answers); err != nil {
		t.Fatal(err)
	}
	// An answer from a computer with IPv6, with its RTCP's address.
	answers["ipv6"] = sdpLines("v=0", "o=- 5 2 IN IP6 2001:db8::7", "s=-", "c=IN IP6 2001:db8::7", "t=0 0",
		"m=audio 50000 UDP/TLS/RTP/SAVPF 111", "c=IN IP6 2001:db8::7", "a=rtcp:50001 IN IP6 2001:db8::7", "a=mid:0",
		"a=rtpmap:111 opus/48000/2", "a=candidate:1 1 udp 2122260223 2001:db8::7 50000 typ host")
	for name, answer := range answers {
		if !strings.Contains(answer, "c=IN IP4 192.0.2.10") && !strings.Contains(answer, "c=IN IP6 2001:db8::7") {
			t.Fatalf("%s: the fixture names no address to take out", name)
		}
		got := StripCandidates(answer)
		var d sdp.SessionDescription
		if err := d.UnmarshalString(got); err != nil {
			t.Fatalf("%s: the den can't read the answer: %v\n%s", name, err, got)
		}
		addrs := []string{d.Origin.UnicastAddress}
		conns := []*sdp.ConnectionInformation{d.ConnectionInformation}
		for _, m := range d.MediaDescriptions {
			conns = append(conns, m.ConnectionInformation)
			for _, a := range m.Attributes {
				switch a.Key {
				case "candidate", "end-of-candidates":
					t.Errorf("%s: a candidate is left: %s", name, a)
				case "rtcp":
					if f := strings.Fields(a.Value); len(f) == 4 {
						addrs = append(addrs, f[3])
					}
				}
			}
		}
		for _, c := range conns {
			if c != nil && c.Address != nil {
				addrs = append(addrs, c.Address.Address)
			}
		}
		for _, a := range addrs {
			if ip, err := netip.ParseAddr(a); err != nil || !ip.IsUnspecified() {
				t.Errorf("%s: the answer names %q", name, a)
			}
		}
		for _, a := range []string{"192.0.2.10", "2001:db8::7", "127.0.0.1"} {
			if strings.Contains(got, a) {
				t.Errorf("%s: %s is left in the answer:\n%s", name, a, got)
			}
		}
		if again := StripCandidates(got); again != got {
			t.Errorf("%s: stripping twice changed the answer", name)
		}
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

// withVideo is the test offer with a video section added, which maps its
// formats as rtpmaps says.
func withVideo(formats string, rtpmaps ...string) string {
	lines := append([]string{"m=video 9 UDP/TLS/RTP/SAVPF " + formats, "c=IN IP4 0.0.0.0", "a=mid:2"}, rtpmaps...)
	return testOffer + sdpLines(append(lines, "a=recvonly")...)
}

func TestCheckOffer(t *testing.T) {
	for name, offer := range map[string]string{
		"audio": testOffer,
		"VP9":   withVideo("98", "a=rtpmap:98 VP9/90000"),
		"AV1":   withVideo("45", "a=rtpmap:45 AV1/90000"),
		"both":  withVideo("98 45", "a=rtpmap:98 vp9/90000", "a=rtpmap:45 AV1/90000"),
	} {
		if err := CheckOffer(offer); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, offer := range map[string]string{
		"H.264":         withVideo("102", "a=rtpmap:102 H264/90000"),
		"VP8 beside":    withVideo("98 96", "a=rtpmap:98 VP9/90000", "a=rtpmap:96 VP8/90000"),
		"an unmapped":   withVideo("98 34", "a=rtpmap:98 VP9/90000"),
		"PCMU in audio": strings.Replace(testOffer, "a=rtpmap:111 opus/48000/2\r\na=sendonly", "a=rtpmap:111 PCMU/8000\r\na=sendonly", 1),
		"data":          strings.Replace(testOffer, "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\na=mid:1", "m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\nc=IN IP4 0.0.0.0\r\na=mid:1", 1),
		"no media":      sdpLines("v=0", "o=- 1 2 IN IP4 0.0.0.0", "s=-", "t=0 0"),
		"malformed":     "not an offer",
		"over limit":    testOffer + strings.Repeat("a=x\r\n", MaxSDP/5),
	} {
		if CheckOffer(offer) == nil {
			t.Errorf("%s: passed", name)
		}
	}
}

func TestCallLimits(t *testing.T) {
	if err := CheckCallLimits(DefaultCallLimits); err != nil {
		t.Fatalf("a new den's limits: %v", err)
	}
	if DefaultCallLimits.ShareViewers*DefaultCallLimits.Shares*DefaultCallLimits.ShareBitrate != 16_000_000 {
		t.Errorf("a new den's shares cost %d bits a second at most, not 16 Mbps", DefaultCallLimits.ShareViewers*DefaultCallLimits.Shares*DefaultCallLimits.ShareBitrate)
	}
	edges := DefaultCallLimits
	edges.Members, edges.Callers, edges.Shares, edges.ShareViewers = MaxCallMembers, MaxDenCallers, 0, MaxCallMembers-1
	edges.ShareBitrate, edges.ShareHeight, edges.ShareFPS = MaxShareBitrate, 2160, MaxShareFPS
	if err := CheckCallLimits(edges); err != nil {
		t.Errorf("limits at the edges of their ranges: %v", err)
	}
	for name, change := range map[string]func(*CallLimits){
		"a call of one":      func(l *CallLimits) { l.Members = 1 },
		"a call too large":   func(l *CallLimits) { l.Members = MaxCallMembers + 1 },
		"too many in calls":  func(l *CallLimits) { l.Callers = MaxDenCallers + 1 },
		"too many shares":    func(l *CallLimits) { l.Shares = MaxShares + 1 },
		"no viewers":         func(l *CallLimits) { l.ShareViewers = 0 },
		"too many viewers":   func(l *CallLimits) { l.ShareViewers = MaxCallMembers },
		"a bitrate too low":  func(l *CallLimits) { l.ShareBitrate = MinShareBitrate - 1 },
		"a bitrate too high": func(l *CallLimits) { l.ShareBitrate = MaxShareBitrate + 1 },
		"an odd size":        func(l *CallLimits) { l.ShareHeight = 1200 },
		"too few frames":     func(l *CallLimits) { l.ShareFPS = MinShareFPS - 1 },
		"too many frames":    func(l *CallLimits) { l.ShareFPS = MaxShareFPS + 1 },
		"none at all":        func(l *CallLimits) { *l = CallLimits{} },
	} {
		l := DefaultCallLimits
		change(&l)
		if CheckCallLimits(l) == nil {
			t.Errorf("%s: passed", name)
		}
	}
}

func TestVoiceBitrate(t *testing.T) {
	for _, b := range []int{MinVoiceBitrate, 24000, DefaultVoiceBitrate, MaxVoiceBitrate} {
		if err := CheckVoiceBitrate(b); err != nil {
			t.Errorf("%d: %v", b, err)
		}
	}
	for _, b := range []int{0, 8000, MaxVoiceBitrate + VoiceBitrateStep, 100000} {
		if CheckVoiceBitrate(b) == nil {
			t.Errorf("%d: passed", b)
		}
	}
}
