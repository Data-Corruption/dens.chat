package media

import (
	"bytes"
	"embed"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/webp"
)

// Everything a phone or an editor might leave in a photo carries one of
// these, so a test can see it's gone.
var secrets = []string{"TestCam", "SecretDescription", "SecretXMP", "SecretIPTC", "SecretMPF", "SecretComment",
	"SecretText", "SecretPrivate", "SecretTrailer", "SecretUnknown", "SecretThumb"}

func assertClean(t *testing.T, out []byte) {
	t.Helper()
	for _, s := range secrets {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("%q survived", s)
		}
	}
	// The GPS directory's coordinates.
	if bytes.Contains(out, binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 2297), 100)) {
		t.Error("the GPS latitude survived")
	}
}

type tag struct {
	id, typ uint16
	count   uint32
	data    []byte
}

// tiffIFD lays out one directory starting at start, with the values that
// don't fit in an entry after it.
func tiffIFD(start uint32, tags []tag) []byte {
	bo := binary.LittleEndian
	dataAt := start + 2 + 12*uint32(len(tags)) + 4
	var dir, data []byte
	dir = bo.AppendUint16(dir, uint16(len(tags)))
	for _, t := range tags {
		dir = bo.AppendUint16(dir, t.id)
		dir = bo.AppendUint16(dir, t.typ)
		dir = bo.AppendUint32(dir, t.count)
		if len(t.data) <= 4 {
			dir = append(dir, append(append([]byte{}, t.data...), make([]byte, 4-len(t.data))...)...)
			continue
		}
		dir = bo.AppendUint32(dir, dataAt+uint32(len(data)))
		data = append(data, t.data...)
		if len(data)%2 == 1 {
			data = append(data, 0)
		}
	}
	dir = bo.AppendUint32(dir, 0)
	return append(dir, data...)
}

// phoneEXIF is an EXIF block like a phone writes: the camera, a
// description, an orientation and the GPS coordinates where the photo was
// taken (47°36'22.97"N 122°19'55.03"W).
func phoneEXIF(orientation int) []byte {
	bo := binary.LittleEndian
	rationals := func(v ...uint32) []byte {
		var b []byte
		for _, x := range v {
			b = bo.AppendUint32(b, x)
		}
		return b
	}
	ifd0 := func(gpsAt uint32) []tag {
		return []tag{
			{0x010E, 2, 18, []byte("SecretDescription\x00")},
			{0x010F, 2, 8, []byte("TestCam\x00")},
			{0x0112, 3, 1, bo.AppendUint16(nil, uint16(orientation))},
			{0x8825, 4, 1, bo.AppendUint32(nil, gpsAt)},
		}
	}
	gps := []tag{
		{1, 2, 2, []byte("N\x00")},
		{2, 5, 3, rationals(47, 1, 36, 1, 2297, 100)},
		{3, 2, 2, []byte("W\x00")},
		{4, 5, 3, rationals(122, 1, 19, 1, 5503, 100)},
	}
	gpsAt := 8 + uint32(len(tiffIFD(8, ifd0(0))))
	out := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00")
	out = append(out, tiffIFD(8, ifd0(gpsAt))...)
	return append(out, tiffIFD(gpsAt, gps)...)
}

