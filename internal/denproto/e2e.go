package denproto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// End-to-end encrypted DMs (M1.7).
//
// A DM key comes from an exchange of one-time keys between the two
// members' clients, which the den relays but can't take part in: X25519
// and ML-KEM-768 together, so the key stays safe if either is broken. The
// exchange runs in commit-first order. The starter sends its ML-KEM key
// and only a commitment to its X25519 key; the other side answers with its
// X25519 key and a ciphertext to the ML-KEM key; then the starter reveals.
// Everything that feeds the check code is fixed before its sender sees the
// other side's part, so a den in the middle, running one exchange with
// each member, can't choose keys that make their codes match: it gets one
// blind guess at 32 digits. Device approval uses the same exchange.
//
// Each member keeps their copy of a DM key on the den sealed with their DM
// seal, 32 random bytes that never leave their devices. So is each side's
// state while an exchange runs, so any of the member's devices can carry
// it on.

const (
	SealSize      = 32
	DMKeySize     = 32
	CheckDigits   = 32 // each side reads out half, and types the other half
	ExchangeNonce = 32
	// MaxSealed bounds a sealed DM message: its text and its files' keys
	// and details.
	MaxSealed = 64 << 10
)

const (
	exchangeCommitContext = "dens-exchange-commit-v1"
	exchangeKeyContext    = "dens-exchange-key-v1"
	exchangeCheckContext  = "dens-exchange-check-v1"
	dmExchangeContext     = "dens-exchange-dm-v1"
	deviceExchangeContext = "dens-exchange-device-v1"
	exchangeStateContext  = "dens-exchange-state-v1"
	sealKeyContext        = "dens-dm-seal-v1"
	sealCheckContext      = "dens-dm-seal-check-v1"
	dmKeyContext          = "dens-dm-key-v1"
	dmMessageContext      = "dens-dm-message-v1"
	dmFileContext         = "dens-dm-file-v1"
	handoverContext       = "dens-seal-handover-v1"
)

// ErrExchange is an exchange message that doesn't fit the exchange: a
// reveal that doesn't match its commitment, or keys of the wrong shape.
var ErrExchange = errors.New("the exchange doesn't check out")

// ErrSealedOpen is a sealed value that doesn't open: the wrong key, or
// data tampered with or moved.
var ErrSealedOpen = errors.New("sealed data doesn't open")

// DMExchangeContext names a DM exchange: the den, the DM, and who started
// it and who answers, so its messages mean nothing anywhere else. Fresh
// one-time keys on both sides tell one exchange of a DM from the next.
func DMExchangeContext(denID []byte, channel, starter, other int64) []byte {
	return join([]byte(dmExchangeContext), denID, u64(channel), u64(starter), u64(other))
}

// DeviceExchangeContext names the exchange that approves a new device: the
// den, the member's username, and the new device's key ID, so the digits
// match only between that device and one of that member's.
func DeviceExchangeContext(denID []byte, username string, keyID []byte) []byte {
	return join([]byte(deviceExchangeContext), denID, u64(int64(len(username))), []byte(username), keyID)
}

func u64(n int64) []byte { return binary.BigEndian.AppendUint64(nil, uint64(n)) }

// ExchangeOffer starts an exchange: the starter's ML-KEM-768 encapsulation
// key, and a commitment to its X25519 key and nonce.
type ExchangeOffer struct {
	EK     Bytes `json:"ek"`
	Commit Bytes `json:"commit"`
}

// ExchangeAnswer is the other side's X25519 key and nonce, and a
// ciphertext to the starter's encapsulation key.
type ExchangeAnswer struct {
	X     Bytes `json:"x"`
	Nonce Bytes `json:"nonce"`
	CT    Bytes `json:"ct"`
}

// ExchangeReveal is the starter's X25519 key and nonce, which its
// commitment fixed.
type ExchangeReveal struct {
	X     Bytes `json:"x"`
	Nonce Bytes `json:"nonce"`
}

func (o ExchangeOffer) Check() error {
	if len(o.EK) != mlkem.EncapsulationKeySize768 || len(o.Commit) != sha256.Size {
		return ErrExchange
	}
	return nil
}

func (a ExchangeAnswer) Check() error {
	if len(a.X) != 32 || len(a.Nonce) != ExchangeNonce || len(a.CT) != mlkem.CiphertextSize768 {
		return ErrExchange
	}
	return nil
}

