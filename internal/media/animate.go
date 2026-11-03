package media

import (
	"bytes"
	"compress/lzw"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"

	xdraw "golang.org/x/image/draw"
)

// Animated pictures (M15.1): an animated GIF or WebP made into a member's
// picture or banner, by their own service, as a GIF. Cropping or scaling an
// animation means making each of its frames again, which a page's canvas
// can't do, so the page sends the file as it is, with the part of it the
// member framed. Each frame is drawn over those before it, as the format's
// disposal and blending say, cropped and scaled, and given a palette of its
// own, and the GIF keeps the original's timing and looping. The den checks
// it as it checks any GIF.

// What an animation may cost, checked before any of its frames is decoded.
const (
	// MaxAnimationInput is the largest file CropAnimation reads.
	MaxAnimationInput  = 32 << 20
	maxAnimationFrames = 1000
	// maxAnimationCanvas bounds the canvas the frames are drawn on, which
	// is held twice over for a GIF's frames that put it back as it was.
	maxAnimationCanvas = 1 << 22
	// maxAnimationWork bounds the pixels decoded and scaled in all: each
	// frame's own, and the framed part's once for each frame.
	maxAnimationWork = 1 << 28
)

var (
	// ErrAnimationTooLarge is an animation past what making a picture of
	// it may cost, refused before any of its frames is decoded.
	ErrAnimationTooLarge = errors.New("the animation is too large to make a picture of")
	// ErrAnimationTooLong is an animation whose picture would be larger
	// than it may be.
	ErrAnimationTooLong = errors.New("the animation is too long for a picture")
	// ErrNotFramed is a framed part that isn't on the image.
	ErrNotFramed = errors.New("the framed part isn't on the image")
)

// AnimatedPicture is what CropAnimation makes of an animation: the part of
// it the member framed, in its pixels, scaled to Width × Height, in at most
// MaxSize bytes.
type AnimatedPicture struct {
	Crop          image.Rectangle
	Width, Height int
	MaxSize       int
}

// CropAnimation makes an animated GIF or WebP of kind k into a GIF as p
// says. A pixel mostly transparent becomes the GIF's transparent one, and
// the rest keep their color. A still GIF or WebP becomes a GIF of one
// frame. It refuses an animation past its budget before decoding a frame,
// and one whose GIF comes out larger than p.MaxSize as it passes it.
func CropAnimation(ctx context.Context, data []byte, k Kind, p AnimatedPicture) ([]byte, error) {
	if len(data) > MaxAnimationInput {
		return nil, ErrAnimationTooLarge
	}
	if p.Width < 1 || p.Height < 1 || p.Width*p.Height > ThumbSize*ThumbSize {
		return nil, fmt.Errorf("a picture of %d × %d", p.Width, p.Height)
	}
	a, err := readAnimation(data, k, maxAnimationFrames)
	if err != nil {
		return nil, err
	}
	canvas := image.Rect(0, 0, a.width, a.height)
	if p.Crop.Empty() || !p.Crop.In(canvas) {
		return nil, ErrNotFramed
	}
	// A GIF's frame may run past its canvas, which it's cut to, but is
	// decoded whole.
	work := len(a.frames) * p.Crop.Dx() * p.Crop.Dy()
	for _, f := range a.frames {
		area := f.rect.Dx() * f.rect.Dy()
		if area > maxAnimationCanvas {
			return nil, ErrAnimationTooLarge
		}
		work += area
	}
	if a.width*a.height > maxAnimationCanvas || work > maxAnimationWork {
		return nil, ErrAnimationTooLarge
	}

	c := image.NewRGBA(canvas)
	var saved, pix []byte
	s := newCropScaler(p.Crop, p.Width, p.Height)
	w := newGIFWriter(p.Width, p.Height, a.plays, p.MaxSize)
	for i := range a.frames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f := &a.frames[i]
		r := f.rect.Intersect(canvas)
		if f.dispose == restoreFrame {
			saved = copyRect(saved[:0], c, r)
		}
		switch a.kind {
		case GIF:
			n := f.rect.Dx() * f.rect.Dy()
			if cap(pix) < n {
				pix = make([]byte, n)
			}
			drawGIF(c, f, pix[:n], f.gifIndexes(pix[:n]))
		case WebP:
			img, err := decodeWebPFrame(f)
			if err != nil {
				return nil, malformed(WebP, "%v", err)
			}
			drawWebP(c, f.rect.Min, img, f.replace)
		}
		if err := w.add(s.scale(c), f.ms); err != nil {
			return nil, err
		}
		switch f.dispose {
		case clearFrame:
			for y := r.Min.Y; y < r.Max.Y; y++ {
				clear(c.Pix[c.PixOffset(r.Min.X, y):c.PixOffset(r.Max.X, y)])
			}
		case restoreFrame:
			for y, n := r.Min.Y, 4*r.Dx(); y < r.Max.Y; y++ {
				copy(c.Pix[c.PixOffset(r.Min.X, y):], saved[(y-r.Min.Y)*n:][:n])
			}
		}
	}
	return w.finish()
}

