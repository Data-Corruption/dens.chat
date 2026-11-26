package client

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/steam"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/go-chi/chi/v5"
)

type steamClient interface {
	Card(ctx context.Context, app string) (steam.Card, error)
	Header(ctx context.Context, app string) ([]byte, error)
	Shot(ctx context.Context, app string, n int) ([]byte, error)
	Trailer(ctx context.Context, app, encoding string) (steam.Trailer, error)
	TrailerInit(ctx context.Context, app, encoding, rep string) ([]byte, string, error)
	TrailerSegment(ctx context.Context, app, encoding, rep string, seg int) ([]byte, string, error)
}

// mountSteam serves Steam's cards: a linked app's details, its
// header picture, its first screenshots, and on click its primary
// trailer's manifest and segments. The page asks only while a member has
// the cards on, and only for cards on screen.
func (rt *router) mountSteam(r chi.Router) {
	r.Get("/api/steam/{app}", rt.handleSteamCard)
	r.Get("/api/steam/{app}/header", rt.handleSteamHeader)
	r.Get("/api/steam/{app}/shots/{n}", rt.handleSteamShot)
	r.Get("/api/steam/{app}/trailer/{encoding}", rt.handleSteamTrailer)
	r.Get("/api/steam/{app}/trailer/{encoding}/{rep}/init", rt.handleSteamTrailerInit)
	r.Get("/api/steam/{app}/trailer/{encoding}/{rep}/{seg}", rt.handleSteamTrailerSegment)
}

func (rt *router) handleSteamHeader(w http.ResponseWriter, r *http.Request) {
	steamPicture(rt.steam.Header)(w, r)
}

func (rt *router) handleSteamCard(w http.ResponseWriter, r *http.Request) {
	c, err := rt.steam.Card(r.Context(), chi.URLParam(r, "app"))
	if err != nil {
		steamError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, c)
}

// shots are the screenshots a card steps through, by their number there.
var shots = map[string]int{"1": 1, "2": 2, "3": 3, "4": 4}

func (rt *router) handleSteamShot(w http.ResponseWriter, r *http.Request) {
	n, ok := shots[chi.URLParam(r, "n")]
	if !ok {
		jsonError(w, http.StatusNotFound, "A card shows four screenshots at most.")
		return
	}
	steamPicture(func(ctx context.Context, app string) ([]byte, error) { return rt.steam.Shot(ctx, app, n) })(w, r)
}

// steamPicture serves an app's picture as YouTube's covers' are: inline
// only as the JPEG it was checked to be, sandboxed, and kept out of the
// browser's cache, which would otherwise record what games a member's
// chats link to.
func steamPicture(picture func(context.Context, string) ([]byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jpeg, err := picture(r.Context(), chi.URLParam(r, "app"))
		if err != nil {
			steamError(w, r, err)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("Content-Type", "image/jpeg")
		h.Set("Content-Disposition", "inline")
		_, _ = w.Write(jpeg)
	}
}

func (rt *router) handleSteamTrailer(w http.ResponseWriter, r *http.Request) {
	tr, err := rt.steam.Trailer(r.Context(), chi.URLParam(r, "app"), chi.URLParam(r, "encoding"))
	if err != nil {
		steamError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, tr)
}

func (rt *router) handleSteamTrailerInit(w http.ResponseWriter, r *http.Request) {
	b, mimeType, err := rt.steam.TrailerInit(r.Context(), chi.URLParam(r, "app"), chi.URLParam(r, "encoding"), chi.URLParam(r, "rep"))
	if err != nil {
		steamError(w, r, err)
		return
	}
	writeTrailerBytes(w, b, mimeType)
}

func (rt *router) handleSteamTrailerSegment(w http.ResponseWriter, r *http.Request) {
	seg, ok := parsePositiveInt(chi.URLParam(r, "seg"))
	if !ok {
		jsonError(w, http.StatusNotFound, "Steam doesn't have this trailer segment.")
		return
	}
	b, mimeType, err := rt.steam.TrailerSegment(r.Context(), chi.URLParam(r, "app"), chi.URLParam(r, "encoding"), chi.URLParam(r, "rep"), seg)
	if err != nil {
		steamError(w, r, err)
		return
	}
	writeTrailerBytes(w, b, mimeType)
}

func parsePositiveInt(s string) (int, bool) {
	if len(s) == 0 || len(s) > 6 || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}

// writeTrailerBytes serves a checked ISO-BMFF trailer init or media chunk
// sandboxed and kept out of the browser's cache.
func writeTrailerBytes(w http.ResponseWriter, b []byte, mimeType string) {
	if mimeType != "video/mp4" && mimeType != "audio/mp4" {
		mimeType = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Type", mimeType)
	h.Set("Content-Disposition", "inline")
	_, _ = w.Write(b)
}

// steamError tells the page why a card has nothing to show. The log gets
// what went wrong, never the app, which comes from a message.
func steamError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case r.Context().Err() != nil:
		// The card scrolled away, or the page moved on.
	case errors.Is(err, steam.ErrBadID):
		jsonError(w, http.StatusBadRequest, "That isn't a Steam app.")
	case errors.Is(err, steam.ErrNotFound):
		jsonError(w, http.StatusNotFound, "Steam doesn't have this app.")
	default:
		xlog.Debugf(r.Context(), "steam card: %v", err)
		jsonError(w, http.StatusBadGateway, "Steam couldn't be reached.")
	}
}
