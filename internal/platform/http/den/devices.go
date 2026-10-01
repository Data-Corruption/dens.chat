package den

import (
	"net/http"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (h *handler) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitPassword) {
		return
	}
	var req denproto.PasswordLoginRequest
	if !h.decode(w, r, &req) {
		return
	}
	resp, err := h.d.PasswordLogin(r.Context(), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusAccepted, resp)
}

func (h *handler) recover(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitPassword) {
		return
	}
	var req denproto.RecoverRequest
	if !h.decode(w, r, &req) {
		return
	}
	resp, err := h.d.Recover(r.Context(), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, resp)
}

func (h *handler) changePassword(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.PasswordChangeRequest
	if !h.decode(w, r, &req) {
		return
	}
	left, err := h.d.ChangePassword(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, left)
}

func (h *handler) newRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.RecoveryCodesRequest
	if !h.decode(w, r, &req) {
		return
	}
	codes, err := h.d.NewRecoveryCodes(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, codes)
}

func (h *handler) devices(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.Devices(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, list)
}

func (h *handler) revokeDevice(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	keyID, err := denproto.ParseBytes(chi.URLParam(r, "key"), denproto.IDSize)
	if err != nil {
		h.fail(w, r, denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such device"))
		return
	}
	if err := h.d.RevokeDevice(r.Context(), session(r), keyID); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
