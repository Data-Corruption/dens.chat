package denclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

const requestTimeout = 30 * time.Second

// api makes protocol requests to one den.
type api struct {
	base   string // scheme://host[:port], no trailing slash
	client *http.Client
	agent  string
	// own is the den this install hosts, reached on loopback: it signs its
	// public address, not the one it's reached at here, and nothing can
	// sit between the two.
	own bool
	// version is the protocol's version this den and this build share,
	// the highest both speak, as the den's range says; 0 until it says.
	version atomic.Int32
}

// speaks is the protocol's version requests to the den go at: the highest
// it and this build share, once the den said, and this build's own until
// then.
func (a *api) speaks() int {
	if v := a.version.Load(); v != 0 {
		return int(v)
	}
	return denproto.Version
}

// heard takes the den's range from a response, and goes at the highest
// version it shares with this build, as the den speaks newer ones once it
// updates. A range that shares none leaves it, for the den to refuse.
func (a *api) heard(h http.Header) bool {
	lo, hi, err := denproto.ParseRange(h.Get(denproto.HeaderRange))
	if err != nil {
		return false
	}
	v := min(hi, denproto.Version)
	if v < max(lo, denproto.MinVersion) {
		return false
	}
	return a.version.Swap(int32(v)) != int32(v)
}

// call sends req (unless nil) as JSON and decodes the response into resp
// (unless nil). token, when set, is the bearer token. A den that speaks
// only older versions of the protocol than this build's is asked again,
// once, at the newest it speaks.
func (a *api) call(ctx context.Context, method, path string, token denproto.Bytes, req, resp any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var data []byte
	if req != nil {
		var err error
		if data, err = json.Marshal(req); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		var body io.Reader
		if req != nil {
			body = bytes.NewReader(data)
		}
		r, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
		if err != nil {
			return err
		}
		a.headers(r.Header, token)
		if req != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		res, err := a.client.Do(r)
		if err != nil {
			return fmt.Errorf("reach the den: %w", err)
		}
		changed := a.heard(res.Header)
		if res.StatusCode >= 300 {
			err := denproto.ReadError(res)
			res.Body.Close()
			if attempt == 0 && changed && denproto.IsCode(err, denproto.CodeProtocolUnsupported) {
				continue
			}
			return err
		}
		err = a.read(res, resp)
		res.Body.Close()
		return err
	}
}

// read decodes a response into resp, unless nil.
func (a *api) read(res *http.Response, resp any) error {
	if resp == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, denproto.MaxBody+1))
	if err != nil {
		return fmt.Errorf("read the den's response: %w", err)
	}
	if len(data) > denproto.MaxBody {
		return fmt.Errorf("the den's response is larger than %d bytes", denproto.MaxBody)
	}
	if err := json.Unmarshal(data, resp); err != nil {
		return fmt.Errorf("the den's response is malformed: %w", err)
	}
	return nil
}

func (a *api) headers(h http.Header, token denproto.Bytes) {
	h.Set(denproto.HeaderVersion, strconv.Itoa(a.speaks()))
	h.Set("User-Agent", a.agent)
	if token != nil {
		h.Set("Authorization", "Bearer "+token.String())
	}
}

// challenge checks that the den holds the identity key pinned as denID,
// or any key when denID is nil, and that it answers at this address. It
// returns a fresh nonce to prove a device key with, and the den's ID. A den
// that signs another address returns its ID with a *denproto.MovedError,
// and nothing more is sent here.
func (a *api) challenge(ctx context.Context, denID []byte) (nonce, id denproto.Bytes, err error) {
	clientNonce := denproto.Random(denproto.NonceSize)
	var resp denproto.ChallengeResponse
	if err := a.call(ctx, http.MethodPost, "/api/auth/challenge", nil, denproto.ChallengeRequest{ClientNonce: clientNonce}, &resp); err != nil {
		return nil, nil, err
	}
	if id, err = denproto.VerifyDen(denID, clientNonce, resp); err != nil {
		return nil, nil, err
	}
	if !a.own {
		if err := denproto.CheckDenURL(resp.URL, a.base); err != nil {
			return nil, id, err
		}
	}
	return resp.Nonce, id, nil
}

// reach runs a challenge at a den's address. A den that signs another
// address is followed there, once, if checkMove lets it go there and gate
// lets a check go now: it must prove the same key at the new address, and
// answer there as itself, before anything more is sent. It returns where
// the den answered, a nonce and the den's ID. gate is nil when the member
// asked for this, as by joining.
func (a *api) reach(ctx context.Context, denID []byte, gate *followGate) (*api, denproto.Bytes, denproto.Bytes, error) {
	nonce, id, err := a.challenge(ctx, denID)
	var moved *denproto.MovedError
	if !errors.As(err, &moved) {
		return a, nonce, id, err
	}
	if err := checkMove(a.base, moved.URL); err != nil {
		return nil, nil, nil, &MovedError{URL: moved.URL, Err: err}
	}
	if !gate.take() {
		return nil, nil, nil, &MovedError{URL: moved.URL, Err: errFollowLater}
	}
	next := &api{base: moved.URL, client: a.client, agent: a.agent}
	nonce, _, err = next.challenge(ctx, id)
	if errors.As(err, &moved) {
		return nil, nil, nil, errors.New("the den gives one address at another, and then another")
	}
	if err != nil {
		return nil, nil, nil, &MovedError{URL: next.base, Err: err}
	}
	return next, nonce, id, nil
}

// ErrMoveRefused is a den's move to an address checkMove refuses.
var ErrMoveRefused = errors.New("the den gives a new address on this computer, or a plain http one, which Dens doesn't follow")

// errFollowLater is a den's move its members check later: they checked
// another new address for it within the minute.
var errFollowLater = errors.New("a new address for the den was checked within the minute")

// checkMove refuses a den's move from where its member reached it to an
// address they shouldn't be sent to (M8): from https to plain http, or from
// elsewhere to this computer, where a hostile den could have its members'
// clients ask their own services. A den on this computer, as in
// development, may move anywhere but from https to http.
func checkMove(from, to string) error {
	if u, err := denproto.NormalizeDenURL(to); err != nil || u != to {
		return ErrMoveRefused
	}
	if strings.HasPrefix(from, "https://") && strings.HasPrefix(to, "http://") || !onThisComputer(from) && onThisComputer(to) {
		return ErrMoveRefused
	}
	return nil
}

// onThisComputer reports whether a den's address names this computer.
func onThisComputer(base string) bool {
	u, err := url.Parse(base)
	return err == nil && denproto.IsLoopbackHost(u.Hostname())
}

// followGate lets a den's member check a new address for it once a minute
// (M8). A den chooses the addresses its members' clients ask, so it could
// otherwise turn them into a crowd asking someone else's server. A check
// ends within requestTimeout, so they also go one at a time.
type followGate struct {
	mu   sync.Mutex
	last time.Time
}

const followEvery = time.Minute

// take reports whether a check of a new address may start now. A nil gate
// lets every check go.
func (g *followGate) take() bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.last.IsZero() && time.Since(g.last) < followEvery {
		return false
	}
	g.last = time.Now()
	return true
}
