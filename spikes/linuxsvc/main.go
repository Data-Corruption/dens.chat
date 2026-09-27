//go:build linux

// Command linuxsvc is a throwaway experiment for the Dens Linux platform
// layer. "service" runs under a hardened systemd unit and reports what the
// sandbox allows. "client" runs as a desktop user and checks the control
// socket, peer credentials and port ownership. "child" is the low-priority
// subprocess the service spawns, and "probe-net" checks interface
// enumeration under a given address-family filter. See ../README.md.
package main

import (
	"bufio"
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
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: linuxsvc service|client|child|probe-net [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "service":
		err = runService(os.Args[2:])
	case "client":
		err = runClient(os.Args[2:])
	case "child":
		err = runChild(os.Args[2:])
	case "probe-net":
		err = runProbeNet()
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
	Want string `json:"want,omitempty"` // "ok", "fail", or empty for information only
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

// expect records whether an operation succeeded when it should have (wantOK)
// or failed when it should have been refused.
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

// Service ---------------------------------------------------------------------

type serviceFlags struct {
	allowedUID  int
	desktopHome string
	clientPort  int
	dualPort    int
	udpPort     int
	tcpPort     int
}

func runService(args []string) error {
	var f serviceFlags
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	fs.IntVar(&f.allowedUID, "allowed-uid", -1, "desktop user allowed on the control socket")
	fs.StringVar(&f.desktopHome, "desktop-home", "", "desktop user's home, for sandbox probes")
	fs.IntVar(&f.clientPort, "client-port", 18484, "client listener, bound on IPv4 loopback only")
	fs.IntVar(&f.dualPort, "dual-port", 18494, "client listener bound on both loopbacks")
	fs.IntVar(&f.udpPort, "udp-port", 17881, "UDP media port")
	fs.IntVar(&f.tcpPort, "tcp-port", 17882, "TCP media fallback port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	stateDir := os.Getenv("STATE_DIRECTORY")
	runtimeDir := os.Getenv("RUNTIME_DIRECTORY")
	if stateDir == "" || runtimeDir == "" {
		return errors.New("STATE_DIRECTORY and RUNTIME_DIRECTORY must be set by systemd")
	}
	logf("service starting (pid %d)", os.Getpid())

	rep := &report{Role: "service"}
	reportIdentity(rep)
	reportCredential(rep)
	reportMemory(rep)
	reportFilesystem(rep, stateDir, runtimeDir, f.desktopHome)
	reportSyscalls(rep)

	ifaces, err := net.Interfaces()
	rep.expect("net.Interfaces (Pion's ICE gathering needs it)", true, err, fmt.Sprintf("%d interfaces", len(ifaces)))

	closers, err := startListeners(rep, f)
	if err != nil {
		return err
	}
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	reportChildren(rep, stateDir)

	control, err := startControl(rep, runtimeDir, f.allowedUID)
	if err != nil {
		return err
	}
	defer control.Close()

	if err := sdNotify("READY=1"); err != nil {
		logf("sd_notify failed: %v", err)
	}
	logf("service ready")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	sig := <-signals
	started := time.Now()
	logf("stop requested by %v", sig)
	marker := filepath.Join(stateDir, "stopped-cleanly")
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
	logf("stopped cleanly in %v", time.Since(started))
	return nil
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func reportIdentity(rep *report) {
	rep.info("uid/gid", fmt.Sprintf("%d/%d", os.Getuid(), os.Getgid()))
	groups, _ := os.Getgroups()
	rep.info("supplementary groups", fmt.Sprint(groups))
	old := unix.Umask(0)
	unix.Umask(old)
	rep.info("umask", fmt.Sprintf("%04o", old))
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		rep.info("/proc/self/status", err.Error())
		return
	}
	for _, line := range strings.Split(string(status), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch key {
		case "NoNewPrivs", "Seccomp", "Seccomp_filters", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb":
			rep.info(key, strings.TrimSpace(value))
		}
	}
}

func reportCredential(rep *report) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		rep.expect("credential directory", true, errors.New("CREDENTIALS_DIRECTORY unset"), "")
		return
	}
	if info, err := os.Stat(dir); err == nil {
		st := info.Sys().(*syscall.Stat_t)
		rep.info("credential directory", fmt.Sprintf("%s mode %04o owner %d", dir, info.Mode().Perm(), st.Uid))
	}
	key, err := os.ReadFile(filepath.Join(dir, "datakey"))
	detail := ""
	if err == nil {
		sum := sha256.Sum256(key)
		detail = fmt.Sprintf("%d bytes, sha256 %s", len(key), hex.EncodeToString(sum[:8]))
		if len(key) != 32 {
			err = fmt.Errorf("want 32 bytes, got %d", len(key))
		}
	}
	rep.expect("read data key from LoadCredentialEncrypted", true, err, detail)
}

func reportMemory(rep *report) {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &limit); err == nil {
		var err error
		if limit.Cur != 0 || limit.Max != 0 {
			err = fmt.Errorf("RLIMIT_CORE is %d/%d", limit.Cur, limit.Max)
		}
		rep.expect("core dumps disabled (RLIMIT_CORE 0)", true, err, "")
	}
	if err := unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit); err == nil {
		rep.info("RLIMIT_MEMLOCK", fmt.Sprintf("%d/%d", limit.Cur, limit.Max))
	}
	page, err := unix.Mmap(-1, 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err == nil {
		err = unix.Mlock(page)
		_ = unix.Munmap(page)
	}
	rep.expect("mlock one page for key material", true, err, "")

	// MemoryDenyWriteExecute refuses writable+executable mappings. Go never
	// needs them; the check shows whether the setting took effect.
	wx, err := unix.Mmap(-1, 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE|unix.PROT_EXEC, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err == nil {
		_ = unix.Munmap(wx)
	}
	rep.expect("W+X mapping refused (MemoryDenyWriteExecute)", false, err, "")
}

func reportFilesystem(rep *report, stateDir, runtimeDir, desktopHome string) {
	if info, err := os.Stat(stateDir); err == nil {
		st := info.Sys().(*syscall.Stat_t)
		rep.info("state directory", fmt.Sprintf("%s mode %04o owner %d:%d", stateDir, info.Mode().Perm(), st.Uid, st.Gid))
	}
	if info, err := os.Stat(runtimeDir); err == nil {
		st := info.Sys().(*syscall.Stat_t)
		rep.info("runtime directory", fmt.Sprintf("%s mode %04o owner %d:%d", runtimeDir, info.Mode().Perm(), st.Uid, st.Gid))
	}
	writes := []struct {
		path   string
		wantOK bool
	}{
		{filepath.Join(stateDir, "probe"), true},
		{filepath.Join(runtimeDir, "probe"), true},
		{"/tmp/dens-spike-probe", true},
		{"/var/tmp/dens-spike-probe", true},
		{"/var/lib/dens-spike-escape", false},
		{"/etc/dens-spike-escape", false},
		{"/usr/local/bin/dens-spike-escape", false},
		{"/dev/shm/dens-spike-probe", true},
	}
	if desktopHome != "" {
		writes = append(writes, struct {
			path   string
			wantOK bool
		}{filepath.Join(desktopHome, "dens-spike-escape"), false})
	}
	for _, w := range writes {
		err := os.WriteFile(w.path, []byte("probe\n"), 0o600)
		if err == nil {
			_ = os.Remove(w.path)
		}
		rep.expect("write "+w.path, w.wantOK, err, "")
	}
	reads := []struct {
		path   string
		wantOK bool
	}{
		{"/etc/passwd", true},
		{"/etc/shadow", false},
		{"/proc/1/environ", false},
		{"/root", false},
	}
	if desktopHome != "" {
		reads = append(reads, struct {
			path   string
			wantOK bool
		}{filepath.Join(desktopHome, "secret.txt"), false})
	}
	for _, r := range reads {
		var err error
		if info, statErr := os.Stat(r.path); statErr == nil && info.IsDir() {
			_, err = os.ReadDir(r.path)
		} else {
			_, err = os.ReadFile(r.path)
		}
		rep.expect("read "+r.path, r.wantOK, err, "")
	}
	entries, err := os.ReadDir("/proc")
	pids := 0
	for _, e := range entries {
		if _, convErr := strconv.Atoi(e.Name()); convErr == nil {
			pids++
		}
	}
	rep.expect("list /proc", true, err, fmt.Sprintf("%d processes visible", pids))
}

func reportSyscalls(rep *report) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE)
	if err == nil {
		_ = unix.Close(fd)
	}
	rep.info("AF_NETLINK socket", errString(err))
	fd, err = unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, 0)
	if err == nil {
		_ = unix.Close(fd)
	}
	rep.expect("AF_PACKET socket refused", false, err, "")
	err = unix.Setpriority(unix.PRIO_PROCESS, 0, -5)
	rep.expect("raise own priority refused", false, err, "")
}

