package media

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Opus in Ogg (M14.3): the den's sounds. The page of the staff member
// adding one encodes it as Opus and writes it in Ogg, and the den strips it
// with the media module like any audio, which writes Ogg's pages anew, up
// to about a second of packets in each. The den sends a sound's packets as
// its file holds them, on the call's track, so it reads them back by the
// lacing values that end each one, which Pion's Ogg reader keeps to itself
// as it hands out whole pages.

// OpusRate is the rate Opus counts samples at, whatever it was made from.
const OpusRate = 48000

const (
	// maxOpusPacket bounds a packet: Opus at a sound's bitrate makes a few
	// hundred bytes in 20 ms, and at most 120 ms goes in one.
	maxOpusPacket = 4 << 10
	// maxOpusSamples is the most a packet may hold, 120 ms (RFC 6716, 3.2).
	maxOpusSamples = 120 * OpusRate / 1000
	// The flags of an Ogg page's header type.
	oggContinued = 0x01
	oggFirst     = 0x02
	oggLast      = 0x04
)

// OggOpus reads the Opus packets of an Ogg file in order, each with how
// long it plays. Anything that isn't one stream of Opus, in mono or stereo,
// in pages that check out, is malformed.
type OggOpus struct {
	r *bufio.Reader
	// Channels and PreSkip are from the stream's head: its channels, and
	// the samples a decoder drops at its start (RFC 7845, 5.1).
	Channels int
	PreSkip  int
	serial   uint32
	page     uint32 // the next page's number
	packets  [][]byte
	partial  []byte // a packet that goes on in the next page
	last     bool   // the stream's last page was read
}

// NewOggOpus starts reading an Ogg file of Opus, through its head and tags.
func NewOggOpus(r io.Reader) (*OggOpus, error) {
	o := &OggOpus{r: bufio.NewReader(r)}
	head, err := o.packet()
	if err != nil {
		return nil, err
	}
	// The head has a page of its own, the stream's first (RFC 7845, 3).
	if len(o.packets) > 0 || o.partial != nil || len(head) != 19 || !bytes.HasPrefix(head, []byte("OpusHead")) {
		return nil, oggMalformed("its first page isn't an Opus head")
	}
	if version := head[8]; version>>4 != 0 {
		return nil, oggMalformed("its Opus head is of version %d", version)
	}
	o.Channels, o.PreSkip = int(head[9]), int(binary.LittleEndian.Uint16(head[10:]))
	if family := head[18]; family != 0 || o.Channels < 1 || o.Channels > 2 {
		return nil, oggMalformed("it isn't Opus in mono or stereo")
	}
	tags, err := o.packet()
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(tags, []byte("OpusTags")) {
		return nil, oggMalformed("its second packet isn't Opus tags")
	}
	return o, nil
}

// Next returns the stream's next packet, and how many samples at OpusRate
// it holds, or io.EOF after its last.
func (o *OggOpus) Next() ([]byte, int, error) {
	p, err := o.packet()
	if err != nil {
		return nil, 0, err
	}
	n := OpusSamples(p)
	if n == 0 {
		return nil, 0, oggMalformed("a packet isn't Opus")
	}
	return p, n, nil
}

// Length reads the rest of the stream, and says how long it plays, as
// samples at OpusRate after the head's pre-skip, and how long its last
// packet is.
func (o *OggOpus) Length() (samples, last int, err error) {
	for {
		_, n, err := o.Next()
		if errors.Is(err, io.EOF) {
			return max(samples-o.PreSkip, 0), last, nil
		}
		if err != nil {
			return 0, 0, err
		}
		samples, last = samples+n, n
	}
}

// packet returns the next packet whole, reading pages as it needs them.
func (o *OggOpus) packet() ([]byte, error) {
	for len(o.packets) == 0 {
		if o.last {
			if o.partial != nil {
				return nil, oggMalformed("its last packet is cut off")
			}
			return nil, io.EOF
		}
		if err := o.readPage(); err != nil {
			return nil, err
		}
	}
	p := o.packets[0]
	o.packets = o.packets[1:]
	return p, nil
}

// readPage reads a page, and splits it into the packets it ends, keeping
// the one that goes on in the next.
func (o *OggOpus) readPage() error {
	var header [27]byte
	if _, err := io.ReadFull(o.r, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return oggMalformed("it ends before its last page")
		}
		return oggMalformed("a page is cut off")
	}
	if string(header[:4]) != "OggS" || header[4] != 0 {
		return oggMalformed("a page doesn't start as Ogg's do")
	}
	kind := header[5]
	serial, number := binary.LittleEndian.Uint32(header[14:]), binary.LittleEndian.Uint32(header[18:])
	first := o.page == 0
	switch {
	case first != (kind&oggFirst != 0):
		return oggMalformed("its pages don't start one stream")
	case !first && serial != o.serial:
		return oggMalformed("it holds more than one stream")
	case number != o.page:
		return oggMalformed("its pages are out of order")
	case (kind&oggContinued != 0) != (o.partial != nil):
		return oggMalformed("a packet that goes on between pages doesn't")
	}
	o.serial, o.page = serial, o.page+1
	o.last = kind&oggLast != 0
	lacing := make([]byte, header[26])
	if _, err := io.ReadFull(o.r, lacing); err != nil {
		return oggMalformed("a page is cut off")
	}
	size := 0
	for _, l := range lacing {
		size += int(l)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(o.r, body); err != nil {
		return oggMalformed("a page is cut off")
	}
	// The checksum covers the page with its own field zeroed.
	want := binary.LittleEndian.Uint32(header[22:])
	clear(header[22:26])
	if oggCRC(oggCRC(oggCRC(0, header[:]), lacing), body) != want {
		return oggMalformed("a page doesn't match its checksum")
	}
	// A lacing value under 255 ends a packet; a page whose last is 255
	// leaves its last packet to go on in the next.
	at := 0
	for _, l := range lacing {
		o.partial = append(o.partial, body[at:at+int(l)]...)
		at += int(l)
		if len(o.partial) > maxOpusPacket {
			return oggMalformed("a packet is larger than %d bytes", maxOpusPacket)
		}
		if l < 255 {
			o.packets = append(o.packets, o.partial)
			o.partial = nil
		}
	}
	return nil
}

// OpusSamples is how many samples at OpusRate an Opus packet holds, by its
// table of contents (RFC 6716, 3.1), or 0 for one that isn't a packet.
func OpusSamples(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	config := int(p[0] >> 3)
	var frame int
	switch {
	case config < 12: // SILK
		frame = [4]int{480, 960, 1920, 2880}[config&3]
	case config < 16: // hybrid
		frame = [2]int{480, 960}[config&1]
	default: // CELT
		frame = [4]int{120, 240, 480, 960}[config&3]
	}
	frames := 1
	switch p[0] & 3 {
	case 1, 2:
		frames = 2
	case 3:
		if len(p) < 2 {
			return 0
		}
		frames = int(p[1] & 0x3f)
	}
	if n := frames * frame; frames > 0 && n <= maxOpusSamples {
		return n
	}
	return 0
}

func oggMalformed(format string, args ...any) error {
	return fmt.Errorf("%w: Opus in Ogg: %s", ErrMalformed, fmt.Sprintf(format, args...))
}

// oggTable is Ogg's CRC-32: polynomial 0x04c11db7, shifted left, from 0.
var oggTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for range 8 {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()

func oggCRC(crc uint32, b []byte) uint32 {
	for _, x := range b {
		crc = crc<<8 ^ oggTable[byte(crc>>24)^x]
	}
	return crc
}
