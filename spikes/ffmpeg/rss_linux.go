package main

import (
	"os"
	"syscall"
)

func peakRSSMB(ps *os.ProcessState) float64 {
	if ru, ok := ps.SysUsage().(*syscall.Rusage); ok {
		return float64(ru.Maxrss) / 1024
	}
	return 0
}

// selfPeakMB is for platforms where the parent can't read a child's peak;
// here it can.
func selfPeakMB() float64 { return 0 }