func errString(err error) string {
	if err == nil {
		return "ok"
	}
	return "error: " + err.Error()
}

func startListeners(rep *report, f serviceFlags) ([]io.Closer, error) {
	var closers []io.Closer
	listen := func(network, address string) error {
		ln, err := net.Listen(network, address)
		rep.expect("listen "+network+" "+address, true, err, "")
		if err != nil {
			return err
		}
		closers = append(closers, ln)
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
	if err := listen("tcp4", fmt.Sprintf("127.0.0.1:%d", f.clientPort)); err != nil {
		return closers, err
	}
	if err := listen("tcp4", fmt.Sprintf("127.0.0.1:%d", f.dualPort)); err != nil {
		return closers, err
	}
	if err := listen("tcp6", fmt.Sprintf("[::1]:%d", f.dualPort)); err != nil {
		return closers, err
	}
	if err := listen("tcp", fmt.Sprintf(":%d", f.tcpPort)); err != nil {
		return closers, err
	}
	udp, err := net.ListenPacket("udp", fmt.Sprintf(":%d", f.udpPort))
	rep.expect(fmt.Sprintf("listen udp :%d", f.udpPort), true, err, "")
	if err != nil {
		return closers, err
	}
	closers = append(closers, udp)
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
	return closers, nil
}

// Children ---------------------------------------------------------------------

type childReport struct {
	Threads   int      `json:"threads"`
	Nice      []int    `json:"nice"`
	Policy    []int    `json:"policy"`
	StateFile string   `json:"stateFile"`
	Escape    string   `json:"escape"`
	Errors    []string `json:"errors,omitempty"`
}

// reportChildren compares two ways of starting a low-priority child. Linux
// nice values and scheduling policies are per thread, so changing a child's
// PID after it starts reaches only its main thread; threads it had already
// created keep the old values. Changing the calling thread before fork makes
// every thread of the child inherit them.
func reportChildren(rep *report, stateDir string) {
	self, err := os.Executable()
	if err != nil {
		rep.info("child", err.Error())
		return
	}

	after := exec.Command(self, "child", "-state", stateDir)
	out, err := runChildWith(after, func(pid int) error {
		return unix.Setpriority(unix.PRIO_PROCESS, pid, 19)
	})
	rep.info("child niced after start (setpriority on its PID)", out+errSuffix(err))

	for _, idle := range []bool{false, true} {
		name := "child niced before fork (nice 19)"
		if idle {
			name = "child niced before fork (nice 19 + SCHED_IDLE)"
		}
		out, err := runChildFromNicedThread(self, stateDir, idle)
		rep.info(name, out+errSuffix(err))
	}
}

func errSuffix(err error) string {
	if err == nil {
		return ""
	}
	return " error: " + err.Error()
}

func runChildWith(cmd *exec.Cmd, afterStart func(pid int) error) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	adjustErr := afterStart(cmd.Process.Pid)
	data, readErr := io.ReadAll(stdout)
	waitErr := cmd.Wait()
	return strings.TrimSpace(string(data)), errors.Join(adjustErr, readErr, waitErr)
}

