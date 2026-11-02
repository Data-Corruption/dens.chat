package sfutest

import (
	"encoding/binary"
)

// Sounds for the den's soundboard (M14.3), as the page of the staff member
// adding one writes it: Opus packets in Ogg, each on a page of its own.

// SoundPackets makes n packets of 20 ms of Opus, each naming its place in
// its last two bytes and padded to size bytes, at least 3: 160 is what
// 64 kbps makes of 20 ms.
func SoundPackets(n, size int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		p := make([]byte, max(size, 3))
		p[0] = 0xfc // CELT, 20 ms, stereo, one frame
		p[len(p)-2], p[len(p)-1] = byte(i>>8), byte(i)
		out[i] = p
	}
	return out
}

// PlaceOf is the place a packet SoundPackets made names.
func PlaceOf(payload []byte) int {
	if len(payload) < 3 {
		return -1
	}
	return int(payload[len(payload)-2])<<8 | int(payload[len(payload)-1])
}

// Sound writes Opus packets of 20 ms as a sound's Ogg file, in stereo with
// libopus's pre-skip, as the page writes one.
func Sound(packets [][]byte) []byte {
	const preSkip = 312
	head := []byte("OpusHead\x01\x02")
	head = binary.LittleEndian.AppendUint16(head, preSkip)
	head = binary.LittleEndian.AppendUint32(head, 48000)
	head = append(head, 0, 0, 0)
	tags := binary.LittleEndian.AppendUint32([]byte("OpusTags"), 4)
	tags = binary.LittleEndian.AppendUint32(append(tags, "Test"...), 0)
	out := append(oggPage(head, 0x02, 0, 0), oggPage(tags, 0, 0, 1)...)
	for i, p := range packets {
		flags := byte(0)
		if i == len(packets)-1 {
			flags = 0x04
		}
		out = append(out, oggPage(p, flags, uint64(i+1)*960, uint32(i+2))...)
	}
	return out
}

// oggPage writes one Ogg page holding one packet whole.
func oggPage(packet []byte, flags byte, granule uint64, number uint32) []byte {
	var lacing []byte
	for n := len(packet); ; n -= 255 {
		lacing = append(lacing, byte(min(n, 255)))
		if n < 255 {
			break
		}
	}
	p := []byte("OggS\x00")
	p = append(p, flags)
	p = binary.LittleEndian.AppendUint64(p, granule)
	p = binary.LittleEndian.AppendUint32(p, 0x74736554)
	p = binary.LittleEndian.AppendUint32(p, number)
	p = append(p, 0, 0, 0, 0, byte(len(lacing)))
	p = append(append(p, lacing...), packet...)
	var crc uint32
	for _, b := range p {
		crc = crc<<8 ^ oggTable[byte(crc>>24)^b]
	}
	binary.LittleEndian.PutUint32(p[22:], crc)
	return p
}

// oggTable is Ogg's CRC-32: polynomial 0x04c11db7, shifted left.
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
