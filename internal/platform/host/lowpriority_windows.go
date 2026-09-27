package host

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// StartLowPriority starts cmd in the idle priority class, which every thread
// of the child runs under.
func StartLowPriority(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.IDLE_PRIORITY_CLASS | windows.CREATE_NO_WINDOW
	return cmd.Start()
}
