package layout

import (
	"fmt"
	"os"
	"path/filepath"
)

func (l *Layout) resolve() error {
	if l.Dev {
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
		return nil
	}
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
