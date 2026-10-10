package media

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// The page's copy goes to 1080p, steps down through 720p to 480p, keeps a
// fifth of the limit for its encoder's overshoot, and is planned for what
// the module doesn't decode too (M5.5).
func TestPlanPageCopy(t *testing.T) {
	v, s := phoneVideo()
	p, err := PlanVideoCopy(v, s, 34_941_781, 0, ByPage, 8)
	if err != nil {
		t.Fatal(err)
	}
	if p.By != ByPage || p.Width != 1920 || p.Height != 1080 || p.KBPS != 2488 || len(p.Chunks) != 0 {
		t.Errorf("the page's copy: %+v", p)
	}
	e := PageEncodingFor(p, s)
	if e != (PageEncoding{Codec: "av01.0.08M.08", Width: 1920, Height: 1080, FPS: 30, Bitrate: 2_488_000, KeyframeUS: 5_000_000}) {
		t.Errorf("the page's encoding: %+v", e)
	}
	// Five minutes in 25 MiB: about 365 kbps, too few for 1080p or 720p.
	s.EndUS = 300_000_000
	s.AudioBytes = 7_200_000
	if p, err = PlanVideoCopy(v, s, 500_000_000, 25<<20, ByPage, 8); err != nil || p.Width != 854 || p.KBPS < 330 || p.KBPS > 400 {
		t.Errorf("a long copy by the page: %+v, %v", p, err)
	}
	// The page decodes what the module doesn't.
	v.Decoder = false
	if _, err := PlanVideoCopy(v, s, 500_000_000, 25<<20, ByPage, 8); err != nil {
		t.Errorf("a video the module doesn't decode: %v", err)
	}
	// Lowered, it keeps to the page.
	if p, err = LowerVideoPlan(p, s, 25<<20, 25<<20, 8); err != nil || p.By != ByPage || len(p.Chunks) != 0 {
		t.Errorf("lowered: %+v, %v", p, err)
	}
}

func hexOf(b ...byte) string { return hex.EncodeToString(b) }

func TestPageDecoding(t *testing.T) {
	// An iPhone's HEVC Main 10: profile 2, its compatibility flag, no tier,
	// level 4.1, and its constraint byte.
	hvcc := make([]byte, 23)
	hvcc[0], hvcc[1], hvcc[2], hvcc[6], hvcc[12] = 1, 0x02, 0x20, 0xB0, 123
	cases := []struct {
		d     ffmpeg.Demuxed
		codec string
		desc  bool
	}{
		{ffmpeg.Demuxed{Codec: "h264", Extradata: hexOf(1, 0x64, 0x00, 0x1f, 0xff, 0xe1, 0)}, "avc1.64001f", true},
		{ffmpeg.Demuxed{Codec: "hevc", Extradata: hex.EncodeToString(hvcc)}, "hvc1.2.4.L123.B0", true},
		{ffmpeg.Demuxed{Codec: "vp8"}, "vp8", false},
		{ffmpeg.Demuxed{Codec: "vp9", Profile: 0, Level: -99}, "vp09.00.41.08", false},
		{ffmpeg.Demuxed{Codec: "vp9", Profile: 2, Level: 30, BitDepth: 10}, "vp09.02.30.10", false},
		{ffmpeg.Demuxed{Codec: "av1", Extradata: hexOf(0x81, 0x08, 0x0c, 0)}, "av01.0.08M.08", false},
		{ffmpeg.Demuxed{Codec: "av1", Extradata: hexOf(0x81, 0x2d, 0xcc, 0)}, "av01.1.13H.10", false},
	}
	for _, c := range cases {
		dec, err := PageDecoding(c.d)
		if err != nil || dec.Codec != c.codec || (len(dec.Description) > 0) != c.desc {
			t.Errorf("%s: %+v, %v; want %s", c.d.Codec, dec, err, c.codec)
		}
	}
	for _, d := range []ffmpeg.Demuxed{{Codec: "mpeg4"}, {Codec: "h264"}, {Codec: "hevc", Extradata: "01"}, {Codec: "av1", Extradata: "zz"}} {
		if _, err := PageDecoding(d); !errors.Is(err, ErrNoCodec) {
			t.Errorf("%+v: %v", d, err)
		}
	}
	// A crop goes on even pixels, and one that leaves nothing goes.
	dec, _ := PageDecoding(ffmpeg.Demuxed{Codec: "vp8", Width: 1440, Height: 1080, Crop: [4]int{51, 49, 66, 66}})
	if dec.Crop != [4]int{50, 48, 66, 66} {
		t.Errorf("the crop: %v", dec.Crop)
	}
	if dec, _ = PageDecoding(ffmpeg.Demuxed{Codec: "vp8", Width: 64, Height: 48, Crop: [4]int{30, 30, 0, 0}}); dec.Crop != [4]int{} {
		t.Errorf("a crop of everything: %v", dec.Crop)
	}
}

// The module's own demux and copies name themselves as the page decodes
// them, and a copy the page made, stood in for by the module's AV1
// packets, goes together with the video's sound (M5.5).
func TestPageCopyFromTheModule(t *testing.T) {
	if raceOn {
		t.Skip("the race detector slows encoding in the module some fifty times over")
	}
	ctx := context.Background()
	m := ffmpeg.TestRunner(nil)
	full := &ffmpeg.Buffer{}
	if _, err := StripMedia(ctx, m, moduleFile(t, "with-gps.mov"), full); err != nil {
		t.Fatal(err)
	}
	d, err := m.Demux(ctx, full, &ffmpeg.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := PageDecoding(d)
	if err != nil || !strings.HasPrefix(dec.Codec, "avc1.") || len(dec.Description) == 0 || dec.CodedWidth != 568 {
		t.Fatalf("the phone video's decoding: %+v, %v", dec, err)
	}

	p, err := m.Probe(ctx, full)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := p.Video()
	s, err := m.Scan(ctx, full)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanVideoCopy(v, s, full.Size(), 0, ByPage, 2)
	if err != nil {
		t.Fatal(err)
	}
	packets := &ffmpeg.Buffer{}
	if _, err := m.Encode(ctx, full, packets, ffmpeg.Chunk{StartUS: s.StartUS, EndUS: s.EndUS + 1e6, MaxSide: PageVideoSide, FPS: plan.FPS,
		KBPS: plan.KBPS}, nil); err != nil {
		t.Fatal(err)
	}
	out := &ffmpeg.Buffer{}
	c, err := MuxPageCopy(ctx, m, full, v, plan, packets, out)
	if err != nil {
		t.Fatal(err)
	}
	if c.Width != 320 || c.Height != 568 || c.VideoPackets != 120 || c.AudioStreams != 1 {
		t.Errorf("the page's copy: %+v", c)
	}
	// The copy, AV1, names itself by its av1C record.
	if d, err = m.Demux(ctx, out, &ffmpeg.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if dec, err = PageDecoding(d); err != nil || !strings.HasPrefix(dec.Codec, "av01.0.") || !strings.HasSuffix(dec.Codec, "M.08") {
		t.Errorf("the copy's decoding: %+v, %v", dec, err)
	}
}
