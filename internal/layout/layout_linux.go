package layout

import (
	"fmt"
	"os"
	"path/filepath"
)

func (l *Layout) resolve() error {
	switch l.Form {
	case Installed:
		l.Root = filepath.Join("/var/lib", l.App, l.Instance)
		l.derive()
		l.HostKey = filepath.Join(l.Control, "datakey.cred")
		l.Runtime = filepath.Join("/run", l.App, l.Instance)
		l.ControlEndpoint = filepath.Join(l.Runtime, "control.sock")
		l.BinaryDir = "/usr/local/bin"
		l.Binary = filepath.Join(l.BinaryDir, l.App)
		l.Cosign = filepath.Join("/usr/local/lib", l.App, "cosign")
		l.ServiceName = fmt.Sprintf("%s@%s.service", l.App, l.Instance)
		l.Account = l.App + "-" + l.Instance
		l.UnitFile = filepath.Join("/etc/systemd/system", l.App+"@.service")
		l.SysusersFile = filepath.Join("/etc/sysusers.d", l.Account+".conf")
		l.AutostartFile = filepath.Join("/etc/xdg/autostart", DesktopID+".desktop")
		l.DesktopFile = filepath.Join("/usr/local/share/applications", DesktopID+".desktop")
		l.IconFile = filepath.Join("/usr/local/share/icons/hicolor/scalable/apps", DesktopID+".svg")
	case Development:
		base, err := devBase(l.App)
		if err != nil {
			return err
		}
		l.Root = filepath.Join(base, l.Instance)
		l.derive()
		l.HostKey = filepath.Join(l.Control, "datakey.dev")
		// Socket paths are limited to 108 bytes, so prefer the short
		// per-user runtime directory over a possibly deep data directory.
		l.Runtime = filepath.Join(l.Root, "run")
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(runtimeDir) {
			l.Runtime = filepath.Join(runtimeDir, l.App+"-dev", l.Instance)
		}
		l.ControlEndpoint = filepath.Join(l.Runtime, "control.sock")
	case Flatpak:
		// Flatpak points the XDG directories at the app's own, under
		// ~/.var/app/<app ID>, and shares $XDG_RUNTIME_DIR/app/<app ID>
		// between every sandbox of the app, so a launch reaches the
		// service another sandbox runs.
		dirs := map[string]string{}
		for _, name := range []string{"XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
			dir := os.Getenv(name)
			if !filepath.IsAbs(dir) {
				return fmt.Errorf("the Flatpak's %s is %q, not an absolute path", name, dir)
			}
			dirs[name] = dir
		}
		l.Root = filepath.Join(dirs["XDG_DATA_HOME"], l.App, l.Instance)
		l.derive()
		l.Cache = filepath.Join(dirs["XDG_CACHE_HOME"], l.App, l.Instance)
		l.HostKey = filepath.Join(l.Control, "datakey.file")
		l.KeyringKey = filepath.Join(l.Control, "datakey.keyring")
		l.Snapshot = filepath.Join(l.Root, "snapshot")
		l.Runtime = filepath.Join(dirs["XDG_RUNTIME_DIR"], "app", DesktopID, l.Instance)
		l.ControlEndpoint = filepath.Join(l.Runtime, "control.sock")
	case Android:
		// The app's shell names its files directory in XDG_DATA_HOME, as
		// Flatpak names the app's own (M12), and everything lives there,
		// the cache too: Android empties an app's cache directory when
		// storage runs low, even under the running app, and the cache's
		// database can't lose its files.
		dataHome := os.Getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(dataHome) {
			return fmt.Errorf("the Android app's XDG_DATA_HOME is %q, not an absolute path", dataHome)
		}
		l.Root = filepath.Join(dataHome, l.App, l.Instance)
		l.derive()
		l.HostKey = filepath.Join(l.Control, "datakey.keystore")
		l.Runtime = filepath.Join(l.Root, "run")
		l.ControlEndpoint = filepath.Join(l.Runtime, "control.sock")
	default:
		return fmt.Errorf("no layout for the %s", l.Form)
	}
	return nil
}

// devBase is $XDG_DATA_HOME/<app>-dev, or ~/.local/share/<app>-dev.
func devBase(app string) (string, error) {
	if os.Geteuid() == 0 {
		return "", fmt.Errorf("refusing to run a development instance as root")
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" || !filepath.IsAbs(dataHome) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		if !filepath.IsAbs(home) {
			return "", fmt.Errorf("home directory is not absolute: %q", home)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, app+"-dev"), nil
}
