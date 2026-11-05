package denproto

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hpke"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// Identities (M16.1).
//
// Each member has one identity in each den: two key pairs made from their
// DM seal by HKDF, salted with the den's ID, each under a label of its
// own. One is for HPKE (RFC 9180) with X-Wing, X25519 and ML-KEM-768
// together, which keys are sealed to; the other is Ed25519, which signs
// what the member's Dens makes. Every device holding the seal holds the
// identity with nothing new to hand over, and no den sees what the member
// is at another.
//
// A DM's key is 32 random bytes, sealed with HPKE to each member's
// identity, and named by a statement its maker signs: the den, the DM,
// the key's random ID and a commitment to it, and each member with the
// identity it was sealed to. HPKE alone doesn't say who sealed a copy, so
// a reader takes a key only once the signature checks and its copy opens
// to what the statement committed to.
//
// A member's code, 20 digits made from their identity, is what others
// compare with them, when they choose, to check nobody sits between them.

const (
	// IdentityKEMSize is an identity's X-Wing public key.
	IdentityKEMSize = 1216
	// IdentitySize is an identity's two public keys together.
	IdentitySize = IdentityKEMSize + ed25519.PublicKeySize
	// IdentityHashSize is an identity's hash, which names it.
	IdentityHashSize = sha256.Size
	// CodeDigits is a member's code, shown in groups of four.
	CodeDigits = 20
	// KIDSize is the random ID a key's maker gives it, which its
	// statement and every copy of it bind.
	KIDSize = 16
	// KeyCopySize is a DM key's copy sealed to an identity: X-Wing's
	// encapsulated key, then the key and its tag.
	KeyCopySize = 1120 + DMKeySize + 16
	// KeyCommitSize is a key's commitment.
	KeyCommitSize = sha256.Size
)

// codeRounds of SHA-512 make a code, as Signal makes its safety numbers,
// so an identity made to match a code takes about 10^20 tries of them.
const codeRounds = 5200

const (
	identityKEMContext  = "dens-identity-kem-v1"
	identitySigContext  = "dens-identity-sig-v1"
	identityHashContext = "dens-identity-v1"
	codeContext         = "dens-code-v1"
	keyCopyContext      = "dens-key-copy-v1"
	keyCommitContext    = "dens-key-commit-v1"
	signedKeyContext    = "dens-signed-key-v1"
	recordContext       = "dens-record-v1"
)

// The HPKE suite keys are sealed to identities with.
var (
	identityKEM  = hpke.MLKEM768X25519()
	identityKDF  = hpke.HKDFSHA256()
	identityAEAD = hpke.ChaCha20Poly1305()
)

var errIdentity = errors.New("an identity is an X-Wing public key and an Ed25519 one")

// Identity is a member's identity as the den keeps it: its two public
// keys.
type Identity struct {
	KEM Bytes `json:"kem"`
	Sig Bytes `json:"sig"`
}

// Check checks an identity's keys are of their kinds.
func (i Identity) Check() error {
	if len(i.KEM) != IdentityKEMSize || len(i.Sig) != ed25519.PublicKeySize {
		return errIdentity
	}
	if _, err := identityKEM.NewPublicKey(i.KEM); err != nil {
		return errIdentity
	}
	return nil
}

// Hash names the identity.
func (i Identity) Hash() Bytes { return IdentityHash(KEMHash(i.KEM), i.Sig) }

// KEMHash is SHA-256 of an identity's X-Wing key, which stands in for it
// where 1,216 bytes would weigh too much, as in a key's statement.
func KEMHash(kem []byte) Bytes {
	sum := sha256.Sum256(kem)
	return sum[:]
}

// IdentityHash names an identity: SHA-256("dens-identity-v1" ‖
// SHA-256(kem) ‖ sig). Hashing the X-Wing key first lets a key carry its
// maker's identity in 64 bytes, from which a reader checks the hash.
func IdentityHash(kemHash, sig []byte) Bytes {
	h := sha256.New()
	h.Write([]byte(identityHashContext))
	h.Write(kemHash)
	h.Write(sig)
	return h.Sum(nil)
}

// IdentityKeys is a member's identity with its private keys. A Dens makes
// it from the seal as it uses it, and closes it after.
type IdentityKeys struct {
	kem    hpke.PrivateKey
	sig    ed25519.PrivateKey
	public Identity
}

