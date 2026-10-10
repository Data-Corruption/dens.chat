package ffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"sync"
	"testing"
)

// Smaller copies of videos (M5.4): scan, encode and mux, on an iPhone's
// video, turned a quarter, of 120 frames over four seconds, with keyframes
// at 0, 1, 2.001667 and 3.001667 seconds.

// record is a packet as encode writes it.
type record struct {
	key            bool
	pts, duration  int64
	size           int
	sequenceHeader bool
}

// records reads a packets file, and fails on one that's malformed.
func records(t *testing.T, data []byte) []record {
	t.Helper()
	var out []record
	for off := 0; off < len(data); {
		if len(data)-off < 24 {
			t.Fatalf("a record cut short at %d", off)
		}
		size := int(binary.LittleEndian.Uint32(data[off:]))
		flags := binary.LittleEndian.Uint32(data[off+4:])
		r := record{key: flags&1 != 0, pts: int64(binary.LittleEndian.Uint64(data[off+8:])),
			duration: int64(binary.LittleEndian.Uint64(data[off+16:])), size: size}
		if flags&^1 != 0 || size == 0 || len(data)-off-24 < size {
			t.Fatalf("a malformed record at %d: flags %x, %d bytes", off, flags, size)
		}
		// libaom starts a keyframe with a temporal delimiter (OBU type 2)
		// and its sequence header (type 1).
		packet := data[off+24 : off+24+size]
		r.sequenceHeader = len(packet) > 2 && packet[0]>>3&15 == 2 && packet[2]>>3&15 == 1
		out = append(out, r)
		off += 24 + size
	}
	return out
}

// stripped is the fixture as the local service sends it full size, which
// its copy is made from.
func stripped(t *testing.T, r *Runner) *memFile {
	t.Helper()
	out := &memFile{}
	if _, err := r.Strip(context.Background(), load(t, "with-gps.mov"), out, ""); err != nil {
		t.Fatal(err)
	}
	return out
}

func skipUnderRace(t *testing.T) {
	if raceOn {
		t.Skip("the race detector slows encoding in the module some fifty times over")
	}
}