func (r ExchangeReveal) Check() error {
	if len(r.X) != 32 || len(r.Nonce) != ExchangeNonce {
		return ErrExchange
	}
	return nil
}

// Opens reports whether a reveal is what an offer's commitment fixed, in an
// exchange with the given context. The answering side checks it before it
// trusts the reveal; the den checks it too, to turn away a broken one early.
func (r ExchangeReveal) Opens(context []byte, o ExchangeOffer) bool {
	return r.Check() == nil && Equal(commitment(context, r.X, r.Nonce), o.Commit)
}

func commitment(context, x, nonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte(exchangeCommitContext))
	h.Write(u64(int64(len(context))))
	h.Write(context)
	h.Write(x)
	h.Write(nonce)
	return h.Sum(nil)
}

// Starter is the starting side of an exchange between its offer and the
// other side's answer. It's secret: a client keeps it sealed, since an
// exchange can wait days for the other member.
type Starter struct {
	Context Bytes `json:"context"`
	X       Bytes `json:"x"`  // X25519 private key
	DK      Bytes `json:"dk"` // ML-KEM-768 decapsulation key seed
	Nonce   Bytes `json:"nonce"`
}

// StartExchange makes a starter's one-time keys, and the offer to send.
func StartExchange(context []byte) (Starter, ExchangeOffer, error) {
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Starter{}, ExchangeOffer{}, err
	}
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return Starter{}, ExchangeOffer{}, err
	}
	s := Starter{Context: append(Bytes{}, context...), X: x.Bytes(), DK: dk.Bytes(), Nonce: Random(ExchangeNonce)}
	offer := ExchangeOffer{EK: dk.EncapsulationKey().Bytes(), Commit: commitment(context, x.PublicKey().Bytes(), s.Nonce)}
	return s, offer, nil
}

// Offer is the offer this starter sent.
func (s Starter) Offer() (ExchangeOffer, error) {
	x, err := ecdh.X25519().NewPrivateKey(s.X)
	if err != nil {
		return ExchangeOffer{}, ErrExchange
	}
	dk, err := mlkem.NewDecapsulationKey768(s.DK)
	if err != nil {
		return ExchangeOffer{}, ErrExchange
	}
	return ExchangeOffer{EK: dk.EncapsulationKey().Bytes(), Commit: commitment(s.Context, x.PublicKey().Bytes(), s.Nonce)}, nil
}

// Finish takes the other side's answer, and returns the reveal to send and
// what the exchange made.
func (s Starter) Finish(ans ExchangeAnswer) (ExchangeReveal, Result, error) {
	if err := ans.Check(); err != nil {
		return ExchangeReveal{}, Result{}, err
	}
	x, err := ecdh.X25519().NewPrivateKey(s.X)
	if err != nil {
		return ExchangeReveal{}, Result{}, ErrExchange
	}
	dk, err := mlkem.NewDecapsulationKey768(s.DK)
	if err != nil {
		return ExchangeReveal{}, Result{}, ErrExchange
	}
	peer, err := ecdh.X25519().NewPublicKey(ans.X)
	if err != nil {
		return ExchangeReveal{}, Result{}, ErrExchange
	}
	dh, err := x.ECDH(peer)
	if err != nil {
		return ExchangeReveal{}, Result{}, ErrExchange
	}
	shared, err := dk.Decapsulate(ans.CT)
	if err != nil {
		return ExchangeReveal{}, Result{}, ErrExchange
	}
	offer := ExchangeOffer{EK: dk.EncapsulationKey().Bytes(), Commit: commitment(s.Context, x.PublicKey().Bytes(), s.Nonce)}
	rev := ExchangeReveal{X: x.PublicKey().Bytes(), Nonce: s.Nonce}
	res, err := derive(s.Context, offer, ans, rev, dh, shared)
	return rev, res, err
}

// Responder is the answering side between its answer and the starter's
// reveal. It's secret, as a Starter is.
type Responder struct {
	Context Bytes         `json:"context"`
	Offer   ExchangeOffer `json:"offer"`
	X       Bytes         `json:"x"` // X25519 private key
	Nonce   Bytes         `json:"nonce"`
	CT      Bytes         `json:"ct"`
	Shared  Bytes         `json:"shared"` // the ML-KEM shared secret
}

