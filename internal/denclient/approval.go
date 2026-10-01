package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Approving new devices (M1.7). A password alone doesn't add a device to a
// den: one of the member's other devices approves it. The two run the
// exchange a DM's key starts with, through the den, and the member types
// each device's digits into the other; neither goes on unless its own
// member typed what the other shows. The approving device then hands over
// the member's DM seal, sealed with the exchange's key, which the den
// carries but can't open.

// RequestView is a sign-in on a new device waiting for this member's
// approval, as the page shows it. Here says this device answered it, and
// Elsewhere that another of theirs did; Lost that this device answered it
// before this service last started, which forgot its side of the
// exchange. Half is the digits this device shows, once both devices can
// work them out. Approved marks one this device approved, which still
// shows its digits, since the new device may be waiting for its member to
// type them.
type RequestView struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	RequestedAt int64  `json:"requested_at"`
	ExpiresAt   int64  `json:"expires_at"`
	Here        bool   `json:"here,omitempty"`
	Elsewhere   bool   `json:"elsewhere,omitempty"`
	Lost        bool   `json:"lost,omitempty"`
	Half        string `json:"half,omitempty"`
	Approved    bool   `json:"approved,omitempty"`
}

// approval is a sign-in this device answered: its side of the exchange,
// and, once the new device revealed, what the exchange made. The request
// is kept for the page once it's approved.
type approval struct {
	context   []byte
	responder denproto.Responder
	result    *denproto.Result
	request   denproto.DeviceRequest
}

func (a *approval) clear() {
	clear(a.responder.X)
	clear(a.responder.Shared)
	if a.result != nil {
		clear(a.result.Key)
	}
}

// requestsLocked lists the sign-ins waiting for this member's approval;
// the caller holds c.mu.
func (c *conn) requestsLocked() []RequestView {
	if c.den == nil {
		return nil
	}
	keyID := denproto.ID(c.j.key.Public())
	now := time.Now().UnixMilli()
	var out []RequestView
	for id, r := range c.den.requests {
		if now >= r.ExpiresAt {
			continue
		}
		v := RequestView{ID: id, Label: r.Label, RequestedAt: r.RequestedAt, ExpiresAt: r.ExpiresAt}
		if r.AnsweredBy != nil {
			a := c.approvals[id]
			mine := denproto.Equal(r.AnsweredBy, keyID)
			v.Here, v.Elsewhere, v.Lost = mine && a != nil, !mine, mine && a == nil
			if v.Here && a.result != nil {
				v.Half = denproto.FormatCheckHalf(denproto.CheckHalf(a.result.Check, false))
			}
		}
		out = append(out, v)
	}
	for id, v := range c.approved {
		if now < v.ExpiresAt {
			out = append(out, v)
		} else {
			delete(c.approved, id)
		}
	}
	slices.SortFunc(out, func(a, b RequestView) int { return int(a.RequestedAt - b.RequestedAt) })
	return out
}

// requestChanged works out what an exchange made once the new device it
// approves reveals, forgets the sign-ins that ended, keeping the digits of
// one this device approved for the page, and tells the page.
func (c *conn) requestChanged(e denproto.Event) {
	var ended denproto.DeviceRequestEnded
	if e.T == denproto.EventDeviceRequestEnded {
		_ = json.Unmarshal(e.D, &ended)
	}
	c.mu.Lock()
	for id, a := range c.approvals {
		r, ok := c.den.requests[id]
		if !ok {
			if ended.Outcome == denproto.PendingApproved && ended.ID.String() == id && a.result != nil {
				c.approved[id] = RequestView{ID: id, Label: a.request.Label, RequestedAt: a.request.RequestedAt,
					ExpiresAt: a.request.ExpiresAt, Half: denproto.FormatCheckHalf(denproto.CheckHalf(a.result.Check, false)), Approved: true}
			}
			a.clear()
			delete(c.approvals, id)
			continue
		}
		if a.result == nil && r.Reveal != nil {
			if res, err := a.responder.Finish(*r.Reveal); err == nil {
				a.result = &res
			} else {
				c.m.log.Warnf("A new device's reveal doesn't fit its offer")
			}
		}
	}
	c.mu.Unlock()
	c.m.notify()
}

