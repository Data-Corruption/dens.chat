package denclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
}

// call sends req (unless nil) as JSON and decodes the response into resp
// (unless nil). token, when set, is the bearer token.
func (a *api) call(ctx context.Context, method, path string, token denproto.Bytes, req, resp any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var body io.Reader
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return err
		}
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
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return denproto.ReadError(res)
	}
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
	h.Set(denproto.HeaderVersion, strconv.Itoa(denproto.Version))
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
// address is followed there, once: it must prove the same key at the new
// address, and answer there as itself, before anything more is sent. It
// returns where the den answered, a nonce and the den's ID.
func (a *api) reach(ctx context.Context, denID []byte) (*api, denproto.Bytes, denproto.Bytes, error) {
	nonce, id, err := a.challenge(ctx, denID)
	var moved *denproto.MovedError
	if !errors.As(err, &moved) {
		return a, nonce, id, err
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