// AnswerExchange answers an offer: one-time keys, and a ciphertext to the
// starter's encapsulation key.
func AnswerExchange(context []byte, offer ExchangeOffer) (Responder, ExchangeAnswer, error) {
	if err := offer.Check(); err != nil {
		return Responder{}, ExchangeAnswer{}, err
	}
	ek, err := mlkem.NewEncapsulationKey768(offer.EK)
	if err != nil {
		return Responder{}, ExchangeAnswer{}, ErrExchange
	}
	shared, ct := ek.Encapsulate()
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Responder{}, ExchangeAnswer{}, err
	}
	r := Responder{Context: append(Bytes{}, context...), Offer: offer, X: x.Bytes(), Nonce: Random(ExchangeNonce), CT: ct, Shared: shared}
	return r, ExchangeAnswer{X: x.PublicKey().Bytes(), Nonce: r.Nonce, CT: ct}, nil
}

// Finish takes the starter's reveal, checks it against the commitment,
// and returns what the exchange made.
func (r Responder) Finish(rev ExchangeReveal) (Result, error) {
	if !rev.Opens(r.Context, r.Offer) {
		return Result{}, ErrExchange
	}
	x, err := ecdh.X25519().NewPrivateKey(r.X)
	if err != nil {
		return Result{}, ErrExchange
	}
	peer, err := ecdh.X25519().NewPublicKey(rev.X)
	if err != nil {
		return Result{}, ErrExchange
	}
	dh, err := x.ECDH(peer)
	if err != nil {
		return Result{}, ErrExchange
	}
	ans := ExchangeAnswer{X: x.PublicKey().Bytes(), Nonce: r.Nonce, CT: r.CT}
	return derive(r.Context, r.Offer, ans, rev, dh, r.Shared)
}

// Result is what an exchange made: a key both sides share, and the check
// code that shows whether anyone sat between them.
type Result struct {
	Key   Bytes  `json:"key"`
	Check string `json:"check"` // CheckDigits digits
}

// derive makes the key and check code from both shared secrets, salted
// with the whole exchange, so both sides agree on them only if they saw
// the same messages and hold the same secrets.
func derive(context []byte, offer ExchangeOffer, ans ExchangeAnswer, rev ExchangeReveal, dh, shared []byte) (Result, error) {
	h := sha256.New()
	h.Write(u64(int64(len(context))))
	h.Write(context)
	for _, part := range [][]byte{offer.EK, offer.Commit, ans.X, ans.Nonce, ans.CT, rev.X, rev.Nonce} {
		h.Write(part)
	}
	transcript := h.Sum(nil)
	secret := join(dh, shared)
	key, err := hkdf.Key(sha256.New, secret, transcript, exchangeKeyContext, DMKeySize)
	if err != nil {
		return Result{}, err
	}
	raw, err := hkdf.Key(sha256.New, secret, transcript, exchangeCheckContext, 16)
	if err != nil {
		return Result{}, err
	}
	// Two 16-digit halves; the bias of taking 64 bits modulo 10^16 is
	// under one part in a thousand.
	const half = 10_000_000_000_000_000
	check := fmt.Sprintf("%016d%016d", binary.BigEndian.Uint64(raw[:8])%half, binary.BigEndian.Uint64(raw[8:])%half)
	return Result{Key: key, Check: check}, nil
}

// CheckHalf is the half of a check code one side reads out: the first 16
// digits for the starter, the last 16 for the other side.
func CheckHalf(check string, starter bool) string {
	if len(check) != CheckDigits {
		return ""
	}
	if starter {
		return check[:CheckDigits/2]
	}
	return check[CheckDigits/2:]
}

// FormatCheckHalf shows 16 digits in groups of four.
func FormatCheckHalf(half string) string {
	var b strings.Builder
	for i := 0; i < len(half); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(half[i:min(i+4, len(half))])
	}
	return b.String()
}

// CheckTyped reports whether digits someone typed, with any spaces or
// dashes, are the other side's half of the check code.
func CheckTyped(check string, starter bool, typed string) bool {
	typed = strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(typed)
	want := CheckHalf(check, !starter)
	return want != "" && Equal([]byte(typed), []byte(want))
}

// NewSeal makes a DM seal.
func NewSeal() Bytes { return Random(SealSize) }

