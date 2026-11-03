package media

import (
	"bytes"
	"compress/lzw"
	"encoding/binary"
	"errors"
	"image"
	"io"

	"golang.org/x/image/webp"
)

// The frames of an animated GIF or WebP, for an animated picture (M15.1).
// An animation is read twice: first its frames' places, timing and count,
// without decoding any, so one past the budget is refused before it costs
// anything; then each frame's pixels in turn, as it's drawn.

// disposal is what happens to a frame's rectangle once its time is up.
type disposal uint8

const (
	keepFrame    disposal = iota // left as it was drawn
	clearFrame                   // cleared to transparent
	restoreFrame                 // put back as it was before the frame
)

// animFrame is one frame of an animation, read but not decoded.
type animFrame struct {
	// rect is where the frame is drawn on the canvas.
	rect    image.Rectangle
	ms      int
	dispose disposal
	// replace draws the frame's pixels in place of those under it, where
	// otherwise they go over them: a WebP's frame that doesn't blend.
	replace bool

	// A GIF's frame: its LZW data as sub-blocks and the code size it
	// starts with, its color table, its transparent index or -1, and
	// whether its rows are interlaced.
	lzw         []byte
	litWidth    int
	colors      []byte
	transparent int
	interlaced  bool

	// A WebP's frame: its alpha and its image chunk, or for a still, the
	// whole file.
	alpha, image []byte
	imageID      string
	file         []byte
}

// animation is an animated GIF or WebP's frames, read but not decoded.
type animation struct {
	kind          Kind
	width, height int
	// plays is how many times the animation plays, 0 for forever.
	plays  int
	frames []animFrame
}

// frameMS is how long a frame shows, from what its file says, in
// milliseconds: browsers show a frame of 10 ms or less, which files often
// say to mean "as fast as you can", for 100.
func frameMS(ms int) int {
	if ms <= 10 {
		return 100
	}
	return ms
}

// readAnimation reads an animated GIF's or WebP's frames, at most limit of
// them: one past it is too large.
func readAnimation(data []byte, k Kind, limit int) (*animation, error) {
	switch k {
	case GIF:
		return readGIF(data, limit)
	case WebP:
		return readWebP(data, limit)
	}
	return nil, errors.New("only a GIF or a WebP is animated")
}

// readGIF reads a GIF's frames. Browsers show a GIF that stops early, or
// has something other than a block after one, up to there, so reading
// stops there too, keeping the whole frames before; a GIF without one is
// damaged. The canvas is the GIF's screen, grown to hold its first frame,
// as browsers grow a screen too small for it.
func readGIF(data []byte, limit int) (*animation, error) {
	if len(data) < 13 || string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a" {
		return nil, malformed(GIF, "no signature")
	}
	a := &animation{kind: GIF, plays: 1, width: int(binary.LittleEndian.Uint16(data[6:])), height: int(binary.LittleEndian.Uint16(data[8:]))}
	p := 13
	var global []byte
	if flags := data[10]; flags&0x80 != 0 {
		n := 3 << (flags&7 + 1)
		if len(data) < p+n {
			return nil, malformed(GIF, "cut short")
		}
		global, p = data[p:p+n], p+n
	}
	// A graphic control extension applies to the image after it.
	delay, dispose, transparent := 0, keepFrame, -1