func TestScanFindsAVideosKeyframes(t *testing.T) {
	r := newRunner(t)
	s, err := r.Scan(context.Background(), stripped(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Keyframes, []int64{0, 1000000, 2001667, 3001667}) {
		t.Errorf("keyframes at %v", s.Keyframes)
	}
	if s.StartUS != 0 || s.EndUS < 4000000 || s.EndUS > 4010000 || s.Frames != 120 || s.FPS < 29.9 || s.FPS > 30.1 {
		t.Errorf("scanned %+v", s)
	}
	if s.AudioStreams != 1 || s.AudioBytes <= 0 || s.VideoBytes <= s.AudioBytes {
		t.Errorf("scanned %d sounds of %d bytes, beside %d of video", s.AudioStreams, s.AudioBytes, s.VideoBytes)
	}
	// Audio alone has no video to plan a copy of.
	if _, err := r.Scan(context.Background(), load(t, "meta.m4a")); !errors.Is(err, ErrUnreadable) {
		t.Errorf("a scan of audio: %v", err)
	}
}

func TestEncodeMakesAChunkFromItsStart(t *testing.T) {
	skipUnderRace(t)
	r := newRunner(t)
	in := stripped(t, r)
	ctx := context.Background()

	// A chunk from a keyframe to the next but one: its 60 frames, as the
	// video stores them, unturned, starting with a keyframe.
	out := &memFile{}
	var mu sync.Mutex
	var seen []int64
	e, err := r.Encode(ctx, in, out, Chunk{StartUS: 1000000, EndUS: 3001667, MaxSide: 1280, FPS: 30, KBPS: 400}, func(us int64) {
		mu.Lock()
		seen = append(seen, us)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Frames != 60 || e.Packets != 60 || e.Width != 568 || e.Height != 320 || e.Skip {
		t.Errorf("encoded %+v", e)
	}
	recs := records(t, out.data)
	if len(recs) != 60 || !recs[0].key || !recs[0].sequenceHeader {
		t.Fatalf("%d packets, the first %+v", len(recs), recs[0])
	}
	if recs[0].pts != 1000000 || recs[59].pts >= 3001667 {
		t.Errorf("the chunk runs from %d to %d", recs[0].pts, recs[59].pts)
	}
	total := 0
	for i, rec := range recs {
		total += rec.size
		if i > 0 && rec.pts <= recs[i-1].pts {
			t.Errorf("packet %d at %d, after %d", i, rec.pts, recs[i-1].pts)
		}
	}
	if int64(total) != e.Bytes {
		t.Errorf("%d bytes of packets, and %d said", total, e.Bytes)
	}
	if len(seen) != 60 || seen[0] != 1000000 || seen[59] != recs[59].pts {
		t.Errorf("progress: %d reports, from %v", len(seen), seen[:min(len(seen), 3)])
	}

	// One that starts between keyframes decodes from the one before, and
	// still starts with a keyframe of its own.
	out = &memFile{}
	if e, err = r.Encode(ctx, in, out, Chunk{StartUS: 1500000, EndUS: 2001667, MaxSide: 1280, FPS: 30, KBPS: 400}, nil); err != nil {
		t.Fatal(err)
	}
	recs = records(t, out.data)
	if e.Frames != 15 || len(recs) != 15 || !recs[0].key || recs[0].pts < 1500000 || recs[0].pts > 1540000 {
		t.Errorf("a chunk from between keyframes: %+v, starting %+v", e, recs[0])
	}

	// At most fps frames a second, and fitting the side it's given.
	out = &memFile{}
	if e, err = r.Encode(ctx, in, out, Chunk{StartUS: 0, EndUS: 2001667, MaxSide: 284, FPS: 15, KBPS: 100}, nil); err != nil {
		t.Fatal(err)
	}
	recs = records(t, out.data)
	if e.Frames != 30 || e.Width != 284 || e.Height != 160 || len(recs) != 30 {
		t.Errorf("at 15 frames a second, fitting 284: %+v", e)
	}
	for i := 1; i < len(recs); i++ {
		if gap := recs[i].pts - recs[i-1].pts; gap < 60000 || gap > 70000 {
			t.Errorf("frames %d and %d are %d µs apart", i-1, i, gap)
		}
	}
}

// copyOf makes the fixture's copy in two chunks, split at its keyframe at
// 2.001667 seconds, and returns the full size and the chunks' packets.
func copyOf(t *testing.T, r *Runner, maxSides ...int) (*memFile, []byte) {
	t.Helper()
	in := stripped(t, r)
	bounds := []int64{0, 2001667, 4100000}
	parts := make([]*memFile, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range parts {
		side := 1280
		if i < len(maxSides) {
			side = maxSides[i]
		}
		parts[i] = &memFile{}
		wg.Go(func() {
			_, errs[i] = r.Encode(context.Background(), in, parts[i],
				Chunk{StartUS: bounds[i], EndUS: bounds[i+1], MaxSide: side, FPS: 30, KBPS: 400}, nil)
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	return in, slices.Concat(parts[0].data, parts[1].data)
}

func TestMuxPutsACopyTogether(t *testing.T) {
	skipUnderRace(t)
	r := newRunner(t)
	ctx := context.Background()
	in, packets := copyOf(t, r)
	out := &memFile{}
	m, err := r.Mux(ctx, in, &memFile{data: packets}, out, 568, 320)
	if err != nil {
		t.Fatal(err)
	}
	if m.Bytes != out.Size() || m.VideoPackets != 120 || m.Keyframes != 2 || m.AudioStreams != 1 || m.AudioPackets < 150 ||
		m.DurationUS < 3990000 || m.DurationUS > 4010000 {
		t.Errorf("muxed %+v, %d bytes written", m, out.Size())
	}
	boxes := topBoxes(out.data)
	if i, j := slices.Index(boxes, "moov"), slices.Index(boxes, "mdat"); i < 0 || j < 0 || i > j {
		t.Errorf("boxes %v: the index isn't before the media", boxes)
	}

	// It keeps the video's turn, and nothing else the full size carries.
	before, err := r.Probe(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	after, err := r.Probe(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := before.Video()
	c, ok := after.Video()
	if !ok || c.Codec != "av1" || c.Turn != v.Turn || c.Turn != "clock" || c.Width != 320 || c.Height != 568 || c.Decoder {
		t.Errorf("the copy's video: %+v", c)
	}
	if len(after.Streams) != 2 || after.Streams[1].Type != "audio" || after.Streams[1].Codec != "aac" {
		t.Errorf("the copy's streams: %+v", after.Streams)
	}
	for _, k := range after.Metadata {
		if !slices.Contains(structuralKeys, k) {
			t.Errorf("container metadata %q", k)
		}
	}
	for _, leak := range [][]byte{[]byte("com.apple.quicktime"), []byte("Lavf"), []byte("Lavc"), appleUUID} {
		if bytes.Contains(out.data, leak) {
			t.Errorf("the copy holds %q", leak)
		}
	}

	// The copy plays from each chunk's keyframe, as a scan of it finds.
	s, err := r.Scan(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Keyframes, []int64{0, 2001667}) || s.Frames != 120 {
		t.Errorf("the copy's frames: %d, keyframes at %v", s.Frames, s.Keyframes)
	}
}

func TestMuxRefusesPacketsThatDontFit(t *testing.T) {
	skipUnderRace(t)
	r := newRunner(t)
	ctx := context.Background()
	in, packets := copyOf(t, r)
	recs := records(t, packets)
	first := 24 + recs[0].size
	second := first + 24 + recs[1].size

	// Packets out of order: the second record's time set before the
	// first's.
	backwards := slices.Clone(packets)
	binary.LittleEndian.PutUint64(backwards[first+8:], 0)
	// A copy that starts without a keyframe.
	keyless := slices.Clone(packets[first:])
	// A record cut short.
	short := slices.Clone(packets[:second-1])
	// Unknown flags.
	flagged := slices.Clone(packets)
	binary.LittleEndian.PutUint32(flagged[4:], 3)
	cases := map[string][]byte{"no packets": nil, "backwards": backwards, "keyless": keyless, "cut short": short,
		"flagged": flagged}
	for name, data := range cases {
		if _, err := r.Mux(ctx, in, &memFile{data: data}, &memFile{}, 568, 320); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Chunks encoded at different sizes have different sequence headers,
	// which one copy can't hold.
	in, mixed := copyOf(t, r, 1280, 284)
	if _, err := r.Mux(ctx, in, &memFile{data: mixed}, &memFile{}, 568, 320); !errors.Is(err, ErrUnreadable) {
		t.Errorf("chunks of two sizes: %v", err)
	}
}
