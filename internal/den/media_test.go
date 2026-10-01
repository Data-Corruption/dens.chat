package den

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func mediaFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "media", "ffmpeg", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A video whose sender didn't strip it: the den's own copy has nothing it
// carried but its picture and sound.
func TestUploadStripsVideoAgain(t *testing.T) {
	f, _, member := chatFixture(t)
	file := f.mustUpload(member, "IMG_0001.MOV", mediaFile(t, "with-gps.mov"))
	// 568 × 320, turned a quarter, for four seconds.
	if file.Type != "video/mp4" || file.Width != 320 || file.Height != 568 || file.Duration < 3900 || file.Duration > 4100 ||
		file.Thumb == nil || file.Thumb.Width != 320 || file.Thumb.Height != 568 {
		t.Fatalf("%+v", file)
	}
	if err := denproto.CheckFile(file); err != nil {
		t.Fatal(err)
	}
	got, err := f.read(member, file.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(got)) != file.Size || string(got[4:8]) != "ftyp" {
		t.Fatalf("stored %d bytes, starting %q", len(got), got[:8])
	}
	// The index comes first, so a player starts before the whole file arrives.
	if moov, mdat := bytes.Index(got, []byte("moov")), bytes.Index(got, []byte("mdat")); moov < 0 || moov > mdat {
		t.Fatal("the index isn't first")
	}
	for _, leak := range []string{"com.apple.quicktime", "iPhone", "Apple"} {
		if bytes.Contains(got, []byte(leak)) {
			t.Errorf("%q survived", leak)
		}
	}
	if loc := regexp.MustCompile(`[+-]\d{2}\.\d{3,}[+-]\d{3}\.\d{3,}`).Find(got); loc != nil {
		t.Errorf("a location survived: %q", loc)
	}
	if _, err := f.read(member, file.ID, true); err != nil {
		t.Fatal(err)
	}
	files, temps := f.stored()
	if len(files) != 2 || len(temps) != 0 {
		t.Fatalf("stored %v, temporary %v", files, temps)
	}

	// A player seeking reads from anywhere.
	r, err := f.d.OpenFile(context.Background(), member, file.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	part := make([]byte, 100_000)
	if _, err := r.ReadAt(part, 200_000); err != nil || !bytes.Equal(part, got[200_000:300_000]) {
		t.Fatalf("reading at an offset: %v", err)
	}
}

func TestUploadAudio(t *testing.T) {
	f, _, member := chatFixture(t)
	file := f.mustUpload(member, "tone.flac", mediaFile(t, "meta.flac"))
	if file.Type != "audio/flac" || file.Width != 0 || file.Thumb != nil || file.Duration < 900 || file.Duration > 1100 {
		t.Fatalf("%+v", file)
	}
	got, err := f.read(member, file.ID, false)
	if err != nil || bytes.Contains(got, []byte("TestPhone")) {
		t.Fatalf("stored: %v", err)
	}
}

func TestMediaRefusals(t *testing.T) {
	f, _, member := chatFixture(t)
	ftyp := func(brands string) []byte {
		head := append([]byte{0, 0, 0, byte(8 + len(brands))}, "ftyp"+brands...)
		return append(head, make([]byte, 100)...)
	}
	_, err := f.upload(member, "photo.avif", ftyp("avif\x00\x00\x00\x00avifmif1miaf"))
	wantCode(t, err, denproto.CodeUnsupportedType)
	if !strings.Contains(err.Error(), "AVIF") {
		t.Fatalf("an AVIF's refusal doesn't say why: %v", err)
	}
	// Clients turn a HEIC into a JPEG before it leaves.
	_, err = f.upload(member, "photo.heic", ftyp("heic\x00\x00\x00\x00mif1heic"))
	wantCode(t, err, denproto.CodeUnsupportedType)
	// A video that's only its header.
	_, err = f.upload(member, "clip.mp4", ftyp("isom\x00\x00\x02\x00isomiso2"))
	wantCode(t, err, denproto.CodeInvalidField)

	// A den without the media module takes none.
	f.storage.Media = nil
	f.open()
	_, err = f.upload(member, "tone.flac", mediaFile(t, "meta.flac"))
	wantCode(t, err, denproto.CodeUnsupportedType)
	if _, temps := f.stored(); len(temps) != 0 {
		t.Fatalf("temporary %v", temps)
	}
}

func testPreview(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A video the module can't decode gets its preview from the uploader's page.
func TestSetThumb(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	set := func(s *Session, id string, img []byte) (denproto.File, error) {
		return f.d.SetThumb(ctx, s, id, int64(len(img)), bytes.NewReader(img))
	}
	// MPEG-4 Part 2, 64 × 48, which the module doesn't decode.
	video := f.mustUpload(member, "clip.mp4", mediaFile(t, "meta.mp4"))
	if video.Thumb != nil || video.Width != 64 || video.Height != 48 {
		t.Fatalf("%+v", video)
	}

	_, err := set(owner, video.ID, testPreview(t, 64, 48))
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = set(member, video.ID, testPreview(t, 48, 64))
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = set(member, video.ID, withComment(testJPEG(t, 64, 48)))
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = set(member, video.ID, []byte("not an image"))
	wantCode(t, err, denproto.CodeInvalidField)

	// Drawn smaller, give or take rounding.
	got, err := set(member, video.ID, testPreview(t, 43, 32))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != video.ID || got.Thumb == nil || got.Thumb.Width != 43 || got.Thumb.Height != 32 || got.Duration == 0 {
		t.Fatalf("%+v", got)
	}
	thumb, err := f.read(member, video.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.d.Storage(ctx, member)
	if err != nil || st.Used != video.Size+int64(len(thumb)) {
		t.Fatalf("storage %+v, %v", st, err)
	}
	// It has one now.
	_, err = set(member, video.ID, testPreview(t, 64, 48))
	wantCode(t, err, denproto.CodeInvalidField)

	audio := f.mustUpload(member, "tone.m4a", mediaFile(t, "meta.m4a"))
	_, err = set(member, audio.ID, testPreview(t, 64, 48))
	wantCode(t, err, denproto.CodeInvalidField)
}
