package ffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// The test binary is its own worker: it runs Work when TestWorkerEnv is set,
// as TestRunner sets it for the workers it starts. "die" and "hang" stand in
// for a worker that crashes, or never answers.
func TestMain(m *testing.M) {
	if os.Getenv(TestWorkerEnv) == "1" {
		switch {
		case len(os.Args) > 1 && os.Args[1] == "die":
			os.Exit(7)
		case len(os.Args) > 1 && os.Args[1] == "hang":
			time.Sleep(time.Hour)
		}
		os.Exit(Work(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

func newRunner(t *testing.T, args ...string) *Runner {
	return TestRunner(func(s string) { t.Log("ffmpeg:", s) }, args...)
}

// memFile is an Input and Output in memory.
type memFile struct {
	mu   sync.Mutex
	data []byte
}

func (f *memFile) Size() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.data))
}

func (f *memFile) ReadAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *memFile) WriteAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if end := off + int64(len(p)); end > int64(len(f.data)) {
		f.data = append(f.data, make([]byte, end-int64(len(f.data)))...)
	}
	return copy(f.data[off:], p), nil
}

func load(t *testing.T, name string) *memFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return &memFile{data: data}
}

// What a stripped file may still name: the container's own fields, which
// FFmpeg writes, and a track's language and handler.
var (
	structuralKeys = []string{"major_brand", "minor_version", "compatible_brands", "language", "handler_name",
		"vendor_id", "DURATION", "encoder", "ENCODER"}
	keptSideData = []string{"Display Matrix", "Stereo 3D", "Spherical Mapping", "Frame Cropping",
		"DOVI configuration record", "HEVC configuration", "Content light level metadata",
		"Mastering display metadata", "Ambient viewing environment", "HDR10+ Dynamic Metadata (SMPTE 2094-40)",
		"ICC Profile"}
	// An ISO 6709 location, as QuickTime and Matroska write one.
	iso6709 = regexp.MustCompile(`[+-]\d{2}\.\d{3,}[+-]\d{3}\.\d{3,}`)
	// The user data iPhones put in every frame.
	appleUUID, _ = hex.DecodeString("47564adc5c4c433f94efc5113cd143a8")
)

func TestStripLeavesNothingBehind(t *testing.T) {
	r := newRunner(t)
	ctx := context.Background()
	names := []string{"with-gps.mov", "with-gps.mp4", "meta.mp4", "meta.mkv", "meta.webm", "meta.mp3",
		"meta.m4a", "meta.flac", "meta.ogg", "meta.wav"}
	if raceOn {
		names = []string{"with-gps.mov", "meta.mkv"}
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			in := load(t, name)
			before, err := r.Probe(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			out := &memFile{}
			s, err := r.Strip(ctx, in, out, "")
			if err != nil {
				t.Fatal(err)
			}
			if s.Bytes != out.Size() || s.MIME == "" {
				t.Errorf("stripped: %+v, %d bytes written", s, out.Size())
			}
			after, err := r.Probe(ctx, out)
			if err != nil {
				t.Fatal(err)
			}
			if after.Chapters != 0 {
				t.Errorf("%d chapters left", after.Chapters)
			}
			for _, k := range after.Metadata {
				if !slices.Contains(structuralKeys, k) {
					t.Errorf("container metadata %q left", k)
				}
			}
			for i, st := range after.Streams {
				if st.Type != "video" && st.Type != "audio" || st.AttachedPic {
					t.Errorf("stream %d left: %s %s", i, st.Type, st.Codec)
				}
				for _, k := range st.Metadata {
					if !slices.Contains(structuralKeys, k) {
						t.Errorf("stream %d metadata %q left", i, k)
					}
				}
				for _, sd := range st.SideData {
					if !slices.Contains(keptSideData, sd) {
						t.Errorf("stream %d side data %q left", i, sd)
					}
				}
			}
			if v, ok := before.Video(); ok {
				w, _ := after.Video()
				if w.Turn != v.Turn || w.Width != v.Width || w.Height != v.Height {
					t.Errorf("the video showed %dx%d %s, and shows %dx%d %s", v.Width, v.Height, v.Turn, w.Width, w.Height, w.Turn)
				}
			}
			for _, leak := range [][]byte{[]byte("TestPhone"), []byte("Fixture"), []byte("Taken at home"),
				[]byte("com.apple.quicktime"), []byte("GPS 48"), appleUUID} {
				if bytes.Contains(out.data, leak) {
					t.Errorf("the copy holds %q", leak)
				}
			}
			if loc := iso6709.Find(out.data); loc != nil {
				t.Errorf("the copy holds a location: %q", loc)
			}
		})
	}
}

// topBoxes lists an MP4's top-level boxes, in order.
func topBoxes(data []byte) []string {
	var names []string
	for off := 0; off+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[off:]))
		names = append(names, string(data[off+4:off+8]))
		if size == 1 && off+16 <= len(data) {
			size = int(binary.BigEndian.Uint64(data[off+8:]))
		}
		if size < 8 {
			break
		}
		off += size
	}
	return names
}