// FormatSeal shows a DM seal to keep: 52 letters and digits in groups of
// four.
func FormatSeal(seal []byte) string { return FormatRecoveryCode(seal) }

// ParseSeal reads a seal as a person types it, as ParseRecoveryCode does.
func ParseSeal(s string) (Bytes, error) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\n", "", "0", "O", "1", "I", "8", "B").Replace(s))
	seal, err := recoveryEncoding.DecodeString(s)
	if err != nil || len(seal) != SealSize {
		return nil, errors.New("a DM seal is 52 letters and digits")
	}
	return seal, nil
}

// sealKey is what a seal seals with at one den, so what one den keeps is
// useless at another.
func sealKey(seal, denID []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, seal, denID, sealKeyContext, chacha20poly1305.KeySize)
}

// SealCheck identifies a member's seal at one den without revealing it, so
// a seal typed on a new device can be checked before it's used.
func SealCheck(seal, denID []byte) (Bytes, error) {
	key, err := sealKey(seal, denID)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(sealCheckContext))
	return mac.Sum(nil), nil
}

// DMKeyAD binds a member's sealed copy of a DM key to the den, the DM,
// the key and the member.
func DMKeyAD(denID []byte, channel, keyID, member int64) []byte {
	return join([]byte(dmKeyContext), denID, u64(channel), u64(keyID), u64(member))
}

// SealDMKey seals a member's copy of a DM key with their seal.
func SealDMKey(seal, denID []byte, channel, keyID, member int64, key []byte) (Bytes, error) {
	k, err := sealKey(seal, denID)
	if err != nil {
		return nil, err
	}
	return sealWith(k, key, DMKeyAD(denID, channel, keyID, member))
}

// OpenDMKey opens a member's sealed copy of a DM key.
func OpenDMKey(seal, denID []byte, channel, keyID, member int64, sealed []byte) (Bytes, error) {
	k, err := sealKey(seal, denID)
	if err != nil {
		return nil, err
	}
	key, err := openWith(k, sealed, DMKeyAD(denID, channel, keyID, member))
	if err != nil || len(key) != DMKeySize {
		return nil, ErrSealedOpen
	}
	return key, nil
}

