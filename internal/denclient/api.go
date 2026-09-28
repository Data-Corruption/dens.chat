package denclient

import (
	"bytes"
	"context"
	"encoding/json"
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

// challenge checks the den holds the identity key pinned as denID, and
// returns a fresh nonce to prove a device key with.
func (a *api) challenge(ctx context.Context, denID []byte) (denproto.Bytes, error) {
	clientNonce := denproto.Random(denproto.NonceSize)
	var resp denproto.ChallengeResponse
	if err := a.call(ctx, http.MethodPost, "/api/auth/challenge", nil, denproto.ChallengeRequest{ClientNonce: clientNonce}, &resp); err != nil {
		return nil, err
	}
	if err := denproto.VerifyDen(denID, clientNonce, resp); err != nil {
		return nil, err
	}
	return resp.Nonce, nil
}
