package vault

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// A Scratch is a temporary file sealed under a key of its own, which lives
// only in memory and only as long as the Scratch does, so a member's file
// being worked on never lies on disk in the clear, and nothing left behind
// by a crash opens. Unlike a sealed stream it reads and writes at any offset,
// as a muxer does when it goes back to fill in sizes or moves an index to
// the front: it's stored in blocks of ScratchBlock bytes, each sealed with a
// nonce of its own and bound to its index, and rewritten whole.
type Scratch struct {
	mu   sync.Mutex
	f    *os.File
	aead cipher.AEAD
	size int64

	// One block stays in plain, so a run of small writes to it seals it
	// once.
	cur   int64
	plain []byte
	dirty bool
	buf   []byte
}

// ScratchBlock is how much of a Scratch is sealed at a time.
const ScratchBlock = 64 << 10

const scratchSealed = chacha20poly1305.NonceSizeX + ScratchBlock + chacha20poly1305.Overhead

// NewScratch creates an empty Scratch in dir, named by pattern as
// os.CreateTemp names files, so whoever owns dir can tell its leftovers
// from a crash apart.
func NewScratch(dir, pattern string) (*Scratch, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate a scratch key: %w", err)
	}
	aead, err := chacha20poly1305.NewX(key)
	clear(key)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return &Scratch{f: f, aead: aead, cur: -1, plain: make([]byte, ScratchBlock), buf: make([]byte, scratchSealed)}, nil
}

// Size is how much has been written, to the furthest offset.
func (s *Scratch) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

// ReadAt reads plain bytes at off, as io.ReaderAt does. Bytes never written
// below Size read as zeros.
func (s *Scratch) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for len(p) > 0 {
		if off >= s.size {
			return n, io.EOF
		}
		if err := s.load(off / ScratchBlock); err != nil {
			return n, err
		}
		in := off % ScratchBlock
		k := copy(p, s.plain[in:min(int64(ScratchBlock), in+s.size-off)])
		p, off, n = p[k:], off+int64(k), n+k
	}
	return n, nil
}

// WriteAt writes p at off, as io.WriterAt does.
func (s *Scratch) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for len(p) > 0 {
		if err := s.load(off / ScratchBlock); err != nil {
			return n, err
		}
		k := copy(s.plain[off%ScratchBlock:], p)
		s.dirty = true
		p, off, n = p[k:], off+int64(k), n+k
		s.size = max(s.size, off)
	}
	return n, nil
}

// Close removes the file. The key goes with the Scratch.
func (s *Scratch) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.plain)
	err := s.f.Close()
	return errors.Join(err, os.Remove(s.f.Name()))
}

// load brings block i into s.plain, sealing the one there first if it
// changed. A block never written is zeros.
func (s *Scratch) load(i int64) error {
	if s.cur == i {
		return nil
	}
	if err := s.flush(); err != nil {
		return err
	}
	s.cur = -1
	n, err := s.f.ReadAt(s.buf, i*scratchSealed)
	switch {
	case n == 0 && (err == nil || errors.Is(err, io.EOF)):
		clear(s.plain)
	case n == scratchSealed && allZero(s.buf[:chacha20poly1305.NonceSizeX]):
		// A gap a later block left, which the file reads as zeros: no
		// sealed block has an all-zero nonce but by a chance of 2^-192.
		clear(s.plain)
	case n == scratchSealed:
		nonce := s.buf[:chacha20poly1305.NonceSizeX]
		if _, err := s.aead.Open(s.plain[:0], nonce, s.buf[len(nonce):], blockAD(i)); err != nil {
			return ErrSealed
		}
	case err != nil:
		return err
	default:
		return ErrSealed
	}
	s.cur = i
	return nil
}

func (s *Scratch) flush() error {
	if !s.dirty {
		return nil
	}
	nonce := s.buf[:chacha20poly1305.NonceSizeX]
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(s.buf[:len(nonce)], nonce, s.plain, blockAD(s.cur))
	if _, err := s.f.WriteAt(sealed, s.cur*scratchSealed); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func blockAD(i int64) []byte {
	return binary.BigEndian.AppendUint64([]byte("dens-scratch-v1"), uint64(i))
}
