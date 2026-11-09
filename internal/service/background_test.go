package service_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/platform/database/background"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
)

// pictures are a small PNG, JPEG and WebP, each with something a picture
// doesn't need to show, which names "Secret".
func pictures(t *testing.T) map[string][]byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	img.Set(0, 0, color.RGBA{200, 30, 30, 255})
	var p, j bytes.Buffer
	if err := png.Encode(&p, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	// A text chunk after the PNG's header chunk, and a comment after the
	// JPEG's start.
	at := 8 + 8 + 13 + 4
	withText := append(append(append([]byte{}, p.Bytes()[:at]...), pngChunk("tEXt", []byte("Comment\x00SecretText"))...), p.Bytes()[at:]...)
	comment := append(binary.BigEndian.AppendUint16([]byte{0xFF, 0xFE}, 2+10), "SecretText"...)
	withComment := append(append(append([]byte{}, j.Bytes()[:2]...), comment...), j.Bytes()[2:]...)
	// The smallest lossless WebP, a pixel, with bytes after its end.
	webp := []byte("RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00\x2f\x00\x00\x00\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00SecretTrailer")
	return map[string][]byte{"image/png": withText, "image/jpeg": withComment, "image/webp": webp}
}

func pngChunk(typ string, data []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	b = append(b, typ...)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(append([]byte(typ), data...)))
}

// backgroundOf is the chat's background as an answer or the dens view
// names it.
type backgroundOf struct {
	Background *struct {
		Version int64 `json:"version"`
		Dim     int   `json:"dim"`
	} `json:"background"`
}

// lastDens waits for the page to hear the dens view that holds, skipping
// older ones.
func (p *pageStream) lastDens(holds func(backgroundOf) bool) backgroundOf {
	p.t.Helper()
	for {
		var v backgroundOf
		if err := json.Unmarshal(p.next("dens"), &v); err != nil {
			p.t.Fatal(err)
		}
		if holds(v) {
			return v
		}
	}
}

