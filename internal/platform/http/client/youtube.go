package client

import (
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/youtube"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/go-chi/chi/v5"
)

// mountYouTube serves the covers of YouTube's players (M4.1): a linked
// video's title and channel, and its picture. The page asks only while a
// member has players on, and only for covers on screen.
func (rt *router) mountYouTube(r chi.Router) {
	r.Get("/api/youtube/{id}", rt.handleVideo)
	r.Get("/api/youtube/{id}/thumb", rt.handleVideoThumb)
}

func (rt *router) handleVideo(w http.ResponseWriter, r *http.Request) {
	v, err := rt.youTube.Video(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		videoError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, v)
}

// handleVideoThumb serves a video's picture as the page's files are
// served: inline only as the JPEG it was checked to be, sandboxed, and kept
// out of the browser's cache, which would otherwise record what videos a
// member's chats link to.
func (rt *router) handleVideoThumb(w http.ResponseWriter, r *http.Request) {
	jpeg, err := rt.youTube.Picture(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		videoError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Disposition", "inline")
	_, _ = w.Write(jpeg)
}

// videoError tells the page why a cover has nothing to show. The log gets
// what went wrong, never the video, which comes from a message.
func videoError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case r.Context().Err() != nil:
		// The cover scrolled away, or the page moved on.
	case errors.Is(err, youtube.ErrBadID):
		jsonError(w, http.StatusBadRequest, "That isn't a YouTube video.")
	case errors.Is(err, youtube.ErrNotFound):
		jsonError(w, http.StatusNotFound, "YouTube doesn't have this video.")
	default:
		xlog.Debugf(r.Context(), "youtube cover: %v", err)
		jsonError(w, http.StatusBadGateway, "YouTube couldn't be reached.")
	}
}
