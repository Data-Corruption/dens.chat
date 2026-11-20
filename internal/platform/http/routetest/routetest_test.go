package routetest

import (
	"net/http"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestTwice(t *testing.T) {
	ok := func(http.ResponseWriter, *http.Request) {}
	rc := New(chi.NewRouter())
	rc.Get("/a", ok)
	rc.Post("/a", ok)
	rc.Get("/m/{id}", ok)
	rc.Group(func(r chi.Router) {
		r.Get("/a", ok)
		r.With().Delete("/m/{key}", ok)
		r.Patch("/m/{key}", ok)
		r.Get("/m/{key}", ok)
	})
	rc.Route("/api", func(r chi.Router) {
		r.Get("/ws", ok)
		r.Group(func(r chi.Router) { r.Put("/x/{n:[0-9]+}", ok) })
	})
	rc.Get("/api/ws", ok)
	rc.Put("/api/x/{n}", ok)
	rc.Get("/h", ok)
	rc.Handle("/h", http.HandlerFunc(ok))

	want := []string{"GET /a", "GET /m/{key}", "GET /api/ws", "* /h"}
	if got := rc.Twice(); !slices.Equal(got, want) {
		t.Fatalf("Twice() = %q, want %q", got, want)
	}
}
