package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// The test binary is its own media worker, as ffmpeg.TestWorkerEnv says.
func TestMain(m *testing.M) {
	if os.Getenv(ffmpeg.TestWorkerEnv) == "1" {
		os.Exit(ffmpeg.Work(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

func moduleFile(t *testing.T, name string) *ffmpeg.Buffer {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("ffmpeg", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var b ffmpeg.Buffer
	if _, err := b.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	return &b
}

func TestPosterIsACleanPreview(t *testing.T) {
	m := ffmpeg.TestRunner(nil)
	th, err := Poster(context.Background(), m, moduleFile(t, "with-gps.mov"))
	if err != nil {
		t.Fatal(err)
	}
	// 568 × 320, turned a quarter.
	if th.Type != "image/jpeg" || th.Width != 320 || th.Height != 568 {
		t.Fatalf("got a %s of %d × %d", th.Type, th.Width, th.Height)
	}
	if _, err := Verify(io.Discard, bytes.NewReader(th.Data), JPEG); err != nil {
		t.Fatal(err)
	}

	// MPEG-4 Part 2 has no decoder in the module.
	if _, err := Poster(context.Background(), m, moduleFile(t, "meta.mp4")); !errors.Is(err, ffmpeg.ErrUnreadable) {
		t.Fatalf("a video the module can't decode: %v", err)
	}
}

func TestStillIsAnImageToStrip(t *testing.T) {
	if raceOn {
		t.Skip("decoding 48 tiles under the race detector takes half a minute")
	}
	m := ffmpeg.TestRunner(nil)
	var out ffmpeg.Buffer
	img, err := m.Still(context.Background(), moduleFile(t, "rotated.heic"), &out, 0, StillQuality)
	if err != nil {
		t.Fatal(err)
	}
	k := Sniff(out.Bytes())
	if k.MIME() != img.MIME() {
		t.Fatalf("a %s still sniffs as a %s", img.MIME(), k)
	}
	// What the sender's service then does with it, and the den checks.
	var stripped bytes.Buffer
	res, err := Strip(&stripped, bytes.NewReader(out.Bytes()), k)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != img.Width || res.Height != img.Height || res.Removed {
		t.Fatalf("stripping the still: %+v", res)
	}
	if _, err := Verify(io.Discard, bytes.NewReader(stripped.Bytes()), k); err != nil {
		t.Fatal(err)
	}
}
