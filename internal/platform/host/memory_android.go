package host

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// AllocLocked returns n bytes outside the Go heap, locked into RAM where
// Android lets an app lock them, and in ordinary memory past that. free
// zeroes, unlocks and releases them. The Go collector never moves or
// copies this memory.
//
// Since Android 14 an app may lock only 64 KiB, 16 pages of 4 KB or 4 of
// 16 KB, where a member of two dens needs five (M12). Android swaps to
// compressed memory rather than storage, so the keys past the limit stay
// in RAM, if compressed; Samsung lets an app lock 64 MiB.
func AllocLocked(n int) (buf []byte, free func(), err error) {
	size := roundUpToPage(n)
	mem, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return nil, nil, fmt.Errorf("map key memory: %w", err)
	}
	locked := true
	if err := unix.Mlock(mem); err != nil {
		// Past the limit, mlock says ENOMEM, or EPERM where the limit is
		// nothing at all.
		if !errors.Is(err, unix.ENOMEM) && !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EAGAIN) {
			_ = unix.Munmap(mem)
			return nil, nil, fmt.Errorf("lock key memory: %w", err)
		}
		locked = false
	}
	// Keep key pages out of core dumps even if RLIMIT_CORE is raised later.
	_ = unix.Madvise(mem, unix.MADV_DONTDUMP)
	free = func() {
		clear(mem)
		if locked {
			_ = unix.Munlock(mem)
		}
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
