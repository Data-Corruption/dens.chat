package media

import (
	"bufio"
	"bytes"
	"encoding/binary"
)

// JPEG markers.
const (
	jpegSOI   = 0xD8
	jpegEOI   = 0xD9
	jpegSOS   = 0xDA
	jpegAPP0  = 0xE0
	jpegAPP1  = 0xE1
	jpegAPP2  = 0xE2
	jpegAPP14 = 0xEE
	jpegCOM   = 0xFE
)

// jpeg copies a JPEG segment by segment. It keeps what decoding and color
// need (frame, tables, scans, the JFIF header without its thumbnail, the
// ICC profile and Adobe's color transform), keeps a minimal EXIF block if
// the image isn't upright, and leaves out every other application segment
// and comment. The entropy-coded data is copied as it is, and nothing after
// the end marker survives: that's where phones append the extra images of
// the Multi-Picture Format, such as HDR gain maps and depth maps.
func (f *filterState) jpeg(r *bufio.Reader) error {
	var soi [2]byte
	if err := f.readFull(r, soi[:]); err != nil {
		return err
	}
	if soi != [2]byte{0xFF, jpegSOI} {
		return malformed(JPEG, "no start marker")
	}
	if err := f.write(soi[:]); err != nil {
		return err
	}
	var st jpegState
	var marker byte
	var fill int
	fromScan := false
	for {
		if !fromScan {
			var err error
			if marker, fill, err = f.jpegMarker(r); err != nil {
				return err
			}
		}
		fromScan = false
		switch {
		case marker == jpegEOI:
			if !st.frame {
				return malformed(JPEG, "no image")
			}
			if err := f.jpegWrite(fill, marker, nil, false); err != nil {
				return err
			}
			if f.res.Orientation >= 5 {
				f.res.Width, f.res.Height = f.res.Height, f.res.Width
			}
			return f.trailing(r)
		case marker >= 0xD0 && marker <= 0xD7, marker == 0x01:
			// Restart markers belong inside scans, but some encoders leave
			// one after the last; decoders ignore it.
			if err := f.jpegWrite(fill, marker, nil, false); err != nil {
				return err
			}
			continue
		}
		payload, err := f.jpegPayload(r)
		if err != nil {
			return err
		}
		if marker == jpegSOS {
			if !st.frame {
				return malformed(JPEG, "a scan before the frame header")
			}
			if err := f.jpegWrite(fill, marker, payload, true); err != nil {
				return err
			}
			if marker, err = f.jpegScan(r); err != nil {
				return err
			}
			fill, fromScan = 0, true
			continue
		}
		out, keep, err := f.jpegSegment(marker, payload, &st)
		if err != nil {
			return err
		}
		if keep {
			if err := f.jpegWrite(fill, marker, out, true); err != nil {
				return err
			}
		}
	}
}

type jpegState struct {
	frame bool // seen the frame header
	exif  bool // seen the first EXIF block
}

// jpegMarker reads up to the next marker, and returns it with the number
// of fill bytes (extra 0xFF) that came before it.
func (f *filterState) jpegMarker(r *bufio.Reader) (marker byte, fill int, err error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, 0, malformed(JPEG, "cut short")
	}
	if b != 0xFF {
		// Decoders skip stray bytes before a marker; they're no part of
		// the image, and they could hold anything.
		if err := f.drop("stray bytes between segments"); err != nil {
			return 0, 0, err
		}
		for b != 0xFF {
			if b, err = r.ReadByte(); err != nil {
				return 0, 0, malformed(JPEG, "cut short")
			}
		}
	}
	for {
		if b, err = r.ReadByte(); err != nil {
			return 0, 0, malformed(JPEG, "cut short")
		}
		if b != 0xFF {
			return b, fill, nil
		}
		fill++
	}
}

