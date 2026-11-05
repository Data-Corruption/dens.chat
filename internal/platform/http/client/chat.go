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
	r.Post("/api/dens/{den}/messages/{message}/tasks/{n}", rt.handleTask)
	r.Delete("/api/dens/{den}/messages/{message}", rt.handleDeleteMessage)
	r.Post("/api/dens/{den}/messages/{message}/reactions", rt.handleReact)
	r.Post("/api/dens/{den}/messages/{message}/reactions/clear", rt.handleClearReaction)
	r.Post("/api/dens/{den}/messages/{message}/poll/vote", rt.handleVote)
	r.Post("/api/dens/{den}/messages/{message}/poll/end", rt.handleEndPoll)
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
	m, err := rt.a.Dens.Edit(r.Context(), chi.URLParam(r, "den"), r.URL.Query().Get("channel"), chi.URLParam(r, "message"), req)
	rt.changed(w, r, m, err)
}

func (rt *router) handleTask(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil {
		jsonError(w, http.StatusNotFound, "No such task.")
		return
	}
	var req denproto.TaskRequest
	if !decodeJSONLimit(w, r, &req, maxMessageBody) {
		return
	}
	m, err := rt.a.Dens.SetTask(r.Context(), chi.URLParam(r, "den"), r.URL.Query().Get("channel"), chi.URLParam(r, "message"), n, req)
	rt.changed(w, r, m, err)
}

// changed answers an edit or a tick: the message as it now is, or a
// conflict carrying it.
func (rt *router) changed(w http.ResponseWriter, r *http.Request, m denclient.PageMessage, err error) {
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

// handleReact adds the member's reaction to a message in the channel the
// query names, or takes it back (M15.3). A DM's answers with the member's
// set as it now is, which this service sealed; a channel's comes back as
// the den's event.
func (rt *router) handleReact(w http.ResponseWriter, r *http.Request) {
	var req denclient.PageReaction
	if !decodeJSON(w, r, &req) {
		return
	}
	set, err := rt.a.Dens.React(r.Context(), chi.URLParam(r, "den"), r.URL.Query().Get("channel"), chi.URLParam(r, "message"), req)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "set": set})
}

// handleClearReaction takes one emoji's reactions off a channel's message,
// at staff's word: those of the members they outrank.
func (rt *router) handleClearReaction(w http.ResponseWriter, r *http.Request) {
	var req denproto.ReactionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := rt.a.Dens.ClearReaction(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "message"), req); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleVote sets the member's vote on a poll (M15.4), the choices they
// pick by their place in it, or none to take it back, and answers with the
// poll as they now see it.
func (rt *router) handleVote(w http.ResponseWriter, r *http.Request) {
	var req denproto.VoteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	poll, err := rt.a.Dens.Vote(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "message"), req.Choices)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, poll)
}

// handleEndPoll ends the member's own poll before its time, and answers
// with it as it ended.
func (rt *router) handleEndPoll(w http.ResponseWriter, r *http.Request) {
	poll, err := rt.a.Dens.EndPoll(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "message"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, poll)
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
