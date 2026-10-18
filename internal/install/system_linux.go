package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
)

// minimumSystemd is the first release with LoadCredentialEncrypted=.
const minimumSystemd = 250

// lxcDropIn is the drop-in LXC adds to every service in an unprivileged
// container, under the unit directories in /run and /etc.
const lxcDropIn = "service.d/zzz-lxc-service.conf"

// serviceStopTimeout bounds waiting for a stop; the unit's TimeoutStopSec
// makes systemd escalate to SIGKILL before it.
const serviceStopTimeout = 30 * time.Second

type linuxSystem struct{ app string }

// NewSystem returns this platform's System.
func NewSystem(app string) System { return linuxSystem{app: app} }

func (s linuxSystem) Layout(name string) (layout.Layout, error) {
	return layout.New(s.app, name, layout.Installed)
}

func (linuxSystem) CheckAdmin() error {
	if os.Geteuid() != 0 {
		return errors.New("this changes the installation; run it with sudo")
	}
	return nil
}

func (linuxSystem) Preflight() error {
	if err := checkDistribution("/"); err != nil {
		return err
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return errors.New("Dens needs systemd as the init system")
	}
	out, err := exec.Command("systemctl", "--version").Output()
	if err != nil {
		return fmt.Errorf("read the systemd version: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return fmt.Errorf("unexpected systemctl --version output %q", out)
	}
	version, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("unexpected systemd version %q", fields[1])
	}
	if version < minimumSystemd {
		return fmt.Errorf("Dens needs systemd %d or newer for encrypted credentials; this system has %d", minimumSystemd, version)
	}
	for _, tool := range []string{"systemd-creds", "systemd-sysusers", "userdel"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is missing", tool)
		}
	}
	return checkLXCDropIn("/")
}

// checkDistribution refuses the distributions an install would fail on
// halfway, on a path it writes that they keep read-only, before anything
// changes (M8).
func checkDistribution(root string) error {
	id, err := osReleaseID(root)
	if err != nil {
		return err
	}
	switch id {
	case "nixos":
		return errors.New("Dens doesn't install on NixOS yet: its /etc/systemd/system, where the service goes, is in the read-only Nix store")
	case "steamos":
		return errors.New("Dens doesn't install on SteamOS yet: an update replaces its read-only root, and what an install adds with it")
	}
	return nil
}

// osReleaseID reads the distribution's ID from os-release under root:
// /etc/os-release, or /usr/lib/os-release where that's missing.
func osReleaseID(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "etc/os-release"))
	if errors.Is(err, fs.ErrNotExist) {
		data, err = os.ReadFile(filepath.Join(root, "usr/lib/os-release"))
	}
	if err != nil {
		return "", fmt.Errorf("read os-release to tell the distribution: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "ID="); ok {
			return strings.Trim(value, `"'`), nil
		}
	}
	return "", nil
}

