package client

import (
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"

	"github.com/go-chi/chi/v5"
)

// mountFlatpak serves what only the Flatpak's page asks of its service
// (M9.3): installing an update through Flatpak, restarting into the newest
// build installed, quitting, and starting at sign-in. The Android app's
// page installs its update at the same address (M12.4).
func (rt *router) mountFlatpak(r chi.Router) {
	r.Post("/api/update", rt.handleUpdate)
	r.Post("/api/restart", rt.handleRestart)
	r.Post("/api/quit", rt.handleQuit)
	r.Post("/api/start-at-sign-in", rt.handleStartAtSignIn)
}

func (rt *router) flatpak(w http.ResponseWriter) bool {
	if rt.a.Flatpak == nil {
		http.Error(w, "only the Flatpak does this", http.StatusNotFound)
		return false
	}
	return true
}

// stopAfter answers, then stops the service: the page waits for the one
// that takes over, or says Dens has quit.
func (rt *router) stopAfter(w http.ResponseWriter, why string) {
	writeJSON(w, map[string]bool{"ok": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	rt.a.Flatpak.Quit(why)
}

// handleUpdate has Flatpak install the update, where it lets Dens, and
// restarts into it; where it won't, the page says to update in the
// software center. In the Android app, the shell hands the update the
// updater checked to Android's installer, which asks the member to confirm
// over the page (M12.4).
func (rt *router) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if rt.a.Layout.Form == layout.Android {
		if err := rt.a.InstallUpdate(); err != nil {
			jsonError(w, http.StatusConflict, "No update is ready to install. Open Dens again, then try again.")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if !rt.flatpak(w) {
		return
	}
	switch err := rt.a.Flatpak.InstallUpdate(r.Context()); {
	case errors.Is(err, host.ErrUpdateRefused):
		jsonError(w, http.StatusConflict, "Flatpak won't install this update from Dens. Update Dens in your software center.")
		return
	case err != nil:
		xhttp.Error(r.Context(), w, err)
		return
	}
	if err := rt.a.Flatpak.StartNewest(); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.stopAfter(w, "restarting into the update")
}

// handleRestart starts the newest build installed, which takes over once
// this one stops.
func (rt *router) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !rt.flatpak(w) {
		return
	}
	if err := rt.a.Flatpak.StartNewest(); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.stopAfter(w, "restarting into the newest build installed")
}

// handleQuit stops the Flatpak, which has no service manager to stop it.
func (rt *router) handleQuit(w http.ResponseWriter, _ *http.Request) {
	if !rt.flatpak(w) {
		return
	}
	rt.stopAfter(w, "asked to from the page")
}

// handleStartAtSignIn turns starting at sign-in on or off.
func (rt *router) handleStartAtSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if !decodeJSON(w, r, &body) || !rt.flatpak(w) {
		return
	}
	if err := rt.a.Flatpak.SetStartAtSignIn(r.Context(), body.On); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, rt.a.Flatpak.StartAtSignIn())
}
