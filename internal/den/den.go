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
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
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
	db    *sql.DB
	v     *vault.Vault
	log   *xlog.Logger
	Hub   *Hub
	files *fileStore

	// TokenLifetime is how long a session token lasts before renewal.
	TokenLifetime time.Duration
	now           func() time.Time

	mu   sync.RWMutex
	info *denproto.Den
	key  *vault.SigningKey

	nonceMu sync.Mutex
	nonces  map[string]time.Time

	// changes orders edits and ticks, and their events, so a message's
	// updates go out in the order of its revisions.
	changes sync.Mutex
	// keys orders changes to DM keys, and their events.
	keys sync.Mutex
	// requests are sign-ins waiting for approval (M1.7).
	requests requests

	limits struct {
		challenge, join, login, socket, write, send, typing, upload, password, passwordName, call *limiter
	}
	sockets  *socketSet
	presence *presence
	calls    callRegistry
	// expiry deletes messages that pass the retention period (M5).
	expiry expiry

	// PresenceDelay is how long presence changes gather before they're
	// announced; tests shorten it.
	PresenceDelay time.Duration
	// CallHold is how long a call whose socket closed waits for its device
	// to take it back (M3); tests shorten it.
	CallHold time.Duration
}

// Session is an authenticated session.
type Session struct {
	TokenHash []byte
	MemberID  int64
	KeyID     []byte
	Role      string
	ExpiresAt time.Time
}

// Storage is where the den keeps uploads: Dir holds them sealed, and
// Temp, on the same filesystem, holds uploads still arriving. Media runs
// the media module, which strips video and audio again and makes their
// previews; without it the den refuses them.
type Storage struct {
	Dir, Temp string
	Media     *ffmpeg.Runner
}

// Open loads the den, if this install has created one.
func Open(ctx context.Context, db *sql.DB, v *vault.Vault, log *xlog.Logger, storage Storage) (*Den, error) {
	d := &Den{
		db: db, v: v, log: log, Hub: NewHub(), files: newFileStore(storage),
		TokenLifetime: DefaultTokenLifetime,
		CallHold:      denproto.CallHold,
		now:           time.Now,
		nonces:        map[string]time.Time{},
		requests:      newRequests(),
		sockets:       newSocketSet(),
		presence:      newPresence(),
		calls:         newCallRegistry(),
		PresenceDelay: defaultPresenceDelay,
	}
	d.limits.challenge = newLimiter(30, 2*time.Second, 100_000)
	d.limits.join = newLimiter(10, 6*time.Minute, 100_000)
	d.limits.login = newLimiter(30, 2*time.Second, 100_000)
	d.limits.socket = newLimiter(20, 3*time.Second, 100_000)
	d.limits.write = newLimiter(30, time.Second/3, 100_000)
	d.limits.send = newLimiter(5, time.Second, 100_000)
	d.limits.typing = newLimiter(1, 2*time.Second, 100_000)
	d.limits.upload = newLimiter(20, 3*time.Second, 100_000)
	d.limits.password = newLimiter(10, 6*time.Minute, 100_000)
	d.limits.passwordName = newLimiter(10, 6*time.Minute, 100_000)
	d.limits.call = newLimiter(10, 6*time.Second, 100_000)

	var name, url string
	var pub, sealed []byte
	var limits denproto.Limits
	var calls denproto.CallLimits
	var retention int
	err := db.QueryRowContext(ctx, `SELECT name, url, public_key, private_key, file_size, member_storage, den_storage,
		call_members, den_callers, shares, share_viewers, share_bitrate, share_height, share_fps, retention FROM den WHERE id = 1`).
		Scan(&name, &url, &pub, &sealed, &limits.FileSize, &limits.MemberStorage, &limits.DenStorage,
			&calls.Members, &calls.Callers, &calls.Shares, &calls.ShareViewers, &calls.ShareBitrate, &calls.ShareHeight, &calls.ShareFPS,
			&retention)
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
	d.info = &denproto.Den{ID: denproto.ID(pub), Name: name, URL: url, Limits: limits, CallLimits: calls, Retention: retention}
	if err := d.tidyFiles(ctx); err != nil {
		d.key.Close()
		return nil, fmt.Errorf("tidy uploads: %w", err)
	}
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

// Close ends calls, stops sweeping uploads and deleting old messages, and
// releases the identity key.
func (d *Den) Close() {
	if err := d.StopCalls(); err != nil {
		d.log.Warnf("stop calls: %v", err)
	}
	d.stopExpiry()
	d.files.stop()
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
	limits := denproto.Limits{FileSize: denproto.DefaultFileSize, MemberStorage: denproto.DefaultMemberStorage, DenStorage: denproto.DefaultDenStorage}
	calls := denproto.DefaultCallLimits
	err = d.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO den (id, name, url, public_key, private_key, created_at, file_size, member_storage, den_storage,
				call_members, den_callers, shares, share_viewers, share_bitrate, share_height, share_fps)
			VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			name, url, []byte(pub), sealed, now.UnixMilli(), limits.FileSize, limits.MemberStorage, limits.DenStorage,
			calls.Members, calls.Callers, calls.Shares, calls.ShareViewers, calls.ShareBitrate, calls.ShareHeight, calls.ShareFPS); err != nil {
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
	d.info = &denproto.Den{ID: denproto.ID(pub), Name: name, URL: url, Limits: limits, CallLimits: calls}
	d.log.Infof("Den created")
	return code, nil
}

// OwnerInvite issues a fresh owner invite while the den has no owner, so
// setup can finish if the owner's first join failed.
func (d *Den) OwnerInvite(ctx context.Context) ([]byte, error) {
	if _, ok := d.Info(); !ok {
		return nil, errors.New("the den hasn't been created")
	}
	code := denproto.Random(denproto.InviteCodeSize)
	now := d.now()
	err := d.tx(ctx, func(tx *sql.Tx) error {
		var owners int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM den_members WHERE role = 'owner'`).Scan(&owners); err != nil {
			return err
		}
		if owners > 0 {
			return errors.New("the den already has an owner")
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO den_invites (code_hash, role, created_by, created_at, expires_at, max_uses) VALUES (?, 'owner', NULL, ?, ?, 1)`,
			denproto.Hash(code), now.UnixMilli(), now.Add(ownerInviteLifetime).UnixMilli())
		return err
	})
	if err != nil {
		return nil, err
	}
	return code, nil
}

