package vault

import (
	"crypto/ed25519"
	"errors"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// SigningKey is an Ed25519 key whose seed lives in locked memory: a den's
// identity key, or a device key for a joined den.
//
// Only the seed is locked. crypto/ed25519 caches expanded keys through weak
// pointers, which can't point outside the Go heap, so each signature expands
// a short-lived copy and clears it. Keys here sign only at sign-in, so the
// cost doesn't matter.
type SigningKey struct {
	pub  ed25519.PublicKey
	mu   sync.Mutex
	seed []byte
	free func()
}

// NewSigningKey copies seed into locked memory. The caller should clear its
// copy.
func NewSigningKey(seed []byte) (*SigningKey, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("an Ed25519 seed is 32 bytes")
	}
	locked, free, err := host.AllocLocked(ed25519.SeedSize)
	if err != nil {
		return nil, err
	}
	copy(locked, seed)
	key := ed25519.NewKeyFromSeed(seed)
	pub := ed25519.PublicKey(append([]byte(nil), key.Public().(ed25519.PublicKey)...))
	clear(key)
	return &SigningKey{pub: pub, seed: locked, free: free}, nil
}

// GenerateSigningKey makes a new random key.
func GenerateSigningKey() (*SigningKey, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	defer clear(priv)
	seed := priv.Seed()
	defer clear(seed)
	return NewSigningKey(seed)
}

// Public returns the public key.
func (k *SigningKey) Public() ed25519.PublicKey { return k.pub }

// Sign signs msg.
func (k *SigningKey) Sign(msg []byte) []byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.seed == nil {
		panic("vault: signing with a closed key")
	}
	key := ed25519.NewKeyFromSeed(k.seed)
	defer clear(key)
	return ed25519.Sign(key, msg)
}

// Seed returns a copy of the seed, for sealing it into the database. The
// caller should clear it.
func (k *SigningKey) Seed() []byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]byte(nil), k.seed...)
}

// Close clears and releases the seed.
func (k *SigningKey) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.free != nil {
		k.free()
		k.free, k.seed = nil, nil
	}
}
