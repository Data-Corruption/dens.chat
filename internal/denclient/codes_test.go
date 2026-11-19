package denclient

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"rsc.io/qr/coding"
)

var updateCodes = flag.Bool("update", false, "rewrite testdata/qrcodes.json, the codes the page's reader is tested on")

// TestQRCode checks a code's rows are the encoder's modules, for every
// sign-in code that reads.
func TestQRCode(t *testing.T) {
	data, err := os.ReadFile("../denproto/testdata/signincodes.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []struct {
			Text string `json:"text"`
			Kind string `json:"kind"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	for _, c := range file.Cases {
		if c.Kind == "" {
			continue
		}
		code, err := QRCode(c.Text)
		if err != nil {
			t.Fatalf("QRCode(%q): %v", c.Text, err)
		}
		want, _, mask, err := encode(c.Text)
		if err != nil {
			t.Fatal(err)
		}
		for m := coding.Mask(0); m < 8; m++ {
			if other := withMask(t, c.Text, m); penalty(other) < penalty(want) {
				t.Errorf("%q takes mask %d, which scores %d, over mask %d, %d", c.Text, mask, penalty(want), m, penalty(other))
			}
		}
		if code.Size != want.Size || len(code.Rows) != want.Size {
			t.Fatalf("QRCode(%q) is %d by %d rows; want %d", c.Text, code.Size, len(code.Rows), want.Size)
		}
		for y, row := range code.Rows {
			bits, err := hex.DecodeString(row)
			if err != nil || len(bits) != (code.Size+7)/8 {
				t.Fatalf("row %d of %q: %q", y, c.Text, row)
			}
			for x := 0; x < code.Size; x++ {
				if dark := bits[x/8]&(0x80>>(x%8)) != 0; dark != want.Black(x, y) {
					t.Fatalf("module %d,%d of %q", x, y, c.Text)
				}
			}
		}
	}
}

// withMask makes text's code as encode does, with mask m.
func withMask(t *testing.T, text string, m coding.Mask) *coding.Code {
	t.Helper()
	_, v, _, _ := encode(text)
	var enc coding.Encoding = coding.String(text)
	if coding.Num(text).Check() == nil {
		enc = coding.Num(text)
	} else if coding.Alpha(text).Check() == nil {
		enc = coding.Alpha(text)
	}
	p, err := coding.NewPlan(v, coding.M, m)
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Encode(enc)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestCodesFit checks the largest code Dens makes holds the longest
// address the comment on MaxCodeVersion promises, with the longest
// username, and refuses one longer.
func TestCodesFit(t *testing.T) {
	address := func(n int) string {
		host := strings.Repeat("a", n-len("https://")-len(".example.com"))
		for i := 60; i < len(host); i += 61 {
			host = host[:i] + "." + host[i+1:]
		}
		return "https://" + host + ".example.com"
	}
	user := strings.Repeat("u", 32)
	for _, tc := range []struct {
		n    int
		fits bool
	}{{168, true}, {169, false}} {
		text, err := denproto.DeviceCode(address(tc.n), user)
		if err != nil || len(address(tc.n)) != tc.n {
			t.Fatalf("the code for an address of %d: %v", tc.n, err)
		}
		if _, err := QRCode(text); (err == nil) != tc.fits {
			t.Errorf("an address of %d characters: %v", tc.n, err)
		}
	}
}

// pageCode is a code the page's reader is tested on, with what made it.
type pageCode struct {
	Text    string   `json:"text"`
	Version int      `json:"version"`
	Level   string   `json:"level"`
	Mask    int      `json:"mask"`
	Size    int      `json:"size"`
	Rows    []string `json:"rows"`
}

// pageCodes makes the codes the page's reader is tested on: sign-in codes
// as QRCode makes them, then each mask, each level, each version it
// reads, and each kind of segment.
func pageCodes(t *testing.T) []pageCode {
	device := "dens-device:https://den.example.com bob"
	type plan struct {
		v    coding.Version
		l    coding.Level
		m    coding.Mask
		segs []coding.Encoding
	}
	var plans []plan
	natural := func(text string, l coding.Level, m coding.Mask) plan {
		enc := coding.Encoding(coding.String(text))
		if coding.Num(text).Check() == nil {
			enc = coding.Num(text)
		}
		v := coding.Version(2)
		for enc.Bits(v) > v.DataBytes(l)*8 {
			v++
		}
		return plan{v, l, m, []coding.Encoding{enc}}
	}
	// As the service makes them, each with the mask encode picks.
	for _, text := range []string{device, "dens-device:http://127.0.0.1:18485 alice_2", "1234567890123456"} {
		_, v, m, err := encode(text)
		if err != nil {
			t.Fatal(err)
		}
		p := natural(text, coding.M, m)
		if p.v != v {
			t.Fatalf("%q: version %d, encode's %d", text, p.v, v)
		}
		plans = append(plans, p)
	}
	for m := coding.Mask(1); m < 8; m++ {
		plans = append(plans, plan{3, coding.M, m, []coding.Encoding{coding.String(device)}})
	}
	for _, l := range []coding.Level{coding.L, coding.Q, coding.H} {
		plans = append(plans, natural(device, l, coding.Mask(l)+4))
	}
	// Each version filled at a level that varies, which takes in each way
	// blocks are laid out up to version 10, and version information from 7.
	filler := device + " "
	for v := coding.Version(1); v <= MaxCodeVersion; v++ {
		l := coding.Level(int(v) % 4)
		n := v.DataBytes(l) - 3
		text := strings.Repeat(filler, n/len(filler)+1)[:n]
		plans = append(plans, plan{v, l, coding.Mask(int(v) % 8), []coding.Encoding{coding.String(text)}})
	}
	plans = append(plans,
		plan{4, coding.Q, 5, []coding.Encoding{coding.Num("1234567890"), coding.Alpha("DENS $%*+-./:"), coding.String("héllo ✓")}},
		plan{1, coding.L, 2, []coding.Encoding{coding.Num("1234567")}},
		plan{1, coding.L, 6, []coding.Encoding{coding.Num("12345678")}},
		plan{2, coding.H, 3, []coding.Encoding{coding.Alpha("ABC")}},
	)
	levels := "LMQH"
	var out []pageCode
	for _, p := range plans {
		pl, err := coding.NewPlan(p.v, p.l, p.m)
		if err != nil {
			t.Fatal(err)
		}
		c, err := pl.Encode(p.segs...)
		if err != nil {
			t.Fatalf("version %d level %d: %v", p.v, p.l, err)
		}
		var text strings.Builder
		for _, s := range p.segs {
			switch s := s.(type) {
			case coding.Num:
				text.WriteString(string(s))
			case coding.Alpha:
				text.WriteString(string(s))
			case coding.String:
				text.WriteString(string(s))
			}
		}
		width := (c.Size + 7) / 8
		rows := make([]string, c.Size)
		for y := range rows {
			rows[y] = hex.EncodeToString(c.Bitmap[y*c.Stride : y*c.Stride+width])
		}
		out = append(out, pageCode{Text: text.String(), Version: int(p.v), Level: levels[p.l : p.l+1], Mask: int(p.m), Size: c.Size, Rows: rows})
	}
	return out
}

// TestPageCodes checks testdata/qrcodes.json holds the codes pageCodes
// makes; -update writes them.
func TestPageCodes(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("{\n  \"about\": \"QR codes made by rsc.io/qr, as the service makes them (M22.6): sign-in codes, then each mask, each error correction level, each version up to 10 and each kind of segment, each with what made it and its modules, a row of hex each, the first module the top bit. denclient's TestPageCodes writes this file (go test ./internal/denclient -run TestPageCodes -args -update), and the page's reader is tested on it.\",\n  \"codes\": [\n")
	codes := pageCodes(t)
	for i, c := range codes {
		line, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("    ")
		b.Write(line)
		if i < len(codes)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]\n}\n")
	const path = "testdata/qrcodes.json"
	if *updateCodes {
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	have, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(have, b.Bytes()) {
		t.Fatalf("%s isn't what pageCodes makes (%v): go test ./internal/denclient -run TestPageCodes -args -update", path, err)
	}
}
