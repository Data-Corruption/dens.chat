package denproto

import (
	"errors"
	"slices"
)

// Private DMs and device approval (M1.7): what the den relays and keeps.
// The cryptography is in e2e.go; the den never holds a key that opens any
// of it.

// Events for DM keys and device approval.
const (
	// EventDMKey is a DM key that changed, as the member sees it: its
	// exchange moved on, someone checked it, or it was retired. Each of the
	// DM's two members gets their own view.
	EventDMKey = "dm.key"
	// EventDeviceRequest is a sign-in waiting for approval, new or moved
	// on, sent to the member's own sessions.
	EventDeviceRequest = "device.request"
	// EventDeviceRequestEnded is one that was approved, refused or
	// cancelled.
	EventDeviceRequestEnded = "device.request_ended"
)

// Error codes for M1.7.
const (
	// CodeKeyExists (409): the DM has a key both members checked, or an
	// exchange under way, so another can't start.
	CodeKeyExists = "key_exists"
	// CodeNoOtherDevice (403): a password sign-in needs one of the
	// member's other devices to approve it, and they have none; a recovery
	// code signs in instead.
	CodeNoOtherDevice = "no_other_device"
)

// Stages of a DM key's exchange.
const (
	StageOffered  = "offered"  // the starter's offer is waiting for an answer
	StageAnswered = "answered" // the answer is waiting for the starter's reveal
	StageRevealed = "revealed" // both sides can work out the key and the check code
)

// Why a DM key was retired.
const (
	RetiredStartedOver = "started_over" // a member started over with a new seal
	RetiredRestarted   = "restarted"    // a member started the exchange again
)

// Statuses of a sign-in waiting for approval, as the new device sees them.
const (
	PendingWaiting   = "waiting"   // for one of the member's devices to answer
	PendingAnswered  = "answered"  // the new device reveals, and both check
	PendingApproved  = "approved"  // the session and the handed-over seal are here
	PendingRefused   = "refused"   // the member refused it
	PendingCancelled = "cancelled" // the member's password or seal changed meanwhile
)

// Sizes for M1.7.
const (
	// SealCheckSize is a seal check's length: an HMAC-SHA-256.
	SealCheckSize = 32
	// MaxExchangeState bounds one side's sealed exchange state.
	MaxExchangeState = 8 << 10
	// SealedKeySize is a sealed DM key: a nonce, the key, and a tag.
	SealedKeySize = 24 + DMKeySize + 16
	// HandoverSize is a handed-over seal, sealed the same way.
	HandoverSize = 24 + SealSize + 16
	// MaxDMFiles bounds the sealed uploads on one DM message: each file,
	// and its preview.
	MaxDMFiles = 2 * MaxAttachments
)

// DMKey is one of a DM's keys, as one of its members sees it. Messages
// name the key they're sealed with. Stage says how far its exchange has
// come; Exchange carries it, to its two members, only while it runs.
// Checks list the members who compared check codes and stored their copy,
// and Sealed is the requesting member's own copy. A retired key takes no
// new messages.
type DMKey struct {
	ID        string       `json:"id"`
	ChannelID string       `json:"channel_id"`
	StartedBy string       `json:"started_by"`
	StartedAt int64        `json:"started_at"`
	Stage     string       `json:"stage"`
	Checks    []DMKeyCheck `json:"checks,omitempty"`
	RetiredAt int64        `json:"retired_at,omitempty"`
	RetiredBy string       `json:"retired_by,omitempty"`
	Retired   string       `json:"retired,omitempty"`
	Sealed    Bytes        `json:"sealed,omitempty"`
	Exchange  *DMExchange  `json:"exchange,omitempty"`
}

// DMKeyCheck is a member's check of a DM key, and when they made it.
type DMKeyCheck struct {
	MemberID string `json:"member_id"`
	At       int64  `json:"at"`
}

// DMExchange is a DM key's exchange while it runs: its three messages as
// far as they've come, and the requesting member's own sealed state.
type DMExchange struct {
	Offer  ExchangeOffer   `json:"offer"`
	Answer *ExchangeAnswer `json:"answer,omitempty"`
	Reveal *ExchangeReveal `json:"reveal,omitempty"`
	State  Bytes           `json:"state,omitempty"`
}

type DMKeyList struct {
	Keys []DMKey `json:"keys"`
}

// Checked reports whether a member checked the key.
func (k DMKey) Checked(member string) bool {
	return slices.ContainsFunc(k.Checks, func(c DMKeyCheck) bool { return c.MemberID == member })
}

// KeyStartRequest starts a DM's key: the starter's offer, and their state
// sealed with their seal. Restart retires a key whose exchange is under
// way, or that only one member checked, to start again.
type KeyStartRequest struct {
	Offer   ExchangeOffer `json:"offer"`
	State   Bytes         `json:"state"`
	Restart bool          `json:"restart,omitempty"`
}

// KeyAnswerRequest answers a DM key's offer, with the answering member's
// state sealed with their seal.
type KeyAnswerRequest struct {
	Answer ExchangeAnswer `json:"answer"`
	State  Bytes          `json:"state"`
}

type KeyRevealRequest struct {
	Reveal ExchangeReveal `json:"reveal"`
}

// KeySealRequest stores the member's copy of a DM key, sealed with their
// seal, once they checked the code.
type KeySealRequest struct {
	Sealed Bytes `json:"sealed"`
}

