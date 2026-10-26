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
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/clientsessions"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/cookies"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/internal/youtube"
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
	// pageOrigin is where browsers load the page (M4.1).
	pageOrigin string
	youTube    *youtube.Lookup
	backups    backups
}

// New returns the client listener's handler.
func New(a *app.App) http.Handler {
	rt := &router{
		a:          a,
		cookieName: "dens_session_" + strconv.Itoa(a.Instance.ClientPort),
		pageOrigin: guard.PageOrigin(a.Instance.ClientPort),
		youTube:    youtube.New(),
		backups:    backups{links: map[string]backupLink{}},
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(xlog.IntoContext(req.Context(), a.Log)))
		})
	})
	r.Use(guard.Host(guard.LoopbackHosts(a.Instance.ClientPort)))
	r.Use(guard.PageSecurityHeaders)
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
		r.Use(rt.requireThisVersion)
		r.Use(rt.requireSession)
		r.Get("/api/status", rt.handleStatus)
		r.Post("/api/unlock", rt.handleUnlock)
		r.Post("/api/logout", rt.handleLogout)
		r.Group(func(r chi.Router) {
			r.Use(rt.requireUnlocked)
			r.Post("/api/password", rt.handleSetPassword)
			r.Post("/api/password/change", rt.handleChangePassword)
			r.Post("/api/remember", rt.handleRemember)
			r.Get("/api/settings", rt.handleSettings)
			r.Post("/api/settings", rt.handleSettingsUpdate)
			r.Post("/api/cache/clear", rt.handleClearCache)
			r.Get("/api/logs", rt.handleLogs)
			r.Post("/api/shortcuts/desktop", rt.handleShortcutsDesktop)
			rt.mountBackups(r)
			rt.mountFlatpak(r)
			rt.mountDens(r)
			rt.mountYouTube(r)
		})
	})
	return r
}

// Headers between the page and the service: the version of Dens that
// served the page, which it names in every request, and the service's
// answer to a page another version served.
const (
	pageVersionHeader = "Dens-Page-Version"
	reloadHeader      = "Dens-Reload"
)

// requireThisVersion refuses a request from a page another version of
// Dens served, as one open across an update that restarted the service
// (M9.3): it answers 409, which has the page load again, from this version,
// rather than ask in a shape this version may not know. A request that
// names no version, as a script's, passes.
func (rt *router) requireThisVersion(next http.Handler) http.Handler {
	version := rt.a.BuildInfo().Version
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if page := r.Header.Get(pageVersionHeader); page != "" && page != version {
			w.Header().Set(reloadHeader, version)
			jsonError(w, http.StatusConflict, "Dens was updated; this page loads again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireUnlocked refuses what needs the data key while the Flatpak waits
// for it (M9.3): the page shows its unlock screen and asks nothing else.
func (rt *router) requireUnlocked(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rt.a.Locked() {
			jsonError(w, http.StatusLocked, "Dens is locked; unlock it with the local password.")
			return
		}
		next.ServeHTTP(w, r)
	})
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
	data["Pair"], data["PairLink"] = rt.pairCommands()
	return data
}

// pairCommands are what pair a browser with this instance: open, which
// opens the page in the default browser, dens open, or none in the
// Flatpak, which pairs as it's opened from the apps menu (M9.3); and link,
// which prints the link to open in any other.
func (rt *router) pairCommands() (open, link string) {
	name, args := rt.a.BuildInfo().Name, "open"
	if instance := rt.a.Layout.Instance; instance != layout.DefaultInstance {
		args += " --instance " + instance
	}
	if rt.a.Layout.Form == layout.Flatpak {
		return "", fmt.Sprintf("flatpak run --command=%s %s %s --print", name, layout.DesktopID, args)
	}
	return name + " " + args, name + " " + args + " --print"
}

func (rt *router) handlePage(w http.ResponseWriter, r *http.Request) {
	// A browser loads the page at localhost, since YouTube's player won't
	// play for a page at an IP address (M4.1). One that loads it at
	// 127.0.0.1 or [::1] goes there, keeping the path, and the pairing
	// token too: browsers carry a fragment across a redirect. Scripts and
	// tests don't load the page as a document, and get it where they asked.
	if r.Header.Get("Sec-Fetch-Dest") == "document" && !strings.EqualFold("http://"+r.Host, rt.pageOrigin) {
		http.Redirect(w, r, rt.pageOrigin+r.URL.RequestURI(), http.StatusTemporaryRedirect)
		return
	}
	// The page has no header, so its tab names an instance other than the
	// main one, as development and test instances run side by side.
	title := "Dens"
	if instance := rt.a.Layout.Instance; instance != layout.DefaultInstance {
		title += " (" + instance + ")"
	}
	rt.render(w, r, "app.html", rt.pageData(title))
}

func (rt *router) handleSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.View(rt.a.DB)
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	usage := rt.a.Dens.CacheUsage()
	settings := map[string]any{
		"logLevel":               cfg.LogLevel,
		"logLevels":              strings.Split(xlog.ValidLevels, "|"),
		"updateNotifications":    cfg.UpdateNotifications,
		"backgroundUpdateChecks": cfg.BackgroundUpdateChecks,
		"updatesManaged":         rt.a.ChecksForUpdates(),
		"cacheLimit":             usage.Limit,
		"cacheUsed":              usage.Used,
		"voiceKeys":              rt.a.Shortcuts.Keys(),
	}
	// What only the Flatpak has (M9.3): its remembered key, starting at
	// sign-in, and updates through Flatpak.
	if f := rt.a.Flatpak; f != nil {
		settings["remember"] = rt.a.Remembering()
		settings["startAtSignIn"] = f.StartAtSignIn()
		settings["updatesThroughFlatpak"] = true
	}
	writeJSON(w, settings)
}