// checkLXCDropIn refuses to go on in a container where LXC's drop-in for
// every service is in effect (M8). It turns off the unit's sandbox and its
// encrypted credential, which carries the data key, so the service would
// fail to start past the point of no return. A file of the same name in
// /etc takes its place, as a link to /dev/null does.
func checkLXCDropIn(root string) error {
	exists := func(dir string) (bool, error) {
		_, err := os.Lstat(filepath.Join(root, dir, lxcDropIn))
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	lxc, err := exists("run/systemd/system")
	if err != nil || !lxc {
		return err
	}
	masked, err := exists("etc/systemd/system")
	if err != nil || masked {
		return err
	}
	return fmt.Errorf("this container's LXC drop-in, /run/systemd/system/%[1]s, would turn off the service's sandbox "+
		"and the credential that carries its data key. Mask it first:\n"+
		"  mkdir -p /etc/systemd/system/service.d\n"+
		"  ln -s /dev/null /etc/systemd/system/%[1]s\n"+
		"  systemctl daemon-reload", lxcDropIn)
}

func (linuxSystem) DesktopUser(override string) (host.Identity, error) {
	var user host.Identity
	var err error
	switch {
	case override != "":
		user, err = host.LookupUser(override)
	case os.Getenv("SUDO_UID") != "":
		user, err = host.LookupUserID(os.Getenv("SUDO_UID"))
	default:
		return host.Identity{}, errors.New("can't tell which desktop user will use Dens: run this with sudo from that account, or pass --user NAME")
	}
	if err != nil {
		return host.Identity{}, err
	}
	if user.ID == "0" {
		return host.Identity{}, errors.New("the desktop user can't be root: run this with sudo from your own account, or pass --user NAME")
	}
	return user, nil
}

func (linuxSystem) PrepareRoot(l layout.Layout) error {
	for _, dir := range []string{filepath.Dir(l.Root), l.Root} {
		if err := ensureRootDir(dir, 0o755); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(l.Control); errors.Is(err, fs.ErrNotExist) {
		return ensureRootDir(l.Control, 0o700)
	}
	return checkNotSymlink(l.Control)
}

func (linuxSystem) EnsureAccount(l layout.Layout) (bool, error) {
	if _, err := user.Lookup(l.Account); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(l.SysusersFile), 0o755); err != nil {
		return false, err
	}
	entry := fmt.Sprintf("# Written by dens install.\nu %s - \"Dens instance %s\" - -\n", l.Account, l.Instance)
	if err := maintenance.WriteFileAtomic(l.SysusersFile, []byte(entry), chmodTo(0o644)); err != nil {
		return false, err
	}
	if err := run("systemd-sysusers", l.SysusersFile); err != nil {
		_ = os.Remove(l.SysusersFile)
		return false, err
	}
	if _, err := user.Lookup(l.Account); err != nil {
		return false, fmt.Errorf("systemd-sysusers didn't create %s: %w", l.Account, err)
	}
	return true, nil
}

func (linuxSystem) RemoveAccount(l layout.Layout) error {
	var joined error
	if _, err := user.Lookup(l.Account); err == nil {
		joined = run("userdel", l.Account)
	}
	if err := os.Remove(l.SysusersFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		joined = errors.Join(joined, err)
	}
	return joined
}

func (linuxSystem) InstallBinary(l layout.Layout, src string) (func() error, error) {
	if same(src, l.Binary) {
		return nil, nil
	}
	previous, err := os.ReadFile(l.Binary)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	hadPrevious := err == nil
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	// Staging inside the target directory gives the file that directory's
	// SELinux label, and makes the rename atomic.
	if err := maintenance.WriteFileAtomic(l.Binary, data, chmodTo(0o755)); err != nil {
		return nil, err
	}
	return func() error {
		if !hadPrevious {
			return os.Remove(l.Binary)
		}
		return maintenance.WriteFileAtomic(l.Binary, previous, chmodTo(0o755))
	}, nil
}

func (linuxSystem) InstallCosign(l layout.Layout, src string) error {
	if same(src, l.Cosign) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.Cosign), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return maintenance.WriteFileAtomic(l.Cosign, data, chmodTo(0o755))
}

