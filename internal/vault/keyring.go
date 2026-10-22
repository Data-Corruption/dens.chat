package vault

import (
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// ErrWrongSecret reports a keyring's secret that doesn't open a wrap: the
// keyring was reset, or answered with something other than its secret, as
// KWallet before KDE Frameworks 6.30 does when its prompt is cancelled.
var ErrWrongSecret = errors.New("the keyring's secret doesn't open the remembered key")

const keyringWrapAD = "dens-vault-keyring-wrap-v1"

// minKeyringSecret is the shortest secret a keyring may give; the Secret
// portal's are 64 random bytes.
const minKeyringSecret = 16

// KeyringWrap is the Flatpak's remembered copy of the data key (M9.3),
// sealed under a key derived from the secret the member's keyring keeps for
// the app, which the Secret portal hands over. The wrap is authenticated,
// so a wrong secret fails to open it rather than yielding a wrong key. It
// stays on this machine, beside the instance's lifecycle state, and never
// travels with a backup.
type KeyringWrap struct {
	Version int    `json:"v"`
	Salt    []byte `json:"salt"`
	Sealed  []byte `json:"sealed"`
}

// WrapWithKeyring seals the data key under the keyring's secret.
func (v *Vault) WrapWithKeyring(secret []byte) (KeyringWrap, error) {
	wrap := KeyringWrap{Version: 1, Salt: make([]byte, 16)}
	if _, err := rand.Read(wrap.Salt); err != nil {
		return KeyringWrap{}, fmt.Errorf("generate salt: %w", err)
	}
	aead, err := wrap.aead(secret)
	if err != nil {
		return KeyringWrap{}, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return KeyringWrap{}, fmt.Errorf("generate nonce: %w", err)
	}
	sealed := append([]byte{sealVersion}, nonce...)
	wrap.Sealed = aead.Seal(sealed, nonce, v.key, []byte(keyringWrapAD))
	return wrap, nil
}

// UnwrapWithKeyring returns the data key sealed in wrap, or ErrWrongSecret.
// The caller must clear the returned key when done with it.
func UnwrapWithKeyring(wrap KeyringWrap, secret []byte) ([]byte, error) {
	if wrap.Version != 1 {
		return nil, fmt.Errorf("unsupported keyring wrap version %d", wrap.Version)
	}
	if len(wrap.Salt) != 16 {
		return nil, errors.New("keyring wrap salt has the wrong length")
	}
	aead, err := wrap.aead(secret)
	if err != nil {
		return nil, err
	}
	key, err := openWith(aead, wrap.Sealed, []byte(keyringWrapAD))
	if err != nil {
		return nil, ErrWrongSecret
	}
	if len(key) != KeySize {
		clear(key)
		return nil, fmt.Errorf("unwrapped key is %d bytes", len(key))
	}
	return key, nil
}

// aead derives the wrap's key from the keyring's secret. The secret is
// random, so HKDF's extract is all it needs; no password stretching.
func (w KeyringWrap) aead(secret []byte) (cipher.AEAD, error) {
	if len(secret) < minKeyringSecret {
		return nil, fmt.Errorf("the keyring's secret is %d bytes, under %d", len(secret), minKeyringSecret)
	}
	kek, err := hkdf.Key(sha256.New, secret, w.Salt, keyringWrapAD, KeySize)
	if err != nil {
		return nil, err
	}
	defer clear(kek)
	return chacha20poly1305.NewX(kek)
}

// MarshalKeyringWrap and UnmarshalKeyringWrap are the file encoding of a
// wrap.
func MarshalKeyringWrap(w KeyringWrap) ([]byte, error) { return json.Marshal(w) }

func UnmarshalKeyringWrap(data []byte) (KeyringWrap, error) {
	var w KeyringWrap
	if err := json.Unmarshal(data, &w); err != nil {
		return KeyringWrap{}, fmt.Errorf("decode keyring wrap: %w", err)
	}
	return w, nil
}
