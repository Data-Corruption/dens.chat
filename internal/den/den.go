// Package den is the den this install hosts: its identity key, members,
// devices, invites, sessions and event stream. The den listener's router
// (internal/platform/http/den) is a thin HTTP layer over it.
//
// Every method returns protocol errors (*denproto.Error) for anything a
// client did wrong, and plain errors for the den's own failures.
package den

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Defaults from protocol.md.
const (
	DefaultTokenLifetime = time.Hour
	nonceLifetime        = 60 * time.Second
	maxNonces            = 100_000
	ownerInviteLifetime  = time.Hour
	defaultInviteExpiry  = 24 * time.Hour
	maxInviteExpiry      = 30 * 24 * time.Hour
	maxInviteUses        = 100
	maxSocketsPerMember  = 5
)

var identityAD = []byte("den.private_key")

// ErrAlreadyCreated reports a second attempt to create the den.
var ErrAlreadyCreated = errors.New("this install's den already exists")

// Den is the den hosted by this install.
type Den struct {
	db  *sql.DB
	v   *vault.Vault
	log *xlog.Logger
	Hub *Hub

	// TokenLifetime is how long a session token lasts before renewal.
	TokenLifetime time.Duration
	now           func() time.Time

	mu   sync.RWMutex
	info *denproto.Den
	key  *vault.SigningKey

	nonceMu sync.Mutex
	nonces  map[string]time.Time

	limits struct {
		challenge, join, login, socket, write *limiter
	}
	sockets *socketSet
}

// Session is an authenticated session.
type Session struct {
	TokenHash []byte
	MemberID  int64
	KeyID     []byte
	Role      string
	ExpiresAt time.Time
}

// Open loads the den, if this install has created one.
func Open(ctx context.Context, db *sql.DB, v *vault.Vault, log *xlog.Logger) (*Den, error) {
	d := &Den{
		db: db, v: v, log: log, Hub: NewHub(),
		TokenLifetime: DefaultTokenLifetime,
		now:           time.Now,
		nonces:        map[string]time.Time{},
		sockets:       newSocketSet(),
	}
	d.limits.challenge = newLimiter(30, 2*time.Second, 100_000)
	d.limits.join = newLimiter(10, 6*time.Minute, 100_000)
	d.limits.login = newLimiter(30, 2*time.Second, 100_000)
	d.limits.socket = newLimiter(20, 3*time.Second, 100_000)
	d.limits.write = newLimiter(30, time.Second/3, 100_000)

	var name, url string
	var pub, sealed []byte
	err := db.QueryRowContext(ctx, `SELECT name, url, public_key, private_key FROM den WHERE id = 1`).
		Scan(&name, &url, &pub, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load den: %w", err)
	}
	seed, err := v.Open(sealed, identityAD)
	if err != nil {
		return nil, fmt.Errorf("open den identity key: %w", err)
	}
	defer clear(seed)
	if err := d.holdKey(seed, pub); err != nil {
		return nil, err
	}
	d.info = &denproto.Den{ID: denproto.ID(pub), Name: name, URL: url}
	return d, nil
}

// holdKey keeps the identity key in locked memory, after checking it
// against the stored public key.
func (d *Den) holdKey(seed, pub []byte) error {
	key, err := vault.NewSigningKey(seed)
	if err != nil {
		return fmt.Errorf("den identity key: %w", err)
	}
	if !denproto.Equal(key.Public(), pub) {
		key.Close()
		return errors.New("den identity key doesn't match its public key")
	}
	d.key = key
	return nil
}

// Close releases the identity key.
func (d *Den) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.key != nil {
		d.key.Close()
		d.key = nil
	}
}

// Info returns the den's description, and false before it is created.
func (d *Den) Info() (denproto.Den, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.info == nil {
		return denproto.Den{}, false
	}
	return *d.info, true
}

