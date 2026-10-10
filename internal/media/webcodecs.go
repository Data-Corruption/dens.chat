package media

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// Copies the page makes (M5.5). In Chrome and Edge, the page makes a
// video's copy itself with WebCodecs, often on the graphics card and many
// times faster than the module: the module hands it the video's packets
// (ffmpeg.Runner.Demux), with what WebCodecs decodes them by, and puts the
// AV1 packets the page sends back together with the video's sound, as it
// puts its own chunks together.

// PageVideoSide is the longer side of a copy the page makes at most: 1080p.
const PageVideoSide = 1920

// Decoding is what the page decodes a video's packets by, in WebCodecs'
// terms: its codec string, its configuration record, the frames' coded size
// and the container's crop of them, which the page applies, as top,
// bottom, left and right, each even, since a 4:2:0 frame's visible
// rectangle starts on an even pixel.
type Decoding struct {
	Codec       string `json:"codec"`
	Description []byte `json:"description,omitempty"`
	CodedWidth  int    `json:"coded_width"`
	CodedHeight int    `json:"coded_height"`
	Crop        [4]int `json:"crop"`
}

// ErrNoCodec is a video whose packets WebCodecs can't be told how to decode.
var ErrNoCodec = errors.New("no WebCodecs codec string for this video")

// PageDecoding says what the page decodes a video's packets by, from what
// the module found demuxing them.
func PageDecoding(d ffmpeg.Demuxed) (Decoding, error) {
	extra, err := hex.DecodeString(d.Extradata)
	if err != nil {
		return Decoding{}, ErrNoCodec
	}
	dec := Decoding{CodedWidth: d.Width, CodedHeight: d.Height}
	for i, c := range d.Crop {
		dec.Crop[i] = max(c, 0) &^ 1
	}
	if dec.Crop[0]+dec.Crop[1] >= d.Height || dec.Crop[2]+dec.Crop[3] >= d.Width {
		dec.Crop = [4]int{}
	}
	switch d.Codec {
	case "h264":
		// An avcC record names the profile, its constraints and the level.
		if len(extra) < 7 || extra[0] != 1 {
			return Decoding{}, ErrNoCodec
		}
		dec.Codec = fmt.Sprintf("avc1.%02x%02x%02x", extra[1], extra[2], extra[3])
		dec.Description = extra
	case "hevc":
		if dec.Codec = hevcCodec(extra); dec.Codec == "" {
			return Decoding{}, ErrNoCodec
		}
		dec.Description = extra
	case "vp8":
		dec.Codec = "vp8"
	case "vp9":
		// Profiles 0 and 1 are eight-bit, and 2 and 3 more; a level
		// FFmpeg doesn't know is 4.1, which only bounds what a decoder
		// may need.
		if d.Profile < 0 || d.Profile > 3 {
			d.Profile = 0
		}
		depth := d.BitDepth
		if depth != 8 && depth != 10 && depth != 12 {
			depth = 8
			if d.Profile >= 2 {
				depth = 10
			}
		}
		level := d.Level
		if level < 10 || level > 62 {
			level = 41
		}
		dec.Codec = fmt.Sprintf("vp09.%02d.%02d.%02d", d.Profile, level, depth)
	case "av1":
		if dec.Codec = av1Codec(extra); dec.Codec == "" {
			return Decoding{}, ErrNoCodec
		}
	default:
		return Decoding{}, ErrNoCodec
	}
	return dec, nil
}

// hevcCodec names an HEVC stream by its hvcC record, as ISO/IEC 14496-15,
// E.3 asks: its profile space and profile, its compatibility flags
// bit-reversed, its tier and level, and its constraint bytes without the
// zeros that end them.
func hevcCodec(b []byte) string {
	if len(b) < 23 || b[0] != 1 {
		return ""
	}
	space, tier, profile := b[1]>>6, (b[1]>>5)&1, b[1]&31
	compat := binary.BigEndian.Uint32(b[2:6])
	var rev uint32
	for i := range 32 {
		rev |= ((compat >> i) & 1) << (31 - i)
	}
	s := "hvc1." + []string{"", "A", "B", "C"}[space] + strconv.Itoa(int(profile)) + "." + strconv.FormatUint(uint64(rev), 16) +
		"." + []string{"L", "H"}[tier] + strconv.Itoa(int(b[12]))
	cons := b[6:12]
	n := len(cons)
	for n > 0 && cons[n-1] == 0 {
		n--
	}
	for _, c := range cons[:n] {
		s += "." + strings.ToUpper(strconv.FormatUint(uint64(c), 16))
	}
	return s
}

// av1Codec names an AV1 stream by its av1C record, as the AV1 codec ISO
// media file format binding asks: its profile, level and tier, and depth.
func av1Codec(b []byte) string {
	if len(b) < 4 || b[0] != 0x81 {
		return ""
	}
	profile, level := b[1]>>5, b[1]&31
	tier := "M"
	if b[2]&0x80 != 0 {
		tier = "H"
	}
	depth := 8
	switch {
	case b[2]&0x40 != 0 && profile == 2 && b[2]&0x20 != 0:
		depth = 12
	case b[2]&0x40 != 0:
		depth = 10
	}
	return fmt.Sprintf("av01.%d.%02d%s.%02d", profile, level, tier, depth)
}

// PageEncoding is how the page encodes a video's copy, in WebCodecs' terms:
// AV1's main profile at level 4.0, which takes 1080p at 30 frames a second,
// in eight bits, at the plan's size and bitrate, a keyframe at least every
// KeyframeUS, from StartUS, the frame the video starts at.
type PageEncoding struct {
	Codec      string `json:"codec"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	FPS        int    `json:"fps"`
	Bitrate    int    `json:"bitrate"`
	KeyframeUS int64  `json:"keyframe_us"`
	StartUS    int64  `json:"start_us"`
}

// PageAV1 is the AV1 the page encodes.
const PageAV1 = "av01.0.08M.08"

// copyKeyframes is how often a copy's keyframes come, for seeking, as the
// module's do.
const copyKeyframes = 5_000_000

// PageEncodingFor says how the page encodes a copy the plan describes.
func PageEncodingFor(p VideoPlan, s ffmpeg.Scanned) PageEncoding {
	return PageEncoding{Codec: PageAV1, Width: p.Width, Height: p.Height, FPS: p.FPS, Bitrate: p.KBPS * 1000,
		KeyframeUS: copyKeyframes, StartUS: s.StartUS}
}

// MuxPageCopy puts together the copy of full, whose video stream the module
// probed as v, from packets, the AV1 records the page made as plan says,
// and full's sound, and writes it to out, checked as any copy the module
// makes.
func MuxPageCopy(ctx context.Context, m *ffmpeg.Runner, full ffmpeg.Input, v ffmpeg.Stream, plan VideoPlan,
	packets ffmpeg.Input, out ffmpeg.Output) (VideoCopy, error) {
	muxed, err := m.Mux(ctx, full, packets, out, plan.Width, plan.Height)
	if err != nil {
		return VideoCopy{}, err
	}
	if muxed.VideoPackets == 0 || muxed.Bytes != out.Size() {
		return VideoCopy{}, &ffmpeg.JobError{Err: ffmpeg.ErrFailed, Reason: "a copy that isn't what the page made"}
	}
	return checkCopy(ctx, m, v, plan, muxed, out)
}
