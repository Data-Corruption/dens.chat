package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

func (rt *router) mountDens(r chi.Router) {
	r.Get("/api/dens", rt.handleDens)
	r.Get("/api/events", rt.handleEvents)
	r.Post("/api/den", rt.handleCreateDen)
	r.Post("/api/dens/preview", rt.handlePreview)
	r.Post("/api/dens/join", rt.handleJoin)
	r.Post("/api/dens/{den}/invites", rt.handleCreateInvite)
	r.Get("/api/dens/{den}/invites", rt.handleInvites)
	r.Delete("/api/dens/{den}/invites/{invite}", rt.handleRevokeInvite)
	r.Post("/api/dens/{den}/settings", rt.handleDenSettings)
	r.Post("/api/dens/{den}/check", rt.handleCheckAddress)
}

// hosting describes the den this install hosts, if the den role is on.
type hosting struct {
	Enabled bool   `json:"enabled"`
	Created bool   `json:"created"`
	Joined  bool   `json:"joined"` // the owner's account exists
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
}

type densView struct {
	Dens    []denclient.Status `json:"dens"`
	Hosting hosting            `json:"hosting"`
}

func (rt *router) densView() densView {
	v := densView{Dens: rt.a.Dens.Statuses(), Hosting: hosting{Enabled: rt.a.Den != nil}}
	if info, _, ok := rt.a.OwnDen(); ok {
		v.Hosting.Created, v.Hosting.Name, v.Hosting.URL = true, info.Name, info.URL
		for _, d := range v.Dens {
			if d.Own {
				v.Hosting.Joined = true
			}
		}
	}
	return v
}

func (rt *router) handleDens(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, rt.densView())
}

// handleEvents streams the dens view to the page whenever it changes.
func (rt *router) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Accept checks the Origin against the Host, which the Host guard has
	// already pinned to this listener, so other sites can't open it.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	changes, stop := rt.a.Dens.Watch()
	defer stop()
	send := func() bool {
		data, err := json.Marshal(map[string]any{"t": "dens", "d": rt.densView()})
		if err != nil {
			return false
		}
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return c.Write(wctx, websocket.MessageText, data) == nil
	}
	if !send() {
		return
	}
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			if !send() {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

type joinBody struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

func (b joinBody) request() denclient.JoinRequest {
	return denclient.JoinRequest{Username: b.Username, DisplayName: b.DisplayName, Password: b.Password}
}

type joinResult struct {
	Den           denclient.Status `json:"den"`
	RecoveryCodes []string         `json:"recovery_codes"`
}

// handleCreateDen creates the den this install hosts and joins it as the
// owner, over loopback. If the den exists but the owner never joined (the
// first attempt failed), it issues a fresh owner invite and joins with it.
func (rt *router) handleCreateDen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		joinBody
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if rt.a.Den == nil {
		jsonError(w, http.StatusConflict, "This install doesn't host a den. Run the installer again with --den to host one.")
		return
	}
	// Check everything the member typed before creating anything.
	if _, err := denproto.NormalizeUsername(body.Username); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := denproto.CleanName(body.DisplayName, denproto.MaxNameRunes); err != nil {
		jsonError(w, http.StatusBadRequest, "Display name: "+err.Error())
		return
	}
	if err := vault.ValidatePassword(body.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var code []byte
	var err error
	if _, created := rt.a.Den.Info(); created {
		code, err = rt.a.Den.OwnerInvite(r.Context())
	} else {
		if _, err := denproto.CleanName(body.Name, denproto.MaxNameRunes); err != nil {
			jsonError(w, http.StatusBadRequest, "Den name: "+err.Error())
			return
		}
		if _, err := denproto.NormalizeDenURL(body.URL); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		code, err = rt.a.Den.Create(r.Context(), body.Name, body.URL)
	}
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	status, codes, err := rt.a.Dens.JoinOwn(r.Context(), code, body.request())
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, joinResult{Den: status, RecoveryCodes: codes})
}

