// Package pairing issues the one-time tokens that pair a browser with the
// service. dens open asks for one over the control endpoint and opens the
// client page with it in the URL fragment; the page exchanges it once for a
// session cookie. Tokens live only in memory.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// TokenLifetime is how long a token may wait to be exchanged.
const TokenLifetime = 2 * time.Minute

// maxPending bounds outstanding tokens; the oldest is dropped past it.
const maxPending = 16

// Store holds outstanding pairing tokens.
type Store struct {
	mu      sync.Mutex
	pending map[[32]byte]pendingToken
	issued  uint64
	now     func() time.Time
}

type pendingToken struct {
	expiry time.Time
	// seq orders tokens by issue; clock readings can tie.
	seq uint64
}

// New returns an empty store.
func New() *Store {
	return &Store{pending: make(map[[32]byte]pendingToken), now: time.Now}
}

// Issue returns a new token.
func (s *Store) Issue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if len(s.pending) >= maxPending {
		var oldest [32]byte
		oldestSeq := ^uint64(0)
		for hash, p := range s.pending {
			if p.seq < oldestSeq {
				oldest, oldestSeq = hash, p.seq
			}
		}
		delete(s.pending, oldest)
	}
	s.issued++
	s.pending[sha256.Sum256([]byte(token))] = pendingToken{expiry: s.now().Add(TokenLifetime), seq: s.issued}
	return token, nil
}

// Redeem consumes token, reporting whether it was outstanding and unexpired.
// A token redeems at most once.
func (s *Store) Redeem(token string) bool {
	hash := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[hash]
	delete(s.pending, hash)
	return ok && s.now().Before(p.expiry)
}

func (s *Store) pruneLocked() {
	now := s.now()
	for hash, p := range s.pending {
		if !now.Before(p.expiry) {
			delete(s.pending, hash)
		}
	}
}
