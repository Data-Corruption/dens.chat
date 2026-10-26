package denclient

import (
	"fmt"
	"slices"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A call's log (M11): what this install logs of the calls it relays,
// naming dens, channels and pages by ID, never by address. The signaling
// goes at debug as it passes, and so do the notes the page that holds a
// call sends of it: what its browser's connection to the den's media
// ports does, and a summary of its statistics. Only a note's own fields
// reach the log, its words only from the lists here, so no text the page
// holds does.

// CallNote is a page's note on its call.
type CallNote struct {
	// What is what happened: "state", the connection changed; "restart",
	// the page asks for an ICE restart; "rejoin", it joins the call again;
	// "ended", it gave the call up; "hidden", its tab was hidden or shown;
	// or "stats", a summary of the connection's statistics.
	What string `json:"what"`
	// State and ICE are the connection's state and ICE's, and Why why the
	// page restarts, joins again or gave up.
	State  string     `json:"state,omitempty"`
	ICE    string     `json:"ice,omitempty"`
	Why    string     `json:"why,omitempty"`
	Hidden bool       `json:"hidden,omitempty"`
	Stats  *CallStats `json:"stats,omitempty"`
	// Ago is how long before it reached the service the page made the
	// note, in milliseconds, for one it kept while its stream was down.
	Ago int64 `json:"ago,omitempty"`
}

// CallStats sums a call's statistics up, from the page's getStats: the
// path's protocol and its candidates' types, packets and bytes each way,
// packets lost on the way in, and jitter and round-trip time in
// milliseconds.
type CallStats struct {
	Protocol        string `json:"protocol"`
	Local           string `json:"local"`
	Remote          string `json:"remote"`
	PacketsSent     int64  `json:"packets_sent"`
	BytesSent       int64  `json:"bytes_sent"`
	PacketsReceived int64  `json:"packets_received"`
	BytesReceived   int64  `json:"bytes_received"`
	PacketsLost     int64  `json:"packets_lost"`
	Jitter          int64  `json:"jitter"`
	RTT             int64  `json:"rtt"`
}

// The words a note's fields may hold: the states of a browser's connection
// and of its ICE, the candidates' protocols and types, and why a page
// restarts, joins again or gives up, as the log says them.
var (
	connectionStates = []string{"new", "connecting", "connected", "disconnected", "failed", "closed"}
	iceStates        = []string{"new", "checking", "connected", "completed", "disconnected", "failed", "closed"}
	protocols        = []string{"udp", "tcp"}
	candidateTypes   = []string{"host", "srflx", "prflx", "relay"}
	noteReasons      = map[string]string{
		"disconnected":    "its connection was disconnected",
		"failed":          "its connection failed",
		"resumed":         "the den took the call back while its connection was down",
		"stream":          "its stream to this service was down",
		"no_offer":        "the den sent no offer in time",
		"restart_timeout": "its connection was still down after a restart",
		"resume_failed":   "the den had no call to take back",
		"den_failed":      "the den ended the call as failed",
		"den_away":        "the den was away and the page held no connection",
		"never_connected": "its connection never came up",
		"lost":            "it couldn't join again in time",
		"browser":         "its browser couldn't set the call up",
	}
)

// maxNoteAgo is the most a note's Ago may say: a page keeps notes for as
// long as it rides out a dropped connection.
const maxNoteAgo = 10 * time.Minute

// line is what the log says of a note, or false for one that isn't one.
func (n CallNote) line() (string, bool) {
	var s string
	switch n.What {
	case "state":
		s = fmt.Sprintf("its connection is %s, its ICE %s", word(connectionStates, n.State), word(iceStates, n.ICE))
	case "restart":
		s = "it asks to restart ICE, as " + reason(n.Why)
	case "rejoin":
		s = "it joins again, as " + reason(n.Why)
	case "ended":
		s = "it gave the call up, as " + reason(n.Why)
	case "hidden":
		s = "its tab shows"
		if n.Hidden {
			s = "its tab is hidden"
		}
	case "stats":
		if n.Stats == nil {
			return "", false
		}
		x := n.Stats
		s = fmt.Sprintf("over %s, from a %s candidate to a %s one: sent %d packets, %d bytes; received %d packets, %d bytes, %d lost; jitter %d ms, round trip %d ms",
			word(protocols, x.Protocol), word(candidateTypes, x.Local), word(candidateTypes, x.Remote), count(x.PacketsSent), count(x.BytesSent),
			count(x.PacketsReceived), count(x.BytesReceived), count(x.PacketsLost), count(x.Jitter), count(x.RTT))
	default:
		return "", false
	}
	if ago := time.Duration(n.Ago) * time.Millisecond; ago > 0 && ago <= maxNoteAgo {
		s += fmt.Sprintf(" (noted %s before)", ago.Round(100*time.Millisecond))
	}
	return s, true
}

// word is s if it's among the words, and "?" otherwise.
func word(words []string, s string) string {
	if slices.Contains(words, s) {
		return s
	}
	return "?"
}

func reason(why string) string {
	if s, ok := noteReasons[why]; ok {
		return s
	}
	return "?"
}

// count keeps a number a page sends within what it can mean.
func count(n int64) int64 { return min(max(n, 0), 1<<53) }

// NoteCall logs a page's note on its call in a den's channel, at debug. It
// logs a note the page kept while the service had no call for it, as while
// the den was away, so it names the call itself; only a den this install
// joined, and a channel's ID, reach the log.
func (m *Manager) NoteCall(page, denID, channel string, n CallNote) {
	s, ok := n.line()
	if !ok || !validID(channel) {
		return
	}
	c, err := m.find(denID)
	if err != nil {
		return
	}
	m.log.Debugf("Den %s, call in channel %s, page %s: %s", denproto.Fingerprint(c.j.denID), channel, page, s)
}
