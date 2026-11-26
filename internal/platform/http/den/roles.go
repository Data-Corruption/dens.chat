package den

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (h *handler) createRole(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.DenRoleRequest
	if !h.decode(w, r, &req) {
		return
	}
	role, err := h.d.CreateRole(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, role)
}

func (h *handler) renameRole(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.DenRoleRequest
	if !h.decode(w, r, &req) {
		return
	}
	role, err := h.d.RenameRole(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, role)
}

func (h *handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.DeleteRole(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) setMemberRoles(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.MemberRolesRequest
	if !h.decode(w, r, &req) {
		return
	}
	m, err := h.d.SetMemberRoles(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, m)
}
