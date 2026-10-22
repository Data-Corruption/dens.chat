package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
)

// Flatpak is what the Flatpak's service does in the member's session
// (M9.3), which the page asks of it.
type Flatpak interface {
	// Update is what Flatpak's update monitor found.
	Update() FlatpakUpdate
	// InstallUpdate has the monitor install the update. Nothing to update
	// means something else installed it first: either way the newest build
	// is installed, for StartNewest.
	InstallUpdate(ctx context.Context) error
	// StartNewest starts the newest build installed as a sandbox of its
	// own, which waits for this one to stop; Quit stops this one.
	StartNewest() error
	Quit(why string)
	// StartAtSignIn is whether Dens starts at the member's sign-in, which
	// SetStartAtSignIn turns on or off.
	StartAtSignIn() StartAtSignIn
	SetStartAtSignIn(ctx context.Context, on bool) error
}

// FlatpakUpdate is the Flatpak's update notice (M9.3): an update on its
// remote, Available, or one installed and not yet running, Installed; and
// whether Dens may install it itself, or the software center does, and
// why.
type FlatpakUpdate struct {
	Available  bool   `json:"available"`
	Installed  bool   `json:"installed"`
	CanInstall bool   `json:"can_install"`
	Refused    string `json:"refused,omitempty"`
}

// StartAtSignIn is starting at sign-in as the settings show it: whether
// Dens does, and where the desktop can't start it, the command to have it
// start by hand.
type StartAtSignIn struct {
	On     bool   `json:"on"`
	Manual string `json:"manual,omitempty"`
}

// flatpakInfo is the file Flatpak puts at the root of each sandbox it runs,
// naming the app; tests point it elsewhere.
var flatpakInfo = "/.flatpak-info"

// inFlatpak reports whether this process runs in Dens's Flatpak sandbox:
// one whose /.flatpak-info and FLATPAK_ID both name Dens's app ID. Another
// app's sandbox is no Flatpak of Dens, whose socket and portals go by
// Dens's ID.
func inFlatpak() bool {
	if os.Getenv("FLATPAK_ID") != layout.DesktopID {
		return false
	}
	data, err := os.ReadFile(flatpakInfo)
	if err != nil {
		return false
	}
	section := ""
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			continue
		}
		if name, ok := strings.CutPrefix(line, "name="); ok && section == "Application" {
			return name == layout.DesktopID
		}
	}
	return false
}

// sessionOptions say how a session form's start takes its lease. The
// Flatpak waits for an older build that's handing over to it, and keeps a
// snapshot of the data an older version left before it migrates them.
func (a *App) sessionOptions() maintenance.SessionOptions {
	switch a.Layout.Form {
	case layout.Flatpak:
		return maintenance.SessionOptions{Wait: maintenance.LifecycleLockTimeout, Snapshot: a.snapshot}
	}
	return maintenance.SessionOptions{}
}

// snapshot keeps a copy of the data an older version left, from before
// this version migrates them (M9.3): a backup archive, which the restore
// takes back should Flatpak roll Dens back. It leaves the cache out, as
// every backup does, and only the last one stays. Nothing else runs on the
// data meanwhile: the start holds the lifecycle lock.
func (a *App) snapshot(from maintenance.State) error {
	l := a.Layout
	path := filepath.Join(l.DB, database.FileName)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("the data from %s has no database: %w", from.Version, err)
	}
	temp, err := os.MkdirTemp(l.Temp, "snapshot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	db, err := database.Open(path)
	if err != nil {
		return err
	}
	ctx := context.Background()
	copied := filepath.Join(temp, "db.sqlite")
	schema, err := database.FileSchemaVersion(ctx, db)
	if err == nil {
		err = database.Snapshot(ctx, db, copied)
	}
	if err := errors.Join(err, db.Close()); err != nil {
		return err
	}

	now := time.Now().UTC()
	name := fmt.Sprintf("%s-%s-%s-%s.backup", a.buildInfo.Name, l.Instance, from.Version, now.Format("20060102-150405"))
	staged := filepath.Join(l.Snapshot, "."+name+".tmp")
	file, err := os.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = backup.Write(file, backup.Manifest{
		App:           a.buildInfo.Name,
		Version:       from.Version,
		SchemaVersion: schema,
		Instance:      l.Instance,
		CreatedAt:     now,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}, copied, l.Uploads)
	if err == nil {
		err = file.Sync()
	}
	if err := errors.Join(err, file.Close()); err != nil {
		_ = os.Remove(staged)
		return err
	}
	kept := filepath.Join(l.Snapshot, name)
	if err := os.Rename(staged, kept); err != nil {
		_ = os.Remove(staged)
		return err
	}
	entries, err := os.ReadDir(l.Snapshot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() != name {
			if err := os.RemoveAll(filepath.Join(l.Snapshot, e.Name())); err != nil {
				return fmt.Errorf("remove the snapshot before: %w", err)
			}
		}
	}
	a.snapshotKept = kept
	return nil
}

// lastSnapshot is the snapshot the Flatpak kept as it last updated, or "".
func (a *App) lastSnapshot() string {
	if a.Layout.Snapshot == "" {
		return ""
	}
	entries, err := os.ReadDir(a.Layout.Snapshot)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".backup") && !strings.HasPrefix(e.Name(), ".") {
			return filepath.Join(a.Layout.Snapshot, e.Name())
		}
	}
	return ""
}

// explainRollback adds the way back to a rollback's refusal: the snapshot
// of the data from before the update, which the restore takes.
func (a *App) explainRollback(err error) error {
	var rolled *maintenance.RolledBackError
	if !errors.As(err, &rolled) {
		return err
	}
	snapshot := a.lastSnapshot()
	if snapshot == "" {
		return err
	}
	return fmt.Errorf("%w, or restore the snapshot of its data from before the update: flatpak run --command=%s %s restore %s",
		err, a.buildInfo.Name, layout.DesktopID, snapshot)
}
