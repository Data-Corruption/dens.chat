package denproto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Context strings keep a signature or a derived value from being usable for
// any other purpose.
const (
	denSigContext   = "dens-den-v1"
	proofContext    = "dens-auth-v1"
	verifierContext = "dens-den-password-v1"
)

// Verifier cost, the same as the local password's.
const (
	verifierTime    = 3
	verifierMemory  = 64 * 1024 // KiB
	verifierThreads = 4
)

// ID is SHA-256 of an Ed25519 public key: a den_id or a key_id.
func ID(pub ed25519.PublicKey) Bytes {
	sum := sha256.Sum256(pub)
	return sum[:]
}

// Hash is SHA-256, for storing tokens, codes and verifiers.
func Hash(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}

// Equal compares two secrets in constant time.
func Equal(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }

// Random returns n random bytes.
func Random(n int) Bytes {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return b
}

// DenChallengeMessage is what the den signs to prove its identity key and
// say where it is: "dens-den-v1" ‖ client_nonce ‖ nonce ‖ url. The address
// comes last, so it needs no length. Without it, anything that could relay
// a den's answers from another address, such as whoever holds a domain
// the den left, would pass for the den.
func DenChallengeMessage(clientNonce, nonce []byte, url string) []byte {
	return join([]byte(denSigContext), clientNonce, nonce, []byte(url))
}

// ProofMessage is what a device signs to prove its key for one nonce:
// "dens-auth-v1" ‖ den_id ‖ key_id ‖ nonce.
func ProofMessage(denID, keyID, nonce []byte) []byte {
	return join([]byte(proofContext), denID, keyID, nonce)
}

// Prove signs a proof for nonce with the device key.
func Prove(key ed25519.PrivateKey, denID, nonce []byte) Bytes {
	pub := key.Public().(ed25519.PublicKey)
	return ed25519.Sign(key, ProofMessage(denID, ID(pub), nonce))
}

// ErrWrongIdentity reports a den that can't prove the identity key pinned
// for it: another server at its address, or one pretending to be it.
var ErrWrongIdentity = errors.New("the den can't prove its identity key")

// VerifyDen checks a challenge response: the den's signature over the
// client's nonce, its own and its address, and that its key is the one
// pinned for it. A nil denID pins nothing and trusts the key the den
// presents, for signing in to a den by its address. It returns the den's
// ID. Callers also check the signed address with CheckDenURL.
func VerifyDen(denID []byte, clientNonce []byte, resp ChallengeResponse) (Bytes, error) {
	if len(resp.DenKey) != PublicKeySize || len(resp.Nonce) != NonceSize || len(resp.DenSig) != SignatureSize {
		return nil, fmt.Errorf("%w: its challenge response is malformed", ErrWrongIdentity)
	}
	if url, err := NormalizeDenURL(resp.URL); err != nil || url != resp.URL {
		return nil, fmt.Errorf("%w: it gave an invalid address", ErrWrongIdentity)
	}
	id := ID(ed25519.PublicKey(resp.DenKey))
	if denID != nil && !Equal(id, denID) {
		return nil, fmt.Errorf("%w: it holds another identity key than the one pinned for it", ErrWrongIdentity)
	}
	if !ed25519.Verify(ed25519.PublicKey(resp.DenKey), DenChallengeMessage(clientNonce, resp.Nonce, resp.URL), resp.DenSig) {
		return nil, fmt.Errorf("%w: its signature doesn't verify", ErrWrongIdentity)
	}
	return id, nil
}

// MovedError is a den that proved its key at one address but signed
// another. Either it moved there, or something at the old address is
// passing its answers along; a client goes on only at URL, and sends
// nothing more where it was.
type MovedError struct{ URL string }

func (e *MovedError) Error() string { return "the den says its address is " + e.URL }

// CheckDenURL returns a *MovedError unless the address a den signed is the
// one it was reached at.
func CheckDenURL(signed, reached string) error {
	if u, err := NormalizeDenURL(reached); err == nil && u == signed {
		return nil
	}
	return &MovedError{URL: signed}
}

// Fingerprint shows the start of a den's ID for people to compare, as
// XXXX-XXXX-XXXX-XXXX: 80 bits, like a recovery code.
func Fingerprint(denID []byte) string {
	if len(denID) < RecoveryCodeSize {
		return ""
	}
	return FormatRecoveryCode(denID[:RecoveryCodeSize])
}

// Verifier derives the value a den stores (hashed) instead of a password:
// Argon2id(password, SHA-256("dens-den-password-v1" ‖ den_id ‖ username)).
// Each den gets a different verifier for the same password.
func Verifier(password string, denID []byte, username string) Bytes {
	salt := Hash(join([]byte(verifierContext), denID, []byte(username)))
	return argon2.IDKey([]byte(password), salt, verifierTime, verifierMemory, verifierThreads, VerifierSize)
}

// Recovery codes are shown as 16 base32 characters in four groups.
var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// FormatRecoveryCode shows a raw code as XXXX-XXXX-XXXX-XXXX.
func FormatRecoveryCode(code []byte) string {
	s := recoveryEncoding.EncodeToString(code)
	var b strings.Builder
	for i := 0; i < len(s); i += 4 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(s[i:min(i+4, len(s))])
	}
	return b.String()
}

// ParseRecoveryCode reads a code as a person types it: any case, with or
// without separators, and with 0, 1 and 8 read as the O, I and B they
// resemble.
func ParseRecoveryCode(s string) ([]byte, error) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "", "0", "O", "1", "I", "8", "B").Replace(s))
	code, err := recoveryEncoding.DecodeString(s)
	if err != nil || len(code) != RecoveryCodeSize {
		return nil, errors.New("a recovery code is 16 letters and digits")
	}
	return code, nil
}

func join(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
