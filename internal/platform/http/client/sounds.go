package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/sounds"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/internal/ui"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"
	"github.com/go-chi/chi/v5"
)

// mountSounds mounts the member's own sounds (M14.2): files of theirs in
// place of the page's sounds, which stay on this install, so only this
// router serves them, and writes pass the Origin check every write does.
func (rt *router) mountSounds(r chi.Router) {
	r.Get("/api/sounds/{name}", rt.handleSound)
	r.Post("/api/sounds/{name}", rt.handleReplaceSound)
	r.Delete("/api/sounds/{name}", rt.handleResetSound)
}

// soundAssets are the page's own sounds, by name, at their assets' addresses,
// as JSON for the page's shell; the ones the build left out aren't there.
func soundAssets(pages *ui.UI) string {
	paths := map[string]string{}
	if pages != nil {
		for _, name := range types.SoundNames {
			if a := pages.Assets["sounds/"+name+".ogg"]; a != nil {
				paths[name] = a.URLPath
			}
		}
	}
	data, _ := json.Marshal(paths)
	return string(data)
}

// soundName is the sound a request names, or "", with the answer written,
// for one the page doesn't have.
func soundName(w http.ResponseWriter, r *http.Request) string {
	name := chi.URLParam(r, "name")
	if !slices.Contains(types.SoundNames, name) {
		jsonError(w, http.StatusNotFound, "There's no such sound.")
		return ""
	}
	return name
}

// handleSound serves the member's own file for a sound, which the page
// plays: as the audio it is, sandboxed, and kept out of the browser's cache,
// as a den's files are.
func (rt *router) handleSound(w http.ResponseWriter, r *http.Request) {
	name := soundName(w, r)
	if name == "" {
		return
	}
	file, _, err := sounds.Open(r.Context(), rt.a.DB, rt.a.Vault, name)
	switch {
	case errors.Is(err, sounds.ErrNone):
		jsonError(w, http.StatusNotFound, "That sound plays its default.")
		return
	case err != nil:
		xhttp.Error(r.Context(), w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	if t := media.PlayType(file[:min(len(file), media.SniffLen)]); t != "" {
		h.Set("Content-Type", t)
	}
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(file))
}

// handleReplaceSound keeps a file of the member's in place of one of the
// page's sounds: audio, as internal/media names it, of at most 1 MiB, which
// the service checks. The page has checked that it plays, and lasts at
// most 10 seconds, which takes decoding it.
func (rt *router) handleReplaceSound(w http.ResponseWriter, r *http.Request) {
	name := soundName(w, r)
	if name == "" {
		return
	}
	const tooLarge = "A sound's file is at most 1 MiB."
	if r.ContentLength > sounds.MaxSize {
		jsonError(w, http.StatusRequestEntityTooLarge, tooLarge)
		return
	}
	file, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sounds.MaxSize))
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
	if media.Sniff(file[:min(len(file), media.SniffLen)]) != media.Audio {
		jsonError(w, http.StatusUnsupportedMediaType, "That file isn't audio Dens can play.")
		return
	}
	version, err := sounds.Put(r.Context(), rt.a.DB, rt.a.Vault, name, file, time.Now())
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.a.Notify.SetOwn(name, version)
	rt.a.Log.Infof("The member replaced the %s sound with a file of their own", name)
	writeJSON(w, rt.a.Notify.Status())
}

// handleResetSound drops the member's own file for a sound, which brings its
// default back.
func (rt *router) handleResetSound(w http.ResponseWriter, r *http.Request) {
	name := soundName(w, r)
	if name == "" {
		return
	}
	if err := sounds.Delete(r.Context(), rt.a.DB, name); err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.a.Notify.SetOwn(name, 0)
	rt.a.Log.Infof("The member put the %s sound's default back", name)
	writeJSON(w, rt.a.Notify.Status())
}
