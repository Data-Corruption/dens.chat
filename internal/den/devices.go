package den

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A member's devices, and the ways to sign in without one: the den password
// on a new device, and a recovery code when the password is forgotten.
// Every new device is announced to the member's other sessions, which is
// what makes a stolen password noticeable, and every new password signs out
// the devices the old one may have let in.

// errSignIn is every failed password sign-in or recovery, so neither says
// whether the username exists.
var errSignIn = denproto.Errorf(http.StatusUnauthorized, denproto.CodeUnauthorized, "the username, password or recovery code is wrong")

var errWrongPassword = denproto.Errorf(http.StatusForbidden, denproto.CodeWrongPassword, "the password or recovery code is wrong")

// activeMember finds a member who can sign in: in the den now, and not
// banned. It returns their ID and their verifier's hash.
func activeMember(ctx context.Context, q querier, username string) (int64, []byte, error) {
	var id int64
	var hash []byte
	err := q.QueryRowContext(ctx, `SELECT id, verifier_hash FROM den_members
		WHERE username = ? AND left_at IS NULL AND banned_at IS NULL`, username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, errSignIn
	}
	return id, hash, err
}

// signInFields checks what every new-device sign-in carries, and proves the
// device holds its key.
func (d *Den) signInFields(username, label string, verifier, pub, nonce, proof denproto.Bytes) (string, string, error) {
	for _, f := range []struct {
		name  string
		value denproto.Bytes
		size  int
	}{{"verifier", verifier, denproto.VerifierSize}, {"public_key", pub, denproto.PublicKeySize}} {
		if err := denproto.Size(f.name, f.value, f.size); err != nil {
			return "", "", err
		}
	}
	name, err := denproto.NormalizeUsername(username)
	if err != nil {
		return "", "", errSignIn
	}
	label, err = denproto.CleanName(label, denproto.MaxLabelRunes)
	if err != nil {
		return "", "", invalid("device_label: %v", err)
	}
	// Guesses are limited per username as well as per address, so a
	// password can't be guessed from many addresses at once.
	if err := d.Allow(LimitPasswordName, name); err != nil {
		return "", "", err
	}
	if err := d.checkProof(pub, nonce, proof); err != nil {
		return "", "", err
	}
	return name, label, nil
}

// addDevice registers a member's new key within tx and starts its session.
func addDevice(ctx context.Context, tx *sql.Tx, member int64, pub []byte, label string, token []byte, now, expires time.Time) (denproto.Device, error) {
	keyID := denproto.ID(ed25519.PublicKey(pub))
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO den_devices (key_id, member_id, public_key, label, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		[]byte(keyID), member, pub, label, now.UnixMilli(), now.UnixMilli()); err != nil {
		return denproto.Device{}, invalid("that device key is already registered")
	}
	if err := insertSession(ctx, tx, token, member, keyID, now, expires); err != nil {
		return denproto.Device{}, err
	}
	return denproto.Device{KeyID: keyID, Label: label, CreatedAt: now.UnixMilli(), LastSeenAt: now.UnixMilli()}, nil
}

