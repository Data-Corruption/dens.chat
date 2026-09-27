package host

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// AllocLocked returns n bytes outside the Go heap, locked into RAM so the
// kernel never writes them to swap. free zeroes, unlocks and releases them.
// The Go collector never moves or copies this memory.
func AllocLocked(n int) (buf []byte, free func(), err error) {
	size := roundUpToPage(n)
	mem, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return nil, nil, fmt.Errorf("map key memory: %w", err)
	}
	if err := unix.Mlock(mem); err != nil {
		_ = unix.Munmap(mem)
		return nil, nil, fmt.Errorf("lock key memory: %w", err)
	}
	// Keep key pages out of core dumps even if RLIMIT_CORE is raised later.
	_ = unix.Madvise(mem, unix.MADV_DONTDUMP)
	free = func() {
		clear(mem)
		_ = unix.Munlock(mem)
		_ = unix.Munmap(mem)
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
