package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// sensitive matches tag names that can say where, when, on what or by whom
// a file was made. It errs wide: a structural tag it catches is checked by
// hand once, a personal one it misses is a leak.
var sensitive = regexp.MustCompile(`(?i)gps|locat|coordinat|latitude|longitude|altitude|make|model|serial|software|` +
	`date|time(?:stamp|zone)|creat|owner|author|artist|copyright|comment|title|description|identifier|uuid|` +
	`device|lens|camera|encoder|user|vendor|product|version|signature|content|livephoto|motion|xmp|exif|icc|` +
	`focal|orientation|maker|firmware|name|address|city|country|phone|email`)

// structural are names the pattern catches that describe the stream, not its
// maker or where it was; they're reported but don't count as left behind.
// FFmpeg writes some itself: the file type's minor version, MP4's standard
// "Apple" handler vendor, its own vendor code, AAC's roll recovery group, and
// "ffmpeg" as the Vorbis comment's vendor. Others are the format's own
// versions, and FLAC's checksum of the decoded audio.
var structural = regexp.MustCompile(`(?i)^(` +
	`QuickTime:(MovieHeaderVersion|CurrentTime|PosterTime|PreviewTime|SelectionTime|TimeScale|MinorVersion|HandlerVendorID)|` +
	`Track\d+:(VendorID|SampleGroupDescription)|Vorbis:Vendor|Opus:OpusVersion|` +
	`Matroska:(DocTypeReadVersion|DocTypeVersion|EBMLReadVersion|EBMLVersion)|MPEG:MPEGAudioVersion|FLAC:MD5Signature|` +
	`Track\d+:(MediaHeaderVersion|TrackHeaderVersion|MediaTimeScale|CompositionTimeToSample|TimeToSampleTable|` +
	`SampleTime|HandlerDescription|CompositionToDecodeTimelineMapping)` +
	`)$`)

// zeroDate is how QuickTime writes a date it doesn't have.
var zeroDate = regexp.MustCompile(`^0000:00:00 00:00:00`)

func exiftags(exiftool, file string) (map[string]string, error) {
	out, err := exec.Command(exiftool, "-j", "-a", "-u", "-ee", "-G1", "-s", "-n", file).Output()
	if err != nil {
		return nil, fmt.Errorf("exiftool %s: %w", file, err)
	}
	var docs []map[string]any
	if err := json.Unmarshal(out, &docs); err != nil || len(docs) != 1 {
		return nil, fmt.Errorf("exiftool %s: unexpected output", file)
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
	return tags, nil
}

// check compares a file and its stripped copy, by tag name only: it never
// prints a value.
func check(exiftool, before, after string) int {
	b, err := exiftags(exiftool, before)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	a, err := exiftags(exiftool, after)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	removed, left, zeroed := []string{}, []string{}, []string{}
	icc := 0
	for k := range b {
		if _, ok := a[k]; !ok && sensitive.MatchString(k) {
			removed = append(removed, k)
		}
	}
	for k, v := range a {
		switch {
		// Dens keeps an image's ICC profile, as the protocol lists, since
		// its colors depend on it.
		case strings.HasPrefix(k, "ICC"):
			icc++
		case !sensitive.MatchString(k) || structural.MatchString(k):
		case zeroDate.MatchString(v) || v == "0" || v == "":
			zeroed = append(zeroed, k)
		default:
			left = append(left, k)
		}
	}
	for _, s := range [][]string{removed, left, zeroed} {
		slices.Sort(s)
	}
	res := map[string]any{
		"file": before, "tags_before": len(b), "tags_after": len(a),
		"removed": removed, "zeroed": zeroed, "icc_tags": icc,
		// Anything here is a leak until shown otherwise, whether or not its
		// value changed.
		"left": left,
	}
	out, _ := json.Marshal(res)
	fmt.Println(string(out))
	if len(left) > 0 {
		return 1
	}
	return 0
}
