package denclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// SignInRequest is what a member enters to sign in to a den they're in
// already, from a machine new to it: the den, as any invite from it or its
// address; their username; and their den password, or a recovery code and
// the new password it sets.
type SignInRequest struct {
	Den          string
	Username     string
	Password     string
	RecoveryCode string
}

// SignedIn is this device's sign-in to a den it's new to: the den's
// status, how many recovery codes the member has left, and how many of
// their other devices a recovery signed out.
type SignedIn struct {
	Den               Status `json:"den"`
	RecoveryCodesLeft int    `json:"recovery_codes_left"`
	SignedOut         int    `json:"signed_out"`
}

// MovedError is a den that moved to an address that can't be reached from
// here, so this install can't follow it yet.
type MovedError struct {
	URL string
	Err error
}

func (e *MovedError) Error() string {
	return fmt.Sprintf("the den moved to %s, which can't be reached: %v", e.URL, e.Err)
}
func (e *MovedError) Unwrap() error { return e.Err }

// SignIn signs this machine in to a den as a member already in it, with a
// new device key. An invite from the den, used or not, pins its identity;
// an address trusts the key the den proves there, which is safe for the
// password, since a verifier made for one den is worthless at another.
// Recovering with a code sets a new password, which signs out the member's
// other devices.
func (m *Manager) SignIn(ctx context.Context, req SignInRequest) (SignedIn, error) {
	username, err := denproto.NormalizeUsername(req.Username)
	if err != nil {
		return SignedIn{}, inputError(err)
	}
	if err := vault.ValidatePassword(req.Password); err != nil {
		return SignedIn{}, inputError(err)
	}
	var code []byte
	if req.RecoveryCode != "" {
		if code, err = denproto.ParseRecoveryCode(req.RecoveryCode); err != nil {
			return SignedIn{}, inputError(err)
		}
	}
	var denID []byte
	target := strings.TrimSpace(req.Den)
	url := target
	if strings.HasPrefix(target, "dens1:") {
		inv, err := denproto.DecodeInvite(target)
		if err != nil {
			return SignedIn{}, inputError(err)
		}
		denID, url = inv.DenID, inv.URL
	} else if url, err = denproto.NormalizeDenURL(target); err != nil {
		return SignedIn{}, inputError(errors.New("enter an invite from the den, or its address, such as https://den.example.com"))
	}
	// The den this install hosts is reached on loopback, since its public
	// address need not loop back from the machine behind it.
	if info, _, ok := m.own(); ok && denID == nil && info.URL == url {
		denID = info.ID
	}
	m.mu.Lock()
	started := m.ctx != nil
	m.mu.Unlock()
	if !started {
		return SignedIn{}, ErrNotStarted
	}
	m.joinMu.Lock()
	defer m.joinMu.Unlock()

	a := &api{base: url, client: m.HTTP, agent: m.agent}
	if denID != nil {
		a, _ = m.apiFor(denID, url)
	}
	a, nonce, id, err := a.reach(ctx, denID)
	if err != nil {
		return SignedIn{}, err
	}
	if !a.own {
		url = a.base
	}
	if c, err := m.find(id.String()); err == nil {
		if !gone(c) {
			return SignedIn{}, ErrAlreadyJoined
		}
		// This device is out of the den; signing in again replaces it.
		if err := m.forget(ctx, c); err != nil {
			return SignedIn{}, err
		}
	}
	key, err := vault.GenerateSigningKey()
	if err != nil {
		return SignedIn{}, err
	}
	keyID := denproto.ID(key.Public())
	proof := key.Sign(denproto.ProofMessage(id, keyID, nonce))
	verifier := denproto.Verifier(req.Password, id, username)
	var resp denproto.SignInResponse
	if code != nil {
		err = a.call(ctx, http.MethodPost, "/api/auth/recover", nil, denproto.RecoverRequest{
			Username: username, RecoveryCode: denproto.FormatRecoveryCode(code), NewVerifier: verifier,
			PublicKey: denproto.Bytes(key.Public()), DeviceLabel: m.label, Nonce: nonce, Proof: proof,
		}, &resp)
	} else {
		err = a.call(ctx, http.MethodPost, "/api/auth/password", nil, denproto.PasswordLoginRequest{
			Username: username, Verifier: verifier,
			PublicKey: denproto.Bytes(key.Public()), DeviceLabel: m.label, Nonce: nonce, Proof: proof,
		}, &resp)
	}
	if err != nil {
		key.Close()
		return SignedIn{}, err
	}
	member, ok := cleanMember(resp.Member)
	name, nameErr := denproto.CleanName(resp.Den.Name, denproto.MaxNameRunes)
	if !ok || member.Username != username || nameErr != nil || !denproto.Equal(resp.Den.ID, id) ||
		denproto.Size("token", resp.Token, denproto.TokenSize) != nil || resp.RecoveryCodesLeft < 0 || resp.RecoveryCodesLeft > denproto.RecoveryCodes ||
		resp.SignedOut < 0 || resp.SignedOut > maxDevices {
		key.Close()
		return SignedIn{}, errors.New("the den's answer to signing in is malformed")
	}
	j := &joined{denID: id, profile: Profile{URL: url, Name: name, Member: member}, key: key, joinedAt: time.Now()}
	if err := insertJoined(ctx, m.db, m.v, j); err != nil {
		key.Close()
		return SignedIn{}, err
	}
	m.mu.Lock()
	m.startLocked(j, resp.Token, time.UnixMilli(resp.ExpiresAt))
	c := m.conns[len(m.conns)-1]
	m.mu.Unlock()
	m.notify()
	m.log.Infof("Signed in to a den on this device")
	return SignedIn{Den: c.status(), RecoveryCodesLeft: resp.RecoveryCodesLeft, SignedOut: resp.SignedOut}, nil
}

