package host

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AllocLocked returns n bytes outside the Go heap, locked into the working
// set so they are never paged out. free zeroes, unlocks and releases them.
func AllocLocked(n int) (buf []byte, free func(), err error) {
	size := roundUpToPage(n)
	addr, err := windows.VirtualAlloc(0, uintptr(size), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		return nil, nil, fmt.Errorf("allocate key memory: %w", err)
	}
	if err := windows.VirtualLock(addr, uintptr(size)); err != nil {
		_ = windows.VirtualFree(addr, 0, windows.MEM_RELEASE)
		return nil, nil, fmt.Errorf("lock key memory: %w", err)
	}
	// VirtualAlloc memory is outside the Go heap and never moves, so the
	// address can be reinterpreted as a pointer.
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	mem := unsafe.Slice((*byte)(ptr), size)
	free = func() {
		clear(mem)
		_ = windows.VirtualUnlock(addr, uintptr(size))
		_ = windows.VirtualFree(addr, 0, windows.MEM_RELEASE)
	}
	return mem[:n:n], free, nil
}

func roundUpToPage(n int) int {
	page := os.Getpagesize()
	if n <= 0 {
		return page
	}
	return (n + page - 1) / page * page
}