// pattern has a different color in each quarter, so a test can tell which
// way up it is: red, green; blue, white.
func pattern(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{255, 0, 0, 255}
			switch {
			case x >= w/2 && y < h/2:
				c = color.RGBA{0, 255, 0, 255}
			case x < w/2 && y >= h/2:
				c = color.RGBA{0, 0, 255, 255}
			case x >= w/2 && y >= h/2:
				c = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func segment(marker byte, payload []byte) []byte {
	n := len(payload) + 2
	return append([]byte{0xFF, marker, byte(n >> 8), byte(n)}, payload...)
}

// withSegments puts segments right after a JPEG's start marker.
func withSegments(jpg []byte, segs ...[]byte) []byte {
	out := append([]byte{}, jpg[:2]...)
	for _, s := range segs {
		out = append(out, s...)
	}
	return append(out, jpg[2:]...)
}

// dirtyJPEG is a phone photo: JFIF with a thumbnail, EXIF with GPS, XMP,
// IPTC, a Multi-Picture index with a second image after the end, an ICC
// profile, Adobe's color transform and a comment.
func dirtyJPEG(t *testing.T, w, h, orientation int) (clean, dirty []byte) {
	clean = encodeJPEG(t, pattern(w, h))
	jfif := append([]byte("JFIF\x00\x01\x02\x00\x00\x48\x00\x48\x01\x01"), "SecretThumb"[:3]...)
	dirty = withSegments(clean,
		segment(0xE0, jfif),
		segment(0xE1, phoneEXIF(orientation)),
		segment(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>SecretXMP exif:GPSLatitude</x:xmpmeta>")),
		segment(0xED, []byte("Photoshop 3.0\x008BIM\x04\x04SecretIPTC")),
		segment(0xE2, []byte("MPF\x00SecretMPF")),
		segment(0xE2, []byte("ICC_PROFILE\x00\x01\x01fake profile")),
		segment(0xEE, []byte("Adobe\x00\x64\x00\x00\x00\x00\x01")),
		segment(0xE9, []byte("SecretPrivate")),
		segment(0xFE, []byte("SecretComment")),
	)
	// The Multi-Picture Format appends its other images after the first
	// one's end.
	dirty = append(dirty, withSegments(encodeJPEG(t, pattern(8, 8)), segment(0xFE, []byte("SecretTrailer")))...)
	return clean, dirty
}

func strip(t *testing.T, in []byte, k Kind) ([]byte, Result) {
	t.Helper()
	var out bytes.Buffer
	res, err := Strip(&out, bytes.NewReader(in), k)
	if err != nil {
		t.Fatalf("strip %s: %v", k, err)
	}
	return out.Bytes(), res
}

// checkVerify requires Verify to pass a stripped file through unchanged,
// and Strip to leave it alone, and Verify to refuse the original.
func checkVerify(t *testing.T, stripped, dirty []byte, k Kind) {
	t.Helper()
	var out bytes.Buffer
	res, err := Verify(&out, bytes.NewReader(stripped), k)
	if err != nil || res.Removed {
		t.Fatalf("verify a stripped %s: %v (removed %t)", k, err, res.Removed)
	}
	if !bytes.Equal(out.Bytes(), stripped) {
		t.Errorf("verify changed a stripped %s", k)
	}
	again, res2 := strip(t, stripped, k)
	if res2.Removed || !bytes.Equal(again, stripped) {
		t.Errorf("stripping a stripped %s changed it", k)
	}
	if _, err := Verify(&bytes.Buffer{}, bytes.NewReader(dirty), k); !errors.Is(err, ErrMetadata) {
		t.Errorf("verify the original %s: %v, want ErrMetadata", k, err)
	}
}

func samePixels(t *testing.T, a, b image.Image) {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds %v and %v", a.Bounds(), b.Bounds())
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if color.RGBAModel.Convert(a.At(x, y)) != color.RGBAModel.Convert(b.At(x, y)) {
				t.Fatalf("pixel %d,%d differs", x, y)
			}
		}
	}
}

func TestSniff(t *testing.T) {
	iso := func(brands ...string) []byte {
		b := []byte{0, 0, 0, byte(8 + 4*len(brands) + 4), 'f', 't', 'y', 'p'}
		b = append(b, brands[0]...)
		b = append(b, 0, 0, 0, 0)
		for _, x := range brands[1:] {
			b = append(b, x...)
		}
		return append(b, make([]byte, 32)...)
	}
	ts := make([]byte, 400)
	ts[0], ts[188], ts[376] = 0x47, 0x47, 0x47
	for _, c := range []struct {
		name string
		head []byte
		want Kind
	}{
		{"jpeg", []byte("\xFF\xD8\xFF\xE0"), JPEG},
		{"png", []byte("\x89PNG\r\n\x1a\n...."), PNG},
		{"gif", []byte("GIF89a...."), GIF},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), WebP},
		{"mp4", iso("isom", "iso2", "mp41"), Video},
		{"mov", iso("qt  "), Video},
		{"old mov", []byte("\x00\x00\x00\x08wide\x00\x00\x00\x00mdat"), Video},
		{"m4a", iso("M4A ", "isom"), Audio},
		{"heic", iso("heic", "mif1", "heic"), Photo},
		{"heic by brand", iso("mif1", "heic"), Photo},
		{"avif", iso("avif", "mif1", "miaf"), Unsupported},
		{"avif after heif's brands", iso("mif1", "miaf", "avif"), Unsupported},
		{"cr3", iso("crx ", "isom"), Unsupported},
		{"webm", []byte("\x1A\x45\xDF\xA3\x9f\x42\x86\x81"), Video},
		{"avi", []byte("RIFF\x00\x00\x00\x00AVI LIST"), Unsupported},
		{"wav", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), Audio},
		{"ogg", []byte("OggS\x00\x02"), Audio},
		{"mp3", []byte("ID3\x04\x00"), Audio},
		{"mp3 without id3", []byte("\xFF\xFB\x90\x64"), Audio},
		{"adts aac", []byte("\xFF\xF1\x50\x80"), Audio},
		{"utf-16 text", []byte("\xFF\xFEh\x00i\x00"), Other},
		{"aiff", []byte("FORM\x00\x00\x00\x00AIFF"), Unsupported},
		{"flac", []byte("fLaC\x00"), Audio},
		{"mpeg-ts", ts, Unsupported},
		{"tiff", []byte("II*\x00\x08\x00\x00\x00"), Photo},
		{"big-endian tiff", []byte("MM\x00*\x00\x00\x00\x08"), Photo},
		{"cr2", []byte("II*\x00\x10\x00\x00\x00CR\x02\x00"), Unsupported},
		{"raf", []byte("FUJIFILMCCD-RAW 0201"), Unsupported},
		{"jxl", []byte("\xFF\x0A\xFA"), Unsupported},
		{"jp2", []byte("\x00\x00\x00\x0CjP  \r\n\x87\n"), Photo},
		{"j2k codestream", []byte("\xFF\x4F\xFF\x51"), Photo},
		{"psd", []byte("8BPS\x00\x01"), Photo},
		{"pdf", []byte("%PDF-1.7\n"), Other},
		{"zip", []byte("PK\x03\x04"), Other},
		{"html", []byte("<!doctype html><script>"), Other},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg">`), Other},
		{"empty", nil, Other},
		{"a lone byte", []byte{0xFF}, Other},
	} {
		if got := Sniff(c.head); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
		if name := Name(c.head); (name != "") != (c.want == Unsupported) {
			t.Errorf("%s: named %q", c.name, name)
		}
	}
	if got := Name([]byte("RIFF\x00\x00\x00\x00AVI LIST")); got != "an AVI video" {
		t.Errorf("an AVI is named %q", got)
	}
}

