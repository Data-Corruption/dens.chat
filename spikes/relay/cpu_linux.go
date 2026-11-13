package main

import (
	"syscall"
	"time"
)

// cpuTime is the processor time this probe has used.
func cpuTime() time.Duration {
	var u syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &u) != nil {
		return 0
	}
	return time.Duration(u.Utime.Nano() + u.Stime.Nano())
}
