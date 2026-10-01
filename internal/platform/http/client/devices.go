package client

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (rt *router) mountDevices(r chi.Router) {
	r.Post("/api/dens/signin", rt.handleSignIn)
	r.Post("/api/dens/signin/{id}/check", rt.handleCheckSignIn)
	r.Delete("/api/dens/signin/{id}", rt.handleDismissSignIn)
	r.Post("/api/dens/{den}/password", rt.handleDenPassword)
	r.Post("/api/dens/{den}/recovery-codes", rt.handleRecoveryCodes)
	r.Get("/api/dens/{den}/devices", rt.handleDevices)
	r.Delete("/api/dens/{den}/devices/{key}", rt.handleRevokeDevice)
	r.Post("/api/dens/{den}/requests/{id}/answer", rt.handleAnswerSignIn)
	r.Post("/api/dens/{den}/requests/{id}/approve", rt.handleApproveSignIn)
	r.Post("/api/dens/{den}/requests/{id}/refuse", rt.handleRefuseSignIn)
	r.Delete("/api/dens/{den}/requests/{id}", rt.handleDismissApproved)
	r.Post("/api/dens/{den}/seal", rt.handleTypeSeal)
	r.Post("/api/dens/{den}/seal/start-over", rt.handleStartOver)
	r.Post("/api/dens/{den}/seal/show", rt.handleShowSeal)
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

// handleCheckSignIn takes the digits the approving device shows, typed
// into this one.
func (rt *router) handleCheckSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Digits string `json:"digits"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := rt.a.Dens.CheckSignIn(r.Context(), chi.URLParam(r, "id"), body.Digits); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (rt *router) handleDismissSignIn(w http.ResponseWriter, r *http.Request) {
	rt.a.Dens.DismissSignIn(chi.URLParam(r, "id"))
	writeJSON(w, map[string]bool{"ok": true})
}

// handleAnswerSignIn starts approving a sign-in on a new device from this
// one.
func (rt *router) handleAnswerSignIn(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.AnswerSignIn(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "id")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleApproveSignIn takes the digits the new device shows, and if they
// match, lets it in with the member's DM seal.
func (rt *router) handleApproveSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Digits string `json:"digits"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := rt.a.Dens.ApproveSignIn(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "id"), body.Digits); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDismissApproved stops showing a sign-in this device approved.
func (rt *router) handleDismissApproved(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DismissApproved(chi.URLParam(r, "den"), chi.URLParam(r, "id")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (rt *router) handleRefuseSignIn(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.RefuseSignIn(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "id")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleTypeSeal gives this device the member's DM seal, as they saved it.
func (rt *router) handleTypeSeal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Seal string `json:"seal"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := rt.a.Dens.TypeSeal(r.Context(), chi.URLParam(r, "den"), body.Seal); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleStartOver gives the member a new DM seal at a den, with their den
// password, and shows it once.
func (rt *router) handleStartOver(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	seal, err := rt.a.Dens.StartOver(r.Context(), chi.URLParam(r, "den"), body.Password)
	if denproto.IsCode(err, denproto.CodeWrongPassword) {
		jsonError(w, http.StatusForbidden, "That isn't your den password.")
		return
	}
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]string{"seal": seal})
}

// handleShowSeal shows the member's DM seal at a den, after the local
// password, like any other key this install holds.
func (rt *router) handleShowSeal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := rt.a.VerifyPassword(r.Context(), body.Password); err != nil {
		passwordError(w, r, err)
		return
	}
	seal, err := rt.a.Dens.ShowSeal(chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]string{"seal": seal})
}
