package client

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// The den's soundboard (M14.3): staff's page adds a sound by an upload it
// prepared, and renames and deletes sounds. Members play them through the
// page's stream (voice.sound).
func (rt *router) mountSoundboard(r chi.Router) {
	r.Post("/api/dens/{den}/soundboard", rt.handleAddSound)
	r.Patch("/api/dens/{den}/soundboard/{sound}", rt.handleRenameSound)
	r.Delete("/api/dens/{den}/soundboard/{sound}", rt.handleDeleteSound)
}

func (rt *router) handleAddSound(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Upload string `json:"upload"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	sound, err := rt.a.Dens.AddSound(r.Context(), chi.URLParam(r, "den"), body.Name, body.Upload)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, sound)
}

func (rt *router) handleRenameSound(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	sound, err := rt.a.Dens.RenameSound(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "sound"), body.Name)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, sound)
}

func (rt *router) handleDeleteSound(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DeleteSound(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "sound")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
