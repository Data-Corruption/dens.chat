//go:build windows

// Command winsvc is a throwaway experiment for the Dens Windows platform
// layer. "orchestrate" (elevated) installs a service that runs as a virtual
// account with a restricted service SID, waits while the desktop-user
// "client" probes it, then tests crash recovery and graceful stop and removes
// everything. "service" is what the SCM runs; "child" is its low-priority
// subprocess. See ../README.md.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName = "dens-spike-main"
	programDir  = `C:\Program Files\DensSpike`
	stateRoot   = `C:\ProgramData\DensSpike`
	stateDir    = `C:\ProgramData\DensSpike\main`
	pipePrefix  = `\\.\pipe\dens-spike-main-`

	exclusivePort = 18484 // IPv4 loopback with SO_EXCLUSIVEADDRUSE
	sharedPort    = 18485 // IPv4 loopback with default options
	dualPort      = 18494 // both loopbacks with SO_EXCLUSIVEADDRUSE
	udpPort       = 17881
	tcpPort       = 17882

	// Read and write data on a pipe without FILE_APPEND_DATA, which for pipes
	// is FILE_CREATE_PIPE_INSTANCE: FILE_GENERIC_READ | FILE_GENERIC_WRITE
	// minus 0x4.
	pipeClientGrant = 0x0012019b
	// What a client asks for when opening the pipe: FILE_GENERIC_READ |
	// FILE_WRITE_DATA. GENERIC_WRITE would also ask for
	// FILE_CREATE_PIPE_INSTANCE and be refused.
	pipeClientAccess = 0x0012008b

	soExclusiveAddrUse = ^windows.SO_REUSEADDR
)

var (
	modadvapi32                    = windows.NewLazySystemDLL("advapi32.dll")
	procImpersonateNamedPipeClient = modadvapi32.NewProc("ImpersonateNamedPipeClient")
	procLookupPrivilegeNameW       = modadvapi32.NewProc("LookupPrivilegeNameW")
	procIsTokenRestricted          = modadvapi32.NewProc("IsTokenRestricted")
	modwer                         = windows.NewLazySystemDLL("wer.dll")
	procWerAddExcludedApplication  = modwer.NewProc("WerAddExcludedApplication")
	procWerRemoveExcludedApp       = modwer.NewProc("WerRemoveExcludedApplication")
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: winsvc orchestrate|service|client|child|bind-precedence [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "orchestrate":
		err = runOrchestrate(os.Args[2:])
	case "service":
		err = runService(os.Args[2:])
	case "client":
		err = runClient(os.Args[2:])
	case "child":
		err = runChild()
	case "bind-precedence":
		err = runBindPrecedence()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// Reports ---------------------------------------------------------------------

type check struct {
	Name string `json:"name"`
	Want string `json:"want,omitempty"`
	Got  string `json:"got"`
	Pass *bool  `json:"pass,omitempty"`
}

type report struct {
	Role   string          `json:"role"`
	Checks []check         `json:"checks"`
	Nested json.RawMessage `json:"nested,omitempty"`
}

func (r *report) info(name, got string) {
	r.Checks = append(r.Checks, check{Name: name, Got: got})
}

func (r *report) expect(name string, wantOK bool, err error, detail string) {
	got := "ok"
	if err != nil {
		got = "error: " + err.Error()
	}
	if detail != "" {
		got += " (" + detail + ")"
	}
	want := "fail"
	if wantOK {
		want = "ok"
	}
	pass := (err == nil) == wantOK
	r.Checks = append(r.Checks, check{Name: name, Want: want, Got: got, Pass: &pass})
}

func errString(err error) string {
	if err == nil {
		return "ok"
	}
	return "error: " + err.Error()
}

func errSuffix(err error) string {
	if err == nil {
		return ""
	}
	return " error: " + err.Error()
}

type logger struct {
	mu sync.Mutex
	w  io.Writer
}

func openLogger(path string) *logger {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return &logger{w: io.Discard}
	}
	return &logger{w: f}
}

func (l *logger) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, time.Now().Format("15:04:05.000")+" "+format+"\n", args...)
}

func fingerprint(b []byte) string {
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%d bytes, sha256 %s", len(b), hex.EncodeToString(sum[:8]))
}

// Orchestrator (elevated) -------------------------------------------------------

func runOrchestrate(args []string) error {
	fs := flag.NewFlagSet("orchestrate", flag.ExitOnError)
	user := fs.String("user", "", "desktop user's SID")
	work := fs.String("work", "", "work directory shared with the desktop user")
	wait := fs.Duration("wait", 20*time.Minute, "how long to wait for the client before cleaning up")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lg := openLogger(filepath.Join(*work, "orchestrate.log"))
	rep := &report{Role: "orchestrate"}
	save := func() {
		data, _ := json.MarshalIndent(rep, "", "  ")
		_ = os.WriteFile(filepath.Join(*work, "orchestrate.json"), data, 0o644)
	}
	touch := func(name string) { _ = os.WriteFile(filepath.Join(*work, name), nil, 0o644) }
	defer func() {
		save()
		touch("done")
	}()

	rep.info("elevated", fmt.Sprint(isElevated()))
	if !isElevated() {
		touch("failed")
		return errors.New("not elevated")
	}
	lg.printf("cleaning up any earlier run")
	uninstall(&report{}, *work)

	lg.printf("installing")
	err := install(rep, *user, *work)
	save()
	if err != nil {
		lg.printf("install failed: %v", err)
		touch("failed")
	} else {
		touch("installed")
		lg.printf("waiting for the client")
		waitForFile(filepath.Join(*work, "uninstall.now"), *wait)
		testRecovery(rep)
		testStop(rep)
	}
	lg.printf("uninstalling")
	uninstall(rep, *work)
	lg.printf("done")
	return nil
}