func (f *filterState) jpegPayload(r *bufio.Reader) ([]byte, error) {
	var n [2]byte
	if err := f.readFull(r, n[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(n[:]))
	if size < 2 {
		return nil, malformed(JPEG, "a segment length of %d", size)
	}
	p := make([]byte, size-2)
	return p, f.readFull(r, p)
}

// jpegWrite writes a marker, after its fill bytes, and its payload if it
// has a length.
func (f *filterState) jpegWrite(fill int, marker byte, payload []byte, sized bool) error {
	for range fill {
		if err := f.w.WriteByte(0xFF); err != nil {
			return err
		}
	}
	if err := f.write([]byte{0xFF, marker}); err != nil {
		return err
	}
	if !sized {
		return nil
	}
	var n [2]byte
	binary.BigEndian.PutUint16(n[:], uint16(len(payload)+2))
	if err := f.write(n[:]); err != nil {
		return err
	}
	return f.write(payload)
}

// jpegScan copies entropy-coded data, and returns the marker that ends it.
func (f *filterState) jpegScan(r *bufio.Reader) (byte, error) {
	for {
		if _, err := r.Peek(1); err != nil {
			return 0, malformed(JPEG, "cut short")
		}
		data, _ := r.Peek(r.Buffered())
		i := bytes.IndexByte(data, 0xFF)
		if i < 0 {
			i = len(data)
		}
		if err := f.write(data[:i]); err != nil {
			return 0, err
		}
		_, _ = r.Discard(i)
		if i == len(data) {
			continue
		}
		two, err := r.Peek(2)
		if err != nil {
			return 0, malformed(JPEG, "cut short")
		}
		switch m := two[1]; {
		case m == 0x00, m >= 0xD0 && m <= 0xD7: // a stuffed 0xFF, or a restart marker
			if err := f.write(two); err != nil {
				return 0, err
			}
			_, _ = r.Discard(2)
		case m == 0xFF: // a fill byte before a marker
			if err := f.w.WriteByte(0xFF); err != nil {
				return 0, err
			}
			_, _ = r.Discard(1)
		default:
			_, _ = r.Discard(2)
			return m, nil
		}
	}
}

// jpegSegment decides what happens to a segment: kept as it is, kept
// rewritten (out), or left out.
func (f *filterState) jpegSegment(marker byte, payload []byte, st *jpegState) (out []byte, keep bool, err error) {
	switch {
	case marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC:
		// A frame header: SOF0 to SOF15.
		if !st.frame {
			if len(payload) < 6 {
				return nil, false, malformed(JPEG, "a short frame header")
			}
			f.res.Height = int(binary.BigEndian.Uint16(payload[1:3]))
			f.res.Width = int(binary.BigEndian.Uint16(payload[3:5]))
			if f.res.Width == 0 || f.res.Height == 0 {
				return nil, false, malformed(JPEG, "no size in the frame header")
			}
			f.res.pixelBytes = max(3, int(payload[5]))
			if payload[5] == 1 {
				f.res.pixelBytes = 1
			}
			st.frame = true
		}
		return payload, true, nil
	case marker == 0xC4, marker == 0xCC, marker == 0xDB, marker == 0xDD, marker == 0xDC, marker == 0xDE, marker == 0xDF:
		// Huffman, arithmetic and quantization tables, the restart
		// interval, the line count, and hierarchical frames.
		return payload, true, nil
	case marker == jpegAPP0:
		if len(payload) >= 14 && string(payload[:5]) == "JFIF\x00" {
			if len(payload) == 14 && payload[12] == 0 && payload[13] == 0 {
				return payload, true, nil
			}
			// A JFIF header can carry a thumbnail, which can show what
			// the image looked like before it was cropped.
			return append(payload[:12:12], 0, 0), true, f.drop("JFIF thumbnail")
		}
		return nil, false, f.drop("APP0 segment")
	case marker == jpegAPP1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) && !st.exif:
		st.exif = true
		o := exifOrientation(payload[6:])
		f.res.Orientation = o
		if o == 1 {
			return nil, false, f.drop("EXIF")
		}
		canon := orientationEXIF(o)
		if bytes.Equal(payload, canon) {
			return payload, true, nil
		}
		return canon, true, f.drop("EXIF")
	case marker == jpegAPP2 && bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")):
		return payload, true, nil
	case marker == jpegAPP14 && len(payload) >= 12 && bytes.HasPrefix(payload, []byte("Adobe")):
		// Adobe's segment says how to convert the colors: fixed fields,
		// and nothing personal. Anything past them goes.
		if len(payload) > 12 {
			return payload[:12], true, f.drop("Adobe segment data")
		}
		return payload, true, nil
	case marker >= jpegAPP0 && marker <= 0xEF:
		return nil, false, f.drop("application segment")
	case marker == jpegCOM:
		return nil, false, f.drop("comment")
	}
	return nil, false, malformed(JPEG, "an unsupported marker 0x%02X", marker)
}

// exifOrientation reads the orientation tag from an EXIF block's TIFF
// structure, and returns 1 when there is none or it can't be read.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	off := int64(bo.Uint32(tiff[4:8]))
	if off < 8 || off+2 > int64(len(tiff)) {
		return 1
	}
	n := int64(bo.Uint16(tiff[off : off+2]))
	for i := range n {
		e := off + 2 + 12*i
		if e+12 > int64(len(tiff)) {
			break
		}
		if bo.Uint16(tiff[e:e+2]) != 0x0112 {
			continue
		}
		if bo.Uint16(tiff[e+2:e+4]) != 3 || bo.Uint32(tiff[e+4:e+8]) != 1 {
			return 1
		}
		if v := int(bo.Uint16(tiff[e+8 : e+10])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orientationEXIF is the smallest EXIF block that holds an orientation:
// a big-endian TIFF header and one directory with the one tag. Browsers
// read it to show the image upright; it says nothing else.
func orientationEXIF(o int) []byte {
	return []byte{
		'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0, 42, 0, 0, 0, 8, // TIFF header, first directory at 8
		0, 1, // one entry
		0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(o), 0, 0, // orientation: SHORT, one value
		0, 0, 0, 0, // no next directory
	}
}
