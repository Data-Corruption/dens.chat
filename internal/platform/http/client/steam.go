package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/steam"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/go-chi/chi/v5"
)

// mountSteam serves Steam's cards: a linked app's details, its
// header picture and its first screenshots. The page asks only while a
// member has the cards on, and only for cards on screen.
func (rt *router) mountSteam(r chi.Router) {
	r.Get("/api/steam/{app}", rt.handleSteamCard)
	r.Get("/api/steam/{app}/header", steamPicture(rt.steam.Header))
	r.Get("/api/steam/{app}/shots/{n}", rt.handleSteamShot)
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
