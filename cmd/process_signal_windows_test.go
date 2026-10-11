//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsConsoleEventHelper   = "SPROUT_TEST_WINDOWS_CONSOLE_EVENT"
	windowsConsoleEventLauncher = "SPROUT_TEST_WINDOWS_CONSOLE_LAUNCHER"
)

var (
	consoleKernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procAllocConsole            = consoleKernel32.NewProc("AllocConsole")
	procAllocConsoleWithOptions = consoleKernel32.NewProc("AllocConsoleWithOptions")
	procFreeConsole             = consoleKernel32.NewProc("FreeConsole")
	procGetConsoleProcessList   = consoleKernel32.NewProc("GetConsoleProcessList")
	procGetConsoleWindow        = consoleKernel32.NewProc("GetConsoleWindow")
	procSetConsoleCtrlHandler   = consoleKernel32.NewProc("SetConsoleCtrlHandler")
)

// TestProcessContextHandlesWindowsConsoleEvents sends each console event to
// a helper in a console of its own. Go's runtime hooks console events as a
// process starts, and only for a console the process has by then, so the
// helper must start in its console. The test binary carries dens.exe's
// manifest, whose console allocation policy gives a process no console it
// didn't inherit, even one CREATE_NEW_CONSOLE asks for (Windows 11 24H2 and
// Server 2025), so a launcher in between makes the console, starts the
// helper in it, and leaves it to the helper, which waits until it's alone
// there before sending the event.
func TestProcessContextHandlesWindowsConsoleEvents(t *testing.T) {
	if event := os.Getenv(windowsConsoleEventLauncher); event != "" {
		launchWindowsConsoleEventHelper(t, event)
		return
	}
	if event := os.Getenv(windowsConsoleEventHelper); event != "" {
		runWindowsConsoleEventHelper(t, event)
		return
	}

	for _, event := range []string{"ctrl-c", "ctrl-break", "ctrl-close"} {
		t.Run(event, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessContextHandlesWindowsConsoleEvents$")
			cmd.Env = append(os.Environ(), windowsConsoleEventLauncher+"="+event)
			// Detached, the launcher shares no console with this test.
			cmd.SysProcAttr = &syscall.SysProcAttr{
				CreationFlags: windows.DETACHED_PROCESS,
				HideWindow:    true,
			}
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("helper timed out: %v\n%s", ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("helper failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), "context canceled") {
				t.Fatalf("helper did not observe cancellation:\n%s", output)
			}
		})
	}
}

// launchWindowsConsoleEventHelper makes a console, starts the helper in it,
// and leaves it once the helper is in, since an event reaches every process
// in the console, and one the launcher can't take would end it.
func launchWindowsConsoleEventHelper(t *testing.T, event string) {
	t.Helper()
	if err := allocHiddenConsole(); err != nil {
		t.Fatalf("allocate a console: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessContextHandlesWindowsConsoleEvents$")
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, windowsConsoleEventLauncher+"=") })
	cmd.Env = append(env, windowsConsoleEventHelper+"="+event)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	if err := waitForConsole(func(pids []uint32) bool { return slices.Contains(pids, uint32(cmd.Process.Pid)) }); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("the helper never joined the console: %v", err)
	}
	procFreeConsole.Call()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper failed: %v", err)
	}
}

// allocHiddenConsole gives the launcher a console whose window stays hidden
// where Windows can (AllocConsoleWithOptions, from 24H2), and a plain one
// before.
func allocHiddenConsole() error {
	if procAllocConsoleWithOptions.Find() != nil {
		if ok, _, err := procAllocConsole.Call(); ok == 0 {
			return err
		}
		return nil
	}
	const allocConsoleModeNewWindow = 1
	options := struct {
		mode          int32
		useShowWindow int32
		showWindow    uint16
	}{mode: allocConsoleModeNewWindow, useShowWindow: 1, showWindow: windows.SW_HIDE}
	var result int32
	hr, _, _ := procAllocConsoleWithOptions.Call(uintptr(unsafe.Pointer(&options)), uintptr(unsafe.Pointer(&result)))
	if int32(hr) < 0 {
		return fmt.Errorf("AllocConsoleWithOptions: %#x", uint32(hr))
	}
	return nil
}

// waitForConsole waits until the processes in this process's console are
// as ok wants them.
func waitForConsole(ok func(pids []uint32) bool) error {
	var pids [16]uint32
	for range 500 {
		n, _, err := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
		if n == 0 {
			return err
		}
		if ok(pids[:min(int(n), len(pids))]) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("timed out")
}

func runWindowsConsoleEventHelper(t *testing.T, event string) {
	t.Helper()
	ctx, stopSignals := notifyProcessContext(context.Background())
	defer stopSignals()
	self := uint32(os.Getpid())
	if err := waitForConsole(func(pids []uint32) bool { return len(pids) == 1 && pids[0] == self }); err != nil {
		t.Fatalf("the launcher never left the console: %v", err)
	}

	var err error
	switch event {
	case "ctrl-c":
		// A process may start with Ctrl+C ignored, which it inherits.
		procSetConsoleCtrlHandler.Call(0, 0)
		err = windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, 0)
	case "ctrl-break":
		err = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, 0)
	case "ctrl-close":
		// Close is the safely reproducible member of the termination-event
		// branch that Go also uses for logoff and shutdown.
		err = postConsoleClose()
	default:
		t.Fatalf("unknown console event %q", event)
	}
	if err != nil {
		t.Fatalf("send %s: %v", event, err)
	}

	select {
	case <-ctx.Done():
		fmt.Println("context canceled")
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not cancel the process context", event)
	}
}

func postConsoleClose() error {
	handle, _, callErr := procGetConsoleWindow.Call()
	if handle == 0 {
		return fmt.Errorf("get console window: %v", callErr)
	}

	const wmClose = 0x0010
	postMessage := windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")
	ok, _, callErr := postMessage.Call(handle, wmClose, 0, 0)
	if ok == 0 {
		return fmt.Errorf("post WM_CLOSE: %v", callErr)
	}
	return nil
}
