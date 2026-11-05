package client

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (rt *router) mountMembers(r chi.Router) {
	r.Get("/api/dens/{den}/members/{member}", rt.handleProfile)
	r.Patch("/api/dens/{den}/me", rt.handleUpdateProfile)
	r.Put("/api/dens/{den}/members/{member}/role", rt.handleSetRole)
	r.Post("/api/dens/{den}/members/{member}/remove", rt.handleRemoveMember)
	r.Post("/api/dens/{den}/members/{member}/disconnect", rt.handleDisconnectFromCall)
	r.Post("/api/dens/{den}/members/{member}/voice-mute", rt.handleStaffMute)
	r.Get("/api/dens/{den}/members/{member}/code", rt.handleCode)
	r.Post("/api/dens/{den}/members/{member}/compare", rt.handleCompare)
	r.Post("/api/dens/{den}/members/{member}/send-anyway", rt.handleSendAnyway)
	r.Get("/api/dens/{den}/bans", rt.handleBans)
	r.Delete("/api/dens/{den}/bans/{member}", rt.handleUnban)
	r.Post("/api/dens/{den}/dms", rt.handleOpenDM)
	r.Post("/api/dens/{den}/dms/{channel}/close", rt.handleCloseDM)
	r.Post("/api/dens/{den}/leave", rt.handleLeave)
	r.Delete("/api/dens/{den}", rt.handleForget)
}

func (rt *router) handleProfile(w http.ResponseWriter, r *http.Request) {
	m, err := rt.a.Dens.Profile(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, m)
}

func (rt *router) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req denproto.ProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := rt.a.Dens.UpdateProfile(r.Context(), chi.URLParam(r, "den"), req)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, m)
}

func (rt *router) handleSetRole(w http.ResponseWriter, r *http.Request) {
	var req denproto.RoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.SetRole(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member"), req.Role); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	var req denproto.RemoveRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.RemoveMember(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member"), req); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleDisconnectFromCall(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DisconnectFromCall(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleStaffMute(w http.ResponseWriter, r *http.Request) {
	var req denproto.VoiceMuteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.SetStaffMute(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member"), req.Muted); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleBans(w http.ResponseWriter, r *http.Request) {
	bans, err := rt.a.Dens.Bans(r.Context(), chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"bans": bans})
}

func (rt *router) handleUnban(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.Unban(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleOpenDM(w http.ResponseWriter, r *http.Request) {
	var req denproto.DMRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := rt.a.Dens.OpenDM(r.Context(), chi.URLParam(r, "den"), req.MemberID)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, c)
}

func (rt *router) handleCloseDM(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.CloseDM(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleLeave(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.Leave(r.Context(), chi.URLParam(r, "den")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleForget(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.Forget(r.Context(), chi.URLParam(r, "den")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCode gives what the page shows to compare codes with a member
// (M16.1): this member's own code, and whether they verified the other;
// with this member's own ID, their code alone.
func (rt *router) handleCode(w http.ResponseWriter, r *http.Request) {
	view, err := rt.a.Dens.Code(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, view)
}

// handleCompare takes the digits the member typed as the other read out
// their code. It asks nothing of the den.
func (rt *router) handleCompare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Digits string `json:"digits"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	view, err := rt.a.Dens.Compare(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member"), body.Digits)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, view)
}

// handleSendAnyway lets the DMs with a member whose keys changed since
// this member verified them take messages again.
func (rt *router) handleSendAnyway(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.SendAnyway(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