// tiff builds a little-endian TIFF whose first directory has the entries
// given, each a tag and a LONG value.
func tiff(entries ...[2]uint32) []byte {
	b := []byte("II*\x00\x08\x00\x00\x00")
	b = binary.LittleEndian.AppendUint16(b, uint16(len(entries)))
	for _, e := range entries {
		b = binary.LittleEndian.AppendUint16(b, uint16(e[0]))
		b = binary.LittleEndian.AppendUint16(b, 4)
		b = binary.LittleEndian.AppendUint32(b, 1)
		b = binary.LittleEndian.AppendUint32(b, e[1])
	}
	return binary.LittleEndian.AppendUint32(b, 0)
}

func TestTIFFRaw(t *testing.T) {
	for _, c := range []struct {
		name string
		file []byte
		raw  bool
	}{
		{"a photo", tiff([2]uint32{0x0100, 640}, [2]uint32{0x0101, 480}), false},
		{"a dng", tiff([2]uint32{0x0100, 256}, [2]uint32{0xC612, 0x01040000}), true},
		{"raw beneath", tiff([2]uint32{0x014A, 1234}), true},
		{"a preview first", tiff([2]uint32{0x00FE, 1}), true},
		{"a page of many", tiff([2]uint32{0x00FE, 2}), false},
		{"cut short", tiff([2]uint32{0x0100, 640})[:12], true},
	} {
		if got := TIFFRaw(bytes.NewReader(c.file)); got != c.raw {
			t.Errorf("%s: raw %t", c.name, got)
		}
	}
}

