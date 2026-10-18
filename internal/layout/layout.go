// Package layout defines every path one instance of Dens owns, and the
// ownership and permissions each must have. Nothing else resolves paths.
//
// An installed instance has a root-owned installation root. Its control
// directory holds what only elevated maintenance commands may change
// (lifecycle state, locks, instance config, the host-wrapped data key); the
// service can read it but not write it. The data directory belongs to the
// service account. A session form's instance, as a development one, keeps
// the same shape under its user's own data directory, owned by its user.
package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DesktopID names Dens to Linux desktops: its .desktop file, and the app ID
// the shortcut helper gives the desktop's portal (M7.2).
const DesktopID = "chat.dens.Dens"

// DefaultInstance is the instance name used when none is given.
const DefaultInstance = "main"

// File names inside the control directory.
const (
	StateFileName          = "state.json"
	OperationLockFileName  = "operation.lock"
	LifecycleLockFileName  = "lifecycle.lock"
	InstanceFileName       = "instance.json"
	MaintenanceLogFileName = "maintenance.log"
)

// Layout is the canonical set of paths for one instance.
type Layout struct {
	App      string
	Instance string
	Form     Form

	// Root is the installation root.
	Root string

	Control        string
	State          string
	OperationLock  string
	LifecycleLock  string
	InstanceConfig string
	HostKey        string
	MaintenanceLog string

	Data    string
	DB      string
	Logs    string
	Uploads string
	Temp    string
	// Cache holds the client's cache of other dens' files (M6), which
	// backups leave out.
	Cache string

	// Runtime is the directory holding the control socket on Linux. It is
	// empty on Windows, where the control endpoint is a named pipe.
	Runtime         string
	ControlEndpoint string

	// System integration. Empty for session forms.
	Binary      string
	BinaryDir   string
	Cosign      string
	ServiceName string
	Account     string
	// UnitFile and SysusersFile are Linux-only.
	UnitFile     string
	SysusersFile string
	// AutostartFile starts the shortcut helper at the desktop user's
	// sign-in, and DesktopFile, with its icon, names Dens to the desktop,
	// whose shortcuts portal needs it, and opens the page from its menu
	// (M7.2). All three are Linux-only.
	AutostartFile string
	DesktopFile   string
	IconFile      string
}

// New returns the layout of instance for application app, deployed as
// form. A session form's layout lives under its user's data directory and
// has no system integration.
func New(app, instance string, form Form) (Layout, error) {
	if err := ValidateApp(app); err != nil {
		return Layout{}, err
	}
	if err := ValidateInstance(instance); err != nil {
		return Layout{}, err
	}
	l := Layout{App: app, Instance: instance, Form: form}
	if err := l.resolve(); err != nil {
		return Layout{}, err
	}
	return l, nil
}

// Instances lists the instances of app in form on this computer by the
// names of their roots, which every account may list, whether or not it
// may read them. The shortcut helper tries each one's control endpoint
// (M7).
func Instances(app string, form Form) ([]string, error) {
	l, err := New(app, DefaultInstance, form)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Dir(l.Root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && ValidateInstance(e.Name()) == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// derive fills every path below Root. resolve sets Root and the platform
// paths first.
func (l *Layout) derive() {
	l.Control = filepath.Join(l.Root, "control")
	l.State = filepath.Join(l.Control, StateFileName)
	l.OperationLock = filepath.Join(l.Control, OperationLockFileName)
	l.LifecycleLock = filepath.Join(l.Control, LifecycleLockFileName)
	l.InstanceConfig = filepath.Join(l.Control, InstanceFileName)
	l.MaintenanceLog = filepath.Join(l.Control, MaintenanceLogFileName)

	l.Data = filepath.Join(l.Root, "data")
	l.DB = filepath.Join(l.Data, "db")
	l.Logs = filepath.Join(l.Data, "logs")
	l.Uploads = filepath.Join(l.Data, "uploads")
	l.Temp = filepath.Join(l.Data, "tmp")
	l.Cache = filepath.Join(l.Data, "cache")
}

// DataDirs are the service-owned directories below Data.
func (l Layout) DataDirs() []string {
	return []string{l.Data, l.DB, l.Logs, l.Uploads, l.Temp, l.Cache}
}

// ValidateApp checks an application name used in paths and account names.
func ValidateApp(name string) error {
	if name == "" {
		return fmt.Errorf("application name is empty")
	}
	for i := range len(name) {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' && i > 0) {
			return fmt.Errorf("application name %q must be lowercase letters and digits, starting with a letter", name)
		}
	}
	return nil
}

// ValidateInstance checks an instance name. Names become part of account,
// service and path names, so they are short and conservative: a lowercase
// letter, then up to 15 lowercase letters, digits or single hyphens, not
// ending in a hyphen.
func ValidateInstance(name string) error {
	if len(name) == 0 || len(name) > 16 {
		return fmt.Errorf("instance name %q must be 1 to 16 characters", name)
	}
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		case i > 0 && c == '-' && name[i-1] != '-':
		default:
			return fmt.Errorf("instance name %q must start with a lowercase letter and use only lowercase letters, digits and single hyphens", name)
		}
	}
	if name[len(name)-1] == '-' {
		return fmt.Errorf("instance name %q must not end with a hyphen", name)
	}
	return nil
}

// WithRoot returns the installed-shape layout of instance with its
// installation root moved under base, keeping every other path. Tests of
// maintenance transactions use it to run against a temporary directory.
func WithRoot(l Layout, base string) Layout {
	l.Root = filepath.Join(base, l.Instance)
	hostKey := filepath.Base(l.HostKey)
	l.derive()
	l.HostKey = filepath.Join(l.Control, hostKey)
	return l
}