// Create creates the den and returns the one-use code of its owner invite,
// which the owner's own client redeems like any other invite.
func (d *Den) Create(ctx context.Context, name, url string) ([]byte, error) {
	name, err := denproto.CleanName(name, denproto.MaxNameRunes)
	if err != nil {
		return nil, fmt.Errorf("den name: %w", err)
	}
	if url, err = denproto.NormalizeDenURL(url); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.info != nil {
		return nil, ErrAlreadyCreated
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	seed := priv.Seed()
	defer clear(priv)
	defer clear(seed)
	sealed, err := d.v.Seal(seed, identityAD)
	if err != nil {
		return nil, err
	}
	code := denproto.Random(denproto.InviteCodeSize)
	now := d.now()
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO den (id, name, url, public_key, private_key, created_at) VALUES (1, ?, ?, ?, ?, ?)`,
			name, url, []byte(pub), sealed, now.UnixMilli()); err != nil {
			return fmt.Errorf("store den: %w", err)
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO den_invites (code_hash, role, created_by, created_at, expires_at, max_uses) VALUES (?, 'owner', NULL, ?, ?, 1)`,
			denproto.Hash(code), now.UnixMilli(), now.Add(ownerInviteLifetime).UnixMilli())
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := d.holdKey(seed, pub); err != nil {
		return nil, err
	}
	d.info = &denproto.Den{ID: denproto.ID(pub), Name: name, URL: url}
	d.log.Infof("Den created")
	return code, nil
}

