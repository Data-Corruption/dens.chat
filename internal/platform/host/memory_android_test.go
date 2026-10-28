package host

import (
	"bytes"
	"testing"

	"golang.org/x/sys/unix"
)

// Android lets an app lock 64 KiB (M12): past it, keys go in ordinary
// memory rather than failing to open the vault. The test holds itself to
// an app's limit, which a process adb starts doesn't have.
func TestAllocLockedGoesPastAndroidsLimit(t *testing.T) {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit); err != nil {
		t.Fatal(err)
	}
	app := limit
	app.Cur = min(limit.Cur, 64<<10)
	if err := unix.Setrlimit(unix.RLIMIT_MEMLOCK, &app); err != nil {
		t.Fatal(err)
	}
	defer unix.Setrlimit(unix.RLIMIT_MEMLOCK, &limit)
	var frees []func()
	defer func() {
		for _, free := range frees {
			free()
		}
	}()
	for i := range 64 {
		buf, free, err := AllocLocked(16 << 10)
		if err != nil {
			t.Fatalf("allocation %d of 16 KiB: %v", i+1, err)
		}
		frees = append(frees, free)
		for j := range buf {
			buf[j] = byte(i)
		}
		if !bytes.Equal(buf, bytes.Repeat([]byte{byte(i)}, len(buf))) {
			t.Fatalf("allocation %d didn't hold its data", i+1)
		}
	}
}
