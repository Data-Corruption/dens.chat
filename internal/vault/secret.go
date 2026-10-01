package vault

import (
	"errors"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// Secret is a short secret held in locked memory, such as a DM seal.
type Secret struct {
	mu   sync.Mutex
	b    []byte
	free func()
}

// NewSecret copies b into locked memory. The caller should clear its copy.
func NewSecret(b []byte) (*Secret, error) {
	if len(b) == 0 {
		return nil, errors.New("a secret can't be empty")
	}
	locked, free, err := host.AllocLocked(len(b))
	if err != nil {
		return nil, err
	}
	copy(locked, b)
	return &Secret{b: locked, free: free}, nil
}

// Use calls fn with the secret, which fn must not keep or change.
func (s *Secret) Use(fn func([]byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.b == nil {
		return errors.New("vault: using a closed secret")
	}
	return fn(s.b)
}

// Copy returns a copy of the secret, for sealing it or showing it. The
// caller should clear it.
func (s *Secret) Copy() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b...)
}

// Close clears and releases the secret.
func (s *Secret) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.free != nil {
		s.free()
		s.free, s.b = nil, nil
	}
}

// SealedSize is how long a stream of n bytes is once sealed: its header,
// the bytes, and a tag for each chunk, of which an empty stream has one.
func SealedSize(n int64) int64 {
	chunks := max((n+StreamChunk-1)/StreamChunk, 1)
	return streamHeader + n + chunks*16
}

// OpenedSize is how many bytes a sealed stream of n bytes opens to, and
// false if no sealed stream is that long.
func OpenedSize(n int64) (int64, bool) {
	body := n - streamHeader
	if body < 16 {
		return 0, false
	}
	const sealedChunk = StreamChunk + 16
	chunks := (body + sealedChunk - 1) / sealedChunk
	plain := body - chunks*16
	return plain, plain >= 0 && SealedSize(plain) == n
}