// ChangePassword sets this member's den password, proven with the current
// one or with a recovery code, which signs out their other devices. It
// returns how many codes are left, and how many devices it signed out.
func (m *Manager) ChangePassword(ctx context.Context, denID, current, recoveryCode, next string) (denproto.PasswordChanged, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.PasswordChanged{}, err
	}
	if err := vault.ValidatePassword(next); err != nil {
		return denproto.PasswordChanged{}, inputError(err)
	}
	username := c.status().Username
	req := denproto.PasswordChangeRequest{NewVerifier: denproto.Verifier(next, c.j.denID, username)}
	if recoveryCode != "" {
		code, err := denproto.ParseRecoveryCode(recoveryCode)
		if err != nil {
			return denproto.PasswordChanged{}, inputError(err)
		}
		req.RecoveryCode = denproto.FormatRecoveryCode(code)
	} else {
		req.Verifier = denproto.Verifier(current, c.j.denID, username)
	}
	var out denproto.PasswordChanged
	if err := c.call(ctx, http.MethodPost, "/api/me/password", req, &out); err != nil {
		return denproto.PasswordChanged{}, err
	}
	if out.RecoveryCodesLeft < 0 || out.RecoveryCodesLeft > denproto.RecoveryCodes || out.SignedOut < 0 || out.SignedOut > maxDevices {
		return denproto.PasswordChanged{}, errors.New("the den's answer is malformed")
	}
	return out, nil
}

// NewRecoveryCodes replaces this member's recovery codes, given their den
// password, and returns the new ones, which are shown once.
func (m *Manager) NewRecoveryCodes(ctx context.Context, denID, password string) ([]string, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	req := denproto.RecoveryCodesRequest{Verifier: denproto.Verifier(password, c.j.denID, c.status().Username)}
	var list denproto.RecoveryCodeList
	if err := c.call(ctx, http.MethodPost, "/api/me/recovery-codes", req, &list); err != nil {
		return nil, err
	}
	if len(list.RecoveryCodes) != denproto.RecoveryCodes {
		return nil, errors.New("the den's answer is malformed")
	}
	for _, code := range list.RecoveryCodes {
		if _, err := denproto.ParseRecoveryCode(code); err != nil {
			return nil, errors.New("the den's answer is malformed")
		}
	}
	return list.RecoveryCodes, nil
}

// maxDevices bounds a device list from a den.
const maxDevices = 1000

// Devices lists this member's devices on a den.
func (m *Manager) Devices(ctx context.Context, denID string) (denproto.DeviceList, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.DeviceList{}, err
	}
	var list denproto.DeviceList
	if err := c.call(ctx, http.MethodGet, "/api/me/devices", nil, &list); err != nil {
		return list, err
	}
	if len(list.Devices) > maxDevices || list.RecoveryCodesLeft < 0 || list.RecoveryCodesLeft > denproto.RecoveryCodes {
		return list, errors.New("the den's answer is malformed")
	}
	for _, d := range list.Devices {
		if denproto.CheckDevice(d) != nil {
			return list, errors.New("the den's answer is malformed")
		}
	}
	if list.Devices == nil {
		list.Devices = []denproto.Device{}
	}
	return list, nil
}

// RevokeDevice removes one of this member's device keys from a den, which
// signs that device out at once.
func (m *Manager) RevokeDevice(ctx context.Context, denID, keyID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	key, err := denproto.ParseBytes(keyID, denproto.IDSize)
	if err != nil {
		return inputError(errors.New("invalid device key"))
	}
	return c.call(ctx, http.MethodDelete, "/api/me/devices/"+key.String(), nil, nil)
}
