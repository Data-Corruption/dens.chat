package denproto

import (
	"errors"
	"slices"
)

// Identities, signed DM keys and the record (M16.1): what the den relays
// and keeps. The cryptography is in identity.go; the den never holds a key
// that opens any of it.

// Events for M16.1.
const (
	// EventDMSignedKey is a DM key sealed to its members' identities that
	// was made or retired, as the member sees it, with their own copy.
	EventDMSignedKey = "dm.signed_key"
	// EventRecordUpdated is the member's record, written at a new
	// revision, sent to their own sessions.
	EventRecordUpdated = "record.updated"
)

// Limits for M16.1.
const (
	// MaxIdentities bounds the members whose identities one request
	// fetches.
	MaxIdentities = 50
	// MaxKeyMembers bounds the members a key is sealed to.
	MaxKeyMembers = 10
)

// RetiredReplaced is a signed key replaced by a newer one, as after a
// member's identity changed. The den's table keeps it as restarted, since
// a retired key's reason is checked to be one of M1.7's.
const RetiredReplaced = RetiredRestarted

// MemberIdentity is a member's identity, as fetched.
type MemberIdentity struct {
	MemberID string `json:"member_id"`
	KEM      Bytes  `json:"kem"`
	Sig      Bytes  `json:"sig"`
}

// Identity returns the identity's public keys.
func (m MemberIdentity) Identity() Identity { return Identity{KEM: m.KEM, Sig: m.Sig} }

type IdentityList struct {
	Identities []MemberIdentity `json:"identities"`
}

// KeyMember is a member a signed key was sealed to, and the identity it
// was sealed to, by its hash.
type KeyMember struct {
	MemberID string `json:"member_id"`
	Identity Bytes  `json:"identity"`
}

// KeyMaker is the identity that made a key, as the hash of its X-Wing key
// and its signing key, which hash to the identity the key names for its
// maker. A reader checks the key's signature with it, after the maker's
// identity changed too.
type KeyMaker struct {
	KEMHash Bytes `json:"kem_hash"`
	Sig     Bytes `json:"sig"`
}

// SignedKey is a DM key sealed to its members' identities (M16.1), as one
// of its members sees it: the statement its maker signed, the maker's
// keys, and that member's own copy. Messages name the key they're sealed
// with by its ID, which the den gives it; KID is the one its maker gave
// it, which its statement and copies bind. A retired key takes no new
// messages.
type SignedKey struct {
	ID        string      `json:"id"`
	ChannelID string      `json:"channel_id"`
	MadeBy    string      `json:"made_by"`
	MadeAt    int64       `json:"made_at"`
	KID       Bytes       `json:"kid"`
	Commit    Bytes       `json:"commit"`
	Members   []KeyMember `json:"members"`
	Maker     KeyMaker    `json:"maker"`
	Signature Bytes       `json:"signature"`
	Copy      Bytes       `json:"copy,omitempty"`
	RetiredAt int64       `json:"retired_at,omitempty"`
	RetiredBy string      `json:"retired_by,omitempty"`
	Retired   string      `json:"retired,omitempty"`
}

// KeyCopy is a member's copy of a key, sealed to their identity.
type KeyCopy struct {
	MemberID string `json:"member_id"`
	Copy     Bytes  `json:"copy"`
}

// SignedKeyRequest makes a DM's key: its statement, signed by the maker's
// identity, and a copy for each member.
type SignedKeyRequest struct {
	KID       Bytes       `json:"kid"`
	Commit    Bytes       `json:"commit"`
	Members   []KeyMember `json:"members"`
	Signature Bytes       `json:"signature"`
	Copies    []KeyCopy   `json:"copies"`
}

type SignedKeyList struct {
	Keys []SignedKey `json:"keys"`
}

// Statement returns what the key's maker signed, at the den with denID.
func (k SignedKey) Statement(denID []byte) (KeyStatement, error) {
	return statement(denID, k.ChannelID, k.MadeBy, k.KID, k.Commit, k.Members)
}

// Statement returns what a request's maker signed.
func (r SignedKeyRequest) Statement(denID []byte, channel, maker string) (KeyStatement, error) {
	return statement(denID, channel, maker, r.KID, r.Commit, r.Members)
}

func statement(denID []byte, channel, maker string, kid, commit []byte, members []KeyMember) (KeyStatement, error) {
	s := KeyStatement{DenID: denID, KID: kid, Commit: commit}
	var err error
	if s.Channel, err = ParseID(channel); err != nil {
		return s, errors.New("a key's DM has an invalid ID")
	}
	if s.MadeBy, err = ParseID(maker); err != nil {
		return s, errors.New("a key's maker has an invalid ID")
	}
	if len(members) > MaxKeyMembers {
		return s, errors.New("a key names too many members")
	}
	for _, m := range members {
		id, err := ParseID(m.MemberID)
		if err != nil {
			return s, errors.New("a key's member has an invalid ID")
		}
		s.Members = append(s.Members, StatedMember{Member: id, Identity: m.Identity})
	}
	return s, s.Check()
}

// CheckSignedKey checks a signed key from a den before a client uses it:
// its shape, and that its maker's keys are the identity it names for its
// maker. Its signature and copy are checked where it's used.
func CheckSignedKey(denID []byte, k SignedKey) error {
	if _, err := ParseID(k.ID); err != nil {
		return errors.New("a signed key has an invalid ID")
	}
	s, err := k.Statement(denID)
	if err != nil {
		return err
	}
	if len(k.Maker.KEMHash) != IdentityHashSize || len(k.Maker.Sig) != PublicKeySize || len(k.Signature) != SignatureSize ||
		!Equal(IdentityHash(k.Maker.KEMHash, k.Maker.Sig), s.Named(s.MadeBy)) {
		return errors.New("a signed key's maker isn't the identity it names")
	}
	if k.Copy != nil && len(k.Copy) != KeyCopySize {
		return errors.New("a signed key has an invalid copy")
	}
	if (k.RetiredAt == 0) != (k.Retired == "") || (k.RetiredBy != "") != (k.Retired != "") {
		return errors.New("a signed key is retired oddly")
	}
	if k.Retired != "" {
		if _, err := ParseID(k.RetiredBy); err != nil || !slices.Contains([]string{RetiredStartedOver, RetiredReplaced}, k.Retired) {
			return errors.New("a signed key is retired oddly")
		}
	}
	return nil
}

// Record is a member's record as the den keeps it: sealed with their
// seal, at a revision, which is 0 with nothing sealed before the first
// write.
type Record struct {
	Revision int64 `json:"revision"`
	Sealed   Bytes `json:"sealed,omitempty"`
}

// RecordWrite writes the member's record over the revision it was merged
// from.
type RecordWrite struct {
	Revision int64 `json:"revision"`
	Sealed   Bytes `json:"sealed"`
}

// RecordUpdated says the member's record is at a new revision.
type RecordUpdated struct {
	Revision int64 `json:"revision"`
}