func (s linuxSystem) RegisterService(l layout.Layout) (func() error, error) {
	unit := unitTemplate(s.app, l)
	previous, err := os.ReadFile(l.UnitFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	hadPrevious := err == nil
	if !hadPrevious || !bytes.Equal(previous, unit) {
		if err := maintenance.WriteFileAtomic(l.UnitFile, unit, chmodTo(0o644)); err != nil {
			return nil, err
		}
		if err := run("systemctl", "daemon-reload"); err != nil {
			return nil, err
		}
	}
	wasEnabled := exec.Command("systemctl", "is-enabled", "--quiet", l.ServiceName).Run() == nil
	if !wasEnabled {
		if err := run("systemctl", "enable", l.ServiceName); err != nil {
			return nil, err
		}
	}
	return func() error {
		var joined error
		if !wasEnabled {
			joined = run("systemctl", "disable", l.ServiceName)
		}
		switch {
		case !hadPrevious:
			joined = errors.Join(joined, os.Remove(l.UnitFile))
		case !bytes.Equal(previous, unit):
			joined = errors.Join(joined, maintenance.WriteFileAtomic(l.UnitFile, previous, chmodTo(0o644)))
		}
		return errors.Join(joined, run("systemctl", "daemon-reload"))
	}, nil
}

func (linuxSystem) ServiceAccount(l layout.Layout) (host.Identity, error) {
	return host.LookupUser(l.Account)
}

func (linuxSystem) Secure(l layout.Layout, account host.Identity) error {
	uid, gid, err := accountIDs(account)
	if err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(l.Root), l.Root} {
		if err := setOwnership(dir, true, 0, 0, 0o755); err != nil {
			return err
		}
	}
	if err := setOwnership(l.Control, true, 0, gid, layout.ControlDirMode); err != nil {
		return err
	}
	for _, file := range []string{l.State, l.LifecycleLock, l.InstanceConfig} {
		if err := setOwnership(file, false, 0, gid, layout.ControlFileMode); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, file := range []string{l.OperationLock, l.MaintenanceLog, l.HostKey} {
		if err := setOwnership(file, false, 0, 0, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if _, err := os.Lstat(l.Data); errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(l.Data, layout.DataDirMode); err != nil {
			return err
		}
	}
	return setOwnership(l.Data, true, uid, gid, layout.DataDirMode)
}

func (linuxSystem) WriteControlFile(l layout.Layout, path string, data []byte, account host.Identity) error {
	prepare := chmodTo(0o600)
	if account.ID != "" {
		_, gid, err := accountIDs(account)
		if err != nil {
			return err
		}
		prepare = func(temp string) error {
			if err := os.Chown(temp, 0, gid); err != nil {
				return err
			}
			return os.Chmod(temp, layout.ControlFileMode)
		}
	}
	return maintenance.WriteFileAtomic(path, data, prepare)
}

func (linuxSystem) WrapHostKey(l layout.Layout, key []byte) error {
	return host.WrapHostKey(l.HostKey, key)
}

// ConfigureFirewall leaves Linux firewalls alone: which of them a distro
// runs, and its policy, is the owner's to manage. The docs list the ports.
func (linuxSystem) ConfigureFirewall(layout.Layout, *instance.Config) error { return nil }

// RegisterExtras has nothing to do: /usr/local/bin is already on PATH.
func (linuxSystem) RegisterExtras(layout.Layout) error { return nil }

// RegisterHelper has the desktop start the shortcut helper at sign-in, for
// every account (it stops where no instance answers its account), and
// names Dens to the desktop: the .desktop file its global shortcuts portal
// needs, which also opens the page from the desktop's menu, and its icon.
func (linuxSystem) RegisterHelper(l layout.Layout, _ host.Identity) (func() error, error) {
	files := []struct {
		path string
		data []byte
	}{
		{l.AutostartFile, []byte(fmt.Sprintf(autostartEntry, l.Binary, shortcuts.Command, layout.DesktopID))},
		{l.DesktopFile, []byte(fmt.Sprintf(desktopEntry, l.Binary, layout.DesktopID))},
		{l.IconFile, []byte(build.Mark + "\n")},
	}
	var undo []func() error
	rollback := func() error {
		var joined error
		for i := len(undo) - 1; i >= 0; i-- {
			joined = errors.Join(joined, undo[i]())
		}
		return joined
	}
	for _, f := range files {
		previous, err := os.ReadFile(f.path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, errors.Join(err, rollback())
		}
		existed := err == nil
		if existed && bytes.Equal(previous, f.data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return nil, errors.Join(err, rollback())
		}
		if err := maintenance.WriteFileAtomic(f.path, f.data, chmodTo(0o644)); err != nil {
			return nil, errors.Join(err, rollback())
		}
		path := f.path
		if existed {
			undo = append(undo, func() error { return maintenance.WriteFileAtomic(path, previous, chmodTo(0o644)) })
		} else {
			undo = append(undo, func() error { return os.Remove(path) })
		}
	}
	return rollback, nil
}

// autostartEntry starts the helper as the desktop session starts. GNOME and
// KDE run it as the systemd user unit app-<its name>@autostart.service,
// from which older portals take the helper's app ID.
const autostartEntry = `[Desktop Entry]
Type=Application
Name=Dens shortcuts
Comment=Carries Dens's voice keys from other programs to its calls
Exec=%s %s
Icon=%s
NoDisplay=true
X-GNOME-Autostart-enabled=true
`

// desktopEntry names Dens to the desktop, and opens its page.
const desktopEntry = `[Desktop Entry]
Type=Application
Name=Dens
GenericName=Chat
Comment=Self-hosted chat for small communities
Exec=%s open
Icon=%s
Terminal=false
Categories=Network;Chat;InstantMessaging;
`

// StartHelper starts the helper in the desktop user's session now, through
// their systemd user manager, if they're signed in to a desktop. An update
// restarts it on the new binary.
func (linuxSystem) StartHelper(l layout.Layout, user host.Identity) error {
	machine := "--machine=" + user.Name + "@"
	env, err := exec.Command("systemctl", "--user", machine, "show-environment").Output()
	if err != nil {
		return fmt.Errorf("%s isn't signed in", user.Name)
	}
	if !bytes.Contains(env, []byte("\nWAYLAND_DISPLAY=")) && !bytes.Contains(env, []byte("\nDISPLAY=")) &&
		!bytes.HasPrefix(env, []byte("WAYLAND_DISPLAY=")) && !bytes.HasPrefix(env, []byte("DISPLAY=")) {
		return fmt.Errorf("%s has no desktop session", user.Name)
	}
	// The autostart entry's unit comes from systemd's generator, which runs
	// again on a reload.
	if err := run("systemctl", "--user", machine, "daemon-reload"); err != nil {
		return err
	}
	return run("systemctl", "--user", machine, "restart", "app-"+layout.DesktopID+"@autostart.service")
}

func (linuxSystem) ServiceRunning(l layout.Layout) (bool, error) {
	state, err := host.ServiceStatus(l.ServiceName)
	if errors.Is(err, host.ErrServiceNotInstalled) {
		return false, nil
	}
	return state.Running, err
}

func (linuxSystem) StartService(l layout.Layout) error { return host.ServiceStart(l.ServiceName) }

func (linuxSystem) StopService(l layout.Layout) error {
	return host.ServiceStop(l.ServiceName, serviceStopTimeout)
}

func (linuxSystem) UnregisterService(l layout.Layout) error {
	if _, err := os.Stat(l.UnitFile); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	_ = exec.Command("systemctl", "disable", "--now", l.ServiceName).Run()
	_ = exec.Command("systemctl", "reset-failed", l.ServiceName).Run()
	return nil
}

func (linuxSystem) RemoveShared(l layout.Layout) error {
	var joined error
	// A helper still running stops once no instance answers it.
	for _, path := range []string{l.UnitFile, l.Binary, l.Cosign, l.AutostartFile, l.DesktopFile, l.IconFile} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			joined = errors.Join(joined, err)
		}
	}
	_ = os.Remove(filepath.Dir(l.Cosign))
	return errors.Join(joined, run("systemctl", "daemon-reload"))
}

