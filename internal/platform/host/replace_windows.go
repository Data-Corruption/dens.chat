package host

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ReplaceProcess starts path with args as a process of its own, detached
// from this one, which should exit next: Windows can't run another binary
// in a process's place. The shortcut helper takes up a newer binary this
// way after an update (M7). The new process leaves the job this one runs
// in, as Task Scheduler's, which may end with this process, where the job
// allows it.
func ReplaceProcess(path string, args ...string) error {
	flags := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS)
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags | windows.CREATE_BREAKAWAY_FROM_JOB}
	if err := cmd.Start(); err != nil {
		cmd = exec.Command(path, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return err
		}
	}
	return cmd.Process.Release()
}