// signOutOthers deletes every device key of a member's but keep, with
// their sessions, within q, and returns the keys it deleted.
func signOutOthers(ctx context.Context, q querier, member int64, keep []byte) ([][]byte, error) {
	rows, err := q.QueryContext(ctx, `DELETE FROM den_devices WHERE member_id = ? AND key_id != ? RETURNING key_id`, member, keep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys [][]byte
	for rows.Next() {
		var key []byte
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// devicesGone closes the sockets of device keys that were just deleted,
// giving reason, and tells the member's other sessions.
func (d *Den) devicesGone(member int64, keys [][]byte, reason string) error {
	var errs []error
	for _, key := range keys {
		d.sockets.closeKey(key, denproto.CloseRevoked, reason)
		errs = append(errs, d.Hub.Publish(denproto.EventDeviceRemoved, denproto.DeviceRemoved{KeyID: key}, OnlyMember(member)))
	}
	return errors.Join(errs...)
}

func codesLeft(ctx context.Context, q querier, member int64) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM den_recovery_codes WHERE member_id = ?`, member).Scan(&n)
	return n, err
}

// signedIn finishes a new device's sign-in: it tells the member's other
// sessions and answers with the session.
func (d *Den) signedIn(ctx context.Context, member int64, dev denproto.Device, token []byte, expires time.Time, left int) (denproto.SignInResponse, error) {
	m, err := d.Member(ctx, member)
	if err != nil {
		return denproto.SignInResponse{}, err
	}
	info, _ := d.Info()
	d.log.Infof("Member %d signed in on a new device", member)
	resp := denproto.SignInResponse{Token: token, ExpiresAt: expires.UnixMilli(), Den: info, Member: m, RecoveryCodesLeft: left}
	return resp, d.Hub.Publish(denproto.EventDeviceAdded, dev, OnlyMember(member))
}

// PasswordLogin registers a new device for a member who proves their den
// password, and starts its session.
func (d *Den) PasswordLogin(ctx context.Context, req denproto.PasswordLoginRequest) (denproto.SignInResponse, error) {
	username, label, err := d.signInFields(req.Username, req.DeviceLabel, req.Verifier, req.PublicKey, req.Nonce, req.Proof)
	if err != nil {
		return denproto.SignInResponse{}, err
	}
	now := d.now()
	expires := now.Add(d.TokenLifetime)
	token := denproto.Random(denproto.TokenSize)
	var member int64
	var dev denproto.Device
	var left int
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var hash []byte
		var err error
		if member, hash, err = activeMember(ctx, tx, username); err != nil {
			return err
		}
		if !denproto.Equal(hash, denproto.Hash(req.Verifier)) {
			return errSignIn
		}
		if dev, err = addDevice(ctx, tx, member, req.PublicKey, label, token, now, expires); err != nil {
			return err
		}
		left, err = codesLeft(ctx, tx, member)
		return err
	})
	if err != nil {
		return denproto.SignInResponse{}, err
	}
	return d.signedIn(ctx, member, dev, token, expires, left)
}

// Recover signs a member in on a new device with one of their recovery
// codes, which it uses up, and sets their new password, which signs out
// every other device of theirs.
func (d *Den) Recover(ctx context.Context, req denproto.RecoverRequest) (denproto.SignInResponse, error) {
	username, label, err := d.signInFields(req.Username, req.DeviceLabel, req.NewVerifier, req.PublicKey, req.Nonce, req.Proof)
	if err != nil {
		return denproto.SignInResponse{}, err
	}
	code, err := denproto.ParseRecoveryCode(req.RecoveryCode)
	if err != nil {
		return denproto.SignInResponse{}, errSignIn
	}
	now := d.now()
	expires := now.Add(d.TokenLifetime)
	token := denproto.Random(denproto.TokenSize)
	var member int64
	var dev denproto.Device
	var left int
	var gone [][]byte
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var err error
		if member, _, err = activeMember(ctx, tx, username); err != nil {
			return err
		}
		if err := useRecoveryCode(ctx, tx, member, code, errSignIn); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_members SET verifier_hash = ? WHERE id = ?`, denproto.Hash(req.NewVerifier), member); err != nil {
			return err
		}
		if dev, err = addDevice(ctx, tx, member, req.PublicKey, label, token, now, expires); err != nil {
			return err
		}
		if gone, err = signOutOthers(ctx, tx, member, dev.KeyID); err != nil {
			return err
		}
		left, err = codesLeft(ctx, tx, member)
		return err
	})
	if err != nil {
		return denproto.SignInResponse{}, err
	}
	if err := d.devicesGone(member, gone, denproto.CloseReasonPasswordChanged); err != nil {
		return denproto.SignInResponse{}, err
	}
	resp, err := d.signedIn(ctx, member, dev, token, expires, left)
	resp.SignedOut = len(gone)
	return resp, err
}

// useRecoveryCode spends one of a member's recovery codes within tx, or
// fails with wrong.
func useRecoveryCode(ctx context.Context, tx *sql.Tx, member int64, code []byte, wrong error) error {
	res, err := tx.ExecContext(ctx, `DELETE FROM den_recovery_codes WHERE code_hash = ? AND member_id = ?`, denproto.Hash(code), member)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return wrong
	}
	return nil
}

// checkPassword proves the session's member knows their den password, or,
// when code is set, holds one of their recovery codes, which it uses up.
func (d *Den) checkPassword(ctx context.Context, tx *sql.Tx, s *Session, verifier denproto.Bytes, code string) error {
	var username string
	var hash []byte
	if err := tx.QueryRowContext(ctx, `SELECT username, verifier_hash FROM den_members WHERE id = ?`, s.MemberID).Scan(&username, &hash); err != nil {
		return err
	}
	if err := d.Allow(LimitPasswordName, username); err != nil {
		return err
	}
	if code != "" {
		raw, err := denproto.ParseRecoveryCode(code)
		if err != nil {
			return errWrongPassword
		}
		return useRecoveryCode(ctx, tx, s.MemberID, raw, errWrongPassword)
	}
	if len(verifier) != denproto.VerifierSize || !denproto.Equal(hash, denproto.Hash(verifier)) {
		return errWrongPassword
	}
	return nil
}

