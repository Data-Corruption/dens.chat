package den

import (
	"net/http"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

// Reactions (M15.3): a member adds a reaction to a channel's message, or
// takes one back; staff take one emoji's reactions off a message; and in a
// DM a member sets their sealed set. Each counts toward the member's other
// writes.

func (h *handler) react(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.limitWrite(w, r) {
			return
		}
		var req denproto.ReactionRequest
		if !h.decode(w, r, &req) {
			return
		}
		react := h.d.Unreact
		if add {
			react = h.d.React
		}
		if err := react(r.Context(), session(r), chi.URLParam(r, "id"), req); err != nil {
			h.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handler) clearReactions(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.ReactionRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.ClearReactions(r.Context(), session(r), chi.URLParam(r, "id"), req); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setDMReactions answers a DM's sealed set with the set as the den keeps
// it, or a conflict carrying the member's set as it is now, as an edit's
// does the message.
func (h *handler) setDMReactions(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.DMReactionsRequest
	if !h.decode(w, r, &req) {
		return
	}
	set, err := h.d.SetDMReactions(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if current, ok := dens.ReactionsConflict(err); ok {
		body := map[string]any{"error": denproto.Errorf(http.StatusConflict, denproto.CodeEditConflict, "the set changed since that revision")}
		if current != nil {
			body["reactions"] = current
		}
		denproto.WriteJSON(w, http.StatusConflict, body)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, set)
}
