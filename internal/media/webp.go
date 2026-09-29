package media

import (
	"bufio"
	"encoding/binary"
	"io"
)

// WebP flags in the VP8X chunk.
const (
	webpAnimation = 1 << 1
	webpXMP       = 1 << 2
	webpEXIF      = 1 << 3
	webpAlpha     = 1 << 4
	webpICC       = 1 << 5
)

// webpKept are the chunks a WebP keeps: the image data, alpha, animation
// and the color profile. EXIF, XMP and unknown chunks go, at the top level
// and inside animation frames.
var webpKept = map[string]bool{"VP8X": true, "VP8 ": true, "VP8L": true, "ALPH": true, "ANIM": true, "ANMF": true, "ICCP": true}

// frameKept are the chunks an animation frame keeps.
var frameKept = map[string]bool{"VP8 ": true, "VP8L": true, "ALPH": true}

type riffChunk struct {
	id   string
	data []byte
}

// webpStrip rewrites a WebP without its metadata. The RIFF header states
// the file's size, which leaving chunks out changes, so the whole file is
// read first.
func (f *filterState) webpStrip(r *bufio.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return malformed(WebP, "no RIFF header")
	}
	end := 8 + int64(binary.LittleEndian.Uint32(data[4:8]))
	if end > int64(len(data)) || end < 12 {
		return malformed(WebP, "cut short")
	}
	if end < int64(len(data)) {
		if err := f.drop("data after the image's end"); err != nil {
			return err
		}
	}
	chunks, err := riffChunks(data[12:end])
	if err != nil {
		return err
	}
	var out []riffChunk
	for i, c := range chunks {
		switch {
		case !webpKept[c.id]:
			if err := f.drop(c.id + " chunk"); err != nil {
				return err
			}
			continue
		case c.id == "VP8X":
			if i != 0 || len(c.data) != 10 {
				return malformed(WebP, "a misplaced or bad extended header")
			}
			if c.data[0]&(webpEXIF|webpXMP) != 0 {
				c.data = append([]byte{c.data[0] &^ (webpEXIF | webpXMP)}, c.data[1:]...)
				if err := f.drop("metadata flags"); err != nil {
					return err
				}
			}
		case c.id == "ANMF":
			if len(c.data) < 16 {
				return malformed(WebP, "a short animation frame")
			}
			frame, err := riffChunks(c.data[16:])
			if err != nil {
				return err
			}
			kept := append([]byte{}, c.data[:16]...)
			for _, fc := range frame {
				if !frameKept[fc.id] {
					if err := f.drop(fc.id + " chunk in a frame"); err != nil {
						return err
					}
					continue
				}
				kept = appendChunk(kept, fc)
			}
			c.data = kept
		}
		if err := f.webpNote(c); err != nil {
			return err
		}
		out = append(out, c)
	}
	body := []byte("WEBP")
	for _, c := range out {
		body = appendChunk(body, c)
	}
	if f.res.Width == 0 || f.res.Height == 0 {
		return malformed(WebP, "no image")
	}
	hdr := make([]byte, 8, 8+len(body))
	copy(hdr, "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(body)))
	return f.write(append(hdr, body...))
}

// webpVerify copies a WebP chunk by chunk without holding it, failing on
// any chunk Strip would leave out.
func (f *filterState) webpVerify(r *bufio.Reader) error {
	var hdr [12]byte
	if err := f.readFull(r, hdr[:]); err != nil {
		return err
	}
	if string(hdr[:4]) != "RIFF" || string(hdr[8:12]) != "WEBP" {
		return malformed(WebP, "no RIFF header")
	}
	left := int64(binary.LittleEndian.Uint32(hdr[4:8])) - 4
	if left < 0 {
		return malformed(WebP, "a bad size")
	}
	if err := f.write(hdr[:]); err != nil {
		return err
	}
	for i := 0; left > 0; i++ {
		var ch [8]byte
		if left < 8 {
			return malformed(WebP, "a bad size")
		}
		if err := f.readFull(r, ch[:]); err != nil {
			return err
		}
		id, n := string(ch[:4]), int64(binary.LittleEndian.Uint32(ch[4:8]))
		padded := n + n&1
		if left -= 8 + padded; left < 0 {
			return malformed(WebP, "a chunk runs past the end")
		}
		if !webpKept[id] {
			if err := f.drop(id + " chunk"); err != nil {
				return err
			}
		}
		if id == "VP8X" && i != 0 {
			return malformed(WebP, "a misplaced extended header")
		}
		if err := f.write(ch[:]); err != nil {
			return err
		}
		// The headers that give the size and flags are small; frames are
		// checked chunk by chunk; image data is only copied.
		var c riffChunk
		switch id {
		case "VP8X", "VP8 ", "VP8L":
			head := min(n, 30)
			c = riffChunk{id: id, data: make([]byte, head)}
			if err := f.readFull(r, c.data); err != nil {
				return err
			}
			if id == "VP8X" && len(c.data) > 0 && c.data[0]&(webpEXIF|webpXMP) != 0 {
				if err := f.drop("metadata flags"); err != nil {
					return err
				}
			}
			if err := f.write(c.data); err != nil {
				return err
			}
			if err := f.copyN(r, padded-head); err != nil {
				return err
			}
		case "ANMF":
			if err := f.webpFrame(r, n); err != nil {
				return err
			}
			if err := f.copyN(r, padded-n); err != nil {
				return err
			}
			c = riffChunk{id: id}
		default:
			if err := f.copyN(r, padded); err != nil {
				return err
			}
		}
		if err := f.webpNote(c); err != nil {
			return err
		}
	}
	if f.res.Width == 0 || f.res.Height == 0 {
		return malformed(WebP, "no image")
	}
	return f.trailing(r)
}

