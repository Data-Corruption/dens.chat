package maintenance

import (
	"golang.org/x/sys/windows"
)

// replaceFile atomically replaces to with from. MoveFileEx with
// MOVEFILE_REPLACE_EXISTING is atomic on NTFS for files in one directory.
func replaceFile(from, to string) error {
	fromPtr, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPtr, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(fromPtr, toPtr, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Windows has no portable directory sync; MOVEFILE_WRITE_THROUGH already
// waits for the rename to reach the disk.
func syncDir(string) error { return nil }
