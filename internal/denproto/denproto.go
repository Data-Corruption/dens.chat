// Package denproto is the client-to-den protocol shared by both sides: wire
// types, the byte layouts that get signed, password verifiers, invite
// strings and name rules. docs/dev/protocol.md is its specification; keeping
// one implementation for the den and the client means the two can't drift.
package denproto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Protocol versions this build speaks.
const (
	Version    = 1
	MinVersion = 1
)

// Headers.
const (
	HeaderVersion = "Dens-Protocol"
	HeaderRange   = "Dens-Protocol-Range"
)

// MaxBody bounds every request and response body, and every den frame.
const MaxBody = 1 << 20

// MaxClientFrame bounds a frame from client to den.
const MaxClientFrame = 64 << 10

// Sizes of the fixed-length values.
const (
	NonceSize        = 32
	TokenSize        = 32
	InviteCodeSize   = 16
	RecoveryCodeSize = 10
	VerifierSize     = 32
	IDSize           = 32 // den_id and key_id: SHA-256 of an Ed25519 public key
	PublicKeySize    = 32
	SignatureSize    = 64
	RecoveryCodes    = 10 // codes issued per member
)

// Error codes. Status codes pair with them as in protocol.md.
const (
	CodeMalformed           = "malformed"
	CodeInvalidField        = "invalid_field"
	CodeUnauthorized        = "unauthorized"
	CodeBadSignature        = "bad_signature"
	CodeBadNonce            = "bad_nonce"
	CodeForbidden           = "forbidden"
	CodeBanned              = "banned"
	CodeKeyRevoked          = "key_revoked"
	CodeNotFound            = "not_found"
	CodeUsernameTaken       = "username_taken"
	CodeEditConflict        = "edit_conflict"
	CodeInviteInvalid       = "invite_invalid"
	CodeTooLarge            = "too_large"
	CodeProtocolUnsupported = "protocol_unsupported"
	CodeRateLimited         = "rate_limited"
	CodeDenNotCreated       = "den_not_created"
	CodeWrongPassword       = "wrong_password"
)

// WebSocket close codes beyond the standard ones.
const (
	CloseSessionExpired = 4001
	CloseRevoked        = 4003
	CloseTooSlow        = 4008
	CloseRateLimited    = 4029
)

// Event types.
const (
	EventReady       = "ready"
	EventResumed     = "resumed"
	EventAuthRenew   = "auth.renew"
	EventAuthRenewed = "auth.renewed"
	EventDenUpdated  = "den.updated"
)

