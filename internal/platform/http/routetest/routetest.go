// Package routetest records the routes given to a chi router, for the
// routers' tests to fail on one registered twice: chi keeps the last
// handler for a method and path, dropping the first without a word, and
// takes /x/{id} and /x/{key} for the same path.
package routetest

import (
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// anyMethod stands for a route that answers every method, as Handle's
// and Mount's do, which takes the place of any other on its path.
const anyMethod = "*"

// Recorder is a chi.Router that notes each route it's given, with the
// path of every Route around it, before handing it on.
type Recorder struct {
	chi.Router
	prefix string
	seen   *seen
}

type seen struct {
	methods map[string][]string
	twice   []string
}

// New wraps r.
func New(r chi.Router) *Recorder {
	return &Recorder{Router: r, seen: &seen{methods: map[string][]string{}}}
}

// Twice returns each route given more than once, as "METHOD /path".
func (rc *Recorder) Twice() []string { return rc.seen.twice }

var param = regexp.MustCompile(`\{[^}:]*`)

func (rc *Recorder) add(method, pattern string) {
	path := param.ReplaceAllString(rc.prefix+pattern, "{")
	methods := rc.seen.methods[path]
	if slices.Contains(methods, method) || slices.Contains(methods, anyMethod) || (method == anyMethod && len(methods) > 0) {
		rc.seen.twice = append(rc.seen.twice, method+" "+rc.prefix+pattern)
	}
	rc.seen.methods[path] = append(methods, method)
}

func (rc *Recorder) wrap(r chi.Router, prefix string) *Recorder {
	return &Recorder{Router: r, prefix: prefix, seen: rc.seen}
}

func (rc *Recorder) With(middlewares ...func(http.Handler) http.Handler) chi.Router {
	return rc.wrap(rc.Router.With(middlewares...), rc.prefix)
}

func (rc *Recorder) Group(fn func(r chi.Router)) chi.Router {
	im := rc.With()
	if fn != nil {
		fn(im)
	}
	return im
}

// Route builds the subrouter as chi's does, through a Recorder, so its
// routes are noted under the pattern it's mounted at.
func (rc *Recorder) Route(pattern string, fn func(r chi.Router)) chi.Router {
	if fn == nil {
		return rc.Router.Route(pattern, fn)
	}
	sub := rc.wrap(chi.NewRouter(), rc.prefix+pattern)
	fn(sub)
	rc.Router.Mount(pattern, sub.Router)
	return sub
}

func (rc *Recorder) Mount(pattern string, h http.Handler) {
	rc.add(anyMethod, strings.TrimSuffix(pattern, "/")+"/*")
	rc.Router.Mount(pattern, h)
}

func (rc *Recorder) Handle(pattern string, h http.Handler) {
	rc.add(anyMethod, pattern)
	rc.Router.Handle(pattern, h)
}

func (rc *Recorder) HandleFunc(pattern string, h http.HandlerFunc) {
	rc.add(anyMethod, pattern)
	rc.Router.HandleFunc(pattern, h)
}

func (rc *Recorder) Method(method, pattern string, h http.Handler) {
	rc.add(strings.ToUpper(method), pattern)
	rc.Router.Method(method, pattern, h)
}

func (rc *Recorder) MethodFunc(method, pattern string, h http.HandlerFunc) {
	rc.add(strings.ToUpper(method), pattern)
	rc.Router.MethodFunc(method, pattern, h)
}

func (rc *Recorder) Connect(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodConnect, pattern)
	rc.Router.Connect(pattern, h)
}

func (rc *Recorder) Delete(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodDelete, pattern)
	rc.Router.Delete(pattern, h)
}

func (rc *Recorder) Get(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodGet, pattern)
	rc.Router.Get(pattern, h)
}

func (rc *Recorder) Head(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodHead, pattern)
	rc.Router.Head(pattern, h)
}

func (rc *Recorder) Options(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodOptions, pattern)
	rc.Router.Options(pattern, h)
}

func (rc *Recorder) Patch(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodPatch, pattern)
	rc.Router.Patch(pattern, h)
}

func (rc *Recorder) Post(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodPost, pattern)
	rc.Router.Post(pattern, h)
}

func (rc *Recorder) Put(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodPut, pattern)
	rc.Router.Put(pattern, h)
}

func (rc *Recorder) Trace(pattern string, h http.HandlerFunc) {
	rc.add(http.MethodTrace, pattern)
	rc.Router.Trace(pattern, h)
}
