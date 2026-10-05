// Package client is the router of the client listener: the chat page and its
// local API, served only to the desktop user's browser on loopback.
//
// A browser pairs by redeeming a one-time token from dens open for a session
// cookie. Everything but the page shell, its assets and the pairing call
// needs that session; sensitive actions also need the local password.
package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/clientsessions"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/cookies"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/go-chi/chi/v5"
)

const maxJSONBody = 16 << 10

type router struct {
	a *app.App
	// cookieName includes the port: cookies are scoped to a host, not a
	// port, and several instances can share 127.0.0.1.
	cookieName string
}

// New returns the client listener's handler.
func New(a *app.App) http.Handler {
	rt := &router{a: a, cookieName: "dens_session_" + strconv.Itoa(a.Instance.ClientPort)}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(xlog.IntoContext(req.Context(), a.Log)))
		})
	})
	r.Use(guard.Host(guard.LoopbackHosts(a.Instance.ClientPort)))
	r.Use(guard.SecurityHeaders)
	r.Use(guard.SameOrigin)

	r.Get("/healthz", handleHealth)
	r.Get("/licenses", handleLicenses)
	r.Get("/assets/*", a.UI.ServeAsset)
	// The page decides what to show from the API; an unpaired browser gets
	// 401s and shows how to pair.
	r.Get("/", rt.handlePage)
	r.Get("/settings", rt.handlePage)
	r.Get("/den/*", rt.handlePage)
	r.Post("/api/pair", rt.handlePair)
	r.Group(func(r chi.Router) {
		r.Use(rt.requireSession)
		r.Get("/api/status", rt.handleStatus)
		r.Post("/api/password", rt.handleSetPassword)
		r.Post("/api/password/change", rt.handleChangePassword)
		r.Get("/api/settings", rt.handleSettings)
		r.Post("/api/settings", rt.handleSettingsUpdate)
		r.Post("/api/logout", rt.handleLogout)
		rt.mountDens(r)
	})
	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// handleLicenses shows the third-party notices the binary carries, as dens
// licenses prints them. They're the same for anyone, so they need no
// session.
func handleLicenses(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(build.Notices()))
}

type sessionKey struct{}

// session returns the hash of the request's valid session token, or "".
func (rt *router) session(w http.ResponseWriter, r *http.Request) (string, error) {
	token := cookies.Read(r, rt.cookieName)
	if token == "" {
		return "", nil
	}
	hash := hashToken(token)
	valid, renewed, err := clientsessions.Validate(r.Context(), rt.a.DB, hash, time.Now())
	if err != nil || !valid {
		return "", err
	}
	if renewed {
		rt.setCookie(w, token)
	}
	return hash, nil
}

func (rt *router) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash, err := rt.session(w, r)
		if err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
		if hash == "" {
			if r.Method == http.MethodGet && r.URL.Path == "/settings" {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			http.Error(w, "this browser is not paired; run dens open", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, hash)))
	})
}

func (rt *router) setCookie(w http.ResponseWriter, token string) {
	cookies.Set(w, rt.cookieName, token, "/", clientsessions.Lifetime, false)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (rt *router) pageData(title string) map[string]any {
	data := rt.a.UI.PageData(title, rt.a.BuildInfo().Version)
	data["Instance"] = rt.a.Layout.Instance
	return data
}

func (rt *router) handlePage(w http.ResponseWriter, r *http.Request) {
	rt.render(w, r, "app.html", rt.pageData("Dens"))
}

func (rt *router) handleSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.View(rt.a.DB)
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, map[string]any{
		"logLevel":               cfg.LogLevel,
		"logLevels":              strings.Split(xlog.ValidLevels, "|"),
		"updateNotifications":    cfg.UpdateNotifications,
		"backgroundUpdateChecks": cfg.BackgroundUpdateChecks,
		"updatesManaged":         rt.a.Instance.ReleaseURL != "" && !rt.a.DevMode(),
	})
}

