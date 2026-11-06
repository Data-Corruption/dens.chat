package den

import (
	"net/http"
	"strconv"
	"strings"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

// Private DMs and device approval (M1.7): the DM keys made before M16.1,
// starting over with a new seal, and sign-ins waiting for approval; and
// from M16.1 identities, the DM keys sealed to them, and members' records.

func (h *handler) dmKeys(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.DMKeys(r.Context(), session(r), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, list)
}

func (h *handler) startKey(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.KeyStartRequest
	if !h.decode(w, r, &req) {
		return
	}
	k, err := h.d.StartKey(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, k)
}

// Identities, signed DM keys and records (M16.1).

func (h *handler) signedKeys(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.SignedKeys(r.Context(), session(r), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, list)
}

func (h *handler) makeSignedKey(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.SignedKeyRequest
	if !h.decode(w, r, &req) {
		return
	}
	k, err := h.d.MakeSignedKey(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, k)
}

func (h *handler) publishIdentity(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.Identity
	if !h.decode(w, r, &req) {
		return
	}
	m, err := h.d.PublishIdentity(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, m)
}

func (h *handler) identities(w http.ResponseWriter, r *http.Request) {
	var members []string
	if v := r.URL.Query().Get("members"); v != "" {
		members = strings.Split(v, ",")
	}
	list, err := h.d.Identities(r.Context(), members)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, list)
}

func (h *handler) record(w http.ResponseWriter, r *http.Request) {
	rec, err := h.d.Record(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, rec)
}

func (h *handler) writeRecord(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.RecordWrite
	if !h.decode(w, r, &req) {
		return
	}
	rec, err := h.d.WriteRecord(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, rec)
}

func (h *handler) startOver(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.StartOverRequest
	if !h.decode(w, r, &req) {
		return
	}
	out, err := h.d.StartOver(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, out)
}

// pendingToken is the token a new device waiting for approval asks with.
func pendingToken(r *http.Request) denproto.Bytes {
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil
	}
	token, _ := denproto.ParseBytes(value, denproto.TokenSize)
	return token
}

func (h *handler) pending(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitLogin) {
		return
	}
	var after int64
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "after must be a version"))
			return
		}
		after = n
	}
	st, err := h.d.Pending(r.Context(), pendingToken(r), after)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, st)
}

func (h *handler) revealPending(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitLogin) {
		return
	}
	var req denproto.KeyRevealRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.RevealPending(r.Context(), pendingToken(r), req.Reveal); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestID reads a sign-in request's ID from the path.
func (h *handler) requestID(w http.ResponseWriter, r *http.Request) (denproto.Bytes, bool) {
	id, err := denproto.ParseBytes(chi.URLParam(r, "id"), denproto.RequestIDSize)
	if err != nil {
		h.fail(w, r, denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such sign-in waiting"))
		return nil, false
	}
	return id, true
}

func (h *handler) answerRequest(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	id, ok := h.requestID(w, r)
	if !ok {
		return
	}
	var req denproto.DeviceAnswerRequest
	if !h.decode(w, r, &req) {
		return
	}
	out, err := h.d.AnswerRequest(r.Context(), session(r), id, req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, out)
}

func (h *handler) approveRequest(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	id, ok := h.requestID(w, r)
	if !ok {
		return
	}
	var req denproto.ApproveRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.d.ApproveRequest(r.Context(), session(r), id, req); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) refuseRequest(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	id, ok := h.requestID(w, r)
	if !ok {
		return
	}
	if err := h.d.RefuseRequest(r.Context(), session(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Group DMs (M16.3).

func (h *handler) reserveGroup(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	g, err := h.d.ReserveGroup(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, g)
}

// openNotes answers with the member's notes, made the first time (M17.3).
func (h *handler) openNotes(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	c, err := h.d.OpenNotes(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, c)
}

func (h *handler) groupChanges(w http.ResponseWriter, r *http.Request) {
	after := 0
	if v := r.URL.Query().Get("after"); v != "" {
		var err error
		if after, err = strconv.Atoi(v); err != nil {
			denproto.WriteError(w, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "after: a number"))
			return
		}
	}
	list, err := h.d.GroupChanges(r.Context(), session(r), chi.URLParam(r, "id"), after)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, list)
}

func (h *handler) changeGroup(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.GroupChangeRequest
	if !h.decode(w, r, &req) {
		return
	}
	c, err := h.d.ChangeGroup(r.Context(), session(r), chi.URLParam(r, "id"), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, c)
}
