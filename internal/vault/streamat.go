package vault

import (
	"crypto/cipher"
	"errors"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// StreamReaderAt reads a sealed file at any offset, opening only the chunks
// a read covers: each chunk's nonce comes from its index, so none depends
// on the ones before it. A read that reaches the last chunk opens it as the
// last, so a file cut short still doesn't open. Videos play this way, from
// wherever the player seeks.
type StreamReaderAt struct {
	aead   cipher.AEAD
	r      io.ReaderAt
	ad     []byte
	nonce  [chacha20poly1305.NonceSizeX]byte
	chunks int64
	size   int64
	sealed int64

	mu     sync.Mutex
	cached int64 // the chunk in plain, or -1
	plain  []byte
	buf    []byte
}

// OpenStreamAt opens a file SealStream sealed, of sealedSize bytes, for
// reading at any offset.
func (v *Vault) OpenStreamAt(r io.ReaderAt, sealedSize int64, ad []byte) (*StreamReaderAt, error) {
	return newStreamReaderAt(v.aead, r, sealedSize, ad)
}

// OpenStreamAtWith opens a file SealStreamWith sealed under key, as
// OpenStreamAt does.
func OpenStreamAtWith(key []byte, r io.ReaderAt, sealedSize int64, ad []byte) (*StreamReaderAt, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return newStreamReaderAt(aead, r, sealedSize, ad)
}

// SealedChunk is a sealed chunk's size on disk, its tag included.
const SealedChunk = StreamChunk + chacha20poly1305.Overhead

func newStreamReaderAt(aead cipher.AEAD, r io.ReaderAt, sealedSize int64, ad []byte) (*StreamReaderAt, error) {
	size, ok := OpenedSize(sealedSize)
	if !ok {
		return nil, ErrSealed
	}
	var header [streamHeader]byte
	if _, err := r.ReadAt(header[:], 0); err != nil {
		return nil, sealedErr(err)
	}
	if header[0] != streamVersion {
		return nil, ErrSealed
	}
	s := &StreamReaderAt{
		aead: aead, r: r, ad: append([]byte{}, ad...), size: size, sealed: sealedSize,
		chunks: (sealedSize - streamHeader + SealedChunk - 1) / SealedChunk, cached: -1,
	}
	copy(s.nonce[:streamPrefix], header[1:])
	return s, nil
}

// Size is the file's plain size.
func (s *StreamReaderAt) Size() int64 { return s.size }

// ReadAt reads plain bytes at off, as io.ReaderAt does.
func (s *StreamReaderAt) ReadAt(p []byte, off int64) (int, error) {
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
		chunk := off / StreamChunk
		if err := s.open(chunk); err != nil {
			return n, err
		}
		k := copy(p, s.plain[off-chunk*StreamChunk:])
		p, off, n = p[k:], off+int64(k), n+k
	}
	return n, nil
}

// open reads and opens one chunk into s.plain.
func (s *StreamReaderAt) open(chunk int64) error {
	if s.cached == chunk {
		return nil
	}
	at := streamHeader + chunk*SealedChunk
	length := min(int64(SealedChunk), s.sealed-at)
	if cap(s.buf) < SealedChunk {
		s.buf = make([]byte, SealedChunk)
	}
	sealed := s.buf[:length]
	if _, err := s.r.ReadAt(sealed, at); err != nil && !(errors.Is(err, io.EOF) && int64(len(sealed)) == length) {
		return sealedErr(err)
	}
	setChunkNonce(&s.nonce, uint64(chunk), chunk == s.chunks-1)
	plain, err := s.aead.Open(s.plain[:0], s.nonce[:], sealed, s.ad)
	if err != nil {
		s.cached = -1
		return ErrSealed
	}
	s.plain, s.cached = plain, chunk
	return nil
}
