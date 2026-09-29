package media

import (
	"bufio"
	"encoding/binary"
)

// gif copies a GIF block by block. It keeps the images and their graphic
// controls (timing and transparency) and the looping extension, and leaves
// out comments, plain text blocks and other application extensions, which
// is where XMP goes.
func (f *filterState) gif(r *bufio.Reader) error {
	var hdr [13]byte // signature and logical screen descriptor
	if err := f.readFull(r, hdr[:]); err != nil {
		return err
	}
	if s := string(hdr[:6]); s != "GIF87a" && s != "GIF89a" {
		return malformed(GIF, "no signature")
	}
	f.res.Width = int(binary.LittleEndian.Uint16(hdr[6:8]))
	f.res.Height = int(binary.LittleEndian.Uint16(hdr[8:10]))
	if f.res.Width == 0 || f.res.Height == 0 {
		return malformed(GIF, "no size")
	}
	// The first frame is drawn onto a canvas the size of the screen.
	f.res.pixelBytes = 5
	if err := f.write(hdr[:]); err != nil {
		return err
	}
	if hdr[10]&0x80 != 0 {
		if err := f.copyN(r, 3<<(hdr[10]&7+1)); err != nil {
			return err
		}
	}
	images := 0
	for {
		b, err := r.ReadByte()
		if err != nil {
			return malformed(GIF, "cut short")
		}
		switch b {
		case 0x21: // an extension
			label, err := r.ReadByte()
			if err != nil {
				return malformed(GIF, "cut short")
			}
			if err := f.gifExtension(r, label); err != nil {
				return err
			}
		case 0x2C: // an image
			var desc [10]byte // the separator, position, size and flags
			desc[0] = b
			if err := f.readFull(r, desc[1:]); err != nil {
				return err
			}
			if err := f.write(desc[:]); err != nil {
				return err
			}
			if desc[9]&0x80 != 0 { // a local color table
				if err := f.copyN(r, 3<<(desc[9]&7+1)); err != nil {
					return err
				}
			}
			code, err := r.ReadByte() // the LZW minimum code size
			if err != nil {
				return malformed(GIF, "cut short")
			}
			if err := f.w.WriteByte(code); err != nil {
				return err
			}
			if err := f.gifBlocks(r, true); err != nil {
				return err
			}
			images++
		case 0x3B: // the trailer
			if images == 0 {
				return malformed(GIF, "no image")
			}
			f.res.Animated = images > 1
			if err := f.w.WriteByte(b); err != nil {
				return err
			}
			return f.trailing(r)
		default:
			return malformed(GIF, "an unknown block 0x%02X", b)
		}
	}
}

// gifExtension copies or leaves out one extension, whose introducer and
// label have been read.
func (f *filterState) gifExtension(r *bufio.Reader, label byte) error {
	keep := false
	var first []byte
	switch label {
	case 0xF9: // graphic control: frame timing and transparency
		keep = true
	case 0xFF: // application
		n, err := r.ReadByte()
		if err != nil {
			return malformed(GIF, "cut short")
		}
		first = make([]byte, n)
		if err := f.readFull(r, first); err != nil {
			return err
		}
		id := string(first)
		keep = id == "NETSCAPE2.0" || id == "ANIMEXTS1.0" // how many times to loop
		first = append([]byte{n}, first...)
	}
	if !keep {
		what := "application extension"
		switch label {
		case 0xFE:
			what = "comment"
		case 0x01:
			what = "plain text block"
		}
		if err := f.drop(what); err != nil {
			return err
		}
		return f.gifBlocks(r, false)
	}
	if err := f.write([]byte{0x21, label}); err != nil {
		return err
	}
	if err := f.write(first); err != nil {
		return err
	}
	return f.gifBlocks(r, true)
}

// gifBlocks copies or skips data sub-blocks up to and including the
// terminator.
func (f *filterState) gifBlocks(r *bufio.Reader, keep bool) error {
	for {
		n, err := r.ReadByte()
		if err != nil {
			return malformed(GIF, "cut short")
		}
		if keep {
			if err := f.w.WriteByte(n); err != nil {
				return err
			}
		}
		if n == 0 {
			return nil
		}
		if keep {
			err = f.copyN(r, int64(n))
		} else {
			err = f.skip(r, int64(n))
		}
		if err != nil {
			return err
		}
	}
}