func install(rep *report, user, work string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(programDir, 0o755); err != nil {
		return err
	}
	exe := filepath.Join(programDir, "dens-spike.exe")
	err = copyFile(self, exe)
	rep.expect("install binary", true, err, exe)
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		StartType:        mgr.StartManual,
		ErrorControl:     mgr.ErrorNormal,
		DisplayName:      "Dens spike (main)",
		Description:      "Throwaway experiment for the Dens Windows platform layer.",
		ServiceStartName: `NT SERVICE\` + serviceName,
		SidType:          windows.SERVICE_SID_TYPE_RESTRICTED,
	}, "service", "-user", user, "-probe", work)
	rep.expect("create service as a virtual account with a restricted SID type", true, err, "")
	if err != nil {
		return err
	}
	defer s.Close()

	err = setRequiredPrivileges(s.Handle, "SeChangeNotifyPrivilege")
	rep.expect("limit required privileges to SeChangeNotifyPrivilege", true, err, "")
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: time.Second},
		{Type: mgr.ServiceRestart, Delay: time.Second},
		{Type: mgr.ServiceRestart, Delay: time.Second},
	}, 86400)
	rep.expect("restart on failure (recovery actions)", true, err, "")
	if cfg, err := s.Config(); err == nil {
		rep.info("service config", fmt.Sprintf("start name %q, SID type %d", cfg.ServiceStartName, cfg.SidType))
	}

	sid, _, _, err := windows.LookupSID("", `NT SERVICE\`+serviceName)
	if err != nil {
		rep.expect("look up service SID", true, err, "")
		return err
	}
	rep.expect("look up service SID", true, nil, sid.String())

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	err = setProtectedDACL(stateRoot, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	rep.expect("protected DACL on state root", true, err, currentSDDL(stateRoot))
	err = setProtectedDACL(stateDir, fmt.Sprintf("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1301bf;;;%s)", sid))
	rep.expect("protected DACL on state directory", true, err, currentSDDL(stateDir))

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	blob, err := dpapiProtect(key, true)
	rep.expect("DPAPI machine-scope protect of the data key", true, err, fingerprint(key))
	if err != nil {
		return err
	}
	keyPath := filepath.Join(stateDir, "datakey.dpapi")
	if err := os.WriteFile(keyPath, blob, 0o600); err != nil {
		return err
	}
	rep.info("data key blob security", currentSDDL(keyPath))

	test, err := dpapiProtect([]byte("dens-spike machine-scope test"), true)
	if err == nil {
		err = os.WriteFile(filepath.Join(work, "machine-scope-test.dpapi"), test, 0o644)
	}
	rep.expect("machine-scope test blob for the desktop user", true, err, "")

	err = werExclude(true)
	rep.expect("exclude from Windows Error Reporting", true, err, werExcludedValue())

	err = s.Start()
	rep.expect("start service", true, err, "")
	if err != nil {
		return err
	}
	st, err := waitState(s, svc.Running, 30*time.Second)
	rep.expect("service reaches RUNNING", true, err, fmt.Sprintf("pid %d", st.ProcessId))
	return err
}

// testRecovery kills the service three ways, each followed by a check that
// the SCM's recovery actions start it again. taskkill and Stop-Process run
// first, before this process enables SeDebugPrivilege, so they don't inherit
// it.
func testRecovery(rep *report) {
	m, err := mgr.Connect()
	if err != nil {
		rep.info("recovery test", err.Error())
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		rep.info("recovery test", err.Error())
		return
	}
	defer s.Close()
	pid := func() uint32 {
		st, err := s.Query()
		if err != nil || st.State != svc.Running {
			return 0
		}
		return st.ProcessId
	}
	waitRestart := func(name string, oldPID uint32) {
		deadline := time.Now().Add(30 * time.Second)
		var st svc.Status
		var err error
		for {
			st, err = s.Query()
			if err == nil && st.State == svc.Running && st.ProcessId != 0 && st.ProcessId != oldPID {
				break
			}
			if time.Now().After(deadline) {
				err = fmt.Errorf("state %d pid %d after 30s", st.State, st.ProcessId)
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		rep.expect("SCM restarts the service after "+name, true, err, fmt.Sprintf("new pid %d", st.ProcessId))
	}

	old := pid()
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, old)
	if err == nil {
		_ = windows.CloseHandle(h)
	}
	rep.expect("elevated admin opening the service process for terminate without SeDebugPrivilege refused", false, err, fmt.Sprintf("pid %d", old))

	out, err := exec.Command("taskkill.exe", "/F", "/PID", fmt.Sprint(old)).CombinedOutput()
	rep.expect("taskkill /F on the service process", true, err, strings.TrimSpace(string(out)))
	if err == nil {
		waitRestart("taskkill /F", old)
	}

	old = pid()
	out, err = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("Stop-Process -Id %d -Force -ErrorAction Stop", old)).CombinedOutput()
	rep.expect("PowerShell Stop-Process -Force on the service process", true, err, strings.TrimSpace(string(out)))
	if err == nil {
		waitRestart("Stop-Process -Force", old)
	}

	old = pid()
	err = enablePrivilege("SeDebugPrivilege")
	rep.expect("enable SeDebugPrivilege", true, err, "")
	h, err = windows.OpenProcess(windows.PROCESS_TERMINATE, false, old)
	if err == nil {
		err = windows.TerminateProcess(h, 1)
		_ = windows.CloseHandle(h)
	}
	rep.expect("TerminateProcess with SeDebugPrivilege", true, err, fmt.Sprintf("pid %d", old))
	if err == nil {
		waitRestart("TerminateProcess", old)
	}
}

func enablePrivilege(name string) error {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return err
	}
	defer tok.Close()
	var luid windows.LUID
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if err := windows.LookupPrivilegeValue(nil, namePtr, &luid); err != nil {
		return err
	}
	privileges := windows.Tokenprivileges{PrivilegeCount: 1}
	privileges.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	return windows.AdjustTokenPrivileges(tok, false, &privileges, 0, nil, nil)
}

func testStop(rep *report) {
	m, err := mgr.Connect()
	if err != nil {
		rep.info("stop test", err.Error())
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		rep.info("stop test", err.Error())
		return
	}
	defer s.Close()
	started := time.Now()
	_, err = s.Control(svc.Stop)
	st, waitErr := waitState(s, svc.Stopped, 30*time.Second)
	rep.expect("graceful stop through the SCM", true, errors.Join(err, waitErr),
		fmt.Sprintf("%v, exit code %d", time.Since(started).Round(time.Millisecond), st.Win32ExitCode))
}

func uninstall(rep *report, work string) {
	if m, err := mgr.Connect(); err == nil {
		if s, err := m.OpenService(serviceName); err == nil {
			if st, err := s.Query(); err == nil && st.State != svc.Stopped {
				_, _ = s.Control(svc.Stop)
				_, _ = waitState(s, svc.Stopped, 30*time.Second)
			}
			err = s.Delete()
			rep.expect("delete service", true, err, "")
			s.Close()
		}
		m.Disconnect()
	}
	rep.expect("remove WER exclusion", true, werExclude(false), "")
	for _, name := range []string{"service.log", "service-report.json"} {
		_ = copyFile(filepath.Join(stateDir, name), filepath.Join(work, name))
	}
	rep.expect("remove state root", true, removeAllRetry(stateRoot), "")
	rep.expect("remove program directory", true, removeAllRetry(programDir), "")
}

func waitForFile(path string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func waitState(s *mgr.Service, want svc.State, timeout time.Duration) (svc.Status, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, err := s.Query()
		if err != nil {
			return st, err
		}
		if st.State == want {
			return st, nil
		}
		if time.Now().After(deadline) {
			return st, fmt.Errorf("state %d after %v", st.State, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func removeAllRetry(path string) error {
	var err error
	for i := 0; i < 40; i++ {
		if err = os.RemoveAll(path); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
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

func setProtectedDACL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func currentSDDL(path string) string {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err.Error()
	}
	return sd.String()
}

func werExclude(add bool) error {
	proc := procWerRemoveExcludedApp
	if add {
		proc = procWerAddExcludedApplication
	}
	if err := proc.Find(); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString("dens-spike.exe")
	if err != nil {
		return err
	}
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(name)), 1)
	if r != 0 {
		return fmt.Errorf("HRESULT %#x", uint32(r))
	}
	return nil
}

func werExcludedValue() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\Windows Error Reporting\ExcludedApplications`, registry.QUERY_VALUE)
	if err != nil {
		return err.Error()
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("dens-spike.exe")
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("registry value %d", v)
}