func (linuxSystem) PrepareRestore(l layout.Layout, _ host.Identity) (string, error) {
	dir := fmt.Sprintf("%s.restoring-%d", l.Data, time.Now().UnixNano())
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// AdoptRestored hands extracted files to the service account with private
// modes. Extraction created them as root; nothing in them is a symlink.
func (linuxSystem) AdoptRestored(_ layout.Layout, dir string, account host.Identity) error {
	uid, gid, err := accountIDs(account)
	if err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o600)
		if entry.IsDir() {
			mode = 0o700
		} else if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected file type at %s", path)
		}
		if err := os.Lchown(path, uid, gid); err != nil {
			return err
		}
		return os.Chmod(path, mode)
	})
}

// Plan describes what an install creates here, for --dry-run and the log.
func (s linuxSystem) Plan(l layout.Layout) []string {
	return []string{
		fmt.Sprintf("account       %s (%s)", l.Account, l.SysusersFile),
		fmt.Sprintf("service       %s from %s", l.ServiceName, l.UnitFile),
		fmt.Sprintf("binary        %s", l.Binary),
		fmt.Sprintf("data          %s (control %s, data %s)", l.Root, l.Control, l.Data),
		fmt.Sprintf("data key      %s, bound to this host by systemd-creds", l.HostKey),
		fmt.Sprintf("CLI endpoint  %s", l.ControlEndpoint),
		fmt.Sprintf("shortcuts     %s %s at each desktop sign-in (%s), named by %s", l.Binary, shortcuts.Command, l.AutostartFile, l.DesktopFile),
	}
}

