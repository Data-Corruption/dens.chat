package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
)

// Backup writes an archive of a consistent snapshot of the database and the
// uploads, as dens backup and the page's Back up stream it, and names it.
// The archive carries the data key sealed under the local password, which
// the caller has checked. It's written as it's read.
func (a *App) Backup(ctx context.Context) (name string, archive io.ReadCloser, err error) {
	if a.Locked() {
		return "", nil, ErrLocked
	}
	// The den takes no file off the disk from before the snapshot until
	// the archive holds the uploads, so the archive has every file the
	// snapshot names (M8).
	release := a.Den.HoldFiles()
	temp, err := os.MkdirTemp(a.Layout.Temp, "backup-")
	if err != nil {
		release()
		return "", nil, err
	}
	snapshot := filepath.Join(temp, "db.sqlite")
	if err := database.Snapshot(ctx, a.DB, snapshot); err != nil {
		release()
		_ = os.RemoveAll(temp)
		return "", nil, err
	}
	now := time.Now().UTC()
	manifest := backup.Manifest{
		App:           a.buildInfo.Name,
		Version:       a.buildInfo.Version,
		SchemaVersion: database.SchemaVersion(a.buildInfo),
		Instance:      a.Layout.Instance,
		CreatedAt:     now,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}
	reader, writer := io.Pipe()
	go func() {
		skipped, err := backup.Write(writer, manifest, snapshot, a.Layout.Uploads)
		release()
		_ = writer.CloseWithError(err)
		_ = os.RemoveAll(temp)
		if skipped > 0 {
			// Only a file whose upload failed as it was stored goes now,
			// and no row names it.
			a.Log.Infof("Backup left out %d files that went while it was written", skipped)
		}
		if err != nil {
			a.Log.Warnf("backup did not finish: %v", err)
		} else {
			a.Log.Info("Backup written")
		}
	}()
	name = fmt.Sprintf("%s-%s-%s.backup", a.buildInfo.Name, a.Layout.Instance, now.Format("20060102-150405"))
	return name, reader, nil
}
