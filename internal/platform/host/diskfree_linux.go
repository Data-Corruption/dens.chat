package host

import "golang.org/x/sys/unix"

// FreeSpace returns how many bytes the service can still write on the
// filesystem that holds path.
func FreeSpace(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
