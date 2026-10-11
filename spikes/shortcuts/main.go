//go:build windows

// Command shortcuts answers M7.1's questions on a real Windows desktop:
// whether reading key state (GetAsyncKeyState) from a background process
// sees a key and a mouse side button while a full-screen game has focus, how
// steady a 10 ms poll stays under a game's load, what it costs, and whether
// the manifest's detached console policy starts it without a window.
//
// It watches only the keys it's given, and logs each press and release with
// the program in front, to the console and to %TEMP%\dens-shortcuts-spike.log.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")

	procGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID   = user32.NewProc("GetWindowThreadProcessId")
	procGetConsoleWindow           = kernel32.NewProc("GetConsoleWindow")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procSetPriorityClass           = kernel32.NewProc("SetPriorityClass")
	procRtlGetNtVersionNumbers     = ntdll.NewProc("RtlGetNtVersionNumbers")
)

const (
	processQueryLimitedInformation = 0x1000
	tokenQuery                     = 0x0008
	tokenElevation                 = 20
)

var priorities = map[string]uintptr{
	"idle":   0x00000040,
	"below":  0x00004000,
	"normal": 0x00000020,
	"above":  0x00008000,
}

type key struct {
	name string
	vk   uintptr
	down bool
}

func main() {
	keysFlag := flag.String("keys", "Mouse4,Mouse5,V,Backquote,F8", "keys to watch, by name (A-Z, 0-9, F1-F24, Mouse4, Mouse5, Backquote, Space, Tab, CapsLock, LShift, LCtrl, LAlt, ...)")
	every := flag.Duration("every", 10*time.Millisecond, "how often to read the keys")
	runFor := flag.Duration("for", 0, "stop after this long (0: until Ctrl+C, or 60s when started without a console)")
	priority := flag.String("priority", "normal", "process priority: idle, below, normal or above")
	flag.Parse()

	logPath := filepath.Join(os.TempDir(), "dens-shortcuts-spike.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()
	// The file first: without a console, writes to stdout fail, and
	// io.MultiWriter stops at the first writer that fails.
	out := io.MultiWriter(file, os.Stdout)
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "%s  %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	}

	keys, err := parseKeys(*keysFlag)
	if err != nil {
		logf("error: %v", err)
		os.Exit(2)
	}
	class, ok := priorities[*priority]
	if !ok {
		logf("error: unknown priority %q", *priority)
		os.Exit(2)
	}
	self, _ := syscall.GetCurrentProcess()
	if r, _, err := procSetPriorityClass.Call(uintptr(self), class); r == 0 {
		logf("error: set priority: %v", err)
	}

	console, _, _ := procGetConsoleWindow.Call()
	if *runFor == 0 && console == 0 {
		*runFor = time.Minute
	}
	var major, minor, build uint32
	procRtlGetNtVersionNumbers.Call(uintptr(unsafe.Pointer(&major)), uintptr(unsafe.Pointer(&minor)), uintptr(unsafe.Pointer(&build)))
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.name
	}
	logf("start: Windows %d.%d build %d, console %s, priority %s, every %v, keys %s, log %s",
		major, minor, build&0xffff, map[bool]string{true: "attached", false: "none"}[console != 0],
		*priority, *every, strings.Join(names, " "), logPath)
	if *runFor > 0 {
		logf("stopping after %v", *runFor)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	var deadline <-chan time.Time
	if *runFor > 0 {
		deadline = time.After(*runFor)
	}

	ticker := time.NewTicker(*every)
	defer ticker.Stop()
	var (
		lastTick   = time.Now()
		gaps       []time.Duration
		reportAt   = time.Now().Add(10 * time.Second)
		cpuBefore  = cpuTime(self)
		wallBefore = time.Now()
		fgWindow   uintptr
		fgName     = "?"
		presses    int
	)
	report := func(now time.Time) {
		if len(gaps) == 0 {
			return
		}
		slices.Sort(gaps)
		over := 0
		for _, g := range gaps {
			if g > 2*(*every) {
				over++
			}
		}
		cpu := cpuTime(self)
		logf("poll: %d reads, median %v, p99 %v, max %v, %d over %v; CPU %.2f%% of one core; %d presses",
			len(gaps), round(gaps[len(gaps)/2]), round(gaps[len(gaps)*99/100]), round(gaps[len(gaps)-1]),
			over, 2*(*every), 100*float64(cpu-cpuBefore)/float64(now.Sub(wallBefore)), presses)
		gaps, cpuBefore, wallBefore, presses = gaps[:0], cpu, now, 0
	}
	for {
		select {
		case <-stop:
			report(time.Now())
			logf("stop: Ctrl+C")
			return
		case <-deadline:
			report(time.Now())
			logf("stop: ran %v", *runFor)
			return
		case now := <-ticker.C:
			gaps = append(gaps, now.Sub(lastTick))
			lastTick = now
			if w, _, _ := procGetForegroundWindow.Call(); w != fgWindow {
				fgWindow = w
				fgName = describe(w)
				logf("in front: %s", fgName)
			}
			for i := range keys {
				r, _, _ := procGetAsyncKeyState.Call(keys[i].vk)
				down := r&0x8000 != 0
				if down == keys[i].down {
					continue
				}
				keys[i].down = down
				if down {
					presses++
					logf("%s down  (in front: %s)", keys[i].name, fgName)
				} else {
					logf("%s up", keys[i].name)
				}
			}
			if now.After(reportAt) {
				report(now)
				reportAt = now.Add(10 * time.Second)
			}
		}
	}
}

// describe names the program that owns a window and whether it runs elevated.
func describe(w uintptr) string {
	if w == 0 {
		return "nothing (the desktop is locked, or a secure desktop is up)"
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(w, uintptr(unsafe.Pointer(&pid)))
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return fmt.Sprintf("pid %d (can't open it: %v)", pid, err)
	}
	defer syscall.CloseHandle(h)
	name := fmt.Sprintf("pid %d", pid)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 {
		name = filepath.Base(syscall.UTF16ToString(buf[:size]))
	}
	var token syscall.Token
	if err := syscall.OpenProcessToken(h, tokenQuery, &token); err != nil {
		return name + ", elevated: can't tell (" + err.Error() + ")"
	}
	defer token.Close()
	var elevated, n uint32
	if err := syscall.GetTokenInformation(token, tokenElevation, (*byte)(unsafe.Pointer(&elevated)), 4, &n); err != nil {
		return name + ", elevated: can't tell (" + err.Error() + ")"
	}
	return fmt.Sprintf("%s, elevated: %v", name, elevated != 0)
}

func cpuTime(h syscall.Handle) time.Duration {
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0
	}
	ticks := func(f syscall.Filetime) int64 { return int64(f.HighDateTime)<<32 | int64(f.LowDateTime) }
	return time.Duration((ticks(kernel) + ticks(user)) * 100)
}