// Update changes the den's name, address, upload limits, limits for calls
// or retention period. Only the owner may.
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
	if req.Limits != nil {
		if err := denproto.CheckLimits(*req.Limits); err != nil {
			d.mu.Unlock()
			return denproto.Den{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "limits: %v", err)
		}
		info.Limits = *req.Limits
	}
	if req.CallLimits != nil {
		if err := denproto.CheckCallLimits(*req.CallLimits); err != nil {
			d.mu.Unlock()
			return denproto.Den{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "call_limits: %v", err)
		}
		info.CallLimits = *req.CallLimits
	}
	if req.Retention != nil {
		if err := denproto.CheckRetention(*req.Retention); err != nil {
			d.mu.Unlock()
			return denproto.Den{}, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "retention: %v", err)
		}
		info.Retention = *req.Retention
	}
	retained := d.info.Retention != info.Retention
	c := info.CallLimits
	if _, err := d.db.ExecContext(ctx, `UPDATE den SET name = ?, url = ?, file_size = ?, member_storage = ?, den_storage = ?,
		call_members = ?, den_callers = ?, shares = ?, share_viewers = ?, share_bitrate = ?, share_height = ?, share_fps = ?,
		retention = ? WHERE id = 1`,
		info.Name, info.URL, info.Limits.FileSize, info.Limits.MemberStorage, info.Limits.DenStorage,
		c.Members, c.Callers, c.Shares, c.ShareViewers, c.ShareBitrate, c.ShareHeight, c.ShareFPS, info.Retention); err != nil {
		d.mu.Unlock()
		return denproto.Den{}, err
	}
	d.info = &info
	d.mu.Unlock()
	if err := d.Hub.Publish(denproto.EventDenUpdated, info, Everyone); err != nil {
		return info, err
	}
	// A shorter period deletes what passed it at once.
	if retained {
		d.planExpiry(true)
	}
	return info, nil
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
		DenSig: d.key.Sign(denproto.DenChallengeMessage(clientNonce, nonce, d.info.URL)),
		URL:    d.info.URL,
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
	info.Limits = denproto.Limits{}
	info.CallLimits = denproto.CallLimits{}
	info.Retention = 0
	return info, nil
}

