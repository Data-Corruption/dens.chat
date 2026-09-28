package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
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
)

// Status describes a joined den and its connection, for the page.
type Status struct {
	DenID       string `json:"den_id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Own         bool   `json:"own"`
	State       State  `json:"state"`
	Error       string `json:"error,omitempty"`
	Since       int64  `json:"since"`
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
	state   State
	errMsg  string
	since   time.Time
	token   denproto.Bytes
	expires time.Time

	loginMu sync.Mutex

	// Only the run goroutine touches these.
	epoch string
	seq   uint64
}

func (c *conn) status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{
		DenID: c.j.denID.String(), Name: c.profile.Name, URL: c.profile.URL,
		Username: c.profile.Member.Username, DisplayName: c.profile.Member.DisplayName, Role: c.profile.Member.Role,
		Own: c.own, State: c.state, Error: c.errMsg, Since: c.since.UnixMilli(),
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
			sleep(ctx, b.next())
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
			c.setState(StateRevoked, "This den no longer accepts this device.")
			return
		case code == denproto.CloseTooSlow:
		case code == denproto.CloseRateLimited:
			wait = jitter(10*time.Second, 30*time.Second)
		default:
			wait = b.next()
		}
		c.setState(StateOffline, msg)
		sleep(ctx, wait)
	}
}

func revoked(err error) bool {
	return denproto.IsCode(err, denproto.CodeUnauthorized) || denproto.IsCode(err, denproto.CodeKeyRevoked) ||
		denproto.IsCode(err, denproto.CodeBanned)
}

func describe(err error) string {
	if err == nil {
		return ""
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
	nonce, err := c.api.challenge(ctx, c.j.denID)
	if err != nil {
		return nil, err
	}
	keyID := denproto.ID(c.j.key.Public())
	var resp denproto.SessionResponse
	err = c.api.call(ctx, http.MethodPost, "/api/auth/login", nil, denproto.LoginRequest{
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

// call makes an authenticated request, signing in again once if the den
// no longer accepts the token.
func (c *conn) call(ctx context.Context, method, path string, req, resp any) error {
	token, err := c.session(ctx)
	if err != nil {
		return err
	}
	err = c.api.call(ctx, method, path, token, req, resp)
	if denproto.IsCode(err, denproto.CodeUnauthorized) {
		c.dropToken()
		if token, err = c.session(ctx); err != nil {
			return err
		}
		err = c.api.call(ctx, method, path, token, req, resp)
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
	u := c.api.base + "/api/ws"
	if c.epoch != "" {
		u += "?resume=" + c.epoch + "." + strconv.FormatUint(c.seq, 10)
	}
	h := http.Header{}
	c.api.headers(h, token)
	c.setState(StateConnecting, "")
	ws, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.api.client, HTTPHeader: h})
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
		for _, e := range events {
			if err := c.apply(ctx, e); err != nil {
				ws.Close(websocket.StatusPolicyViolation, "protocol violation")
				return connectedAt, -1, err
			}
			if e.T == denproto.EventReady || e.T == denproto.EventResumed {
				if connectedAt.IsZero() {
					connectedAt = time.Now()
				}
				c.setState(StateConnected, "")
			}
		}
	}
}

// apply handles one event from the den. Everything a den sends is checked
// before it is kept: dens are other people's servers.
func (c *conn) apply(ctx context.Context, e denproto.Event) error {
	switch e.T {
	case denproto.EventReady:
		var r denproto.Ready
		if err := json.Unmarshal(e.D, &r); err != nil {
			return fmt.Errorf("malformed ready: %w", err)
		}
		c.epoch, c.seq = r.Epoch, r.Seq
		if err := c.updateProfile(ctx, r.Den, &r.Me); err != nil {
			return err
		}
	case denproto.EventDenUpdated:
		var d denproto.Den
		if err := json.Unmarshal(e.D, &d); err != nil {
			return fmt.Errorf("malformed den.updated: %w", err)
		}
		if err := c.updateProfile(ctx, d, nil); err != nil {
			return err
		}
	case denproto.EventAuthRenewed:
		var r denproto.Renewed
		if json.Unmarshal(e.D, &r) == nil {
			c.mu.Lock()
			if until := time.UnixMilli(r.ExpiresAt); until.After(c.expires) {
				c.expires = until
			}
			c.mu.Unlock()
		}
	}
	if e.Seq > c.seq {
		c.seq = e.Seq
	}
	return nil
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
	c.profile = p
	c.mu.Unlock()
	if !changed {
		return nil
	}
	c.m.notify()
	return updateProfile(ctx, c.m.db, c.m.v, c.j.denID, p)
}

func cleanMember(m denproto.Member) (denproto.Member, bool) {
	username, err := denproto.NormalizeUsername(m.Username)
	if err != nil {
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
		if nonce, err := c.api.challenge(ctx, c.j.denID); err == nil {
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