// Update changes the den's name or address. Only the owner may.
func (d *Den) Update(ctx context.Context, s *Session, req denproto.DenUpdateRequest) (denproto.Den, error) {
	if s.Role != denproto.RoleOwner {
		return denproto.Den{}, denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, "only the owner changes the den")
	}
	d.mu.Lock()
	info := *d.info
	var err error
	if req.Name != nil {
		if info.Name, err = denproto.CleanName(*req.Name, denproto.MaxNameRunes); err != nil {
			d.mu.Unlock()
			return denproto.Den{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "name: %v", err)
		}
	}
	if req.URL != nil {
		if info.URL, err = denproto.NormalizeDenURL(*req.URL); err != nil {
			d.mu.Unlock()
			return denproto.Den{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "url: %v", err)
		}
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE den SET name = ?, url = ? WHERE id = 1`, info.Name, info.URL); err != nil {
		d.mu.Unlock()
		return denproto.Den{}, err
	}
	d.info = &info
	d.mu.Unlock()
	return info, d.Hub.Publish(denproto.EventDenUpdated, info, Everyone)
}

// Challenge proves the den's identity key and issues a single-use nonce.
func (d *Den) Challenge(clientNonce []byte) (denproto.ChallengeResponse, error) {
	if err := denproto.Size("client_nonce", clientNonce, denproto.NonceSize); err != nil {
		return denproto.ChallengeResponse{}, err
	}
	now := d.now()
	d.nonceMu.Lock()
	if len(d.nonces) >= maxNonces {
		for n, exp := range d.nonces {
			if now.After(exp) {
				delete(d.nonces, n)
			}
		}
	}
	if len(d.nonces) >= maxNonces {
		d.nonceMu.Unlock()
		return denproto.ChallengeResponse{}, denproto.Errorf(http.StatusTooManyRequests, denproto.CodeRateLimited, "too many sign-ins in progress")
	}
	nonce := denproto.Random(denproto.NonceSize)
	d.nonces[string(nonce)] = now.Add(nonceLifetime)
	d.nonceMu.Unlock()

	d.mu.RLock()
	defer d.mu.RUnlock()
	return denproto.ChallengeResponse{
		Nonce:  nonce,
		DenKey: denproto.Bytes(d.key.Public()),
		DenSig: d.key.Sign(denproto.DenChallengeMessage(clientNonce, nonce)),
	}, nil
}

// consumeNonce spends a nonce, reporting whether it was outstanding.
func (d *Den) consumeNonce(nonce []byte) bool {
	d.nonceMu.Lock()
	defer d.nonceMu.Unlock()
	exp, ok := d.nonces[string(nonce)]
	delete(d.nonces, string(nonce))
	return ok && !d.now().After(exp)
}

// checkProof spends the nonce and verifies the device's proof over it.
func (d *Den) checkProof(pub, nonce, proof []byte) error {
	if err := denproto.Size("nonce", nonce, denproto.NonceSize); err != nil {
		return err
	}
	if err := denproto.Size("proof", proof, denproto.SignatureSize); err != nil {
		return err
	}
	if !d.consumeNonce(nonce) {
		return denproto.Errorf(http.StatusUnauthorized, denproto.CodeBadNonce, "the nonce is unknown, used or expired")
	}
	info, _ := d.Info()
	msg := denproto.ProofMessage(info.ID, denproto.ID(pub), nonce)
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, proof) {
		return denproto.Errorf(http.StatusUnauthorized, denproto.CodeBadSignature, "the proof doesn't verify")
	}
	return nil
}

var errInviteInvalid = denproto.Errorf(http.StatusGone, denproto.CodeInviteInvalid, "the invite is unknown, expired or used up")

// Preview describes the den to someone holding a valid invite.
func (d *Den) Preview(ctx context.Context, code []byte) (denproto.Den, error) {
	if len(code) != denproto.InviteCodeSize {
		return denproto.Den{}, errInviteInvalid
	}
	var n int
	err := d.db.QueryRowContext(ctx,
		`SELECT count(*) FROM den_invites WHERE code_hash = ? AND expires_at > ? AND uses < max_uses`,
		denproto.Hash(code), d.now().UnixMilli()).Scan(&n)
	if err != nil {
		return denproto.Den{}, err
	}
	if n == 0 {
		return denproto.Den{}, errInviteInvalid
	}
	info, _ := d.Info()
	return info, nil
}

// Join redeems an invite: it creates the member and their first device,
// and returns a session and the member's recovery codes.
func (d *Den) Join(ctx context.Context, req denproto.JoinRequest) (denproto.JoinResponse, error) {
	for _, f := range []struct {
		name  string
		value denproto.Bytes
		size  int
	}{
		{"invite", req.Invite, denproto.InviteCodeSize},
		{"verifier", req.Verifier, denproto.VerifierSize},
		{"public_key", req.PublicKey, denproto.PublicKeySize},
	} {
		if err := denproto.Size(f.name, f.value, f.size); err != nil {
			return denproto.JoinResponse{}, err
		}
	}
	username, err := denproto.NormalizeUsername(req.Username)
	if err != nil {
		return denproto.JoinResponse{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "username: %v", err)
	}
	displayName, err := denproto.CleanName(req.DisplayName, denproto.MaxNameRunes)
	if err != nil {
		return denproto.JoinResponse{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "display_name: %v", err)
	}
	label, err := denproto.CleanName(req.DeviceLabel, denproto.MaxLabelRunes)
	if err != nil {
		return denproto.JoinResponse{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "device_label: %v", err)
	}
	if err := d.checkProof(req.PublicKey, req.Nonce, req.Proof); err != nil {
		return denproto.JoinResponse{}, err
	}

	now := d.now()
	token := denproto.Random(denproto.TokenSize)
	codes := make([][]byte, denproto.RecoveryCodes)
	for i := range codes {
		codes[i] = denproto.Random(denproto.RecoveryCodeSize)
	}
	keyID := denproto.ID(ed25519.PublicKey(req.PublicKey))
	var member denproto.Member
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var inviteID int64
		var role string
		err := tx.QueryRowContext(ctx,
			`SELECT id, role FROM den_invites WHERE code_hash = ? AND expires_at > ? AND uses < max_uses`,
			denproto.Hash(req.Invite), now.UnixMilli()).Scan(&inviteID, &role)
		if errors.Is(err, sql.ErrNoRows) {
			return errInviteInvalid
		}
		if err != nil {
			return err
		}
		var taken int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_members WHERE username = ?`, username).Scan(&taken); err != nil {
			return err
		}
		if taken > 0 {
			return denproto.Errorf(http.StatusConflict, denproto.CodeUsernameTaken, "that username is taken")
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO den_members (username, display_name, role, verifier_hash, joined_at) VALUES (?, ?, ?, ?, ?)`,
			username, displayName, role, denproto.Hash(req.Verifier), now.UnixMilli())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO den_devices (key_id, member_id, public_key, label, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
			[]byte(keyID), id, []byte(req.PublicKey), label, now.UnixMilli(), now.UnixMilli()); err != nil {
			return denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "that device key is already registered")
		}
		for _, code := range codes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO den_recovery_codes (code_hash, member_id) VALUES (?, ?)`,
				denproto.Hash(code), id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_invites SET uses = uses + 1 WHERE id = ?`, inviteID); err != nil {
			return err
		}
		if err := insertSession(ctx, tx, token, id, keyID, now, now.Add(d.TokenLifetime)); err != nil {
			return err
		}
		member = denproto.Member{ID: strconv.FormatInt(id, 10), Username: username, DisplayName: displayName, Role: role, JoinedAt: now.UnixMilli()}
		return nil
	})
	if err != nil {
		return denproto.JoinResponse{}, err
	}
	shown := make([]string, len(codes))
	for i, code := range codes {
		shown[i] = denproto.FormatRecoveryCode(code)
	}
	d.log.Infof("Member %s joined", member.ID)
	return denproto.JoinResponse{Member: member, Token: token, ExpiresAt: now.Add(d.TokenLifetime).UnixMilli(), RecoveryCodes: shown}, nil
}

// Login starts a session for a registered device.
func (d *Den) Login(ctx context.Context, req denproto.LoginRequest) (denproto.SessionResponse, error) {
	if err := denproto.Size("key_id", req.KeyID, denproto.IDSize); err != nil {
		return denproto.SessionResponse{}, err
	}
	var memberID int64
	var pub []byte
	err := d.db.QueryRowContext(ctx, `SELECT member_id, public_key FROM den_devices WHERE key_id = ?`, []byte(req.KeyID)).
		Scan(&memberID, &pub)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend the nonce anyway, so an unknown key can't probe with it.
		d.consumeNonce(req.Nonce)
		return denproto.SessionResponse{}, denproto.Errorf(http.StatusUnauthorized, denproto.CodeUnauthorized, "unknown device")
	}
	if err != nil {
		return denproto.SessionResponse{}, err
	}
	if err := d.checkProof(pub, req.Nonce, req.Proof); err != nil {
		return denproto.SessionResponse{}, err
	}
	now := d.now()
	token := denproto.Random(denproto.TokenSize)
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM den_sessions WHERE expires_at <= ?`, now.UnixMilli()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE den_devices SET last_seen_at = ? WHERE key_id = ?`, now.UnixMilli(), []byte(req.KeyID)); err != nil {
			return err
		}
		return insertSession(ctx, tx, token, memberID, req.KeyID, now, now.Add(d.TokenLifetime))
	})
	if err != nil {
		return denproto.SessionResponse{}, err
	}
	return denproto.SessionResponse{Token: token, ExpiresAt: now.Add(d.TokenLifetime).UnixMilli()}, nil
}

func insertSession(ctx context.Context, tx *sql.Tx, token []byte, member int64, keyID []byte, now, expires time.Time) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO den_sessions (token_hash, member_id, key_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		denproto.Hash(token), member, keyID, now.UnixMilli(), expires.UnixMilli())
	return err
}

var errUnauthorized = denproto.Errorf(http.StatusUnauthorized, denproto.CodeUnauthorized, "missing, unknown or expired token")

// Authenticate returns the session a bearer token belongs to.
func (d *Den) Authenticate(ctx context.Context, token []byte) (*Session, error) {
	if len(token) != denproto.TokenSize {
		return nil, errUnauthorized
	}
	s := &Session{TokenHash: denproto.Hash(token)}
	var expires int64
	err := d.db.QueryRowContext(ctx, `
		SELECT s.member_id, s.key_id, s.expires_at, m.role
		FROM den_sessions s JOIN den_members m ON m.id = s.member_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		s.TokenHash, d.now().UnixMilli()).Scan(&s.MemberID, &s.KeyID, &expires, &s.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, err
	}
	s.ExpiresAt = time.UnixMilli(expires)
	return s, nil
}

