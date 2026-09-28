package den

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (h *handler) member(w http.ResponseWriter, r *http.Request) {
	m, err := h.d.Profile(r.Context(), chi.URLParam(r, "id"))
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