// decodeWebPFrame decodes a WebP frame. The decoder is another's code
// reading a member's file, so a panic in it refuses the file.
func decodeWebPFrame(f *animFrame) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			img, err = nil, fmt.Errorf("the decoder failed: %v", r)
		}
	}()
	return f.webpImage()
}

// copyRect appends the canvas's pixels in r, row by row.
func copyRect(dst []byte, c *image.RGBA, r image.Rectangle) []byte {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dst = append(dst, c.Pix[c.PixOffset(r.Min.X, y):c.PixOffset(r.Max.X, y)]...)
	}
	return dst
}

// drawGIF draws a GIF frame on the canvas from its color indexes, of which
// the first n decoded: those past them, an index out of its color table,
// and its transparent index leave the canvas as it was.
func drawGIF(c *image.RGBA, f *animFrame, pix []byte, n int) {
	var colors [256][4]byte
	var has [256]bool
	for i := 0; i < len(f.colors)/3 && i < 256; i++ {
		if i != f.transparent {
			colors[i] = [4]byte{f.colors[3*i], f.colors[3*i+1], f.colors[3*i+2], 0xFF}
			has[i] = true
		}
	}
	w, h := f.rect.Dx(), f.rect.Dy()
	r := f.rect.Intersect(c.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := y - f.rect.Min.Y
		if f.interlaced {
			row = gifRow(row, h)
		}
		from := row*w + r.Min.X - f.rect.Min.X
		to := min(row*w+r.Max.X-f.rect.Min.X, n)
		out := c.Pix[c.PixOffset(r.Min.X, y):]
		for i := from; i < to; i++ {
			if x := pix[i]; has[x] {
				copy(out[4*(i-from):], colors[x][:])
			}
		}
	}
}

// drawWebP draws a WebP frame on the canvas at its place: over what's
// there, or in its place where the frame doesn't blend.
func drawWebP(c *image.RGBA, at image.Point, img image.Image, replace bool) {
	b := img.Bounds()
	r := image.Rectangle{at, at.Add(b.Size())}.Intersect(c.Bounds())
	row := make([]byte, 4*r.Dx())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		premultipliedRow(row, img, b.Min.X+r.Min.X-at.X, b.Min.Y+y-at.Y)
		out := c.Pix[c.PixOffset(r.Min.X, y):c.PixOffset(r.Max.X, y)]
		if replace {
			copy(out, row)
			continue
		}
		for i := 0; i < len(row); i += 4 {
			switch a := uint32(row[i+3]); a {
			case 0:
			case 0xFF:
				copy(out[i:i+4], row[i:i+4])
			default:
				for j := i; j < i+4; j++ {
					out[j] = byte(uint32(row[j]) + (uint32(out[j])*(0xFF-a)+0x7F)/0xFF)
				}
			}
		}
	}
}

// premultipliedRow fills row with an image's pixels from (x, y) on, as
// premultiplied RGBA, the kinds the WebP decoder makes read directly.
func premultipliedRow(row []byte, img image.Image, x, y int) {
	premultiply := func(i int, r, g, b, a uint32) {
		row[i], row[i+1], row[i+2], row[i+3] = byte((r*a+0x7F)/0xFF), byte((g*a+0x7F)/0xFF), byte((b*a+0x7F)/0xFF), byte(a)
	}
	switch m := img.(type) {
	case *image.NRGBA:
		p := m.Pix[m.PixOffset(x, y):]
		for i := 0; i < len(row); i += 4 {
			premultiply(i, uint32(p[i]), uint32(p[i+1]), uint32(p[i+2]), uint32(p[i+3]))
		}
	case *image.YCbCr:
		for i := 0; i < len(row); i += 4 {
			yi, ci := m.YOffset(x+i/4, y), m.COffset(x+i/4, y)
			row[i], row[i+1], row[i+2] = color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
			row[i+3] = 0xFF
		}
	case *image.NYCbCrA:
		for i := 0; i < len(row); i += 4 {
			yi, ci := m.YOffset(x+i/4, y), m.COffset(x+i/4, y)
			r, g, b := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
			premultiply(i, uint32(r), uint32(g), uint32(b), uint32(m.A[m.AOffset(x+i/4, y)]))
		}
	default:
		for i := 0; i < len(row); i += 4 {
			r, g, b, a := img.At(x+i/4, y).RGBA()
			row[i], row[i+1], row[i+2], row[i+3] = byte(r>>8), byte(g>>8), byte(b>>8), byte(a>>8)
		}
	}
}

