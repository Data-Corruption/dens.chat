package host

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func testControlPath(t *testing.T) string {
	return fmt.Sprintf(`\\.\pipe\dens-test-%d-%d`, windows.GetCurrentProcessId(), time.Now().UnixNano())
}

// otherAccountID is a SID the test process doesn't run as: LocalService.
func otherAccountID() string { return "S-1-5-19" }

func TestListenControlRefusesClaimedName(t *testing.T) {
	me, err := CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	path := testControlPath(t)
	ln, err := ListenControl(path, me)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if second, err := ListenControl(path, me); err == nil {
		second.Close()
		t.Fatal("a second listener joined an existing pipe name")
	}
}

func TestHostKeyRoundTripThroughDPAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datakey.dpapi")
	key := make([]byte, HostKeySize)
	for i := range key {
		key[i] = byte(255 - i)
	}
	if err := WrapHostKey(path, key); err != nil {
		t.Fatalf("WrapHostKey: %v", err)
	}
	got, err := LoadHostKey(path)
	if err != nil {
		t.Fatalf("LoadHostKey: %v", err)
	}
	if string(got) != string(key) {
		t.Fatal("unwrapped key differs")
	}
}

func TestStartLowPriorityUsesIdleClass(t *testing.T) {
	cmd := exec.Command("ping", "-n", "3", "127.0.0.1")
	if err := StartLowPriority(cmd); err != nil {
		t.Fatalf("StartLowPriority: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	class, err := windows.GetPriorityClass(process)
	if err != nil {
		t.Fatal(err)
	}
	if class != windows.IDLE_PRIORITY_CLASS {
		t.Fatalf("priority class = %#x, want IDLE_PRIORITY_CLASS", class)
	}
}

func TestServiceStatusOfMissingService(t *testing.T) {
	_, err := ServiceStatus(fmt.Sprintf("dens-test-missing-%d", time.Now().UnixNano()))
	if !errors.Is(err, ErrServiceNotInstalled) {
		t.Fatalf("ServiceStatus = %v, want ErrServiceNotInstalled", err)
	}
}
