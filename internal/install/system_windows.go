package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// Windows 11 is build 22000 and later.
const minimumWindowsBuild = 22000

const serviceStopTimeout = 30 * time.Second

// eventSourceKey is where the Application event log registers its sources.
const eventSourceKey = `SYSTEM\CurrentControlSet\Services\EventLog\Application\`

// DACLs. Administrators and SYSTEM always have full control. The service's
// own SID may read the installation root (and so the control files, which
// inherit it) and modify its data directory.
const (
	rootSDDL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	// serviceReadRights is read and execute: FILE_GENERIC_READ | FILE_EXECUTE.
	serviceReadRights = "0x1200a9"
	// serviceModifyRights is read, write, execute and delete.
	serviceModifyRights = "0x1301bf"
)

var (
	modwer                        = windows.NewLazySystemDLL("wer.dll")
	procWerAddExcludedApplication = modwer.NewProc("WerAddExcludedApplication")
	procWerRemoveExcludedApp      = modwer.NewProc("WerRemoveExcludedApplication")
	modwtsapi32                   = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInfo       = modwtsapi32.NewProc("WTSQuerySessionInformationW")
	moduser32                     = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeoutW       = moduser32.NewProc("SendMessageTimeoutW")
)

type windowsSystem struct{ app string }

// NewSystem returns this platform's System.
func NewSystem(app string) System { return windowsSystem{app: app} }

func (s windowsSystem) Layout(name string) (layout.Layout, error) {
	return layout.New(s.app, name, false)
}

func (windowsSystem) CheckAdmin() error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	if !token.IsElevated() {
		return errors.New("this changes the installation; run it from an elevated terminal (Run as administrator)")
	}
	return nil
}

func (windowsSystem) Preflight() error {
	version := windows.RtlGetVersion()
	if version.MajorVersion < 10 || version.BuildNumber < minimumWindowsBuild {
		return fmt.Errorf("Dens needs Windows 11; this is Windows %d.%d build %d",
			version.MajorVersion, version.MinorVersion, version.BuildNumber)
	}
	return nil
}

func (windowsSystem) DesktopUser(override string) (host.Identity, error) {
	if override != "" {
		return host.LookupUser(override)
	}
	name, err := consoleUser()
	if err != nil {
		return host.Identity{}, err
	}
	if name == "" {
		return host.Identity{}, errors.New("nobody is signed in at the console; pass --user NAME")
	}
	return host.LookupUser(name)
}

// consoleUser names the user signed in at the physical console, who is the
// desktop user even when an administrator elevated with other credentials.
func consoleUser() (string, error) {
	session := windows.WTSGetActiveConsoleSessionId()
	if session == 0xFFFFFFFF {
		return "", nil
	}
	query := func(class uint32) (string, error) {
		var buf *uint16
		var size uint32
		r, _, err := procWTSQuerySessionInfo.Call(0, uintptr(session), uintptr(class), uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&size)))
		if r == 0 {
			return "", fmt.Errorf("query console session: %w", err)
		}
		defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(buf)))
		return windows.UTF16PtrToString(buf), nil
	}
	const wtsUserName, wtsDomainName = 5, 7
	userName, err := query(wtsUserName)
	if err != nil || userName == "" {
		return "", err
	}
	domain, err := query(wtsDomainName)
	if err != nil {
		return "", err
	}
	if domain == "" {
		return userName, nil
	}
	return domain + `\` + userName, nil
}

func (windowsSystem) PrepareRoot(l layout.Layout) error {
	for _, dir := range []string{filepath.Dir(l.Root), l.Root} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := setDACL(filepath.Dir(l.Root), rootSDDL); err != nil {
		return err
	}
	if err := setDACLIfNew(l.Root, rootSDDL); err != nil {
		return err
	}
	return os.MkdirAll(l.Control, 0o700)
}

// EnsureAccount has nothing to create: the service's virtual account
// exists as soon as the service does.
func (windowsSystem) EnsureAccount(layout.Layout) (bool, error) { return false, nil }

func (windowsSystem) RemoveAccount(layout.Layout) error { return nil }

func (windowsSystem) InstallBinary(l layout.Layout, src string) (func() error, error) {
	if same(src, l.Binary) {
		return nil, nil
	}
	if err := os.MkdirAll(l.BinaryDir, 0o755); err != nil {
		return nil, err
	}
	removeStaleBinaries(l)
	// A running executable can't be overwritten but can be renamed, and
	// dens update itself runs from it.
	var setAside string
	if _, err := os.Stat(l.Binary); err == nil {
		setAside = fmt.Sprintf("%s.old-%d", l.Binary, time.Now().UnixNano())
		if err := os.Rename(l.Binary, setAside); err != nil {
			return nil, fmt.Errorf("set the installed binary aside: %w", err)
		}
	}
	data, err := os.ReadFile(src)
	if err == nil {
		err = maintenance.WriteFileAtomic(l.Binary, data, nil)
	}
	if err != nil {
		if setAside != "" {
			_ = os.Rename(setAside, l.Binary)
		}
		return nil, err
	}
	return func() error {
		if err := os.Remove(l.Binary); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if setAside != "" {
			return os.Rename(setAside, l.Binary)
		}
		return nil
	}, nil
}

// removeStaleBinaries deletes binaries set aside by earlier updates, except
// ones still running.
func removeStaleBinaries(l layout.Layout) {
	matches, _ := filepath.Glob(l.Binary + ".old-*")
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

func (windowsSystem) InstallCosign(l layout.Layout, src string) error {
	if same(src, l.Cosign) {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return maintenance.WriteFileAtomic(l.Cosign, data, nil)
}

func (windowsSystem) RegisterService(l layout.Layout) (func() error, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect to the service manager: %w", err)
	}
	defer m.Disconnect()
	config := mgr.Config{
		ServiceType:      windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
		ErrorControl:     mgr.ErrorNormal,
		DisplayName:      fmt.Sprintf("Dens (%s)", l.Instance),
		Description:      "Dens chat service. Runs as its own restricted account.",
		ServiceStartName: l.Account,
		SidType:          windows.SERVICE_SID_TYPE_RESTRICTED,
	}
	args := []string{"service", "run", "--instance", l.Instance}

	var undo func() error
	s, err := m.OpenService(l.ServiceName)
	if err == nil {
		previous, err := s.Config()
		if err != nil {
			s.Close()
			return nil, err
		}
		// Keep the administrator's choice of start type.
		config.StartType = previous.StartType
		config.DelayedAutoStart = previous.DelayedAutoStart
		config.BinaryPathName = commandLine(l.Binary, args)
		if err := s.UpdateConfig(config); err != nil {
			s.Close()
			return nil, fmt.Errorf("update service %s: %w", l.ServiceName, err)
		}
		undo = func() error {
			m, err := mgr.Connect()
			if err != nil {
				return err
			}
			defer m.Disconnect()
			s, err := m.OpenService(l.ServiceName)
			if err != nil {
				return err
			}
			defer s.Close()
			return s.UpdateConfig(previous)
		}
	} else {
		s, err = m.CreateService(l.ServiceName, l.Binary, config, args...)
		if err != nil {
			return nil, fmt.Errorf("create service %s: %w", l.ServiceName, err)
		}
		undo = func() error { return errors.Join(deleteService(l.ServiceName), removeEventSource(l.ServiceName)) }
	}
	defer s.Close()
	if err := setRequiredPrivileges(s.Handle, "SeChangeNotifyPrivilege"); err != nil {
		_ = undo()
		return nil, fmt.Errorf("limit the service's privileges: %w", err)
	}
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400); err != nil {
		_ = undo()
		return nil, fmt.Errorf("set the service's recovery actions: %w", err)
	}
	if err := registerEventSource(l.ServiceName); err != nil {
		_ = undo()
		return nil, fmt.Errorf("register the service's event log source: %w", err)
	}
	return undo, nil
}

// registerEventSource lets the service record why it stopped in the
// Application event log under its own name (see host.RunService).
func registerEventSource(name string) error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, eventSourceKey+name, registry.QUERY_VALUE)
	if err == nil {
		key.Close()
		return nil
	}
	return eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
}

func removeEventSource(name string) error {
	if err := eventlog.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove event log source %s: %w", name, err)
	}
	return nil
}

func (windowsSystem) ServiceAccount(l layout.Layout) (host.Identity, error) {
	return host.LookupUser(l.Account)
}

func (windowsSystem) Secure(l layout.Layout, account host.Identity) error {
	sddl := rootSDDL + fmt.Sprintf("(A;OICI;%s;;;%s)", serviceReadRights, account.ID)
	if err := setDACL(l.Root, sddl); err != nil {
		return err
	}
	if err := os.MkdirAll(l.Data, 0o700); err != nil {
		return err
	}
	return setDACL(l.Data, dataSDDL(account))
}

func dataSDDL(account host.Identity) string {
	return rootSDDL + fmt.Sprintf("(A;OICI;%s;;;%s)", serviceModifyRights, account.ID)
}

// WriteControlFile writes a file that inherits the control directory's
// DACL: administrators may change it, the service may only read it.
func (windowsSystem) WriteControlFile(_ layout.Layout, path string, data []byte, _ host.Identity) error {
	return maintenance.WriteFileAtomic(path, data, nil)
}

func (windowsSystem) WrapHostKey(l layout.Layout, key []byte) error {
	return host.WrapHostKey(l.HostKey, key)
}

func (windowsSystem) ConfigureFirewall(l layout.Layout, cfg instance.Config) error {
	for _, proto := range []string{"UDP", "TCP"} {
		name := firewallRuleName(l, proto)
		_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+name).Run()
		if !cfg.Den.Enabled {
			continue
		}
		port := cfg.Den.MediaUDPPort
		if proto == "TCP" {
			port = cfg.Den.MediaTCPPort
		}
		out, err := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
			"name="+name, "dir=in", "action=allow", "enable=yes",
			"program="+l.Binary, "protocol="+proto, fmt.Sprintf("localport=%d", port)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("add firewall rule %q: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func firewallRuleName(l layout.Layout, proto string) string {
	return fmt.Sprintf("Dens (%s) media %s", l.Instance, proto)
}

func (windowsSystem) RegisterExtras(l layout.Layout) error {
	if err := werExclusion(filepath.Base(l.Binary), true); err != nil {
		return fmt.Errorf("exclude %s from crash reporting: %w", filepath.Base(l.Binary), err)
	}
	return setSystemPath(l.BinaryDir, true)
}

func (windowsSystem) ServiceRunning(l layout.Layout) (bool, error) {
	state, err := host.ServiceStatus(l.ServiceName)
	if errors.Is(err, host.ErrServiceNotInstalled) {
		return false, nil
	}
	return state.Running, err
}

func (windowsSystem) StartService(l layout.Layout) error { return host.ServiceStart(l.ServiceName) }

func (windowsSystem) StopService(l layout.Layout) error {
	return host.ServiceStop(l.ServiceName, serviceStopTimeout)
}

func (windowsSystem) UnregisterService(l layout.Layout) error {
	for _, proto := range []string{"UDP", "TCP"} {
		_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+firewallRuleName(l, proto)).Run()
	}
	return errors.Join(deleteService(l.ServiceName), removeEventSource(l.ServiceName))
}

func (windowsSystem) RemoveShared(l layout.Layout) error {
	joined := errors.Join(setSystemPath(l.BinaryDir, false), werExclusion(filepath.Base(l.Binary), false))
	removeStaleBinaries(l)
	_ = os.Remove(l.Cosign)
	if err := os.Remove(l.Binary); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// The uninstaller itself runs from it: remove it at the next boot.
		setAside := fmt.Sprintf("%s.old-%d", l.Binary, time.Now().UnixNano())
		if err := os.Rename(l.Binary, setAside); err != nil {
			return errors.Join(joined, err)
		}
		joined = errors.Join(joined, deleteAtReboot(setAside), deleteAtReboot(l.BinaryDir))
		return joined
	}
	if err := os.Remove(l.BinaryDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		joined = errors.Join(joined, deleteAtReboot(l.BinaryDir))
	}
	return joined
}

func (windowsSystem) PrepareRestore(l layout.Layout, account host.Identity) (string, error) {
	dir := fmt.Sprintf("%s.restoring-%d", l.Data, time.Now().UnixNano())
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	// Extracted files inherit the data directory's permissions.
	if err := setDACL(dir, dataSDDL(account)); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func (windowsSystem) AdoptRestored(layout.Layout, string, host.Identity) error { return nil }

func (windowsSystem) Plan(l layout.Layout) []string {
	return []string{
		fmt.Sprintf("service       %s, running as %s (restricted SID, only SeChangeNotifyPrivilege)", l.ServiceName, l.Account),
		fmt.Sprintf("binary        %s (added to the system PATH)", l.Binary),
		fmt.Sprintf("data          %s (control %s, data %s)", l.Root, l.Control, l.Data),
		fmt.Sprintf("data key      %s, machine-scope DPAPI", l.HostKey),
		fmt.Sprintf("CLI endpoint  %s", l.ControlEndpoint),
	}
}

func (windowsSystem) UninstallPlan(l layout.Layout, last bool) []string {
	lines := []string{fmt.Sprintf("stops and deletes the service %s, its event log source and its firewall rules", l.ServiceName)}
	if last {
		lines = append(lines, fmt.Sprintf("removes %s from the system PATH and deletes it (no instances remain)", l.BinaryDir))
	}
	return lines
}

func commandLine(exe string, args []string) string {
	parts := []string{windows.EscapeArg(exe)}
	for _, arg := range args {
		parts = append(parts, windows.EscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

func deleteService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return err
	}
	if status, err := s.Query(); err == nil && status.State != svc.Stopped {
		_ = host.ServiceStop(name, serviceStopTimeout)
	}
	err = s.Delete()
	s.Close()
	if err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return fmt.Errorf("delete service %s: %w", name, err)
	}
	// Deletion completes when the last handle closes; wait so a
	// reinstall right after doesn't find it marked for delete.
	for range 50 {
		if s, err := m.OpenService(name); err != nil {
			return nil
		} else {
			s.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func setRequiredPrivileges(h windows.Handle, privileges ...string) error {
	var multi []uint16
	for _, p := range privileges {
		u, err := windows.UTF16FromString(p)
		if err != nil {
			return err
		}
		multi = append(multi, u...)
	}
	multi = append(multi, 0)
	info := struct{ RequiredPrivileges *uint16 }{&multi[0]}
	return windows.ChangeServiceConfig2(h, windows.SERVICE_CONFIG_REQUIRED_PRIVILEGES_INFO, (*byte)(unsafe.Pointer(&info)))
}

// setDACL replaces path's DACL with sddl, protected from inheritance, and
// lets the change flow to children that inherit.
func setDACL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("set permissions on %s: %w", path, err)
	}
	return nil
}

// setDACLIfNew protects a directory whose DACL still inherits from its
// parent; one already protected keeps what Secure set.
func setDACLIfNew(path, sddl string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		return nil
	}
	return setDACL(path, sddl)
}

func werExclusion(exe string, add bool) error {
	proc := procWerRemoveExcludedApp
	if add {
		proc = procWerAddExcludedApplication
	}
	if err := proc.Find(); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(name)), 1); r != 0 && add {
		return fmt.Errorf("HRESULT %#x", uint32(r))
	}
	return nil
}

// setSystemPath adds dir to, or removes it from, the machine PATH, and tells
// running programs the environment changed.
func setSystemPath(dir string, add bool) error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, valueType, err := key.GetStringValue("Path")
	if err != nil {
		return err
	}
	var kept []string
	present := false
	for _, entry := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimRight(entry, `\`), strings.TrimRight(dir, `\`)) {
			present = true
			if !add {
				continue
			}
		}
		if entry != "" {
			kept = append(kept, entry)
		}
	}
	if add == present {
		return nil
	}
	if add {
		kept = append(kept, dir)
	}
	updated := strings.Join(kept, ";")
	if valueType == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", updated)
	} else {
		err = key.SetStringValue("Path", updated)
	}
	if err != nil {
		return err
	}
	environment, _ := windows.UTF16PtrFromString("Environment")
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	var result uintptr
	_, _, _ = procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(environment)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
	return nil
}

func deleteAtReboot(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(p, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
}

func same(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

func (windowsSystem) InstallerName() string { return "install.ps1" }

func (windowsSystem) RunInstaller(ctx context.Context, script, instance string, env []string) error {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", script, "-Update", "-Instance", instance)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}