// Join redeems an invite: it creates the member and their first device,
// and returns a session and the member's recovery codes. A former member
// who gives their username and password comes back as themselves, with
// their history, unless they were banned.
func (d *Den) Join(ctx context.Context, req denproto.JoinRequest) (denproto.JoinResponse, error) {
	for _, f := range []struct {
		name  string
		value denproto.Bytes
		size  int
	}{
		{"invite", req.Invite, denproto.InviteCodeSize},
		{"verifier", req.Verifier, denproto.VerifierSize},
		{"public_key", req.PublicKey, denproto.PublicKeySize},
		{"seal_check", req.SealCheck, denproto.SealCheckSize},
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
	var retired []int64
	d.keys.Lock()
	defer d.keys.Unlock()
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
		id, joinedAt, err := claimUsername(ctx, tx, username, req.Verifier)
		if err != nil {
			return err
		}
		if id != 0 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE den_members SET display_name = ?, role = ?, invite_id = ?, left_at = NULL WHERE id = ?`,
				displayName, role, inviteID, id); err != nil {
				return err
			}
			// Coming back with another seal is starting over: nothing opens
			// the copies of DM keys the old one sealed.
			var sealCheck []byte
			if err := tx.QueryRowContext(ctx, `SELECT seal_check FROM den_members WHERE id = ?`, id).Scan(&sealCheck); err != nil {
				return err
			}
			if !denproto.Equal(sealCheck, req.SealCheck) {
				if retired, err = resetSeal(ctx, tx, id, req.SealCheck, now.UnixMilli()); err != nil {
					return err
				}
			}
		} else {
			joinedAt = now.UnixMilli()
			res, err := tx.ExecContext(ctx,
				`INSERT INTO den_members (username, display_name, role, verifier_hash, joined_at, invite_id, seal_check) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				username, displayName, role, denproto.Hash(req.Verifier), joinedAt, inviteID, []byte(req.SealCheck))
			if err != nil {
				return err
			}
			if id, err = res.LastInsertId(); err != nil {
				return err
			}
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
		member = denproto.Member{ID: strconv.FormatInt(id, 10), Username: username, DisplayName: displayName, Role: role, JoinedAt: joinedAt}
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
	if err := d.Hub.Publish(denproto.EventMemberJoined, member, Everyone); err != nil {
		return denproto.JoinResponse{}, err
	}
	if err := d.publishKeys(ctx, retired...); err != nil {
		return denproto.JoinResponse{}, err
	}
	return denproto.JoinResponse{Member: member, Token: token, ExpiresAt: now.Add(d.TokenLifetime).UnixMilli(), RecoveryCodes: shown}, nil
}

var errUsernameTaken = denproto.Errorf(http.StatusConflict, denproto.CodeUsernameTaken, "that username is taken")

// claimUsername checks whether a joining member may take a username. It
// returns 0 for a free username, or the ID and first join time of the
// former member it belongs to, when the verifier proves the joiner is them.
// A wrong verifier gets the same answer as a taken name, so an invite
// can't be used to learn who used to be here.
func claimUsername(ctx context.Context, tx *sql.Tx, username string, verifier []byte) (id, joinedAt int64, err error) {
	var hash []byte
	var left, banned sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, verifier_hash, joined_at, left_at, banned_at FROM den_members WHERE username = ?`, username).
		Scan(&id, &hash, &joinedAt, &left, &banned)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if banned.Valid {
		return 0, 0, denproto.Errorf(http.StatusForbidden, denproto.CodeBanned, "that username is banned from this den")
	}
	if !left.Valid || subtle.ConstantTimeCompare(hash, denproto.Hash(verifier)) != 1 {
		return 0, 0, errUsernameTaken
	}
	return id, joinedAt, nil
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

// CreateInvite makes an invite. Moderators and the owner may.
func (d *Den) CreateInvite(ctx context.Context, s *Session, req denproto.InviteCreateRequest) (denproto.Invite, error) {
	if !IsStaff(s.Role) {
		return denproto.Invite{}, forbidden("only moderators and the owner create invites")
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
		ID: strconv.FormatInt(id, 10), Code: code, CreatedBy: denproto.FormatID(s.MemberID), CreatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(expiry).UnixMilli(), MaxUses: uses,
	}, nil
}

// Invites lists the invites that can still be used, without their codes.
func (d *Den) Invites(ctx context.Context, s *Session) ([]denproto.Invite, error) {
	if !IsStaff(s.Role) {
		return nil, forbidden("only moderators and the owner manage invites")
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, created_by, created_at, expires_at, max_uses, uses FROM den_invites
		WHERE role = 'member' AND expires_at > ? AND uses < max_uses ORDER BY id`, d.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invites := []denproto.Invite{}
	for rows.Next() {
		var inv denproto.Invite
		var id int64
		var by sql.NullInt64
		if err := rows.Scan(&id, &by, &inv.CreatedAt, &inv.ExpiresAt, &inv.MaxUses, &inv.Uses); err != nil {
			return nil, err
		}
		inv.ID = strconv.FormatInt(id, 10)
		if by.Valid {
			inv.CreatedBy = denproto.FormatID(by.Int64)
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

// RevokeInvite deletes an invite.
func (d *Den) RevokeInvite(ctx context.Context, s *Session, id int64) error {
	if !IsStaff(s.Role) {
		return forbidden("only moderators and the owner manage invites")
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
