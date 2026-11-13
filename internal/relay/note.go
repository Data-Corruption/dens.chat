package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// ID is an install's Relay ID: 16 random bytes made once, by which two
// installs that share more than one den know they're one pair.
type ID [16]byte

// NewID makes a Relay ID.
func NewID() ID {
	var id ID
	_, _ = rand.Read(id[:])
	return id
}

// ParseID reads a Relay ID as the configuration keeps it, in hex.
func ParseID(s string) (ID, error) {
	var id ID
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(id) {
		return id, errors.New("a Relay ID is 32 hex digits")
	}
	copy(id[:], b)
	return id, nil
}

func (id ID) String() string { return hex.EncodeToString(id[:]) }

// short names an install in the log by the start of its Relay ID.
func (id ID) short() string { return id.String()[:8] }

// The kinds of note: who asks the member's other devices to say they're
// here, which here does, with what each is and offers; hello asks for a
// link with this side's half of it, and answer agrees, with the other.
const (
	kindWho    = "who"
	kindHere   = "here"
	kindHello  = "hello"
	kindAnswer = "answer"
)

// What an install offers, and what a link is for: keys, as Keybind relay
// carries them.
const offerKeys = "keys"

// known are the offers this build knows; others, from a newer Dens, it
// leaves out rather than refuse the box.
var known = []string{offerKeys}

// note is what a box between the member's devices holds. At is when it
// was written, which the other side takes within a minute of.
type note struct {
	Kind   string         `json:"kind"`
	ID     denproto.Bytes `json:"id"`
	Name   string         `json:"name"`
	Offers []string       `json:"offers,omitempty"`
	At     int64          `json:"at"`
	// A hello's attempt, which its answer names; what the hello asks the
	// link for; and each side's half.
	Attempt denproto.Bytes `json:"attempt,omitempty"`
	Wants   []string       `json:"wants,omitempty"`
	Half    *half          `json:"half,omitempty"`
}

// half is one side's part of a link: its DTLS certificate's SHA-256, the
// ICE credentials made for the attempt, the UDP port it holds, and its
// addresses on the local network.
type half struct {
	Fingerprint denproto.Bytes `json:"fingerprint"`
	Ufrag       string         `json:"ufrag"`
	Pwd         string         `json:"pwd"`
	Port        int            `json:"port"`
	Addrs       []string       `json:"addrs"`
}

const (
	attemptSize = 8
	// maxAddrs bounds the addresses a half names: a computer's bridges for
	// containers and virtual machines add some.
	maxAddrs = 8
	// stale is how far a note's time may be from the reader's.
	stale = time.Minute
)

// encode writes a note for its box.
func (n note) encode() ([]byte, error) {
	b, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	if len(b) > denproto.MaxRelayNote {
		return nil, errors.New("the note is too long for its box")
	}
	return b, nil
}

// decodeNote reads a note from a box, and checks it: its kind, its ID and
// name, its time within a minute of now, and for a hello or an answer its
// attempt and its half, whose every address must be one allowed says.
// Offers and wants this build doesn't know are left out.
func decodeNote(b []byte, now time.Time, allowed func(netip.Addr) bool) (note, error) {
	var n note
	if err := json.Unmarshal(b, &n); err != nil {
		return n, errors.New("the note doesn't decode")
	}
	if !slices.Contains([]string{kindWho, kindHere, kindHello, kindAnswer}, n.Kind) {
		return n, fmt.Errorf("no kind of note %q", n.Kind)
	}
	if len(n.ID) != len(ID{}) {
		return n, errors.New("the note's Relay ID isn't 16 bytes")
	}
	name, err := denproto.CleanName(n.Name, denproto.MaxLabelRunes)
	if err != nil {
		return n, fmt.Errorf("the note's name: %w", err)
	}
	n.Name = name
	if d := now.Sub(time.UnixMilli(n.At)); d > stale || d < -stale {
		return n, errors.New("the note is stale")
	}
	n.Offers = knownOnly(n.Offers)
	n.Wants = knownOnly(n.Wants)
	if n.Kind != kindHello && n.Kind != kindAnswer {
		return n, nil
	}
	if len(n.Attempt) != attemptSize {
		return n, errors.New("the note names no attempt")
	}
	h := n.Half
	switch {
	case h == nil:
		return n, errors.New("the note has no half of a link")
	case len(h.Fingerprint) != 32:
		return n, errors.New("the half's fingerprint isn't SHA-256's")
	case !iceText(h.Ufrag, 4, 32) || !iceText(h.Pwd, 22, 64):
		return n, errors.New("the half's ICE credentials are malformed")
	case h.Port < 1 || h.Port > 65535:
		return n, errors.New("the half's port is out of range")
	case len(h.Addrs) == 0 || len(h.Addrs) > maxAddrs:
		return n, fmt.Errorf("the half names %d addresses, not 1 to %d", len(h.Addrs), maxAddrs)
	}
	for _, s := range h.Addrs {
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() || !allowed(a) {
			return n, errors.New("the half names an address off the local network")
		}
	}
	return n, nil
}

func knownOnly(list []string) []string {
	var out []string
	for _, x := range list {
		if slices.Contains(known, x) && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// iceText is ICE's characters for credentials, within lengths.
func iceText(s string, least, most int) bool {
	if len(s) < least || len(s) > most {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return false
		}
	}
	return true
}

// privateAddr is an address a half may name: IPv4 on a private network,
// or on loopback where loopback is allowed, as a development instance's.
func privateAddr(loopback bool) func(netip.Addr) bool {
	return func(a netip.Addr) bool {
		return a.Is4() && (a.IsPrivate() || (loopback && a.IsLoopback()))
	}
}