// handleShortcutsDesktop passes what the member asks of a desktop that owns
// the voice keys to the shortcut helper: to bind them, to show its
// settings for them, or to stop using them (M7.2).
func (rt *router) handleShortcutsDesktop(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Do string `json:"do"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	switch err := rt.a.Shortcuts.Desktop(body.Do); {
	case err == nil:
		writeJSON(w, map[string]bool{"ok": true})
	case errors.Is(err, shortcuts.ErrNoHelper):
		http.Error(w, "Dens's helper isn't running.", http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// handleLogs downloads the newest logs as one text file, for the member to
// send with a problem (M11).
func (rt *router) handleLogs(w http.ResponseWriter, r *http.Request) {
	name, logs, err := rt.a.Logs()
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(logs)
}

// handleClearCache empties the client's cache of files (M6).
func (rt *router) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.ClearCache(); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, rt.a.Dens.CacheUsage())
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
		"denEnabled":  rt.a.Hosting.State().On,
		"locked":      rt.a.Locked(),
	}
	// Only the Flatpak remembers its key, and can start locked (M9.3).
	if rt.a.Layout.Form == layout.Flatpak {
		status["remember"] = rt.a.Remembering()
	}
	if latest, ok := rt.a.LatestUpdate(cfg); ok && cfg.UpdateNotifications {
		status["updateVersion"] = latest
		status["updateCommand"] = rt.a.UpdateCommand()
	}
	// The Flatpak's notice is Flatpak's update monitor's (M9.3), and what
	// the newest build installed says it is, which the page's next look
	// finds once the software center has installed it (M9.4).
	if f := rt.a.Flatpak; f != nil && cfg.UpdateNotifications {
		f.CheckInstalled(r.Context())
		if u := f.Update(); u.Available || u.Installed {
			status["flatpakUpdate"] = u
		}
	}
	writeJSON(w, status)
}

// handleSetPassword sets the first local password. Changing it later needs
// the current one. The Flatpak keeps a copy of its key that opens it
// without the password where the member asks it to remember (M9.3).
func (rt *router) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
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
	if err := rt.a.PasswordChosen(body.Remember); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleUnlock opens a locked Flatpak with the local password, and keeps or
// forgets the copy of its key that opens it without, as the member chose
// (M9.3).
func (rt *router) handleUnlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := rt.a.UnlockWithPassword(r.Context(), body.Password, body.Remember); err != nil {
		passwordError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleRemember turns remembering the Flatpak's key on, after the local
// password, or off, deleting the copy (M9.3).
func (rt *router) handleRemember(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On       bool   `json:"on"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if rt.a.Layout.Form != layout.Flatpak {
		http.Error(w, "only the Flatpak remembers its key", http.StatusBadRequest)
		return
	}
	if err := rt.a.SetRemember(r.Context(), body.Password, body.On); err != nil {
		passwordError(w, r, err)
		return
	}
	writeJSON(w, rt.a.Remembering())
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
	case errors.Is(err, app.ErrLocked):
		http.Error(w, err.Error(), http.StatusLocked)
	default:
		xhttp.Error(r.Context(), w, err)
	}
}

func (rt *router) handleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LogLevel               *string `json:"logLevel"`
		UpdateNotifications    *bool   `json:"updateNotifications"`
		BackgroundUpdateChecks *bool   `json:"backgroundUpdateChecks"`
		CacheLimit             *int64  `json:"cacheLimit"`
		// VoiceKeys replaces the member's keys, all of them (M7).
		VoiceKeys *map[string]types.VoiceKey `json:"voiceKeys"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.CacheLimit != nil {
		if *body.CacheLimit < cache.MinLimit || *body.CacheLimit > cache.MaxLimit || rt.a.Cache == nil {
			http.Error(w, "the cache's size is 512 MiB to 50 GiB", http.StatusBadRequest)
			return
		}
	}
	cfg, err := config.Update(rt.a.DB, func(cfg *types.Configuration) error {
		if body.CacheLimit != nil {
			cfg.CacheLimit = *body.CacheLimit
		}
		if body.VoiceKeys != nil {
			cfg.VoiceKeys = *body.VoiceKeys
		}
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
	if body.LogLevel != nil && rt.a.Layout.Form != layout.Development {
		if err := rt.a.Log.SetLevel(cfg.LogLevel); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
	}
	if body.CacheLimit != nil {
		if err := rt.a.Cache.SetLimit(cfg.CacheLimit); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
	}
	if body.VoiceKeys != nil {
		rt.a.Shortcuts.SetKeys(cfg.VoiceKeys)
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
