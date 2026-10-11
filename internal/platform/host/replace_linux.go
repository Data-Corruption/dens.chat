package host

import (
	"os"
	"syscall"
)

// ReplaceProcess runs path with args in this process's place, keeping its
// session and systemd unit, as the shortcut helper does to take up a newer
// binary after an update (M7). It returns only if that fails.
func ReplaceProcess(path string, args ...string) error {
	return syscall.Exec(path, append([]string{path}, args...), os.Environ())
}
