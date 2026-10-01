package denproto

import (
	"errors"
	"unicode/utf8"
)

// Events about a member's own devices, which only their sessions get.
const (
	EventDeviceAdded   = "device.added"
	EventDeviceRemoved = "device.removed"
)

// PasswordLoginRequest signs in on a new device with the den password: the
// member's verifier, and the new device's key with its proof over a nonce.
// Offer starts the exchange with the device that approves it (M1.7).
type PasswordLoginRequest struct {
	Username    string        `json:"username"`
	Verifier    Bytes         `json:"verifier"`
	PublicKey   Bytes         `json:"public_key"`
	DeviceLabel string        `json:"device_label"`
	Nonce       Bytes         `json:"nonce"`
	Proof       Bytes         `json:"proof"`
	Offer       ExchangeOffer `json:"offer"`
}

// RecoverRequest signs in on a new device with a recovery code instead of
// the password, and sets a new password.
type RecoverRequest struct {
	Username     string `json:"username"`
	RecoveryCode string `json:"recovery_code"`
	NewVerifier  Bytes  `json:"new_verifier"`
	PublicKey    Bytes  `json:"public_key"`
	DeviceLabel  string `json:"device_label"`
	Nonce        Bytes  `json:"nonce"`
	Proof        Bytes  `json:"proof"`
}

// SignInResponse starts a new device's session, and says which den and
// member it is, how many recovery codes the member has left, and how many
// of their other devices a recovery signed out. SealCheck identifies the
// member's DM seal, so the device can check the one it's given or typed.
type SignInResponse struct {
	Token             Bytes  `json:"token"`
	ExpiresAt         int64  `json:"expires_at"`
	Den               Den    `json:"den"`
	Member            Member `json:"member"`
	RecoveryCodesLeft int    `json:"recovery_codes_left"`
	SignedOut         int    `json:"signed_out,omitempty"`
	SealCheck         Bytes  `json:"seal_check"`
}

// PasswordChangeRequest changes the den password. The member proves it's
// them with the current password's verifier, or a recovery code, which it
// uses up. Every other device of theirs is signed out.
type PasswordChangeRequest struct {
	Verifier     Bytes  `json:"verifier,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
	NewVerifier  Bytes  `json:"new_verifier"`
}

// RecoveryCodesRequest replaces the member's recovery codes. It takes the
// password, so a stolen session can't make codes to take the account with.
type RecoveryCodesRequest struct {
	Verifier Bytes `json:"verifier"`
}

type RecoveryCodeList struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// PasswordChanged says how many recovery codes are left, and how many of
// the member's other devices the new password signed out.
type PasswordChanged struct {
	RecoveryCodesLeft int `json:"recovery_codes_left"`
	SignedOut         int `json:"signed_out"`
}

// Device is one of a member's keys. Current marks the session's own.
type Device struct {
	KeyID      Bytes  `json:"key_id"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	Current    bool   `json:"current,omitempty"`
}

type DeviceList struct {
	Devices           []Device `json:"devices"`
	RecoveryCodesLeft int      `json:"recovery_codes_left"`
}

type DeviceRemoved struct {
	KeyID Bytes `json:"key_id"`
}

// CheckDevice checks a device from a den before a client shows it.
func CheckDevice(d Device) error {
	if len(d.KeyID) != IDSize || d.CreatedAt < 0 || d.LastSeenAt < 0 {
		return errors.New("device has an invalid key or time")
	}
	if name, err := CleanName(d.Label, MaxLabelRunes); err != nil || name != d.Label || !utf8.ValidString(d.Label) {
		return errors.New("device has an invalid label")
	}
	return nil
}
