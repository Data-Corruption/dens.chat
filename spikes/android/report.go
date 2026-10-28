package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"golang.org/x/sys/unix"
)

// reporter gathers the report's sections, each a heading and its lines.
type reporter struct {
	buf bytes.Buffer
}

func (r *reporter) section(name string) { fmt.Fprintf(&r.buf, "\n== %s\n", name) }

func (r *reporter) line(key string, format string, args ...any) {
	fmt.Fprintf(&r.buf, "%-22s %s\n", key+":", fmt.Sprintf(format, args...))
}

func (r *reporter) err(key string, err error) { r.line(key, "error: %v", err) }

func report(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	keyStdin := fs.Bool("key-stdin", false, "read the shell's unwrapped key from standard input")
	lan := fs.String("lan", "", "a HOST:PORT on the local network, for Android 17's permission")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r := &reporter{}
	fmt.Fprintf(&r.buf, "aprobe %s report, %s\n", version, time.Now().Format(time.RFC3339))
	// The key comes first: the shell writes it and closes the pipe as the
	// probe starts.
	if *keyStdin {
		r.section("key over standard input")
		readKey(r)
	}
	system(r)
	process(r)
	environment(r)
	clock(r)
	files(r)
	lockedMemory(r)
	control(r)
	lowPriority(r)
	syscalls(r)
	network(r, *lan)
	features(r)
	r.section("heartbeat")
	if err := gaps(&r.buf); err != nil {
		r.err("gaps", err)
	}
	if err := os.WriteFile("report.txt", r.buf.Bytes(), 0o600); err != nil {
		return err
	}
	_, err := os.Stdout.Write(r.buf.Bytes())
	return err
}

// readKey takes what the shell unwrapped with the Keystore and shows only
// its length and a hash prefix, which the shell's screen shows too.
func readKey(r *reporter) {
	got := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 64))
		got <- b
	}()
	select {
	case b := <-got:
		sum := sha256.Sum256(b)
		r.line("bytes", "%d, sha256 %s", len(b), hex.EncodeToString(sum[:4]))
		clear(b)
	case <-time.After(3 * time.Second):
		r.line("bytes", "none within 3 s")
	}
}

func getprop(name string) string {
	out, err := exec.Command("/system/bin/getprop", name).Output()
	if err != nil {
		return "error: " + err.Error()
	}
	return strings.TrimSpace(string(out))
}

func system(r *reporter) {
	r.section("system")
	for _, p := range []string{
		"ro.product.manufacturer", "ro.product.model", "ro.product.device", "ro.soc.model",
		"ro.build.version.release", "ro.build.version.sdk", "ro.build.version.sdk_full", "ro.build.version.security_patch",
		"ro.build.version.oneui", "ro.build.fingerprint", "ro.build.type", "ro.product.cpu.abilist",
		"ro.config.low_ram", "ro.boot.flash.locked", "persist.sys.timezone", "ro.kernel.version",
	} {
		r.line(p, "%s", getprop(p))
	}
	var u unix.Utsname
	if err := unix.Uname(&u); err == nil {
		r.line("uname", "%s %s %s", unix.ByteSliceToString(u.Sysname[:]), unix.ByteSliceToString(u.Release[:]), unix.ByteSliceToString(u.Machine[:]))
		r.line("nodename", "%s", unix.ByteSliceToString(u.Nodename[:]))
	}
	r.line("page size", "%d", os.Getpagesize())
	r.line("CPUs", "%d", runtime.NumCPU())
	r.line("Go", "%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.SplitN(string(b), "\n", 4)[:3] {
			r.line("meminfo", "%s", strings.Join(strings.Fields(l), " "))
		}
	}
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "error: " + err.Error()
	}
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", ""))
}

