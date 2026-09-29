package den

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"

	"github.com/go-chi/chi/v5"
)

// uploadIdle is how long an upload may send nothing before it's dropped.
// A slow upload is fine; a stalled one would hold its place forever.
const uploadIdle = time.Minute

func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	if err := h.d.Allow(dens.LimitUpload, strconv.FormatInt(s.MemberID, 10)); err != nil {
		h.fail(w, r, err)
		return
	}
	name, err := url.PathUnescape(r.Header.Get(denproto.HeaderFilename))
	if err != nil {
		h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "%s must be percent-encoded", denproto.HeaderFilename))
		return
	}
	body := &idleReader{r: r.Body, rc: http.NewResponseController(w)}
	f, err := h.d.Upload(r.Context(), s, name, r.ContentLength, body)
	if err != nil {
		if body.err != nil {
			// The client stopped sending: it went away, or took the file
			// off. That's the client's business, not the den's failure.
			h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeMalformed, "the upload stopped before its end"))
			return
		}
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusCreated, f)
}

// idleReader gives each read of a request body uploadIdle to arrive, and
// remembers how reading it failed.
type idleReader struct {
	r   io.Reader
	rc  *http.ResponseController
	err error
}

func (i *idleReader) Read(p []byte) (int, error) {
	_ = i.rc.SetReadDeadline(time.Now().Add(uploadIdle))
	n, err := i.r.Read(p)
	if err != nil && err != io.EOF {
		i.err = err
	}
	return n, err
}

// file serves a file, or its preview, as bytes to download: the den never
// says what a file is in a way a browser would act on, and clients check
// what they got before showing anything.
func (h *handler) file(thumb bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, size, err := h.d.OpenFile(r.Context(), session(r), chi.URLParam(r, "id"), thumb)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		defer rc.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.Header().Set("Cache-Control", "no-store")
		if _, err := io.Copy(w, rc); errors.Is(err, vault.ErrSealed) {
			// The response is cut short, so the client notices too.
			h.log.Errorf("serve file %s: %v", chi.URLParam(r, "id"), err)
		}
	}
}

func (h *handler) storage(w http.ResponseWriter, r *http.Request) {
	st, err := h.d.Storage(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, st)
}