func (rt *router) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := rt.a.UI.Execute(w, name, data); err != nil {
		xlog.Errorf(r.Context(), "render %s: %v", name, err)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSONLimit(w, r, v, maxJSONBody)
}

// decodeJSONLimit decodes a body of at most limit bytes. Messages need
// more room than other requests: their text alone can be 16 KiB.
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		http.Error(w, "malformed request", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	writeBody(w, v)
}

func writeBody(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

// handlePair redeems a pairing token for a new session.
func (rt *router) handlePair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !rt.a.Pairing.Redeem(body.Token) {
		http.Error(w, "this pairing link has expired or was already used; run dens open again", http.StatusForbidden)
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	if err := clientsessions.DeleteExpired(r.Context(), rt.a.DB, now); err != nil {
		rt.a.Log.Warnf("prune expired sessions: %v", err)
	}
	if err := clientsessions.Create(r.Context(), rt.a.DB, hashToken(token), now); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.a.Log.Info("Browser paired")
	rt.setCookie(w, token)
	writeJSON(w, map[string]bool{"ok": true})
}

func (rt *router) handleStatus(w http.ResponseWriter, r *http.Request) {
	passwordSet, err := rt.a.PasswordSet(r.Context())
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	cfg, err := config.View(rt.a.DB)
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	status := map[string]any{
		"version":     rt.a.BuildInfo().Version,
		"instance":    rt.a.Layout.Instance,
		"passwordSet": passwordSet,
		"denEnabled":  rt.a.Instance.Den.Enabled,
	}
	if latest, ok := rt.a.LatestUpdate(cfg); ok && cfg.UpdateNotifications {
		status["updateVersion"] = latest
		status["updateCommand"] = rt.a.UpdateCommand()
	}
	writeJSON(w, status)
}

// handleSetPassword sets the first local password. Changing it later needs
// the current one.
func (rt *router) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	passwordSet, err := rt.a.PasswordSet(r.Context())
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	if passwordSet {
		http.Error(w, "a local password is already set; change it from settings", http.StatusConflict)
		return
	}
	if err := vault.ValidatePassword(body.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := rt.a.SetPassword(r.Context(), body.Password); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.a.Log.Info("Local password set")
	writeJSON(w, map[string]bool{"ok": true})
}

func (rt *router) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := vault.ValidatePassword(body.Next); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := rt.a.VerifyPassword(r.Context(), body.Current); err != nil {
		passwordError(w, r, err)
		return
	}
	if err := rt.a.SetPassword(r.Context(), body.Next); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.a.Log.Info("Local password changed")
	writeJSON(w, map[string]bool{"ok": true})
}

func passwordError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vault.ErrWrongPassword):
		http.Error(w, "wrong password", http.StatusForbidden)
	case errors.Is(err, app.ErrTooManyAttempts):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
	case errors.Is(err, app.ErrNoPassword):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		xhttp.Error(r.Context(), w, err)
	}
}

func (rt *router) handleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LogLevel               *string `json:"logLevel"`
		UpdateNotifications    *bool   `json:"updateNotifications"`
		BackgroundUpdateChecks *bool   `json:"backgroundUpdateChecks"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	cfg, err := config.Update(rt.a.DB, func(cfg *types.Configuration) error {
		if body.LogLevel != nil {
			cfg.LogLevel = *body.LogLevel
		}
		if body.UpdateNotifications != nil {
			cfg.UpdateNotifications = *body.UpdateNotifications
		}
		if body.BackgroundUpdateChecks != nil {
			cfg.BackgroundUpdateChecks = *body.BackgroundUpdateChecks
		}
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.LogLevel != nil && !rt.a.DevMode() {
		if err := rt.a.Log.SetLevel(cfg.LogLevel); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (rt *router) handleLogout(w http.ResponseWriter, r *http.Request) {
	hash, _ := r.Context().Value(sessionKey{}).(string)
	if err := clientsessions.Delete(r.Context(), rt.a.DB, hash); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	cookies.Set(w, rt.cookieName, "", "/", -time.Second, false)
	writeJSON(w, map[string]bool{"ok": true})
}