func process(r *reporter) {
	r.section("process")
	r.line("pid", "%d, parent %d", os.Getpid(), os.Getppid())
	exe, err := os.Executable()
	r.line("executable", "%s %v", exe, errOrEmpty(err))
	r.line("uid", "%d, gid %d", os.Getuid(), os.Getgid())
	groups, _ := os.Getgroups()
	r.line("groups", "%v", groups)
	if u, err := user.LookupId(fmt.Sprint(os.Getuid())); err == nil {
		r.line("user name", "%s, home %q", u.Username, u.HomeDir)
	} else {
		r.err("user name", err)
	}
	me, err := host.CurrentUser()
	r.line("host.CurrentUser", "%v %v", me, errOrEmpty(err))
	r.line("SELinux", "%s", readTrim("/proc/self/attr/current"))
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(l, ":")
			switch k {
			case "Seccomp", "Seccomp_filters", "NoNewPrivs", "CapEff", "CapBnd", "Cpus_allowed_list", "VmLck", "Threads":
				if ok {
					r.line(k, "%s", strings.TrimSpace(v))
				}
			}
		}
	}
	r.line("cgroup", "%s", strings.ReplaceAll(readTrim("/proc/self/cgroup"), "\n", " | "))
	r.line("oom_score_adj", "%s, parent's %s", readTrim("/proc/self/oom_score_adj"), readTrim(fmt.Sprintf("/proc/%d/oom_score_adj", os.Getppid())))
	r.line("parent", "%s", readTrim(fmt.Sprintf("/proc/%d/cmdline", os.Getppid())))
	if raw, err := unix.Getpriority(unix.PRIO_PROCESS, 0); err == nil {
		r.line("nice", "%d", 20-raw)
	}
	if attr, err := unix.SchedGetAttr(0, 0); err == nil {
		r.line("scheduling", "policy %d, nice %d", attr.Policy, attr.Nice)
	} else {
		r.err("scheduling", err)
	}
	for _, l := range []struct {
		name string
		res  int
	}{
		{"MEMLOCK", unix.RLIMIT_MEMLOCK}, {"NOFILE", unix.RLIMIT_NOFILE}, {"NPROC", unix.RLIMIT_NPROC},
		{"AS", unix.RLIMIT_AS}, {"DATA", unix.RLIMIT_DATA}, {"STACK", unix.RLIMIT_STACK},
		{"CORE", unix.RLIMIT_CORE}, {"NICE", unix.RLIMIT_NICE}, {"RTPRIO", unix.RLIMIT_RTPRIO},
	} {
		var lim unix.Rlimit
		if err := unix.Getrlimit(l.res, &lim); err != nil {
			r.err("RLIMIT_"+l.name, err)
			continue
		}
		r.line("RLIMIT_"+l.name, "%s / %s", limit(lim.Cur), limit(lim.Max))
	}
	old := unix.Umask(0)
	unix.Umask(old)
	r.line("umask", "%04o", old)
}

func limit(v uint64) string {
	if v == unix.RLIM_INFINITY {
		return "unlimited"
	}
	return fmt.Sprint(v)
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return "error: " + err.Error()
}

func environment(r *reporter) {
	r.section("environment")
	var names []string
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		names = append(names, k)
		switch k {
		case "HOME", "TMPDIR", "TZ", "PATH", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
			"ANDROID_DATA", "ANDROID_ROOT", "ANDROID_TZDATA_ROOT", "EXTERNAL_STORAGE", "LD_LIBRARY_PATH", "LD_PRELOAD":
			r.line(k, "%q", v)
		}
	}
	sort.Strings(names)
	r.line("names", "%s", strings.Join(names, " "))
	r.line("os.TempDir", "%s", os.TempDir())
	h, err := os.Hostname()
	r.line("os.Hostname", "%q %s", h, errOrEmpty(err))
	home, err := os.UserHomeDir()
	r.line("os.UserHomeDir", "%q %s", home, errOrEmpty(err))
	cache, err := os.UserCacheDir()
	r.line("os.UserCacheDir", "%q %s", cache, errOrEmpty(err))
}

func clock(r *reporter) {
	r.section("time")
	now := time.Now()
	r.line("time.Local", "%s, now %s", time.Local, now.Format("2006-01-02 15:04:05 MST -0700"))
	if tz := os.Getenv("TZ"); tz != "" {
		loc, err := time.LoadLocation(tz)
		r.line("LoadLocation(TZ)", "%v %s", loc, errOrEmpty(err))
	}
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err == nil {
		r.line("Los Angeles", "%s", now.In(loc).Format("15:04 MST"))
	} else {
		r.err("Los Angeles", err)
	}
	for _, p := range []string{"/apex/com.android.tzdata/etc/tz/tzdata", "/system/usr/share/zoneinfo/tzdata", "/data/misc/zoneinfo/current/tzdata"} {
		_, err := os.Stat(p)
		r.line("tzdata", "%s %s", p, errOrEmpty(err))
	}
}

