package client

import (
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/routetest"

	"github.com/go-chi/chi/v5"
)

// TestNoRouteTwice: chi drops the first of two routes on one method and
// path without a word.
func TestNoRouteTwice(t *testing.T) {
	r := routetest.New(chi.NewRouter())
	(&router{a: &app.App{}}).routes(r)
	for _, route := range r.Twice() {
		t.Errorf("%s is registered twice", route)
	}
}