func runChildFromNicedThread(self, stateDir string, idle bool) (string, error) {
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		// Never unlock: this thread's priority can't be raised back without
		// CAP_SYS_NICE, so the runtime must discard it when the goroutine exits.
		runtime.LockOSThread()
		if err := unix.Setpriority(unix.PRIO_PROCESS, 0, 19); err != nil {
			done <- result{err: fmt.Errorf("setpriority: %w", err)}
			return
		}
		if idle {
			attr := unix.SchedAttr{Size: unix.SizeofSchedAttr, Policy: unix.SCHED_IDLE}
			if err := unix.SchedSetAttr(0, &attr, 0); err != nil {
				done <- result{err: fmt.Errorf("sched_setattr: %w", err)}
				return
			}
		}
		cmd := exec.Command(self, "child", "-state", stateDir)
		out, err := runChildWith(cmd, func(int) error { return nil })
		done <- result{out: out, err: err}
	}()
	r := <-done
	return r.out, r.err
}

func runChild(args []string) error {
	fs := flag.NewFlagSet("child", flag.ExitOnError)
	stateDir := fs.String("state", "", "state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var rep childReport
	// Blocking syscalls make the runtime start more threads, like an encoder
	// starting worker threads.
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			ts := unix.Timespec{Nsec: 200_000_000}
			_ = unix.Nanosleep(&ts, nil)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	tasks, err := os.ReadDir("/proc/self/task")
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	}
	for _, task := range tasks {
		stat, err := os.ReadFile(filepath.Join("/proc/self/task", task.Name(), "stat"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		// fields[0] is field 3 (state); nice is field 19 and policy field 41.
		if len(fields) > 38 {
			nice, _ := strconv.Atoi(fields[16])
			policy, _ := strconv.Atoi(fields[38])
			rep.Nice = append(rep.Nice, nice)
			rep.Policy = append(rep.Policy, policy)
		}
	}
	rep.Threads = len(rep.Nice)
	path := filepath.Join(*stateDir, "child-probe")
	rep.StateFile = errString(os.WriteFile(path, []byte("child\n"), 0o600))
	_ = os.Remove(path)
	rep.Escape = errString(os.WriteFile("/var/lib/dens-spike-child-escape", []byte("child\n"), 0o600))
	return json.NewEncoder(os.Stdout).Encode(rep)
}

// Control socket ---------------------------------------------------------------

func startControl(rep *report, runtimeDir string, allowedUID int) (io.Closer, error) {
	path := filepath.Join(runtimeDir, "control.sock")
	_ = os.Remove(path)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	rep.expect("listen on control socket", true, err, path)
	if err != nil {
		return nil, err
	}
	// UMask=0077 creates the socket 0600. Any local user may connect; the
	// peer credential check below is the authorization.
	err = os.Chmod(path, 0o666)
	rep.expect("chmod control socket 0666", true, err, "")

	payload, err := json.Marshal(rep)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			go serveControl(conn, allowedUID, payload)
		}
	}()
	return ln, nil
}

