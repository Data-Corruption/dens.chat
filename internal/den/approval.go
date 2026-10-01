package den

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Approving new devices (M1.7). A password sign-in on a device new to the
// den doesn't add it at once. The den holds it for ten minutes and asks
// the member's other devices; one of them runs an exchange with it through
// the den, and once the member compared the check codes on both, hands it
// their DM seal, sealed with the exchange's key. Only then does the den
// register the device. Requests live in memory: a restart drops them, and
// the new device asks again.

const (
	requestLifetime   = 10 * time.Minute
	maxMemberRequests = 3
	maxRequests       = 1000
	// pendingWait is how long a new device's question waits for a change
	// before the den says nothing changed; clients give a request 30
	// seconds.
	pendingWait = 20 * time.Second
)

var (
	errNoOtherDevice = denproto.Errorf(http.StatusForbidden, denproto.CodeNoOtherDevice,
		"no other device of this member's can approve a sign-in; use a recovery code")
	errRequestNotFound = denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such sign-in waiting")
	errRequestStep     = denproto.Errorf(http.StatusConflict, denproto.CodeKeyExists, "the sign-in isn't at that step")
	errPendingUnknown  = denproto.Errorf(http.StatusUnauthorized, denproto.CodeUnauthorized, "no such sign-in waiting, or it lapsed")
)

// deviceRequest is a sign-in waiting for approval.
type deviceRequest struct {
	view      denproto.DeviceRequest // as the member's devices see it
	tokenHash []byte
	member    int64
	username  string
	pub       []byte
	verifier  []byte // the verifier's hash when it asked; a new password cancels it
	status    string
	version   int64
	changed   chan struct{} // closed at each change, for the new device waiting on it
	session   *denproto.SignInResponse
	handover  denproto.Bytes
}

// waiting reports whether the member can still act on a request.
func (r *deviceRequest) waiting() bool {
	return r.status == denproto.PendingWaiting || r.status == denproto.PendingAnswered
}

// change moves a request on and wakes the new device waiting on it; the
// caller holds requests.mu.
func (r *deviceRequest) change(status string) {
	r.status = status
	r.version++
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *deviceRequest) pending() denproto.PendingStatus {
	st := denproto.PendingStatus{Version: r.version, Status: r.status, Answer: r.view.Answer}
	if r.status == denproto.PendingApproved {
		st.Session, st.Handover = r.session, r.handover
	}
	return st
}

type requests struct {
	mu      sync.Mutex
	byID    map[string]*deviceRequest
	byToken map[string]*deviceRequest
}

func newRequests() requests {
	return requests{byID: map[string]*deviceRequest{}, byToken: map[string]*deviceRequest{}}
}

// prune drops requests past their time; the caller holds mu. A new device
// still waiting then hears its request is gone.
func (rs *requests) prune(now time.Time) {
	for id, r := range rs.byID {
		if now.UnixMilli() >= r.view.ExpiresAt {
			delete(rs.byID, id)
			delete(rs.byToken, string(r.tokenHash))
			close(r.changed)
		}
	}
}