// AnswerSignIn starts approving a sign-in from this device by answering
// the new device's offer. The member then compares digits on both.
func (m *Manager) AnswerSignIn(ctx context.Context, denID, requestID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	var r denproto.DeviceRequest
	ok := false
	if c.den != nil {
		r, ok = c.den.requests[requestID]
	}
	hasSeal := c.j.seal != nil
	username := c.profile.Member.Username
	c.mu.Unlock()
	if !ok {
		return inputError(errors.New("that sign-in isn't waiting any more"))
	}
	if !hasSeal {
		return inputError(errors.New("this device doesn't have your DM seal, so it can't pass it on: approve on another device, or type your seal here first"))
	}
	xctx := denproto.DeviceExchangeContext(c.j.denID, username, r.KeyID)
	responder, answer, err := denproto.AnswerExchange(xctx, r.Offer)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.approvals[requestID] = &approval{context: xctx, responder: responder, request: r}
	c.mu.Unlock()
	if err := c.call(ctx, http.MethodPost, "/api/me/device-requests/"+r.ID.String()+"/answer", denproto.DeviceAnswerRequest{Answer: answer}, nil); err != nil {
		c.mu.Lock()
		if a := c.approvals[requestID]; a != nil {
			a.clear()
			delete(c.approvals, requestID)
		}
		c.mu.Unlock()
		if denproto.IsCode(err, denproto.CodeKeyExists) {
			return inputError(errors.New("another of your devices is approving that sign-in"))
		}
		return err
	}
	c.m.notify()
	return nil
}

// ApproveSignIn takes the digits the new device shows, as the member typed
// them. If they match, this device hands over the member's DM seal, and
// the den lets the new device in.
func (m *Manager) ApproveSignIn(ctx context.Context, denID, requestID, digits string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	a := c.approvals[requestID]
	var result denproto.Result
	if a != nil && a.result != nil {
		result = *a.result
	}
	c.mu.Unlock()
	if a == nil || result.Key == nil {
		return inputError(errors.New("the new device hasn't shown its digits yet"))
	}
	if !denproto.CheckTyped(result.Check, false, digits) {
		return errDigits
	}
	var handover denproto.Bytes
	if err := c.useSeal(func(seal []byte) error {
		handover, err = denproto.SealHandover(result.Key, a.context, seal)
		return err
	}); err != nil {
		return err
	}
	id, err := denproto.ParseBytes(requestID, denproto.RequestIDSize)
	if err != nil {
		return inputError(errors.New("no such sign-in"))
	}
	return c.call(ctx, http.MethodPost, "/api/me/device-requests/"+id.String()+"/approve", denproto.ApproveRequest{Handover: handover}, nil)
}

// DismissApproved stops showing the digits of a sign-in this device
// approved.
func (m *Manager) DismissApproved(denID, requestID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.approved, requestID)
	c.mu.Unlock()
	m.notify()
	return nil
}

// RefuseSignIn keeps a new device out.
func (m *Manager) RefuseSignIn(ctx context.Context, denID, requestID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	id, err := denproto.ParseBytes(requestID, denproto.RequestIDSize)
	if err != nil {
		return inputError(errors.New("no such sign-in"))
	}
	return c.call(ctx, http.MethodPost, "/api/me/device-requests/"+id.String()+"/refuse", nil, nil)
}

// Stages of a sign-in on this device, as the page shows them.
const (
	SignInWaiting   = "waiting"   // for one of the member's other devices to answer
	SignInCheck     = "check"     // both devices show digits to compare
	SignInApproved  = "approved"  // the other device approved; this one waits for its member's digits
	SignInDone      = "done"      // signed in
	SignInRefused   = "refused"   // the member refused it on another device
	SignInCancelled = "cancelled" // their password or seal changed meanwhile
	SignInExpired   = "expired"   // nobody approved it in time
	SignInFailed    = "failed"    // something went wrong; Error says what
)

// SignInView is a sign-in on this device waiting for approval, as the page
// shows it. Half is the digits this device shows, and Checked says the
// member typed the other device's.
type SignInView struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Stage     string `json:"stage"`
	Half      string `json:"half,omitempty"`
	Checked   bool   `json:"checked"`
	ExpiresAt int64  `json:"expires_at"`
	Error     string `json:"error,omitempty"`
	DenID     string `json:"den_id,omitempty"`
}

// pendingSignIn is a sign-in on this device waiting for approval. The run
// goroutine follows it on the den; the page reads it through Watch.
type pendingSignIn struct {
	view     SignInView
	denID    denproto.Bytes
	api      *api
	url      string
	username string
	key      *vault.SigningKey
	context  []byte
	starter  denproto.Starter
	token    denproto.Bytes
	result   *denproto.Result
	session  *denproto.SignInResponse
	handover denproto.Bytes
	stop     context.CancelFunc
}

// end stops following a sign-in and lets its key go, unless it's done.
func (p *pendingSignIn) end(stage, msg string) {
	p.view.Stage, p.view.Error = stage, msg
	p.stop()
	if stage != SignInDone && p.key != nil {
		p.key.Close()
	}
	p.key = nil
	clear(p.starter.X)
	clear(p.starter.DK)
	if p.result != nil {
		clear(p.result.Key)
	}
}

