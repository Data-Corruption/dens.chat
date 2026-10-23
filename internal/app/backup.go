package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
)

// Archive is a backup's archive, written as it's read: its name, and at
// least its size, which the room for it takes.
type Archive struct {
	Name string
	Size int64
	io.ReadCloser
}

// Backup writes an archive of a consistent snapshot of the database and the
// uploads, as dens backup and the page's Back up stream it, and names it.
// The archive carries the data key sealed under the local password, which
// the caller has checked.
func (a *App) Backup(ctx context.Context) (Archive, error) {
	if a.Locked() {
		return Archive{}, ErrLocked
	}
	// The den takes no file off the disk from before the snapshot until
	// the archive holds the uploads, so the archive has every file the
	// snapshot names (M8).
	release := a.Den.HoldFiles()
	temp, err := os.MkdirTemp(a.Layout.Temp, "backup-")
	if err != nil {
		release()
		return Archive{}, err
	}
	snapshot := filepath.Join(temp, "db.sqlite")
	if err := database.Snapshot(ctx, a.DB, snapshot); err != nil {
		release()
		_ = os.RemoveAll(temp)
		return Archive{}, err
	}
	size, err := archiveSize(snapshot, a.Layout.Uploads)
	if err != nil {
		release()
		_ = os.RemoveAll(temp)
		return Archive{}, err
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
	name := fmt.Sprintf("%s-%s-%s.backup", a.buildInfo.Name, a.Layout.Instance, now.Format("20060102-150405"))
	return Archive{Name: name, Size: size, ReadCloser: reader}, nil
}

// BackupSize is at least how large a backup made now would be, from the
// database as it stands, with its write-ahead log, and the uploads, so the
// page can say before it makes one (M10).
func (a *App) BackupSize() (int64, error) {
	return archiveSize(filepath.Join(a.Layout.DB, database.FileName), a.Layout.Uploads)
}

// archiveSize is at least the size of an archive of the database at db,
// with any write-ahead log beside it, and the uploads under uploads.
func archiveSize(db, uploads string) (int64, error) {
	var size int64
	for _, path := range []string{db, db + "-wal"} {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		size += info.Size()
	}
	files, count, err := backup.UploadsSize(uploads)
	if err != nil {
		return 0, err
	}
	return backup.Estimate(size, files, count), nil
}
