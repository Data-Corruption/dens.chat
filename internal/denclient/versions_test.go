package denclient_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// photo is a phone's JPEG of w × h stored pixels, turned a quarter by its
// EXIF orientation, which also names the camera and where it was: grainy,
// as a camera's are, so it's large at full size, with its stored top left
// corner red, which shows at the top right once it's upright.
func photo(t *testing.T, w, h, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for y := range h {
		for x := range w {
			seed ^= seed << 13
			seed ^= seed >> 17
			seed ^= seed << 5
			n := uint8(seed % 48)
			c := color.RGBA{R: 40 + n, G: uint8(60 + x*100/w + int(n)), B: uint8(60 + y*100/h + int(n)), A: 255}
			if x < w/8 && y < h/8 {
				c = color.RGBA{R: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	entry := func(tag, typ uint16, count, value uint32) []byte {
		e := le.AppendUint16(le.AppendUint16(nil, tag), typ)
		return le.AppendUint32(le.AppendUint32(e, count), value)
	}
	tiff := append([]byte("II*\x00"), le.AppendUint32(nil, 8)...)
	tiff = append(tiff, le.AppendUint16(nil, 3)...)
	tiff = append(tiff, entry(0x010F, 2, 8, 50)...)
	tiff = append(tiff, entry(0x0112, 3, 1, 6)...)
	tiff = append(tiff, entry(0x8825, 4, 1, 58)...)
	tiff = append(tiff, 0, 0, 0, 0)
	tiff = append(tiff, "PhoneCam"...)
	tiff = append(tiff, le.AppendUint16(nil, 1)...)
	tiff = append(tiff, entry(1, 2, 2, 'N')...)
	tiff = append(tiff, 0, 0, 0, 0)
	exif := append([]byte("Exif\x00\x00"), tiff...)
	seg := append([]byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	return slices.Concat(b.Bytes()[:2], seg, b.Bytes()[2:])
}

func uploadVersions(t *testing.T, m *denclient.Manager, denID, channel, name string, data []byte, send denclient.Send) denclient.Uploaded {
	t.Helper()
	up, err := m.UploadVersions(context.Background(), denID, channel, name, int64(len(data)), bytes.NewReader(data), send, "")
	if err != nil {
		t.Fatal(err)
	}
	return up
}

// version reads a version kept of a photo waiting to be sent.
func version(t *testing.T, m *denclient.Manager, denID, id string, which denclient.Send) ([]byte, media.Kind, error) {
	t.Helper()
	f, err := m.OpenVersion(denID, id, which)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	return data, f.Kind, err
}

// upright checks a copy of photo: w × h, a JPEG with nothing but its
// pixels, turned so its red corner is at the top right.
func upright(t *testing.T, data []byte, w, h int) {
	t.Helper()
	res, err := media.Verify(io.Discard, bytes.NewReader(data), media.JPEG)
	if err != nil || res.Width != w || res.Height != h || res.Orientation != 1 {
		t.Fatalf("the copy: %+v %v", res, err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return r > 0xC000 && g < 0x4000 && b < 0x4000
	}
	if !at(w-10, 10) || at(10, 10) {
		t.Fatalf("the copy isn't upright: red at its top right %t, top left %t", at(w-10, 10), at(10, 10))
	}
}

// A phone photo goes as a smaller copy, upright and without its details,
// and its sender compares the two and switches to full size and back
// before it's sent (M5).
func TestPhotoGoesSmaller(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	// 2,800 × 1,200 turned a quarter shows 1,200 × 2,800, and its copy
	// fits 2,560.
	data := photo(t, 2800, 1200, 92)
	up := uploadVersions(t, member, denID, channelID, "IMG_3001.jpg", data, denclient.SendSmaller)
	v := up.Versions
	if v == nil || v.Sent != denclient.SendSmaller || up.Width != 1097 || up.Height != 2560 || up.Type != "image/jpeg" ||
		!up.Stripped || up.Name != "IMG_3001.jpg" || up.Thumb == nil {
		t.Fatalf("the upload: %+v", up)
	}
	if v.Smaller != (denclient.Version{Type: "image/jpeg", Size: up.Size, Width: 1097, Height: 2560, Fits: true}) ||
		v.Full.Width != 1200 || v.Full.Height != 2800 || !v.Full.Fits || v.Full.Size*3 < up.Size*4 {
		t.Fatalf("the versions: %+v", v)
	}
	smaller, kind, err := version(t, member, denID, up.ID, denclient.SendSmaller)
	if err != nil || kind != media.JPEG || int64(len(smaller)) != v.Smaller.Size {
		t.Fatalf("the smaller version: %s %d bytes, %v", kind, len(smaller), err)
	}
	upright(t, smaller, 1097, 2560)
	full, kind, err := version(t, member, denID, up.ID, denclient.SendFull)
	if err != nil || kind != media.JPEG || int64(len(full)) != v.Full.Size || bytes.Contains(full, []byte("PhoneCam")) {
		t.Fatalf("the full size: %s %d bytes, %v", kind, len(full), err)
	}

	// Switching uploads the other version, and drops the one waiting.
	big, err := member.SwitchVersion(ctx, denID, up.ID, denclient.SendFull)
	if err != nil {
		t.Fatal(err)
	}
	if big.ID == up.ID || big.Size != v.Full.Size || big.Width != 1200 || big.Height != 2800 || big.Versions.Sent != denclient.SendFull {
		t.Fatalf("switched to full size: %+v", big)
	}
	page, err := member.Files(ctx, denID, "")
	if err != nil || len(page.Files) != 1 || page.Files[0].ID != big.ID {
		t.Fatalf("the member's files after switching: %+v %v", page, err)
	}
	if _, _, err := version(t, member, denID, up.ID, denclient.SendSmaller); err == nil {
		t.Fatal("the versions are still kept under the upload switched from")
	}
	again, err := member.SwitchVersion(ctx, denID, big.ID, denclient.SendFull)
	if err != nil || again.ID != big.ID {
		t.Fatalf("switching to the version it is: %+v %v", again, err)
	}
	back, err := member.SwitchVersion(ctx, denID, big.ID, denclient.SendSmaller)
	if err != nil || back.Size != v.Smaller.Size || back.Versions.Sent != denclient.SendSmaller {
		t.Fatalf("switched back: %+v %v", back, err)
	}

	// Sent, the copy is what arrives, and the versions go.
	attach(t, member, denID, channelID, back.ID)
	got, kind, err := fetch(t, owner, denID, back.ID, false)
	if err != nil || kind != media.JPEG {
		t.Fatalf("the owner fetching the photo: %v %s", err, kind)
	}
	upright(t, got, 1097, 2560)
	if _, _, err := version(t, member, denID, back.ID, denclient.SendFull); err == nil {
		t.Fatal("the versions are still kept once it's sent")
	}
	if _, err := member.SwitchVersion(ctx, denID, back.ID, denclient.SendFull); !isInput(err) {
		t.Fatalf("switching a photo that's sent: %v", err)
	}

	// Sent full size, it goes as it is, stripped, with both kept until then.
	full2 := uploadVersions(t, member, denID, channelID, "IMG_3002.jpg", data, denclient.SendFull)
	if full2.Versions == nil || full2.Versions.Sent != denclient.SendFull || full2.Size != v.Full.Size || full2.Width != 1200 {
		t.Fatalf("a photo sent full size: %+v", full2)
	}
	// Taken off the message, its versions go too.
	if err := member.DropUpload(ctx, denID, full2.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := version(t, member, denID, full2.ID, denclient.SendSmaller); err == nil {
		t.Fatal("the versions are still kept once the upload is dropped")
	}
}

// A photo over the den's limit goes as its copy whatever the setting, and
// can't switch to full size; one whose copy wouldn't save a quarter goes
// full size, as does what has no copy (M5).
func TestPhotoCopyRules(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	limits := denproto.Limits{FileSize: denproto.MinFileSize, MemberStorage: 1 << 30, DenStorage: 1 << 31}
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the new limits", func(v denclient.View) bool { return v.Limits == limits })
	data := photo(t, 2800, 1200, 98)
	if len(data) <= denproto.MinFileSize {
		t.Fatalf("the photo is %d bytes, under the limit", len(data))
	}
	up := uploadVersions(t, member, denID, channelID, "IMG_3003.jpg", data, denclient.SendFull)
	if up.Versions == nil || up.Versions.Sent != denclient.SendSmaller || up.Versions.Full.Fits || up.Height != 2560 {
		t.Fatalf("a photo over the limit: %+v", up)
	}
	if _, err := member.SwitchVersion(ctx, denID, up.ID, denclient.SendFull); !errors.Is(err, denclient.ErrTooLarge) {
		t.Fatalf("switching to a full size over the limit: %v", err)
	}
	// Nothing over the limit without a copy goes.
	big := bytes.Repeat([]byte("b"), denproto.MinFileSize+1)
	if _, err := member.UploadVersions(ctx, denID, channelID, "big.bin", int64(len(big)), bytes.NewReader(big), denclient.SendSmaller, ""); !errors.Is(err, denclient.ErrTooLarge) {
		t.Fatalf("a file over the limit: %v", err)
	}

	// A small photo saved at low quality comes out no smaller as a copy.
	small := photo(t, 640, 480, 40)
	plain := uploadVersions(t, member, denID, channelID, "small.jpg", small, denclient.SendSmaller)
	if plain.Versions != nil || plain.Width != 480 || plain.Height != 640 {
		t.Fatalf("a photo whose copy isn't worth it: %+v", plain)
	}
	// A PNG within the copy's size is only stripped, and so is a WebP.
	var shot bytes.Buffer
	if err := png.Encode(&shot, image.NewRGBA(image.Rect(0, 0, 800, 600))); err != nil {
		t.Fatal(err)
	}
	for name, file := range map[string][]byte{"shot.png": shot.Bytes(), "still.webp": readTestdata(t, "still.webp")} {
		if up := uploadVersions(t, member, denID, channelID, name, file, denclient.SendSmaller); up.Versions != nil {
			t.Fatalf("%s: %+v", name, up)
		}
	}
	if _, err := member.UploadVersions(ctx, denID, channelID, "x.jpg", 1, bytes.NewReader([]byte("x")), "tiny", ""); !isInput(err) {
		t.Fatalf("a version that isn't one: %v", err)
	}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "media", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A PNG larger than a copy stays a PNG, with its transparency, scaled
// down so a screenshot's text stays sharp (M5).
func TestPNGCopy(t *testing.T) {
	if raceOn {
		t.Skip("covered without the race detector")
	}
	_, _, member, denID, channelID := chatDen(t)
	img := image.NewNRGBA(image.Rect(0, 0, 3200, 400))
	seed := uint32(7)
	for i := range img.Pix {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		img.Pix[i] = byte(seed)
		if i%4 == 3 && i/4%3200 >= 1600 {
			img.Pix[i] = 0
		}
	}
	var shot bytes.Buffer
	if err := png.Encode(&shot, img); err != nil {
		t.Fatal(err)
	}
	up := uploadVersions(t, member, denID, channelID, "wide.png", shot.Bytes(), denclient.SendSmaller)
	if up.Versions == nil || up.Type != "image/png" || up.Width != 2560 || up.Height != 320 {
		t.Fatalf("a wide PNG: %+v", up)
	}
	data, kind, err := version(t, member, denID, up.ID, denclient.SendSmaller)
	if err != nil || kind != media.PNG {
		t.Fatalf("the copy: %s %v", kind, err)
	}
	copied, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := copied.At(2500, 100).RGBA(); a != 0 {
		t.Fatalf("the copy lost its transparency: %d", a)
	}
}

// A DM's photo goes as a smaller copy too, sealed, and opens for the other
// member (M5).
func TestDMPhotoGoesSmaller(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	up := uploadVersions(t, owner, denID, dm, "IMG_3004.jpg", photo(t, 2800, 1200, 92), denclient.SendSmaller)
	if up.Versions == nil || up.Versions.Sent != denclient.SendSmaller || up.Height != 2560 || up.Thumb == nil {
		t.Fatalf("a DM's photo: %+v", up)
	}
	big, err := owner.SwitchVersion(ctx, denID, up.ID, denclient.SendFull)
	if err != nil {
		t.Fatal(err)
	}
	back, err := owner.SwitchVersion(ctx, denID, big.ID, denclient.SendSmaller)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{back.ID}}); err != nil {
		t.Fatal(err)
	}
	page, err := owner.Files(ctx, denID, "")
	if err != nil || len(page.Files) != 1 || page.Files[0].ID != back.ID {
		t.Fatalf("the owner's files: %+v %v", page, err)
	}
	var got []byte
	for range 100 {
		if got, _, err = fetch(t, member, denID, back.ID, false); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	upright(t, got, 1097, 2560)
}

// An iPhone's HEIC goes as a smaller copy that keeps its colors and its
// turn, and its full size is the JPEG it would be sent as (M5).
func TestHEICGoesSmaller(t *testing.T) {
	if raceOn {
		t.Skip("decoding 48 tiles under the race detector takes half a minute")
	}
	_, owner, member, denID, channelID := chatDen(t)
	up := uploadVersions(t, member, denID, channelID, "IMG_0002.HEIC", mediaFile(t, "rotated.heic"), denclient.SendSmaller)
	v := up.Versions
	if v == nil || !up.Converted || !up.Stripped || up.Name != "IMG_0002.jpg" || up.Width != 1920 || up.Height != 2560 ||
		v.Full.Width != 3024 || v.Full.Height != 4032 || v.Full.Type != "image/jpeg" {
		t.Fatalf("%+v %+v", up, v)
	}
	attach(t, member, denID, channelID, up.ID)
	data, kind, err := fetch(t, owner, denID, up.ID, false)
	if err != nil || kind != media.JPEG || !bytes.Contains(data, []byte("ICC_PROFILE")) {
		t.Fatalf("the copy: %s %v, with its profile %t", kind, err, bytes.Contains(data, []byte("ICC_PROFILE")))
	}
	assertNoLocation(t, data)
}
