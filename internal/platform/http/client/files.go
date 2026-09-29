package client

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (rt *router) mountFiles(r chi.Router) {
	r.Post("/api/dens/{den}/uploads", rt.handleUpload)
	r.Get("/api/dens/{den}/files/{file}", rt.handleFile(false))
	r.Get("/api/dens/{den}/files/{file}/thumb", rt.handleFile(true))
	r.Get("/api/dens/{den}/storage", rt.handleStorage)
}

// handleUpload passes a file from the page to a den. The page sends the
// file's bytes as they are; images lose their metadata on the way.
func (rt *router) handleUpload(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.Header.Get(denproto.HeaderFilename))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "The file's name couldn't be read.")
		return
	}
	if r.ContentLength < 0 {
		jsonError(w, http.StatusLengthRequired, "The upload didn't say how large it is.")
		return
	}
	up, err := rt.a.Dens.Upload(r.Context(), chi.URLParam(r, "den"), name, r.ContentLength, r.Body)
	if err != nil {
		// The page stops an upload when the member takes the file off;
		// nobody is left to tell.
		if r.Context().Err() != nil {
			return
		}
		// A browser still sending the file doesn't read the answer, and
		// takes the connection closing under it for Dens going away. So
		// the rest of the file is read and dropped first, which takes
		// moments on this computer.
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, denproto.MaxFileSize))
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, up)
}

// handleFile serves a file, or its preview, to the page. Only a file that
// turned out to be an image of a kind Dens reads shows inline, as that
// image type; anything else downloads, and never as a type the browser
// would render. The sandbox keeps even a file opened on its own from
// running anything, and no-store keeps it out of the browser's cache.
func (rt *router) handleFile(thumb bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := rt.a.Dens.OpenFile(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "file"), thumb)
		if err != nil {
			if r.Context().Err() != nil {
				return // the page moved on, as it does when a message scrolls away
			}
			rt.denError(w, r, err)
			return
		}
		defer f.Close()
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("Content-Length", strconv.FormatInt(f.Size, 10))
		name, download := r.URL.Query()["download"]
		if f.Kind.Image() && !download {
			h.Set("Content-Type", f.Kind.MIME())
			h.Set("Content-Disposition", "inline")
		} else {
			h.Set("Content-Type", "application/octet-stream")
			disposition := "attachment"
			if len(name) > 0 {
				if d := mime.FormatMediaType("attachment", map[string]string{"filename": denproto.CleanFilename(name[0])}); d != "" {
					disposition = d
				}
			}
			h.Set("Content-Disposition", disposition)
		}
		_, _ = io.Copy(w, f)
	}
}

func (rt *router) handleStorage(w http.ResponseWriter, r *http.Request) {
	st, err := rt.a.Dens.Storage(r.Context(), chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, st)
}
