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
		{`{"what": "frozen"}`, "its browser froze it"},
		{`{"what": "resumed", "ago": 2000}`, "its browser thawed it, and it plays the call again (noted 2s before)"},
		{`{"what": "stats", "stats": {"protocol": "udp", "local": "host", "remote": "prflx", "packets_sent": 1500, "bytes_sent": 180000,
			"packets_received": 3000, "bytes_received": 360000, "packets_lost": 4, "jitter": 3, "rtt": 28, "send_rate": 41, "target_rate": 96, "behind": 12}}`,
			"over udp, from a host candidate to a prflx one: sent 1500 packets, 180000 bytes; received 3000 packets, 360000 bytes, 4 lost; jitter 3 ms, round trip 28 ms; " +
				"sending 41 kbit/s of the 96 its encoder aims at; its voice processor 12 ms behind"},
		// Firefox's encoder doesn't say what it aims at, and a call whose
		// processor couldn't run has none to fall behind.
		{`{"what": "stats", "stats": {"protocol": "tcp", "local": "host", "remote": "host", "packets_sent": 10, "bytes_sent": 1000,
			"packets_received": 5, "bytes_received": 500, "packets_lost": 0, "jitter": 10, "rtt": 31, "send_rate": 0, "target_rate": 0, "behind": 0}}`,
			"over tcp, from a host candidate to a host one: sent 10 packets, 1000 bytes; received 5 packets, 500 bytes, 0 lost; jitter 10 ms, round trip 31 ms; " +
				"sending 0 kbit/s; its voice processor 0 ms behind"},
		{`{"what": "stats", "stats": {"protocol": "secret", "local": "192.0.2.5", "remote": "relay", "packets_sent": -5, "bytes_sent": 99999999999999999,
			"packets_received": 0, "bytes_received": 0, "packets_lost": 0, "jitter": 0, "rtt": 0, "address": "192.0.2.5", "send_rate": -3}}`,
			"over ?, from a ? candidate to a relay one: sent 0 packets, 9007199254740992 bytes; received 0 packets, 0 bytes, 0 lost; jitter 0 ms, round trip 0 ms; sending 0 kbit/s"},
		// A sealed call's frames, by count alone (M16.2), on each track,
		// and why the page dropped those it dropped (M18): a watcher whose
		// share's sound came in the clear.
		{`{"what": "stats", "stats": {"protocol": "udp", "local": "host", "remote": "host", "packets_sent": 10, "bytes_sent": 1000,
			"packets_received": 5, "bytes_received": 500, "packets_lost": 0, "jitter": 1, "rtt": 2, "send_rate": 40, "target_rate": 0,
			"frames": {"voice": {"sealed": 500, "opened": 480, "empty": 24, "dropped": {"clear": 0, "lacking": 2, "track": 0, "replay": 1, "unopened": -7}, "key": "secret"},
				"screen": {"sealed": 0, "opened": 900, "dropped": {"clear": 0, "lacking": 0, "track": 0, "replay": 0, "unopened": 0}},
				"sound": {"sealed": 0, "opened": 0, "dropped": {"clear": 450, "lacking": 0, "track": 0, "replay": 0, "unopened": 0, "secret": 3}},
				"unnamed": 2}}}`,
			"over udp, from a host candidate to a host one: sent 10 packets, 1000 bytes; received 5 packets, 500 bytes, 0 lost; jitter 1 ms, round trip 2 ms; " +
				"sending 40 kbit/s; voice frames sealed 500, opened 480, 24 empty, dropped 3 (2 under a key it lacks, 1 at a counter it had); " +
				"screen frames sealed 0, opened 900, dropped 0; sound frames sealed 0, opened 0, dropped 450 (450 in the clear); 2 frames on a receiver it hadn't named"},
		// Every reason, and a call with no frames yet.
		{`{"what": "stats", "stats": {"protocol": "udp", "local": "host", "remote": "host", "packets_sent": 0, "bytes_sent": 0,
			"packets_received": 0, "bytes_received": 0, "packets_lost": 0, "jitter": 0, "rtt": 0, "send_rate": 0, "target_rate": 0,
			"frames": {"screen": {"dropped": {"clear": 1, "lacking": 2, "track": 3, "replay": 4, "unopened": 5}}}}}`,
			"over udp, from a host candidate to a host one: sent 0 packets, 0 bytes; received 0 packets, 0 bytes, 0 lost; jitter 0 ms, round trip 0 ms; " +
				"sending 0 kbit/s; screen frames sealed 0, opened 0, dropped 15 (1 in the clear, 2 under a key it lacks, 3 on another track, 4 at a counter it had, 5 that didn't open)"},
		{`{"what": "stats", "stats": {"protocol": "udp", "local": "host", "remote": "host", "packets_sent": 0, "bytes_sent": 0,
			"packets_received": 0, "bytes_received": 0, "packets_lost": 0, "jitter": 0, "rtt": 0, "send_rate": 0, "target_rate": 0, "frames": {}}}`,
			"over udp, from a host candidate to a host one: sent 0 packets, 0 bytes; received 0 packets, 0 bytes, 0 lost; jitter 0 ms, round trip 0 ms; " +
				"sending 0 kbit/s; no frames yet"},
		// A sender the browser sent around its seal, which the page stopped
		// (M18).
		{`{"what": "unsealed", "track": "sound", "sent": 50}`, "its browser sent its share's sound unsealed, 50 packets, so it stopped the sound"},
		{`{"what": "unsealed", "track": "screen", "sent": 31}`, "its browser sent its screen unsealed, 31 frames, so it ended the share"},
		{`{"what": "unsealed", "track": "voice", "sent": -4}`, "its browser sent its voice unsealed, 0 packets, so it left the call"},
		{`{"what": "unsealed", "track": "a secret", "sent": 4}`, "its browser sent ? unsealed"},
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
