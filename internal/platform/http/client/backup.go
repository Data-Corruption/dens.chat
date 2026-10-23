package client

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/pkg/xhttp"

	"github.com/go-chi/chi/v5"
)

// downloadFor is how long the link to a backup's archive lasts.
const downloadFor = time.Minute

// backups are the links the page downloads backups from (M9.3): one for
// each local password the member typed, which works once, and soon goes.
// The archive is written as it downloads, so the browser saves it as it
// does any download, rather than the page holding it.
type backups struct {
	mu    sync.Mutex
	links map[string]time.Time
}

func (b *backups) issue() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for t, expires := range b.links {
		if now.After(expires) {
			delete(b.links, t)
		}
	}
	b.links[token] = now.Add(downloadFor)
	return token, nil
}

func (b *backups) redeem(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	expires, ok := b.links[token]
	delete(b.links, token)
	return ok && time.Now().Before(expires)
}

func (rt *router) mountBackups(r chi.Router) {
	r.Get("/api/backup", rt.handleBackupSize)
	r.Post("/api/backup", rt.handleBackup)
	r.Get("/api/backup/{token}", rt.handleDownloadBackup)
}

// handleBackupSize says at least how large a backup made now would be,
// which the page shows before the member makes one (M10).
func (rt *router) handleBackupSize(w http.ResponseWriter, r *http.Request) {
	size, err := rt.a.BackupSize()
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, map[string]int64{"size": size})
}

// handleBackup takes the local password, the re-authentication a backup
// needs, since its archive carries the data key sealed under it, and gives
// the page the link to download it from.
func (rt *router) handleBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := rt.a.VerifyPassword(r.Context(), body.Password); err != nil {
		passwordError(w, r, err)
		return
	}
	token, err := rt.backups.issue()
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, map[string]string{"url": "/api/backup/" + token})
}

// handleDownloadBackup streams the archive dens backup writes, as a
// download.
func (rt *router) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if !rt.backups.redeem(chi.URLParam(r, "token")) {
		http.Error(w, "This backup's link has expired; make the backup again.", http.StatusGone)
		return
	}
	archive, err := rt.a.Backup(r.Context())
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	defer archive.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": archive.Name}))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, archive); err != nil {
		rt.a.Log.Warnf("The backup's download didn't finish: %v", err)
		// Ending the response would leave what went looking whole in the
		// browser's downloads; dropping the connection marks it failed
		// (M10).
		panic(http.ErrAbortHandler)
	}
}