// Renew extends a session by the token lifetime, given a fresh proof from
// its device.
func (d *Den) Renew(ctx context.Context, s *Session, nonce, proof []byte) (time.Time, error) {
	var pub []byte
	err := d.db.QueryRowContext(ctx, `SELECT public_key FROM den_devices WHERE key_id = ?`, s.KeyID).Scan(&pub)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, denproto.Errorf(http.StatusForbidden, denproto.CodeKeyRevoked, "the device key was revoked")
	}
	if err != nil {
		return time.Time{}, err
	}
	if err := d.checkProof(pub, nonce, proof); err != nil {
		return time.Time{}, err
	}
	expires := d.now().Add(d.TokenLifetime)
	res, err := d.db.ExecContext(ctx, `UPDATE den_sessions SET expires_at = ? WHERE token_hash = ? AND expires_at > ?`,
		expires.UnixMilli(), s.TokenHash, d.now().UnixMilli())
	if err != nil {
		return time.Time{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return time.Time{}, errUnauthorized
	}
	return expires, nil
}

// Logout ends a session and closes its sockets.
func (d *Den) Logout(ctx context.Context, s *Session) error {
	if _, err := d.db.ExecContext(ctx, `DELETE FROM den_sessions WHERE token_hash = ?`, s.TokenHash); err != nil {
		return err
	}
	d.sockets.closeSession(s.TokenHash, 1000, "logged out")
	return nil
}

