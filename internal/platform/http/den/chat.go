package den

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	var q dens.HistoryQuery
	anchors := 0
	for name, field := range map[string]*int64{"before": &q.Before, "after": &q.After, "around": &q.Around} {
		if v := r.URL.Query().Get(name); v != "" {
			id, err := denproto.ParseID(v)
			if err != nil {
				h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "%s must be a message ID", name))
				return
			}
			*field = id
			anchors++
		}
	}
	if anchors > 1 {
		h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "use one of before, after and around"))
		return
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "limit must be a number"))
			return
		}
		q.Limit = n
	}
	page, err := h.d.History(r.Context(), session(r), chi.URLParam(r, "id"), q)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeGzip(w, r, page)
}

// writeGzip writes a JSON response, gzipped when the client accepts it. A
// history page shrinks to about a third.
func writeGzip(w http.ResponseWriter, r *http.Request, v any) {
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		denproto.WriteJSON(w, http.StatusOK, v)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	w.WriteHeader(http.StatusOK)
	gz := gzip.NewWriter(w)
	_ = json.NewEncoder(gz).Encode(v)
	_ = gz.Close()
}

func (h *handler) send(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	if err := h.d.Allow(dens.LimitSend, strconv.FormatInt(s.MemberID, 10)+":"+chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	var req denproto.SendRequest
	if !h.decode(w, r, &req) {
		return
	}
	m, err := h.d.Send(r.Context(), s, chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, m)
}

func (h *handler) editMessage(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.EditRequest
	if !h.decode(w, r, &req) {
		return
	}
	m, err := h.d.Edit(r.Context(), session(r), chi.URLParam(r, "id"), req)
	h.changed(w, r, m, err)
}

func (h *handler) setTask(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil || n < 0 || n > denproto.MaxTextRunes {
		h.fail(w, r, denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such task"))
		return
	}
	var req denproto.TaskRequest
	if !h.decode(w, r, &req) {
		return
	}
	m, err := h.d.SetTask(r.Context(), session(r), chi.URLParam(r, "id"), n, req)
	h.changed(w, r, m, err)
}

// changed answers an edit or a tick: the message as it now is, or a
// conflict carrying it.
func (h *handler) changed(w http.ResponseWriter, r *http.Request, m denproto.Message, err error) {
	if current, ok := dens.Conflict(err); ok {
		denproto.WriteJSON(w, http.StatusConflict, map[string]any{
			"error":   denproto.Errorf(http.StatusConflict, denproto.CodeEditConflict, "the message changed since that revision"),
			"message": current,
		})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, m)
}

func (h *handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.Delete(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) markRead(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.ReadRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.MarkRead(r.Context(), session(r), chi.URLParam(r, "id"), req); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) createChannel(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.ChannelRequest
	if !h.decode(w, r, &req) {
		return
	}
	c, err := h.d.CreateChannel(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, c)
}

func (h *handler) updateChannel(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.ChannelRequest
	if !h.decode(w, r, &req) {
		return
	}
	c, err := h.d.UpdateChannel(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, c)
}

func (h *handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.DeleteChannel(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) createGroup(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.GroupRequest
	if !h.decode(w, r, &req) {
		return
	}
	g, err := h.d.CreateGroup(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, g)
}

func (h *handler) updateGroup(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.GroupRequest
	if !h.decode(w, r, &req) {
		return
	}
	g, err := h.d.UpdateGroup(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, g)
}

func (h *handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	if err := h.d.DeleteGroup(r.Context(), session(r), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