// ChangePassword sets the member's den password, given the current one or
// a recovery code. It signs out every device but the session's own, since
// anyone who had the old password may have signed one in with it.
func (d *Den) ChangePassword(ctx context.Context, s *Session, req denproto.PasswordChangeRequest) (denproto.PasswordChanged, error) {
	if err := denproto.Size("new_verifier", req.NewVerifier, denproto.VerifierSize); err != nil {
		return denproto.PasswordChanged{}, err
	}
	if (req.RecoveryCode == "") == (len(req.Verifier) == 0) {
		return denproto.PasswordChanged{}, invalid("give the current password's verifier or a recovery code")
	}
	var out denproto.PasswordChanged
	var gone [][]byte
	err := d.tx(ctx, func(tx *sql.Tx) error {
		if err := d.checkPassword(ctx, tx, s, req.Verifier, strings.TrimSpace(req.RecoveryCode)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_members SET verifier_hash = ? WHERE id = ?`, denproto.Hash(req.NewVerifier), s.MemberID); err != nil {
			return err
		}
		var err error
		if gone, err = signOutOthers(ctx, tx, s.MemberID, s.KeyID); err != nil {
			return err
		}
		out.RecoveryCodesLeft, err = codesLeft(ctx, tx, s.MemberID)
		return err
	})
	if err != nil {
		return denproto.PasswordChanged{}, err
	}
	out.SignedOut = len(gone)
	d.log.Infof("Member %d changed their den password, which signed out %d devices", s.MemberID, len(gone))
	return out, d.devicesGone(s.MemberID, gone, denproto.CloseReasonPasswordChanged)
}

// NewRecoveryCodes replaces the member's recovery codes with a fresh set,
// shown once.
func (d *Den) NewRecoveryCodes(ctx context.Context, s *Session, req denproto.RecoveryCodesRequest) (denproto.RecoveryCodeList, error) {
	codes := make([][]byte, denproto.RecoveryCodes)
	for i := range codes {
		codes[i] = denproto.Random(denproto.RecoveryCodeSize)
	}
	err := d.tx(ctx, func(tx *sql.Tx) error {
		if err := d.checkPassword(ctx, tx, s, req.Verifier, ""); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_recovery_codes WHERE member_id = ?`, s.MemberID); err != nil {
			return err
		}
		for _, code := range codes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO den_recovery_codes (code_hash, member_id) VALUES (?, ?)`,
				denproto.Hash(code), s.MemberID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return denproto.RecoveryCodeList{}, err
	}
	out := denproto.RecoveryCodeList{RecoveryCodes: make([]string, len(codes))}
	for i, code := range codes {
		out.RecoveryCodes[i] = denproto.FormatRecoveryCode(code)
	}
	return out, nil
}

// Devices lists the member's devices, the session's own marked, and how
// many recovery codes they have left.
func (d *Den) Devices(ctx context.Context, s *Session) (denproto.DeviceList, error) {
	list := denproto.DeviceList{Devices: []denproto.Device{}}
	rows, err := d.db.QueryContext(ctx, `SELECT key_id, label, created_at, last_seen_at FROM den_devices
		WHERE member_id = ? ORDER BY created_at, key_id`, s.MemberID)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var dev denproto.Device
		var keyID []byte
		if err := rows.Scan(&keyID, &dev.Label, &dev.CreatedAt, &dev.LastSeenAt); err != nil {
			return list, err
		}
		dev.KeyID, dev.Current = keyID, denproto.Equal(keyID, s.KeyID)
		list.Devices = append(list.Devices, dev)
	}
	if err := rows.Err(); err != nil {
		return list, err
	}
	list.RecoveryCodesLeft, err = codesLeft(ctx, d.db, s.MemberID)
	return list, err
}

// RevokeDevice removes one of the member's device keys, which ends its
// sessions and closes their sockets at once. Revoking the session's own
// device signs this device out.
func (d *Den) RevokeDevice(ctx context.Context, s *Session, keyID []byte) error {
	res, err := d.db.ExecContext(ctx, `DELETE FROM den_devices WHERE key_id = ? AND member_id = ?`, keyID, s.MemberID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such device")
	}
	d.log.Infof("Member %d revoked a device", s.MemberID)
	return d.devicesGone(s.MemberID, [][]byte{keyID}, denproto.CloseReasonKeyRevoked)
}