func (rt *router) handlePreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Invite string `json:"invite"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	info, err := rt.a.Dens.Preview(r.Context(), body.Invite)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"den": map[string]string{"name": info.Name, "url": info.URL}})
}

func (rt *router) handleJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		joinBody
		Invite string `json:"invite"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	status, codes, err := rt.a.Dens.Join(r.Context(), body.Invite, body.request())
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, joinResult{Den: status, RecoveryCodes: codes})
}

func (rt *router) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body denproto.InviteCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	invite, inv, err := rt.a.Dens.CreateInvite(r.Context(), chi.URLParam(r, "den"), body)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	inv.Code = nil
	writeJSON(w, map[string]any{"invite": invite, "details": inv})
}

func (rt *router) handleInvites(w http.ResponseWriter, r *http.Request) {
	list, err := rt.a.Dens.Invites(r.Context(), chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"invites": list})
}

func (rt *router) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.RevokeInvite(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "invite")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDenSettings changes the name or public address of a den the member
// owns. Invites made afterwards carry the new address.
func (rt *router) handleDenSettings(w http.ResponseWriter, r *http.Request) {
	var body denproto.DenUpdateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name != nil {
		if _, err := denproto.CleanName(*body.Name, denproto.MaxNameRunes); err != nil {
			jsonError(w, http.StatusBadRequest, "Den name: "+err.Error())
			return
		}
	}
	if body.URL != nil {
		if _, err := denproto.NormalizeDenURL(*body.URL); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := rt.a.Dens.UpdateDen(r.Context(), chi.URLParam(r, "den"), body); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleCheckAddress reaches a den at its public address from here.
func (rt *router) handleCheckAddress(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.CheckAddress(r.Context(), chi.URLParam(r, "den")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// denError explains a failed den operation to the member. A den's own
// messages are never shown verbatim: dens are other people's servers.
func (rt *router) denError(w http.ResponseWriter, r *http.Request, err error) {
	var input *denclient.InputError
	var perr *denproto.Error
	var netErr net.Error
	var urlErr *url.Error
	switch {
	case errors.As(err, &input):
		jsonError(w, http.StatusBadRequest, input.Error())
	case errors.Is(err, denclient.ErrAlreadyJoined):
		jsonError(w, http.StatusConflict, "You've already joined this den.")
	case errors.Is(err, denclient.ErrUnknownDen):
		jsonError(w, http.StatusNotFound, "You haven't joined that den.")
	case errors.Is(err, denclient.ErrNotStarted):
		jsonError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, denproto.ErrWrongIdentity):
		rt.a.Log.Warnf("den identity check failed: %v", err)
		jsonError(w, http.StatusBadGateway, "This server can't prove it is the den the invite was made for. "+
			"Don't join it; ask whoever sent the invite.")
	case errors.As(err, &perr):
		jsonError(w, http.StatusBadGateway, denMessage(perr))
	case errors.As(err, &netErr), errors.As(err, &urlErr):
		jsonError(w, http.StatusBadGateway, denclient.Explain(err))
	default:
		xhttp.Error(r.Context(), w, err)
	}
}

func denMessage(e *denproto.Error) string {
	switch e.Code {
	case denproto.CodeInviteInvalid:
		return "This invite has expired, was revoked, or was already used. Ask for a new one."
	case denproto.CodeUsernameTaken:
		return "That username is taken on this den."
	case denproto.CodeRateLimited:
		return "The den asked us to slow down. Try again in a minute."
	case denproto.CodeProtocolUnsupported:
		return "This den runs a version of Dens that can't talk to this one."
	case denproto.CodeDenNotCreated:
		return "That den isn't set up yet."
	case denproto.CodeForbidden:
		return "Only the den's owner can do that."
	case denproto.CodeInvalidField:
		return "The den rejected one of the details you entered."
	}
	return "The den refused the request (" + e.Code + ")."
}
