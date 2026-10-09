package denproto

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

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
	// From M3: a client asks for an ICE restart, and the den says it took a
	// held call back.
	EventVoiceRestart = "voice.restart" // a client frame
	EventVoiceResumed = "voice.resumed"
	// From M4.2: a member shares their screen, or watches another's, and
	// the den refuses either.
	EventVoiceShare   = "voice.share" // a client frame
	EventVoiceWatch   = "voice.watch" // a client frame
	EventVoiceRefused = "voice.refused"
)

// MaxSDP bounds an offer or an answer, in bytes.
const MaxSDP = 32 << 10

// MaxWatching is how many shares a member watches at once (M4.2).
const MaxWatching = 4

// CallLimits are a den's limits for calls and screen shares, which its
// owner sets (M4.2): members in one call and in calls across the den,
// members sharing at once and watching one share, and a share's bitrate in
// bits a second, its size as the height of a 16:9 picture, whose pixels it
// may have in any shape, and its frame rate.
type CallLimits struct {
	Members      int `json:"members"`
	Callers      int `json:"callers"`
	Shares       int `json:"shares"`
	ShareViewers int `json:"share_viewers"`
	ShareBitrate int `json:"share_bitrate"`
	ShareHeight  int `json:"share_height"`
	ShareFPS     int `json:"share_fps"`
}

// DefaultCallLimits are a new den's.
var DefaultCallLimits = CallLimits{Members: 15, Callers: 30, Shares: 1, ShareViewers: 8,
	ShareBitrate: 2_000_000, ShareHeight: 1080, ShareFPS: 30}

// The ranges an owner sets the limits in. A call holds at most
// MaxCallMembers so that its offers fit in MaxSDP.
const (
	MaxCallMembers  = 30
	MaxDenCallers   = 100
	MaxShares       = 10
	MinShareBitrate = 250_000
	MaxShareBitrate = 50_000_000
	MinShareFPS     = 5
	MaxShareFPS     = 60
)

// ShareHeights are the sizes a share may have.
var ShareHeights = []int{720, 1080, 1440, 2160}

// CheckCallLimits checks limits an owner sets, or a den sends.
func CheckCallLimits(l CallLimits) error {
	switch {
	case l.Members < 2 || l.Members > MaxCallMembers:
		return fmt.Errorf("a call holds 2 to %d members", MaxCallMembers)
	case l.Callers < 2 || l.Callers > MaxDenCallers:
		return fmt.Errorf("calls across the den hold 2 to %d members", MaxDenCallers)
	case l.Shares < 0 || l.Shares > MaxShares:
		return fmt.Errorf("0 to %d members may share at once", MaxShares)
	case l.ShareViewers < 1 || l.ShareViewers > MaxCallMembers-1:
		return fmt.Errorf("1 to %d members may watch a share", MaxCallMembers-1)
	case l.ShareBitrate < MinShareBitrate || l.ShareBitrate > MaxShareBitrate:
		return fmt.Errorf("a share's bitrate is %d kbps to %d Mbps", MinShareBitrate/1000, MaxShareBitrate/1_000_000)
	case !slices.Contains(ShareHeights, l.ShareHeight):
		return errors.New("a share's size is 720p, 1080p, 1440p or 2160p")
	case l.ShareFPS < MinShareFPS || l.ShareFPS > MaxShareFPS:
		return fmt.Errorf("a share's frame rate is %d to %d", MinShareFPS, MaxShareFPS)
	}
	return nil
}

// A voice channel's bitrate, in bits a second (M4.2): what its call's Opus
// may send, which staff set.
const (
	DefaultVoiceBitrate = 96_000
	MinVoiceBitrate     = 16_000
	MaxVoiceBitrate     = 128_000
	VoiceBitrateStep    = 8_000
)

// CheckVoiceBitrate checks a voice channel's bitrate.
func CheckVoiceBitrate(b int) error {
	if b < MinVoiceBitrate || b > MaxVoiceBitrate || b%VoiceBitrateStep != 0 {
		return fmt.Errorf("a voice channel's bitrate is %d to %d kbps, in steps of %d", MinVoiceBitrate/1000, MaxVoiceBitrate/1000, VoiceBitrateStep/1000)
	}
	return nil
}

// How long the den holds a call whose socket closed, for the member's
// device to take back (M3), and how long it remembers why a held call
// ended, for that device's resume.
const (
	CallHold          = 30 * time.Second
	CallEndRemembered = 10 * time.Minute
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
	// VoiceDisconnectedByStaff is staff ending a member's call (M3).
	VoiceDisconnectedByStaff = "disconnected_by_staff"
)