func files(r *reporter) {
	r.section("files")
	wd, _ := os.Getwd()
	for _, dir := range []string{wd, filepath.Dir(wd), os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("TMPDIR")} {
		if dir == "" {
			continue
		}
		info, err := os.Lstat(dir)
		if err != nil {
			r.err(dir, err)
			continue
		}
		var st unix.Statfs_t
		_ = unix.Statfs(dir, &st)
		uid, gid := -1, -1
		if s, ok := info.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(s.Uid), int(s.Gid)
		}
		free, _ := host.FreeSpace(dir)
		r.line("dir", "%s %v uid %d gid %d fs 0x%x free %d MiB", dir, info.Mode(), uid, gid, st.Type, free>>20)
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		r.err("native lib dir", err)
	}
	for _, e := range entries {
		info, _ := e.Info()
		r.line("native lib", "%s %v %d", e.Name(), info.Mode(), info.Size())
	}
	f, err := os.CreateTemp(".", "write-*")
	if err == nil {
		r.line("write here", "ok")
		f.Close()
		os.Remove(f.Name())
	} else {
		r.err("write here", err)
	}
	// Android keeps an app's executables out of its own files, so a binary
	// copied there and run should fail, where the native library
	// directory's run.
	copied := filepath.Join(wd, "copied-probe")
	if b, err := os.ReadFile(exe); err == nil && os.WriteFile(copied, b, 0o700) == nil {
		err := exec.Command(copied, "syscall", "noop").Run()
		r.line("exec from files", "%s", orOK(err))
		os.Remove(copied)
	}
}

func orOK(err error) string {
	if err == nil {
		return "ok"
	}
	return "error: " + err.Error()
}

// lockedMemory takes key pages as the vault does until the lock limit
// refuses one.
func lockedMemory(r *reporter) {
	r.section("locked memory (host.AllocLocked)")
	var frees []func()
	var lastErr error
	for len(frees) < 4096 {
		_, free, err := host.AllocLocked(32)
		if err != nil {
			lastErr = err
			break
		}
		frees = append(frees, free)
	}
	r.line("pages locked", "%d of %d bytes, then %v", len(frees), os.Getpagesize(), lastErr)
	for _, free := range frees {
		free()
	}
}

// control runs the control endpoint's seams on a socket in the runtime
// directory, as the service and its commands use them.
func control(r *reporter) {
	r.section("control endpoint")
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "aprobe.sock")
	r.line("path", "%s (%d bytes)", path, len(path))
	me, _ := host.CurrentUser()
	ln, err := host.ListenControl(path, me)
	if err != nil {
		r.err("ListenControl", err)
		return
	}
	defer ln.Close()
	peer := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			peer <- "accept: " + err.Error()
			return
		}
		id, err := host.PeerIdentity(conn)
		conn.Close()
		peer <- fmt.Sprintf("%v %s", id, errOrEmpty(err))
	}()
	conn, err := host.DialControl(path, host.ControlServer{Account: me}, 2*time.Second)
	if err != nil {
		r.err("DialControl", err)
		return
	}
	conn.Close()
	r.line("DialControl", "ok, server is %v", me)
	select {
	case p := <-peer:
		r.line("PeerIdentity", "%s", p)
	case <-time.After(2 * time.Second):
		r.line("PeerIdentity", "no connection")
	}
	info, err := os.Stat(path)
	if err == nil {
		r.line("socket mode", "%v", info.Mode())
	}
}

// lowPriority starts a child through host.StartLowPriority, as the media
// runner starts its workers, and has it report its own priority. Android's
// filter refusing sched_setattr would kill this process, so it runs in a
// child of its own first.
func lowPriority(r *reporter) {
	r.section("low-priority children (host.StartLowPriority)")
	r.line("in a child", "%s", runChild("startlow"))
}

func syscalls(r *reporter) {
	r.section("system calls, each in a child")
	for _, name := range syscallNames {
		r.line(name, "%s", runChild(name))
	}
}

// runChild runs aprobe syscall NAME and says how it ended: a call
// Android's filter refuses ends the child with SIGSYS, which Go's runtime
// reports as it dies.
func runChild(name string) string {
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "syscall", name)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	switch {
	case strings.Contains(errText, "SIGSYS") || strings.Contains(errText, "bad system call"):
		return "KILLED by Android's filter (SIGSYS)"
	case err == nil:
		return "ok " + out
	case ctx.Err() != nil:
		return "timed out"
	default:
		first, _, _ := strings.Cut(errText, "\n")
		return fmt.Sprintf("failed (%v): %s", err, first)
	}
}

func features(r *reporter) {
	r.section("WebView features (aprobe serve)")
	b, err := os.ReadFile("features.json")
	if err != nil {
		r.line("features.json", "none yet: open the feature page from the shell")
		return
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		r.err("features.json", err)
		return
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b, _ := json.Marshal(v[k])
		r.line(k, "%s", b)
	}
}
