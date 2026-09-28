// Package denclient is the client side of the den protocol: the dens this
// install has joined, joining new ones, and keeping each one connected
// with sign-in, session renewal, resume and backoff. The page reaches it
// through the client listener; browsers never talk to dens.
package denclient

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// OwnDen describes the den this install hosts, and the loopback address
// its own client reaches it at. ok is false when there is none yet.
type OwnDen func() (den denproto.Den, loopback string, ok bool)

// ErrNotStarted reports a request before the manager is running.
var ErrNotStarted = errors.New("den connections are still starting; try again in a moment")

// ErrAlreadyJoined reports joining a den twice.
var ErrAlreadyJoined = errors.New("you've already joined this den")

// ErrUnknownDen reports a den this install hasn't joined.
var ErrUnknownDen = errors.New("you haven't joined that den")

// Manager holds a connection to every joined den.
type Manager struct {
	db    *sql.DB
	v     *vault.Vault
	log   *xlog.Logger
	own   OwnDen
	agent string
	label string

	// HTTP reaches dens; it trusts the system's certificate authorities.
	HTTP *http.Client

	// Timings from protocol.md; tests shorten them. A token is used only
	// while it has MinTokenLife left, and renewed RenewLead before expiry.
	MinTokenLife time.Duration
	RenewLead    time.Duration
	RenewRetry   time.Duration
	PingInterval time.Duration
	PingTimeout  time.Duration

	mu      sync.Mutex
	ctx     context.Context
	conns   []*conn
	watches map[chan struct{}]struct{}
	wg      sync.WaitGroup
}

// New returns a manager; Run starts it.
func New(db *sql.DB, v *vault.Vault, log *xlog.Logger, userAgent string, own OwnDen) *Manager {
	return &Manager{
		db: db, v: v, log: log, own: own, agent: userAgent, label: deviceLabel(),
		HTTP: &http.Client{
			Timeout:   requestTimeout,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
		},
		MinTokenLife: time.Minute,
		RenewLead:    10 * time.Minute,
		RenewRetry:   30 * time.Second,
		PingInterval: 20 * time.Second,
		PingTimeout:  10 * time.Second,
		watches:      map[chan struct{}]struct{}{},
	}
}

// deviceLabel names this device on dens, such as "Pearl (Windows)".
func deviceLabel() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "Dens"
	}
	system := map[string]string{"linux": "Linux", "windows": "Windows"}[runtime.GOOS]
	if system == "" {
		system = runtime.GOOS
	}
	label, err := denproto.CleanName(host+" ("+system+")", denproto.MaxLabelRunes)
	if err != nil {
		return system
	}
	return label
}

// Run connects every joined den and keeps them connected until ctx ends.
func (m *Manager) Run(ctx context.Context) error {
	joined, err := loadJoined(ctx, m.db, m.v)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.ctx = ctx
	for _, j := range joined {
		m.startLocked(j, nil, time.Time{})
	}
	m.mu.Unlock()
	m.notify()
	<-ctx.Done()
	m.wg.Wait()
	m.mu.Lock()
	for _, c := range m.conns {
		c.j.key.Close()
	}
	m.mu.Unlock()
	return nil
}

// startLocked starts a connection. The caller holds m.mu.
func (m *Manager) startLocked(j *joined, token denproto.Bytes, expires time.Time) {
	c := &conn{m: m, j: j, profile: j.profile, state: StateConnecting, since: time.Now(), token: token, expires: expires}
	c.api = m.apiFor(j.denID, j.profile.URL)
	c.own = c.api.base != j.profile.URL
	m.conns = append(m.conns, c)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		c.run(m.ctx)
	}()
}

// apiFor reaches the den this install hosts over loopback, and any other
// den at its address.
func (m *Manager) apiFor(denID []byte, url string) *api {
	base := url
	if own, loopback, ok := m.own(); ok && denproto.Equal(own.ID, denID) {
		base = loopback
	}
	return &api{base: base, client: m.HTTP, agent: m.agent}
}

// Watch returns a channel that receives after any den's status changes,
// and a function to stop watching.
func (m *Manager) Watch() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.watches[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.watches, ch)
		m.mu.Unlock()
	}
}

func (m *Manager) notify() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.watches {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Statuses lists the joined dens in the order they were joined.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	conns := append([]*conn(nil), m.conns...)
	m.mu.Unlock()
	out := make([]Status, 0, len(conns))
	for _, c := range conns {
		out = append(out, c.status())
	}
	return out
}

func (m *Manager) find(denID string) (*conn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		if c.j.denID.String() == denID {
			return c, nil
		}
	}
	return nil, ErrUnknownDen
}

// Preview checks an invite string's den and describes it.
func (m *Manager) Preview(ctx context.Context, invite string) (denproto.Den, error) {
	inv, err := denproto.DecodeInvite(invite)
	if err != nil {
		return denproto.Den{}, err
	}
	a := m.apiFor(inv.DenID, inv.URL)
	if _, err := a.challenge(ctx, inv.DenID); err != nil {
		return denproto.Den{}, err
	}
	return preview(ctx, a, inv.DenID, inv.Code)
}

func preview(ctx context.Context, a *api, denID, code []byte) (denproto.Den, error) {
	var resp denproto.JoinPreviewResponse
	if err := a.call(ctx, http.MethodPost, "/api/join/preview", nil, denproto.JoinPreviewRequest{Invite: code}, &resp); err != nil {
		return denproto.Den{}, err
	}
	if !denproto.Equal(resp.Den.ID, denID) {
		return denproto.Den{}, errors.New("the den described itself with another identity")
	}
	name, err := denproto.CleanName(resp.Den.Name, denproto.MaxNameRunes)
	if err != nil {
		return denproto.Den{}, fmt.Errorf("the den's name is invalid: %w", err)
	}
	resp.Den.Name = name
	return resp.Den, nil
}

