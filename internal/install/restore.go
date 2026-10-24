package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
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
	key, manifest, err := openBackup(bi, archive, staging, password, target)
	if err != nil {
		return err
	}
	defer clear(key)
	p.printf("%s", describeArchive(manifest))
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
		return fmt.Errorf("%s did not start with the restored data: %w\nThe previous data is in %s and the previous key in %s. Find out why in %s", target.ServiceName, err, old, oldKey, host.ServiceLogHint(target.ServiceName, target.Logs))
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
	flag := ""
	if target.Instance != layout.DefaultInstance {
		flag = " --instance " + target.Instance
	}
	if manifest.DenExport {
		p.printf("Restored the den's export into instance %s, with hosting off. Turn hosting on with: %s den hosting on%s", target.Instance, bi.Name, flag)
		return nil
	}
	p.printf("Restored instance %s. Browsers don't carry over; pair one with: %s open%s", target.Instance, bi.Name, flag)
	return nil
}

// describeArchive is the line a restore gives the archive it read: a
// backup, or a den's export (M10), which holds the den alone.
func describeArchive(m backup.Manifest) string {
	made := fmt.Sprintf("from %s, made by %s %s on %s", m.CreatedAt.Format(time.RFC1123), m.App, m.Version, m.Platform)
	if m.DenExport {
		return fmt.Sprintf("The export of the den %s, from instance %s %s. It only holds the den, not any client data of the computer it left.",
			m.Den, m.Instance, made)
	}
	return fmt.Sprintf("Backup of instance %s %s.", m.Instance, made)
}

// acceptArchive refuses an archive the form can't restore: a den's export
// where the form hosts no den, as the Flatpak (M10).
func acceptArchive(form layout.Form, m backup.Manifest) error {
	if !m.DenExport {
		return nil
	}
	switch form {
	case layout.Flatpak:
		return errors.New("this is a den's export, and the Flatpak hosts no den; restore it where Dens is installed on Linux or Windows")
	}
	return nil
}

// movingGuide is where the docs say how to give a den an instance of its
// own on a computer whose Dens is in use.
const movingGuide = "https://dens.chat/docs/backups/#moving-a-den"

// checkTarget refuses a den's export over an instance that would lose
// something to it (M10): one that has joined a den, or hosts another. The
// export holds its den alone, so the instance's accounts, and a den of its
// own, would go with nothing in their place. The same den, on an instance
// that has joined none, is a move done again with a later export.
func checkTarget(ctx context.Context, target layout.Layout, m backup.Manifest, snapshot string) error {
	if !m.DenExport {
		return nil
	}
	held, err := readHoldings(ctx, filepath.Join(target.DB, database.FileName))
	if err != nil {
		return fmt.Errorf("check what instance %s holds before restoring a den's export over it: %w", target.Instance, err)
	}
	if held.joined == 0 {
		if held.den == "" {
			return nil
		}
		export, err := readHoldings(ctx, snapshot)
		if err != nil {
			return fmt.Errorf("read the export's den: %w", err)
		}
		if export.denID != nil && bytes.Equal(export.denID, held.denID) {
			return nil
		}
	}
	return fmt.Errorf("instance %s %s; restoring a den's export replaces all of an instance's data with the export's den alone.\n"+
		"Restore it into an instance of its own instead, as %s describes, or uninstall instance %s first if nothing in it needs keeping",
		target.Instance, held.describe(), movingGuide, target.Instance)
}

// restoreFloor is the room a restore leaves free on the disk it fills, as
// the den's uploads leave: the database and the system need it too.
const restoreFloor = 1 << 30

// freeSpace is host.FreeSpace; tests replace it.
var freeSpace = host.FreeSpace

