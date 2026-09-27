package vault

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Password length limits, in characters.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 1024
)

// ErrWrongPassword reports a password that doesn't unwrap the data key.
var ErrWrongPassword = errors.New("wrong password")

// Argon2id cost for new wraps. Existing wraps keep the parameters they were
// made with, so these can rise later without breaking old backups.
var (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // KiB
	argonThreads uint8  = 4
)

const passwordWrapAD = "dens-vault-password-wrap-v1"

// PasswordWrap is the data key sealed under a key derived from the local
// password. It is stored as JSON in the database and travels with backups.
type PasswordWrap struct {
	Version   int    `json:"v"`
	Time      uint32 `json:"t"`
	MemoryKiB uint32 `json:"m"`
	Threads   uint8  `json:"p"`
	Salt      []byte `json:"salt"`
	Sealed    []byte `json:"sealed"`
}

// ValidatePassword enforces the password length limits.
func ValidatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	if n < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if n > MaxPasswordLength {
		return fmt.Errorf("password must be at most %d characters", MaxPasswordLength)
	}
	return nil
}

// WrapWithPassword seals the data key under password.
func (v *Vault) WrapWithPassword(password string) (PasswordWrap, error) {
	if err := ValidatePassword(password); err != nil {
		return PasswordWrap{}, err
	}
	wrap := PasswordWrap{Version: 1, Time: argonTime, MemoryKiB: argonMemory, Threads: argonThreads}
	wrap.Salt = make([]byte, 16)
	if _, err := rand.Read(wrap.Salt); err != nil {
		return PasswordWrap{}, fmt.Errorf("generate salt: %w", err)
	}
	kek := wrap.derive(password)
	defer clear(kek)
	aead, err := chacha20poly1305.NewX(kek)
	if err != nil {
		return PasswordWrap{}, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return PasswordWrap{}, fmt.Errorf("generate nonce: %w", err)
	}
	sealed := append([]byte{sealVersion}, nonce...)
	wrap.Sealed = aead.Seal(sealed, nonce, v.key, []byte(passwordWrapAD))
	return wrap, nil
}

// UnwrapWithPassword returns the data key sealed in wrap. The caller must
// clear the returned key when done with it.
func UnwrapWithPassword(wrap PasswordWrap, password string) ([]byte, error) {
	if err := wrap.validate(); err != nil {
		return nil, err
	}
	kek := wrap.derive(password)
	defer clear(kek)
	aead, err := chacha20poly1305.NewX(kek)
	if err != nil {
		return nil, err
	}
	key, err := openWith(aead, wrap.Sealed, []byte(passwordWrapAD))
	if err != nil {
		return nil, ErrWrongPassword
	}
	if len(key) != KeySize {
		clear(key)
		return nil, fmt.Errorf("unwrapped key is %d bytes", len(key))
	}
	return key, nil
}

// VerifyPassword reports whether password unwraps this vault's own key.
func (v *Vault) VerifyPassword(wrap PasswordWrap, password string) error {
	key, err := UnwrapWithPassword(wrap, password)
	if err != nil {
		return err
	}
	defer clear(key)
	if !v.Matches(key) {
		return ErrWrongKey
	}
	return nil
}

func (w PasswordWrap) derive(password string) []byte {
	return argon2.IDKey([]byte(password), w.Salt, w.Time, w.MemoryKiB, w.Threads, KeySize)
}

// validate bounds the parameters an attacker-supplied backup could use to
// make unwrapping arbitrarily expensive.
func (w PasswordWrap) validate() error {
	switch {
	case w.Version != 1:
		return fmt.Errorf("unsupported password wrap version %d", w.Version)
	case w.Time < 1 || w.Time > 16:
		return fmt.Errorf("password wrap time cost %d is out of range", w.Time)
	case w.MemoryKiB < 8*1024 || w.MemoryKiB > 1024*1024:
		return fmt.Errorf("password wrap memory cost %d KiB is out of range", w.MemoryKiB)
	case w.Threads < 1 || w.Threads > 16:
		return fmt.Errorf("password wrap parallelism %d is out of range", w.Threads)
	case len(w.Salt) < 16 || len(w.Salt) > 64:
		return errors.New("password wrap salt has the wrong length")
	}
	return nil
}

// MarshalWrap and UnmarshalWrap are the database encoding of a wrap.
func MarshalWrap(w PasswordWrap) (string, error) {
	data, err := json.Marshal(w)
	return string(data), err
}

func UnmarshalWrap(data string) (PasswordWrap, error) {
	var w PasswordWrap
	if err := json.Unmarshal([]byte(data), &w); err != nil {
		return PasswordWrap{}, fmt.Errorf("decode password wrap: %w", err)
	}
	return w, w.validate()
}
