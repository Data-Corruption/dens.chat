package client

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"

	"github.com/go-chi/chi/v5"
)

// downloadFor is how long the link to a backup's archive lasts.
const downloadFor = time.Minute

// backups are the links the page downloads backups from (M9.3), and the
// den's export (M10): one for each local password the member typed, which
// works once, and soon goes. The archive is written as it downloads, so
// the browser saves it as it does any download, rather than the page
// holding it.
type backups struct {
	mu    sync.Mutex
	links map[string]backupLink
}

// backupLink is when a link stops working, and whether it's to the den's
// export rather than a backup.
type backupLink struct {
	expires time.Time
	export  bool
}

func (b *backups) issue(export bool) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for t, link := range b.links {
		if now.After(link.expires) {
			delete(b.links, t)
		}
	}
	b.links[token] = backupLink{expires: now.Add(downloadFor), export: export}
	return token, nil
}

func (b *backups) redeem(token string) (link backupLink, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	link, ok = b.links[token]
	delete(b.links, token)
	return link, ok && time.Now().Before(link.expires)
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
	token, err := rt.backups.issue(false)
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, map[string]string{"url": "/api/backup/" + token})
}

// handleDownloadBackup streams the archive dens backup writes, or the one
// dens den export writes (M10), as a download.
func (rt *router) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	link, ok := rt.backups.redeem(chi.URLParam(r, "token"))
	if !ok {
		http.Error(w, "This backup's link has expired; make the backup again.", http.StatusGone)
		return
	}
	var archive app.Archive
	var err error
	if link.export {
		archive, err = rt.a.ExportDen(r.Context())
	} else {
		archive, err = rt.a.Backup(r.Context())
	}
	if errors.Is(err, app.ErrHostingOn) {
		http.Error(w, "Hosting went on again, so the den wasn't exported.", http.StatusConflict)
		return
	}
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
