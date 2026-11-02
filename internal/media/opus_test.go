package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"

	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

// soundCase is testdata/ogg.json: Opus packets, and the file the page
// writes of them as a sound (M14.3), which the page's tests check it
// writes too.
type soundCase struct {
	Channels int      `json:"channels"`
	PreSkip  int      `json:"pre_skip"`
	Samples  int      `json:"samples"`
	Packets  [][]byte `json:"packets"`
	Ogg      []byte   `json:"ogg"`
}

func readSoundCase(t *testing.T) soundCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ogg.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c soundCase
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOpusSamples(t *testing.T) {
	for _, c := range []struct {
		packet []byte
		want   int
	}{
		{[]byte{0xfc, 1, 2}, 960},        // CELT, 20 ms, one frame
		{[]byte{0xf4}, 480},              // CELT, 10 ms, the table alone
		{[]byte{0xf9, 1, 1}, 1920},       // two frames of 20 ms
		{[]byte{0xff, 0x03, 1, 2}, 2880}, // three frames of 20 ms
		{[]byte{0x18}, 2880},             // SILK, 60 ms
		{[]byte{0x60}, 480},              // hybrid, 10 ms
		{[]byte{0x80}, 120},              // CELT, 2.5 ms
		{[]byte{0x1b, 0x02}, 5760},       // two frames of 60 ms, the most a packet holds
		{[]byte{0x1b, 0x03}, 0},          // three, which is more
		{[]byte{0xff, 0x00}, 0},          // no frames
		{[]byte{0xff}, 0},                // a count that isn't there
		{nil, 0},
	} {
		if got := OpusSamples(c.packet); got != c.want {
			t.Errorf("% x holds %d samples, not %d", c.packet, got, c.want)
		}
	}
}

// The page's file reads back through Pion's reader as one page a packet,
// with granule positions counting its samples and the last trimming its
// padding, and through the den's as its packets.
func TestThePagesSoundFile(t *testing.T) {
	c := readSoundCase(t)
	r, head, err := oggreader.NewWith(bytes.NewReader(c.Ogg))
	if err != nil {
		t.Fatal(err)
	}
	if int(head.Channels) != c.Channels || int(head.PreSkip) != c.PreSkip || head.SampleRate != OpusRate {
		t.Fatalf("its head is %+v", head)
	}
	tags, _, err := r.ParseNextPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oggreader.ParseOpusTags(tags); err != nil {
		t.Fatal(err)
	}
	granule := 0
	for i, want := range c.Packets {
		got, page, err := r.ParseNextPage()
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("packet %d reads back as % x", i, got[:min(len(got), 8)])
		}
		granule += OpusSamples(want)
		if i == len(c.Packets)-1 {
			granule = c.PreSkip + c.Samples
		}
		if int(page.GranulePosition) != granule {
			t.Errorf("packet %d's page ends at %d, not %d", i, page.GranulePosition, granule)
		}
	}
	if _, _, err := r.ParseNextPage(); !errors.Is(err, io.EOF) {
		t.Errorf("after the packets: %v", err)
	}

	o, err := NewOggOpus(bytes.NewReader(c.Ogg))
	if err != nil {
		t.Fatal(err)
	}
	if o.Channels != c.Channels || o.PreSkip != c.PreSkip {
		t.Fatalf("the den reads its head as %d channels, %d pre-skip", o.Channels, o.PreSkip)
	}
	got := readPackets(t, o)
	if !slices.EqualFunc(got, c.Packets, bytes.Equal) {
		t.Fatalf("the den reads %d packets, not the %d written", len(got), len(c.Packets))
	}
}

