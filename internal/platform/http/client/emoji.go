package client

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// The den's emoji (M15.2): staff's page adds an emoji by an upload, a PNG
// it drew or a GIF this service made, and renames and deletes them. Every
// page shows them by their images, which the den's files serve.
func (rt *router) mountEmoji(r chi.Router) {
	r.Post("/api/dens/{den}/emoji", rt.handleAddEmoji)
	r.Patch("/api/dens/{den}/emoji/{emoji}", rt.handleRenameEmoji)
	r.Delete("/api/dens/{den}/emoji/{emoji}", rt.handleDeleteEmoji)
}

func (rt *router) handleAddEmoji(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Upload string `json:"upload"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	emoji, err := rt.a.Dens.AddEmoji(r.Context(), chi.URLParam(r, "den"), body.Name, body.Upload)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, emoji)
}

func (rt *router) handleRenameEmoji(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	emoji, err := rt.a.Dens.RenameEmoji(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "emoji"), body.Name)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, emoji)
}

func (rt *router) handleDeleteEmoji(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DeleteEmoji(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "emoji")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
