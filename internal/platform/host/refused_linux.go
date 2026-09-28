package host

import (
	"errors"
	"syscall"
)

// ConnRefused reports whether err is a connection that nothing was
// listening for.
func ConnRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
