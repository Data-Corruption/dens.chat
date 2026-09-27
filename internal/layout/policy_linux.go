package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Modes of installed instances. The control directory and its files belong
// to root and are grouped to the service account, which may read them but
// not change them. The data directory belongs to the service account alone.
const (
	ControlDirMode  fs.FileMode = 0o750
	ControlFileMode fs.FileMode = 0o640
	DataDirMode     fs.FileMode = 0o700
)

// CheckControl verifies the control directory and the named files in it
// before the service trusts them. Anything else is refused, never repaired.
func (l Layout) CheckControl(files ...string) error {
	dirUID, fileUID, gid := 0, 0, os.Getgid()
	dirMode, fileMode := ControlDirMode, ControlFileMode
	if l.Dev {
		dirUID, fileUID, gid = os.Getuid(), os.Getuid(), -1
		dirMode, fileMode = 0o700, 0o600
	}
	if err := checkOwned(l.Control, true, dirUID, gid, dirMode); err != nil {
		return fmt.Errorf("control directory %s: %w", l.Control, err)
	}
	for _, file := range files {
		if err := checkOwned(file, false, fileUID, gid, fileMode); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	return nil
}

// CheckData verifies that the data directory belongs to this process's
// account and is private to it.
func (l Layout) CheckData() error {
	if err := checkOwned(l.Data, true, os.Getuid(), -1, DataDirMode); err != nil {
		return fmt.Errorf("data directory %s: %w", l.Data, err)
	}
	return nil
}

// EnsureData creates the directories below Data. The service calls it after
// CheckData; systemd creates Data itself from StateDirectory=.
func (l Layout) EnsureData() error {
	for _, dir := range l.DataDirs()[1:] {
		if err := ensurePrivateDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDev creates a development instance's directories, private to the
// developer.
func (l Layout) EnsureDev() error {
	if !l.Dev {
		return errors.New("EnsureDev called on an installed layout")
	}
	for _, base := range []string{filepath.Dir(l.Root), filepath.Dir(l.Runtime)} {
		if err := os.MkdirAll(base, 0o700); err != nil {
			return fmt.Errorf("create development base directory: %w", err)
		}
	}
	for _, dir := range append([]string{l.Root, l.Control, l.Runtime}, l.DataDirs()...) {
		if err := ensurePrivateDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := checkOwned(dir, true, os.Getuid(), -1, 0o700); err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	return nil
}

// checkOwned verifies path's type, owner, group (unless gid < 0) and exact
// permission bits, without following a symlink.
func checkOwned(path string, dir bool, uid, gid int, perm fs.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return errors.New("is a symlink")
	case dir && !info.IsDir():
		return errors.New("is not a directory")
	case !dir && !info.Mode().IsRegular():
		return errors.New("is not a regular file")
	case info.Mode().Perm() != perm:
		return fmt.Errorf("permissions are %04o, want %04o", info.Mode().Perm(), perm)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot determine owner")
	}
	if int(stat.Uid) != uid {
		return fmt.Errorf("owner UID is %d, want %d", stat.Uid, uid)
	}
	if gid >= 0 && int(stat.Gid) != gid {
		return fmt.Errorf("group GID is %d, want %d", stat.Gid, gid)
	}
	return nil
}

// ServiceControlFiles are the control files the service reads and checks.
// On Linux the encrypted data key is root-only; systemd decrypts it for the
// service.
func (l Layout) ServiceControlFiles() []string {
	files := []string{l.State, l.LifecycleLock, l.InstanceConfig}
	if l.Dev {
		files = append(files, l.HostKey)
	}
	return files
}
