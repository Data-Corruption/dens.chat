// Package den is the router of the den listener, which Caddy exposes to the
// internet. It serves only den routes; the client page and its API are never
// mounted here.
package den

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/go-chi/chi/v5"
)

// New returns the den listener's handler.
func New(a *app.App) http.Handler {
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
	return r
}