// webpFrame copies an animation frame, checking the chunks inside it.
func (f *filterState) webpFrame(r *bufio.Reader, n int64) error {
	if n < 16 {
		return malformed(WebP, "a short animation frame")
	}
	if err := f.copyN(r, 16); err != nil {
		return err
	}
	for left := n - 16; left > 0; {
		var ch [8]byte
		if left < 8 {
			return malformed(WebP, "a bad frame size")
		}
		if err := f.readFull(r, ch[:]); err != nil {
			return err
		}
		id, m := string(ch[:4]), int64(binary.LittleEndian.Uint32(ch[4:8]))
		padded := m + m&1
		if left -= 8 + padded; left < 0 {
			return malformed(WebP, "a frame chunk runs past the frame")
		}
		if !frameKept[id] {
			if err := f.drop(id + " chunk in a frame"); err != nil {
				return err
			}
		}
		if err := f.write(ch[:]); err != nil {
			return err
		}
		if err := f.copyN(r, padded); err != nil {
			return err
		}
	}
	return nil
}

// webpNote takes the size and animation from the chunks that say them.
func (f *filterState) webpNote(c riffChunk) error {
	d := c.data
	switch c.id {
	case "VP8X":
		if len(d) != 10 {
			return malformed(WebP, "a bad extended header")
		}
		f.res.Width = 1 + (int(d[4]) | int(d[5])<<8 | int(d[6])<<16)
		f.res.Height = 1 + (int(d[7]) | int(d[8])<<8 | int(d[9])<<16)
		f.res.Animated = d[0]&webpAnimation != 0
		f.res.pixelBytes = 4
	case "VP8 ":
		if f.res.Width != 0 {
			return nil
		}
		if len(d) < 10 || d[3] != 0x9d || d[4] != 0x01 || d[5] != 0x2a {
			return malformed(WebP, "a bad lossy header")
		}
		f.res.Width = int(binary.LittleEndian.Uint16(d[6:8]) & 0x3fff)
		f.res.Height = int(binary.LittleEndian.Uint16(d[8:10]) & 0x3fff)
		f.res.pixelBytes = 4
	case "VP8L":
		if f.res.Width != 0 {
			return nil
		}
		if len(d) < 5 || d[0] != 0x2f {
			return malformed(WebP, "a bad lossless header")
		}
		bits := binary.LittleEndian.Uint32(d[1:5])
		f.res.Width = 1 + int(bits&0x3fff)
		f.res.Height = 1 + int(bits>>14&0x3fff)
		f.res.pixelBytes = 4
	}
	return nil
}

// riffChunks splits RIFF chunk data into chunks.
func riffChunks(b []byte) ([]riffChunk, error) {
	var out []riffChunk
	for len(b) > 0 {
		if len(b) < 8 {
			return nil, malformed(WebP, "a short chunk")
		}
		n := int64(binary.LittleEndian.Uint32(b[4:8]))
		if n > int64(len(b)-8) {
			return nil, malformed(WebP, "a chunk runs past the end")
		}
		out = append(out, riffChunk{id: string(b[:4]), data: b[8 : 8+n]})
		next := 8 + n + n&1
		if next > int64(len(b)) {
			return nil, malformed(WebP, "a chunk is missing its padding")
		}
		b = b[next:]
	}
	return out, nil
}

func appendChunk(b []byte, c riffChunk) []byte {
	b = append(b, c.id...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(c.data)))
	b = append(b, c.data...)
	if len(c.data)%2 == 1 {
		b = append(b, 0)
	}
	return b
}