// SealHandover seals a member's seal for their new device, with the key of
// the exchange that approved it.
func SealHandover(exchangeKey, context, seal []byte) (Bytes, error) {
	k, err := hkdf.Key(sha256.New, exchangeKey, nil, handoverContext, chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	return sealWith(k, seal, context)
}

// OpenHandover opens a seal handed over by the approving device.
func OpenHandover(exchangeKey, context, sealed []byte) (Bytes, error) {
	k, err := hkdf.Key(sha256.New, exchangeKey, nil, handoverContext, chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	seal, err := openWith(k, sealed, context)
	if err != nil || len(seal) != SealSize {
		return nil, ErrSealedOpen
	}
	return seal, nil
}

// stateAD binds one side's sealed exchange state to the den, the DM, the
// member, which side they're on, and the exchange, which its offer's
// commitment names.
func stateAD(denID []byte, channel, member int64, commit []byte, starter bool) []byte {
	side := []byte{0}
	if starter {
		side[0] = 1
	}
	return join([]byte(exchangeStateContext), denID, u64(channel), u64(member), commit, side)
}

// SealExchangeState seals one side's state in a DM exchange (a Starter or
// a Responder) with the member's seal, for the den to keep while the
// exchange runs.
func SealExchangeState(seal, denID []byte, channel, member int64, commit []byte, starter bool, state any) (Bytes, error) {
	plain, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	k, err := sealKey(seal, denID)
	if err != nil {
		return nil, err
	}
	return sealWith(k, plain, stateAD(denID, channel, member, commit, starter))
}

// OpenExchangeState opens one side's sealed exchange state into state.
func OpenExchangeState(seal, denID []byte, channel, member int64, commit []byte, starter bool, sealed []byte, state any) error {
	k, err := sealKey(seal, denID)
	if err != nil {
		return err
	}
	plain, err := openWith(k, sealed, stateAD(denID, channel, member, commit, starter))
	if err != nil {
		return ErrSealedOpen
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, state); err != nil {
		return ErrSealedOpen
	}
	return nil
}

// DMPayload is what a DM message seals: its text, and each file's key and
// details, which the den never sees.
type DMPayload struct {
	Text  string   `json:"text"`
	Files []DMFile `json:"files,omitempty"`
}

// DMFile is an attachment in a DM: the den's sealed blob of the file and
// of its preview, and what the page needs to show them.
type DMFile struct {
	ID       string   `json:"id"`
	Key      Bytes    `json:"key"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Size     int64    `json:"size"`
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Animated bool     `json:"animated,omitempty"`
	Thumb    *DMThumb `json:"thumb,omitempty"`
}

// DMThumb is a DM file's preview: its own sealed blob, under the file's
// key.
type DMThumb struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// MessageAD binds a sealed DM message to the den, the DM, its author, its
// nonce, the key and its revision, so the den can't move it elsewhere or
// pass off an old version as a new one.
type MessageAD struct {
	DenID    []byte
	Channel  int64
	Author   int64
	Nonce    []byte
	KeyID    int64
	Revision int
}

func (a MessageAD) bytes() []byte {
	return join([]byte(dmMessageContext), a.DenID, u64(a.Channel), u64(a.Author), u64(int64(len(a.Nonce))), a.Nonce,
		u64(a.KeyID), u64(int64(a.Revision)))
}

// SealDMMessage seals a DM message's payload with the DM key.
func SealDMMessage(key []byte, ad MessageAD, p DMPayload) (Bytes, error) {
	// Without HTML escaping, which would make text with < > & up to six
	// times larger, and nothing here is HTML.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	plain := buf.Bytes()
	defer clear(plain)
	sealed, err := sealWith(key, plain, ad.bytes())
	if err != nil {
		return nil, err
	}
	if len(sealed) > MaxSealed {
		return nil, fmt.Errorf("a sealed message is at most %d bytes", MaxSealed)
	}
	return sealed, nil
}

// OpenDMMessage opens a DM message and checks its payload, which the other
// member wrote and so is untrusted.
func OpenDMMessage(key []byte, ad MessageAD, sealed []byte) (DMPayload, error) {
	plain, err := openWith(key, sealed, ad.bytes())
	if err != nil {
		return DMPayload{}, ErrSealedOpen
	}
	var p DMPayload
	if err := json.Unmarshal(plain, &p); err != nil {
		return DMPayload{}, ErrSealedOpen
	}
	if err := CheckDMPayload(p); err != nil {
		return DMPayload{}, err
	}
	return p, nil
}

// CheckDMPayload checks a DM message's payload as the den checks a
// channel message's text and files.
func CheckDMPayload(p DMPayload) error {
	if len(p.Files) > MaxAttachments {
		return errors.New("a DM message has too many files")
	}
	if err := CheckMessageText(p.Text, len(p.Files) > 0); err != nil {
		return err
	}
	for _, f := range p.Files {
		if len(f.Key) != chacha20poly1305.KeySize {
			return errors.New("a DM file has an invalid key")
		}
		var thumb *Thumb
		if t := f.Thumb; t != nil {
			if _, err := ParseID(t.ID); err != nil || t.ID == f.ID {
				return errors.New("a DM file's preview has an invalid ID")
			}
			thumb = &Thumb{Width: t.Width, Height: t.Height}
		}
		file := File{ID: f.ID, Name: f.Name, Type: f.Type, Size: f.Size, Width: f.Width, Height: f.Height, Animated: f.Animated, Thumb: thumb}
		if err := CheckFile(file); err != nil {
			return err
		}
	}
	return nil
}

// DMFileAD binds a DM file's sealed blob to the den, the DM and whether
// it's the preview.
func DMFileAD(denID []byte, channel int64, thumb bool) []byte {
	part := []byte("file")
	if thumb {
		part = []byte("thumb")
	}
	return join([]byte(dmFileContext), denID, u64(channel), part)
}

func sealWith(key, plain, ad []byte) (Bytes, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := Random(chacha20poly1305.NonceSizeX)
	return aead.Seal(nonce, nonce, plain, ad), nil
}

func openWith(key, sealed, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < chacha20poly1305.NonceSizeX+aead.Overhead() {
		return nil, ErrSealedOpen
	}
	return aead.Open(nil, sealed[:chacha20poly1305.NonceSizeX], sealed[chacha20poly1305.NonceSizeX:], ad)
}
