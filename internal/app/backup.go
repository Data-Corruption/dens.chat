package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/types"
)

// Archive is a backup's archive, written as it's read: its name, and at
// least its size, which the room for it takes.
type Archive struct {
	Name string
	Size int64
	io.ReadCloser
}

// ErrNoDen is a den's export or removal where this instance holds no den
// (M10).
var ErrNoDen = errors.New("this instance holds no den")

// ErrNotExported is removing a den that hasn't been exported since hosting
// was last on, which no other computer can hold all of (M10).
var ErrNotExported = errors.New("the den hasn't been exported since hosting was last on")

// AddressError is a den that doesn't answer at its address from this
// computer as itself, so its copy here stays (M10). It says what to try.
type AddressError struct {
	Den, URL string
	Err      error
}

func (e *AddressError) Error() string {
	advice := "It goes once the other computer hosts the den behind Caddy, and your router forwards there."
	if u, err := url.Parse(e.URL); err == nil && u.Hostname() != "" && net.ParseIP(u.Hostname()) == nil {
		host := u.Hostname()
		advice = fmt.Sprintf("It goes once the other computer hosts the den behind Caddy, %s points at that computer, and your router forwards there. "+
			"A router that can't reach its own public address needs a hosts entry for %s on this computer, with the server's local address.", host, host)
	}
	return fmt.Sprintf("%s doesn't answer at %s from this computer, so its copy here stays. %s %s", e.Den, e.URL, denclient.Describe(e.Err), advice)
}

func (e *AddressError) Unwrap() error { return e.Err }

// Backup writes an archive of a consistent snapshot of the database and the
// uploads, as dens backup and the page's Back up stream it, and names it.
// The archive carries the data key sealed under the local password, which
// the caller has checked.
func (a *App) Backup(ctx context.Context) (Archive, error) {
	if a.Locked() {
		return Archive{}, ErrLocked
	}
	now := time.Now().UTC()
	name := fmt.Sprintf("%s-%s-%s.backup", a.buildInfo.Name, a.Layout.Instance, now.Format("20060102-150405"))
	return a.archive(ctx, backup.Manifest{CreatedAt: now}, name, "Backup", func(path string) error {
		return database.Snapshot(ctx, a.DB, path)
	}, nil)
}

// ExportDen writes the den's export, which moves it to another computer
// (M10): an archive in a backup's format whose database holds the den and
// none of the client (database.SnapshotDen), as dens den export and the
// page's Move stream it, and names it. It refuses while hosting is on, so
// nothing reaches the den after its snapshot, and records the export,
// which turning hosting on forgets, and which an export that fails takes
// back. The archive carries the data key sealed under the local password,
// which the caller has checked.
func (a *App) ExportDen(ctx context.Context) (Archive, error) {
	if a.Locked() {
		return Archive{}, ErrLocked
	}
	info, ok := a.Den.Info()
	if !ok {
		return Archive{}, ErrNoDen
	}
	now := time.Now().UTC()
	err := a.Hosting.WhileOff(func() error {
		_, err := config.Update(a.DB, func(c *types.Configuration) error {
			c.DenExported = now
			return nil
		})
		return err
	})
	if err != nil {
		return Archive{}, err
	}
	a.Dens.Changed()
	// An export that fails holds nothing another computer could serve, so
	// its record goes, unless hosting or another export has replaced it.
	failed := func() {
		if _, err := config.Update(a.DB, func(c *types.Configuration) error {
			if c.DenExported.Equal(now) {
				c.DenExported = time.Time{}
			}
			return nil
		}); err != nil {
			a.Log.Warnf("Couldn't forget the den's export that failed: %v", err)
		}
		a.Dens.Changed()
	}
	name := fmt.Sprintf("%s-%s-den-%s.backup", a.buildInfo.Name, a.Layout.Instance, now.Format("20060102-150405"))
	return a.archive(ctx, backup.Manifest{CreatedAt: now, DenExport: true, Den: info.Name}, name, "The den's export", func(path string) error {
		return database.SnapshotDen(ctx, a.DB, path, a.buildInfo)
	}, failed)
}

