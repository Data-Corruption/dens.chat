package client

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

// Events (M17.4): the page makes, changes, cancels and ends a den's events,
// and marks the member Interested, through the den; what changes comes
// back as the den's events too.
func (rt *router) mountEvents(r chi.Router) {
	r.Post("/api/dens/{den}/events", rt.handleCreateEvent)
	r.Patch("/api/dens/{den}/events/{event}", rt.handleUpdateEvent)
	r.Delete("/api/dens/{den}/events/{event}", rt.handleCancelEvent)
	r.Post("/api/dens/{den}/events/{event}/end", rt.handleEndEvent)
	r.Put("/api/dens/{den}/events/{event}/interest", rt.handleInterest(true))
	r.Delete("/api/dens/{den}/events/{event}/interest", rt.handleInterest(false))
}

func (rt *router) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	var req denproto.EventRequest
	if !decodeJSONLimit(w, r, &req, maxMessageBody) {
		return
	}
	e, err := rt.a.Dens.CreateEvent(r.Context(), chi.URLParam(r, "den"), req)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, e)
}

func (rt *router) handleUpdateEvent(w http.ResponseWriter, r *http.Request) {
	var req denproto.EventRequest
	if !decodeJSONLimit(w, r, &req, maxMessageBody) {
		return
	}
	e, err := rt.a.Dens.UpdateEvent(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "event"), req)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, e)
}

func (rt *router) handleCancelEvent(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.CancelEvent(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "event")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (rt *router) handleEndEvent(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.EndEvent(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "event")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleInterest marks the member Interested in an event, or takes it
// back, and answers with how many are and the member's mark.
func (rt *router) handleInterest(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in, err := rt.a.Dens.SetInterest(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "event"), on)
		if err != nil {
			rt.denError(w, r, err)
			return
		}
		writeJSON(w, in)
	}
}
