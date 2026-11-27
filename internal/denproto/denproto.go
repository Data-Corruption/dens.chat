// Package denproto is the client-to-den protocol shared by both sides: wire
// types, the byte layouts that get signed, password verifiers, invite
// strings, name rules and the rule that takes tracking out of links.
// docs/dev/protocol.md and the parts under docs/dev/protocol describe it;
// keeping one implementation for the den and the client means the two
// can't drift.
//
// It holds every rule both sides apply alike: the cryptography of private
// DMs and device approval; members' identities, their codes, the DM keys
// sealed to them and the record of those kept (identity.go); a sealed
// call's keys, their copies, commitments and statements (callkeys.go);
// the times after which a DM's messages disappear (disappearing.go); the
// key a member's notes are sealed under, made from their seal (notes.go);
// the boxes between a member's devices for Relay, their key and their seal
// (relay.go); what of a poll each member sees; events' limits, wire types and checks, and
// badges (events.go); a name's style, its fonts and its checks
// (namestyle.go); and sign-in codes, Add device's and the digits', made and
// read by one grammar (signincode.go). emojilist.go, the list of Unicode's
// emoji, is written by scripts/emoji.sh.
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

// Protocol versions this build speaks. Version 2 brought group DMs, which
// are gone, so a den serves 1 and 2 alike, and version 3 notes to self,
// which it sends nothing of to a client speaking 1 or 2.
const (
	Version    = 3
	MinVersion = 1
	// GroupsVersion is the version that brought group DMs, which nothing
	// asks of any more.
	GroupsVersion = 2
	// NotesVersion is the first version with notes to self.
	NotesVersion = 3
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

// Error codes. Status codes pair with them as docs/dev/protocol.md's Errors
// has it.
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

// codes are every error code the protocol has.
var codes = map[string]bool{
	CodeMalformed: true, CodeInvalidField: true, CodeUnauthorized: true, CodeBadSignature: true, CodeBadNonce: true,
	CodeForbidden: true, CodeBanned: true, CodeKeyRevoked: true, CodeNotFound: true, CodeUsernameTaken: true,
	CodeEditConflict: true, CodeInviteInvalid: true, CodeTooLarge: true, CodeProtocolUnsupported: true,
	CodeRateLimited: true, CodeDenNotCreated: true, CodeWrongPassword: true, CodeUnsupportedType: true,
	CodeQuotaExceeded: true, CodeDenFull: true, CodeKeyExists: true, CodeNoOtherDevice: true, CodeChangesGone: true,
	CodeSoundNameTaken: true, CodeEmojiNameTaken: true, CodeCallHeld: true, CodeCallFull: true, CodeRoleNameTaken: true,
}

// KnownCode reports whether code is one of the protocol's error codes. A
// den's code is its own text, so the client shows a member only the codes
// it knows.
func KnownCode(code string) bool { return codes[code] }

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
		// Both are the den's own text, which logs show.
		if len(parsed.Error.Code) > 64 {
			parsed.Error.Code = parsed.Error.Code[:64]
		}
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
	// Limits, CallLimits, Retention, SoundLimits, EmojiLimits and
	// EventLimits come to members, in ready and den.updated, and not in a
	// join preview. Retention is the days a message is kept, 0 for ever.
	// A den older than v1.7.0 sends no SoundLimits, the soundboard's, one
	// older than v1.8.0 no EmojiLimits, and one older than v1.10.0 no
	// EventLimits. Emoji turned off are a count of 0, which a den sends as
	// it is, so it needs telling from a den older than emoji, which sends
	// none; and a den sends EventLimits to say it has events at all.
	Limits      Limits       `json:"limits,omitzero"`
	CallLimits  CallLimits   `json:"call_limits,omitzero"`
	Retention   int          `json:"retention,omitempty"`
	SoundLimits SoundLimits  `json:"sound_limits,omitzero"`
	EmojiLimits *EmojiLimits `json:"emoji_limits,omitempty"`
	EventLimits *EventLimits `json:"event_limits,omitempty"`
	// SealedCalls says the den seals every call end to end, and
	// takes no join that doesn't; a den older than it doesn't say so.
	SealedCalls bool `json:"sealed_calls,omitempty"`
	// Disappearing says the den deletes a DM's messages at the time its
	// members set; a den older than it doesn't.
	Disappearing bool `json:"disappearing,omitempty"`
	// Spoilers says the den keeps which of a channel's files went as
	// spoilers, and MoveMembers that staff move members from one call to
	// another; a den older than them says neither.
	Spoilers    bool `json:"spoilers,omitempty"`
	MoveMembers bool `json:"move_members,omitempty"`
	// NameStyles says the den keeps a style for each member's name; a den
	// older than them drops one a member sets.
	NameStyles bool `json:"name_styles,omitempty"`
	// Away says the den keeps a member's Away in their call; a den older
	// than it drops the mark.
	Away bool `json:"away,omitempty"`
	// ReadAll says the den marks every channel and DM a member sees read
	// at once, at POST /api/read; a den older than it has no such route.
	ReadAll bool `json:"read_all,omitempty"`
	// Relay says the den passes boxes between a member's devices, so
	// their installs ask only a den that does.
	Relay bool `json:"relay,omitempty"`
	// AuditLog says the den keeps staff's acts for staff to read, at GET
	// /api/audit, and MembersSeen that it tells staff when each member was
	// last seen, at GET /api/members/seen; a den older than them has
	// neither route.
	AuditLog    bool `json:"audit_log,omitempty"`
	MembersSeen bool `json:"members_seen,omitempty"`
	// Forward says the den keeps a channel's message's forwarded mark; a
	// den older than it drops the mark.
	Forward bool `json:"forward,omitempty"`
	// DeviceNames says a device may change its own label, at PATCH
	// /api/me/devices/{key_id}, and Status that members set a status, at
	// PUT /api/me/status; a den older than them has neither route.
	DeviceNames bool `json:"device_names,omitempty"`
	Status      bool `json:"status,omitempty"`
	// Roles says the den supports custom roles for channel visibility; an
	// older den has no role routes and sends none.
	Roles bool `json:"roles,omitempty"`
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
// Identity is the hash of the identity their Dens published, as
// base64url, which a change to it reaches everyone with; none before it
// published one, as from an older Dens. NameStyle is how their name
// shows, left out when they set none. Status is a line they set, shown
// until StatusUntil, in unix milliseconds, or with none until they clear
// it; a den leaves out one whose end has passed.
type Member struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	JoinedAt    int64     `json:"joined_at"`
	LeftAt      int64     `json:"left_at,omitempty"`
	Bio         string    `json:"bio,omitempty"`
	Avatar      Image     `json:"avatar,omitzero"`
	Banner      Image     `json:"banner,omitzero"`
	Identity    string    `json:"identity,omitempty"`
	NameStyle   NameStyle `json:"name_style,omitzero"`
	Status      string    `json:"status,omitempty"`
	StatusUntil int64     `json:"status_until,omitempty"`
	// RoleIDs are the custom roles this member holds that the viewer can
	// see, sorted by numeric ID.
	RoleIDs []string `json:"role_ids,omitempty"`
}