// DPAPI --------------------------------------------------------------------------

func dpapiProtect(data []byte, machine bool) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN)
	if machine {
		flags |= windows.CRYPTPROTECT_LOCAL_MACHINE
	}
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, flags, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return bytes.Clone(unsafe.Slice(out.Data, out.Size)), nil
}

func dpapiUnprotect(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, errors.New("empty blob")
	}
	in := windows.DataBlob{Size: uint32(len(blob)), Data: &blob[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return bytes.Clone(unsafe.Slice(out.Data, out.Size)), nil
}

// Tokens -------------------------------------------------------------------------

func processToken() (windows.Token, error) {
	var tok windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok)
	return tok, err
}

func isElevated() bool {
	tok, err := processToken()
	if err != nil {
		return false
	}
	defer tok.Close()
	var elevation, n uint32
	err = windows.GetTokenInformation(tok, windows.TokenElevation, (*byte)(unsafe.Pointer(&elevation)), 4, &n)
	return err == nil && elevation != 0
}

func isTokenRestricted() bool {
	tok, err := processToken()
	if err != nil {
		return false
	}
	defer tok.Close()
	r, _, _ := procIsTokenRestricted.Call(uintptr(tok))
	return r != 0
}

func tokenInfo(t windows.Token, class uint32) ([]byte, error) {
	n := uint32(512)
	for {
		buf := make([]byte, n)
		err := windows.GetTokenInformation(t, class, &buf[0], n, &n)
		if err == nil {
			return buf, nil
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER {
			return nil, err
		}
	}
}

func accountName(sid *windows.SID) string {
	account, domain, _, err := sid.LookupAccount("")
	if err != nil {
		return sid.String()
	}
	if domain == "" {
		return account
	}
	return domain + `\` + account
}

func groupList(groups []windows.SIDAndAttributes) string {
	const (
		enabled  = 0x4
		denyOnly = 0x10
		logonID  = 0xC0000000
	)
	var names []string
	for _, g := range groups {
		name := accountName(g.Sid)
		switch {
		case g.Attributes&logonID == logonID:
			name += " (logon session)"
		case g.Attributes&denyOnly != 0:
			name += " (deny-only)"
		case g.Attributes&enabled == 0 && g.Attributes != 0:
			name += " (disabled)"
		}
		names = append(names, name)
	}
	return strings.Join(names, "; ")
}

func privilegeName(luid windows.LUID) string {
	var buf [128]uint16
	n := uint32(len(buf))
	r, _, err := procLookupPrivilegeNameW.Call(0, uintptr(unsafe.Pointer(&luid)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return fmt.Sprintf("luid %d:%d (%v)", luid.HighPart, luid.LowPart, err)
	}
	return windows.UTF16ToString(buf[:n])
}

func reportToken(rep *report) string {
	tok, err := processToken()
	if err != nil {
		rep.info("process token", err.Error())
		return ""
	}
	defer tok.Close()
	user, err := tok.GetTokenUser()
	if err != nil {
		rep.info("token user", err.Error())
		return ""
	}
	sid := user.User.Sid.String()
	rep.info("token user", sid+" "+accountName(user.User.Sid))
	if groups, err := tok.GetTokenGroups(); err == nil {
		rep.info("token groups", groupList(groups.AllGroups()))
	}
	if buf, err := tokenInfo(tok, windows.TokenRestrictedSids); err == nil {
		groups := (*windows.Tokengroups)(unsafe.Pointer(&buf[0]))
		rep.info("restricted SIDs", groupList(groups.AllGroups()))
	}
	if buf, err := tokenInfo(tok, windows.TokenPrivileges); err == nil {
		privs := (*windows.Tokenprivileges)(unsafe.Pointer(&buf[0]))
		var names []string
		for _, p := range privs.AllPrivileges() {
			name := privilegeName(p.Luid)
			if p.Attributes&windows.SE_PRIVILEGE_ENABLED == 0 {
				name += " (disabled)"
			}
			names = append(names, name)
		}
		rep.info("privileges", strings.Join(names, "; "))
	}
	if buf, err := tokenInfo(tok, windows.TokenIntegrityLevel); err == nil {
		label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0]))
		rep.info("integrity level", label.Label.Sid.String())
	}
	rep.info("IsTokenRestricted", fmt.Sprint(isTokenRestricted()))
	return sid
}

// Service -------------------------------------------------------------------------

type service struct {
	user     string
	probeDir string
	log      *logger
	report   []byte
	closers  []io.Closer
	held     []windows.Handle
	sid      string
}

func runService(args []string) error {
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	user := fs.String("user", "", "desktop user's SID")
	probe := fs.String("probe", "", "a directory in the desktop user's profile")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lg := openLogger(filepath.Join(stateDir, "service.log"))
	lg.printf("service process starting (pid %d)", os.Getpid())
	s := &service{user: *user, probeDir: *probe, log: lg}
	if err := svc.Run(serviceName, &handler{s: s}); err != nil {
		lg.printf("svc.Run: %v", err)
		return err
	}
	return nil
}

type handler struct{ s *service }

func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending, WaitHint: 20000}
	if err := h.s.start(); err != nil {
		h.s.log.printf("start failed: %v", err)
		return true, 1
	}
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	h.s.log.printf("service running")
	for req := range requests {
		switch req.Cmd {
		case svc.Interrogate:
			status <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			started := time.Now()
			h.s.log.printf("stop requested (control %d)", req.Cmd)
			status <- svc.Status{State: svc.StopPending}
			for _, c := range h.s.closers {
				_ = c.Close()
			}
			h.s.log.printf("stopped cleanly in %v", time.Since(started))
			return false, 0
		}
	}
	return false, 0
}

func (s *service) start() error {
	rep := &report{Role: "service"}
	s.sid = reportToken(rep)
	rep.info("os.TempDir", os.TempDir())

	writes := []struct {
		path   string
		wantOK bool
	}{
		{filepath.Join(stateDir, "probe"), true},
		{`C:\ProgramData\dens-spike-escape`, false},
		{`C:\Windows\Temp\dens-spike-escape`, false},
		{`C:\Users\Public\dens-spike-escape`, false},
		{filepath.Join(stateRoot, "escape"), false},
		{filepath.Join(programDir, "escape"), false},
		{filepath.Join(s.probeDir, "service-escape"), false},
	}
	for _, w := range writes {
		err := os.WriteFile(w.path, []byte("probe\n"), 0o600)
		if err == nil {
			_ = os.Remove(w.path)
		}
		rep.expect("write "+w.path, w.wantOK, err, "")
	}
	tempProbe := filepath.Join(os.TempDir(), "dens-spike-probe")
	err := os.WriteFile(tempProbe, []byte("probe\n"), 0o600)
	_ = os.Remove(tempProbe)
	rep.info("write "+tempProbe, errString(err))
	_, err = os.ReadFile(`C:\Windows\System32\drivers\etc\hosts`)
	rep.expect(`read C:\Windows\System32\drivers\etc\hosts (readable by Users)`, true, err, "")
	_, err = os.ReadFile(filepath.Join(s.probeDir, "machine-scope-test.dpapi"))
	rep.expect("read a file in the desktop user's profile", false, err, "")
	entries, err := os.ReadDir(`C:\Users`)
	rep.info(`list C:\Users`, fmt.Sprintf("%d entries", len(entries))+errSuffix(err))

	blob, err := os.ReadFile(filepath.Join(stateDir, "datakey.dpapi"))
	detail := ""
	if err == nil {
		var key []byte
		key, err = dpapiUnprotect(blob)
		if err == nil {
			detail = fingerprint(key)
		}
	}
	rep.expect("unprotect the data key with DPAPI", true, err, detail)

	addr, err := windows.VirtualAlloc(0, 4096, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err == nil {
		err = windows.VirtualLock(addr, 4096)
	}
	rep.expect("VirtualLock one page for key material", true, err, "")

	ifaces, err := net.Interfaces()
	rep.expect("net.Interfaces (Pion's ICE gathering needs it)", true, err, fmt.Sprintf("%d interfaces", len(ifaces)))

	if err := s.startListeners(rep); err != nil {
		return err
	}
	s.reportChild(rep)
	if err := s.startPipes(rep); err != nil {
		return err
	}
	return nil
}

func exclusiveControl(_, _ string, c syscall.RawConn) error {
	var sockErr error
	if err := c.Control(func(fd uintptr) {
		sockErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1)
	}); err != nil {
		return err
	}
	return sockErr
}

func (s *service) startListeners(rep *report) error {
	exclusive := net.ListenConfig{Control: exclusiveControl}
	listen := func(name string, fn func() (net.Listener, error)) error {
		ln, err := fn()
		rep.expect("listen "+name, true, err, "")
		if err != nil {
			return err
		}
		s.closers = append(s.closers, ln)
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_ = conn.Close()
			}
		}()
		return nil
	}
	steps := []struct {
		name string
		fn   func() (net.Listener, error)
	}{
		{fmt.Sprintf("tcp4 127.0.0.1:%d exclusive", exclusivePort), func() (net.Listener, error) {
			return exclusive.Listen(context.Background(), "tcp4", fmt.Sprintf("127.0.0.1:%d", exclusivePort))
		}},
		{fmt.Sprintf("tcp4 127.0.0.1:%d default options", sharedPort), func() (net.Listener, error) {
			return net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", sharedPort))
		}},
		{fmt.Sprintf("tcp4 127.0.0.1:%d exclusive", dualPort), func() (net.Listener, error) {
			return exclusive.Listen(context.Background(), "tcp4", fmt.Sprintf("127.0.0.1:%d", dualPort))
		}},
		{fmt.Sprintf("tcp6 [::1]:%d exclusive", dualPort), func() (net.Listener, error) {
			return exclusive.Listen(context.Background(), "tcp6", fmt.Sprintf("[::1]:%d", dualPort))
		}},
		{fmt.Sprintf("tcp :%d", tcpPort), func() (net.Listener, error) {
			return net.Listen("tcp", fmt.Sprintf(":%d", tcpPort))
		}},
	}
	for _, step := range steps {
		if err := listen(step.name, step.fn); err != nil {
			return err
		}
	}
	udp, err := net.ListenPacket("udp", fmt.Sprintf(":%d", udpPort))
	rep.expect(fmt.Sprintf("listen udp :%d", udpPort), true, err, "")
	if err != nil {
		return err
	}
	s.closers = append(s.closers, udp)
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = udp.WriteTo(buf[:n], addr)
		}
	}()
	return nil
}

func (s *service) reportChild(rep *report) {
	self, err := os.Executable()
	if err != nil {
		rep.info("child", err.Error())
		return
	}
	cmd := exec.Command(self, "child")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.IDLE_PRIORITY_CLASS | windows.CREATE_NO_WINDOW}
	out, err := cmd.Output()
	rep.info("child started with IDLE_PRIORITY_CLASS", strings.TrimSpace(string(out))+errSuffix(err))
}

func runChild() error {
	out := map[string]string{}
	class, err := windows.GetPriorityClass(windows.CurrentProcess())
	out["priorityClass"] = fmt.Sprintf("%#x", class) + errSuffix(err)
	out["restrictedToken"] = fmt.Sprint(isTokenRestricted())
	probe := filepath.Join(stateDir, "child-probe")
	out["stateFile"] = errString(os.WriteFile(probe, []byte("child\n"), 0o600))
	_ = os.Remove(probe)
	out["escape"] = errString(os.WriteFile(`C:\ProgramData\dens-spike-child-escape`, []byte("child\n"), 0o600))
	return json.NewEncoder(os.Stdout).Encode(out)
}

func createPipe(name, sddl string, first bool) (windows.Handle, error) {
	var sa *windows.SecurityAttributes
	if sddl != "" {
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			return windows.InvalidHandle, err
		}
		sa = &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX)
	if first {
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateNamedPipe(p, flags,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		windows.PIPE_UNLIMITED_INSTANCES, 4096, 4096, 0, sa)
}

func (s *service) startPipes(rep *report) error {
	control := pipePrefix + "control"
	controlSDDL := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)(A;;%#x;;;%s)", s.sid, pipeClientGrant, s.user)
	h, err := createPipe(control, controlSDDL, true)
	rep.expect("create control pipe (first instance)", true, err, controlSDDL)
	if err != nil {
		return err
	}
	deny, err := createPipe(pipePrefix+"deny", fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)", s.sid), true)
	rep.expect("create pipe whose DACL omits the desktop user", true, err, "")
	if err == nil {
		s.held = append(s.held, deny)
	}
	grgw, err := createPipe(pipePrefix+"grgw", fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)(A;;GRGW;;;%s)", s.sid, s.user), true)
	rep.expect("create pipe granting the desktop user GENERIC_WRITE", true, err, "")
	if err == nil {
		s.held = append(s.held, grgw)
	}

	payload, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	s.report = payload
	_ = os.WriteFile(filepath.Join(stateDir, "service-report.json"), payload, 0o600)
	go s.acceptLoop(control, controlSDDL, h)
	return nil
}

// acceptLoop always creates the next instance before serving the current one,
// so an instance of the pipe exists at all times and no other process can
// recreate the name between connections.
func (s *service) acceptLoop(name, sddl string, h windows.Handle) {
	for {
		err := windows.ConnectNamedPipe(h, nil)
		if err != nil && err != windows.ERROR_PIPE_CONNECTED {
			s.log.printf("connect %s: %v", name, err)
			_ = windows.CloseHandle(h)
			return
		}
		next, nextErr := createPipe(name, sddl, false)
		go s.serve(h)
		if nextErr != nil {
			s.log.printf("next instance of %s: %v", name, nextErr)
			return
		}
		h = next
	}
}

// pipeClientSID impersonates the client just long enough to read its SID.
// Without SeImpersonatePrivilege the impersonation is identification-level,
// which is all this needs.
func pipeClientSID(h windows.Handle) (string, error) {
	runtime.LockOSThread()
	r, _, callErr := procImpersonateNamedPipeClient.Call(uintptr(h))
	if r == 0 {
		runtime.UnlockOSThread()
		return "", fmt.Errorf("ImpersonateNamedPipeClient: %w", callErr)
	}
	sid, err := threadTokenSID()
	if revertErr := windows.RevertToSelf(); revertErr != nil {
		// Leave the thread locked: the runtime discards it with the goroutine
		// rather than reusing a thread that is still impersonating.
		return "", fmt.Errorf("RevertToSelf: %w", revertErr)
	}
	runtime.UnlockOSThread()
	return sid, err
}

func threadTokenSID() (string, error) {
	thread, err := windows.GetCurrentThread()
	if err != nil {
		return "", err
	}
	var tok windows.Token
	if err := windows.OpenThreadToken(thread, windows.TOKEN_QUERY, true, &tok); err != nil {
		return "", fmt.Errorf("OpenThreadToken: %w", err)
	}
	defer tok.Close()
	user, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func (s *service) serve(h windows.Handle) {
	defer windows.CloseHandle(h)
	defer windows.DisconnectNamedPipe(h)
	buf := make([]byte, 1024)
	var n uint32
	if err := windows.ReadFile(h, buf, &n, nil); err != nil {
		s.log.printf("read request: %v", err)
		return
	}
	req := strings.TrimSpace(string(buf[:n]))
	var clientPID uint32
	_ = windows.GetNamedPipeClientProcessId(h, &clientPID)
	sid, err := pipeClientSID(h)
	s.log.printf("pipe request %q from %s (pid %d) %s", req, sid, clientPID, errString(err))

	reply := map[string]any{"clientSID": sid, "clientPID": clientPID}
	switch {
	case err != nil:
		reply["error"] = err.Error()
	case sid != s.user:
		reply["denied"] = true
	case req == "report":
		reply["report"] = json.RawMessage(s.report)
	case strings.HasPrefix(req, "squat-test "):
		target := strings.TrimPrefix(req, "squat-test ")
		first, err1 := createPipe(target, "D:P(A;;GA;;;SY)", true)
		if err1 == nil {
			_ = windows.CloseHandle(first)
		}
		join, err2 := createPipe(target, "D:P(A;;GA;;;SY)", false)
		if err2 == nil {
			_ = windows.CloseHandle(join)
		}
		reply["firstInstance"] = errString(err1)
		reply["joinExisting"] = errString(err2)
	default:
		reply["error"] = "unknown request"
	}
	data, _ := json.Marshal(reply)
	var written uint32
	_ = windows.WriteFile(h, append(data, '\n'), &written, nil)
	_ = windows.FlushFileBuffers(h)
}

// Client (desktop user) -------------------------------------------------------------

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	work := fs.String("work", "", "work directory shared with the orchestrator")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep := &report{Role: "client"}
	if tok, err := processToken(); err == nil {
		if user, err := tok.GetTokenUser(); err == nil {
			rep.info("user", user.User.Sid.String()+" "+accountName(user.User.Sid))
		}
		tok.Close()
	}
	rep.info("elevated", fmt.Sprint(isElevated()))

	scmPID, state, err := queryServiceAsUser()
	rep.expect("query service status as the desktop user", true, err, fmt.Sprintf("state %d pid %d", state, scmPID))

	control := pipePrefix + "control"
	if h, err := openPipe(control, windows.GENERIC_READ|windows.GENERIC_WRITE); err == nil {
		_ = windows.CloseHandle(h)
		rep.expect("open control pipe asking for GENERIC_WRITE refused", false, nil, "")
	} else {
		rep.expect("open control pipe asking for GENERIC_WRITE refused", false, err, "")
	}
	h, err := openPipe(control, pipeClientAccess)
	rep.expect("open control pipe asking for read and write-data rights", true, err, "")
	if err == nil {
		var serverPID uint32
		err := windows.GetNamedPipeServerProcessId(h, &serverPID)
		if err == nil && serverPID != scmPID {
			err = fmt.Errorf("server pid %d, SCM pid %d", serverPID, scmPID)
		}
		rep.expect("pipe server PID matches the SCM's", true, err, fmt.Sprintf("server pid %d", serverPID))
		reply, err := pipeRequest(h, "report")
		rep.expect("request report over the control pipe", true, err, "")
		if err == nil {
			rep.Nested = reply
		}
		_ = windows.CloseHandle(h)
	}

	rogue, err := createPipe(control, "", false)
	if err == nil {
		_ = windows.CloseHandle(rogue)
	}
	rep.expect("add a server instance to the control pipe refused", false, err, "")
	rogue, err = createPipe(pipePrefix+"grgw", "", false)
	if err == nil {
		_ = windows.CloseHandle(rogue)
	}
	rep.expect("pitfall: GENERIC_WRITE lets a client add server instances", true, err, "")
	if h, err := openPipe(pipePrefix+"deny", pipeClientAccess); err == nil {
		_ = windows.CloseHandle(h)
		rep.expect("open pipe whose DACL omits the user refused", false, nil, "")
	} else {
		rep.expect("open pipe whose DACL omits the user refused", false, err, "")
	}

	squat := pipePrefix + "squat"
	squatHandle, err := createPipe(squat, "", true)
	rep.expect("claim a pipe name as the desktop user", true, err, "")
	if err == nil {
		if h, err := openPipe(control, pipeClientAccess); err == nil {
			reply, err := pipeRequest(h, "squat-test "+squat)
			rep.info("service creating a pipe name the user already claimed", string(reply)+errSuffix(err))
			_ = windows.CloseHandle(h)
		}
		_ = windows.CloseHandle(squatHandle)
	}

	binds := []struct {
		name   string
		family int
		addr   string
		port   int
		reuse  bool
	}{
		{"service exclusive", windows.AF_INET, "127.0.0.1", exclusivePort, false},
		{"service exclusive, SO_REUSEADDR", windows.AF_INET, "127.0.0.1", exclusivePort, true},
		{"service default options", windows.AF_INET, "127.0.0.1", sharedPort, false},
		{"service default options, SO_REUSEADDR", windows.AF_INET, "127.0.0.1", sharedPort, true},
		{"wildcard, service exclusive", windows.AF_INET, "0.0.0.0", exclusivePort, false},
		{"wildcard, service exclusive, SO_REUSEADDR", windows.AF_INET, "0.0.0.0", exclusivePort, true},
		{"wildcard, service default options", windows.AF_INET, "0.0.0.0", sharedPort, false},
		{"wildcard, service default options, SO_REUSEADDR", windows.AF_INET, "0.0.0.0", sharedPort, true},
		{"IPv6 loopback, service bound IPv4 only", windows.AF_INET6, "::1", exclusivePort, false},
		{"IPv6 loopback, service bound both", windows.AF_INET6, "::1", dualPort, false},
	}
	for _, b := range binds {
		err := tryBind(b.family, b.addr, b.port, b.reuse)
		rep.expect(fmt.Sprintf("squat %s:%d refused (%s)", b.addr, b.port, b.name), false, err, "")
	}

	udp, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", udpPort))
	if err == nil {
		_ = udp.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = udp.Write([]byte("ping"))
		if err == nil {
			b := make([]byte, 16)
			var n int
			n, err = udp.Read(b)
			if err == nil && string(b[:n]) != "ping" {
				err = fmt.Errorf("echo %q", b[:n])
			}
		}
		_ = udp.Close()
	}
	rep.expect("UDP echo from the media port", true, err, "")
	tcp, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort), 2*time.Second)
	if err == nil {
		_ = tcp.Close()
	}
	rep.expect("TCP connect to the media fallback port", true, err, "")

	_, err = os.ReadDir(stateDir)
	rep.expect("list the service state directory refused", false, err, "")
	_, err = os.ReadFile(filepath.Join(stateDir, "datakey.dpapi"))
	rep.expect("read the data key blob refused", false, err, "")
	err = os.WriteFile(filepath.Join(stateRoot, "user-escape"), []byte("x"), 0o600)
	rep.expect("create a file in the state root refused", false, err, "")

	blob, err := os.ReadFile(filepath.Join(*work, "machine-scope-test.dpapi"))
	plain := []byte{}
	if err == nil {
		plain, err = dpapiUnprotect(blob)
	}
	rep.expect("desktop user decrypts a machine-scope blob it can read", true, err, string(plain))

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func queryServiceAsUser() (pid, state uint32, err error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, 0, fmt.Errorf("OpenSCManager: %w", err)
	}
	defer windows.CloseServiceHandle(scm)
	name, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return 0, 0, err
	}
	h, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return 0, 0, fmt.Errorf("OpenService: %w", err)
	}
	defer windows.CloseServiceHandle(h)
	var st windows.SERVICE_STATUS_PROCESS
	var needed uint32
	err = windows.QueryServiceStatusEx(h, windows.SC_STATUS_PROCESS_INFO, (*byte)(unsafe.Pointer(&st)), uint32(unsafe.Sizeof(st)), &needed)
	return st.ProcessId, st.CurrentState, err
}

func openPipe(name string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h, err := windows.CreateFile(p, access, 0, nil, windows.OPEN_EXISTING, 0, 0)
		if err != windows.ERROR_PIPE_BUSY || time.Now().After(deadline) {
			return h, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func pipeRequest(h windows.Handle, req string) ([]byte, error) {
	var n uint32
	if err := windows.WriteFile(h, []byte(req+"\n"), &n, nil); err != nil {
		return nil, err
	}
	var out []byte
	buf := make([]byte, 4096)
	for {
		err := windows.ReadFile(h, buf, &n, nil)
		out = append(out, buf[:n]...)
		if err == windows.ERROR_BROKEN_PIPE || err == windows.ERROR_PIPE_NOT_CONNECTED {
			break
		}
		if err != nil && err != windows.ERROR_MORE_DATA {
			return out, err
		}
		if bytes.HasSuffix(out, []byte("\n")) {
			break
		}
	}
	return bytes.TrimSpace(out), nil
}

func tryBind(family int, addr string, port int, reuse bool) error {
	fd, err := windows.Socket(family, windows.SOCK_STREAM, windows.IPPROTO_TCP)
	if err != nil {
		return err
	}
	defer windows.Closesocket(fd)
	if reuse {
		if err := windows.SetsockoptInt(fd, windows.SOL_SOCKET, windows.SO_REUSEADDR, 1); err != nil {
			return err
		}
	}
	ip := net.ParseIP(addr)
	var sa windows.Sockaddr
	if family == windows.AF_INET6 {
		a := &windows.SockaddrInet6{Port: port}
		copy(a.Addr[:], ip.To16())
		sa = a
	} else {
		a := &windows.SockaddrInet4{Port: port}
		copy(a.Addr[:], ip.To4())
		sa = a
	}
	if err := windows.Bind(fd, sa); err != nil {
		return err
	}
	return windows.Listen(fd, 1)
}

// runBindPrecedence checks which socket receives a loopback connection when
// one socket holds 127.0.0.1:port and another holds 0.0.0.0:port.
func runBindPrecedence() error {
	const port = 18584
	specific, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer specific.Close()
	wildcard, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		fmt.Println("wildcard bind refused:", err)
		return nil
	}
	defer wildcard.Close()
	got := make(chan string, 2)
	accept := func(name string, ln net.Listener) {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
			got <- name
		}
	}
	go accept("specific 127.0.0.1", specific)
	go accept("wildcard 0.0.0.0", wildcard)
	conn, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	select {
	case name := <-got:
		fmt.Println("connection to 127.0.0.1 accepted by:", name)
	case <-time.After(2 * time.Second):
		fmt.Println("no listener accepted")
	}
	return nil
}
