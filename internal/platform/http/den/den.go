// Package den is the router of the den listener, which Caddy exposes to the
// internet. It serves only den routes; the client page and its API are never
// mounted here. The den's logic lives in internal/den; this package is the
// HTTP layer of docs/dev/protocol.md.
package den

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/app"
	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/go-chi/chi/v5"
)

type handler struct {
	d   *dens.Den
	log *xlog.Logger
}

// New returns the den listener's handler.
func New(a *app.App) http.Handler {
	h := &handler{d: a.Den, log: a.Log}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(xlog.IntoContext(req.Context(), a.Log)))
		})
	})
	r.Use(guard.SecurityHeaders)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	r.Route("/api", func(r chi.Router) {
		r.Use(versionCheck)
		r.Use(h.requireCreated)
		r.Post("/auth/challenge", h.challenge)
		r.Post("/join/preview", h.preview)
		r.Post("/join", h.join)
		r.Post("/auth/login", h.login)
		r.Post("/auth/password", h.passwordLogin)
		r.Post("/auth/recover", h.recover)
		r.Group(func(r chi.Router) {
			r.Use(h.authenticate)
			r.Post("/auth/logout", h.logout)
			r.Get("/ws", h.socket)
			r.Post("/invites", h.createInvite)
			r.Get("/invites", h.invites)
			r.Delete("/invites/{id}", h.revokeInvite)
			r.Patch("/den", h.updateDen)
			r.Get("/channels/{id}/messages", h.history)
			r.Post("/channels/{id}/messages", h.send)
			r.Put("/channels/{id}/read", h.markRead)
			r.Patch("/messages/{id}", h.editMessage)
			r.Delete("/messages/{id}", h.deleteMessage)
			r.Post("/channels", h.createChannel)
			r.Patch("/channels/{id}", h.updateChannel)
			r.Delete("/channels/{id}", h.deleteChannel)
			r.Post("/groups", h.createGroup)
			r.Patch("/groups/{id}", h.updateGroup)
			r.Delete("/groups/{id}", h.deleteGroup)
			r.Get("/members/{id}", h.member)
			r.Patch("/members/{id}", h.setRole)
			r.Post("/members/{id}/remove", h.removeMember)
			r.Patch("/me", h.updateProfile)
			r.Post("/me/leave", h.leave)
			r.Get("/bans", h.bans)
			r.Delete("/bans/{id}", h.unban)
			r.Post("/dms", h.openDM)
			r.Post("/dms/{id}/close", h.closeDM)
			r.Post("/uploads", h.upload)
			r.Get("/files/{id}", h.file(false))
			r.Get("/files/{id}/thumb", h.file(true))
			r.Get("/me/storage", h.storage)
			r.Post("/me/password", h.changePassword)
			r.Post("/me/recovery-codes", h.newRecoveryCodes)
			r.Get("/me/devices", h.devices)
			r.Delete("/me/devices/{key}", h.revokeDevice)
		})
	})
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		denproto.WriteError(w, denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such route"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		denproto.WriteError(w, denproto.Errorf(http.StatusMethodNotAllowed, denproto.CodeNotFound, "method not allowed"))
	})
	return r
}

// versionCheck admits requests that speak a protocol version this den
// supports, and tells every client which versions those are.
func versionCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(denproto.HeaderRange, denproto.RangeHeader())
		v, err := strconv.Atoi(r.Header.Get(denproto.HeaderVersion))
		if err != nil || v < denproto.MinVersion || v > denproto.Version {
			denproto.WriteError(w, denproto.Errorf(http.StatusUpgradeRequired, denproto.CodeProtocolUnsupported,
				"this den speaks protocol %s", denproto.RangeHeader()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) requireCreated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.d.Info(); !ok {
			denproto.WriteError(w, denproto.Errorf(http.StatusServiceUnavailable, denproto.CodeDenNotCreated,
				"this den hasn't been created yet"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

type sessionKey struct{}

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		var token denproto.Bytes
		if ok {
			token, _ = denproto.ParseBytes(value, denproto.TokenSize)
		}
		s, err := h.d.Authenticate(r.Context(), token)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, s)))
	})
}