// The picture behind the chat: a still PNG, JPEG or WebP of at most 10 MB,
// stripped, kept by the service and served to the page alone, sandboxed
// and never cached; its dim kept with the settings; Remove dropping it;
// and every page hearing of each change with the dens view. Writes pass
// the Origin check.
func TestChatBackground(t *testing.T) {
	a, client := startDevService(t)
	b := pair(t, a, client)
	page := b.stream()
	if first := page.lastDens(func(backgroundOf) bool { return true }); first.Background != nil {
		t.Fatalf("a chat with no background has %+v", first.Background)
	}
	if code, _, body := b.send("GET", "/api/background", "", nil, false); code != http.StatusNotFound {
		t.Fatalf("a background never set: %d %s", code, body)
	}
	pics := pictures(t)
	picture := pics["image/png"]
	animated := append(append(append([]byte{}, picture[:33]...), pngChunk("acTL", []byte{0, 0, 0, 2, 0, 0, 0, 0})...), picture[33:]...)
	for _, c := range []struct {
		what   string
		ctype  string
		body   []byte
		origin bool
		code   int
	}{
		{"from another page", "application/octet-stream", picture, false, http.StatusForbidden},
		{"as a form would send it", "text/plain", picture, true, http.StatusUnsupportedMediaType},
		{"of a GIF", "application/octet-stream", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), true, http.StatusUnsupportedMediaType},
		{"of text", "application/octet-stream", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), true, http.StatusUnsupportedMediaType},
		{"of nothing", "application/octet-stream", nil, true, http.StatusUnsupportedMediaType},
		{"of a PNG cut short", "application/octet-stream", picture[:len(picture)/2], true, http.StatusUnsupportedMediaType},
		{"of an animated PNG", "application/octet-stream", animated, true, http.StatusUnsupportedMediaType},
		{"past 10 MB", "application/octet-stream", append(append([]byte{}, picture...), make([]byte, background.MaxSize)...), true, http.StatusRequestEntityTooLarge},
	} {
		if code, _, body := b.send("POST", "/api/background", c.ctype, c.body, c.origin); code != c.code {
			t.Errorf("a background %s: %d %s, want %d", c.what, code, body, c.code)
		}
	}
	stranger := &browser{t: t, base: b.base, http: &http.Client{}}
	if code, _, body := stranger.send("POST", "/api/background", "application/octet-stream", picture, true); code != http.StatusUnauthorized {
		t.Fatalf("a browser not paired set a background: %d %s", code, body)
	}
	var fetched backgroundOf
	if b.expect(http.StatusOK, "the dens view", "GET", "/api/dens", nil, &fetched); fetched.Background != nil {
		t.Fatalf("after the refusals the dens view has %+v", fetched.Background)
	}

	var last int64
	for _, ctype := range []string{"image/png", "image/jpeg", "image/webp"} {
		file := pics[ctype]
		code, _, body := b.send("POST", "/api/background", "application/octet-stream", file, true)
		var set backgroundOf
		if code != http.StatusOK || json.Unmarshal(body, &set) != nil || set.Background == nil || set.Background.Version <= last || set.Background.Dim != 70 {
			t.Fatalf("setting a %s: %d %s", ctype, code, body)
		}
		last = set.Background.Version
		if heard := page.lastDens(func(v backgroundOf) bool { return v.Background != nil && v.Background.Version == last }); heard.Background.Dim != 70 {
			t.Fatalf("the page heard %+v", heard.Background)
		}
		code, header, got := b.send("GET", "/api/background", "", nil, false)
		if code != http.StatusOK || len(got) == 0 || len(got) >= len(file) || bytes.Contains(got, []byte("Secret")) {
			t.Fatalf("the %s as kept: %d, %d bytes of %d", ctype, code, len(got), len(file))
		}
		if header.Get("Content-Type") != ctype || header.Get("Cache-Control") != "no-store" ||
			!strings.Contains(header.Get("Content-Security-Policy"), "sandbox") || header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("the %s's headers: %v", ctype, header)
		}
		if kept, err := background.Open(t.Context(), a.DB, a.Vault); err != nil || !bytes.Equal(kept, got) {
			t.Fatalf("the database keeps %d bytes, %v", len(kept), err)
		}
	}

	b.expect(http.StatusOK, "set the dim", "POST", "/api/settings", map[string]int{"backgroundDim": 80})
	if heard := page.lastDens(func(v backgroundOf) bool { return v.Background != nil && v.Background.Dim == 80 }); heard.Background.Version != last {
		t.Fatalf("the page heard %+v", heard.Background)
	}
	if cfg, err := config.View(a.DB); err != nil || cfg.BackgroundDim == nil || *cfg.BackgroundDim != 80 {
		t.Fatalf("the settings keep %+v, %v", cfg.BackgroundDim, err)
	}
	for _, dim := range []int{-1, 91} {
		if code, body := b.do("POST", "/api/settings", map[string]int{"backgroundDim": dim}); code != http.StatusBadRequest {
			t.Errorf("a dim of %d: %d %s", dim, code, body)
		}
	}

	if code, _, body := b.send("DELETE", "/api/background", "", nil, false); code != http.StatusForbidden {
		t.Fatalf("Remove from another page: %d %s", code, body)
	}
	code, _, body := b.send("DELETE", "/api/background", "", nil, true)
	var removed backgroundOf
	if code != http.StatusOK || json.Unmarshal(body, &removed) != nil || removed.Background != nil {
		t.Fatalf("Remove: %d %s", code, body)
	}
	page.lastDens(func(v backgroundOf) bool { return v.Background == nil })
	if code, _, _ := b.send("GET", "/api/background", "", nil, false); code != http.StatusNotFound {
		t.Fatalf("after Remove the background answers %d", code)
	}
	if _, err := background.Open(t.Context(), a.DB, a.Vault); !errors.Is(err, background.ErrNone) {
		t.Fatalf("after Remove the database opens it with %v", err)
	}
	// The dim outlasts the picture, for the next one.
	if code, _, body := b.send("POST", "/api/background", "application/octet-stream", picture, true); code != http.StatusOK ||
		json.Unmarshal(body, &removed) != nil || removed.Background == nil || removed.Background.Dim != 80 {
		t.Fatalf("a background set after Remove: %d %s", code, body)
	}
}