// Error is a protocol error: an HTTP status with a stable code. Message is
// English for logs; it comes from the den, so it is never shown verbatim.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	// Body is the whole response, for errors that carry more, such as
	// edit_conflict's current message.
	Body []byte `json:"-"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (%d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("%s (%d)", e.Code, e.Status)
}

// Errorf builds an Error.
func Errorf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// IsCode reports whether err is a protocol error with code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

type errorBody struct {
	Error *Error `json:"error"`
}

// WriteError writes err as a protocol error response.
func WriteError(w http.ResponseWriter, err *Error) {
	if err.Status == http.StatusTooManyRequests {
		if w.Header().Get("Retry-After") == "" {
			w.Header().Set("Retry-After", "10")
		}
	}
	WriteJSON(w, err.Status, errorBody{Error: err})
}

// WriteJSON writes v as a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ReadError turns a failed response into an *Error, reading at most
// MaxBody of it.
func ReadError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	var parsed errorBody
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Code != "" {
		parsed.Error.Status = resp.StatusCode
		parsed.Error.Body = body
		if len(parsed.Error.Message) > 200 {
			parsed.Error.Message = parsed.Error.Message[:200]
		}
		return parsed.Error
	}
	return &Error{Status: resp.StatusCode, Code: "http_" + strconv.Itoa(resp.StatusCode)}
}

// RangeHeader is this build's Dens-Protocol-Range value.
func RangeHeader() string { return fmt.Sprintf("%d-%d", MinVersion, Version) }

// ParseRange parses a Dens-Protocol-Range value.
func ParseRange(value string) (lo, hi int, err error) {
	a, b, ok := strings.Cut(value, "-")
	if ok {
		lo, err = strconv.Atoi(a)
		if err == nil {
			hi, err = strconv.Atoi(b)
		}
	}
	if !ok || err != nil || lo < 1 || hi < lo {
		return 0, 0, fmt.Errorf("invalid protocol range %q", value)
	}
	return lo, hi, nil
}

// Bytes is a fixed or bounded byte string, base64url without padding in JSON.
type Bytes []byte

func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.RawURLEncoding.EncodeToString(b))
}

func (b *Bytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid base64url: %w", err)
	}
	*b = decoded
	return nil
}

// String encodes b as base64url without padding.
func (b Bytes) String() string { return base64.RawURLEncoding.EncodeToString(b) }

// ParseBytes decodes a base64url string of exactly size bytes.
func ParseBytes(s string, size int) (Bytes, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != size {
		return nil, fmt.Errorf("want %d bytes of base64url", size)
	}
	return b, nil
}

// Size returns an invalid_field error unless value is size bytes long.
func Size(name string, value Bytes, size int) *Error {
	if len(value) != size {
		return Errorf(http.StatusBadRequest, CodeInvalidField, "%s must be %d bytes", name, size)
	}
	return nil
}

// Event is one entry of a frame. Durable events carry Seq; ephemeral ones
// don't.
type Event struct {
	T   string          `json:"t"`
	Seq uint64          `json:"seq,omitempty"`
	D   json.RawMessage `json:"d,omitempty"`
}

// NewEvent encodes data as an event's payload.
func NewEvent(t string, seq uint64, data any) (Event, error) {
	e := Event{T: t, Seq: seq}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return Event{}, err
		}
		e.D = raw
	}
	return e, nil
}

// EncodeFrame encodes events as one frame.
func EncodeFrame(events ...Event) ([]byte, error) { return json.Marshal(events) }

// DecodeFrame decodes a frame into its events.
func DecodeFrame(data []byte) ([]Event, error) {
	var events []Event
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("decode frame: %w", err)
	}
	return events, nil
}

// Objects ---------------------------------------------------------------------

// Den describes a den.
type Den struct {
	ID   Bytes  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// Limits and CallLimits come to members, in ready and den.updated,
	// and not in a join preview. CallLimits are from M4.2.
	Limits     Limits     `json:"limits,omitzero"`
	CallLimits CallLimits `json:"call_limits,omitzero"`
}

// Roles.
const (
	RoleMember    = "member"
	RoleModerator = "moderator"
	RoleOwner     = "owner"
)

// Member is an account on a den. A member who left or was removed keeps
// their record, with LeftAt set, so their messages still have a name. Bio
// comes only with GET /api/members/{id}, to keep snapshots small.
type Member struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	JoinedAt    int64  `json:"joined_at"`
	LeftAt      int64  `json:"left_at,omitempty"`
	Bio         string `json:"bio,omitempty"`
	Avatar      Image  `json:"avatar,omitzero"`
	Banner      Image  `json:"banner,omitzero"`
}

// Requests and responses ------------------------------------------------------

type ChallengeRequest struct {
	ClientNonce Bytes `json:"client_nonce"`
}

// ChallengeResponse proves the den's identity key, and says where the den
// is: URL is the den's own address, which the signature covers.
type ChallengeResponse struct {
	Nonce  Bytes  `json:"nonce"`
	DenKey Bytes  `json:"den_key"`
	DenSig Bytes  `json:"den_sig"`
	URL    string `json:"url"`
}

type JoinPreviewRequest struct {
	Invite Bytes `json:"invite"`
}

type JoinPreviewResponse struct {
	Den Den `json:"den"`
}

type JoinRequest struct {
	Invite      Bytes  `json:"invite"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Verifier    Bytes  `json:"verifier"`
	PublicKey   Bytes  `json:"public_key"`
	DeviceLabel string `json:"device_label"`
	Nonce       Bytes  `json:"nonce"`
	Proof       Bytes  `json:"proof"`
	// SealCheck identifies the DM seal the joining member's client holds
	// (M1.7).
	SealCheck Bytes `json:"seal_check"`
}

type JoinResponse struct {
	Member        Member   `json:"member"`
	Token         Bytes    `json:"token"`
	ExpiresAt     int64    `json:"expires_at"`
	RecoveryCodes []string `json:"recovery_codes"`
}

type LoginRequest struct {
	KeyID Bytes `json:"key_id"`
	Nonce Bytes `json:"nonce"`
	Proof Bytes `json:"proof"`
}

type SessionResponse struct {
	Token     Bytes `json:"token"`
	ExpiresAt int64 `json:"expires_at"`
}

type InviteCreateRequest struct {
	ExpiresIn int64 `json:"expires_in"` // seconds
	MaxUses   int   `json:"max_uses"`
}

type Invite struct {
	ID        string `json:"id"`
	Code      Bytes  `json:"code,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
	MaxUses   int    `json:"max_uses"`
	Uses      int    `json:"uses"`
}

type InviteList struct {
	Invites []Invite `json:"invites"`
}

type DenUpdateRequest struct {
	Name       *string     `json:"name,omitempty"`
	URL        *string     `json:"url,omitempty"`
	Limits     *Limits     `json:"limits,omitempty"`
	CallLimits *CallLimits `json:"call_limits,omitempty"`
}

// Ready is the snapshot that starts a connection, or replaces the client's
// state mid-stream.
type Ready struct {
	Epoch      string      `json:"epoch"`
	Seq        uint64      `json:"seq"`
	Den        Den         `json:"den"`
	Me         Member      `json:"me"`
	Members    []Member    `json:"members"`
	Groups     []Group     `json:"groups"`
	Channels   []Channel   `json:"channels"`
	ReadStates []ReadState `json:"read_states"`
	Online     []string    `json:"online"`
	// M1.7: the member's seal check, the keys of their DMs without their
	// exchanges, and sign-ins waiting for their approval.
	SealCheck      Bytes           `json:"seal_check"`
	DMKeys         []DMKey         `json:"dm_keys"`
	DeviceRequests []DeviceRequest `json:"device_requests"`
	// M2: the calls the member can see that have someone in them.
	Calls []Call `json:"calls"`
}

type Renew struct {
	Nonce Bytes `json:"nonce"`
	Proof Bytes `json:"proof"`
}

type Renewed struct {
	ExpiresAt int64 `json:"expires_at"`
}