// PasswordLogin checks a member's den password on a new device, and holds
// the device for one of their other devices to approve. A member with no
// other device signs in with a recovery code instead.
func (d *Den) PasswordLogin(ctx context.Context, req denproto.PasswordLoginRequest) (denproto.PendingSignIn, error) {
	username, label, err := d.signInFields(req.Username, req.DeviceLabel, req.Verifier, req.PublicKey, req.Nonce, req.Proof)
	if err != nil {
		return denproto.PendingSignIn{}, err
	}
	if err := req.Offer.Check(); err != nil {
		return denproto.PendingSignIn{}, invalid("offer: %v", err)
	}
	member, hash, err := activeMember(ctx, d.db, username)
	if err != nil {
		return denproto.PendingSignIn{}, err
	}
	if !denproto.Equal(hash, denproto.Hash(req.Verifier)) {
		return denproto.PendingSignIn{}, errSignIn
	}
	keyID := denproto.ID(ed25519.PublicKey(req.PublicKey))
	var devices int
	var taken bool
	if err := d.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM den_devices WHERE member_id = ?),
		EXISTS (SELECT 1 FROM den_devices WHERE key_id = ?)`, member, []byte(keyID)).Scan(&devices, &taken); err != nil {
		return denproto.PendingSignIn{}, err
	}
	if taken {
		return denproto.PendingSignIn{}, invalid("that device key is already registered")
	}
	if devices == 0 {
		return denproto.PendingSignIn{}, errNoOtherDevice
	}
	now := d.now()
	token := denproto.Random(denproto.TokenSize)
	r := &deviceRequest{
		view: denproto.DeviceRequest{
			ID: denproto.Random(denproto.RequestIDSize), KeyID: keyID, Label: label,
			RequestedAt: now.UnixMilli(), ExpiresAt: now.Add(requestLifetime).UnixMilli(), Offer: req.Offer,
		},
		tokenHash: denproto.Hash(token), member: member, username: username, pub: req.PublicKey, verifier: hash,
		status: denproto.PendingWaiting, version: 1, changed: make(chan struct{}),
	}
	rs := &d.requests
	rs.mu.Lock()
	rs.prune(now)
	mine := 0
	for _, other := range rs.byID {
		if other.member == member && other.waiting() {
			mine++
		}
	}
	if mine >= maxMemberRequests || len(rs.byID) >= maxRequests {
		rs.mu.Unlock()
		return denproto.PendingSignIn{}, denproto.Errorf(http.StatusTooManyRequests, denproto.CodeRateLimited, "too many sign-ins waiting")
	}
	rs.byID[string(r.view.ID)] = r
	rs.byToken[string(r.tokenHash)] = r
	view := r.view
	rs.mu.Unlock()
	d.log.Infof("Member %d asked to sign in on a new device", member)
	if err := d.Hub.Publish(denproto.EventDeviceRequest, view, OnlyMember(member)); err != nil {
		return denproto.PendingSignIn{}, err
	}
	return denproto.PendingSignIn{PendingToken: token, ExpiresAt: view.ExpiresAt}, nil
}

// pendingRequest finds a new device's request by its token; the caller
// holds requests.mu.
func (d *Den) pendingRequest(token []byte) (*deviceRequest, error) {
	d.requests.prune(d.now())
	r, ok := d.requests.byToken[string(denproto.Hash(token))]
	if len(token) != denproto.TokenSize || !ok {
		return nil, errPendingUnknown
	}
	return r, nil
}

// Pending tells a new device where its sign-in stands, once it has moved
// past the version the device last saw, or after pendingWait if it hasn't.
func (d *Den) Pending(ctx context.Context, token []byte, after int64) (denproto.PendingStatus, error) {
	timer := time.NewTimer(pendingWait)
	defer timer.Stop()
	for {
		d.requests.mu.Lock()
		r, err := d.pendingRequest(token)
		if err != nil {
			d.requests.mu.Unlock()
			return denproto.PendingStatus{}, err
		}
		st, wait := r.pending(), r.changed
		d.requests.mu.Unlock()
		if st.Version > after {
			return st, nil
		}
		select {
		case <-wait:
		case <-timer.C:
			return st, nil
		case <-ctx.Done():
			return denproto.PendingStatus{}, ctx.Err()
		}
	}
}

// RevealPending takes the new device's reveal, once one of the member's
// devices answered. It must open the commitment in the device's offer.
func (d *Den) RevealPending(ctx context.Context, token []byte, rev denproto.ExchangeReveal) error {
	info, _ := d.Info()
	d.requests.mu.Lock()
	r, err := d.pendingRequest(token)
	if err != nil {
		d.requests.mu.Unlock()
		return err
	}
	if r.status != denproto.PendingAnswered || r.view.Reveal != nil {
		d.requests.mu.Unlock()
		return errRequestStep
	}
	if !rev.Opens(denproto.DeviceExchangeContext(info.ID, r.username, r.view.KeyID), r.view.Offer) {
		d.requests.mu.Unlock()
		return invalid("reveal: it doesn't open the offer's commitment")
	}
	r.view.Reveal = &rev
	r.change(r.status)
	view, member := r.view, r.member
	d.requests.mu.Unlock()
	return d.Hub.Publish(denproto.EventDeviceRequest, view, OnlyMember(member))
}

// memberRequest finds a request of the session's member's that they can
// still act on; the caller holds requests.mu.
func (d *Den) memberRequest(s *Session, id []byte) (*deviceRequest, error) {
	d.requests.prune(d.now())
	r, ok := d.requests.byID[string(id)]
	if !ok || r.member != s.MemberID || !r.waiting() {
		return nil, errRequestNotFound
	}
	return r, nil
}

// AnswerRequest answers a new device's offer from one of the member's
// devices, which alone may then approve it.
func (d *Den) AnswerRequest(ctx context.Context, s *Session, id []byte, req denproto.DeviceAnswerRequest) (denproto.DeviceRequest, error) {
	if err := req.Answer.Check(); err != nil {
		return denproto.DeviceRequest{}, invalid("answer: %v", err)
	}
	d.requests.mu.Lock()
	r, err := d.memberRequest(s, id)
	if err == nil && r.status != denproto.PendingWaiting {
		err = errRequestStep
	}
	if err != nil {
		d.requests.mu.Unlock()
		return denproto.DeviceRequest{}, err
	}
	answer := req.Answer
	r.view.Answer, r.view.AnsweredBy = &answer, append(denproto.Bytes{}, s.KeyID...)
	r.change(denproto.PendingAnswered)
	view := r.view
	d.requests.mu.Unlock()
	return view, d.Hub.Publish(denproto.EventDeviceRequest, view, OnlyMember(s.MemberID))
}

// ApproveRequest registers a new device, from the member's device that
// answered it, once the new device revealed its keys. The handover is the
// member's seal, sealed for the new device with the exchange's key, which
// the den carries and can't open. A new password since the request, or a
// member who left, cancels it instead.
func (d *Den) ApproveRequest(ctx context.Context, s *Session, id []byte, req denproto.ApproveRequest) error {
	if len(req.Handover) != denproto.HandoverSize {
		return invalid("handover: a handed-over seal is %d bytes", denproto.HandoverSize)
	}
	d.requests.mu.Lock()
	defer d.requests.mu.Unlock()
	r, err := d.memberRequest(s, id)
	if err != nil {
		return err
	}
	if r.view.Reveal == nil || !denproto.Equal(r.view.AnsweredBy, s.KeyID) {
		return errRequestStep
	}
	now := d.now()
	expires := now.Add(d.TokenLifetime)
	token := denproto.Random(denproto.TokenSize)
	var dev denproto.Device
	var left int
	var sealCheck []byte
	err = d.tx(ctx, func(tx *sql.Tx) error {
		var hash []byte
		err := tx.QueryRowContext(ctx, `SELECT verifier_hash, seal_check FROM den_members WHERE id = ? AND left_at IS NULL AND banned_at IS NULL`,
			r.member).Scan(&hash, &sealCheck)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !denproto.Equal(hash, r.verifier) {
			return errRequestNotFound
		}
		if err != nil {
			return err
		}
		if dev, err = addDevice(ctx, tx, r.member, r.pub, r.view.Label, token, now, expires); err != nil {
			return err
		}
		left, err = codesLeft(ctx, tx, r.member)
		return err
	})
	if errors.Is(err, errRequestNotFound) {
		r.change(denproto.PendingCancelled)
		return errors.Join(err, d.Hub.Publish(denproto.EventDeviceRequestEnded,
			denproto.DeviceRequestEnded{ID: r.view.ID, Outcome: denproto.PendingCancelled}, OnlyMember(r.member)))
	}
	if err != nil {
		return err
	}
	m, err := d.Member(ctx, r.member)
	if err != nil {
		return err
	}
	info, _ := d.Info()
	r.session = &denproto.SignInResponse{Token: token, ExpiresAt: expires.UnixMilli(), Den: info, Member: m, RecoveryCodesLeft: left, SealCheck: sealCheck}
	r.handover = req.Handover
	r.change(denproto.PendingApproved)
	d.log.Infof("Member %d approved a sign-in on a new device", r.member)
	return errors.Join(
		d.Hub.Publish(denproto.EventDeviceAdded, dev, OnlyMember(r.member)),
		d.Hub.Publish(denproto.EventDeviceRequestEnded, denproto.DeviceRequestEnded{ID: r.view.ID, Outcome: denproto.PendingApproved}, OnlyMember(r.member)),
	)
}

// RefuseRequest keeps a new device out, from any of the member's devices.
// Someone has their password, so the client tells them to change it.
func (d *Den) RefuseRequest(ctx context.Context, s *Session, id []byte) error {
	d.requests.mu.Lock()
	r, err := d.memberRequest(s, id)
	if err != nil {
		d.requests.mu.Unlock()
		return err
	}
	r.change(denproto.PendingRefused)
	view := r.view
	d.requests.mu.Unlock()
	d.log.Infof("Member %d refused a sign-in on a new device", s.MemberID)
	return d.Hub.Publish(denproto.EventDeviceRequestEnded, denproto.DeviceRequestEnded{ID: view.ID, Outcome: denproto.PendingRefused}, OnlyMember(s.MemberID))
}

// cancelRequests cancels a member's waiting sign-ins, as a new password or
// seal, or their leaving, does.
func (d *Den) cancelRequests(member int64) {
	d.requests.mu.Lock()
	var ended [][]byte
	for _, r := range d.requests.byID {
		if r.member == member && r.waiting() {
			r.change(denproto.PendingCancelled)
			ended = append(ended, r.view.ID)
		}
	}
	d.requests.mu.Unlock()
	for _, id := range ended {
		if err := d.Hub.Publish(denproto.EventDeviceRequestEnded, denproto.DeviceRequestEnded{ID: id, Outcome: denproto.PendingCancelled}, OnlyMember(member)); err != nil {
			d.log.Errorf("announce a cancelled sign-in: %v", err)
		}
	}
}

// memberRequests lists the sign-ins waiting for a member's approval,
// oldest first.
func (d *Den) memberRequests(member int64) []denproto.DeviceRequest {
	d.requests.mu.Lock()
	defer d.requests.mu.Unlock()
	d.requests.prune(d.now())
	out := []denproto.DeviceRequest{}
	for _, r := range d.requests.byID {
		if r.member == member && r.waiting() {
			out = append(out, r.view)
		}
	}
	slices.SortFunc(out, func(a, b denproto.DeviceRequest) int { return int(a.RequestedAt - b.RequestedAt) })
	return out
}