// openBackup extracts archive into dir, arranges it as a data directory, and
// returns the data key sealed in it under password. It refuses a backup the
// disk hasn't room for before it starts, and stops one that runs out of
// room before the disk fills. One from another app or a newer schema, under
// another password or whose key doesn't match its own database, one the
// target's form can't restore, or a den's export that would delete what
// the target holds, it refuses as soon as its database is out, before any
// upload (M10).
func openBackup(bi build.BuildInfo, archive, dir, password string, target layout.Layout) ([]byte, backup.Manifest, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, backup.Manifest{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, backup.Manifest{}, err
	}
	// The uploads are sealed, so they don't compress, and the archive is
	// about as large as what it holds.
	free, err := freeSpace(dir)
	if err != nil {
		return nil, backup.Manifest{}, fmt.Errorf("check the room for the backup: %w", err)
	}
	if size := uint64(info.Size()); free < size+restoreFloor {
		return nil, backup.Manifest{}, fmt.Errorf("the backup is %s, and the disk holding %s has %s free: restoring it takes that much, and %s to spare",
			backup.SizeText(info.Size()), filepath.Dir(dir), backup.SizeText(int64(free)), backup.SizeText(restoreFloor))
	}
	read := &countingReader{r: file}
	var key []byte
	manifest, err := backup.Extract(read, dir, backup.ExtractOptions{
		Check: func(m backup.Manifest, database string) error {
			if err := acceptArchive(target.Form, m); err != nil {
				return err
			}
			k, err := checkBackup(bi, m, database, password)
			key = k
			if err != nil {
				return err
			}
			return checkTarget(context.Background(), target, m, database)
		},
		FreeSpace: freeSpace,
		Floor:     restoreFloor,
	})
	var noRoom *backup.NoRoomError
	if errors.As(err, &noRoom) {
		// What's left of the archive is about what's left to write.
		need := max(noRoom.Need, uint64(max(info.Size()-read.n, 0))+restoreFloor)
		err = fmt.Errorf("the disk holding %s ran out of room partway: the rest of the backup needs about %s more",
			filepath.Dir(dir), backup.SizeText(int64(need-min(need, noRoom.Free))))
	}
	if err != nil {
		clear(key)
		return nil, manifest, err
	}

	dbDir := filepath.Join(dir, "db")
	if err := os.Mkdir(dbDir, 0o700); err != nil {
		clear(key)
		return nil, manifest, err
	}
	if err := os.Rename(backup.DatabasePath(dir), filepath.Join(dbDir, database.FileName)); err != nil {
		clear(key)
		return nil, manifest, err
	}
	return key, manifest, nil
}

