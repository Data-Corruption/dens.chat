package den

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

// Polls (M15.4): a member votes on a poll, or takes their vote back, and
// its author ends it early. Each answers with the poll as the member now
// sees it, and counts toward their other writes.

func (h *handler) vote(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.VoteRequest
	if !h.decode(w, r, &req) {
		return
	}
	poll, err := h.d.Vote(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, poll)
}

func (h *handler) endPoll(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	poll, err := h.d.EndPoll(r.Context(), session(r), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, poll)
}
