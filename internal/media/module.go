package media

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// What the media module (internal/media/ffmpeg) makes is checked like any
// image a member sends before anyone sees it: the module reads hostile
// files, so its output is untrusted too.

const (
	// StillQuality is a converted photo's JPEG quality, on FFmpeg's scale
	// where 2 is best: about what a phone's camera saves.
	StillQuality = 3
	// posterQuality is a video preview's, near an image preview's.
	posterQuality = 5
)

// Stripped describes a video or audio file the media module stripped.
type Stripped struct {
	// MIME is the type the copy plays as, which its container decides.
	MIME string
	// Width and Height are a video's size as it shows, and Decodes says
	// the module decodes its frames, so it can make its poster. Audio has
	// none of them.
	Width, Height int
	Decodes       bool
	// DurationMS is the copy's length, or 0 when it isn't known.
	DurationMS int64
}

// StripMedia copies a video's or audio's streams from in into out with the
// media module, without anything else it carried, and describes the copy.
func StripMedia(ctx context.Context, m *ffmpeg.Runner, in ffmpeg.Input, out ffmpeg.Output) (Stripped, error) {
	s, err := m.Strip(ctx, in, out, "")
	if err != nil {
		return Stripped{}, err
	}
	p, err := m.Probe(ctx, out)
	if err != nil {
		return Stripped{}, err
	}
	if denproto.CheckMediaType(s.MIME) != nil || !strings.HasPrefix(s.MIME, "video/") && !strings.HasPrefix(s.MIME, "audio/") {
		return Stripped{}, &ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a copy of no type it writes"}
	}
	d := Stripped{MIME: s.MIME}
	if p.DurationMS > 0 && p.DurationMS <= denproto.MaxDuration {
		d.DurationMS = p.DurationMS
	}
	if v, ok := p.Video(); ok && v.Width > 0 && v.Height > 0 && v.Width <= denproto.MaxImageSide && v.Height <= denproto.MaxImageSide {
		d.Width, d.Height, d.Decodes = v.Width, v.Height, v.Decoder
	}
	return d, nil
}

// Poster makes a video's preview with the media module: its first frame,
// upright, fitting ThumbSize.
func Poster(ctx context.Context, m *ffmpeg.Runner, in ffmpeg.Input) (Thumb, error) {
	var buf ffmpeg.Buffer
	img, err := m.Poster(ctx, in, &buf, ThumbSize, posterQuality)
	if err != nil {
		return Thumb{}, err
	}
	data := buf.Bytes()
	k := Sniff(data)
	if k != JPEG {
		return Thumb{}, malformed(JPEG, "a poster that isn't one")
	}
	res, err := Verify(io.Discard, bytes.NewReader(data), k)
	if err != nil {
		return Thumb{}, err
	}
	if res.Width != img.Width || res.Height != img.Height || res.Width > ThumbSize || res.Height > ThumbSize {
		return Thumb{}, malformed(k, "a poster that isn't the size it was made")
	}
	return Thumb{Data: data, Type: k.MIME(), Width: res.Width, Height: res.Height}, nil
}