func round(d time.Duration) time.Duration { return d.Round(10 * time.Microsecond) }

func parseKeys(list string) ([]key, error) {
	named := map[string]uintptr{
		"Mouse4": 0x05, "Mouse5": 0x06, "Backquote": 0xC0, "Space": 0x20, "Tab": 0x09,
		"CapsLock": 0x14, "LShift": 0xA0, "RShift": 0xA1, "LCtrl": 0xA2, "RCtrl": 0xA3,
		"LAlt": 0xA4, "RAlt": 0xA5, "Insert": 0x2D, "Delete": 0x2E, "Home": 0x24,
		"End": 0x23, "PageUp": 0x21, "PageDown": 0x22, "Pause": 0x13, "ScrollLock": 0x91,
	}
	for c := 'A'; c <= 'Z'; c++ {
		named[string(c)] = uintptr(c)
	}
	for c := '0'; c <= '9'; c++ {
		named[string(c)] = uintptr(c)
	}
	for i := 1; i <= 24; i++ {
		named[fmt.Sprintf("F%d", i)] = uintptr(0x6F + i)
	}
	var keys []key
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		vk, ok := named[name]
		if !ok {
			return nil, fmt.Errorf("unknown key %q", name)
		}
		keys = append(keys, key{name: name, vk: vk})
	}
	return keys, nil
}
