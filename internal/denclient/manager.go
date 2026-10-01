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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

// InputError is a problem with what the member entered. Its message is
// written for them and safe to show as is.
type InputError struct{ Err error }

func (e *InputError) Error() string { return e.Err.Error() }
func (e *InputError) Unwrap() error { return e.Err }

func inputError(err error) error { return &InputError{Err: err} }

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
	// Transfer carries files to and from dens, over HTTP's connections
	// but without its time limit, since a large file can take minutes on
	// a home connection. A transfer ends with its request instead.
	Transfer *http.Client
	files    *fileCache

	// Timings from protocol.md; tests shorten them. A token is used only
	// while it has MinTokenLife left, and renewed RenewLead before expiry.
	MinTokenLife time.Duration
	RenewLead    time.Duration
	RenewRetry   time.Duration
	PingInterval time.Duration
	PingTimeout  time.Duration

	joinMu sync.Mutex

	// epoch and changes version what Watch reports: a page keeps the
	// newest statuses it has seen, from the stream or a fetch.
	epoch   string
	changes atomic.Uint64

	mu      sync.Mutex
	ctx     context.Context
	conns   []*conn
	watches map[chan struct{}]struct{}
	streams map[chan PageEvent]struct{}
	wg      sync.WaitGroup

	// focus is the channel each page shows in each den: page, den, channel.
	focusMu sync.Mutex
	focus   map[string]map[string]string

	// signIns are sign-ins on this device waiting for another of the
	// member's to approve them, by an ID of this manager's; guarded by mu.
	signIns   map[string]*pendingSignIn
	signInSeq uint64

	// TempDir holds DM files while they're prepared for sending, sealed
	// with a key that lives only in memory.
	TempDir string
}

// PageEvent carries a den's checked events to the page, or tells it to
// drop what it holds for the den and load it again (Reset). Reads are the
// read states the events changed, so unread marks and mention counts come
// from one place.
type PageEvent struct {
	DenID  string               `json:"den"`
	Reset  bool                 `json:"reset,omitempty"`
	Events []denproto.Event     `json:"events,omitempty"`
	Reads  []denproto.ReadState `json:"reads,omitempty"`
}

// Stream returns a channel of page events, and a function to stop. A
// stream that falls behind is closed; the page then reloads everything.
func (m *Manager) Stream() (<-chan PageEvent, func()) {
	ch := make(chan PageEvent, 256)
	m.mu.Lock()
	m.streams[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, ok := m.streams[ch]; ok {
			delete(m.streams, ch)
			close(ch)
		}
	}
}

func (m *Manager) publish(e PageEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.streams {
		select {
		case ch <- e:
		default:
			delete(m.streams, ch)
			close(ch)
		}
	}
}

// New returns a manager; Run starts it.
func New(db *sql.DB, v *vault.Vault, log *xlog.Logger, userAgent string, own OwnDen) *Manager {
	// A den answers within ResponseHeaderTimeout of hearing a whole
	// request, uploads included; a file's body then takes what it takes.
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: requestTimeout}
	return &Manager{
		db: db, v: v, log: log, own: own, agent: userAgent, label: deviceLabel(), epoch: denproto.Random(8).String(),
		HTTP:         &http.Client{Timeout: requestTimeout, Transport: transport},
		Transfer:     &http.Client{Transport: transport},
		files:        newFileCache(),
		MinTokenLife: time.Minute,
		RenewLead:    10 * time.Minute,
		RenewRetry:   30 * time.Second,
		PingInterval: 20 * time.Second,
		PingTimeout:  10 * time.Second,
		watches:      map[chan struct{}]struct{}{},
		streams:      map[chan PageEvent]struct{}{},
		focus:        map[string]map[string]string{},
		signIns:      map[string]*pendingSignIn{},
		TempDir:      os.TempDir(),
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
		c.j.close()
	}
	m.mu.Unlock()
	m.dropSignIns()
	return nil
}

// startLocked starts a connection. The caller holds m.mu.
func (m *Manager) startLocked(j *joined, token denproto.Bytes, expires time.Time) {
	ctx, stop := context.WithCancel(m.ctx)
	c := &conn{m: m, j: j, profile: j.profile, state: StateConnecting, since: time.Now(), token: token, expires: expires,
		stop: stop, done: make(chan struct{}), wake: make(chan struct{}, 1),
		due: map[string]bool{}, keysDue: make(chan struct{}, 1), dmFiles: map[string]dmFile{},
		uploads: map[string]dmUpload{}, approvals: map[string]*approval{}, approved: map[string]RequestView{}}
	c.api, c.own = m.apiFor(j.denID, j.profile.URL)
	m.conns = append(m.conns, c)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(c.done)
		work := make(chan struct{})
		go func() {
			defer close(work)
			c.keyWork(ctx)
		}()
		c.run(ctx)
		<-work
	}()
}