func readPackets(t *testing.T, o *OggOpus) [][]byte {
	t.Helper()
	var out [][]byte
	for {
		p, _, err := o.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
}

// The media module writes a sound's pages anew as it strips it, a second's
// packets in a page, and the den reads the same packets back from them.
func TestTheModuleKeepsASoundsPackets(t *testing.T) {
	c := readSoundCase(t)
	// Ten seconds of the packets, so the module's pages hold many.
	var many [][]byte
	for len(many) < 500 {
		many = append(many, c.Packets...)
	}
	var in ffmpeg.Buffer
	if _, err := in.WriteAt(oggOpusFile(c.Channels, c.PreSkip, 0, many), 0); err != nil {
		t.Fatal(err)
	}
	var out ffmpeg.Buffer
	s, err := StripMedia(context.Background(), ffmpeg.TestRunner(nil), &in, &out)
	if err != nil {
		t.Fatal(err)
	}
	if s.MIME != "audio/ogg" {
		t.Fatalf("the copy is %s", s.MIME)
	}
	stripped := out.Bytes()
	if pages, want := bytes.Count(stripped, []byte("OggS")), len(many)/10; pages > want {
		t.Errorf("the module wrote %d pages for %d packets, where it puts a second in each", pages, len(many))
	}
	o, err := NewOggOpus(bytes.NewReader(stripped))
	if err != nil {
		t.Fatal(err)
	}
	got := readPackets(t, o)
	if !slices.EqualFunc(got, many, bytes.Equal) {
		t.Fatalf("the den reads %d packets from the module's copy, not the %d written", len(got), len(many))
	}
	samples := 0
	for _, p := range many {
		samples += OpusSamples(p)
	}
	o, _ = NewOggOpus(bytes.NewReader(stripped))
	if n, last, err := o.Length(); err != nil || n != samples-c.PreSkip || last != OpusSamples(many[len(many)-1]) {
		t.Errorf("the copy plays %d samples, its last %d, %v; want %d", n, last, err, samples-c.PreSkip)
	}
}

// oggOpusFile writes packets as the page does, without trimming its end
// when samples is 0.
func oggOpusFile(channels, preSkip, samples int, packets [][]byte) []byte {
	head := []byte("OpusHead\x01")
	head = append(head, byte(channels))
	head = binary.LittleEndian.AppendUint16(head, uint16(preSkip))
	head = binary.LittleEndian.AppendUint32(head, OpusRate)
	head = append(head, 0, 0, 0)
	tags := binary.LittleEndian.AppendUint32([]byte("OpusTags"), 4)
	tags = binary.LittleEndian.AppendUint32(append(tags, "Dens"...), 0)
	out := append(oggPage(head, oggFirst, 0, 0), oggPage(tags, 0, 0, 1)...)
	granule := 0
	for i, p := range packets {
		granule += OpusSamples(p)
		flags := byte(0)
		if i == len(packets)-1 {
			flags = oggLast
			if samples > 0 {
				granule = preSkip + samples
			}
		}
		out = append(out, oggPage(p, flags, granule, uint32(i+2))...)
	}
	return out
}

func oggPage(packet []byte, flags byte, granule int, number uint32) []byte {
	var lacing []byte
	for n := len(packet); ; n -= 255 {
		lacing = append(lacing, byte(min(n, 255)))
		if n < 255 {
			break
		}
	}
	p := []byte("OggS\x00")
	p = append(p, flags)
	p = binary.LittleEndian.AppendUint64(p, uint64(granule))
	p = binary.LittleEndian.AppendUint32(p, 0x736e6544)
	p = binary.LittleEndian.AppendUint32(p, number)
	p = append(p, 0, 0, 0, 0, byte(len(lacing)))
	p = append(append(p, lacing...), packet...)
	binary.LittleEndian.PutUint32(p[22:], oggCRC(0, p))
	return p
}

// pagesOf splits an Ogg file into its pages.
func pagesOf(file []byte) [][]byte {
	var pages [][]byte
	for len(file) >= 27 {
		n := 27 + int(file[26])
		for _, l := range file[27:n] {
			n += int(l)
		}
		pages, file = append(pages, file[:n]), file[n:]
	}
	return pages
}

// sealed gives a page its checksum again, once a test changed it.
func sealed(p []byte) []byte {
	p = slices.Clone(p)
	clear(p[22:26])
	binary.LittleEndian.PutUint32(p[22:], oggCRC(0, p))
	return p
}

func TestOggOpusRefuses(t *testing.T) {
	c := readSoundCase(t)
	pages := pagesOf(c.Ogg)
	if !bytes.Equal(slices.Concat(pages...), c.Ogg) || len(pages) != len(c.Packets)+2 {
		t.Fatalf("the file splits into %d pages", len(pages))
	}
	with := func(change func(pages [][]byte) [][]byte) []byte {
		copied := make([][]byte, len(pages))
		for i, p := range pages {
			copied[i] = slices.Clone(p)
		}
		return slices.Concat(change(copied)...)
	}
	cases := map[string][]byte{
		"a page whose checksum is off": with(func(p [][]byte) [][]byte { p[3][30] ^= 1; return p }),
		"a second stream": with(func(p [][]byte) [][]byte {
			p[4][14]++
			p[4] = sealed(p[4])
			return p
		}),
		"pages out of order": with(func(p [][]byte) [][]byte { p[3], p[4] = p[4], p[3]; return p }),
		"no last page":       with(func(p [][]byte) [][]byte { return p[:len(p)-1] }),
		"a page cut off":     with(func(p [][]byte) [][]byte { p[len(p)-1] = p[len(p)-1][:40]; return p }),
		"six channels": with(func(p [][]byte) [][]byte {
			p[0][27+1+9] = 6
			p[0] = sealed(p[0])
			return p
		}),
		"no head": with(func(p [][]byte) [][]byte {
			p[0][27+1] = 'X'
			p[0] = sealed(p[0])
			return p
		}),
		"a packet that isn't Opus": with(func(p [][]byte) [][]byte {
			p[2] = oggPage([]byte{0xff, 0x00, 1}, 0, 960, 2)
			return p
		}),
		"a second first page": with(func(p [][]byte) [][]byte {
			p[2][5] |= oggFirst
			p[2] = sealed(p[2])
			return p
		}),
		"a packet too large": oggOpusFile(2, 312, 0, [][]byte{append([]byte{0xfc}, make([]byte, maxOpusPacket)...)}),
		"empty":              {},
	}
	for name, file := range cases {
		o, err := NewOggOpus(bytes.NewReader(file))
		if err == nil {
			_, _, err = o.Length()
		}
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", name, err)
		}
	}
}
