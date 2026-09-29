package vault

import (
	"bufio"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// Files are sealed in chunks, so a file of any size streams in and out in
// bounded memory. This is the STREAM construction (Hoang, Reyhanitabar,
// Rogaway and Vizár) over the data key's XChaCha20-Poly1305: a sealed file
// is a version byte and a random nonce prefix, then chunks of up to
// StreamChunk bytes, each sealed with a nonce made of that prefix, the
// chunk's index and a byte that marks the last chunk. Reordered, dropped
// or appended chunks don't open, and neither does a file cut short.
const (
	streamVersion byte = 1
	StreamChunk        = 64 << 10
	streamPrefix       = 16
	streamHeader       = 1 + streamPrefix
)

// SealStream returns a writer that seals what is written to it into w,
// bound to ad, which should name what the file is. Close seals the last
// chunk; a file that wasn't closed doesn't open.
func (v *Vault) SealStream(w io.Writer, ad []byte) (io.WriteCloser, error) {
	s := &sealWriter{aead: v.aead, w: w, ad: append([]byte{}, ad...), buf: make([]byte, 0, StreamChunk)}
	if _, err := rand.Read(s.nonce[:streamPrefix]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	header := append([]byte{streamVersion}, s.nonce[:streamPrefix]...)
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return s, nil
}

type sealWriter struct {
	aead    cipher.AEAD
	w       io.Writer
	ad      []byte
	nonce   [chacha20poly1305.NonceSizeX]byte
	buf     []byte
	out     []byte
	counter uint64
	err     error
	closed  bool
}

func (s *sealWriter) Write(p []byte) (int, error) {
	if s.closed {
		return 0, errors.New("write to a closed sealed file")
	}
	n := 0
	for len(p) > 0 {
		if s.err != nil {
			return n, s.err
		}
		// A full chunk goes out only once more data follows, so the chunk
		// Close seals is always the last.
		if len(s.buf) == StreamChunk {
			s.seal(false)
			continue
		}
		k := copy(s.buf[len(s.buf):StreamChunk], p)
		s.buf = s.buf[:len(s.buf)+k]
		p = p[k:]
		n += k
	}
	return n, s.err
}

func (s *sealWriter) seal(last bool) {
	if s.counter >= 1<<56 {
		s.err = errors.New("the file is too large to seal")
		return
	}
	setChunkNonce(&s.nonce, s.counter, last)
	s.out = s.aead.Seal(s.out[:0], s.nonce[:], s.buf, s.ad)
	s.buf = s.buf[:0]
	s.counter++
	_, s.err = s.w.Write(s.out)
}

// Close seals the last chunk.
func (s *sealWriter) Close() error {
	if s.closed {
		return s.err
	}
	s.closed = true
	if s.err == nil {
		s.seal(true)
	}
	return s.err
}

func setChunkNonce(n *[chacha20poly1305.NonceSizeX]byte, counter uint64, last bool) {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], counter)
	copy(n[streamPrefix:streamPrefix+7], c[1:])
	n[len(n)-1] = 0
	if last {
		n[len(n)-1] = 1
	}
}

// OpenStream returns a reader of what SealStream sealed into r with ad.
// Reads fail with ErrSealed at the first chunk that doesn't open, which
// includes a file that was tampered with, cut short or bound to other
// data; what was read before then is authentic.
func (v *Vault) OpenStream(r io.Reader, ad []byte) io.Reader {
	return &openReader{aead: v.aead, r: bufio.NewReaderSize(r, 4096), ad: append([]byte{}, ad...)}
}

type openReader struct {
	aead    cipher.AEAD
	r       *bufio.Reader
	ad      []byte
	nonce   [chacha20poly1305.NonceSizeX]byte
	counter uint64
	chunk   []byte
	buf     []byte
	started bool
	done    bool
	err     error
}

func (o *openReader) Read(p []byte) (int, error) {
	for len(o.buf) == 0 {
		switch {
		case o.err != nil:
			return 0, o.err
		case o.done:
			return 0, io.EOF
		}
		o.next()
	}
	n := copy(p, o.buf)
	o.buf = o.buf[n:]
	return n, nil
}

func (o *openReader) next() {
	if !o.started {
		o.started = true
		var header [streamHeader]byte
		if _, err := io.ReadFull(o.r, header[:]); err != nil {
			o.err = sealedErr(err)
			return
		}
		if header[0] != streamVersion {
			o.err = ErrSealed
			return
		}
		copy(o.nonce[:streamPrefix], header[1:])
		o.chunk = make([]byte, StreamChunk+o.aead.Overhead())
	}
	n, err := io.ReadFull(o.r, o.chunk)
	last := false
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF):
		last = true // a short chunk ends the file
	case err != nil:
		o.err = sealedErr(err) // no chunk at all: the last one is missing
		return
	default:
		if _, err := o.r.Peek(1); errors.Is(err, io.EOF) {
			last = true
		} else if err != nil {
			o.err = err
			return
		}
	}
	setChunkNonce(&o.nonce, o.counter, last)
	plain, err := o.aead.Open(o.chunk[:0], o.nonce[:], o.chunk[:n], o.ad)
	if err != nil {
		o.err = ErrSealed
		return
	}
	o.buf = plain
	o.counter++
	o.done = last
}

// sealedErr reports running out of data as a file that doesn't open, and
// passes on the reader's own failures.
func sealedErr(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrSealed
	}
	return err
}
