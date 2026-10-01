package denclient_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

func mediaFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "media", "ffmpeg", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// An ISO 6709 location, as QuickTime and Matroska write one.
var iso6709 = regexp.MustCompile(`[+-]\d{2}\.\d{3,}[+-]\d{3}\.\d{3,}`)

func assertNoLocation(t *testing.T, data []byte) {
	t.Helper()
	for _, leak := range []string{"com.apple.quicktime", "iPhone", "TestPhone", "Exif"} {
		if bytes.Contains(data, []byte(leak)) {
			t.Errorf("%q survived", leak)
		}
	}
	if loc := iso6709.Find(data); loc != nil {
		t.Errorf("a location survived: %q", loc)
	}
}

func attach(t *testing.T, m *denclient.Manager, denID, channel string, files ...string) {
	t.Helper()
	if _, err := m.Send(context.Background(), denID, channel, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: files}); err != nil {
		t.Fatal(err)
	}
}

// A phone video with a location arrives without it, with its preview, as
// an MP4 the page plays inline.
func TestVideoArrivesStripped(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	up := upload(t, member, denID, "IMG_0001.MOV", mediaFile(t, "with-gps.mov"))
	// 568 × 320, turned a quarter, for four seconds.
	if !up.Stripped || up.Converted || up.Type != "video/mp4" || up.Width != 320 || up.Height != 568 ||
		up.Duration < 3900 || up.Duration > 4100 || up.Thumb == nil {
		t.Fatalf("%+v", up)
	}
	attach(t, member, denID, channelID, up.ID)
	f, err := owner.OpenFile(ctx, denID, up.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.PlayType() != "video/mp4" || f.Size() != up.Size {
		t.Fatalf("a %s of %d bytes plays as %q", f.Kind, f.Size(), f.PlayType())
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	assertNoLocation(t, data)
	if _, kind, err := fetch(t, owner, denID, up.ID, true); err != nil || kind != media.JPEG {
		t.Fatalf("its preview: %v %v", kind, err)
	}
}

// In a DM, the sender's service makes the preview, and the den gets only
// what it can't open.
func TestDMVideo(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	video := mediaFile(t, "with-gps.mov")
	up, err := owner.Upload(ctx, denID, dm, "IMG_0001.MOV", int64(len(video)), bytes.NewReader(video))
	if err != nil || !up.Stripped || up.Type != "video/mp4" || up.Width != 320 || up.Height != 568 || up.Duration < 3900 ||
		up.Thumb == nil || up.Thumb.Width != 320 || up.Thumb.Height != 568 {
		t.Fatalf("a DM video: %+v %v", up, err)
	}
	attach(t, owner, denID, dm, up.ID)
	var seen denproto.File
	for _, m := range history(t, member, denID, dm) {
		if len(m.Attachments) == 1 {
			seen = m.Attachments[0]
		}
	}
	if seen.ID != up.ID || seen.Duration != up.Duration || seen.Thumb == nil || seen.Type != "video/mp4" {
		t.Fatalf("bob sees %+v", seen)
	}
	f, err := member.OpenFile(ctx, denID, up.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil || f.PlayType() != "video/mp4" {
		t.Fatalf("bob's copy plays as %q: %v", f.PlayType(), err)
	}
	assertNoLocation(t, data)
}

// An iPhone's HEIC arrives as a JPEG, upright, named for what it is now.
func TestHEICArrivesAsJPEG(t *testing.T) {
	if raceOn {
		t.Skip("decoding 48 tiles under the race detector takes half a minute")
	}
	_, owner, member, denID, channelID := chatDen(t)
	up := upload(t, member, denID, "IMG_0002.HEIC", mediaFile(t, "rotated.heic"))
	if !up.Converted || !up.Stripped || up.Type != "image/jpeg" || up.Name != "IMG_0002.jpg" ||
		up.Width != 3024 || up.Height != 4032 || up.Thumb == nil || up.Thumb.Width != 480 {
		t.Fatalf("%+v", up)
	}
	attach(t, member, denID, channelID, up.ID)
	data, kind, err := fetch(t, owner, denID, up.ID, false)
	if err != nil || kind != media.JPEG {
		t.Fatalf("%v %v", kind, err)
	}
	assertNoLocation(t, data)
}

func TestMediaRefusals(t *testing.T) {
	_, _, member, denID, _ := chatDen(t)
	ctx := context.Background()
	try := func(name string, data []byte, says string) {
		t.Helper()
		_, err := member.Upload(ctx, denID, "", name, int64(len(data)), bytes.NewReader(data))
		if !isInput(err) || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ftyp := func(brands string) []byte {
		head := append([]byte{0, 0, 0, byte(8 + len(brands))}, "ftyp"+brands...)
		return append(head, make([]byte, 100)...)
	}
	try("photo.avif", ftyp("avif\x00\x00\x00\x00avifmif1miaf"), "an AVIF image")
	try("clip.mp4", ftyp("isom\x00\x00\x02\x00isomiso2"), "damaged")
	// A DNG: a TIFF whose first directory names its version.
	le := binary.LittleEndian
	dng := le.AppendUint16(append([]byte("II*\x00"), 8, 0, 0, 0), 1)
	dng = le.AppendUint32(le.AppendUint32(le.AppendUint16(le.AppendUint16(dng, 0xC612), 1), 4), 0x00000401)
	dng = append(dng, make([]byte, 100)...)
	try("photo.dng", dng, "camera raw")
}

func testPreview(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A video the media module can't decode gets the preview the page draws,
// in a channel and in a DM.
func TestPagePreviews(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	setThumb := func(m *denclient.Manager, id string, img []byte) (denclient.Uploaded, error) {
		return m.SetThumb(ctx, denID, id, int64(len(img)), bytes.NewReader(img))
	}
	// MPEG-4 Part 2, 64 × 48, which the module doesn't decode.
	clip := mediaFile(t, "meta.mp4")
	up := upload(t, member, denID, "clip.mp4", clip)
	if up.Thumb != nil || up.Width != 64 || up.Height != 48 {
		t.Fatalf("%+v", up)
	}
	if _, err := setThumb(member, up.ID, testPreview(t, 48, 64)); err == nil {
		t.Fatal("a preview of the wrong shape")
	}
	got, err := setThumb(member, up.ID, testPreview(t, 64, 48))
	if err != nil || got.ID != up.ID || got.Thumb == nil {
		t.Fatalf("%+v %v", got, err)
	}
	attach(t, member, denID, channelID, up.ID)
	if _, kind, err := fetch(t, owner, denID, up.ID, true); err != nil || !kind.Image() {
		t.Fatalf("its preview: %v %v", kind, err)
	}

	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	dup, err := owner.Upload(ctx, denID, dm, "clip.mp4", int64(len(clip)), bytes.NewReader(clip))
	if err != nil || dup.Thumb != nil || dup.Width != 64 {
		t.Fatalf("a DM video: %+v %v", dup, err)
	}
	if _, err := setThumb(owner, dup.ID, testPreview(t, 48, 64)); !isInput(err) {
		t.Fatalf("a DM preview of the wrong shape: %v", err)
	}
	got, err = setThumb(owner, dup.ID, testPreview(t, 32, 24))
	if err != nil || got.ID != dup.ID || got.Thumb == nil || got.Thumb.Width != 32 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := setThumb(owner, dup.ID, testPreview(t, 64, 48)); !isInput(err) {
		t.Fatalf("a second preview: %v", err)
	}
	attach(t, owner, denID, dm, dup.ID)
	// The member's service learns a DM file's key from the message, as the
	// page reads it before it asks for the file.
	history(t, member, denID, dm)
	if _, kind, err := fetch(t, member, denID, dup.ID, true); err != nil || !kind.Image() {
		t.Fatalf("bob's copy of its preview: %v %v", kind, err)
	}
}

// Files too large to keep in memory are read by ranges, sealed or not, as
// a player seeking through a video reads them.
func TestLargeFilesReadByRanges(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	big := make([]byte, 9<<20)
	for i := range big {
		big[i] = byte(i*7 + i>>13)
	}
	check := func(m *denclient.Manager, id string) {
		t.Helper()
		f, err := m.OpenFile(ctx, denID, id, false)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if f.Size() != int64(len(big)) {
			t.Fatalf("%d bytes", f.Size())
		}
		// Back to the start too, as a whole read after the first bytes do.
		for _, at := range []int{5 << 20, 5<<20 + 1000, 100, 9<<20 - 10, 3 << 20, 0} {
			got := make([]byte, 70_000)
			n, err := f.ReadAt(got, int64(at))
			if !bytes.Equal(got[:n], big[at:min(at+len(got), len(big))]) || (err != nil && err != io.EOF) {
				t.Fatalf("at %d: %d bytes, %v", at, n, err)
			}
		}
		if _, err := f.Seek(1<<20, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		rest, err := io.ReadAll(f)
		if err != nil || !bytes.Equal(rest, big[1<<20:]) {
			t.Fatalf("the rest: %d bytes, %v", len(rest), err)
		}
	}
	up := upload(t, member, denID, "big.bin", big)
	attach(t, member, denID, channelID, up.ID)
	check(owner, up.ID)

	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	dup, err := owner.Upload(ctx, denID, dm, "big.bin", int64(len(big)), bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	attach(t, owner, denID, dm, dup.ID)
	history(t, member, denID, dm)
	check(member, dup.ID)
}
