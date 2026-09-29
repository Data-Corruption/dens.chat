package client

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (rt *router) mountDevices(r chi.Router) {
	r.Post("/api/dens/signin", rt.handleSignIn)
	r.Post("/api/dens/{den}/password", rt.handleDenPassword)
	r.Post("/api/dens/{den}/recovery-codes", rt.handleRecoveryCodes)
	r.Get("/api/dens/{den}/devices", rt.handleDevices)
	r.Delete("/api/dens/{den}/devices/{key}", rt.handleRevokeDevice)
}

// handleSignIn signs this machine in to a den the member is in already,
// with the den password or a recovery code.
func (rt *router) handleSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Den          string `json:"den"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		RecoveryCode string `json:"recovery_code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	signedIn, err := rt.a.Dens.SignIn(r.Context(), denclient.SignInRequest{
		Den: body.Den, Username: body.Username, Password: body.Password, RecoveryCode: body.RecoveryCode,
	})
	if denproto.IsCode(err, denproto.CodeUnauthorized) {
		msg := "That username and password don't match an account on this den."
		if body.RecoveryCode != "" {
			msg = "That username and recovery code don't match an account on this den. Each code works once."
		}
		jsonError(w, http.StatusUnauthorized, msg)
		return
	}
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, signedIn)
}

func (rt *router) handleDenPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current      string `json:"current"`
		RecoveryCode string `json:"recovery_code"`
		Password     string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	changed, err := rt.a.Dens.ChangePassword(r.Context(), chi.URLParam(r, "den"), body.Current, body.RecoveryCode, body.Password)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, changed)
}

func (rt *router) handleRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	codes, err := rt.a.Dens.NewRecoveryCodes(r.Context(), chi.URLParam(r, "den"), body.Password)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"recovery_codes": codes})
}

func (rt *router) handleDevices(w http.ResponseWriter, r *http.Request) {
	list, err := rt.a.Dens.Devices(r.Context(), chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, list)
}

func (rt *router) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.RevokeDevice(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "key")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