// StartOverRequest replaces the member's DM seal with a new one: the
// password's verifier, and the new seal's check.
type StartOverRequest struct {
	Verifier  Bytes `json:"verifier"`
	SealCheck Bytes `json:"seal_check"`
}

// StartedOver says how many of the member's other devices starting over
// signed out.
type StartedOver struct {
	SignedOut int `json:"signed_out"`
}

// PendingSignIn answers a password sign-in that waits for approval: the
// token the new device asks after it with, and when it lapses.
type PendingSignIn struct {
	PendingToken Bytes `json:"pending_token"`
	ExpiresAt    int64 `json:"expires_at"`
}

// PendingStatus is where a waiting sign-in stands, for the new device.
// Version grows with each change. Answer comes once one of the member's
// devices answered the offer; Session and Handover once it approved.
type PendingStatus struct {
	Version  int64           `json:"version"`
	Status   string          `json:"status"`
	Answer   *ExchangeAnswer `json:"answer,omitempty"`
	Session  *SignInResponse `json:"session,omitempty"`
	Handover Bytes           `json:"handover,omitempty"`
}

// DeviceRequest is a sign-in waiting for approval, as the member's own
// devices see it. AnsweredBy is the key ID of the device that answered,
// which alone may approve it.
type DeviceRequest struct {
	ID          Bytes           `json:"id"`
	KeyID       Bytes           `json:"key_id"`
	Label       string          `json:"label"`
	RequestedAt int64           `json:"requested_at"`
	ExpiresAt   int64           `json:"expires_at"`
	Offer       ExchangeOffer   `json:"offer"`
	Answer      *ExchangeAnswer `json:"answer,omitempty"`
	AnsweredBy  Bytes           `json:"answered_by,omitempty"`
	Reveal      *ExchangeReveal `json:"reveal,omitempty"`
}

// DeviceRequestEnded says a sign-in stopped waiting: approved, refused or
// cancelled.
type DeviceRequestEnded struct {
	ID      Bytes  `json:"id"`
	Outcome string `json:"outcome"`
}

type DeviceAnswerRequest struct {
	Answer ExchangeAnswer `json:"answer"`
}

// ApproveRequest approves a sign-in: the member's seal, sealed with the
// exchange's key for the new device.
type ApproveRequest struct {
	Handover Bytes `json:"handover"`
}

// CheckDMKey checks a DM key from a den before a client uses it. Its
// cryptography is checked where it's used.
func CheckDMKey(k DMKey) error {
	for _, id := range []string{k.ID, k.ChannelID, k.StartedBy} {
		if _, err := ParseID(id); err != nil {
			return errors.New("DM key has an invalid ID")
		}
	}
	switch k.Stage {
	case StageOffered, StageAnswered, StageRevealed:
	default:
		return errors.New("DM key has an invalid stage")
	}
	if len(k.Checks) > 2 {
		return errors.New("DM key has too many checks")
	}
	for i, c := range k.Checks {
		if _, err := ParseID(c.MemberID); err != nil || k.Stage != StageRevealed ||
			slices.ContainsFunc(k.Checks[:i], func(o DMKeyCheck) bool { return o.MemberID == c.MemberID }) {
			return errors.New("DM key has an invalid check")
		}
	}
	if (k.RetiredAt == 0) != (k.Retired == "") || (k.RetiredBy != "") != (k.Retired != "") {
		return errors.New("DM key is retired oddly")
	}
	if k.Retired != "" {
		if _, err := ParseID(k.RetiredBy); err != nil || (k.Retired != RetiredStartedOver && k.Retired != RetiredRestarted) {
			return errors.New("DM key is retired oddly")
		}
	}
	if k.Sealed != nil && len(k.Sealed) != SealedKeySize {
		return errors.New("DM key has an invalid sealed copy")
	}
	if x := k.Exchange; x != nil {
		if err := x.Offer.Check(); err != nil {
			return err
		}
		if (x.Answer != nil) != (k.Stage != StageOffered) || (x.Reveal != nil) != (k.Stage == StageRevealed) {
			return errors.New("DM key's exchange doesn't fit its stage")
		}
		if x.Answer != nil {
			if err := x.Answer.Check(); err != nil {
				return err
			}
		}
		if x.Reveal != nil {
			if err := x.Reveal.Check(); err != nil {
				return err
			}
		}
		if len(x.State) > MaxExchangeState {
			return errors.New("DM key's state is too large")
		}
	}
	return nil
}

// CheckDeviceRequest checks a sign-in request from a den before a client
// shows it.
func CheckDeviceRequest(r DeviceRequest) error {
	if len(r.ID) != RequestIDSize || len(r.KeyID) != IDSize || r.RequestedAt < 0 || r.ExpiresAt < r.RequestedAt {
		return errors.New("device request has an invalid ID or time")
	}
	if err := CheckDevice(Device{KeyID: r.KeyID, Label: r.Label}); err != nil {
		return err
	}
	if err := r.Offer.Check(); err != nil {
		return err
	}
	if r.Answer != nil {
		if err := r.Answer.Check(); err != nil || len(r.AnsweredBy) != IDSize {
			return errors.New("device request has an invalid answer")
		}
	}
	if r.Reveal != nil {
		if err := r.Reveal.Check(); err != nil || r.Answer == nil {
			return errors.New("device request has an invalid reveal")
		}
	}
	return nil
}

// RequestIDSize is a device request's ID length.
const RequestIDSize = 16