blocks:
	for p < len(data) {
		switch data[p] {
		case 0x21: // an extension
			if p+2 >= len(data) {
				break blocks
			}
			label, start := data[p+1], p+2
			end, ok := subBlocks(data, start)
			if !ok {
				break blocks
			}
			switch first := data[start:end]; {
			case label == 0xF9 && first[0] >= 4 && len(first) >= 6: // graphic control
				delay = 10 * int(binary.LittleEndian.Uint16(first[2:]))
				switch first[1] >> 2 & 7 {
				case 2:
					dispose = clearFrame
				case 3, 4: // browsers read 4 as 3, which some files use
					dispose = restoreFrame
				default:
					dispose = keepFrame
				}
				transparent = -1
				if first[1]&1 != 0 {
					transparent = int(first[4])
				}
			case label == 0xFF && first[0] == 11 && len(first) >= 16 && first[12] >= 3 && first[13] == 1:
				// How many times it loops after the first.
				if id := string(first[1:12]); id == "NETSCAPE2.0" || id == "ANIMEXTS1.0" {
					a.plays = 0
					if loops := int(binary.LittleEndian.Uint16(first[14:])); loops > 0 {
						a.plays = loops + 1
					}
				}
			}
			p = end
		case 0x2C: // an image
			if p+11 > len(data) {
				break blocks
			}
			d := data[p+1 : p+10]
			x, y := int(binary.LittleEndian.Uint16(d)), int(binary.LittleEndian.Uint16(d[2:]))
			w, h := int(binary.LittleEndian.Uint16(d[4:])), int(binary.LittleEndian.Uint16(d[6:]))
			f := animFrame{rect: image.Rect(x, y, x+w, y+h), ms: frameMS(delay), dispose: dispose, transparent: transparent,
				interlaced: d[8]&0x40 != 0, colors: global}
			q := p + 10
			if d[8]&0x80 != 0 {
				n := 3 << (d[8]&7 + 1)
				if q+n >= len(data) {
					break blocks
				}
				f.colors, q = data[q:q+n], q+n
			}
			f.litWidth = int(data[q])
			end, ok := subBlocks(data, q+1)
			if !ok {
				break blocks
			}
			if f.litWidth < 2 || f.litWidth > 8 {
				return nil, malformed(GIF, "a code size of %d", f.litWidth)
			}
			if len(a.frames) == limit {
				return nil, ErrAnimationTooLarge
			}
			f.lzw = data[q+1 : end]
			if len(a.frames) == 0 {
				a.width, a.height = max(a.width, f.rect.Max.X), max(a.height, f.rect.Max.Y)
			}
			a.frames = append(a.frames, f)
			delay, dispose, transparent = 0, keepFrame, -1
			p = end
		default: // the trailer, or something that isn't a block
			break blocks
		}
	}
	if len(a.frames) == 0 {
		return nil, malformed(GIF, "no image")
	}
	return a, nil
}

// subBlocks finds the end of the data sub-blocks starting at p, past their
// terminator, or says they run past the data.
func subBlocks(data []byte, p int) (int, bool) {
	for p < len(data) {
		n := int(data[p])
		if n == 0 {
			return p + 1, true
		}
		p += 1 + n
	}
	return 0, false
}

// gifIndexes decodes a GIF frame's color indexes into pix, which holds the
// frame, in the order its rows come, and says how many it decoded: a frame
// cut short or damaged partway keeps what came before, as browsers show
// it, and data after the frame's last pixel is left unread.
func (f *animFrame) gifIndexes(pix []byte) int {
	r := lzw.NewReader(&blockReader{data: f.lzw}, lzw.LSB, f.litWidth)
	defer r.Close()
	n, _ := io.ReadFull(r, pix)
	return n
}

// blockReader reads the data in a run of GIF sub-blocks, up to their
// terminator.
type blockReader struct {
	data []byte
	left int // what's left of the sub-block it's in
}

func (b *blockReader) Read(p []byte) (int, error) {
	for b.left == 0 {
		if len(b.data) == 0 || b.data[0] == 0 {
			return 0, io.EOF
		}
		b.left = min(int(b.data[0]), len(b.data)-1)
		b.data = b.data[1:]
	}
	n := copy(p, b.data[:b.left])
	b.data, b.left = b.data[n:], b.left-n
	return n, nil
}

// gifRow is which of an interlaced frame's rows, in the order they come,
// shows as row r of h: every eighth from the first, every eighth from the
// fifth, every fourth from the third, then the rest.
func gifRow(r, h int) int {
	switch {
	case r%8 == 0:
		return r / 8
	case r%8 == 4:
		return (h+7)/8 + r/8
	case r%4 == 2:
		return (h+7)/8 + (h+3)/8 + r/4
	}
	return (h+7)/8 + (h+3)/8 + (h+1)/4 + r/2
}

