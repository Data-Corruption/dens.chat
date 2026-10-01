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

// upload stores an upload: a file to check, or with sealed, a DM's file
// the den can't open, which comes without a name.
func (h *handler) upload(sealed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := session(r)
		if err := h.d.Allow(dens.LimitUpload, strconv.FormatInt(s.MemberID, 10)); err != nil {
			h.fail(w, r, err)
			return
		}
		var name string
		if !sealed {
			var err error
			if name, err = url.PathUnescape(r.Header.Get(denproto.HeaderFilename)); err != nil {
				h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeInvalidField, "%s must be percent-encoded", denproto.HeaderFilename))
				return
			}
		}
		body := &idleReader{r: r.Body, rc: http.NewResponseController(w)}
		f, err := h.d.Upload(r.Context(), s, name, r.ContentLength, body, sealed)
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

// setThumb takes the preview a member's page made for a video the den
// can't make one for.
func (h *handler) setThumb(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	if err := h.d.Allow(dens.LimitUpload, strconv.FormatInt(s.MemberID, 10)); err != nil {
		h.fail(w, r, err)
		return
	}
	body := &idleReader{r: r.Body, rc: http.NewResponseController(w)}
	f, err := h.d.SetThumb(r.Context(), s, chi.URLParam(r, "id"), r.ContentLength, body)
	if err != nil {
		if body.err != nil {
			h.fail(w, r, denproto.Errorf(http.StatusBadRequest, denproto.CodeMalformed, "the upload stopped before its end"))
			return
		}
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, f)
}

// file serves a file, or its preview, as bytes to download: the den never
// says what a file is in a way a browser would act on, and clients check
// what they got before showing anything. It answers byte ranges, opening
// only the chunks a range covers, so a video plays from wherever its player
// seeks.
func (h *handler) file(thumb bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		f, err := h.d.OpenFile(r.Context(), session(r), id, thumb)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Cache-Control", "no-store")
		// A file's bytes never change under its ID.
		tag := `"` + id + `"`
		if thumb {
			tag = `"` + id + `.thumb"`
		}
		w.Header().Set("ETag", tag)
		rs := &readWatch{ReadSeeker: f}
		http.ServeContent(w, r, "", time.Time{}, rs)
		if errors.Is(rs.err, vault.ErrSealed) {
			// The response is cut short, so the client notices too.
			h.log.Errorf("serve file %s: %v", id, rs.err)
		}
	}
}

// readWatch remembers how reading failed, which http.ServeContent doesn't
// say.
type readWatch struct {
	io.ReadSeeker
	err error
}

func (r *readWatch) Read(p []byte) (int, error) {
	n, err := r.ReadSeeker.Read(p)
	if err != nil && err != io.EOF && r.err == nil {
		r.err = err
	}
	return n, err
}

func (h *handler) storage(w http.ResponseWriter, r *http.Request) {
	st, err := h.d.Storage(r.Context(), session(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	denproto.WriteJSON(w, http.StatusOK, st)
}