// checkBackup returns the data key a backup's database seals under
// password, once it has checked the backup is this app's, from a schema
// this build knows, and that the key matches the database.
func checkBackup(bi build.BuildInfo, manifest backup.Manifest, snapshot, password string) ([]byte, error) {
	if manifest.App != bi.Name {
		return nil, fmt.Errorf("this is a backup of %q, not %s", manifest.App, bi.Name)
	}
	db, err := database.OpenReadOnly(snapshot)
	if err != nil {
		return nil, fmt.Errorf("open the backed-up database: %w", err)
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
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if row == nil || row.PasswordWrap == "" {
		return nil, errors.New("the backup has no password-protected data key")
	}
	wrap, err := vault.UnmarshalWrap(row.PasswordWrap)
	if err != nil {
		return nil, err
	}
	key, err := vault.UnwrapWithPassword(wrap, password)
	if err != nil {
		return nil, err
	}
	check, err := vault.New(key)
	if err != nil {
		clear(key)
		return nil, err
	}
	defer check.Close()
	if err := check.VerifyCheckValue(row.CheckValue); err != nil {
		clear(key)
		return nil, err
	}
	return key, nil
}

// countingReader counts what's read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// RestoreSession replaces a session form's data with a backup (M9.3): the
// installed restore's steps without the elevation, as the user whose
// instance it is. stop asks a running service to stop, and the restore
// waits for its lifecycle lock. The restored data's first start migrates
// them and clears what a restore's clears, then removes the data they
// replaced. Their data key is the backup's: a development instance keeps
// it in its file, and the Flatpak, whose remembered copies held another
// key, starts locked, to open with the backup's local password. The state
// the data were in doesn't matter, since they go: as after Flatpak rolled
// Dens back past them, when restoring the snapshot from before the update
// is the way back.
func RestoreSession(ctx context.Context, bi build.BuildInfo, l layout.Layout, archive, password string, stop func() error, opts Options) (err error) {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	p := newProgress(out)
	defer p.close()
	if !l.Form.Session() {
		return fmt.Errorf("RestoreSession called on the %s layout", l.Form)
	}
	prior, err := maintenance.ReadState(l.State)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("instance %s has no data yet; start Dens once, then restore into it", l.Instance)
	}
	if err != nil {
		return err
	}
	if err := l.EnsureSession(); err != nil {
		return err
	}

	p.printf("Reading %s ...", archive)
	staging, err := os.MkdirTemp(l.Root, "data.restore-")
	if err != nil {
		return err
	}
	stagingKept := false
	defer func() {
		if !stagingKept {
			_ = os.RemoveAll(staging)
		}
	}()
	key, manifest, err := openBackup(bi, archive, staging, password, l)
	if err != nil {
		return err
	}
	defer clear(key)
	p.printf("%s", describeArchive(manifest))

	opLock, err := maintenance.AcquireOperationLock(ctx, l)
	if err != nil {
		return err
	}
	defer opLock.Close()
	p.openLog(l.MaintenanceLog)
	if err := stop(); err != nil {
		return err
	}
	lifeLock, err := maintenance.AcquireLifecycleLock(ctx, l)
	if err != nil {
		return err
	}
	defer lifeLock.Close()
	j := &journal{}
	defer func() {
		if err != nil {
			err = errors.Join(err, j.rollback(out))
		}
	}()
	if err := maintenance.WriteState(l.State, maintenance.NewState(maintenance.PhaseRestoring, bi.Version, bi.Version), nil); err != nil {
		return err
	}
	j.add(func() error { return maintenance.WriteState(l.State, prior, nil) })

	p.printf("Replacing the data of instance %s ...", l.Instance)
	old := fmt.Sprintf("%s.replaced-%s", l.Data, time.Now().UTC().Format("20060102-150405"))
	if err := os.Rename(l.Data, old); err != nil {
		return fmt.Errorf("set the current data aside: %w", err)
	}
	j.add(func() error { return os.Rename(old, l.Data) })
	if err := os.Rename(staging, l.Data); err != nil {
		return fmt.Errorf("move the restored data in: %w", err)
	}
	stagingKept = true
	j.add(func() error { return os.Rename(l.Data, staging) })
	switch l.Form {
	case layout.Development:
		if err := maintenance.WriteFileAtomic(l.HostKey, key, nil); err != nil {
			return fmt.Errorf("keep the backup's data key: %w", err)
		}
	case layout.Flatpak:
		for _, path := range []string{l.HostKey, l.KeyringKey} {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("forget the data key the instance remembered: %w", err)
			}
		}
	}
	j.commit()

	// Logs describe this machine, not the backup; keep them. A cache
	// outside the data holds the replaced data's dens.
	_ = os.Rename(filepath.Join(old, "logs"), l.Logs)
	if filepath.Dir(l.Cache) != l.Data {
		entries, _ := os.ReadDir(l.Cache)
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(l.Cache, e.Name())); err != nil {
				p.printf("Couldn't empty the cache in %s: %v", l.Cache, err)
			}
		}
	}
	switch l.Form {
	case layout.Flatpak:
		p.printf("Restored instance %s. Open Dens from your apps menu: it starts locked, and opens with the backup's local password.", l.Instance)
	default:
		flag := ""
		if l.Instance != layout.DefaultInstance {
			flag = " --instance " + l.Instance
		}
		if manifest.DenExport {
			p.printf("Restored the den's export into instance %s, with hosting off. Start it with: %s service run%s, then turn hosting on with: %s den hosting on%s",
				l.Instance, bi.Name, flag, bi.Name, flag)
			break
		}
		p.printf("Restored instance %s. Start it with: %s service run%s", l.Instance, bi.Name, flag)
	}
	return nil
}