// JoinRequest is what a member enters to join a den.
type JoinRequest struct {
	Username    string
	DisplayName string
	Password    string
}

// Join redeems an invite string. It returns the new den's status and the
// member's recovery codes, which are shown once.
func (m *Manager) Join(ctx context.Context, invite string, req JoinRequest) (Status, []string, error) {
	inv, err := denproto.DecodeInvite(invite)
	if err != nil {
		return Status{}, nil, err
	}
	return m.join(ctx, inv.DenID, inv.Code, inv.URL, req)
}

// JoinOwn redeems the owner invite of the den this install hosts.
func (m *Manager) JoinOwn(ctx context.Context, code []byte, req JoinRequest) (Status, []string, error) {
	own, _, ok := m.own()
	if !ok {
		return Status{}, nil, errors.New("this install hosts no den")
	}
	return m.join(ctx, own.ID, code, own.URL, req)
}

func (m *Manager) join(ctx context.Context, denID, code []byte, url string, req JoinRequest) (Status, []string, error) {
	username, err := denproto.NormalizeUsername(req.Username)
	if err != nil {
		return Status{}, nil, err
	}
	displayName, err := denproto.CleanName(req.DisplayName, denproto.MaxNameRunes)
	if err != nil {
		return Status{}, nil, fmt.Errorf("display name: %w", err)
	}
	if err := vault.ValidatePassword(req.Password); err != nil {
		return Status{}, nil, err
	}
	m.mu.Lock()
	started := m.ctx != nil
	m.mu.Unlock()
	if !started {
		return Status{}, nil, ErrNotStarted
	}
	if _, err := m.find(denproto.Bytes(denID).String()); err == nil {
		return Status{}, nil, ErrAlreadyJoined
	}

	a := m.apiFor(denID, url)
	nonce, err := a.challenge(ctx, denID)
	if err != nil {
		return Status{}, nil, err
	}
	info, err := preview(ctx, a, denID, code)
	if err != nil {
		return Status{}, nil, err
	}
	key, err := vault.GenerateSigningKey()
	if err != nil {
		return Status{}, nil, err
	}
	keyID := denproto.ID(key.Public())
	var resp denproto.JoinResponse
	err = a.call(ctx, http.MethodPost, "/api/join", nil, denproto.JoinRequest{
		Invite: code, Username: username, DisplayName: displayName,
		Verifier:  denproto.Verifier(req.Password, denID, username),
		PublicKey: denproto.Bytes(key.Public()), DeviceLabel: m.label,
		Nonce: nonce, Proof: key.Sign(denproto.ProofMessage(denID, keyID, nonce)),
	}, &resp)
	if err != nil {
		key.Close()
		return Status{}, nil, err
	}
	member, ok := cleanMember(resp.Member)
	if !ok || denproto.Size("token", resp.Token, denproto.TokenSize) != nil || len(resp.RecoveryCodes) != denproto.RecoveryCodes {
		key.Close()
		return Status{}, nil, errors.New("the den's answer to joining is malformed")
	}
	j := &joined{denID: denID, profile: Profile{URL: url, Name: info.Name, Member: member}, key: key, joinedAt: time.Now()}
	if err := insertJoined(ctx, m.db, m.v, j); err != nil {
		key.Close()
		return Status{}, nil, err
	}
	m.mu.Lock()
	m.startLocked(j, resp.Token, time.UnixMilli(resp.ExpiresAt))
	c := m.conns[len(m.conns)-1]
	m.mu.Unlock()
	m.notify()
	m.log.Infof("Joined a den")
	return c.status(), resp.RecoveryCodes, nil
}

// CreateInvite makes an invite on a joined den and returns its invite
// string, which can't be shown again.
func (m *Manager) CreateInvite(ctx context.Context, denID string, req denproto.InviteCreateRequest) (string, denproto.Invite, error) {
	c, err := m.find(denID)
	if err != nil {
		return "", denproto.Invite{}, err
	}
	var inv denproto.Invite
	if err := c.call(ctx, http.MethodPost, "/api/invites", req, &inv); err != nil {
		return "", denproto.Invite{}, err
	}
	if err := denproto.Size("code", inv.Code, denproto.InviteCodeSize); err != nil {
		return "", denproto.Invite{}, err
	}
	url := c.status().URL
	return denproto.EncodeInvite(c.j.denID, inv.Code, url), inv, nil
}

// Invites lists a den's usable invites.
func (m *Manager) Invites(ctx context.Context, denID string) ([]denproto.Invite, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	var list denproto.InviteList
	if err := c.call(ctx, http.MethodGet, "/api/invites", nil, &list); err != nil {
		return nil, err
	}
	for i := range list.Invites {
		list.Invites[i].Code = nil
	}
	return list.Invites, nil
}

// RevokeInvite deletes an invite on a den.
func (m *Manager) RevokeInvite(ctx context.Context, denID, inviteID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/invites/"+inviteID, nil, nil)
}

// UpdateDen changes the name or address of a den this member owns.
func (m *Manager) UpdateDen(ctx context.Context, denID string, req denproto.DenUpdateRequest) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	var info denproto.Den
	return c.call(ctx, http.MethodPatch, "/api/den", req, &info)
}