// cropScaler crops a canvas to the framed part and scales it to the
// picture's size. A part many times the picture's size is first shrunk by
// a whole factor, each square of pixels averaged, which costs a fraction
// of the scaler's filter at that size and leaves it at most about twice
// the picture's.
type cropScaler struct {
	crop   image.Rectangle
	k      int
	shrunk *image.RGBA
	sums   []uint32
	out    *image.RGBA
	scaler xdraw.Scaler
}

func newCropScaler(crop image.Rectangle, w, h int) *cropScaler {
	s := &cropScaler{crop: crop, out: image.NewRGBA(image.Rect(0, 0, w, h))}
	sw, sh := crop.Dx(), crop.Dy()
	if k := min(sw/w, sh/h) / 2; k >= 2 {
		s.k, sw, sh = k, sw/k, sh/k
		s.shrunk, s.sums = image.NewRGBA(image.Rect(0, 0, sw, sh)), make([]uint32, 4*sw)
	}
	if sw != w || sh != h {
		s.scaler = xdraw.CatmullRom.NewScaler(w, h, sw, sh)
	}
	return s
}

func (s *cropScaler) scale(c *image.RGBA) *image.RGBA {
	src, sr := c, s.crop
	if s.shrunk != nil {
		s.shrink(c)
		src, sr = s.shrunk, s.shrunk.Bounds()
	}
	if s.scaler != nil {
		s.scaler.Scale(s.out, s.out.Bounds(), src, sr, xdraw.Src, nil)
		return s.out
	}
	for y := range s.out.Rect.Dy() {
		copy(s.out.Pix[y*s.out.Stride:(y+1)*s.out.Stride], src.Pix[src.PixOffset(sr.Min.X, sr.Min.Y+y):])
	}
	return s.out
}

// shrink averages each k × k square of the framed part into a pixel.
func (s *cropScaler) shrink(c *image.RGBA) {
	k, n := s.k, uint32(s.k*s.k)
	for y := range s.shrunk.Rect.Dy() {
		clear(s.sums)
		for dy := range k {
			p := c.Pix[c.PixOffset(s.crop.Min.X, s.crop.Min.Y+y*k+dy):]
			for i := range len(s.sums) / 4 * k {
				o := 4 * (i / k)
				s.sums[o] += uint32(p[4*i])
				s.sums[o+1] += uint32(p[4*i+1])
				s.sums[o+2] += uint32(p[4*i+2])
				s.sums[o+3] += uint32(p[4*i+3])
			}
		}
		out := s.shrunk.Pix[y*s.shrunk.Stride:]
		for i, sum := range s.sums {
			out[i] = byte((sum + n/2) / n)
		}
	}
}

// holdTolerance is how far, in each channel, a pixel's color may move from
// how it was last drawn before it's drawn again: below the error of a
// palette of 255 colors, and above the noise of scaling and lossy frames.
const holdTolerance = 4

// gifWriter writes a picture's frames as a GIF. Each frame holds only the
// rectangle that changed since those before it, with the pixels in it that
// didn't change transparent, in a palette of its own; a frame that changes
// nothing adds its time to the one before. A GIF of a small part of a busy
// animation, or of one that mostly holds still, is then as large as its
// motion.
type gifWriter struct {
	out           bytes.Buffer
	width, height int
	plays         int
	maxSize       int
	// target is the frame being added, and ref each pixel's color when
	// it was last drawn, before its palette took it: transparent is 0, and
	// an opaque color has bit 24 set.
	target, ref []uint32
	// held is the last frame, which the next may still change: a pixel
	// that turns transparent can only be drawn so by clearing the frame
	// before once its time is up.
	held    *gifFrame
	written int
	q       quantizer
}

// gifFrame is a frame as it's written: its rectangle, color indexes and
// palette, its transparent index or -1, how long it shows, and whether
// it's cleared once its time is up.
type gifFrame struct {
	rect        image.Rectangle
	pix         []byte
	colors      []byte
	transparent int
	ms          int
	clear       bool
}

