package den

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

// The den's emoji (M15.2): staff add emoji, rename and delete them.

func (h *handler) addEmoji(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.EmojiRequest
	if !h.decode(w, r, &req) {
		return
	}
	emoji, err := h.d.AddEmoji(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, emoji)
}

func (h *handler) renameEmoji(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.EmojiUpdate
	if !h.decode(w, r, &req) {
		return
	}
	emoji, err := h.d.RenameEmoji(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, emoji)
}

func (h *handler) deleteEmoji(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.DeleteEmoji(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
