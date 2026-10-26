package denclient

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCallNotesLogTheirFieldsAlone checks what the log says of a page's
// notes on its call (M11): their own fields, words from the lists alone and
// numbers within reason, whatever else the page sends.
func TestCallNotesLogTheirFieldsAlone(t *testing.T) {
	for _, c := range []struct {
		note string
		want string
	}{
		{`{"what": "state", "state": "connected", "ice": "completed"}`, "its connection is connected, its ICE completed"},
		{`{"what": "state", "state": "<b>secret</b>", "ice": "connected", "text": "secret"}`, "its connection is ?, its ICE connected"},
		{`{"what": "restart", "why": "disconnected"}`, "it asks to restart ICE, as its connection was disconnected"},
		{`{"what": "rejoin", "why": "no_offer"}`, "it joins again, as the den sent no offer in time"},
		{`{"what": "rejoin", "why": "a secret"}`, "it joins again, as ?"},
		{`{"what": "ended", "why": "never_connected"}`, "it gave the call up, as its connection never came up"},
		{`{"what": "hidden", "hidden": true}`, "its tab is hidden"},
		{`{"what": "hidden"}`, "its tab shows"},
		{`{"what": "stats", "stats": {"protocol": "udp", "local": "host", "remote": "prflx", "packets_sent": 1500, "bytes_sent": 180000,
			"packets_received": 3000, "bytes_received": 360000, "packets_lost": 4, "jitter": 3, "rtt": 28}}`,
			"over udp, from a host candidate to a prflx one: sent 1500 packets, 180000 bytes; received 3000 packets, 360000 bytes, 4 lost; jitter 3 ms, round trip 28 ms"},
		{`{"what": "stats", "stats": {"protocol": "secret", "local": "192.0.2.5", "remote": "relay", "packets_sent": -5, "bytes_sent": 99999999999999999,
			"packets_received": 0, "bytes_received": 0, "packets_lost": 0, "jitter": 0, "rtt": 0, "address": "192.0.2.5"}}`,
			"over ?, from a ? candidate to a relay one: sent 0 packets, 9007199254740992 bytes; received 0 packets, 0 bytes, 0 lost; jitter 0 ms, round trip 0 ms"},
		{`{"what": "state", "state": "disconnected", "ice": "disconnected", "ago": 1500}`, "its connection is disconnected, its ICE disconnected (noted 1.5s before)"},
		{`{"what": "state", "state": "failed", "ice": "failed", "ago": 3600000}`, "its connection is failed, its ICE failed"},
		{`{"what": "state", "state": "failed", "ice": "failed", "ago": -20}`, "its connection is failed, its ICE failed"},
		{`{"what": "stats"}`, ""},
		{`{"what": "anything", "state": "connected"}`, ""},
	} {
		var n CallNote
		if err := json.Unmarshal([]byte(c.note), &n); err != nil {
			t.Fatalf("%s: %v", c.note, err)
		}
		got, ok := n.line()
		if ok != (c.want != "") || got != c.want {
			t.Errorf("%s:\n got %q, %t\nwant %q", c.note, got, ok, c.want)
		}
		if strings.Contains(got, "secret") || strings.Contains(got, "192.0.2") {
			t.Errorf("%s reached the log: %q", c.note, got)
		}
	}
}
