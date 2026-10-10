package client

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

func (rt *router) mountFiles(r chi.Router) {
	r.Post("/api/dens/{den}/uploads", rt.handleUpload)
	r.Get("/api/dens/{den}/files/{file}", rt.handleFile(false))
	r.Get("/api/dens/{den}/files/{file}/thumb", rt.handleFile(true))
	r.Post("/api/dens/{den}/uploads/{file}/thumb", rt.handleSetThumb)
	r.Get("/api/dens/{den}/uploads/{file}/versions/{version}", rt.handleVersion)
	r.Post("/api/dens/{den}/uploads/{file}/switch", rt.handleSwitchVersion)
	r.Get("/api/dens/{den}/progress/{key}", rt.handleFollowUpload)
	r.Post("/api/dens/{den}/progress/{key}/full", rt.handleSendFullSize)
	r.Get("/api/dens/{den}/storage", rt.handleStorage)
	r.Get("/api/dens/{den}/files", rt.handleOwnFiles)
	r.Delete("/api/dens/{den}/uploads/{file}", rt.handleDropUpload)
	r.Post("/api/dens/{den}/messages/{message}/files/{file}/remove", rt.handleRemoveFile)
	r.Post("/api/dens/{den}/messages/{message}/files/{file}/swap", rt.handleSwapFile)
}

// handleUpload passes a file from the page to a den. The page sends the
// file's bytes as they are; images lose their metadata on the way. The
// page names the channel the file is for, since a DM's goes sealed, and
// the file it replaces, if any (M5). A file for a message says which
// version of a photo or video it sends, smaller or full, and it then goes
// with both kept for the page to compare (M5), and names the key the page
// follows its progress by while a video's copy is made (M5.4).
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
	q := r.URL.Query()
	den, channel, replaces, send := chi.URLParam(r, "den"), q.Get("channel"), q.Get("replaces"), q.Get("send")
	var up denclient.Uploaded
	switch {
	case send != "":
		up, err = rt.a.Dens.UploadVersions(r.Context(), den, channel, name, r.ContentLength, r.Body, denclient.UploadOptions{
			Send: denclient.Send(send), Replaces: replaces, Progress: q.Get("progress"), PageCopies: q.Get("copier") == "page"})
	case replaces != "":
		up, err = rt.a.Dens.Replace(r.Context(), den, channel, replaces, name, r.ContentLength, r.Body)
	default:
		up, err = rt.a.Dens.Upload(r.Context(), den, channel, name, r.ContentLength, r.Body)
	}
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

// handleSetThumb passes the preview the page drew for a video it uploaded,
// which the media module couldn't make one for.
func (rt *router) handleSetThumb(w http.ResponseWriter, r *http.Request) {
	up, err := rt.a.Dens.SetThumb(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "file"), r.ContentLength, r.Body)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, up)
}

// handleFile serves a file, or its preview, to the page. Only a file that
// turned out to be an image of a kind Dens reads, or video or audio in a
// container the media module writes, shows inline, as that type; anything
// else downloads, and never as a type the browser would render. Byte
// ranges let a player seek. The sandbox keeps even a file opened on its own
// from running anything, and no-store keeps it out of the browser's cache.
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
		serveFile(w, r, f)
	}
}

// handleVersion serves one of the two versions kept of a photo or video
// waiting to be sent, smaller or full, for the page to compare them (M5).
// Each is a JPEG, a PNG or an MP4 this service made or stripped, served as
// any file is, with byte ranges for a player.
func (rt *router) handleVersion(w http.ResponseWriter, r *http.Request) {
	f, err := rt.a.Dens.OpenVersion(chi.URLParam(r, "den"), chi.URLParam(r, "file"), denclient.Send(chi.URLParam(r, "version")))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	defer f.Close()
	serveFile(w, r, f)
}

// handleSwitchVersion switches a photo or video waiting to be sent to its
// other version, and answers with the upload that takes its place (M5).
func (rt *router) handleSwitchVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To string `json:"to"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	up, err := rt.a.Dens.SwitchVersion(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "file"), denclient.Send(body.To))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, up)
}

// handleFollowUpload serves the socket of the page following an upload by
// its key: how far the upload has come, making a video's smaller copy and
// sending it to the den (M5.4), and in Chrome and Edge, the copy the page
// makes itself (M5.5).
func (rt *router) handleFollowUpload(w http.ResponseWriter, r *http.Request) {
	// Accept checks the Origin against the Host, which the Host guard has
	// already pinned to this listener, so other sites can't open it.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(denclient.MaxPageMessage)
	if err := rt.a.Dens.FollowUpload(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "key"), pageSocket{c}); err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "no such upload to follow")
		return
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// pageSocket is the page's end of an upload it follows, as denclient reads
// and writes it.
type pageSocket struct{ c *websocket.Conn }

func (s pageSocket) Read(ctx context.Context) (bool, []byte, error) {
	typ, data, err := s.c.Read(ctx)
	return typ == websocket.MessageText, data, err
}

func (s pageSocket) Write(ctx context.Context, text bool, data []byte) error {
	typ := websocket.MessageBinary
	if text {
		typ = websocket.MessageText
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.c.Write(ctx, typ, data)
}

// handleSendFullSize has an upload making a video's copy send the video
// full size instead, which the upload then answers with (M5.4).
func (rt *router) handleSendFullSize(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.SendFullSize(chi.URLParam(r, "den"), chi.URLParam(r, "key")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// serveFile serves a file to the page: inline, as exactly what it is, only
// when that's an image of a kind Dens reads, or video or audio in a
// container the media module writes, and otherwise as a download.
func serveFile(w http.ResponseWriter, r *http.Request, f *denclient.OpenedFile) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	name, download := r.URL.Query()["download"]
	var inline string
	switch {
	case download:
	case f.Kind.Image():
		inline = f.Kind.MIME()
	default:
		inline = f.PlayType()
	}
	if inline != "" {
		h.Set("Content-Type", inline)
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
	http.ServeContent(w, r, "", time.Time{}, f)
}

func (rt *router) handleStorage(w http.ResponseWriter, r *http.Request) {
	st, err := rt.a.Dens.Storage(r.Context(), chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, st)
}

// handleOwnFiles lists a page of this member's files on a den, largest
// first (M5).
func (rt *router) handleOwnFiles(w http.ResponseWriter, r *http.Request) {
	page, err := rt.a.Dens.Files(r.Context(), chi.URLParam(r, "den"), r.URL.Query().Get("after"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, page)
}

// handleDropUpload deletes an upload waiting to be sent, which the page
// took off the composer, so its space is free at once.
func (rt *router) handleDropUpload(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DropUpload(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "file")); err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleRemoveFile takes one of this member's files off its message.
func (rt *router) handleRemoveFile(w http.ResponseWriter, r *http.Request) {
	err := rt.a.Dens.RemoveFile(r.Context(), chi.URLParam(r, "den"), r.URL.Query().Get("channel"), chi.URLParam(r, "message"),
		chi.URLParam(r, "file"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSwapFile puts an upload made to replace one of this member's files
// in that file's place.
func (rt *router) handleSwapFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Upload string `json:"upload"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	err := rt.a.Dens.SwapFile(r.Context(), chi.URLParam(r, "den"), r.URL.Query().Get("channel"), chi.URLParam(r, "message"),
		chi.URLParam(r, "file"), body.Upload)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