// forget stops a den's connection and deletes what this install keeps for
// it, its device key and profile.
func (m *Manager) forget(ctx context.Context, c *conn) error {
	m.mu.Lock()
	m.conns = slices.DeleteFunc(m.conns, func(x *conn) bool { return x == c })
	m.mu.Unlock()
	c.stop()
	<-c.done
	c.j.close()
	m.dropDenFocus(c.j.denID.String())
	m.notify()
	if _, err := m.db.ExecContext(ctx, `DELETE FROM joined_dens WHERE den_id = ?`, []byte(c.j.denID)); err != nil {
		return fmt.Errorf("forget den: %w", err)
	}
	m.log.Infof("Forgot a den")
	return nil
}

// apiFor reaches the den this install hosts over loopback, and any other
// den at its address. own reports which it is.
func (m *Manager) apiFor(denID []byte, url string) (a *api, own bool) {
	base := url
	if info, loopback, ok := m.own(); ok && denproto.Equal(info.ID, denID) {
		base, own = loopback, true
	}
	return &api{base: base, client: m.HTTP, agent: m.agent, own: own}, own
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

// Version says how far the dens' statuses have changed: within one epoch,
// a higher count is newer. Read it before the statuses it goes with.
func (m *Manager) Version() (epoch string, count uint64) {
	return m.epoch, m.changes.Load()
}

func (m *Manager) notify() {
	m.changes.Add(1)
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
		return denproto.Den{}, inputError(err)
	}
	a, _ := m.apiFor(inv.DenID, inv.URL)
	a, _, _, err = a.reach(ctx, inv.DenID)
	if err != nil {
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

// Joined is a den this install just joined: its status, and what the
// member keeps safe, shown once: their recovery codes, and their DM seal.
// SealNew says the seal was made for this den, rather than the one this
// install's other dens use.
type Joined struct {
	Status        Status   `json:"den"`
	RecoveryCodes []string `json:"recovery_codes"`
	Seal          string   `json:"seal"`
	SealNew       bool     `json:"seal_new"`
}

// Join redeems an invite string.
func (m *Manager) Join(ctx context.Context, invite string, req JoinRequest) (Joined, error) {
	inv, err := denproto.DecodeInvite(invite)
	if err != nil {
		return Joined{}, inputError(err)
	}
	return m.join(ctx, inv.DenID, inv.Code, inv.URL, req)
}

// JoinOwn redeems the owner invite of the den this install hosts.
func (m *Manager) JoinOwn(ctx context.Context, code []byte, req JoinRequest) (Joined, error) {
	own, _, ok := m.own()
	if !ok {
		return Joined{}, errors.New("this install hosts no den")
	}
	return m.join(ctx, own.ID, code, own.URL, req)
}

func (m *Manager) join(ctx context.Context, denID, code []byte, url string, req JoinRequest) (Joined, error) {
	username, err := denproto.NormalizeUsername(req.Username)
	if err != nil {
		return Joined{}, inputError(err)
	}
	displayName, err := denproto.CleanName(req.DisplayName, denproto.MaxNameRunes)
	if err != nil {
		return Joined{}, inputError(fmt.Errorf("display name: %w", err))
	}
	if err := vault.ValidatePassword(req.Password); err != nil {
		return Joined{}, inputError(err)
	}
	m.mu.Lock()
	started := m.ctx != nil
	m.mu.Unlock()
	if !started {
		return Joined{}, ErrNotStarted
	}
	if c, err := m.find(denproto.Bytes(denID).String()); err == nil && !gone(c) {
		return Joined{}, ErrAlreadyJoined
	}
	// Two joins of one den at once would both pass the check above.
	m.joinMu.Lock()
	defer m.joinMu.Unlock()
	if c, err := m.find(denproto.Bytes(denID).String()); err == nil {
		if !gone(c) {
			return Joined{}, ErrAlreadyJoined
		}
		// This device is out of the den; joining again replaces it.
		if err := m.forget(ctx, c); err != nil {
			return Joined{}, err
		}
	}
	// The member's DM seal here is the one this install's other dens use,
	// so they have one to keep. The first den makes it.
	seal, err := installSeal(ctx, m.db, m.v)
	if err != nil {
		return Joined{}, err
	}
	made := seal == nil
	if made {
		seal = denproto.NewSeal()
	}
	defer clear(seal)
	sealCheck, err := denproto.SealCheck(seal, denID)
	if err != nil {
		return Joined{}, err
	}

	a, _ := m.apiFor(denID, url)
	// An invite made before the den moved leads to where it is now.
	a, nonce, _, err := a.reach(ctx, denID)
	if err != nil {
		return Joined{}, err
	}
	if !a.own {
		url = a.base
	}
	info, err := preview(ctx, a, denID, code)
	if err != nil {
		return Joined{}, err
	}
	key, err := vault.GenerateSigningKey()
	if err != nil {
		return Joined{}, err
	}
	keyID := denproto.ID(key.Public())
	var resp denproto.JoinResponse
	err = a.call(ctx, http.MethodPost, "/api/join", nil, denproto.JoinRequest{
		Invite: code, Username: username, DisplayName: displayName,
		Verifier:  denproto.Verifier(req.Password, denID, username),
		PublicKey: denproto.Bytes(key.Public()), DeviceLabel: m.label,
		Nonce: nonce, Proof: key.Sign(denproto.ProofMessage(denID, keyID, nonce)), SealCheck: sealCheck,
	}, &resp)
	if err != nil {
		key.Close()
		return Joined{}, err
	}
	member, ok := cleanMember(resp.Member)
	if !ok || denproto.Size("token", resp.Token, denproto.TokenSize) != nil || len(resp.RecoveryCodes) != denproto.RecoveryCodes {
		key.Close()
		return Joined{}, errors.New("the den's answer to joining is malformed")
	}
	secret, err := vault.NewSecret(seal)
	if err != nil {
		key.Close()
		return Joined{}, err
	}
	j := &joined{denID: denID, profile: Profile{URL: url, Name: info.Name, Member: member}, key: key, joinedAt: time.Now(), seal: secret}
	if err := insertJoined(ctx, m.db, m.v, j); err != nil {
		j.close()
		return Joined{}, err
	}
	if made {
		if err := setInstallSeal(ctx, m.db, m.v, seal); err != nil {
			m.log.Errorf("keep the DM seal for new dens: %v", err)
		}
	}
	m.mu.Lock()
	m.startLocked(j, resp.Token, time.UnixMilli(resp.ExpiresAt))
	c := m.conns[len(m.conns)-1]
	m.mu.Unlock()
	m.notify()
	m.log.Infof("Joined a den")
	return Joined{Status: c.status(), RecoveryCodes: resp.RecoveryCodes, Seal: denproto.FormatSeal(seal), SealNew: made}, nil
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

// CheckAddress reaches a den at its public address, even when this install
// hosts it and normally uses loopback, and checks it proves its identity
// there. It is how an owner finds out whether members can reach the den.
func (m *Manager) CheckAddress(ctx context.Context, denID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	a := &api{base: c.status().URL, client: m.HTTP, agent: m.agent}
	_, _, err = a.challenge(ctx, c.j.denID)
	return err
}

// Relocate gives a den a new address, for one that moved while this
// install was away and whose old address no longer answers. The den must
// prove its pinned identity there, and sign that address, before this
// install uses it.
func (m *Manager) Relocate(ctx context.Context, denID, address string) (Status, error) {
	c, err := m.find(denID)
	if err != nil {
		return Status{}, err
	}
	if c.own {
		return Status{}, inputError(errors.New("this computer hosts that den, and reaches it without an address"))
	}
	url, err := denproto.NormalizeDenURL(strings.TrimSpace(address))
	if err != nil {
		return Status{}, inputError(errors.New("enter the den's address, such as https://den.example.com"))
	}
	a := &api{base: url, client: m.HTTP, agent: m.agent}
	if a, _, _, err = a.reach(ctx, c.j.denID); err != nil {
		return Status{}, err
	}
	if err := c.moveTo(ctx, a); err != nil {
		return Status{}, err
	}
	c.poke()
	return c.status(), nil
}

// UpdateDen changes the name, address or upload limits of a den this
// member owns.
func (m *Manager) UpdateDen(ctx context.Context, denID string, req denproto.DenUpdateRequest) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if req.Limits != nil {
		if err := denproto.CheckLimits(*req.Limits); err != nil {
			return inputError(err)
		}
	}
	var info denproto.Den
	return c.call(ctx, http.MethodPatch, "/api/den", req, &info)
}