// SignIns lists the sign-ins on this device that wait for approval, or
// just ended, oldest first.
func (m *Manager) SignIns() []SignInView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SignInView, 0, len(m.signIns))
	for _, p := range m.signIns {
		out = append(out, p.view)
	}
	slices.SortFunc(out, func(a, b SignInView) int { return compareIDs(a.ID, b.ID) })
	return out
}

// waitForApproval asks a den to let this device in with the password, and
// follows the sign-in until one of the member's other devices approves or
// refuses it. The caller holds m.joinMu.
func (m *Manager) waitForApproval(ctx context.Context, a *api, denID denproto.Bytes, url, username string, key *vault.SigningKey,
	verifier, nonce, proof denproto.Bytes) (SignInView, error) {
	xctx := denproto.DeviceExchangeContext(denID, username, denproto.ID(key.Public()))
	starter, offer, err := denproto.StartExchange(xctx)
	if err != nil {
		return SignInView{}, err
	}
	var pending denproto.PendingSignIn
	err = a.call(ctx, http.MethodPost, "/api/auth/password", nil, denproto.PasswordLoginRequest{
		Username: username, Verifier: verifier, PublicKey: denproto.Bytes(key.Public()), DeviceLabel: m.label,
		Nonce: nonce, Proof: proof, Offer: offer,
	}, &pending)
	if denproto.IsCode(err, denproto.CodeNoOtherDevice) {
		return SignInView{}, inputError(errors.New("none of your other devices can approve this one, since you have none signed in to this den: sign in with a recovery code instead"))
	}
	if err != nil {
		return SignInView{}, err
	}
	if len(pending.PendingToken) != denproto.TokenSize {
		return SignInView{}, errors.New("the den's answer to signing in is malformed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.signInSeq++
	fctx, stop := context.WithCancel(m.ctx)
	p := &pendingSignIn{
		view:  SignInView{ID: strconv.FormatUint(m.signInSeq, 10), URL: url, Username: username, Stage: SignInWaiting, ExpiresAt: pending.ExpiresAt},
		denID: denID, api: a, url: url, username: username, key: key, context: xctx, starter: starter, token: pending.PendingToken, stop: stop,
	}
	m.signIns[p.view.ID] = p
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.followSignIn(fctx, p)
	}()
	m.log.Infof("Asked to sign in to a den, waiting for approval")
	return p.view, nil
}

// followSignIn waits on the den for a sign-in to move on: an answer to
// reveal to, an approval, or a refusal.
func (m *Manager) followSignIn(ctx context.Context, p *pendingSignIn) {
	var after int64
	for ctx.Err() == nil {
		var st denproto.PendingStatus
		err := p.api.call(ctx, http.MethodGet, "/api/auth/pending?after="+strconv.FormatInt(after, 10), p.token, nil, &st)
		if ctx.Err() != nil {
			return
		}
		if denproto.IsCode(err, denproto.CodeUnauthorized) {
			m.endSignIn(p, SignInExpired, "")
			return
		}
		if err != nil {
			sleep(ctx, 2*time.Second)
			continue
		}
		if st.Version <= after {
			continue
		}
		after = st.Version
		switch st.Status {
		case denproto.PendingAnswered:
			m.mu.Lock()
			revealed := p.result != nil
			m.mu.Unlock()
			if revealed || st.Answer == nil {
				continue
			}
			reveal, res, err := p.starter.Finish(*st.Answer)
			if err != nil {
				m.endSignIn(p, SignInFailed, "The other device's answer doesn't fit this one's keys.")
				return
			}
			if err := p.api.call(ctx, http.MethodPost, "/api/auth/pending/reveal", p.token, denproto.KeyRevealRequest{Reveal: reveal}, nil); err != nil {
				m.log.Warnf("reveal to the approving device: %v", err)
			}
			m.mu.Lock()
			p.result = &res
			p.view.Stage = SignInCheck
			p.view.Half = denproto.FormatCheckHalf(denproto.CheckHalf(res.Check, true))
			m.mu.Unlock()
			m.notify()
		case denproto.PendingApproved:
			if st.Session == nil || len(st.Handover) != denproto.HandoverSize {
				m.endSignIn(p, SignInFailed, "The den's answer to the approval is malformed.")
				return
			}
			m.mu.Lock()
			p.session, p.handover = st.Session, st.Handover
			checked := p.view.Checked
			if !checked {
				p.view.Stage = SignInApproved
			}
			m.mu.Unlock()
			if checked {
				m.finishSignIn(context.WithoutCancel(ctx), p)
			} else {
				m.notify()
			}
			return
		case denproto.PendingRefused:
			m.endSignIn(p, SignInRefused, "")
			return
		case denproto.PendingCancelled:
			m.endSignIn(p, SignInCancelled, "")
			return
		}
	}
}

func (m *Manager) endSignIn(p *pendingSignIn, stage, msg string) {
	m.mu.Lock()
	p.end(stage, msg)
	m.mu.Unlock()
	m.notify()
}

// CheckSignIn takes the digits the approving device shows, as the member
// typed them into this one. The device goes on only once they match and
// the other device approved.
func (m *Manager) CheckSignIn(ctx context.Context, id, digits string) error {
	m.mu.Lock()
	p, ok := m.signIns[id]
	if !ok || p.result == nil || (p.view.Stage != SignInCheck && p.view.Stage != SignInApproved) {
		m.mu.Unlock()
		return inputError(errors.New("that sign-in isn't waiting for digits"))
	}
	if !denproto.CheckTyped(p.result.Check, true, digits) {
		m.mu.Unlock()
		return errDigits
	}
	p.view.Checked = true
	approved := p.session != nil
	m.mu.Unlock()
	if approved {
		m.finishSignIn(ctx, p)
	} else {
		m.notify()
	}
	return nil
}

// finishSignIn keeps an approved device's key and session, and the seal
// the approving device handed over, once its member checked the digits.
func (m *Manager) finishSignIn(ctx context.Context, p *pendingSignIn) {
	m.joinMu.Lock()
	defer m.joinMu.Unlock()
	m.mu.Lock()
	if p.key == nil {
		m.mu.Unlock()
		return
	}
	resp, key, res, handover := *p.session, p.key, *p.result, p.handover
	m.mu.Unlock()
	fail := func(msg string) { m.endSignIn(p, SignInFailed, msg) }
	member, ok := cleanMember(resp.Member)
	name, nameErr := denproto.CleanName(resp.Den.Name, denproto.MaxNameRunes)
	if !ok || member.Username != p.username || nameErr != nil || !denproto.Equal(resp.Den.ID, p.denID) ||
		len(resp.Token) != denproto.TokenSize || resp.RecoveryCodesLeft < 0 || resp.RecoveryCodesLeft > denproto.RecoveryCodes ||
		len(resp.SealCheck) != denproto.SealCheckSize {
		fail("The den's answer to signing in is malformed.")
		return
	}
	seal, err := denproto.OpenHandover(res.Key, p.context, handover)
	if err != nil {
		fail("The seal the other device handed over doesn't open here.")
		return
	}
	defer clear(seal)
	if check, err := denproto.SealCheck(seal, p.denID); err != nil || !denproto.Equal(check, resp.SealCheck) {
		fail("The other device handed over a DM seal that isn't yours for this den.")
		return
	}
	if c, err := m.find(p.denID.String()); err == nil {
		if !gone(c) {
			fail(ErrAlreadyJoined.Error())
			return
		}
		if err := m.forget(ctx, c); err != nil {
			fail("Dens couldn't replace this den's old sign-in.")
			return
		}
	}
	secret, err := vault.NewSecret(seal)
	if err != nil {
		fail("Dens couldn't keep the DM seal.")
		return
	}
	// The device key now belongs to the joined den.
	m.mu.Lock()
	p.key = nil
	m.mu.Unlock()
	j := &joined{denID: p.denID, profile: Profile{URL: p.url, Name: name, Member: member}, key: key, joinedAt: time.Now(), seal: secret}
	if err := insertJoined(ctx, m.db, m.v, j); err != nil {
		j.close()
		fail("Dens couldn't keep the sign-in.")
		return
	}
	if have, err := installSeal(ctx, m.db, m.v); err == nil && have == nil {
		if err := setInstallSeal(ctx, m.db, m.v, seal); err != nil {
			m.log.Errorf("keep the DM seal for new dens: %v", err)
		}
	} else {
		clear(have)
	}
	m.mu.Lock()
	m.startLocked(j, resp.Token, time.UnixMilli(resp.ExpiresAt))
	p.view.DenID = p.denID.String()
	p.end(SignInDone, "")
	m.mu.Unlock()
	m.notify()
	m.log.Infof("Signed in to a den on this device, approved by another")
}

// DismissSignIn forgets a sign-in on this device, stopping it if it's
// still waiting.
func (m *Manager) DismissSignIn(id string) {
	m.mu.Lock()
	p, ok := m.signIns[id]
	if ok {
		if p.key != nil {
			p.end(SignInCancelled, "")
		}
		delete(m.signIns, id)
	}
	m.mu.Unlock()
	if ok {
		m.notify()
	}
}

// dropSignIns lets every waiting sign-in's key go, when the manager stops.
func (m *Manager) dropSignIns() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.signIns {
		if p.key != nil {
			p.end(SignInCancelled, "")
		}
		delete(m.signIns, id)
	}
}