func peerCred(conn *net.UnixConn) (*unix.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	return cred, credErr
}

func serveControl(conn *net.UnixConn, allowedUID int, payload []byte) {
	defer conn.Close()
	cred, err := peerCred(conn)
	if err != nil {
		logf("control: peer credentials: %v", err)
		return
	}
	if int(cred.Uid) != allowedUID {
		logf("control: denied uid %d pid %d", cred.Uid, cred.Pid)
		_ = json.NewEncoder(conn).Encode(map[string]any{"denied": true, "peerUID": cred.Uid})
		return
	}
	logf("control: authorized uid %d pid %d", cred.Uid, cred.Pid)
	_, _ = conn.Write(append(payload, '\n'))
}

func sdNotify(state string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return errors.New("NOTIFY_SOCKET unset")
	}
	if strings.HasPrefix(socket, "@") {
		socket = "\x00" + socket[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}

// Client ------------------------------------------------------------------------

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	socket := fs.String("socket", "/run/dens-spike/main/control.sock", "control socket")
	serviceUser := fs.String("service-user", "dens-spike-main", "account the service must run as")
	stateDir := fs.String("state", "/var/lib/dens-spike/main", "service state directory")
	credFile := fs.String("cred", "/run/credentials/dens-spike@main.service/datakey", "decrypted credential path")
	clientPort := fs.Int("client-port", 18484, "")
	dualPort := fs.Int("dual-port", 18494, "")
	udpPort := fs.Int("udp-port", 17881, "")
	tcpPort := fs.Int("tcp-port", 17882, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep := &report{Role: "client"}
	rep.info("uid", strconv.Itoa(os.Getuid()))

	svcUser, err := user.Lookup(*serviceUser)
	rep.expect("look up service account", true, err, "")

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: *socket, Net: "unix"})
	rep.expect("connect to control socket", true, err, "")
	if err == nil {
		cred, credErr := peerCred(conn)
		if credErr == nil && svcUser != nil && strconv.Itoa(int(cred.Uid)) != svcUser.Uid {
			credErr = fmt.Errorf("server uid %d is not %s (%s)", cred.Uid, *serviceUser, svcUser.Uid)
		}
		detail := ""
		if cred != nil {
			detail = fmt.Sprintf("server uid %d pid %d", cred.Uid, cred.Pid)
		}
		rep.expect("server peer is the service account", true, credErr, detail)
		line, readErr := bufio.NewReader(conn).ReadBytes('\n')
		_ = conn.Close()
		if readErr == nil || len(line) > 0 {
			rep.Nested = json.RawMessage(strings.TrimSpace(string(line)))
		} else {
			rep.info("control reply", readErr.Error())
		}
	}

	type bindCase struct {
		name   string
		family int
		addr   string
		port   int
		opts   []int
	}
	cases := []bindCase{
		{"IPv4 loopback", unix.AF_INET, "127.0.0.1", *clientPort, nil},
		{"IPv4 loopback + SO_REUSEADDR", unix.AF_INET, "127.0.0.1", *clientPort, []int{unix.SO_REUSEADDR}},
		{"IPv4 loopback + SO_REUSEPORT", unix.AF_INET, "127.0.0.1", *clientPort, []int{unix.SO_REUSEPORT}},
		{"IPv4 wildcard", unix.AF_INET, "0.0.0.0", *clientPort, nil},
		{"IPv4 wildcard + SO_REUSEADDR", unix.AF_INET, "0.0.0.0", *clientPort, []int{unix.SO_REUSEADDR}},
		{"IPv6 loopback (service bound IPv4 only)", unix.AF_INET6, "::1", *clientPort, nil},
		{"IPv6 loopback (service bound both)", unix.AF_INET6, "::1", *dualPort, nil},
	}
	for _, c := range cases {
		err := tryBind(c.family, c.addr, c.port, c.opts)
		rep.expect(fmt.Sprintf("squat %s:%d refused (%s)", c.addr, c.port, c.name), false, err, "")
	}

	udp, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", *udpPort))
	if err == nil {
		_ = udp.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = udp.Write([]byte("ping"))
		if err == nil {
			buf := make([]byte, 16)
			var n int
			n, err = udp.Read(buf)
			if err == nil && string(buf[:n]) != "ping" {
				err = fmt.Errorf("echo %q", buf[:n])
			}
		}
		_ = udp.Close()
	}
	rep.expect("UDP echo from media port", true, err, "")
	tcp, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", *tcpPort), 2*time.Second)
	if err == nil {
		_ = tcp.Close()
	}
	rep.expect("TCP connect to media fallback port", true, err, "")

	_, err = os.ReadDir(*stateDir)
	rep.expect("list service state directory refused", false, err, "")
	_, err = os.ReadFile(filepath.Join(*stateDir, "datakey.cred"))
	rep.expect("read encrypted credential refused", false, err, "")
	_, err = os.ReadFile(*credFile)
	rep.expect("read decrypted credential refused", false, err, "")

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func tryBind(family int, addr string, port int, opts []int) error {
	fd, err := unix.Socket(family, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	for _, opt := range opts {
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, opt, 1); err != nil {
			return err
		}
	}
	ip := net.ParseIP(addr)
	var sa unix.Sockaddr
	if family == unix.AF_INET6 {
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1); err != nil {
			return err
		}
		a := &unix.SockaddrInet6{Port: port}
		copy(a.Addr[:], ip.To16())
		sa = a
	} else {
		a := &unix.SockaddrInet4{Port: port}
		copy(a.Addr[:], ip.To4())
		sa = a
	}
	if err := unix.Bind(fd, sa); err != nil {
		return err
	}
	return unix.Listen(fd, 1)
}

// Probe -------------------------------------------------------------------------

func runProbeNet() error {
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("net.Interfaces: error:", err)
		return nil
	}
	names := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		names = append(names, iface.Name)
	}
	fmt.Println("net.Interfaces: ok:", strings.Join(names, ","))
	return nil
}
