package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/background"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"
	"github.com/go-chi/chi/v5"
)

// mountBackground mounts the picture behind the member's chat, which stays
// on this install as their own sounds do: only this router serves it, and
// writes pass the Origin check every write does.
func (rt *router) mountBackground(r chi.Router) {
	r.Get("/api/background", rt.handleBackground)
	r.Post("/api/background", rt.handleSetBackground)
	r.Delete("/api/background", rt.handleRemoveBackground)
}

// backgroundView is what the page learns of the chat's background with the
// dens view, so every page paired with the install shows the same: its
// version, which the page asks for the file by, and its dim in percent.
type backgroundView struct {
	Version int64 `json:"version"`
	Dim     int   `json:"dim"`
}

// backgroundView returns the chat's background, or nil where there's none.
func (rt *router) backgroundView(ctx context.Context) (*backgroundView, error) {
	version, err := background.Version(ctx, rt.a.DB)
	if err != nil || version == 0 {
		return nil, err
	}
	cfg, err := config.View(rt.a.DB)
	if err != nil {
		return nil, err
	}
	v := &backgroundView{Version: version, Dim: types.BackgroundDefaultDim}
	if cfg.BackgroundDim != nil {
		v.Dim = *cfg.BackgroundDim
	}
	return v, nil
}

// handleBackground serves the chat's background, which the page draws
// behind its messages: as the image it is, sandboxed, and kept out of the
// browser's cache, as a den's files are.
func (rt *router) handleBackground(w http.ResponseWriter, r *http.Request) {
	file, err := background.Open(r.Context(), rt.a.DB, rt.a.Vault)
	switch {
	case errors.Is(err, background.ErrNone):
		jsonError(w, http.StatusNotFound, "The chat has no background.")
		return
	case err != nil:
		xhttp.Error(r.Context(), w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	if k := media.Sniff(file[:min(len(file), media.SniffLen)]); k == media.PNG || k == media.JPEG || k == media.WebP {
		h.Set("Content-Type", k.MIME())
	}
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(file))
}

// handleSetBackground keeps a picture of the member's behind their chat: a
// still PNG, JPEG or WebP of at most 10 MB, with everything but the
// picture taken out of it, as a file bound for a den has.
func (rt *router) handleSetBackground(w http.ResponseWriter, r *http.Request) {
	const tooLarge = "A background is at most 10 MB."
	if r.ContentLength > background.MaxSize {
		jsonError(w, http.StatusRequestEntityTooLarge, tooLarge)
		return
	}
	file, err := io.ReadAll(http.MaxBytesReader(w, r.Body, background.MaxSize))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			jsonError(w, http.StatusRequestEntityTooLarge, tooLarge)
			return
		}
		jsonError(w, http.StatusBadRequest, "The file didn't arrive whole.")
		return
	}
	const wrongKind = "A background is a PNG, a JPEG or a WebP."
	k := media.Sniff(file[:min(len(file), media.SniffLen)])
	if k != media.PNG && k != media.JPEG && k != media.WebP {
		jsonError(w, http.StatusUnsupportedMediaType, wrongKind)
		return
	}
	var stripped bytes.Buffer
	res, err := media.Strip(&stripped, bytes.NewReader(file), k)
	switch {
	case errors.Is(err, media.ErrMalformed):
		jsonError(w, http.StatusUnsupportedMediaType, "That file is damaged, or isn't the kind of image it starts out as.")
		return
	case err != nil:
		xhttp.Error(r.Context(), w, err)
		return
	case res.Animated:
		jsonError(w, http.StatusUnsupportedMediaType, "A background is a still picture.")
		return
	}
	if _, err := background.Put(r.Context(), rt.a.DB, rt.a.Vault, stripped.Bytes(), time.Now()); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.a.Log.Info("The member set a picture behind their chat")
	rt.backgroundChanged(w, r)
}

// handleRemoveBackground drops the chat's background.
func (rt *router) handleRemoveBackground(w http.ResponseWriter, r *http.Request) {
	if err := background.Delete(r.Context(), rt.a.DB); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.a.Log.Info("The member removed the picture behind their chat")
	rt.backgroundChanged(w, r)
}

// backgroundChanged tells every page the background, with the dens view,
// and answers with it.
func (rt *router) backgroundChanged(w http.ResponseWriter, r *http.Request) {
	rt.a.Dens.Changed()
	v, err := rt.backgroundView(r.Context())
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, map[string]*backgroundView{"background": v})
}
