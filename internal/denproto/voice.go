package denproto

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/pion/sdp/v3"
)

// Client frames and den events for calls (M2). All are ephemeral.
const (
	EventVoiceJoin   = "voice.join"   // a client frame
	EventVoiceAnswer = "voice.answer" // a client frame
	EventVoiceMute   = "voice.mute"   // a client frame
	EventVoiceLeave  = "voice.leave"  // a client frame
	EventVoiceOffer  = "voice.offer"
	EventVoiceEnded  = "voice.ended"
	EventVoiceState  = "voice.state"
)

// Limits on calls.
const (
	MaxCallMembers = 15       // members in one voice channel's call
	MaxDenCallers  = 30       // members in calls across a den
	MaxSDP         = 32 << 10 // bytes in an offer or an answer
)

// Reasons a call ends, in voice.ended.
const (
	VoiceMoved       = "moved"     // the member joined another call, or this one elsewhere
	VoiceNotFound    = "not_found" // no voice channel the member can see has that ID
	VoiceFull        = "full"
	VoiceRateLimited = "rate_limited"
	VoiceForbidden   = "forbidden" // the member can no longer see the channel
	VoiceDeleted     = "deleted"   // the channel was deleted
	VoiceFailed      = "failed"    // the connection didn't come up, or broke
)

type VoiceJoin struct {
	ChannelID string `json:"channel_id"`
	Muted     bool   `json:"muted,omitempty"`
}

type VoiceAnswer struct {
	Version int    `json:"version"`
	SDP     string `json:"sdp"`
}

type VoiceMute struct {
	Muted bool `json:"muted"`
}

// VoiceOffer is the den's offer for a call. Its SDP carries no
// candidates: the client writes the den's, at UDPPort and TCPPort.
type VoiceOffer struct {
	ChannelID string `json:"channel_id"`
	Version   int    `json:"version"`
	SDP       string `json:"sdp"`
	UDPPort   int    `json:"udp_port"`
	TCPPort   int    `json:"tcp_port"`
}

type VoiceEnded struct {
	ChannelID string `json:"channel_id"`
	Reason    string `json:"reason"`
}

// Call is who is in a voice channel's call, in the order they joined.
type Call struct {
	ChannelID string       `json:"channel_id"`
	Members   []CallMember `json:"members"`
}

type CallMember struct {
	ID    string `json:"id"`
	Muted bool   `json:"muted,omitempty"`
}

// VoiceState gives the members of each call that changed. Full replaces
// every call the client held.
type VoiceState struct {
	Calls []Call `json:"calls"`
	Full  bool   `json:"full,omitempty"`
}

// StripCandidates takes every candidate out of an SDP. Neither side sends
// any: the den answers checks from wherever they come, and the client
// writes the den's itself.
func StripCandidates(s string) string {
	lines := strings.Split(s, "\r\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "a=candidate:") || line == "a=end-of-candidates" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\r\n")
}

// CheckOffer checks an offer from a den before a browser sees it: within
// MaxSDP, and audio only.
func CheckOffer(s string) error {
	if len(s) > MaxSDP {
		return fmt.Errorf("the offer is larger than %d bytes", MaxSDP)
	}
	var d sdp.SessionDescription
	if err := d.UnmarshalString(s); err != nil {
		return fmt.Errorf("the offer is malformed: %w", err)
	}
	if len(d.MediaDescriptions) == 0 {
		return errors.New("the offer has no media")
	}
	for _, m := range d.MediaDescriptions {
		if m.MediaName.Media != "audio" {
			return fmt.Errorf("the offer has %q media; calls carry audio only", m.MediaName.Media)
		}
	}
	return nil
}

// Candidate priorities: UDP before the TCP fallback, and within each, the
// addresses in the order given.
const (
	udpPriority = 2130706431
	tcpPriority = 1671430143
)

// AddCandidates writes the den's candidates into an offer that has none:
// each address, at udpPort over UDP and tcpPort over TCP. They go in the
// first media section, whose transport the bundle shares.
func AddCandidates(s string, addrs []netip.Addr, udpPort, tcpPort int) (string, error) {
	if len(addrs) == 0 {
		return "", errors.New("no address to reach the den at")
	}
	if udpPort < 1 || udpPort > 65535 || tcpPort < 1 || tcpPort > 65535 {
		return "", errors.New("the den gave media ports out of range")
	}
	var cands []string
	for i, a := range addrs {
		ip := a.Unmap().WithZone("").String()
		cands = append(cands, fmt.Sprintf("a=candidate:%d 1 udp %d %s %d typ host", 2*i+1, udpPriority-i, ip, udpPort))
	}
	for i, a := range addrs {
		ip := a.Unmap().WithZone("").String()
		cands = append(cands, fmt.Sprintf("a=candidate:%d 1 tcp %d %s %d typ host tcptype passive", 2*i+2, tcpPriority-i, ip, tcpPort))
	}
	cands = append(cands, "a=end-of-candidates")
	lines := strings.Split(s, "\r\n")
	first := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "m=") {
			if first >= 0 {
				return insertAt(lines, i, cands), nil
			}
			first = i
		}
	}
	if first < 0 {
		return "", errors.New("the offer has no media")
	}
	// The first section is the last: its lines end where the SDP's
	// trailing line break leaves an empty one.
	end := len(lines)
	if end > 0 && lines[end-1] == "" {
		end--
	}
	return insertAt(lines, end, cands), nil
}

func insertAt(lines []string, at int, more []string) string {
	out := make([]string, 0, len(lines)+len(more))
	out = append(out, lines[:at]...)
	out = append(out, more...)
	out = append(out, lines[at:]...)
	return strings.Join(out, "\r\n")
}
