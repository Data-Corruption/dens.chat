package main

import (
	"time"

	"golang.org/x/sys/windows"
)

// cpuTime is the processor time this probe has used.
func cpuTime() time.Duration {
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user) != nil {
		return 0
	}
	ticks := int64(kernel.HighDateTime)<<32 | int64(kernel.LowDateTime)
	ticks += int64(user.HighDateTime)<<32 | int64(user.LowDateTime)
	return time.Duration(ticks * 100)
}
