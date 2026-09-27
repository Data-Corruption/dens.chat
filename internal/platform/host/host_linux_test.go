package host

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testControlPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "control.sock")
}

// otherAccountID is a UID the test process doesn't run as.
func otherAccountID() string {
	if os.Getuid() == 0 {
		return "65534"
	}
	return "0"
}

func TestControlSocketIsOpenToLocalUsers(t *testing.T) {
	me, _ := CurrentUser()
	path := testControlPath(t)
	ln, err := ListenControl(path, me)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("socket mode = %04o, want 0666", got)
	}
}

func TestListenControlRefusesNonSocketPath(t *testing.T) {
	path := testControlPath(t)
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	me, _ := CurrentUser()
	if ln, err := ListenControl(path, me); err == nil {
		ln.Close()
		t.Fatal("ListenControl replaced a regular file")
	}
}

func TestLoadHostKeyFromCredentialsDirectory(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, HostKeySize)
	for i := range key {
		key[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, HostKeyCredential), key, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	got, err := LoadHostKey("")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(key) {
		t.Fatal("loaded key differs")
	}

	if err := os.WriteFile(filepath.Join(dir, HostKeyCredential), key[:10], 0o400); err == nil {
		if _, err := LoadHostKey(""); err == nil {
			t.Fatal("short key accepted")
		}
	}
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if _, err := LoadHostKey(""); err == nil {
		t.Fatal("missing CREDENTIALS_DIRECTORY accepted")
	}
}

func TestStartLowPriorityAppliesToEveryThread(t *testing.T) {
	cmd := exec.Command("sleep", "2")
	if err := StartLowPriority(cmd); err != nil {
		t.Fatalf("StartLowPriority: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "stat"))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	// fields[0] is field 3 of stat; nice is field 19 and policy field 41.
	if nice := fields[16]; nice != "19" {
		t.Errorf("nice = %s, want 19", nice)
	}
	if policy := fields[38]; policy != "5" {
		t.Errorf("scheduling policy = %s, want 5 (SCHED_IDLE)", policy)
	}
}

func TestServiceStatusOfMissingUnit(t *testing.T) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("systemctl unavailable")
	}
	if out, err := exec.Command("systemctl", "is-system-running").Output(); err != nil && len(out) == 0 {
		t.Skip("no running systemd")
	}
	_, err := ServiceStatus("dens-test-missing-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".service")
	if !errors.Is(err, ErrServiceNotInstalled) {
		t.Fatalf("ServiceStatus = %v, want ErrServiceNotInstalled", err)
	}
}
