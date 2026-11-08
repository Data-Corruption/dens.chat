package denclient

import (
	"fmt"
	"slices"
	"strings"
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
	// "frozen" and "resumed", its browser froze the page and thawed it
	// (M12); "stats", a summary of the connection's statistics; or
	// "unsealed", its browser sent one of its senders around its seal, and
	// the page stopped it (M18).
	What string `json:"what"`
	// State and ICE are the connection's state and ICE's, and Why why the
	// page restarts, joins again or gave up.
	State  string     `json:"state,omitempty"`
	ICE    string     `json:"ice,omitempty"`
	Why    string     `json:"why,omitempty"`
	Hidden bool       `json:"hidden,omitempty"`
	Stats  *CallStats `json:"stats,omitempty"`
	// Track is the track an unsealed sender carried, "voice", "screen" or
	// "sound", and Sent what it sent around its seal before the page
	// stopped it: a screen's frames, or a voice's or a sound's packets.
	Track string `json:"track,omitempty"`
	Sent  int64  `json:"sent,omitempty"`
	// Ago is how long before it reached the service the page made the
	// note, in milliseconds, for one it kept while its stream was down.
	Ago int64 `json:"ago,omitempty"`
}

// CallStats sums a call's statistics up, from the page's getStats: the
// path's protocol and its candidates' types, packets and bytes each way,
// packets lost on the way in, and jitter and round-trip time in
// milliseconds. From M12, what the page sent a second since its last
// summary and what its encoder aims at, in kbit/s, the aim 0 where the
// browser doesn't say, and how far its voice processor runs behind the
// clock, in milliseconds, where one runs: together they tell a renderer
// short of CPU, as on a phone off screen, from Opus's rate falling with
// the network's.
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
	SendRate        int64  `json:"send_rate"`
	TargetRate      int64  `json:"target_rate"`
	Behind          *int64 `json:"behind,omitempty"`
	// Frames counts what a sealed call's page did with its frames
	// (M16.2), by count alone, and from M18 on each track.
	Frames *FrameCounts `json:"frames,omitempty"`
}

// FrameCounts are a sealed call's frames since its page joined it, on each
// track (M18): the members' voices, their shares' screens, and the shares'
// sound; and those that reached a receiver the page hadn't named yet,
// which it dropped.
type FrameCounts struct {
	Voice   TrackFrames `json:"voice"`
	Screen  TrackFrames `json:"screen"`
	Sound   TrackFrames `json:"sound"`
	Unnamed int64       `json:"unnamed"`
}

// TrackFrames are a track's frames: those the page sealed and sent, those
// it opened, those that came with nothing in them, as a sender's DTX passes
// a few on, and those it dropped, by why.
type TrackFrames struct {
	Sealed  int64         `json:"sealed"`
	Opened  int64         `json:"opened"`
	Empty   int64         `json:"empty"`
	Dropped DroppedFrames `json:"dropped"`
}

// DroppedFrames are why the page dropped a track's frames: they came in the
// clear, as frames that don't start with a header it can read; under a key
// it lacks; under another track's key ID; at a counter their key had; or
// they didn't open.
type DroppedFrames struct {
	Clear    int64 `json:"clear"`
	Lacking  int64 `json:"lacking"`
	Track    int64 `json:"track"`
	Replay   int64 `json:"replay"`
	Unopened int64 `json:"unopened"`
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
	// Why a page dropped a track's frames, as DroppedFrames lists them.
	dropReasons = []string{"in the clear", "under a key it lacks", "on another track", "at a counter it had", "that didn't open"}
	// What a page stopped as its browser sent it around its seal, and how,
	// by the track the sender carried (M18).
	unsealedTracks = map[string]string{
		"voice":  "its voice unsealed, %d packets, so it left the call",
		"screen": "its screen unsealed, %d frames, so it ended the share",
		"sound":  "its share's sound unsealed, %d packets, so it stopped the sound",
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
	case "frozen":
		s = "its browser froze it"
	case "resumed":
		s = "its browser thawed it, and it plays the call again"
	case "stats":
		if n.Stats == nil {
			return "", false
		}
		x := n.Stats
		s = fmt.Sprintf("over %s, from a %s candidate to a %s one: sent %d packets, %d bytes; received %d packets, %d bytes, %d lost; jitter %d ms, round trip %d ms; sending %d kbit/s",
			word(protocols, x.Protocol), word(candidateTypes, x.Local), word(candidateTypes, x.Remote), count(x.PacketsSent), count(x.BytesSent),
			count(x.PacketsReceived), count(x.BytesReceived), count(x.PacketsLost), count(x.Jitter), count(x.RTT), count(x.SendRate))
		if x.TargetRate > 0 {
			s += fmt.Sprintf(" of the %d its encoder aims at", count(x.TargetRate))
		}
		if x.Behind != nil {
			s += fmt.Sprintf("; its voice processor %d ms behind", count(*x.Behind))
		}
		if f := x.Frames; f != nil {
			s += f.line()
		}
	case "unsealed":
		what, ok := unsealedTracks[n.Track]
		if !ok {
			s = "its browser sent ? unsealed"
			break
		}
		s = "its browser sent " + fmt.Sprintf(what, count(n.Sent))
	default:
		return "", false
	}
	if ago := time.Duration(n.Ago) * time.Millisecond; ago > 0 && ago <= maxNoteAgo {
		s += fmt.Sprintf(" (noted %s before)", ago.Round(100*time.Millisecond))
	}
	return s, true
}

// line is what the log says of a call's frames: each track's that the page
// counted any of, with why it dropped those it dropped, and those on a
// receiver it hadn't named.
func (f FrameCounts) line() string {
	var s string
	for _, t := range []struct {
		name string
		n    TrackFrames
	}{{"voice", f.Voice}, {"screen", f.Screen}, {"sound", f.Sound}} {
		d := t.n.Dropped
		var dropped int64
		var reasons []string
		for i, n := range []int64{d.Clear, d.Lacking, d.Track, d.Replay, d.Unopened} {
			if n = count(n); n > 0 {
				dropped += n
				reasons = append(reasons, fmt.Sprintf("%d %s", n, dropReasons[i]))
			}
		}
		sealed, opened, empty := count(t.n.Sealed), count(t.n.Opened), count(t.n.Empty)
		if sealed == 0 && opened == 0 && empty == 0 && dropped == 0 {
			continue
		}
		s += fmt.Sprintf("; %s frames sealed %d, opened %d", t.name, sealed, opened)
		if empty > 0 {
			s += fmt.Sprintf(", %d empty", empty)
		}
		s += fmt.Sprintf(", dropped %d", count(dropped))
		if len(reasons) > 0 {
			s += " (" + strings.Join(reasons, ", ") + ")"
		}
	}
	if n := count(f.Unnamed); n > 0 {
		s += fmt.Sprintf("; %d frames on a receiver it hadn't named", n)
	}
	if s == "" {
		return "; no frames yet"
	}
	return s
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
	// A sender its browser sent around its seal sent part of the call in
	// the clear, which the log keeps at warn (M18).
	logf := m.log.Debugf
	if n.What == "unsealed" {
		logf = m.log.Warnf
	}
	logf("Den %s, call in channel %s, page %s: %s", denproto.Fingerprint(c.j.denID), channel, page, s)
}
