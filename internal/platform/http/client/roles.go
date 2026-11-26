package client

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (rt *router) mountRoles(r chi.Router) {
	r.Post("/api/dens/{den}/roles", rt.handleCreateRole)
	r.Patch("/api/dens/{den}/roles/{role}", rt.handleRenameRole)
	r.Delete("/api/dens/{den}/roles/{role}", rt.handleDeleteRole)
	r.Put("/api/dens/{den}/members/{member}/roles", rt.handleSetMemberRoles)
}

func (rt *router) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req denproto.DenRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	role, err := rt.a.Dens.CreateRole(r.Context(), chi.URLParam(r, "den"), req.Name)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, role)
}

func (rt *router) handleRenameRole(w http.ResponseWriter, r *http.Request) {
	var req denproto.DenRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	role, err := rt.a.Dens.RenameRole(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "role"), req.Name)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, role)
}

func (rt *router) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DeleteRole(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "role")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleSetMemberRoles(w http.ResponseWriter, r *http.Request) {
	var req denproto.MemberRolesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.SetMemberRoles(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "member"), req.RoleIDs); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
