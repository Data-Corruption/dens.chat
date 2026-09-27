package host

import (
	"fmt"
	"os/exec"
	"runtime"

	"golang.org/x/sys/unix"
)

// StartLowPriority starts cmd with nice 19 and the SCHED_IDLE policy on every
// one of its threads. Both are per-thread on Linux, so they are set on the
// thread that forks the child, which then inherits them; setting them on the
// child's PID afterwards would miss threads it had already started. The
// forking thread can't raise its own priority back without CAP_SYS_NICE, so
// it stays locked and the runtime discards it when the goroutine exits.
func StartLowPriority(cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.Setpriority(unix.PRIO_PROCESS, 0, 19); err != nil {
			done <- fmt.Errorf("lower thread priority: %w", err)
			return
		}
		attr := unix.SchedAttr{Size: unix.SizeofSchedAttr, Policy: unix.SCHED_IDLE}
		if err := unix.SchedSetAttr(0, &attr, 0); err != nil {
			done <- fmt.Errorf("set idle scheduling: %w", err)
			return
		}
		done <- cmd.Start()
	}()
	return <-done
}