func session(r *http.Request) *dens.Session { return r.Context().Value(sessionKey{}).(*dens.Session) }

// clientIP is the address rate limits apply to. Caddy connects from
// loopback and names the client in X-Forwarded-For; nothing else is
// trusted to set it. A loopback connection without the header is a client
// on this machine, such as the owner's own.
func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if forwarded := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); forwarded != nil {
				return forwarded
			}
		}
	}
	if ip == nil {
		return net.IPv4zero
	}
	return ip
}

func (h *handler) limitIP(w http.ResponseWriter, r *http.Request, kind int) bool {
	if err := h.d.Allow(kind, dens.IPKey(clientIP(r))); err != nil {
		h.fail(w, r, err)
		return false
	}
	return true
}

// decode reads a JSON body of at most MaxBody bytes. Unknown fields are
// ignored, as the protocol requires; the den checks every field it uses.
func (h *handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, denproto.MaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.fail(w, r, denproto.Errorf(http.StatusRequestEntityTooLarge, denproto.CodeTooLarge, "request body too large"))
		} else {
			h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeMalformed, "unreadable body"))
		}
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeMalformed, "malformed JSON: %v", err))
		return false
	}
	return true
}

// fail writes a protocol error as is, and logs anything else as the den's
// own failure without telling the client more than that.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var perr *denproto.Error
	if errors.As(err, &perr) {
		denproto.WriteError(w, perr)
		return
	}
	h.log.Errorf("%s %s: %v", r.Method, r.URL.Path, err)
	denproto.WriteError(w, denproto.Errorf(http.StatusInternalServerError, "internal", "the den failed; try again"))
}

func (h *handler) challenge(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitChallenge) {
		return
	}
	var req denproto.ChallengeRequest
	if !h.decode(w, r, &req) {
		return
	}
	resp, err := h.d.Challenge(req.ClientNonce)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, resp)
}

func (h *handler) preview(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitJoin) {
		return
	}
	var req denproto.JoinPreviewRequest
	if !h.decode(w, r, &req) {
		return
	}
	info, err := h.d.Preview(r.Context(), req.Invite)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, denproto.JoinPreviewResponse{Den: info})
}

func (h *handler) join(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitJoin) {
		return
	}
	var req denproto.JoinRequest
	if !h.decode(w, r, &req) {
		return
	}
	resp, err := h.d.Join(r.Context(), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, resp)
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitLogin) {
		return
	}
	var req denproto.LoginRequest
	if !h.decode(w, r, &req) {
		return
	}
	resp, err := h.d.Login(r.Context(), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, resp)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Logout(r.Context(), session(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) socket(w http.ResponseWriter, r *http.Request) {
	if !h.limitIP(w, r, dens.LimitSocket) {
		return
	}
	h.d.ServeSocket(w, r, session(r))
}

func (h *handler) limitWrite(w http.ResponseWriter, r *http.Request) bool {
	if err := h.d.Allow(dens.LimitWrite, strconv.FormatInt(session(r).MemberID, 10)); err != nil {
		h.fail(w, r, err)
		return false
	}
	return true
}

func (h *handler) createInvite(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.InviteCreateRequest
	if !h.decode(w, r, &req) {
		return
	}
	inv, err := h.d.CreateInvite(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, inv)
}

func (h *handler) invites(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.Invites(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, denproto.InviteList{Invites: list})
}

func (h *handler) revokeInvite(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.fail(w, r, denproto.Errorf(http.StatusNotFound, denproto.CodeNotFound, "no such invite"))
		return
	}
	if err := h.d.RevokeInvite(r.Context(), session(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) updateDen(w http.ResponseWriter, r *http.Request) {
	if !h.limitWrite(w, r) {
		return
	}
	var req denproto.DenUpdateRequest
	if !h.decode(w, r, &req) {
		return
	}
	info, err := h.d.Update(r.Context(), session(r), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, info)
}
