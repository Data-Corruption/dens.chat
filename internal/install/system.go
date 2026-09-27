// Package install runs the elevated maintenance transactions: install,
// update, uninstall and restore. The installer scripts only download and
// verify a release, then run its binary's install command; everything that
// changes an installation happens here, once, for both platforms.
//
// A transaction follows the lifecycle protocol in internal/maintenance:
// take every instance's operation lock, publish a transitional state, stop
// the services, take their lifecycle locks, change the installation, release
// the lifecycle locks and start the services, which migrate on start. Until
// the services start, a failure rolls every change back. After that it
// leaves the transitional state in place, and rerunning the installer
// recovers.
//
// Every instance on a machine runs the one installed binary, so installing
// a new version moves every existing instance through the transition.
package install

import (
	"context"

	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// System is the platform's side of a transaction. Implementations live in
// system_linux.go and system_windows.go.
type System interface {
	// Layout resolves an installed instance's layout.
	Layout(instance string) (layout.Layout, error)
	// CheckAdmin fails unless the process may change the installation.
	CheckAdmin() error
	// Preflight checks the platform can host the service.
	Preflight() error
	// DesktopUser resolves the account allowed to pair: override when set,
	// otherwise the user who elevated or who is signed in at the console.
	DesktopUser(override string) (host.Identity, error)

	// PrepareRoot creates the installation root and control directory,
	// writable only by administrators, if they don't exist.
	PrepareRoot(l layout.Layout) error
	// EnsureAccount creates the service account if the platform needs one
	// before the service is registered, and reports whether it created it.
	EnsureAccount(l layout.Layout) (created bool, err error)
	// RemoveAccount deletes an account EnsureAccount created.
	RemoveAccount(l layout.Layout) error
	// InstallBinary places the binary at src as the installed binary and
	// returns an undo.
	InstallBinary(l layout.Layout, src string) (undo func() error, err error)
	// InstallCosign places a verified cosign binary where dens update finds it.
	InstallCosign(l layout.Layout, src string) error
	// RegisterService registers or re-registers the instance's service and
	// returns an undo.
	RegisterService(l layout.Layout) (undo func() error, err error)
	// ServiceAccount is the account the service runs as, once registered.
	ServiceAccount(l layout.Layout) (host.Identity, error)
	// Secure creates the data directory if needed and sets the intended
	// ownership and permissions on the installation root, the control files
	// and the data directory.
	Secure(l layout.Layout, account host.Identity) error
	// WriteControlFile atomically writes a control file readable by the
	// service account (or, with an empty account, by administrators only).
	WriteControlFile(l layout.Layout, path string, data []byte, account host.Identity) error
	// WrapHostKey stores the data key bound to this host.
	WrapHostKey(l layout.Layout, key []byte) error
	// ConfigureFirewall opens or closes the den's media ports as cfg says.
	ConfigureFirewall(l layout.Layout, cfg instance.Config) error
	// RegisterExtras applies machine-wide integration: PATH, crash reporting.
	RegisterExtras(l layout.Layout) error

	ServiceRunning(l layout.Layout) (bool, error)
	StartService(l layout.Layout) error
	StopService(l layout.Layout) error

	// UnregisterService removes the service and its firewall rules.
	UnregisterService(l layout.Layout) error
	// RemoveShared removes what every instance shares, after the last one
	// is uninstalled: the binary, cosign, the unit template, PATH entries.
	RemoveShared(l layout.Layout) error
	// PrepareRestore makes an empty directory beside the data directory,
	// with the data directory's permissions, to extract a backup into.
	PrepareRestore(l layout.Layout, account host.Identity) (string, error)
	// AdoptRestored gives the service account ownership of restored files.
	AdoptRestored(l layout.Layout, dir string, account host.Identity) error

	// InstallerName is the file name of this platform's installer script.
	InstallerName() string
	// RunInstaller runs a verified installer script in the foreground to
	// update instance, with env added to the environment.
	RunInstaller(ctx context.Context, script, instance string, env []string) error
}
