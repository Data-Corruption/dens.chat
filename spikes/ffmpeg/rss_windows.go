package main

import (
	"os"
	"syscall"
	"unsafe"
)

// Windows reports no peak memory for a finished child, so the worker reports
// its own peak working set as it finishes.
func peakRSSMB(*os.ProcessState) float64 { return 0 }

var getProcessMemoryInfo = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

func selfPeakMB() float64 {
	var c processMemoryCounters
	c.cb = uint32(unsafe.Sizeof(c))
	self, _ := syscall.GetCurrentProcess()
	if r, _, _ := getProcessMemoryInfo.Call(uintptr(self), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); r == 0 {
		return 0
	}
	return float64(c.peakWorkingSetSize) / (1 << 20)
}
