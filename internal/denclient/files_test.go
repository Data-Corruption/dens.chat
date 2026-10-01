package denclient_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// gpsJPEG is a phone photo: EXIF with the camera, an orientation and GPS
// coordinates, and XMP that repeats them.
func gpsJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1600, 1200))
	for i := range img.Pix {
		img.Pix[i] = byte(i / 5000)
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	entry := func(tag, typ uint16, count, value uint32) []byte {
		e := le.AppendUint16(le.AppendUint16(nil, tag), typ)
		return le.AppendUint32(le.AppendUint32(e, count), value)
	}
	// IFD0 at 8: make (at 50), orientation 6, GPS directory (at 58).
	tiff := append([]byte("II*\x00"), le.AppendUint32(nil, 8)...)
	tiff = append(tiff, le.AppendUint16(nil, 3)...)
	tiff = append(tiff, entry(0x010F, 2, 8, 50)...)
	tiff = append(tiff, entry(0x0112, 3, 1, 6)...)
	tiff = append(tiff, entry(0x8825, 4, 1, 58)...)
	tiff = append(tiff, 0, 0, 0, 0)
	tiff = append(tiff, "PhoneCam"...)
	// GPS directory: latitude 47/1 36/1 2297/100 north.
	tiff = append(tiff, le.AppendUint16(nil, 2)...)
	tiff = append(tiff, entry(1, 2, 2, 'N')...)
	tiff = append(tiff, entry(2, 5, 3, 58+2+24+4)...)
	tiff = append(tiff, 0, 0, 0, 0)
	for _, v := range []uint32{47, 1, 36, 1, 2297, 100} {
		tiff = le.AppendUint32(tiff, v)
	}
	seg := func(marker byte, payload []byte) []byte {
		n := len(payload) + 2
		return append([]byte{0xFF, marker, byte(n >> 8), byte(n)}, payload...)
	}
	photo := append([]byte{}, b.Bytes()[:2]...)
	photo = append(photo, seg(0xE1, append([]byte("Exif\x00\x00"), tiff...))...)
	photo = append(photo, seg(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<exif:GPSLatitude>47,36.38N</exif:GPSLatitude>"))...)
	return append(photo, b.Bytes()[2:]...)
}

func upload(t *testing.T, m *denclient.Manager, denID, name string, data []byte) denclient.Uploaded {
	t.Helper()
	up, err := m.Upload(context.Background(), denID, "", name, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return up
}

func fetch(t *testing.T, m *denclient.Manager, denID, id string, thumb bool) ([]byte, media.Kind, error) {
	t.Helper()
	f, err := m.OpenFile(context.Background(), denID, id, thumb)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	return data, f.Kind, err
}

// A phone photo with GPS data arrives stripped: the den never gets the
// location, and the other member gets the photo, upright, with a preview.
func TestPhotoArrivesStripped(t *testing.T) {
	h, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	photo := gpsJPEG(t)
	up := upload(t, member, denID, "IMG_1234.jpg", photo)
	// Orientation 6 is a quarter turn: it shows 1200 wide, 1600 high.
	if !up.Stripped || up.Type != "image/jpeg" || up.Width != 1200 || up.Height != 1600 || up.Thumb == nil ||
		up.Thumb.Width != 480 || up.Thumb.Height != 640 {
		t.Fatalf("%+v", up)
	}
	if _, err := member.Send(ctx, denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{up.ID}}); err != nil {
		t.Fatal(err)
	}
	got, kind, err := fetch(t, owner, denID, up.ID, false)
	if err != nil || kind != media.JPEG {
		t.Fatalf("the owner fetching the photo: %v, %s", err, kind)
	}
	for _, s := range []string{"PhoneCam", "GPSLatitude", "ns.adobe.com"} {
		if bytes.Contains(got, []byte(s)) {
			t.Errorf("%q arrived", s)
		}
	}
	if _, err := media.Verify(io.Discard, bytes.NewReader(got), media.JPEG); err != nil {
		t.Errorf("what arrived still has metadata: %v", err)
	}
	thumb, kind, err := fetch(t, owner, denID, up.ID, true)
	if err != nil || kind != media.JPEG {
		t.Fatalf("the preview: %v, %s", err, kind)
	}
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(thumb)); err != nil || cfg.Width != 480 || cfg.Height != 640 {
		t.Fatalf("the preview is %+v, %v", cfg, err)
	}
	page, err := owner.History(ctx, denID, channelID, denclient.HistoryQuery{})
	if err != nil || len(page.Messages) != 1 || len(page.Messages[0].Attachments) != 1 {
		t.Fatalf("history: %+v %v", page, err)
	}

	// Deleting the message deletes the photo, and the owner's copy in
	// memory goes with it.
	if err := member.DeleteMessage(ctx, denID, page.Messages[0].ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := fetch(t, owner, denID, up.ID, true); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the deleted photo's preview stayed in the cache")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = h
}

func TestUploadRefusalsAndLimits(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	video := append([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), make([]byte, 64)...)
	var input *denclient.InputError
	if _, err := member.Upload(ctx, denID, "", "clip.mp4", int64(len(video)), bytes.NewReader(video)); !errors.As(err, &input) {
		t.Fatalf("a video: %v", err)
	}
	cut := gpsJPEG(t)[:2000]
	if _, err := member.Upload(ctx, denID, "", "cut.jpg", int64(len(cut)), bytes.NewReader(cut)); !errors.As(err, &input) {
		t.Fatalf("a damaged photo: %v", err)
	}
	limits := denproto.Limits{FileSize: 1 << 20, MemberStorage: 1 << 30, DenStorage: 1 << 31}
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the new limits", func(v denclient.View) bool { return v.Limits == limits })
	big := make([]byte, 1<<20+1)
	if _, err := member.Upload(ctx, denID, "", "big.bin", int64(len(big)), bytes.NewReader(big)); !errors.Is(err, denclient.ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	st, err := member.Storage(ctx, denID)
	if err != nil || st.Used != 0 {
		t.Fatalf("storage %+v %v", st, err)
	}
}

// alphaWebP is a 32x24 lossy WebP with transparency, as libwebp makes one:
// an extended header, alpha and the image, and no metadata.
const alphaWebP = "UklGRnAAAABXRUJQVlA4WAoAAAAQAAAAHwAAFwAAQUxQSAoAAAABB9C/iAhERP8DVlA4IEAAAABQAwCdASogABgAPpFCnEolo6KhqAgAsBIJZQDGqoAAQFEcAAD+7qY//sWctgXj//ucD/ucD/ucD+NspCdnCgAA"

func TestWebPStripped(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	clean, err := base64.StdEncoding.DecodeString(alphaWebP)
	if err != nil {
		t.Fatal(err)
	}
	// The same image with an EXIF chunk, flagged in its extended header.
	webp := append([]byte{}, clean...)
	webp[20] |= 0x08
	webp = append(webp, "EXIF\x08\x00\x00\x00PhoneCam"...)
	binary.LittleEndian.PutUint32(webp[4:], uint32(len(webp)-8))
	up := upload(t, member, denID, "sticker.webp", webp)
	if !up.Stripped || up.Type != "image/webp" || up.Width != 32 || up.Thumb == nil {
		t.Fatalf("%+v", up)
	}
	if _, err := member.Send(ctx, denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "look", Attachments: []string{up.ID}}); err != nil {
		t.Fatal(err)
	}
	got, kind, err := fetch(t, owner, denID, up.ID, false)
	if err != nil || kind != media.WebP || !bytes.Equal(got, clean) {
		t.Fatalf("%s %v, stripped back to the original: %t", kind, err, bytes.Equal(got, clean))
	}
}

func TestAvatar(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	var b bytes.Buffer
	png.Encode(&b, img)
	up := upload(t, member, denID, "avatar.png", b.Bytes())
	if up.Stripped {
		t.Error("a clean PNG was stripped")
	}
	m, err := member.UpdateProfile(ctx, denID, denproto.ProfileRequest{Avatar: &up.ID})
	if err != nil || m.Avatar.ID != up.ID {
		t.Fatalf("%+v %v", m, err)
	}
	v := viewOf(t, owner, denID, "bob's avatar", func(v denclient.View) bool {
		bob, ok := memberIn(v, m.ID)
		return ok && bob.Avatar.ID == up.ID
	})
	_ = v
	if _, kind, err := fetch(t, owner, denID, up.ID, false); err != nil || kind != media.PNG {
		t.Fatalf("the owner fetching bob's avatar: %v %s", err, kind)
	}
	// A new avatar deletes the old one, and the owner's cached copy.
	next := upload(t, member, denID, "avatar2.png", b.Bytes())
	if _, err := member.UpdateProfile(ctx, denID, denproto.ProfileRequest{Avatar: &next.ID}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, owner, denID, "bob's new avatar", func(v denclient.View) bool {
		bob, ok := memberIn(v, m.ID)
		return ok && bob.Avatar.ID == next.ID
	})
	if _, _, err := fetch(t, owner, denID, up.ID, false); err == nil {
		t.Fatal("the old avatar is still served")
	}
}

// A failed upload leaves the rest of its body to the caller, which drains
// it for the browser's sake; nothing else may still be reading it.
func TestFailedUploadHandsBackItsBody(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	limits := denproto.Limits{FileSize: 4 << 20, MemberStorage: 5 << 20, DenStorage: 1 << 30}
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the new limits", func(v denclient.View) bool { return v.Limits == limits })
	upload(t, member, denID, "first.bin", make([]byte, 3<<20))
	for name, data := range map[string][]byte{
		// Refused here after its first bytes.
		"clip.avi": append([]byte("RIFF\x00\x00\x30\x00AVI LIST"), make([]byte, 3<<20)...),
		// Refused by the den before it reads the body: past the member's space.
		"second.bin": make([]byte, 3<<20),
	} {
		body := bytes.NewReader(data)
		if _, err := member.Upload(ctx, denID, "", name, int64(len(data)), body); err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if n, err := io.Copy(io.Discard, body); err != nil || n == 0 {
			t.Fatalf("%s: the caller drained %d bytes, %v", name, n, err)
		}
	}
}
