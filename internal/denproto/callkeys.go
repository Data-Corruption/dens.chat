package denproto

import (
	"crypto/ed25519"
	"crypto/hpke"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

// Call keys (M16.2).
//
// Each member of a call seals what their page sends, frame by frame, with
// SFrame (RFC 9605) in its AES-128-GCM suite, under a key of their own for
// the call: 16 random bytes, SFrame's base key, which their Dens makes as
// they join and again whenever anyone joins or leaves. Their Dens seals
// the key to each other member's identity with HPKE, as a DM's key is,
// and signs a statement naming the den, the channel, the key's random ID
// and a commitment to it, and each member it was sealed to, with the
// identity it was sealed to. A call's keys are 16 bytes where a DM's are
// 32, and live only while the call does, so their copies, commitments and
// statements have contexts of their own: none can pass for a DM's.
//
// The den passes each member of the call their own copy, while both are
// in it, and keeps the latest from each while the call lasts, so a member
// gets theirs as their call starts or is taken back. It never holds a key
// that opens one.

const (
	// CallKeySize is a call's key: SFrame's base key for AES-128-GCM.
	CallKeySize = 16
	// CallKeyCopySize is a call key's copy sealed to an identity: X-Wing's
	// encapsulated key, then the key and its tag.
	CallKeyCopySize = 1120 + CallKeySize + 16
	// MaxCallKeyMembers bounds the members a call's key names: everyone in
	// a call but its maker, with room for a den that lets more in later, as
	// clients bound a call's members.
	MaxCallKeyMembers = 999
)

const (
	callKeyCopyContext   = "dens-call-key-copy-v1"
	callKeyCommitContext = "dens-call-key-commit-v1"
	callKeyContext       = "dens-call-key-v1"
)

// EventVoiceKey is a member's key for their call: a client frame, with a
// copy for each other member, and the den's event, with the copy for the
// member it reaches alone.
const EventVoiceKey = "voice.key"

// VoiceUpdateNeeded is a join the den refuses since it doesn't say it
// seals, as from a Dens older than M16.2, whose page would send in the
// clear what every other page drops.
const VoiceUpdateNeeded = "update_needed"

// VoiceKeysPer10s is how many keys for calls a member's Dens may send the
// den in 10 seconds: each join and leave has every member of the call send
// one, and a Dens makes one at most every half second.
const VoiceKeysPer10s = 30

// callKeyCopyInfo binds a call key's copy to the den, the call's channel,
// the key and the member it's for.
func callKeyCopyInfo(denID []byte, channel int64, kid []byte, member int64) []byte {
	return join([]byte(callKeyCopyContext), denID, u64(channel), kid, u64(member))
}

// SealCallKeyCopy seals a call's key to a member's identity, bound to the
// den, the channel, the key and the member.
func SealCallKeyCopy(to Identity, denID []byte, channel int64, kid []byte, member int64, key []byte) (Bytes, error) {
	if len(key) != CallKeySize || len(kid) != KIDSize {
		return nil, errors.New("a call's key is 16 bytes, with an ID of 16")
	}
	pk, err := identityKEM.NewPublicKey(to.KEM)
	if err != nil {
		return nil, errIdentity
	}
	sealed, err := hpke.Seal(pk, identityKDF, identityAEAD, callKeyCopyInfo(denID, channel, kid, member), key)
	if err != nil {
		return nil, err
	}
	if len(sealed) != CallKeyCopySize {
		return nil, fmt.Errorf("a call key's copy is %d bytes, not %d", len(sealed), CallKeyCopySize)
	}
	return sealed, nil
}

// OpenCallKeyCopy opens the member's copy of a call's key.
func (k *IdentityKeys) OpenCallKeyCopy(denID []byte, channel int64, kid []byte, member int64, sealed []byte) (Bytes, error) {
	if len(sealed) != CallKeyCopySize {
		return nil, ErrSealedOpen
	}
	key, err := hpke.Open(k.kem, identityKDF, identityAEAD, callKeyCopyInfo(denID, channel, kid, member), sealed)
	if err != nil || len(key) != CallKeySize {
		return nil, ErrSealedOpen
	}
	return key, nil
}

// CallKeyCommit commits to a call's key: SHA-256("dens-call-key-commit-v1"
// ‖ kid ‖ key), as KeyCommit does to a DM's.
func CallKeyCommit(kid, key []byte) Bytes {
	h := sha256.New()
	h.Write([]byte(callKeyCommitContext))
	h.Write(kid)
	h.Write(key)
	return h.Sum(nil)
}

// CallKeyNumber is the number a call's key goes by in its frames' SFrame
// headers: the first two bytes of its ID. Each of the member's tracks has
// a key ID of its own from it, four times the number and the track's.
func CallKeyNumber(kid []byte) int {
	if len(kid) < 2 {
		return 0
	}
	return int(binary.BigEndian.Uint16(kid))
}

// CallKeyStatement is what a call key's maker signs: the den, the call's
// channel, the key's ID and commitment, who made it, and each member it
// was sealed to, by ID, with the identity it was sealed to. The maker
// isn't among them: their own Dens gives them their key.
type CallKeyStatement struct {
	DenID   []byte
	Channel int64
	KID     []byte
	Commit  []byte
	MadeBy  int64
	Members []StatedMember
}

// bytes is the statement as signed: "dens-call-key-v1" ‖ den_id ‖ channel
// ‖ kid ‖ commit ‖ made_by ‖ len(members) ‖ (member ‖ identity)…, its
// members in the order of their IDs.
func (s CallKeyStatement) bytes() []byte {
	b := join([]byte(callKeyContext), s.DenID, u64(s.Channel), s.KID, s.Commit, u64(s.MadeBy), u64(int64(len(s.Members))))
	for _, m := range s.Members {
		b = append(b, u64(m.Member)...)
		b = append(b, m.Identity...)
	}
	return b
}

// Check checks a statement's shape: its sizes, and its members, each once
// and in order, at least one, its maker not among them.
func (s CallKeyStatement) Check() error {
	if len(s.DenID) != IDSize || len(s.KID) != KIDSize || len(s.Commit) != KeyCommitSize || s.Channel <= 0 || s.MadeBy <= 0 {
		return errors.New("a call key's statement is malformed")
	}
	if len(s.Members) == 0 || len(s.Members) > MaxCallKeyMembers {
		return fmt.Errorf("a call's key names 1 to %d members", MaxCallKeyMembers)
	}
	for i, m := range s.Members {
		if len(m.Identity) != IdentityHashSize || m.Member <= 0 || (i > 0 && m.Member <= s.Members[i-1].Member) {
			return errors.New("a call key's statement names its members oddly")
		}
	}
	if s.Named(s.MadeBy) != nil {
		return errors.New("a call key's maker is among those it's sealed to")
	}
	return nil
}

// Named returns the identity the statement names for a member, or nil.
func (s CallKeyStatement) Named(member int64) []byte {
	i := slices.IndexFunc(s.Members, func(m StatedMember) bool { return m.Member == member })
	if i < 0 {
		return nil
	}
	return s.Members[i].Identity
}

// SignCallKey signs a call key's statement with the identity.
func (k *IdentityKeys) SignCallKey(s CallKeyStatement) Bytes { return ed25519.Sign(k.sig, s.bytes()) }

// VerifyCallKey reports whether a call key's statement's signature checks
// against an identity's signing key.
func VerifyCallKey(sigKey []byte, s CallKeyStatement, signature []byte) bool {
	if len(sigKey) != ed25519.PublicKeySize || len(signature) != SignatureSize || s.Check() != nil {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(sigKey), s.bytes(), signature)
}

// VoiceKey is a member's key for their call, as their Dens sends it: the
// call's channel, the key's ID and commitment, each member it was sealed
// to with their identity's hash, the maker's identity in 64 bytes, from
// which a reader checks the signature, and each member's copy.
type VoiceKey struct {
	ChannelID string      `json:"channel_id"`
	KID       Bytes       `json:"kid"`
	Commit    Bytes       `json:"commit"`
	Members   []KeyMember `json:"members"`
	Maker     KeyMaker    `json:"maker"`
	Signature Bytes       `json:"signature"`
	Copies    []KeyCopy   `json:"copies"`
}

// VoiceKeyEvent is another member's key for the call, as the den passes it
// to a member of it: who made it, its statement and signature, and the
// copy for this member alone.
type VoiceKeyEvent struct {
	ChannelID string      `json:"channel_id"`
	MemberID  string      `json:"member_id"`
	KID       Bytes       `json:"kid"`
	Commit    Bytes       `json:"commit"`
	Members   []KeyMember `json:"members"`
	Maker     KeyMaker    `json:"maker"`
	Signature Bytes       `json:"signature"`
	Copy      Bytes       `json:"copy"`
}

// Statement returns what the key's maker signed, at the den with denID.
func (k VoiceKey) Statement(denID []byte, maker string) (CallKeyStatement, error) {
	return callStatement(denID, k.ChannelID, maker, k.KID, k.Commit, k.Members)
}

// Statement returns what the key's maker signed, at the den with denID.
func (e VoiceKeyEvent) Statement(denID []byte) (CallKeyStatement, error) {
	return callStatement(denID, e.ChannelID, e.MemberID, e.KID, e.Commit, e.Members)
}

// Event is the key as the den passes it to one member: with their copy.
func (k VoiceKey) Event(maker string, copied []byte) VoiceKeyEvent {
	return VoiceKeyEvent{ChannelID: k.ChannelID, MemberID: maker, KID: k.KID, Commit: k.Commit, Members: k.Members, Maker: k.Maker,
		Signature: k.Signature, Copy: copied}
}

// Copy returns the copy a key holds for a member, or nil.
func (k VoiceKey) Copy(member string) Bytes {
	i := slices.IndexFunc(k.Copies, func(c KeyCopy) bool { return c.MemberID == member })
	if i < 0 {
		return nil
	}
	return k.Copies[i].Copy
}

func callStatement(denID []byte, channel, maker string, kid, commit []byte, members []KeyMember) (CallKeyStatement, error) {
	s := CallKeyStatement{DenID: denID, KID: kid, Commit: commit}
	var err error
	if s.Channel, err = ParseID(channel); err != nil {
		return s, errors.New("a call key's channel has an invalid ID")
	}
	if s.MadeBy, err = ParseID(maker); err != nil {
		return s, errors.New("a call key's maker has an invalid ID")
	}
	if len(members) > MaxCallKeyMembers {
		return s, errors.New("a call's key names too many members")
	}
	for _, m := range members {
		id, err := ParseID(m.MemberID)
		if err != nil {
			return s, errors.New("a call key's member has an invalid ID")
		}
		s.Members = append(s.Members, StatedMember{Member: id, Identity: m.Identity})
	}
	return s, s.Check()
}

// checkMaker checks a call key's maker's identity is of the sizes it has,
// and its signature of the size it has.
func checkMaker(m KeyMaker, signature []byte) error {
	if len(m.KEMHash) != IdentityHashSize || len(m.Sig) != PublicKeySize || len(signature) != SignatureSize {
		return errors.New("a call key's maker or signature is malformed")
	}
	return nil
}

// CheckVoiceKey checks a member's key for their call as the den takes it:
// its shape, and a copy of its size for each member it names, and for no
// one else. The den can't check the signature means anything, and needn't:
// each member's Dens checks it against the identity it kept.
func CheckVoiceKey(denID []byte, maker string, k VoiceKey) error {
	s, err := k.Statement(denID, maker)
	if err != nil {
		return err
	}
	if err := checkMaker(k.Maker, k.Signature); err != nil {
		return err
	}
	if len(k.Copies) != len(k.Members) {
		return errors.New("a call's key has a copy for each member it names")
	}
	seen := map[string]bool{}
	for _, c := range k.Copies {
		id, err := ParseID(c.MemberID)
		if err != nil || s.Named(id) == nil || seen[c.MemberID] || len(c.Copy) != CallKeyCopySize {
			return fmt.Errorf("a call's key has a copy of %d bytes for each member it names", CallKeyCopySize)
		}
		seen[c.MemberID] = true
	}
	return nil
}

// CheckVoiceKeyEvent checks another member's key for the call as a client
// takes it from the den, before it checks the signature and opens the
// copy: its shape.
func CheckVoiceKeyEvent(denID []byte, e VoiceKeyEvent) error {
	if _, err := e.Statement(denID); err != nil {
		return err
	}
	if err := checkMaker(e.Maker, e.Signature); err != nil {
		return err
	}
	if len(e.Copy) != CallKeyCopySize {
		return errors.New("a call key's copy is malformed")
	}
	return nil
}