// DeriveIdentity makes a member's identity at a den from their seal.
func DeriveIdentity(seal, denID []byte) (*IdentityKeys, error) {
	if len(seal) != SealSize || len(denID) != IDSize {
		return nil, errors.New("an identity comes from a seal and a den's ID")
	}
	ikm, err := hkdf.Key(sha256.New, seal, denID, identityKEMContext, 32)
	if err != nil {
		return nil, err
	}
	kem, err := identityKEM.DeriveKeyPair(ikm)
	clear(ikm)
	if err != nil {
		return nil, err
	}
	seed, err := hkdf.Key(sha256.New, seal, denID, identitySigContext, ed25519.SeedSize)
	if err != nil {
		return nil, err
	}
	sig := ed25519.NewKeyFromSeed(seed)
	clear(seed)
	return &IdentityKeys{kem: kem, sig: sig,
		public: Identity{KEM: kem.PublicKey().Bytes(), Sig: Bytes(sig.Public().(ed25519.PublicKey))}}, nil
}

// Public returns the identity's public keys.
func (k *IdentityKeys) Public() Identity { return k.public }

// Maker is the identity as a key or a change carries its maker's: the
// hash of its X-Wing key, and its signing key.
func (k *IdentityKeys) Maker() KeyMaker {
	return KeyMaker{KEMHash: KEMHash(k.public.KEM), Sig: k.public.Sig}
}

// Close clears the signing key. The HPKE key's own copies are the
// library's, and go with the garbage collector.
func (k *IdentityKeys) Close() { clear(k.sig) }

// Code is a member's code at a den: 20 digits from 5,200 rounds of SHA-512
// over the den's ID, the member's ID and their identity, each round taking
// the identity again, as Signal makes its safety numbers. Four chunks of
// five bytes, each modulo 100,000, make five digits each, the bias of which
// is under one part in ten million.
func Code(identityHash, denID []byte, member int64) string {
	h := sha512.New()
	h.Write([]byte(codeContext))
	h.Write(denID)
	h.Write(u64(member))
	h.Write(identityHash)
	sum := h.Sum(nil)
	for range codeRounds {
		h.Reset()
		h.Write(sum)
		h.Write(identityHash)
		sum = h.Sum(sum[:0])
	}
	var b strings.Builder
	for i := range 4 {
		chunk := sum[i*5 : i*5+5]
		n := binary.BigEndian.Uint64(append([]byte{0, 0, 0}, chunk...))
		fmt.Fprintf(&b, "%05d", n%100_000)
	}
	return b.String()
}

// FormatCode shows a code in groups of four.
func FormatCode(code string) string { return FormatCheckHalf(code) }

// CodeTyped reports whether digits someone typed, with any spaces or
// dashes, are a code.
func CodeTyped(code, typed string) bool {
	typed = strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(typed)
	return len(code) == CodeDigits && Equal([]byte(typed), []byte(code))
}

// keyCopyInfo binds a key's copy to the den, the DM, the key and the
// member it's for.
func keyCopyInfo(denID []byte, channel int64, kid []byte, member int64) []byte {
	return join([]byte(keyCopyContext), denID, u64(channel), kid, u64(member))
}

// SealKeyCopy seals a DM's key to a member's identity, bound to the den,
// the DM, the key and the member.
func SealKeyCopy(to Identity, denID []byte, channel int64, kid []byte, member int64, key []byte) (Bytes, error) {
	pk, err := identityKEM.NewPublicKey(to.KEM)
	if err != nil {
		return nil, errIdentity
	}
	sealed, err := hpke.Seal(pk, identityKDF, identityAEAD, keyCopyInfo(denID, channel, kid, member), key)
	if err != nil {
		return nil, err
	}
	if len(sealed) != KeyCopySize {
		return nil, fmt.Errorf("a key's copy is %d bytes, not %d", len(sealed), KeyCopySize)
	}
	return sealed, nil
}

// OpenKeyCopy opens the member's copy of a DM's key.
func (k *IdentityKeys) OpenKeyCopy(denID []byte, channel int64, kid []byte, member int64, sealed []byte) (Bytes, error) {
	if len(sealed) != KeyCopySize {
		return nil, ErrSealedOpen
	}
	key, err := hpke.Open(k.kem, identityKDF, identityAEAD, keyCopyInfo(denID, channel, kid, member), sealed)
	if err != nil || len(key) != DMKeySize {
		return nil, ErrSealedOpen
	}
	return key, nil
}

// KeyCommit commits to a key: SHA-256("dens-key-commit-v1" ‖ kid ‖ key).
// Anyone can seal a copy to an identity, so the statement its maker signs
// names the key itself, and a copy that opens to another is refused.
func KeyCommit(kid, key []byte) Bytes {
	h := sha256.New()
	h.Write([]byte(keyCommitContext))
	h.Write(kid)
	h.Write(key)
	return h.Sum(nil)
}

// StatedMember is a member a key was sealed to, with the identity it was
// sealed to.
type StatedMember struct {
	Member   int64
	Identity []byte
}

// KeyStatement is what a key's maker signs: the den, the DM, the key's ID
// and commitment, who made it, and each member it was sealed to, by ID,
// with the identity it was sealed to.
type KeyStatement struct {
	DenID   []byte
	Channel int64
	KID     []byte
	Commit  []byte
	MadeBy  int64
	Members []StatedMember
}