// readWebP reads a WebP's frames: each of an animation's, with its place,
// time, disposal and blending, or a still's one. A frame must lie on the
// canvas, as the WebP library browsers use requires.
func readWebP(data []byte, limit int) (*animation, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, malformed(WebP, "no RIFF header")
	}
	end := 8 + int64(binary.LittleEndian.Uint32(data[4:8]))
	if end > int64(len(data)) || end < 12 {
		return nil, malformed(WebP, "cut short")
	}
	data = data[:end]
	chunks, err := riffChunks(data[12:])
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, malformed(WebP, "no image")
	}
	f := &filterState{kind: WebP}
	if err := f.webpNote(chunks[0]); err != nil {
		return nil, err
	}
	a := &animation{kind: WebP, width: f.res.Width, height: f.res.Height, plays: 1}
	if a.width == 0 || a.height == 0 {
		return nil, malformed(WebP, "no image")
	}
	if !f.res.Animated {
		a.frames = []animFrame{{rect: image.Rect(0, 0, a.width, a.height), ms: frameMS(0), file: data}}
		return a, nil
	}
	a.plays = 0
	for _, c := range chunks {
		switch c.id {
		case "ANIM":
			if len(c.data) < 6 {
				return nil, malformed(WebP, "a short animation header")
			}
			a.plays = int(binary.LittleEndian.Uint16(c.data[4:]))
		case "ANMF":
			if len(a.frames) == limit {
				return nil, ErrAnimationTooLarge
			}
			fr, err := webpFrame(c.data, a.width, a.height)
			if err != nil {
				return nil, err
			}
			a.frames = append(a.frames, fr)
		}
	}
	if len(a.frames) == 0 {
		return nil, malformed(WebP, "no animation frame")
	}
	return a, nil
}

// webpFrame reads an animation frame's chunk, on a canvas of w × h.
func webpFrame(d []byte, w, h int) (animFrame, error) {
	if len(d) < 16 {
		return animFrame{}, malformed(WebP, "a short animation frame")
	}
	x, y := 2*get24(d), 2*get24(d[3:])
	f := animFrame{rect: image.Rect(x, y, x+1+get24(d[6:]), y+1+get24(d[9:])), ms: frameMS(get24(d[12:])), replace: d[15]&2 != 0}
	if d[15]&1 != 0 {
		f.dispose = clearFrame
	}
	if f.rect.Max.X > w || f.rect.Max.Y > h {
		return animFrame{}, malformed(WebP, "a frame off the canvas")
	}
	inner, err := riffChunks(d[16:])
	if err != nil {
		return animFrame{}, err
	}
	for _, c := range inner {
		switch {
		case c.id == "ALPH" && f.alpha == nil && f.image == nil:
			f.alpha = c.data
		case (c.id == "VP8 " || c.id == "VP8L") && f.image == nil:
			f.image, f.imageID = c.data, c.id
		}
	}
	if f.image == nil {
		return animFrame{}, malformed(WebP, "a frame without an image")
	}
	return f, nil
}

func get24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// webpImage decodes a WebP frame, rebuilt as a still WebP for the decoder,
// which reads no animations: its image, with its alpha when a lossy image
// has some.
func (f *animFrame) webpImage() (image.Image, error) {
	still := f.file
	if still == nil {
		body := []byte("WEBP")
		w, h := f.rect.Dx(), f.rect.Dy()
		if f.alpha != nil && f.imageID == "VP8 " {
			vp8x := make([]byte, 10)
			vp8x[0] = webpAlpha
			put24(vp8x[4:], w-1)
			put24(vp8x[7:], h-1)
			body = appendChunk(body, riffChunk{id: "VP8X", data: vp8x})
			body = appendChunk(body, riffChunk{id: "ALPH", data: f.alpha})
		}
		body = appendChunk(body, riffChunk{id: f.imageID, data: f.image})
		still = append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
	}
	img, err := webp.Decode(bytes.NewReader(still))
	if err != nil {
		return nil, err
	}
	if b := img.Bounds(); b.Dx() != f.rect.Dx() || b.Dy() != f.rect.Dy() {
		return nil, errors.New("a frame's size doesn't match its header")
	}
	return img, nil
}