// MemberProfile is a member as GET /api/members/{id} answers, for their
// profile card: their record with their bio, and the badges they earned
// whose events the asker sees, latest first, which travel with the
// card alone, as the bio does.
type MemberProfile struct {
	Member
	Badges []MemberBadge `json:"badges,omitempty"`
}

// IdentityHash returns the hash of the member's identity, or nil when they
// have none or it's malformed.
func (m Member) IdentityHash() Bytes {
	if m.Identity == "" {
		return nil
	}
	b, err := ParseBytes(m.Identity, IdentityHashSize)
	if err != nil {
		return nil
	}
	return b
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
	// SealCheck identifies the DM seal the joining member's client holds.
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
	// Retention is the days a message is kept, or 0 to keep them all.
	Retention   *int         `json:"retention,omitempty"`
	SoundLimits *SoundLimits `json:"sound_limits,omitempty"`
	EmojiLimits *EmojiLimits `json:"emoji_limits,omitempty"`
	EventLimits *EventLimits `json:"event_limits,omitempty"`
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
	// The member's seal check, the keys of their DMs without their
	// exchanges, and sign-ins waiting for their approval.
	SealCheck      Bytes           `json:"seal_check"`
	DMKeys         []DMKey         `json:"dm_keys"`
	DeviceRequests []DeviceRequest `json:"device_requests"`
	// The calls the member can see that have someone in them.
	Calls []Call `json:"calls"`
	// Where the den's changes stand, for a client to check what it
	// cached against.
	Sync Sync `json:"sync"`
	// The den's sounds, by name.
	Sounds []Sound `json:"sounds"`
	// The den's emoji, by name.
	Emoji []Emoji `json:"emoji"`
	// The keys of the member's DMs sealed to identities, each with
	// their own copy. A den older than v1.9.0 sends none, which tells a
	// client it keeps no identities.
	SignedKeys []SignedKey `json:"signed_keys"`
	// The den's events to come and live that the member sees, live
	// first and then by their start, each with the member's own mark.
	Events []DenEvent `json:"events"`
	// Roles are the custom roles the member can see: all of them for staff,
	// or only those referenced by visible channels for non-staff, by name.
	Roles []Role `json:"roles,omitempty"`
}

type Renew struct {
	Nonce Bytes `json:"nonce"`
	Proof Bytes `json:"proof"`
}

type Renewed struct {
	ExpiresAt int64 `json:"expires_at"`
}
