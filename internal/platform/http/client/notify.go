package client

import (
	"net/http"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/go-chi/chi/v5"
)

func (rt *router) mountNotify(r chi.Router) {
	r.Post("/api/dens/{den}/notifications", rt.handleMute)
}

// handleMute mutes a den's notifications, or with channel one of its
// channels', DMs among them, or unmutes it (M12.3). The settings are the
// service's, so every browser paired with it shares them.
func (rt *router) handleMute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
		Muted   bool   `json:"muted"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	den := chi.URLParam(r, "den")
	view, err := rt.a.Dens.View(den)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	// A channel goes, and its mute with it, only as the den says it's
	// gone; one the den never had isn't kept.
	if body.Channel != "" && !slices.ContainsFunc(view.Channels, func(c denproto.Channel) bool { return c.ID == body.Channel }) {
		jsonError(w, http.StatusNotFound, "That channel is gone, or you can't see it.")
		return
	}
	if err := rt.a.Notify.Mute(den, body.Channel, body.Muted); err != nil {
		jsonError(w, http.StatusInternalServerError, "Dens couldn't keep that: "+err.Error())
		return
	}
	writeJSON(w, rt.a.Notify.Muted(den))
}
