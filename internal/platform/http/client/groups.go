package client

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Group DMs (M16.3): making one, adding and removing members, leaving and
// naming it. The page's own requests, each a change this service signs.

func (rt *router) mountGroups(r chi.Router) {
	r.Post("/api/dens/{den}/group-dms", rt.handleMakeGroup)
	r.Post("/api/dens/{den}/group-dms/{channel}/members", rt.handleAddToGroup)
	r.Delete("/api/dens/{den}/group-dms/{channel}/members/{member}", rt.handleRemoveFromGroup)
	r.Post("/api/dens/{den}/group-dms/{channel}/leave", rt.handleLeaveGroup)
	r.Put("/api/dens/{den}/group-dms/{channel}/name", rt.handleNameGroup)
}

type groupRequest struct {
	Members []string `json:"members"`
	Name    string   `json:"name"`
}

func (rt *router) handleMakeGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id, err := rt.a.Dens.MakeGroup(r.Context(), chi.URLParam(r, "den"), req.Members, req.Name)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func (rt *router) handleAddToGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.AddToGroup(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel"), req.Members); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleRemoveFromGroup(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.RemoveFromGroup(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel"), chi.URLParam(r, "member")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleLeaveGroup(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.LeaveGroup(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleNameGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.NameGroup(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel"), req.Name); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
