package client

import (
	"net/http"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/notify"
	"github.com/go-chi/chi/v5"
)

func (rt *router) mountNotify(r chi.Router) {
	r.Post("/api/dens/{den}/notifications", rt.handleMute)
}

// handleMute mutes a den's notifications, or with channel one of its
// channels', DMs among them, or unmutes it; or with level, sets a text
// channel's level. The settings are the service's, so every browser
// paired with it shares them.
func (rt *router) handleMute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
		Muted   bool   `json:"muted"`
		// Level is all, mentions or nothing; a DM's and a group's bell
		// stays a mute, since each of their messages notifies already.
		Level notify.Level `json:"level"`
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
	i := slices.IndexFunc(view.Channels, func(c denproto.Channel) bool { return c.ID == body.Channel })
	if body.Channel != "" && i < 0 {
		jsonError(w, http.StatusNotFound, "That channel is gone, or you can't see it.")
		return
	}
	if body.Level != "" {
		if i < 0 || view.Channels[i].Kind != denproto.KindText {
			jsonError(w, http.StatusBadRequest, "Only a text channel has levels.")
			return
		}
		if body.Level != notify.LevelAll && body.Level != notify.LevelMentions && body.Level != notify.LevelNothing {
			jsonError(w, http.StatusBadRequest, "A level is all, mentions or nothing.")
			return
		}
		if err := rt.a.Notify.SetLevel(den, body.Channel, body.Level); err != nil {
			jsonError(w, http.StatusInternalServerError, "Dens couldn't keep that: "+err.Error())
			return
		}
		writeJSON(w, rt.a.Notify.Muted(den))
		return
	}
	if err := rt.a.Notify.Mute(den, body.Channel, body.Muted); err != nil {
		jsonError(w, http.StatusInternalServerError, "Dens couldn't keep that: "+err.Error())
		return
	}
	writeJSON(w, rt.a.Notify.Muted(den))
}
