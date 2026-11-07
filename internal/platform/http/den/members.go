package den

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (h *handler) member(w http.ResponseWriter, r *http.Request) {
	m, err := h.d.Profile(r.Context(), session(r), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, m)
}

func (h *handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.ProfileRequest
	if !h.decode(w, r, &req) {
		return
	}
	m, err := h.d.UpdateProfile(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, m)
}

// setBadgeHidden hides a badge the member earned from their profile card,
// or shows it again (M17.6).
func (h *handler) setBadgeHidden(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.BadgeHiddenRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.SetBadgeHidden(r.Context(), session(r), chi.URLParam(r, "id"), req.Hidden); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) leave(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.Leave(r.Context(), session(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) setRole(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.RoleRequest
	if !h.decode(w, r, &req) {
		return
	}
	m, err := h.d.SetRole(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, m)
}

func (h *handler) removeMember(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.RemoveRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.Remove(r.Context(), session(r), chi.URLParam(r, "id"), req); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// disconnectMember ends a member's call at staff's word (M3).
func (h *handler) disconnectMember(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.DisconnectFromCall(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// moveMember moves a member's call to another voice channel at staff's
// word (M17.2).
func (h *handler) moveMember(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.MoveRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.MoveMember(r.Context(), session(r), chi.URLParam(r, "id"), req); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// voiceMute mutes a member in calls, or lifts it, at staff's word (M3).
func (h *handler) voiceMute(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.VoiceMuteRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.SetStaffMute(r.Context(), session(r), chi.URLParam(r, "id"), req.Muted); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) bans(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.Bans(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, list)
}

func (h *handler) unban(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.Unban(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) closeDM(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.CloseDM(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setExpiry sets how long a DM's or a group's messages stay (M16.4).
func (h *handler) setExpiry(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.ExpiryRequest
	if !h.decode(w, r, &req) {
		return
	}
	c, err := h.d.SetExpiry(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, c)
}

func (h *handler) openDM(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.DMRequest
	if !h.decode(w, r, &req) {
		return
	}
	c, err := h.d.OpenDM(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, c)
}