// bytes is the statement as signed: "dens-signed-key-v1" ‖ den_id ‖
// channel ‖ kid ‖ commit ‖ made_by ‖ len(members) ‖ (member ‖
// identity)…, its members in the order of their IDs.
func (s KeyStatement) bytes() []byte {
	b := join([]byte(signedKeyContext), s.DenID, u64(s.Channel), s.KID, s.Commit, u64(s.MadeBy), u64(int64(len(s.Members))))
	for _, m := range s.Members {
		b = append(b, u64(m.Member)...)
		b = append(b, m.Identity...)
	}
	return b
}

// Check checks a statement's shape: its sizes, and its members, each once
// and in order, the maker among them.
func (s KeyStatement) Check() error {
	if len(s.DenID) != IDSize || len(s.KID) != KIDSize || len(s.Commit) != KeyCommitSize || len(s.Members) < 2 {
		return errors.New("a key's statement is malformed")
	}
	for i, m := range s.Members {
		if len(m.Identity) != IdentityHashSize || m.Member <= 0 || (i > 0 && m.Member <= s.Members[i-1].Member) {
			return errors.New("a key's statement names its members oddly")
		}
	}
	if s.Named(s.MadeBy) == nil {
		return errors.New("a key's maker isn't among its members")
	}
	return nil
}

// Named returns the identity the statement names for a member, or nil.
func (s KeyStatement) Named(member int64) []byte {
	i := slices.IndexFunc(s.Members, func(m StatedMember) bool { return m.Member == member })
	if i < 0 {
		return nil
	}
	return s.Members[i].Identity
}

// Sign signs a statement with the identity.
func (k *IdentityKeys) Sign(s KeyStatement) Bytes { return ed25519.Sign(k.sig, s.bytes()) }

// VerifyStatement reports whether a statement's signature checks against
// an identity's signing key.
func VerifyStatement(sigKey []byte, s KeyStatement, signature []byte) bool {
	if len(sigKey) != ed25519.PublicKeySize || len(signature) != SignatureSize || s.Check() != nil {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(sigKey), s.bytes(), signature)
}

// The record (M16.1): the identities a member's Dens kept, and those they
// verified, sealed with their seal and kept on the den, which each of
// their devices writes every hour. It's padded to a power of four from 16
// KiB, so the den learns little from its size.
var recordSizes = []int{16 << 10, 64 << 10, 256 << 10}

// MaxRecord is the most a record holds.
var MaxRecord = recordSizes[len(recordSizes)-1] - 4

// recordOverhead is a sealed record's nonce and tag.
const recordOverhead = chacha20poly1305.NonceSizeX + chacha20poly1305.Overhead

// CheckSealedRecord reports whether a sealed record is of a size a record
// is padded to.
func CheckSealedRecord(sealed []byte) bool {
	return slices.Contains(recordSizes, len(sealed)-recordOverhead)
}

// recordAD binds a sealed record to the den, the member and the revision
// it was written at, so the den can't pass an old one off as new.
func recordAD(denID []byte, member, revision int64) []byte {
	return join([]byte(recordContext), denID, u64(member), u64(revision))
}

// SealRecord pads a record, its length first, and seals it with the
// member's seal for the revision it's written at.
func SealRecord(seal, denID []byte, member, revision int64, record []byte) (Bytes, error) {
	if len(record) > MaxRecord {
		return nil, fmt.Errorf("a record is at most %d bytes", MaxRecord)
	}
	size := recordSizes[slices.IndexFunc(recordSizes, func(n int) bool { return 4+len(record) <= n })]
	plain := make([]byte, size)
	defer clear(plain)
	binary.BigEndian.PutUint32(plain, uint32(len(record)))
	copy(plain[4:], record)
	k, err := sealKey(seal, denID)
	if err != nil {
		return nil, err
	}
	defer clear(k)
	return sealWith(k, plain, recordAD(denID, member, revision))
}

// OpenRecord opens a member's record, as written at a revision.
func OpenRecord(seal, denID []byte, member, revision int64, sealed []byte) ([]byte, error) {
	if !CheckSealedRecord(sealed) {
		return nil, ErrSealedOpen
	}
	k, err := sealKey(seal, denID)
	if err != nil {
		return nil, err
	}
	defer clear(k)
	plain, err := openWith(k, sealed, recordAD(denID, member, revision))
	if err != nil {
		return nil, ErrSealedOpen
	}
	defer clear(plain)
	n := int(binary.BigEndian.Uint32(plain))
	if n > len(plain)-4 {
		return nil, ErrSealedOpen
	}
	return append([]byte(nil), plain[4:4+n]...), nil
}
