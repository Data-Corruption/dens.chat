package den

import (
	"net/http"
	"strconv"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (h *handler) listCollections(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.ListCollections(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, list)
}

func (h *handler) createPersonalCollection(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.CollectionRequest
	if !h.decode(w, r, &req) {
		return
	}
	col, err := h.d.CreatePersonalCollection(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, col)
}

func (h *handler) createChannelCollection(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.CollectionRequest
	if !h.decode(w, r, &req) {
		return
	}
	col, err := h.d.CreateChannelCollection(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, col)
}

func (h *handler) updateCollection(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.CollectionRequest
	if !h.decode(w, r, &req) {
		return
	}
	col, err := h.d.UpdateCollection(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, col)
}

func (h *handler) deleteCollection(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.DeleteCollection(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) collectionItems(w http.ResponseWriter, r *http.Request) {
	var q dens.CollectionItemsQuery
	atRaw := r.URL.Query().Get("before_at")
	idRaw := r.URL.Query().Get("before_id")
	if (atRaw == "") != (idRaw == "") {
		h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "before_at and before_id must be set together"))
		return
	}
	if atRaw != "" {
		at, err := strconv.ParseInt(atRaw, 10, 64)
		if err != nil || at < 0 {
			h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "before_at must be a timestamp"))
			return
		}
		id, err := denproto.ParseID(idRaw)
		if err != nil {
			h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "before_id must be a message ID"))
			return
		}
		q.BeforeAt, q.BeforeID = at, id
	}
	items, err := h.d.CollectionItems(r.Context(), session(r), chi.URLParam(r, "id"), q)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeGzip(w, r, items)
}

func (h *handler) addCollectionItem(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.CollectionItemRequest
	if !h.decode(w, r, &req) {
		return
	}
	ev, err := h.d.AddCollectionItem(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, ev)
}

func (h *handler) removeCollectionItem(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	ev, err := h.d.RemoveCollectionItem(r.Context(), session(r), chi.URLParam(r, "id"), chi.URLParam(r, "message_id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, ev)
}

func (h *handler) messageCollections(w http.ResponseWriter, r *http.Request) {
	res, err := h.d.MessageCollections(r.Context(), session(r), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, res)
}