// archive streams an archive of the database, as snapshot copies it to the
// path it's given, and of the uploads, under the manifest m, filled in.
// what names it in the log, and failed, if set, runs when it fails.
func (a *App) archive(ctx context.Context, m backup.Manifest, name, what string, snapshot func(path string) error, failed func()) (Archive, error) {
	if failed == nil {
		failed = func() {}
	}
	// The den takes no file off the disk from before the snapshot until
	// the archive holds the uploads, so the archive has every file the
	// snapshot names (M8).
	release := a.Den.HoldFiles()
	temp, err := os.MkdirTemp(a.Layout.Temp, "backup-")
	if err != nil {
		release()
		failed()
		return Archive{}, err
	}
	path := filepath.Join(temp, "db.sqlite")
	if err := snapshot(path); err != nil {
		release()
		_ = os.RemoveAll(temp)
		failed()
		return Archive{}, err
	}
	size, err := archiveSize(path, a.Layout.Uploads)
	if err != nil {
		release()
		_ = os.RemoveAll(temp)
		failed()
		return Archive{}, err
	}
	m.App = a.buildInfo.Name
	m.Version = a.buildInfo.Version
	m.SchemaVersion = database.SchemaVersion(a.buildInfo)
	m.Instance = a.Layout.Instance
	m.Platform = runtime.GOOS + "/" + runtime.GOARCH
	reader, writer := io.Pipe()
	go func() {
		skipped, err := backup.Write(writer, m, path, a.Layout.Uploads)
		release()
		_ = writer.CloseWithError(err)
		_ = os.RemoveAll(temp)
		if skipped > 0 {
			// Only a file whose upload failed as it was stored goes now,
			// and no row names it.
			a.Log.Infof("%s left out %d files that went while it was written", what, skipped)
		}
		if err != nil {
			a.Log.Warnf("%s did not finish: %v", what, err)
			failed()
		} else {
			a.Log.Infof("%s written", what)
		}
	}()
	return Archive{Name: name, Size: size, ReadCloser: reader}, nil
}

// RemoveDen deletes this install's copy of its den once another computer
// serves it (M10): with hosting off, the den exported and hosting not on
// since, and the den answering at its address from this computer as
// itself, which with hosting off here means another computer serves it.
// authorize runs once those hold, before anything goes, as the local
// password's check, so a refusal names what didn't hold without spending
// one. The record of the export goes with the den, and this install's
// connection to the den starts again at its address, keeping its account
// there.
func (a *App) RemoveDen(ctx context.Context, authorize func() error) error {
	if a.Locked() {
		return ErrLocked
	}
	info, ok := a.Den.Info()
	if !ok {
		return ErrNoDen
	}
	if a.Hosting.State().On {
		return ErrHostingOn
	}
	exported := func() error {
		cfg, err := config.View(a.DB)
		if err != nil {
			return err
		}
		if cfg.DenExported.IsZero() {
			return ErrNotExported
		}
		return nil
	}
	if err := exported(); err != nil {
		return err
	}
	if err := a.Dens.CheckDen(ctx, info.ID, info.URL); err != nil {
		return &AddressError{Den: info.Name, URL: info.URL, Err: err}
	}
	if err := authorize(); err != nil {
		return err
	}
	// Hosting may have gone on and off again while the den was checked,
	// which forgot the export.
	erased := false
	err := a.Hosting.WhileOff(func() error {
		if err := exported(); err != nil {
			return err
		}
		if err := a.Den.Erase(ctx); err != nil {
			return err
		}
		erased = true
		_, err := config.Update(a.DB, func(c *types.Configuration) error {
			c.DenExported = time.Time{}
			return nil
		})
		return err
	})
	if erased {
		a.Dens.OwnDenGone()
	}
	return err
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
