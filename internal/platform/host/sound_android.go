package host

import (
	"context"
	"errors"
)

// CaptureSound reads nothing on Android: the phone plays a relay, and
// never shares its own sound.
func CaptureSound(context.Context, func([]int16, int), func(string)) error {
	return errors.ErrUnsupported
}
