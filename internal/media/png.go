package media

import (
	"bufio"
	"encoding/binary"
	"hash/crc32"
	"io"
)

// pngKept are the chunks a PNG keeps: the critical ones, what color and
// transparency need, and the frames of an animated PNG. Text, EXIF, time
// stamps, suggested palettes (which have names) and private chunks go.
var pngKept = map[string]bool{
	"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true,
	"tRNS": true, "cHRM": true, "gAMA": true, "iCCP": true, "sBIT": true, "sRGB": true,
	"cICP": true, "mDCV": true, "cLLI": true, "bKGD": true, "hIST": true, "pHYs": true,
	"acTL": true, "fcTL": true, "fdAT": true,
}

// png copies a PNG chunk by chunk, checking each kept chunk's checksum,
// and stops at its end chunk. An eXIf chunk's orientation isn't kept:
// browsers don't agree on whether to apply it, and an image that turns in
// one browser and not another would shift the layout.
func (f *filterState) png(r *bufio.Reader) error {
	var sig [8]byte
	if err := f.readFull(r, sig[:]); err != nil {
		return err
	}
	if string(sig[:]) != "\x89PNG\r\n\x1a\n" {
		return malformed(PNG, "no signature")
	}
	if err := f.write(sig[:]); err != nil {
		return err
	}
	first := true
	for {
		var hdr [8]byte
		if err := f.readFull(r, hdr[:]); err != nil {
			return err
		}
		n := binary.BigEndian.Uint32(hdr[:4])
		typ := string(hdr[4:8])
		if n > 1<<31-1 || !chunkName(typ) {
			return malformed(PNG, "a bad chunk header")
		}
		if first && (typ != "IHDR" || n != 13) {
			return malformed(PNG, "no header chunk")
		}
		first = false
		if !pngKept[typ] {
			if typ[0]&0x20 == 0 {
				// A critical chunk this doesn't know can't be left out,
				// and can't be shown either.
				return malformed(PNG, "an unknown critical chunk %q", typ)
			}
			if err := f.drop(typ + " chunk"); err != nil {
				return err
			}
			if err := f.skip(r, int64(n)+4); err != nil {
				return err
			}
			continue
		}
		if err := f.write(hdr[:]); err != nil {
			return err
		}
		crc := crc32.NewIEEE()
		crc.Write(hdr[4:8])
		switch typ {
		case "IHDR", "acTL":
			if typ == "acTL" && n != 8 {
				return malformed(PNG, "a bad animation header")
			}
			data := make([]byte, n)
			if err := f.readFull(r, data); err != nil {
				return err
			}
			if typ == "IHDR" {
				f.pngHeader(data)
				if f.res.Width == 0 || f.res.Height == 0 {
					return malformed(PNG, "no size in the header")
				}
			} else if len(data) >= 4 && binary.BigEndian.Uint32(data[:4]) > 1 {
				f.res.Animated = true
			}
			crc.Write(data)
			if err := f.write(data); err != nil {
				return err
			}
		default:
			if err := f.copyN(io.TeeReader(r, crc), int64(n)); err != nil {
				return err
			}
		}
		var sum [4]byte
		if err := f.readFull(r, sum[:]); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(sum[:]) != crc.Sum32() {
			return malformed(PNG, "a bad checksum in the %s chunk", typ)
		}
		if err := f.write(sum[:]); err != nil {
			return err
		}
		if typ == "IEND" {
			return f.trailing(r)
		}
	}
}

// pngHeader reads the size from the header chunk, and what a pixel costs
// decoded.
func (f *filterState) pngHeader(ihdr []byte) {
	f.res.Width = int(binary.BigEndian.Uint32(ihdr[0:4]))
	f.res.Height = int(binary.BigEndian.Uint32(ihdr[4:8]))
	if f.res.Width > 1<<24 || f.res.Height > 1<<24 {
		f.res.Width, f.res.Height = 0, 0
	}
	depth, color := ihdr[8], ihdr[9]
	switch {
	case color == 3, color == 0 && depth <= 8:
		f.res.pixelBytes = 1
	case color == 0:
		f.res.pixelBytes = 2
	case depth == 16:
		f.res.pixelBytes = 8
	default:
		f.res.pixelBytes = 4
	}
}

func chunkName(s string) bool {
	for i := range len(s) {
		if c := s[i] | 0x20; c < 'a' || c > 'z' {
			return false
		}
	}
	return len(s) == 4
}
