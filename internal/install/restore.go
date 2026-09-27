package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Restore replaces an installed instance's data with a backup. The backup's
// data key is unwrapped with the local password it was made under and
// wrapped again for this host; the data itself is never re-encrypted, so a
// backup moves between machines and platforms. A backup from an older
// version is migrated when the service starts.
func Restore(ctx context.Context, sys System, bi build.BuildInfo, opts Options, archive, password string) (err error) {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	p := newProgress(out)
	defer p.close()
	if err := sys.CheckAdmin(); err != nil {
		return err
	}
	target, err := sys.Layout(opts.Instance)
	if err != nil {
		return err
	}
	state, err := maintenance.ReadState(target.State)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("instance %s is not installed; install it first, then restore into it", target.Instance)
	}
	if err != nil {
		return err
	}
	if state.Version != bi.Version || !(state.Phase == maintenance.PhaseReady || state.Phase == maintenance.PhaseRestoring) {
		return fmt.Errorf("instance %s is %s at %s; restore with the installed binary once it is ready", target.Instance, state.Phase, state.Version)
	}
	account, err := sys.ServiceAccount(target)
	if err != nil {
		return err
	}

	p.printf("Reading %s ...", archive)
	staging, err := sys.PrepareRestore(target, account)
	if err != nil {
		return err
	}
	stagingKept := false
	defer func() {
		if !stagingKept {
			_ = os.RemoveAll(staging)
		}
	}()
	key, manifest, err := openBackup(bi, archive, staging, password)
	if err != nil {
		return err
	}
	defer clear(key)
	p.printf("Backup of instance %s from %s, made by %s %s on %s.",
		manifest.Instance, manifest.CreatedAt.Format(time.RFC1123), manifest.App, manifest.Version, manifest.Platform)
	if err := sys.AdoptRestored(target, staging, account); err != nil {
		return err
	}

	opLock, err := maintenance.AcquireOperationLock(ctx, target)
	if err != nil {
		return err
	}
	defer opLock.Close()
	p.openLog(target.MaintenanceLog)
	m := &member{l: target, prior: &state, account: account}
	j := &journal{}
	defer func() {
		if err != nil {
			err = errors.Join(err, j.rollback(out))
		}
	}()
	j.add(func() error {
		if m.wasRunning {
			return sys.StartService(target)
		}
		return nil
	})
	if err := publish(sys, m, maintenance.NewState(maintenance.PhaseRestoring, bi.Version, bi.Version), j); err != nil {
		return err
	}
	if running, err := sys.ServiceRunning(target); err != nil {
		return err
	} else if running {
		p.printf("Stopping %s ...", target.ServiceName)
		if err := sys.StopService(target); err != nil {
			return err
		}
		m.wasRunning = true
	}
	lifeLock, err := maintenance.AcquireLifecycleLock(ctx, target)
	if err != nil {
		return err
	}
	j.add(lifeLock.Close)

	p.printf("Replacing the data of instance %s ...", target.Instance)
	old := fmt.Sprintf("%s.replaced-%s", target.Data, time.Now().UTC().Format("20060102-150405"))
	if err := os.Rename(target.Data, old); err != nil {
		return fmt.Errorf("set the current data aside: %w", err)
	}
	j.add(func() error { return os.Rename(old, target.Data) })
	if err := os.Rename(staging, target.Data); err != nil {
		return fmt.Errorf("move the restored data in: %w", err)
	}
	stagingKept = true
	j.add(func() error { return os.Rename(target.Data, staging) })

	oldKey := target.HostKey + ".replaced"
	if err := os.Rename(target.HostKey, oldKey); err != nil {
		return fmt.Errorf("set the current data key aside: %w", err)
	}
	j.add(func() error { return os.Rename(oldKey, target.HostKey) })
	if err := sys.WrapHostKey(target, key); err != nil {
		return err
	}

	_ = lifeLock.Close()
	j.commit()
	// Logs describe this machine, not the backup; keep them.
	_ = os.Rename(filepath.Join(old, "logs"), target.Logs)
	p.printf("Starting %s (this migrates the restored data if needed) ...", target.ServiceName)
	if err := sys.StartService(target); err != nil {
		return fmt.Errorf("%s did not start with the restored data: %w\nThe previous data is in %s and the previous key in %s; see the service log in %s", target.ServiceName, err, old, oldKey, target.Logs)
	}
	if err := writeState(sys, m, maintenance.NewState(maintenance.PhaseReady, bi.Version, "")); err != nil {
		return err
	}
	if !m.wasRunning {
		if err := sys.StopService(target); err != nil {
			return err
		}
	}
	_ = os.Remove(oldKey)
	if err := os.RemoveAll(old); err != nil {
		p.printf("Couldn't remove the replaced data at %s: %v", old, err)
	}
	p.printf("Restored instance %s. Browsers paired with the backed-up install are paired here too; run dens open to pair this one.", target.Instance)
	return nil
}

// openBackup extracts archive into dir, arranges it as a data directory, and
// returns the data key sealed in it under password. It refuses a backup from
// a newer schema than this build knows and one whose key doesn't match its
// own database.
func openBackup(bi build.BuildInfo, archive, dir, password string) ([]byte, backup.Manifest, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, backup.Manifest{}, err
	}
	defer file.Close()
	manifest, err := backup.Extract(file, dir, backup.DefaultLimits)
	if err != nil {
		return nil, manifest, err
	}
	if manifest.App != bi.Name {
		return nil, manifest, fmt.Errorf("this is a backup of %q, not %s", manifest.App, bi.Name)
	}

	snapshot := backup.DatabasePath(dir)
	db, err := database.OpenReadOnly(snapshot)
	if err != nil {
		return nil, manifest, fmt.Errorf("open the backed-up database: %w", err)
	}
	ctx := context.Background()
	version, err := database.FileSchemaVersion(ctx, db)
	if err == nil && version > database.SchemaVersion(bi) {
		err = fmt.Errorf("the backup comes from a newer %s (schema %d); install %s or later first", bi.Name, version, manifest.Version)
	}
	var row *vaultstore.Row
	if err == nil {
		row, err = vaultstore.Get(ctx, db)
	}
	closeErr := db.Close()
	if err != nil {
		return nil, manifest, err
	}
	if closeErr != nil {
		return nil, manifest, closeErr
	}
	if row == nil || row.PasswordWrap == "" {
		return nil, manifest, errors.New("the backup has no password-protected data key")
	}
	wrap, err := vault.UnmarshalWrap(row.PasswordWrap)
	if err != nil {
		return nil, manifest, err
	}
	key, err := vault.UnwrapWithPassword(wrap, password)
	if err != nil {
		return nil, manifest, err
	}
	check, err := vault.New(key)
	if err != nil {
		clear(key)
		return nil, manifest, err
	}
	defer check.Close()
	if err := check.VerifyCheckValue(row.CheckValue); err != nil {
		clear(key)
		return nil, manifest, err
	}

	dbDir := filepath.Join(dir, "db")
	if err := os.Mkdir(dbDir, 0o700); err != nil {
		clear(key)
		return nil, manifest, err
	}
	if err := os.Rename(snapshot, filepath.Join(dbDir, database.FileName)); err != nil {
		clear(key)
		return nil, manifest, err
	}
	return key, manifest, nil
}
