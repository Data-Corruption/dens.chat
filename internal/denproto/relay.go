package denproto

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

// Relay (M20).
//
// A member's installs link straight across their network, and the den only
// introduces them: relay.signal carries a box from one of the member's
// devices to their others, or to one by its key ID, which the den passes to
// that member's sockets alone and keeps nothing of. A box is sealed under a
// key made from the member's DM seal by HKDF, salted with the den's ID
// under a label of its own, as the notes' key is, and bound to the den, the
// member, the device it came from and the one it's for, or none for every
// other. Every box is one size, so the den learns only that two of a
// member's devices spoke, and when. What a box holds is the installs' own
// business (internal/relay).

// EventRelaySignal is a box between a member's devices: a client frame,
// and the event the den passes it on as.
const EventRelaySignal = "relay.signal"

// RelayBoxSize is every box's size, sealed.
const RelayBoxSize = 1024

// MaxRelayNote is the most a box holds: its size less the seal's nonce
// and tag, and the note's length.
const MaxRelayNote = RelayBoxSize - chacha20poly1305.NonceSizeX - chacha20poly1305.Overhead - 2

// RelaySignalsPer10s is how many boxes a member's Dens may send the den in
// ten seconds, together; the den drops the rest.
const RelaySignalsPer10s = 30

const (
	relayKeyContext = "dens-relay-v1"
	relayBoxContext = "dens-relay-box-v1"
)

// RelaySignal is a box from a member's Dens: for the device whose key ID To
// names, or with none for each of the member's other devices.
type RelaySignal struct {
	To  Bytes `json:"to,omitempty"`
	Box Bytes `json:"box"`
}

// RelaySignalEvent is a box the den passes on: the key ID of the device it
// came from, and of the one it's for, if the box named one.
type RelaySignalEvent struct {
	From Bytes `json:"from"`
	To   Bytes `json:"to,omitempty"`
	Box  Bytes `json:"box"`
}

var errRelayShape = errors.New("a relay box is 1,024 bytes, for no device or one key ID")

// CheckRelaySignal checks a box's shape, which is all the den can check.
func CheckRelaySignal(s RelaySignal) error {
	if len(s.Box) != RelayBoxSize || (len(s.To) != 0 && len(s.To) != IDSize) {
		return errRelayShape
	}
	return nil
}

// CheckRelaySignalEvent checks the shape of a box the den passed on.
func CheckRelaySignalEvent(e RelaySignalEvent) error {
	if len(e.From) != IDSize {
		return errRelayShape
	}
	return CheckRelaySignal(RelaySignal{To: e.To, Box: e.Box})
}

// RelayKey makes the key a member's boxes at a den are sealed with, from
// their seal.
func RelayKey(seal, denID []byte) (Bytes, error) {
	if len(seal) != SealSize || len(denID) != IDSize {
		return nil, errors.New("a relay key comes from a seal and a den's ID")
	}
	return hkdf.Key(sha256.New, seal, denID, relayKeyContext, chacha20poly1305.KeySize)
}

// RelayBoxAD binds a box to the den, the member, the device it came from
// and the one it's for, empty for every other.
func RelayBoxAD(denID []byte, member int64, from, to []byte) []byte {
	return join([]byte(relayBoxContext), denID, u64(member), from, to)
}

// SealRelayBox pads a note to the box's size and seals it.
func SealRelayBox(key, ad, note []byte) (Bytes, error) {
	if len(note) > MaxRelayNote {
		return nil, errors.New("a relay note is too long for its box")
	}
	plain := make([]byte, MaxRelayNote+2)
	defer clear(plain)
	binary.BigEndian.PutUint16(plain, uint16(len(note)))
	copy(plain[2:], note)
	return sealWith(key, plain, ad)
}

// OpenRelayBox opens a box, and returns its note.
func OpenRelayBox(key, ad, box []byte) (Bytes, error) {
	if len(box) != RelayBoxSize {
		return nil, ErrSealedOpen
	}
	plain, err := openWith(key, box, ad)
	if err != nil {
		return nil, ErrSealedOpen
	}
	defer clear(plain)
	n := int(binary.BigEndian.Uint16(plain))
	if n > len(plain)-2 {
		return nil, ErrSealedOpen
	}
	return append(Bytes{}, plain[2:2+n]...), nil
}
