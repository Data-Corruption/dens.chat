package host

import (
	"fmt"
	"os/exec"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// StartLowPriority starts cmd with nice 19 and the SCHED_IDLE policy on every
// one of its threads. Both are per-thread on Linux, so they are set on the
// thread that forks the child, which then inherits them; setting them on the
// child's PID afterwards would miss threads it had already started. The
// forking thread can't raise its own priority back without CAP_SYS_NICE, so
// it stays locked and the runtime discards it when the goroutine exits.
//
// The policy goes through sched_setscheduler, not sched_setattr: systemd's
// RestrictRealtime=, in the service's unit, reads sched_setscheduler's
// policy and lets SCHED_IDLE through, but can't read sched_setattr's, which
// is in a struct, and systemd 262.1 refuses that call whole. Idle scheduling
// is a courtesy to the rest of the computer, not a guard, so where it's
// refused anyway the job runs at nice 19 alone, rather than not at all.
func StartLowPriority(cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.Setpriority(unix.PRIO_PROCESS, 0, 19); err != nil {
			done <- fmt.Errorf("lower thread priority: %w", err)
			return
		}
		_ = idle()
		done <- cmd.Start()
	}()
	return <-done
}

// idle puts the calling thread under SCHED_IDLE.
func idle() error {
	var param struct{ priority int32 }
	if _, _, errno := unix.RawSyscall(unix.SYS_SCHED_SETSCHEDULER, 0, unix.SCHED_IDLE, uintptr(unsafe.Pointer(&param))); errno != 0 {
		return errno
	}
	return nil
}
