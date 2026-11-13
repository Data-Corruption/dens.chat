package host

import (
	"context"
	"time"
)

// The computer's sound, which Relay plays on another of the member's
// devices (M20.2): the shortcut helper reads it, since it runs in the
// desktop session where the sound plays. Windows reads its default
// output's loopback through WASAPI, and Linux its default output's
// monitor over PulseAudio's protocol, which PipeWire speaks too. Each
// follows the default output as it changes, and opens again when its
// device goes. CaptureSound is the seam:
//
//	CaptureSound(ctx, take, problem) error
//
// reads until ctx ends, handing take each piece as the system gives it:
// frames of 16-bit stereo, interleaved, at a rate, the slice good only
// until take returns. problem hears why nothing reads, as each try to
// open fails, and "" as one succeeds. It returns nil as ctx ends, and
// errors.ErrUnsupported where nothing on the system can read it, as on
// Android.

// soundPause waits before trying the sound again, or until ctx ends.
func soundPause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
