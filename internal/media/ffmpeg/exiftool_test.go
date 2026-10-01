package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A reader independent of FFmpeg checks what stripping leaves, since
// FFmpeg's demuxers skip what they don't know, so FFmpeg's own probe would
// miss it. DENS_EXIFTOOL names exiftool (scripts/vendor.sh exiftool); CI
// sets it.

// sensitive matches tag names that can say where, when, on what or by whom
// a file was made. It errs wide: a structural tag it catches is listed
// below once someone has looked at it, and a personal one it misses is a
// leak.
var sensitive = regexp.MustCompile(`(?i)gps|locat|coordinat|latitude|longitude|altitude|make|model|serial|software|` +
	`date|time(?:stamp|zone)|creat|owner|author|artist|copyright|comment|title|description|identifier|uuid|` +
	`device|lens|camera|encoder|user|vendor|product|version|signature|content|livephoto|motion|xmp|exif|icc|` +
	`focal|orientation|maker|firmware|name|address|city|country|phone|email`)

// structural are names the pattern catches that describe the stream, not
// its maker or where it was. FFmpeg writes some itself: the file type's
// minor version, MP4's standard "Apple" handler vendor, its own vendor code,
// AAC's roll recovery group, and "ffmpeg" as the Vorbis comment's vendor.
// Others are the format's own versions, and FLAC's checksum of the decoded
// audio.
var structural = regexp.MustCompile(`(?i)^(` +
	`QuickTime:(MovieHeaderVersion|CurrentTime|PosterTime|PreviewTime|SelectionTime|TimeScale|MinorVersion|HandlerVendorID)|` +
	`Track\d+:(VendorID|SampleGroupDescription)|Vorbis:Vendor|Opus:OpusVersion|` +
	`Matroska:(DocTypeReadVersion|DocTypeVersion|EBMLReadVersion|EBMLVersion)|MPEG:MPEGAudioVersion|FLAC:MD5Signature|` +
	`Track\d+:(MediaHeaderVersion|TrackHeaderVersion|MediaTimeScale|CompositionTimeToSample|TimeToSampleTable|` +
	`SampleTime|HandlerDescription|CompositionToDecodeTimelineMapping)` +
	`)$`)

// zeroDate is how QuickTime writes a date it doesn't have.
var zeroDate = regexp.MustCompile(`^0000:00:00 00:00:00`)

// exiftags lists what exiftool finds in a file, by group and name.
func exiftags(t *testing.T, exiftool, file string) map[string]string {
	t.Helper()
	out, err := exec.Command(exiftool, "-j", "-a", "-u", "-ee", "-G1", "-s", "-n", file).Output()
	if err != nil {
		t.Fatalf("exiftool on %s: %v", filepath.Base(file), err)
	}
	var docs []map[string]any
	if err := json.Unmarshal(out, &docs); err != nil || len(docs) != 1 {
		t.Fatalf("exiftool on %s: unexpected output", filepath.Base(file))
	}
	tags := map[string]string{}
	for k, v := range docs[0] {
		group, _, _ := strings.Cut(k, ":")
		switch group {
		case "SourceFile", "System", "File", "ExifTool", "Composite":
			continue
		}
		tags[k] = fmt.Sprint(v)
	}
	return tags
}

// TestExiftoolFindsNothingLeft strips every kind of file Dens takes, and
// turns the HEIC into a JPEG, and fails on any tag that could be personal
// left in the copy. It names tags, never their values.
func TestExiftoolFindsNothingLeft(t *testing.T) {
	exiftool := os.Getenv("DENS_EXIFTOOL")
	if exiftool == "" {
		t.Skip("DENS_EXIFTOOL doesn't name exiftool")
	}
	if raceOn {
		t.Skip("covered without the race detector")
	}
	r := newRunner(t)
	for _, name := range []string{"with-gps.mov", "with-gps.mp4", "rotated.heic", "meta.mp4", "meta.mkv", "meta.webm",
		"meta.mp3", "meta.m4a", "meta.flac", "meta.ogg", "meta.wav"} {
		t.Run(name, func(t *testing.T) {
			in := load(t, name)
			out := &memFile{}
			var err error
			if name == "rotated.heic" {
				_, err = r.Still(context.Background(), in, out, 0, 3)
			} else {
				_, err = r.Strip(context.Background(), in, out, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "copy")
			if err := os.WriteFile(path, out.data, 0o600); err != nil {
				t.Fatal(err)
			}
			before := exiftags(t, exiftool, filepath.Join("testdata", name))
			after := exiftags(t, exiftool, path)
			found := 0
			for k := range before {
				if sensitive.MatchString(k) {
					found++
				}
			}
			for k, v := range after {
				switch {
				// An image keeps its ICC profile, as the protocol lists,
				// since its colors depend on it.
				case strings.HasPrefix(k, "ICC"):
				case !sensitive.MatchString(k) || structural.MatchString(k):
				case zeroDate.MatchString(v) || v == "0" || v == "":
				default:
					t.Errorf("%s is left", k)
				}
			}
			t.Logf("%d tags before, %d of them telling; %d after", len(before), found, len(after))
		})
	}
}