func TestStripJPEG(t *testing.T) {
	clean, dirty := dirtyJPEG(t, 64, 48, 6)
	out, res := strip(t, dirty, JPEG)
	assertClean(t, out)
	if !res.Removed || res.Orientation != 6 {
		t.Errorf("removed %t, orientation %d", res.Removed, res.Orientation)
	}
	// A quarter turn: it shows 48 wide and 64 high.
	if res.Width != 48 || res.Height != 64 {
		t.Errorf("size %dx%d, want 48x64", res.Width, res.Height)
	}
	if !bytes.Contains(out, orientationEXIF(6)) {
		t.Error("the orientation wasn't kept")
	}
	for _, kept := range []string{"ICC_PROFILE\x00\x01\x01fake profile", "Adobe\x00\x64", "JFIF\x00"} {
		if !bytes.Contains(out, []byte(kept)) {
			t.Errorf("%q was left out", kept)
		}
	}
	if bytes.Count(out, []byte("\xFF\xD8")) != 1 {
		t.Error("the second image survived")
	}
	a, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := jpeg.Decode(bytes.NewReader(clean))
	samePixels(t, a, b)
	checkVerify(t, out, dirty, JPEG)
}

func TestStripUprightJPEGDropsEXIF(t *testing.T) {
	_, dirty := dirtyJPEG(t, 32, 16, 1)
	out, res := strip(t, dirty, JPEG)
	assertClean(t, out)
	if bytes.Contains(out, []byte("Exif")) || res.Orientation != 1 || res.Width != 32 || res.Height != 16 {
		t.Errorf("orientation %d, size %dx%d", res.Orientation, res.Width, res.Height)
	}
}

func TestStripJPEGScans(t *testing.T) {
	// Pillow made these: one progressive, in ten scans, and one baseline
	// with restart markers every two blocks.
	for _, name := range []string{"progressive.jpg", "restart.jpg"} {
		orig := readFixture(t, name)
		dirty := withSegments(orig, segment(0xE1, phoneEXIF(1)), segment(0xFE, []byte("SecretComment")))
		out, res := strip(t, dirty, JPEG)
		assertClean(t, out)
		if res.Width != 48 || res.Height != 32 {
			t.Errorf("%s: size %dx%d", name, res.Width, res.Height)
		}
		if !bytes.Equal(out, orig) {
			t.Errorf("%s: stripping didn't give back the original", name)
		}
		if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		checkVerify(t, out, dirty, JPEG)
	}
}

func TestDamagedJPEG(t *testing.T) {
	_, dirty := dirtyJPEG(t, 64, 48, 1)
	for name, in := range map[string][]byte{
		"cut short":    dirty[:len(dirty)/3],
		"no start":     dirty[2:],
		"scan first":   append([]byte("\xFF\xD8"), segment(0xDA, []byte{1, 1, 0, 0, 63, 0})...),
		"no image":     []byte("\xFF\xD8\xFF\xD9"),
		"odd marker":   append([]byte("\xFF\xD8"), segment(0xF7, []byte{0})...),
		"short length": []byte("\xFF\xD8\xFF\xE1\x00\x01"),
	} {
		if _, err := Strip(&bytes.Buffer{}, bytes.NewReader(in), JPEG); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", name, err)
		}
	}
}

func pngChunk(typ string, data []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	b = append(b, typ...)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(append([]byte(typ), data...)))
}

// withChunks puts chunks right after a PNG's header chunk.
func withChunks(p []byte, chunks ...[]byte) []byte {
	at := 8 + 8 + 13 + 4
	out := append([]byte{}, p[:at]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, p[at:]...)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestStripPNG(t *testing.T) {
	clean := encodePNG(t, pattern(40, 30))
	dirty := withChunks(clean,
		pngChunk("gAMA", []byte{0, 0, 0xB1, 0x8F}),
		pngChunk("pHYs", []byte{0, 0, 0x0B, 0x13, 0, 0, 0x0B, 0x13, 1}),
		pngChunk("tEXt", []byte("Comment\x00SecretText")),
		pngChunk("zTXt", []byte("Author\x00\x00SecretText")),
		pngChunk("iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00<x:xmpmeta>SecretXMP</x:xmpmeta>")),
		pngChunk("eXIf", phoneEXIF(6)[6:]),
		pngChunk("tIME", []byte{0x07, 0xEA, 9, 28, 12, 0, 0}),
		pngChunk("prVt", []byte("SecretPrivate")),
	)
	dirty = append(dirty, "SecretTrailer"...)
	out, res := strip(t, dirty, PNG)
	assertClean(t, out)
	// An eXIf orientation isn't kept, so the size stays as stored.
	if !res.Removed || res.Orientation != 1 || res.Width != 40 || res.Height != 30 || res.Animated {
		t.Errorf("%+v", res)
	}
	for _, kept := range []string{"gAMA", "pHYs"} {
		if !bytes.Contains(out, []byte(kept)) {
			t.Errorf("%s was left out", kept)
		}
	}
	a, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := png.Decode(bytes.NewReader(clean))
	samePixels(t, a, b)
	checkVerify(t, out, dirty, PNG)
}