func (linuxSystem) UninstallPlan(l layout.Layout, last bool) []string {
	lines := []string{
		fmt.Sprintf("disables and removes %s", l.ServiceName),
		fmt.Sprintf("deletes the account %s and %s", l.Account, l.SysusersFile),
	}
	if last {
		lines = append(lines, fmt.Sprintf("removes %s, %s and %s (no instances remain)", l.Binary, l.UnitFile, l.Cosign))
		lines = append(lines, fmt.Sprintf("removes the shortcut helper's %s, %s and %s", l.AutostartFile, l.DesktopFile, l.IconFile))
	}
	return lines
}

// unitTemplate is the systemd unit every instance runs from. Its hardening
// is the set the platform spike verified on every supported distro.
func unitTemplate(app string, l layout.Layout) []byte {
	stateDir := strings.TrimPrefix(filepath.Dir(l.Root), "/var/lib/")
	return []byte(fmt.Sprintf(`# Written by %[1]s install; the next install or update replaces it.
[Unit]
Description=Dens (%%i)
Documentation=https://dens.chat/
Wants=network-online.target
After=network-online.target

[Service]
Type=notify
User=%[1]s-%%i
Group=%[1]s-%%i
ExecStart=%[2]s service run --instance %%i
LoadCredentialEncrypted=%[3]s:%[4]s/%%i/control/%[5]s
StateDirectory=%[6]s/%%i/data
StateDirectoryMode=0700
RuntimeDirectory=%[6]s/%%i
RuntimeDirectoryMode=0755
UMask=0077
Restart=on-failure
RestartSec=2
RestartPreventExitStatus=%[7]d
TimeoutStartSec=300
TimeoutStopSec=20
LimitCORE=0
LimitNOFILE=65535
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
ProcSubset=pid
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RemoveIPC=yes
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM

[Install]
WantedBy=multi-user.target
`, app, l.Binary, host.HostKeyCredential, filepath.Dir(l.Root), filepath.Base(l.HostKey), stateDir, host.ExitRefused))
}

func accountIDs(account host.Identity) (uid, gid int, err error) {
	u, err := user.LookupId(account.ID)
	if err != nil {
		return 0, 0, fmt.Errorf("look up service account %s: %w", account, err)
	}
	if uid, err = strconv.Atoi(u.Uid); err != nil {
		return 0, 0, err
	}
	if gid, err = strconv.Atoi(u.Gid); err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func ensureRootDir(dir string, mode fs.FileMode) error {
	if err := os.Mkdir(dir, mode); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return setOwnership(dir, true, 0, 0, mode)
}

func checkNotSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
	}
	return nil
}

// setOwnership sets owner, group and mode on a directory or file, refusing
// a symlink or the wrong type rather than following it.
func setOwnership(path string, dir bool, uid, gid int, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
	}
	if dir != info.IsDir() || (!dir && !info.Mode().IsRegular()) {
		return fmt.Errorf("%s is not the expected type", path)
	}
	if err := os.Lchown(path, uid, gid); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func chmodTo(mode fs.FileMode) func(string) error {
	return func(path string) error { return os.Chmod(path, mode) }
}

func same(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (linuxSystem) InstallerName() string { return "install.sh" }

func (linuxSystem) RunInstaller(ctx context.Context, script, instance, releaseURL string, skipVerify bool) error {
	args := []string{script, "--update", "--instance", instance, "--release-url", releaseURL}
	if skipVerify {
		args = append(args, "--skip-verify")
	}
	cmd := exec.CommandContext(ctx, "sh", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