func TestStripWritesPhoneVideoAsMP4WithItsIndexFirst(t *testing.T) {
	r := newRunner(t)
	in := load(t, "with-gps.mov")
	if !bytes.Contains(in.data, appleUUID) {
		t.Fatal("the fixture lost Apple's user data")
	}
	out := &memFile{}
	s, err := r.Strip(context.Background(), in, out, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Muxer != "mp4" || s.MIME != "video/mp4" || s.SEIDropped == 0 {
		t.Errorf("stripped: %+v", s)
	}
	boxes := topBoxes(out.data)
	if i, j := slices.Index(boxes, "moov"), slices.Index(boxes, "mdat"); i < 0 || j < 0 || i > j {
		t.Errorf("boxes %v: the index isn't before the media", boxes)
	}
}

func TestStillTurnsAHEICIntoAJPEG(t *testing.T) {
	if raceOn {
		t.Skip("decoding 48 tiles under the race detector takes half a minute")
	}
	r := newRunner(t)
	in := load(t, "rotated.heic")
	out := &memFile{}
	img, err := r.Still(context.Background(), in, out, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := Image{Format: "jpeg", Width: 3024, Height: 4032, SourceWidth: 4032, SourceHeight: 3024, Turn: "clock",
		ICCBytes: 548, Bytes: len(out.data), Tiles: 48, TilesPlaced: 48}
	if img != want {
		t.Errorf("still: %+v, want %+v", img, want)
	}
	if !bytes.HasPrefix(out.data, []byte{0xFF, 0xD8, 0xFF}) || !bytes.Contains(out.data, []byte("ICC_PROFILE")) {
		t.Error("the still isn't a JPEG with its profile")
	}
	// Apple names itself in the ICC profile, which stays; not in EXIF.
	for _, leak := range [][]byte{[]byte("Exif"), []byte("iPhone")} {
		if bytes.Contains(out.data, leak) {
			t.Errorf("the still holds %q", leak)
		}
	}
}

func TestPosterFitsAPreview(t *testing.T) {
	r := newRunner(t)
	out := &memFile{}
	img, err := r.Poster(context.Background(), load(t, "with-gps.mov"), out, 640, 3)
	if err != nil {
		t.Fatal(err)
	}
	// 568 × 320, turned a quarter.
	if img.Width != 320 || img.Height != 568 || img.Turn != "clock" || !bytes.HasPrefix(out.data, []byte{0xFF, 0xD8}) {
		t.Errorf("poster: %+v", img)
	}
}

func TestRunsThroughAScratch(t *testing.T) {
	if raceOn {
		t.Skip("covered without the race detector")
	}
	r := newRunner(t)
	in := load(t, "with-gps.mp4")
	s, err := vault.NewScratch(t.TempDir(), "scratch-*")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := r.Strip(context.Background(), in, s, ""); err != nil {
		t.Fatal(err)
	}
	mem := &memFile{}
	if _, err := r.Strip(context.Background(), in, mem, ""); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(io.NewSectionReader(s, 0, s.Size()))
	if err != nil || !bytes.Equal(got, mem.data) {
		t.Errorf("a scratch output differs from one in memory: %v", err)
	}
}

func TestLimitedOutputStopsTheJob(t *testing.T) {
	r := newRunner(t)
	out := &Limited{Output: &memFile{}, Max: 100_000}
	if _, err := r.Strip(context.Background(), load(t, "with-gps.mov"), out, ""); !out.Over() || !errors.Is(err, ErrUnreadable) && !errors.Is(err, ErrFailed) {
		t.Fatalf("a copy past its limit: over %v, %v", out.Over(), err)
	}
	if out.Output.Size() > 100_000 {
		t.Fatalf("%d bytes written past the limit", out.Output.Size())
	}
}

func TestUnreadableFiles(t *testing.T) {
	r := newRunner(t)
	for name, data := range map[string][]byte{
		"empty":   nil,
		"garbage": bytes.Repeat([]byte{0x13, 0x37, 0x42}, 5000),
		"cut":     load(t, "with-gps.mov").data[:4000],
	} {
		_, err := r.Strip(context.Background(), &memFile{data: data}, &memFile{}, "")
		var je *JobError
		if !errors.Is(err, ErrUnreadable) || !errors.As(err, &je) || je.Reason == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A JPEG has no video stream to make a poster from: the decoder for it
	// isn't in the module.
	if _, err := r.Poster(context.Background(), load(t, "meta.mp3"), &memFile{}, 640, 3); !errors.Is(err, ErrUnreadable) {
		t.Errorf("a poster of audio: %v", err)
	}
}

func TestWorkerFailures(t *testing.T) {
	in := load(t, "meta.wav")
	if _, err := newRunner(t, "die").Probe(context.Background(), in); !errors.Is(err, ErrFailed) {
		t.Errorf("a worker that died: %v", err)
	}
	r := newRunner(t, "hang")
	r.timeout = func(Op, int64) time.Duration { return 300 * time.Millisecond }
	start := time.Now()
	_, err := r.Probe(context.Background(), in)
	var je *JobError
	if !errors.As(err, &je) || je.Err != ErrFailed || je.Reason != "it ran past its deadline" {
		t.Errorf("a worker that hung: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("the deadline took %v", time.Since(start))
	}
	// A cap below what the module needs ends the job, not the worker.
	small := newRunner(t)
	small.MemoryMB = 1
	if _, err := small.Probe(context.Background(), in); !errors.Is(err, ErrFailed) {
		t.Errorf("a cap below the module's minimum: %v", err)
	}
}

func TestMemoryHasNoSpareCapacity(t *testing.T) {
	m := newMemory(16 << 20)
	if old := m.Grow(10, 1<<16); old != 0 {
		t.Fatalf("grew from %d", old)
	}
	if len(m.buf) != 10<<16 || cap(m.buf) != len(m.buf) {
		t.Errorf("the module sees %d bytes with capacity %d", len(m.buf), cap(m.buf))
	}
	m.buf[len(m.buf)-1] = 1
	if m.Grow(1, 1<<16) != 10 || m.reserved[(10<<16)-1] != 1 || cap(m.buf) != 11<<16 {
		t.Error("growing moved or lost the memory")
	}
	if m.Grow(1000, 1<<16) != -1 {
		t.Error("grew past the cap")
	}
}