func TestStripAnimatedPNG(t *testing.T) {
	clean := encodePNG(t, pattern(8, 8))
	actl := pngChunk("acTL", []byte{0, 0, 0, 2, 0, 0, 0, 0})
	fctl := pngChunk("fcTL", make([]byte, 26))
	in := withChunks(clean, actl, fctl)
	out, res := strip(t, in, PNG)
	if res.Removed || !res.Animated || !bytes.Equal(out, in) {
		t.Errorf("an animated PNG changed: %+v", res)
	}
}

func TestDamagedPNG(t *testing.T) {
	clean := encodePNG(t, pattern(8, 8))
	badCRC := append([]byte{}, clean...)
	badCRC[8+8+13] ^= 0xFF
	for name, in := range map[string][]byte{
		"cut short":         clean[:len(clean)-6],
		"bad checksum":      badCRC,
		"unknown critical":  withChunks(clean, pngChunk("ABCD", []byte("x"))),
		"no header first":   append([]byte("\x89PNG\r\n\x1a\n"), pngChunk("IDAT", nil)...),
		"bad animation hdr": withChunks(clean, pngChunk("acTL", make([]byte, 4))),
	} {
		if _, err := Strip(&bytes.Buffer{}, bytes.NewReader(in), PNG); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", name, err)
		}
	}
}

func encodeGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}}
	a := image.NewPaletted(image.Rect(0, 0, 10, 6), pal)
	b := image.NewPaletted(image.Rect(0, 0, 10, 6), pal)
	for i := range b.Pix {
		b.Pix[i] = 1
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, &gif.GIF{Image: []*image.Paletted{a, b}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestStripGIF(t *testing.T) {
	clean := encodeGIF(t)
	at := 13
	if clean[10]&0x80 != 0 {
		at += 3 << (clean[10]&7 + 1)
	}
	var extra []byte
	extra = append(extra, 0x21, 0xFE, 13)
	extra = append(extra, "SecretComment"...)
	extra = append(extra, 0, 0x21, 0xFF, 11)
	extra = append(extra, "XMP DataXMP"...)
	extra = append(extra, 9)
	extra = append(extra, "SecretXMP"...)
	extra = append(extra, 0, 0x21, 0x01, 12)
	extra = append(extra, make([]byte, 12)...)
	extra = append(extra, 4, 'T', 'e', 'x', 't', 0)
	dirty := append(append(append([]byte{}, clean[:at]...), extra...), clean[at:]...)
	dirty = append(dirty, "SecretTrailer"...)

	out, res := strip(t, dirty, GIF)
	assertClean(t, out)
	if !res.Removed || !res.Animated || res.Width != 10 || res.Height != 6 {
		t.Errorf("%+v", res)
	}
	if !bytes.Equal(out, clean) {
		t.Error("stripping didn't give back the clean GIF")
	}
	if !bytes.Contains(out, []byte("NETSCAPE2.0")) {
		t.Error("the loop extension was left out")
	}
	g, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil || len(g.Image) != 2 {
		t.Fatalf("decode: %v", err)
	}
	checkVerify(t, out, dirty, GIF)
}

// The fixtures are embedded, since the Windows test runner doesn't run
// from the package directory.
//
//go:embed testdata
var fixtures embed.FS

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fixtures.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// riffFile wraps chunks in a WebP's RIFF header.
func riffFile(chunks ...riffChunk) []byte {
	body := []byte("WEBP")
	for _, c := range chunks {
		body = appendChunk(body, c)
	}
	return append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
}

func TestStripWebP(t *testing.T) {
	// ffmpeg's libwebp made these, without metadata.
	still := readFixture(t, "still.webp")
	chunks, err := riffChunks(still[12:])
	if err != nil {
		t.Fatal(err)
	}
	vp8x := []byte{webpEXIF | webpXMP, 0, 0, 0, 31, 0, 0, 23, 0, 0} // 32x24
	dirty := riffFile(riffChunk{"VP8X", vp8x}, chunks[0],
		riffChunk{"EXIF", phoneEXIF(6)[6:]}, riffChunk{"XMP ", []byte("SecretXMP")}, riffChunk{"ZZZZ", []byte("SecretUnknown")})
	dirty = append(dirty, "SecretTrailer"...)
	out, res := strip(t, dirty, WebP)
	assertClean(t, out)
	if !res.Removed || res.Width != 32 || res.Height != 24 || res.Orientation != 1 {
		t.Errorf("%+v", res)
	}
	outChunks, err := riffChunks(out[12:])
	if err != nil || len(outChunks) != 2 || outChunks[0].data[0] != 0 {
		t.Fatalf("chunks %v, %v", outChunks, err)
	}
	a, err := webp.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := webp.Decode(bytes.NewReader(still))
	samePixels(t, a, b)
	checkVerify(t, out, dirty, WebP)

	for _, name := range []string{"still.webp", "alpha.webp", "lossless.webp", "anim.webp"} {
		in := readFixture(t, name)
		out, res := strip(t, in, WebP)
		if res.Removed || !bytes.Equal(out, in) || res.Width != 32 || res.Height != 24 || res.Animated != (name == "anim.webp") {
			t.Errorf("%s: %+v", name, res)
		}
	}
}

func TestStripAnimatedWebP(t *testing.T) {
	anim := readFixture(t, "anim.webp")
	chunks, err := riffChunks(anim[12:])
	if err != nil {
		t.Fatal(err)
	}
	var dirtyChunks []riffChunk
	for i, c := range chunks {
		if c.id == "ANMF" && i == 2 {
			// Something unknown inside the first frame.
			c.data = appendChunk(append([]byte{}, c.data...), riffChunk{"ZZZZ", []byte("SecretUnknown")})
		}
		dirtyChunks = append(dirtyChunks, c)
	}
	dirtyChunks[0].data = append([]byte{dirtyChunks[0].data[0] | webpEXIF}, dirtyChunks[0].data[1:]...)
	dirtyChunks = append(dirtyChunks, riffChunk{"EXIF", phoneEXIF(1)[6:]})
	dirty := riffFile(dirtyChunks...)
	out, res := strip(t, dirty, WebP)
	assertClean(t, out)
	if !res.Removed || !res.Animated || !bytes.Equal(out, anim) {
		t.Errorf("%+v", res)
	}
	checkVerify(t, out, dirty, WebP)
}

func TestOtherFilesPassThrough(t *testing.T) {
	in := []byte("%PDF-1.7 SecretText")
	out, res := strip(t, in, Other)
	if !bytes.Equal(out, in) || res.Removed {
		t.Error("an ordinary file changed")
	}
}

func near(a, b color.Color) bool {
	r1, g1, b1, _ := a.RGBA()
	r2, g2, b2, _ := b.RGBA()
	d := func(x, y uint32) bool { return x>>8 > y>>8+40 || y>>8 > x>>8+40 }
	return !d(r1, r2) && !d(g1, g2) && !d(b1, b2)
}

func TestThumbnailTurnsUpright(t *testing.T) {
	_, dirty := dirtyJPEG(t, 1280, 960, 6)
	out, res := strip(t, dirty, JPEG)
	th, err := Thumbnail(bytes.NewReader(out), JPEG, res)
	if err != nil {
		t.Fatal(err)
	}
	if th.Type != "image/jpeg" || th.Width != 480 || th.Height != 640 {
		t.Fatalf("%s %dx%d", th.Type, th.Width, th.Height)
	}
	img, err := jpeg.Decode(bytes.NewReader(th.Data))
	if err != nil {
		t.Fatal(err)
	}
	// A quarter turn clockwise brings the bottom left (blue) to the top
	// left, and the top left (red) to the top right.
	for _, c := range []struct {
		x, y int
		want color.RGBA
	}{
		{20, 20, color.RGBA{0, 0, 255, 255}},
		{460, 20, color.RGBA{255, 0, 0, 255}},
		{20, 620, color.RGBA{255, 255, 255, 255}},
		{460, 620, color.RGBA{0, 255, 0, 255}},
	} {
		if got := img.At(c.x, c.y); !near(got, c.want) {
			t.Errorf("at %d,%d: %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestThumbnailKinds(t *testing.T) {
	alpha := image.NewNRGBA(image.Rect(0, 0, 900, 300))
	for i := 3; i < len(alpha.Pix); i += 8 {
		alpha.Pix[i-3], alpha.Pix[i] = 200, 255
	}
	for _, c := range []struct {
		name     string
		data     []byte
		kind     Kind
		wantType string
		w, h     int
	}{
		{"opaque png", encodePNG(t, pattern(300, 900)), PNG, "image/jpeg", 213, 640},
		{"transparent png", encodePNG(t, alpha), PNG, "image/png", 640, 213},
		{"small jpeg", encodeJPEG(t, pattern(20, 10)), JPEG, "image/jpeg", 20, 10},
		{"gif", encodeGIF(t), GIF, "image/jpeg", 10, 6},
		{"webp", readFixture(t, "still.webp"), WebP, "image/jpeg", 32, 24},
		{"webp with alpha", readFixture(t, "alpha.webp"), WebP, "image/png", 32, 24},
		{"lossless webp", readFixture(t, "lossless.webp"), WebP, "image/jpeg", 32, 24},
		{"animated webp", readFixture(t, "anim.webp"), WebP, "image/jpeg", 32, 24},
	} {
		out, res := strip(t, c.data, c.kind)
		th, err := Thumbnail(bytes.NewReader(out), c.kind, res)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if th.Type != c.wantType || th.Width != c.w || th.Height != c.h {
			t.Errorf("%s: %s %dx%d, want %s %dx%d", c.name, th.Type, th.Width, th.Height, c.wantType, c.w, c.h)
		}
		if _, _, err := image.Decode(bytes.NewReader(th.Data)); err != nil {
			t.Errorf("%s: the preview doesn't decode: %v", c.name, err)
		}
	}
}

func TestThumbnailBudget(t *testing.T) {
	// A PNG header can claim any size; a small file can decode to
	// gigabytes. The budget goes by the header.
	huge := Result{Width: 40000, Height: 40000, Orientation: 1, pixelBytes: 4}
	if CanPreview(huge) {
		t.Error("a 1.6 gigapixel image fits the budget")
	}
	if _, err := Thumbnail(bytes.NewReader(nil), PNG, huge); !errors.Is(err, ErrTooBig) {
		t.Errorf("%v, want ErrTooBig", err)
	}
	if !CanPreview(Result{Width: 8160, Height: 6120, Orientation: 1, pixelBytes: 3}) {
		t.Error("a 50 megapixel phone photo doesn't fit")
	}
	// A header that lies about the size is caught once decoded.
	img := encodePNG(t, pattern(8, 8))
	_, res := strip(t, img, PNG)
	res.Width = 9
	if _, err := Thumbnail(bytes.NewReader(img), PNG, res); !errors.Is(err, ErrMalformed) {
		t.Errorf("%v, want ErrMalformed", err)
	}
}

func TestPlayType(t *testing.T) {
	for name, want := range map[string]string{
		"meta.mp4": "video/mp4", "with-gps.mov": "video/mp4", "meta.m4a": "audio/mp4", "meta.mkv": "video/x-matroska",
		"meta.webm": "video/webm", "meta.mp3": "audio/mpeg", "meta.flac": "audio/flac", "meta.ogg": "audio/ogg",
		"meta.wav": "audio/wav",
	} {
		head, err := os.ReadFile(filepath.Join("ffmpeg", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := PlayType(head[:min(len(head), SniffLen)]); got != want {
			t.Errorf("%s plays as %q, want %q", name, got, want)
		}
	}
	if got := PlayType([]byte("\xFF\xD8\xFF\xE0")); got != "" {
		t.Errorf("a JPEG plays as %q", got)
	}
}
