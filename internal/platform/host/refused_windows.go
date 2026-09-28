package host

import (
	"errors"

	"golang.org/x/sys/windows"
)

// ConnRefused reports whether err is a connection that nothing was
// listening for. Winsock reports it as WSAECONNREFUSED, which isn't
// syscall.ECONNREFUSED.
func ConnRefused(err error) bool { return errors.Is(err, windows.WSAECONNREFUSED) }
