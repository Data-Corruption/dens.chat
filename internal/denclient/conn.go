package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/coder/websocket"
)

// State is where a den connection stands.
type State string

const (
	StateConnecting State = "connecting"
	StateConnected  State = "connected"
	StateOffline    State = "offline" // retrying
	StateRevoked    State = "revoked" // this device can't sign in any more
	StateRemoved    State = "removed" // left, removed or banned from the den
)

// Status describes a joined den and its connection, for the page.
// Fingerprint is the start of the den's ID, for members to compare.
type Status struct {
	DenID       string `json:"den_id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Own         bool   `json:"own"`
	State       State  `json:"state"`
	Error       string `json:"error,omitempty"`
	Since       int64  `json:"since"`
	// Seal says this device holds the member's DM seal here; Requests are
	// sign-ins on new devices waiting for their approval (M1.7).
	Seal     bool          `json:"seal"`
	Requests []RequestView `json:"requests,omitempty"`
}

var errDialUnauthorized = errors.New("the den no longer accepts this session")

// conn keeps one joined den connected.
type conn struct {
	m   *Manager
	j   *joined
	api *api
	own bool

	mu      sync.Mutex
	profile Profile
	den     *denState // nil until the first ready
	state   State
	errMsg  string
	since   time.Time
	token   denproto.Bytes
	expires time.Time

	loginMu sync.Mutex

	// ws is the open socket, for frames to the den; focus is the channels
	// pages here show, which the den hears on every (re)connection.
	ws    *websocket.Conn
	focus []string

	stop context.CancelFunc // ends run, when the den is forgotten
	done chan struct{}      // closed when run has returned
	wake chan struct{}      // cuts a wait between reconnects short

	// DMs (M1.7): those whose exchange waits on this member, for keyWork;
	// the keys of DM files this service opened; files uploaded for DMs,
	// waiting to be sent; and sign-ins this device is approving, or
	// approved, whose digits the new device may still be waiting for.
	due       map[string]bool
	keysDue   chan struct{}
	dmFiles   map[string]dmFile
	uploads   map[string]dmUpload
	approvals map[string]*approval
	approved  map[string]RequestView

	// Only the run goroutine touches these.
	epoch string
	seq   uint64
}

func (c *conn) status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{
		DenID: c.j.denID.String(), Fingerprint: denproto.Fingerprint(c.j.denID), Name: c.profile.Name, URL: c.profile.URL,
		Username: c.profile.Member.Username, DisplayName: c.profile.Member.DisplayName, Role: c.profile.Member.Role,
		Own: c.own, State: c.state, Error: c.errMsg, Since: c.since.UnixMilli(),
		Seal: c.j.seal != nil, Requests: c.requestsLocked(),
	}
}

func (c *conn) setState(state State, msg string) {
	c.mu.Lock()
	changed := c.state != state || c.errMsg != msg
	if changed {
		c.state, c.errMsg, c.since = state, msg, time.Now()
	}
	c.mu.Unlock()
	if changed {
		c.m.notify()
	}
}

// run keeps the den connected until ctx ends or the device is revoked.
func (c *conn) run(ctx context.Context) {
	b := backoff{min: 500 * time.Millisecond, max: 30 * time.Second}
	for ctx.Err() == nil {
		if _, err := c.session(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			if revoked(err) {
				c.setState(StateRevoked, "This den no longer accepts this device.")
				return
			}
			c.setState(StateOffline, describe(err))
			c.pause(ctx, b.next())
			continue
		}
		connectedAt, code, err := c.stream(ctx)
		if ctx.Err() != nil {
			return
		}
		if !connectedAt.IsZero() && time.Since(connectedAt) >= time.Minute {
			b.reset()
		}
		var wait time.Duration
		msg := describe(err)
		switch {
		case code == websocket.StatusServiceRestart:
			// Spread 500 clients out instead of reconnecting at once.
			wait = jitter(500*time.Millisecond, 5*time.Second)
			msg = "The den is restarting."
		case code == denproto.CloseSessionExpired, errors.Is(err, errDialUnauthorized):
			c.dropToken()
			wait = b.next()
		case code == denproto.CloseRevoked:
			c.setState(farewell(err))
			return
		case code == denproto.CloseTooSlow:
		case code == denproto.CloseRateLimited:
			wait = jitter(10*time.Second, 30*time.Second)
		default:
			wait = b.next()
		}
		c.setState(StateOffline, msg)
		c.pause(ctx, wait)
	}
}

// pause waits between reconnects, or until poked.
func (c *conn) pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	case <-c.wake:
	}
}

// poke has the den reconnect now rather than after its backoff, as when
// the member gives it a new address.
func (c *conn) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// farewell describes a den closing the socket for good, by its reason.
func farewell(err error) (State, string) {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Reason {
		case denproto.CloseReasonBanned:
			return StateRemoved, "You were banned from this den."
		case denproto.CloseReasonRemoved:
			return StateRemoved, "You were removed from this den."
		case denproto.CloseReasonLeft:
			return StateRemoved, "You left this den."
		case denproto.CloseReasonKeyRevoked:
			return StateRevoked, "This device was signed out of this den. Sign in again with your den password."
		case denproto.CloseReasonPasswordChanged:
			return StateRevoked, "Your den password was changed on another device, which signed this one out. " +
				"Sign in again with the new password; if you didn't change it, use a recovery code."
		case denproto.CloseReasonStartedOver:
			return StateRevoked, "You started over with a new DM seal on another device, which signed this one out. " +
				"Sign in again, and approve it from that device."
		}
	}
	return StateRevoked, "This den no longer accepts this device. Sign in again with your den password."
}

func revoked(err error) bool {
	return denproto.IsCode(err, denproto.CodeUnauthorized) || denproto.IsCode(err, denproto.CodeKeyRevoked) ||
		denproto.IsCode(err, denproto.CodeBanned)
}

func describe(err error) string {
	if err == nil {
		return ""
	}
	var moved *MovedError
	if errors.As(err, &moved) {
		return "The den moved to " + moved.URL + ". " + describe(moved.Err)
	}
	var perr *denproto.Error
	if errors.As(err, &perr) {
		switch perr.Code {
		case denproto.CodeProtocolUnsupported:
			return "This den needs a newer or older Dens."
		case denproto.CodeRateLimited:
			return "The den asked us to slow down."
		case denproto.CodeDenNotCreated:
			return "The den isn't set up yet."
		}
		if msg, ok := ExplainStatus(perr); ok {
			return msg
		}
		return "The den refused the connection (" + perr.Code + ")."
	}
	if errors.Is(err, denproto.ErrWrongIdentity) {
		return "The server at the den's address can't prove it is this den."
	}
	return Explain(err)
}

// session returns a token that is valid for at least a minute, signing in
// again when needed. Page requests and the socket share it.
func (c *conn) session(ctx context.Context) (denproto.Bytes, error) {
	valid := func() denproto.Bytes {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.token != nil && time.Until(c.expires) > c.m.MinTokenLife {
			return c.token
		}
		return nil
	}
	if t := valid(); t != nil {
		return t, nil
	}
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if t := valid(); t != nil {
		return t, nil
	}
	a, nonce, err := c.challenge(ctx)
	if err != nil {
		return nil, err
	}
	keyID := denproto.ID(c.j.key.Public())
	var resp denproto.SessionResponse
	err = a.call(ctx, http.MethodPost, "/api/auth/login", nil, denproto.LoginRequest{
		KeyID: keyID, Nonce: nonce, Proof: c.j.key.Sign(denproto.ProofMessage(c.j.denID, keyID, nonce)),
	}, &resp)
	if err != nil {
		return nil, err
	}
	if err := denproto.Size("token", resp.Token, denproto.TokenSize); err != nil {
		return nil, err
	}
	c.setToken(resp.Token, time.UnixMilli(resp.ExpiresAt))
	return resp.Token, nil
}

func (c *conn) setToken(token denproto.Bytes, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.expires = token, expires
}

func (c *conn) dropToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = nil
}

func (c *conn) expiry() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.expires
}

// remote is where the den is reached, which changes when it moves.
func (c *conn) remote() *api {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.api
}

// challenge gets a nonce from the den, following it to a new address it
// signs and keeping that address from then on.
func (c *conn) challenge(ctx context.Context) (*api, denproto.Bytes, error) {
	old := c.remote()
	a, nonce, _, err := old.reach(ctx, c.j.denID)
	if err != nil || a == old {
		return a, nonce, err
	}
	return a, nonce, c.moveTo(ctx, a)
}

// moveTo keeps a den's new address, where it has just proven itself.
func (c *conn) moveTo(ctx context.Context, a *api) error {
	c.mu.Lock()
	c.api = a
	p := c.profile
	p.URL = a.base
	c.profile = p
	c.mu.Unlock()
	c.m.log.Infof("A den moved to a new address")
	c.m.notify()
	return updateProfile(ctx, c.m.db, c.m.v, c.j.denID, p)
}

// call makes an authenticated request, signing in again once if the den
// no longer accepts the token.
func (c *conn) call(ctx context.Context, method, path string, req, resp any) error {
	token, err := c.session(ctx)
	if err != nil {
		return err
	}
	err = c.remote().call(ctx, method, path, token, req, resp)
	if denproto.IsCode(err, denproto.CodeUnauthorized) {
		c.dropToken()
		if token, err = c.session(ctx); err != nil {
			return err
		}
		err = c.remote().call(ctx, method, path, token, req, resp)
	}
	return err
}

// stream holds one WebSocket connection until it closes, and returns when
// it came up (zero if it never did) and its close status.
func (c *conn) stream(ctx context.Context) (connectedAt time.Time, code websocket.StatusCode, err error) {
	token, err := c.session(ctx)
	if err != nil {
		return time.Time{}, -1, err
	}
	a := c.remote()
	u := a.base + "/api/ws"
	if c.epoch != "" {
		u += "?resume=" + c.epoch + "." + strconv.FormatUint(c.seq, 10)
	}
	h := http.Header{}
	a.headers(h, token)
	c.setState(StateConnecting, "")
	ws, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: a.client, HTTPHeader: h})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return time.Time{}, -1, errDialUnauthorized
		}
		if resp != nil {
			return time.Time{}, -1, denproto.ReadError(resp)
		}
		return time.Time{}, -1, err
	}
	defer ws.CloseNow()
	ws.SetReadLimit(denproto.MaxBody)
	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.ws = nil
		c.mu.Unlock()
		// The den ends a call with the socket that joined it.
		c.m.denDropped(c.j.denID.String())
	}()

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.ping(sctx, cancel, ws)
	go c.renew(sctx, ws)
	for {
		_, data, err := ws.Read(sctx)
		if err != nil {
			return connectedAt, websocket.CloseStatus(err), err
		}
		events, err := denproto.DecodeFrame(data)
		if err != nil {
			ws.Close(websocket.StatusInvalidFramePayloadData, "malformed frame")
			return connectedAt, -1, err
		}
		var forward []denproto.Event
		fresh := false
		for _, e := range events {
			out, ok, err := c.apply(ctx, e)
			if err != nil {
				c.m.log.Warnf("den protocol violation: %v", err)
				ws.Close(websocket.StatusPolicyViolation, "protocol violation")
				return connectedAt, -1, err
			}
			if ok {
				forward = append(forward, out)
			}
			if e.T == denproto.EventReady || e.T == denproto.EventResumed {
				if connectedAt.IsZero() {
					connectedAt = time.Now()
				}
				c.setState(StateConnected, "")
				fresh = true
			}
		}
		// The den forgets focus with each connection and snapshot.
		if fresh {
			c.mu.Lock()
			focus := c.focus
			c.mu.Unlock()
			c.send(ws, denproto.EventFocus, denproto.Focus{Channels: append([]string{}, focus...)})
		}
		var reads []denproto.ReadState
		c.mu.Lock()
		if c.den != nil {
			reads = c.den.takeTouched()
		}
		c.mu.Unlock()
		if len(forward) > 0 || len(reads) > 0 {
			c.m.publish(PageEvent{DenID: c.j.denID.String(), Events: forward, Reads: reads})
		}
	}
}

// apply handles one event from the den, and returns what to forward to
// the page, if anything. Everything a den sends is checked before it is
// kept or forwarded: dens are other people's servers.
func (c *conn) apply(ctx context.Context, e denproto.Event) (denproto.Event, bool, error) {
	defer func() {
		if e.Seq > c.seq {
			c.seq = e.Seq
		}
	}()
	switch e.T {
	case denproto.EventReady:
		var r denproto.Ready
		if err := json.Unmarshal(e.D, &r); err != nil {
			return e, false, fmt.Errorf("malformed ready: %w", err)
		}
		state := newState()
		if err := state.load(r); err != nil {
			return e, false, err
		}
		c.epoch, c.seq = r.Epoch, r.Seq
		if err := c.updateProfile(ctx, r.Den, &r.Me); err != nil {
			return e, false, err
		}
		c.mu.Lock()
		c.den = state
		c.markDueLocked(c.dueKeysLocked()...)
		clear(c.dmFiles)
		c.mu.Unlock()
		c.m.notify()
		// What the member can see may have changed, so cached files go
		// too, and the page drops what it holds for this den and loads it
		// again.
		c.m.files.drop(c.j.denID.String())
		c.m.publish(PageEvent{DenID: c.j.denID.String(), Reset: true})
		return e, false, nil
	case denproto.EventDenUpdated:
		var d denproto.Den
		if err := json.Unmarshal(e.D, &d); err != nil {
			return e, false, fmt.Errorf("malformed den.updated: %w", err)
		}
		if !cleanLimits(d) {
			return e, false, errMalformed
		}
		c.mu.Lock()
		if c.den != nil {
			c.den.limits, c.den.callLimits = d.Limits, d.CallLimits
		}
		c.mu.Unlock()
		if err := c.updateProfile(ctx, d, nil); err != nil {
			return e, false, err
		}
		// The page shows the den's name and limits; the address is this
		// install's business.
		out, err := denproto.NewEvent(e.T, e.Seq, denproto.Den{ID: c.j.denID, Name: c.status().Name, Limits: d.Limits,
			CallLimits: d.CallLimits})
		return out, err == nil, err
	case denproto.EventVoiceOffer, denproto.EventVoiceEnded, denproto.EventVoiceResumed, denproto.EventVoiceRefused:
		// These go to the page holding the call, not to every page.
		return e, false, c.callEvent(e)
	case denproto.EventAuthRenewed:
		var r denproto.Renewed
		if json.Unmarshal(e.D, &r) == nil {
			c.mu.Lock()
			if until := time.UnixMilli(r.ExpiresAt); until.After(c.expires) {
				c.expires = until
			}
			c.mu.Unlock()
		}
		return e, false, nil
	}
	c.mu.Lock()
	if c.den == nil {
		c.mu.Unlock()
		return e, false, nil
	}
	out, ok, err := c.den.applyEvent(e, c.profile.Member)
	me := c.profile.Member.ID
	gone := c.den.gone
	c.den.gone = nil
	switch {
	case err != nil:
	case e.T == denproto.EventMessageCreated || e.T == denproto.EventMessageUpdated:
		// A DM's message reaches the page opened, or saying why it can't be.
		var m denproto.Message
		if json.Unmarshal(out.D, &m) == nil && m.Sealed != nil {
			out, err = denproto.NewEvent(out.T, out.Seq, c.openLocked(m))
		}
	case e.T == denproto.EventDMKey:
		var k denproto.DMKey
		if json.Unmarshal(e.D, &k) == nil && needsMe(k, me) {
			c.markDueLocked(k.ChannelID)
		}
	}
	requests := e.T == denproto.EventDeviceRequest || e.T == denproto.EventDeviceRequestEnded
	c.mu.Unlock()
	if requests && err == nil {
		c.requestChanged(e)
	}
	if len(gone) > 0 {
		c.m.files.drop(c.j.denID.String(), gone...)
		c.mu.Lock()
		for _, id := range gone {
			delete(c.dmFiles, id)
		}
		c.mu.Unlock()
	}
	if err == nil && e.T == denproto.EventChannelDeleted {
		c.m.files.drop(c.j.denID.String())
	}
	if err == nil && ok && out.T == denproto.EventMemberUpdated {
		var m denproto.Member
		if json.Unmarshal(out.D, &m) == nil && m.ID == me {
			return out, ok, c.updateMe(ctx, m)
		}
	}
	return out, ok, err
}

// updateMe keeps this member's own account current as the den reports a
// change to it, such as a new role.
func (c *conn) updateMe(ctx context.Context, m denproto.Member) error {
	c.mu.Lock()
	p := c.profile
	p.Member = m
	changed := p != c.profile
	c.profile = p
	c.mu.Unlock()
	if !changed {
		return nil
	}
	c.m.notify()
	return updateProfile(ctx, c.m.db, c.m.v, c.j.denID, p)
}

// send writes one client frame to the den. Frames are hints the den
// repeats on its side as needed, so a failed write is only dropped.
func (c *conn) send(ws *websocket.Conn, t string, data any) {
	_ = writeFrame(ws, t, data)
}

func writeFrame(ws *websocket.Conn, t string, data any) error {
	e, err := denproto.NewEvent(t, 0, data)
	if err != nil {
		return err
	}
	frame, err := denproto.EncodeFrame(e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ws.Write(ctx, websocket.MessageText, frame)
}

// sendFrame writes a client frame to the den, and reports whether it
// could: a call needs to know, where a hint doesn't.
func (c *conn) sendFrame(t string, data any) error {
	c.mu.Lock()
	ws := c.ws
	c.mu.Unlock()
	if ws == nil {
		return errNotConnected
	}
	return writeFrame(ws, t, data)
}

var errNotConnected = errors.New("the den isn't connected")

// setFocus records the channels the pages show and tells the den.
func (c *conn) setFocus(channels []string) {
	c.mu.Lock()
	if slices.Equal(c.focus, channels) {
		c.mu.Unlock()
		return
	}
	c.focus = channels
	ws := c.ws
	c.mu.Unlock()
	if ws != nil {
		c.send(ws, denproto.EventFocus, denproto.Focus{Channels: channels})
	}
}

// typing tells the den this member is typing in a channel.
func (c *conn) typing(channel string) {
	c.mu.Lock()
	ws := c.ws
	c.mu.Unlock()
	if ws != nil {
		c.send(ws, denproto.EventTyping, denproto.Typing{ChannelID: channel})
	}
}

// updateProfile keeps the den's name and address, and this member's
// account, current. A den that names another identity is refused.
func (c *conn) updateProfile(ctx context.Context, d denproto.Den, me *denproto.Member) error {
	if !denproto.Equal(d.ID, c.j.denID) {
		return errors.New("the den described itself with another identity")
	}
	c.mu.Lock()
	p := c.profile
	if name, err := denproto.CleanName(d.Name, denproto.MaxNameRunes); err == nil {
		p.Name = name
	}
	if u, err := denproto.NormalizeDenURL(d.URL); err == nil {
		p.URL = u
	}
	if me != nil {
		if m, ok := cleanMember(*me); ok {
			p.Member = m
		}
	}
	changed := p != c.profile
	follow := !c.own && p.URL != c.api.base
	c.profile = p
	c.mu.Unlock()
	if follow {
		go c.follow(ctx, p.URL)
	}
	if !changed {
		return nil
	}
	c.m.notify()
	return updateProfile(ctx, c.m.db, c.m.v, c.j.denID, p)
}

// follow moves to the address the den announced once it proves itself
// there, so this session's token stops going to the old one. If it can't
// be reached yet, the den stays where it is, and the next challenge there
// sends it on.
func (c *conn) follow(ctx context.Context, url string) {
	a := &api{base: url, client: c.m.HTTP, agent: c.m.agent}
	if a, _, _, err := a.reach(ctx, c.j.denID); err == nil {
		_ = c.moveTo(ctx, a)
	}
}

func cleanMember(m denproto.Member) (denproto.Member, bool) {
	username, err := denproto.NormalizeUsername(m.Username)
	if err != nil || !validID(m.ID) || m.JoinedAt < 0 || m.LeftAt < 0 || denproto.CheckBio(m.Bio) != nil ||
		denproto.CheckImage(m.Avatar) != nil || denproto.CheckImage(m.Banner) != nil {
		return m, false
	}
	display, err := denproto.CleanName(m.DisplayName, denproto.MaxNameRunes)
	if err != nil {
		return m, false
	}
	switch m.Role {
	case denproto.RoleMember, denproto.RoleModerator, denproto.RoleOwner:
	default:
		return m, false
	}
	m.Username, m.DisplayName = username, display
	return m, true
}

// ping checks the connection is alive; a den that stops answering is
// reconnected rather than waited on.
func (c *conn) ping(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn) {
	t := time.NewTicker(c.m.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, c.m.PingTimeout)
			err := ws.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

// renew extends the session on the open socket before it expires. If the
// den doesn't confirm, it tries again; if the token runs out anyway, the den
// closes the socket with 4001 and run signs in again.
func (c *conn) renew(ctx context.Context, ws *websocket.Conn) {
	for {
		wait := time.Until(c.expiry().Add(-c.m.RenewLead))
		if wait > 0 {
			if !sleep(ctx, wait) {
				return
			}
			continue
		}
		if _, nonce, err := c.challenge(ctx); err == nil {
			keyID := denproto.ID(c.j.key.Public())
			e, _ := denproto.NewEvent(denproto.EventAuthRenew, 0, denproto.Renew{
				Nonce: nonce, Proof: c.j.key.Sign(denproto.ProofMessage(c.j.denID, keyID, nonce)),
			})
			if frame, err := denproto.EncodeFrame(e); err == nil {
				_ = ws.Write(ctx, websocket.MessageText, frame)
			}
		}
		if !sleep(ctx, c.m.RenewRetry) {
			return
		}
	}
}

type backoff struct {
	min, max, cur time.Duration
}

// next returns an exponential delay with full jitter.
func (b *backoff) next() time.Duration {
	if b.cur == 0 {
		b.cur = b.min
	} else {
		b.cur = min(b.cur*2, b.max)
	}
	return time.Duration(rand.Int64N(int64(b.cur)) + 1)
}

func (b *backoff) reset() { b.cur = 0 }

func jitter(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(rand.Int64N(int64(hi-lo)))
}

// sleep waits for d, and reports false if ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
