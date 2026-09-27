// Package vault holds the install's data key and the encryption built on it.
//
// The data key is 32 random bytes generated at install. The service gets it
// unwrapped by the host (systemd-creds or DPAPI) and keeps it in locked
// memory here. Fields are sealed with XChaCha20-Poly1305 under the data key;
// a second copy of the key is wrapped with a key derived from the local
// password (Argon2id) for backups and moving machines.
package vault

import (
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"

	"golang.org/x/crypto/chacha20poly1305"
)

// KeySize is the data key length.
const KeySize = host.HostKeySize

const sealVersion byte = 1

var (
	// ErrSealed reports ciphertext that doesn't open under this key and
	// associated data: tampered, moved between fields, or another key.
	ErrSealed = errors.New("sealed data does not open with this key")
	// ErrWrongKey reports a data key that doesn't match the database's check value.
	ErrWrongKey = errors.New("data key does not match this database")
)

// Vault holds the data key in locked memory for the life of the service.
type Vault struct {
	key  []byte
	free func()
	aead cipher.AEAD
}

// New copies key into locked memory. The caller should clear its copy.
func New(key []byte) (*Vault, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("data key is %d bytes, want %d", len(key), KeySize)
	}
	locked, free, err := host.AllocLocked(KeySize)
	if err != nil {
		return nil, err
	}
	copy(locked, key)
	aead, err := chacha20poly1305.NewX(locked)
	if err != nil {
		free()
		return nil, err
	}
	return &Vault{key: locked, free: free, aead: aead}, nil
}

// Close clears and releases the key. The vault is unusable afterwards.
func (v *Vault) Close() {
	if v == nil || v.free == nil {
		return
	}
	v.aead = nil
	v.free()
	v.free = nil
	v.key = nil
}

// Seal encrypts plaintext bound to ad, which should name where the value
// lives (table, column and row) so a sealed value can't be moved elsewhere.
// The result is version || nonce || ciphertext.
func (v *Vault) Seal(plaintext, ad []byte) ([]byte, error) {
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+v.aead.Overhead())
	out = append(out, sealVersion)
	out = append(out, nonce...)
	return v.aead.Seal(out, nonce, plaintext, ad), nil
}

// Open decrypts a value from Seal with the same associated data.
func (v *Vault) Open(sealed, ad []byte) ([]byte, error) {
	return openWith(v.aead, sealed, ad)
}

func openWith(aead cipher.AEAD, sealed, ad []byte) ([]byte, error) {
	if len(sealed) < 1+chacha20poly1305.NonceSizeX+aead.Overhead() || sealed[0] != sealVersion {
		return nil, ErrSealed
	}
	nonce := sealed[1 : 1+chacha20poly1305.NonceSizeX]
	plain, err := aead.Open(nil, nonce, sealed[1+chacha20poly1305.NonceSizeX:], ad)
	if err != nil {
		return nil, ErrSealed
	}
	return plain, nil
}

// CheckValue identifies the data key without revealing it. The database
// stores it so a mismatched host key (a restore gone wrong, a copied data
// directory) is refused at startup instead of producing unreadable data.
func (v *Vault) CheckValue() []byte { return checkValue(v.key) }

// VerifyCheckValue reports whether kcv came from this vault's key.
func (v *Vault) VerifyCheckValue(kcv []byte) error {
	if subtle.ConstantTimeCompare(v.CheckValue(), kcv) != 1 {
		return ErrWrongKey
	}
	return nil
}

func checkValue(key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("dens-vault-check-v1"))
	return mac.Sum(nil)[:16]
}

// Matches reports whether key is this vault's key, in constant time.
func (v *Vault) Matches(key []byte) bool {
	return subtle.ConstantTimeCompare(v.key, key) == 1
}