// VoiceJoin starts a call, or with Resume takes back the call the den holds
// for this device (M3).
type VoiceJoin struct {
	ChannelID string `json:"channel_id"`
	Muted     bool   `json:"muted,omitempty"`
	Deafened  bool   `json:"deafened,omitempty"` // M3.3
	Resume    bool   `json:"resume,omitempty"`
}

// VoiceResumed says the den moved a held call to this socket.
type VoiceResumed struct {
	ChannelID string `json:"channel_id"`
}

// VoiceShare starts the member's screen share, with its sound if Sound, or
// with On false ends it (M4.2).
type VoiceShare struct {
	On    bool `json:"on"`
	Sound bool `json:"sound,omitempty"`
}

// VoiceWatch watches the share of another member of the call, or with On
// false stops (M4.2).
type VoiceWatch struct {
	MemberID string `json:"member_id"`
	On       bool   `json:"on"`
}

// What a refusal refuses, and why (M4.2).
const (
	RefusedShare = "share"
	RefusedWatch = "watch"

	RefusedOff        = "off"         // the den allows no shares
	RefusedFull       = "full"        // as many share as the den allows, or watch that share, or the member watches MaxWatching
	RefusedStaffMuted = "staff_muted" // staff muted the member
	RefusedNotSharing = "not_sharing" // that member isn't sharing in this call
)

// VoiceRefused says the den refused a share or a watch, which leaves the
// call as it was (M4.2). MemberID names the share a watch asked for.
type VoiceRefused struct {
	ChannelID string `json:"channel_id"`
	What      string `json:"what"`
	MemberID  string `json:"member_id,omitempty"`
	Reason    string `json:"reason"`
}

// VoiceMuteRequest is staff muting a member in calls, or lifting it.
type VoiceMuteRequest struct {
	Muted bool `json:"muted"`
}

type VoiceAnswer struct {
	Version int    `json:"version"`
	SDP     string `json:"sdp"`
}

// VoiceMute sets the member's own marks in their call: muted, and deafened
// (M3.3), which says they hear nothing of it either.
type VoiceMute struct {
	Muted    bool `json:"muted"`
	Deafened bool `json:"deafened,omitempty"`
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

// CallMember is one member of a call: Muted is their own mark, and
// StaffMuted staff's, which the den enforces (M3). Sharing says they share
// their screen, Sound that the share has sound, and Watching lists the
// members whose shares they watch (M4.2).
type CallMember struct {
	ID         string   `json:"id"`
	Muted      bool     `json:"muted,omitempty"`
	Deafened   bool     `json:"deafened,omitempty"`
	StaffMuted bool     `json:"staff_muted,omitempty"`
	Sharing    bool     `json:"sharing,omitempty"`
	Sound      bool     `json:"sound,omitempty"`
	Watching   []string `json:"watching,omitempty"`
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

// The codecs an offer may carry: Opus for audio, and for video VP9, which
// dens offer, or AV1, which a later den may (M4.2). A browser decodes what
// a den forwards from other members, so it's kept to these.
var offerCodecs = map[string][]string{"audio": {"opus"}, "video": {"vp9", "av1"}}

// CheckOffer checks an offer from a den before a browser sees it: within
// MaxSDP, audio and video only, and only in the codecs dens use.
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
		allowed, ok := offerCodecs[m.MediaName.Media]
		if !ok {
			return fmt.Errorf("the offer has %q media; calls carry audio and video only", m.MediaName.Media)
		}
		// Every format the section names must be mapped to an allowed
		// codec: a browser would read a well-known payload type that isn't
		// mapped as a codec of its own.
		mapped := map[string]bool{}
		for _, a := range m.Attributes {
			if a.Key != "rtpmap" {
				continue
			}
			pt, codec, _ := strings.Cut(a.Value, " ")
			name, _, _ := strings.Cut(codec, "/")
			if !slices.Contains(allowed, strings.ToLower(name)) {
				return fmt.Errorf("the offer has %s in %s; calls carry %s", name, m.MediaName.Media, strings.Join(allowed, " or "))
			}
			mapped[pt] = true
		}
		for _, f := range m.MediaName.Formats {
			if !mapped[f] {
				return fmt.Errorf("the offer has a %s format with no codec named", m.MediaName.Media)
			}
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
