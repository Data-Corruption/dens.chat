package ffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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

// TestExiftoolFindsNothingLeft strips every kind of file Dens takes, turns
// the HEIC into a JPEG, and makes a phone JPEG's smaller copy, and fails on
// any tag that could be personal left in what comes out. It names tags,
// never their values.
func TestExiftoolFindsNothingLeft(t *testing.T) {
	exiftool := os.Getenv("DENS_EXIFTOOL")
	if exiftool == "" {
		t.Skip("DENS_EXIFTOOL doesn't name exiftool")
	}
	if raceOn {
		t.Skip("covered without the race detector")
	}
	r := newRunner(t)
	// A phone's JPEG, as a smaller copy turns and scales it (M5), as well
	// as the files in testdata.
	phone := filepath.Join(t.TempDir(), "phone.jpg")
	if err := os.WriteFile(phone, phoneJPEG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"with-gps.mov", "with-gps.mp4", "rotated.heic", "meta.mp4", "meta.mkv", "meta.webm",
		"meta.mp3", "meta.m4a", "meta.flac", "meta.ogg", "meta.wav", phone} {
		t.Run(filepath.Base(name), func(t *testing.T) {
			source := name
			if !filepath.IsAbs(name) {
				source = filepath.Join("testdata", name)
			}
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			in, out := &memFile{data: data}, &memFile{}
			switch name {
			case "rotated.heic":
				_, err = r.Still(context.Background(), in, out, 0, 3, 0)
			case phone:
				_, err = r.Still(context.Background(), in, out, 2560, 5, 6)
			default:
				_, err = r.Strip(context.Background(), in, out, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "copy")
			if err := os.WriteFile(path, out.data, 0o600); err != nil {
				t.Fatal(err)
			}
			before := exiftags(t, exiftool, source)
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

// phoneJPEG is a phone's photo: EXIF with the camera, a quarter turn, the
// time it was taken and where, and XMP that repeats the place.
func phoneJPEG(t *testing.T) []byte {
	t.Helper()
	var img bytes.Buffer
	if err := jpeg.Encode(&img, cornered(640, 480, false), nil); err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	entry := func(tag, typ uint16, count, value uint32) []byte {
		e := le.AppendUint16(le.AppendUint16(nil, tag), typ)
		return le.AppendUint32(le.AppendUint32(e, count), value)
	}
	// IFD0 at 8: make (at 62), orientation 6, the time (at 70), and the GPS
	// directory (at 90).
	tiff := append([]byte("II*\x00"), le.AppendUint32(nil, 8)...)
	tiff = append(tiff, le.AppendUint16(nil, 4)...)
	tiff = append(tiff, entry(0x010F, 2, 8, 62)...)
	tiff = append(tiff, entry(0x0112, 3, 1, 6)...)
	tiff = append(tiff, entry(0x0132, 2, 20, 70)...)
	tiff = append(tiff, entry(0x8825, 4, 1, 90)...)
	tiff = append(tiff, 0, 0, 0, 0)
	tiff = append(tiff, "PhoneCam"...)
	tiff = append(tiff, "2026:10:10 12:00:00\x00"...)
	// The GPS directory: latitude 47/1 36/1 2297/100 north.
	tiff = append(tiff, le.AppendUint16(nil, 2)...)
	tiff = append(tiff, entry(1, 2, 2, 'N')...)
	tiff = append(tiff, entry(2, 5, 3, 90+2+24+4)...)
	tiff = append(tiff, 0, 0, 0, 0)
	for _, v := range []uint32{47, 1, 36, 1, 2297, 100} {
		tiff = le.AppendUint32(tiff, v)
	}
	seg := func(marker byte, payload []byte) []byte {
		n := len(payload) + 2
		return append([]byte{0xFF, marker, byte(n >> 8), byte(n)}, payload...)
	}
	return slices.Concat(img.Bytes()[:2], seg(0xE1, append([]byte("Exif\x00\x00"), tiff...)),
		seg(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<exif:GPSLatitude>47,36.38N</exif:GPSLatitude>")), img.Bytes()[2:])
}
