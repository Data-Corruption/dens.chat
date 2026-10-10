package denclient

import (
	"errors"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// A video over a den's limit that can't go says why, in the page's words
// (M5.4).
func TestTooLargeVideo(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&media.TooLong{Longest: 7*time.Minute + 9*time.Second},
			"This video is too long for this den's 25 MB limit, even as a smaller copy. About 7 minutes of it would fit."},
		{&media.TooLong{Longest: 40 * time.Second},
			"This video is too long for this den's 25 MB limit, even as a smaller copy. About 40 seconds of it would fit."},
		{media.ErrNoCopy, "This video is larger than this den's 25 MB limit, and Dens can't make a smaller copy of it."},
		{&ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "trap"},
			"This video is larger than this den's 25 MB limit, and Dens couldn't make a smaller copy of it. It may be damaged."},
	}
	for _, c := range cases {
		var input *InputError
		if err := tooLargeVideo(c.err, 25<<20); !errors.As(err, &input) || err.Error() != c.want {
			t.Errorf("%v: %v", c.err, err)
		}
	}
	if err := tooLargeVideo(ErrTooLarge, 25<<20); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	for n, want := range map[int64]string{25 << 20: "25 MB", 1 << 30: "1 GB", 1536 << 20: "1.5 GB", 512 << 10: "512 KB"} {
		if got := sizeText(n); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", n, got, want)
		}
	}
}
