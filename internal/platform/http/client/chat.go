package client

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

// maxMessageBody leaves room for a message's text (16 KiB) and the rest.
const maxMessageBody = 64 << 10

func (rt *router) mountChat(r chi.Router) {
	r.Get("/api/dens/{den}/state", rt.handleDenState)
	r.Get("/api/dens/{den}/channels/{channel}/messages", rt.handleHistory)
	r.Post("/api/dens/{den}/channels/{channel}/messages", rt.handleSend)
	r.Put("/api/dens/{den}/channels/{channel}/read", rt.handleMarkRead)
	r.Patch("/api/dens/{den}/messages/{message}", rt.handleEdit)
	r.Delete("/api/dens/{den}/messages/{message}", rt.handleDeleteMessage)
	for _, kind := range []string{"channels", "groups"} {
		r.Post("/api/dens/{den}/"+kind, rt.handleManage(kind, http.MethodPost))
		r.Patch("/api/dens/{den}/"+kind+"/{id}", rt.handleManage(kind, http.MethodPatch))
		r.Delete("/api/dens/{den}/"+kind+"/{id}", rt.handleManage(kind, http.MethodDelete))
	}
}

func (rt *router) handleDenState(w http.ResponseWriter, r *http.Request) {
	view, err := rt.a.Dens.View(chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, view)
}

func (rt *router) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := denclient.HistoryQuery{
		Before: r.URL.Query().Get("before"),
		After:  r.URL.Query().Get("after"),
		Around: r.URL.Query().Get("around"),
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > denproto.MaxHistory {
			jsonError(w, http.StatusBadRequest, "limit must be 1 to 100")
			return
		}
		q.Limit = n
	}
	page, err := rt.a.Dens.History(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel"), q)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, page)
}

func (rt *router) handleSend(w http.ResponseWriter, r *http.Request) {
	var req denproto.SendRequest
	if !decodeJSONLimit(w, r, &req, maxMessageBody) {
		return
	}
	m, err := rt.a.Dens.Send(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel"), req)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, m)
}

func (rt *router) handleEdit(w http.ResponseWriter, r *http.Request) {
	var req denproto.EditRequest
	if !decodeJSONLimit(w, r, &req, maxMessageBody) {
		return
	}
	m, err := rt.a.Dens.Edit(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "message"), req)
	var conflict *denclient.ErrEditConflict
	if errors.As(err, &conflict) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusConflict)
		writeBody(w, map[string]any{"error": conflict.Error(), "message": conflict.Current})
		return
	}
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, m)
}

func (rt *router) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DeleteMessage(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "message")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (rt *router) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	var req denproto.ReadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.MarkRead(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel"), req.MessageID); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleManage passes channel and group management to the den; the
// result comes back as events.
func (rt *router) handleManage(kind, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req any
		switch {
		case method == http.MethodDelete:
		case kind == "channels":
			var body denproto.ChannelRequest
			if !decodeJSONLimit(w, r, &body, maxMessageBody) {
				return
			}
			req = body
		default:
			var body denproto.GroupRequest
			if !decodeJSON(w, r, &body) {
				return
			}
			req = body
		}
		if err := rt.a.Dens.Manage(r.Context(), chi.URLParam(r, "den"), kind, method, chi.URLParam(r, "id"), req); err != nil {
			rt.denError(w, r, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}