func newGIFWriter(w, h, plays, maxSize int) *gifWriter {
	return &gifWriter{width: w, height: h, plays: plays, maxSize: maxSize, target: make([]uint32, w*h), ref: make([]uint32, w*h)}
}

// add adds a frame of the picture's size that shows for ms.
func (w *gifWriter) add(img *image.RGBA, ms int) error {
	t := w.target
	for i := range t {
		p := img.Pix[4*i : 4*i+4]
		a := uint32(p[3])
		if a < 0x80 {
			t[i] = 0
			continue
		}
		un := func(v byte) uint32 { return min(0xFF, (uint32(v)*0xFF+a/2)/a) }
		t[i] = 1<<24 | un(p[0])<<16 | un(p[1])<<8 | un(p[2])
	}
	if w.held == nil {
		w.held = w.frame(image.Rect(0, 0, w.width, w.height), ms, func(int) bool { return true })
		return nil
	}

	var gone, moved image.Rectangle
	for y := range w.height {
		for x := range w.width {
			i := y*w.width + x
			switch tc, rc := t[i], w.ref[i]; {
			case tc == 0 && rc != 0:
				gone = gone.Union(image.Rect(x, y, x+1, y+1))
			case tc != 0 && !holds(tc, rc):
				moved = moved.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if !gone.Empty() {
		h := w.held
		h.grow(h.rect.Union(gone))
		h.clear = true
		for y := h.rect.Min.Y; y < h.rect.Max.Y; y++ {
			clear(w.ref[y*w.width+h.rect.Min.X : y*w.width+h.rect.Max.X])
		}
		// What the clearing takes away must be drawn again.
		for y := h.rect.Min.Y; y < h.rect.Max.Y; y++ {
			for x := h.rect.Min.X; x < h.rect.Max.X; x++ {
				if t[y*w.width+x] != 0 {
					moved = moved.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
	}
	if moved.Empty() {
		if !w.held.clear {
			w.held.ms += ms
			return nil
		}
		// A frame of nothing, so the cleared picture shows for its time.
		moved = image.Rect(0, 0, 1, 1)
	}
	if err := w.write(w.held, true); err != nil {
		return err
	}
	w.held = w.frame(moved, ms, func(i int) bool { return !holds(t[i], w.ref[i]) })
	return nil
}

// holds reports whether an opaque color is within holdTolerance of how a
// pixel was last drawn, which then needn't be drawn again.
func holds(c, ref uint32) bool {
	if ref == 0 {
		return false
	}
	for shift := 0; shift < 24; shift += 8 {
		d := int(c>>shift&0xFF) - int(ref>>shift&0xFF)
		if d > holdTolerance || d < -holdTolerance {
			return false
		}
	}
	return true
}

// frame makes a frame of r from the target: the opaque pixels draw takes,
// in a palette made of them, and the rest transparent.
func (w *gifWriter) frame(r image.Rectangle, ms int, draw func(i int) bool) *gifFrame {
	t := w.target
	w.q.reset()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if i := y*w.width + x; t[i] != 0 && draw(i) {
				w.q.add(t[i])
			}
		}
	}
	f := &gifFrame{rect: r, pix: make([]byte, 0, r.Dx()*r.Dy()), colors: w.q.palette(), transparent: -1, ms: ms}
	none := byte(len(f.colors) / 3)
	// A frame of more colors than its palette is dithered, each pixel's
	// error spread to the pixels drawn after it, as Floyd and Steinberg
	// did, so a gradient doesn't band.
	var errs [2][][3]int
	if w.q.cut {
		errs = [2][][3]int{make([][3]int, r.Dx()+2), make([][3]int, r.Dx()+2)}
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := y*w.width + x
			if t[i] == 0 || !draw(i) {
				f.pix = append(f.pix, none)
				f.transparent = int(none)
				continue
			}
			w.ref[i] = t[i]
			if !w.q.cut {
				f.pix = append(f.pix, w.q.index(t[i]))
				continue
			}
			e, at := errs[0], x-r.Min.X+1
			var want [3]int
			for ch := range 3 {
				want[ch] = min(0xFF, max(0, int(t[i]>>(16-8*ch)&0xFF)+e[at][ch]/16))
			}
			k := w.q.near(want[0], want[1], want[2])
			f.pix = append(f.pix, k)
			for ch := range 3 {
				d := want[ch] - int(f.colors[3*int(k)+ch])
				e[at+1][ch] += 7 * d
				errs[1][at-1][ch] += 3 * d
				errs[1][at][ch] += 5 * d
				errs[1][at+1][ch] += d
			}
		}
		if w.q.cut {
			errs[0], errs[1] = errs[1], errs[0]
			clear(errs[1])
		}
	}
	if f.transparent >= 0 {
		f.colors = append(f.colors, 0, 0, 0)
	}
	return f
}

// grow widens a frame to r, the pixels it gains transparent.
func (f *gifFrame) grow(r image.Rectangle) {
	if r == f.rect {
		return
	}
	if f.transparent < 0 {
		f.transparent = len(f.colors) / 3
		f.colors = append(f.colors, 0, 0, 0)
	}
	pix := bytes.Repeat([]byte{byte(f.transparent)}, r.Dx()*r.Dy())
	w := f.rect.Dx()
	for y := f.rect.Min.Y; y < f.rect.Max.Y; y++ {
		o := (y-r.Min.Y)*r.Dx() + f.rect.Min.X - r.Min.X
		copy(pix[o:o+w], f.pix[(y-f.rect.Min.Y)*w:])
	}
	f.rect, f.pix = r, pix
}

// write writes a frame; more says frames follow it, which makes the GIF an
// animation.
func (w *gifWriter) write(f *gifFrame, more bool) error {
	b := &w.out
	if w.written == 0 {
		b.WriteString("GIF89a")
		b.Write(binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, uint16(w.width)), uint16(w.height)))
		b.Write([]byte{0, 0, 0}) // no global color table
		if more && w.plays != 1 {
			loops := 0
			if w.plays > 1 {
				loops = min(w.plays-1, 0xFFFF)
			}
			b.Write([]byte{0x21, 0xFF, 11})
			b.WriteString("NETSCAPE2.0")
			b.Write([]byte{3, 1, byte(loops), byte(loops >> 8), 0})
		}
	}
	packed := byte(1 << 2) // left as it's drawn
	if f.clear {
		packed = 2 << 2
	}
	tidx := byte(0)
	if f.transparent >= 0 {
		packed |= 1
		tidx = byte(f.transparent)
	}
	cs := min(max(2, (f.ms+5)/10), 0xFFFF)
	b.Write([]byte{0x21, 0xF9, 4, packed, byte(cs), byte(cs >> 8), tidx, 0})

	bits := 1
	for 1<<bits < len(f.colors)/3 {
		bits++
	}
	b.WriteByte(0x2C)
	for _, v := range []int{f.rect.Min.X, f.rect.Min.Y, f.rect.Dx(), f.rect.Dy()} {
		b.Write(binary.LittleEndian.AppendUint16(nil, uint16(v)))
	}
	b.WriteByte(0x80 | byte(bits-1))
	b.Write(f.colors)
	b.Write(make([]byte, 3<<bits-len(f.colors)))
	b.WriteByte(byte(max(2, bits)))
	bw := &blockWriter{out: b}
	lw := lzw.NewWriter(bw, lzw.LSB, max(2, bits))
	if _, err := lw.Write(f.pix); err != nil {
		return err
	}
	if err := lw.Close(); err != nil {
		return err
	}
	bw.close()
	w.written++
	if b.Len() > w.maxSize {
		return ErrAnimationTooLong
	}
	return nil
}

// finish writes the last frame and the trailer, and returns the GIF.
func (w *gifWriter) finish() ([]byte, error) {
	if err := w.write(w.held, false); err != nil {
		return nil, err
	}
	w.out.WriteByte(0x3B)
	if w.out.Len() > w.maxSize {
		return nil, ErrAnimationTooLong
	}
	return w.out.Bytes(), nil
}

// blockWriter writes data as GIF sub-blocks of up to 255 bytes.
type blockWriter struct {
	out *bytes.Buffer
	buf [256]byte
	n   int
}

func (b *blockWriter) Write(p []byte) (int, error) {
	for _, c := range p {
		b.n++
		b.buf[b.n] = c
		if b.n == 0xFF {
			b.flush()
		}
	}
	return len(p), nil
}

func (b *blockWriter) flush() {
	if b.n > 0 {
		b.buf[0] = byte(b.n)
		b.out.Write(b.buf[:b.n+1])
		b.n = 0
	}
}

// close writes what's left and the terminator.
func (b *blockWriter) close() {
	b.flush()
	b.out.WriteByte(0)
}
