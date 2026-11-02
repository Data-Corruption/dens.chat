package service_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/notify"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/sounds"
)

// wav is a short WAV file of silence, as a member's own sound might be.
func wav(samples int) []byte {
	var b bytes.Buffer
	data := uint32(samples * 2)
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, 36+data)
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, data)
	b.Write(make([]byte, data))
	return b.Bytes()
}

// send makes a request with a raw body, as the page uploads a file, from
// the page's origin unless origin is false, and returns its answer.
func (b *browser) send(method, path, contentType string, body []byte, origin bool) (int, http.Header, []byte) {
	b.t.Helper()
	req, _ := http.NewRequest(method, b.base+path, bytes.NewReader(body))
	if origin {
		req.Header.Set("Origin", b.base)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, data
}

// lastStatus waits for the page to hear the notifications' status that
// holds, skipping older ones.
func (p *pageStream) lastStatus(holds func(notify.Status) bool) notify.Status {
	p.t.Helper()
	for {
		var st notify.Status
		if err := json.Unmarshal(p.next("notifications"), &st); err != nil {
			p.t.Fatal(err)
		}
		if holds(st) {
			return st
		}
	}
}

// A member's own sounds (M14.2): a file of audio, of at most 1 MiB, in
// place of one of the page's sounds, kept by the service and served to the
// page alone, sandboxed and never cached; the sounds' switches and volumes
// kept with the notifications' settings; Reset bringing the default back;
// and every page hearing of each change. Writes pass the Origin check.
func TestOwnSounds(t *testing.T) {
	a, client := startDevService(t)
	b := pair(t, a, client)
	page := b.stream()
	page.next("notifications")
	file := wav(4000)

	if code, _, body := b.send("GET", "/api/sounds/joined", "", nil, false); code != http.StatusNotFound {
		t.Fatalf("a sound never replaced: %d %s", code, body)
	}
	for _, c := range []struct {
		what        string
		path, ctype string
		body        []byte
		origin      bool
		code        int
	}{
		{"from another page", "/api/sounds/joined", "application/octet-stream", file, false, http.StatusForbidden},
		{"as a form would send it", "/api/sounds/joined", "text/plain", file, true, http.StatusUnsupportedMediaType},
		{"for a sound the page hasn't", "/api/sounds/bell", "application/octet-stream", file, true, http.StatusNotFound},
		{"of a picture", "/api/sounds/joined", "application/octet-stream", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), true, http.StatusUnsupportedMediaType},
		{"of nothing", "/api/sounds/joined", "application/octet-stream", nil, true, http.StatusUnsupportedMediaType},
		{"past 1 MiB", "/api/sounds/joined", "application/octet-stream", append(wav(4000), make([]byte, sounds.MaxSize)...), true, http.StatusRequestEntityTooLarge},
	} {
		if code, _, body := b.send("POST", c.path, c.ctype, c.body, c.origin); code != c.code {
			t.Errorf("a sound %s: %d %s, want %d", c.what, code, body, c.code)
		}
	}
	stranger := &browser{t: t, base: b.base, http: &http.Client{}}
	if code, _, body := stranger.send("POST", "/api/sounds/joined", "application/octet-stream", file, true); code != http.StatusUnauthorized {
		t.Fatalf("a browser not paired replaced a sound: %d %s", code, body)
	}

	code, _, body := b.send("POST", "/api/sounds/joined", "application/octet-stream", file, true)
	var st notify.Status
	if code != http.StatusOK || json.Unmarshal(body, &st) != nil || st.Own["joined"] == 0 || len(st.Own) != 1 {
		t.Fatalf("replacing a sound: %d %s", code, body)
	}
	if heard := page.lastStatus(func(s notify.Status) bool { return s.Own["joined"] != 0 }); heard.Own["joined"] != st.Own["joined"] {
		t.Fatalf("the page heard version %d, the answer said %d", heard.Own["joined"], st.Own["joined"])
	}
	code, header, got := b.send("GET", "/api/sounds/joined", "", nil, false)
	if code != http.StatusOK || !bytes.Equal(got, file) {
		t.Fatalf("the own sound: %d, %d bytes of %d", code, len(got), len(file))
	}
	if header.Get("Content-Type") != "audio/wav" || header.Get("Cache-Control") != "no-store" ||
		!strings.Contains(header.Get("Content-Security-Policy"), "sandbox") || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("the own sound's headers: %v", header)
	}
	if kept, _, err := sounds.Open(t.Context(), a.DB, a.Vault, "joined"); err != nil || !bytes.Equal(kept, file) {
		t.Fatalf("the database keeps %d bytes, %v", len(kept), err)
	}

	b.expect(http.StatusOK, "turn a sound off, and set another's volume", "POST", "/api/settings",
		map[string]any{"sounds": map[string]any{"joined": map[string]bool{"off": true}, "micOn": map[string]int{"volume": 40}}})
	heard := page.lastStatus(func(s notify.Status) bool { return s.Sounds["joined"].Off })
	if heard.Sounds["micOn"].Volume == nil || *heard.Sounds["micOn"].Volume != 40 || heard.Own["joined"] == 0 {
		t.Fatalf("the page heard %+v", heard)
	}
	if cfg, err := config.View(a.DB); err != nil || !cfg.Notifications.Sounds["joined"].Off || *cfg.Notifications.Sounds["micOn"].Volume != 40 {
		t.Fatalf("the settings keep %+v, %v", cfg.Notifications.Sounds, err)
	}
	for what, sound := range map[string]any{
		"a sound the page hasn't": map[string]any{"bell": map[string]bool{"off": true}},
		"a volume past 100":       map[string]any{"left": map[string]int{"volume": 101}},
	} {
		if code, body := b.do("POST", "/api/settings", map[string]any{"sounds": sound}); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", what, code, body)
		}
	}

	if code, _, body := b.send("DELETE", "/api/sounds/joined", "", nil, false); code != http.StatusForbidden {
		t.Fatalf("Reset from another page: %d %s", code, body)
	}
	code, _, body = b.send("DELETE", "/api/sounds/joined", "", nil, true)
	var reset notify.Status
	if code != http.StatusOK || json.Unmarshal(body, &reset) != nil || len(reset.Own) != 0 {
		t.Fatalf("Reset: %d %s", code, body)
	}
	page.lastStatus(func(s notify.Status) bool { return len(s.Own) == 0 })
	if code, _, _ := b.send("GET", "/api/sounds/joined", "", nil, false); code != http.StatusNotFound {
		t.Fatalf("after Reset the own sound answers %d", code)
	}
	if _, _, err := sounds.Open(t.Context(), a.DB, a.Vault, "joined"); !errors.Is(err, sounds.ErrNone) {
		t.Fatalf("after Reset the database opens it with %v", err)
	}

	// With every kind off, a DM makes no sound (M14's manual pass); with
	// DMs on, and nothing to show its notification, as the browser won't
	// let the page notify and no helper runs, its sound plays alone, on the
	// page on screen.
	page.send("page", map[string]bool{"visible": true, "allowed": false})
	page.send("focus", map[string]string{"den": "d1", "channel": "5"})
	// The service reads the page's lines in their own time.
	time.Sleep(200 * time.Millisecond)
	a.Notify.Message(notify.Message{Den: "d1", DenName: "Den", Channel: "7", DM: true, ID: "9", Author: "Alice", Text: "hi"})
	b.expect(http.StatusOK, "turn DMs on", "POST", "/api/settings", map[string]any{"notifications": map[string]bool{"dms": true}})
	a.Notify.Message(notify.Message{Den: "d1", DenName: "Den", Channel: "7", DM: true, ID: "10", Author: "Alice", Text: "hi"})
	if got := string(page.next("sound")); got != `{"den":"d1","channel":"7","message":"10"}` {
		t.Fatalf("the page was sent %s", got)
	}
}
