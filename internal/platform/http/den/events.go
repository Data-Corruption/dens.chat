package den

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

// Events (M17.4): members make events, as the den's limits allow, change,
// cancel and end them, and mark Interested or take it back. Each counts
// toward the member's writes.

func (h *handler) createEvent(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.EventRequest
	if !h.decode(w, r, &req) {
		return
	}
	e, err := h.d.CreateEvent(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, e)
}

func (h *handler) updateEvent(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.EventRequest
	if !h.decode(w, r, &req) {
		return
	}
	e, err := h.d.UpdateEvent(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, e)
}

func (h *handler) cancelEvent(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.CancelEvent(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) endEvent(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.EndEvent(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// markInterest marks the member Interested in an event, or takes it back,
// and answers with how many are and the member's mark.
func (h *handler) markInterest(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.limitWrite(w, r) {
			return
		}
		interest, err := h.d.SetInterest(r.Context(), session(r), chi.URLParam(r, "id"), on)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		denproto.WriteJSON(w, http.StatusOK, interest)
	}
}