// Member returns a member by ID.
func (d *Den) Member(ctx context.Context, id int64) (denproto.Member, error) {
	m := denproto.Member{ID: strconv.FormatInt(id, 10)}
	err := d.db.QueryRowContext(ctx, `SELECT username, display_name, role, joined_at FROM den_members WHERE id = ?`, id).
		Scan(&m.Username, &m.DisplayName, &m.Role, &m.JoinedAt)
	return m, err
}

// CreateInvite makes an invite. Only the owner may until roles land.
func (d *Den) CreateInvite(ctx context.Context, s *Session, req denproto.InviteCreateRequest) (denproto.Invite, error) {
	if s.Role != denproto.RoleOwner {
		return denproto.Invite{}, denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, "only the owner creates invites")
	}
	expiry := defaultInviteExpiry
	if req.ExpiresIn != 0 {
		expiry = time.Duration(req.ExpiresIn) * time.Second
	}
	if expiry < time.Minute || expiry > maxInviteExpiry {
		return denproto.Invite{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "expires_in must be a minute to 30 days")
	}
	uses := req.MaxUses
	if uses == 0 {
		uses = 1
	}
	if uses < 1 || uses > maxInviteUses {
		return denproto.Invite{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "max_uses must be 1 to %d", maxInviteUses)
	}
	now := d.now()
	code := denproto.Random(denproto.InviteCodeSize)
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO den_invites (code_hash, role, created_by, created_at, expires_at, max_uses) VALUES (?, 'member', ?, ?, ?, ?)`,
		denproto.Hash(code), s.MemberID, now.UnixMilli(), now.Add(expiry).UnixMilli(), uses)
	if err != nil {
		return denproto.Invite{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return denproto.Invite{}, err
	}
	return denproto.Invite{
		ID: strconv.FormatInt(id, 10), Code: code, CreatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(expiry).UnixMilli(), MaxUses: uses,
	}, nil
}

// Invites lists the invites that can still be used, without their codes.
func (d *Den) Invites(ctx context.Context, s *Session) ([]denproto.Invite, error) {
	if s.Role != denproto.RoleOwner {
		return nil, denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, "only the owner manages invites")
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, created_at, expires_at, max_uses, uses FROM den_invites
		WHERE role = 'member' AND expires_at > ? AND uses < max_uses ORDER BY id`, d.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invites := []denproto.Invite{}
	for rows.Next() {
		var inv denproto.Invite
		var id int64
		if err := rows.Scan(&id, &inv.CreatedAt, &inv.ExpiresAt, &inv.MaxUses, &inv.Uses); err != nil {
			return nil, err
		}
		inv.ID = strconv.FormatInt(id, 10)
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

// RevokeInvite deletes an invite.
func (d *Den) RevokeInvite(ctx context.Context, s *Session, id int64) error {
	if s.Role != denproto.RoleOwner {
		return denproto.Errorf(http.StatusForbidden, denproto.CodeForbidden, "only the owner manages invites")
	}
	res, err := d.db.ExecContext(ctx, `DELETE FROM den_invites WHERE id = ? AND role = 'member'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such invite")
	}
	return nil
}

func (d *Den) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
