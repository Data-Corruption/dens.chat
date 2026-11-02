package media

import (
	"bytes"
	"compress/lzw"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"math"
	"math/bits"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// bitWriter writes bits from each byte's lowest up, as VP8L reads them.
type bitWriter struct {
	buf []byte
	acc uint64
	n   uint
}

func (w *bitWriter) write(v uint32, n uint) {
	w.acc |= uint64(v) << w.n
	for w.n += n; w.n >= 8; w.n -= 8 {
		w.buf = append(w.buf, byte(w.acc))
		w.acc >>= 8
	}
}

func (w *bitWriter) bytes() []byte {
	if w.n > 0 {
		w.buf = append(w.buf, byte(w.acc))
	}
	return w.buf
}

// vp8l encodes an image losslessly, as plainly as VP8L allows: no
// transforms or cache, and each channel's value in eight bits of its own,
// so a test can make any frame it likes.
func vp8l(img *image.NRGBA) []byte {
	b := img.Bounds()
	w := &bitWriter{}
	w.write(0x2F, 8)
	w.write(uint32(b.Dx()-1), 14)
	w.write(uint32(b.Dy()-1), 14)
	w.write(1, 1) // alpha
	w.write(0, 3) // version
	w.write(0, 3) // no transform, cache or meta codes
	// Green, red, blue and alpha: every literal eight bits long, written
	// by a code of lengths in which 0 is "0" and 8 is "1"; green's alphabet
	// also has the 24 lengths of back references, never used.
	for _, size := range []int{280, 256, 256, 256} {
		w.write(0, 1)
		w.write(12-4, 4)
		for _, sym := range []int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8} {
			if sym == 0 || sym == 8 {
				w.write(1, 3)
			} else {
				w.write(0, 3)
			}
		}
		w.write(0, 1)
		for s := range size {
			if s < 256 {
				w.write(1, 1)
			} else {
				w.write(0, 1)
			}
		}
	}
	w.write(0b0001, 4) // distances: a simple code of one symbol
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			for _, v := range []uint8{c.G, c.R, c.B, c.A} {
				w.write(uint32(bits.Reverse8(v)), 8)
			}
		}
	}
	return w.bytes()
}

// webpFrameSpec is a frame of a test's animated WebP.
type webpFrameSpec struct {
	img          *image.NRGBA
	x, y, ms     int
	blend, clear bool
}

func animatedWebP(w, h, loops int, frames ...webpFrameSpec) []byte {
	vp8x := make([]byte, 10)
	vp8x[0] = webpAnimation | webpAlpha
	put24(vp8x[4:], w-1)
	put24(vp8x[7:], h-1)
	chunks := []riffChunk{{id: "VP8X", data: vp8x}, {id: "ANIM", data: []byte{0, 0, 0, 0, byte(loops), byte(loops >> 8)}}}
	for _, f := range frames {
		d := make([]byte, 16)
		put24(d, f.x/2)
		put24(d[3:], f.y/2)
		put24(d[6:], f.img.Bounds().Dx()-1)
		put24(d[9:], f.img.Bounds().Dy()-1)
		put24(d[12:], f.ms)
		if !f.blend {
			d[15] |= 2
		}
		if f.clear {
			d[15] |= 1
		}
		chunks = append(chunks, riffChunk{id: "ANMF", data: appendChunk(d, riffChunk{id: "VP8L", data: vp8l(f.img)})})
	}
	return riffFile(chunks...)
}

func filled(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

// showWebP composes a test's WebP frames as the format's specification
// says, in colors that aren't premultiplied, and returns each frame's
// picture.
func showWebP(w, h int, frames ...webpFrameSpec) []*image.NRGBA {
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	var out []*image.NRGBA
	for _, f := range frames {
		b := f.img.Bounds()
		for y := range b.Dy() {
			for x := range b.Dx() {
				src, dst := f.img.NRGBAAt(x, y), canvas.NRGBAAt(f.x+x, f.y+y)
				if f.blend {
					sa, da := float64(src.A)/255, float64(dst.A)/255
					a := sa + da*(1-sa)
					mix := func(s, d uint8) uint8 {
						if a == 0 {
							return 0
						}
						return uint8((float64(s)*sa+float64(d)*da*(1-sa))/a + 0.5)
					}
					src = color.NRGBA{mix(src.R, dst.R), mix(src.G, dst.G), mix(src.B, dst.B), uint8(a*255 + 0.5)}
				}
				canvas.SetNRGBA(f.x+x, f.y+y, src)
			}
		}
		out = append(out, image.NewNRGBA(canvas.Rect))
		copy(out[len(out)-1].Pix, canvas.Pix)
		if f.clear {
			draw.Draw(canvas, b.Add(image.Pt(f.x, f.y)), image.Transparent, image.Point{}, draw.Src)
		}
	}
	return out
}

// showGIF composes a GIF's frames as browsers show them, and returns the
// GIF and each frame's picture.
func showGIF(t *testing.T, data []byte) (*gif.GIF, []*image.RGBA) {
	t.Helper()
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding the GIF: %v", err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	var out []*image.RGBA
	for i, m := range g.Image {
		saved := image.NewRGBA(canvas.Rect)
		copy(saved.Pix, canvas.Pix)
		draw.Draw(canvas, m.Bounds(), m, m.Bounds().Min, draw.Over)
		shown := image.NewRGBA(canvas.Rect)
		copy(shown.Pix, canvas.Pix)
		out = append(out, shown)
		switch g.Disposal[i] {
		case gif.DisposalBackground:
			draw.Draw(canvas, m.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = saved
		}
	}
	return g, out
}

// samePicture checks a picture against what it should show, a pixel mostly
// transparent being transparent, and the rest within tol in each channel.
func samePicture(t *testing.T, what string, got *image.RGBA, want image.Image, tol int) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("%s: %v, not %v", what, got.Bounds(), want.Bounds())
	}
	bad := 0
	for y := range got.Bounds().Dy() {
		for x := range got.Bounds().Dx() {
			g := got.RGBAAt(x, y)
			w := color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
			ok := g.A == 0
			if w.A >= 0x80 {
				d := func(a, b uint8) bool { return int(a)-int(b) <= tol && int(b)-int(a) <= tol }
				ok = g.A == 0xFF && d(g.R, w.R) && d(g.G, w.G) && d(g.B, w.B)
			}
			if !ok {
				if bad++; bad <= 3 {
					t.Errorf("%s at %d,%d: %v, not %v", what, x, y, g, w)
				}
			}
		}
	}
}

func crop(t *testing.T, data []byte, k Kind, p AnimatedPicture) []byte {
	t.Helper()
	if p.MaxSize == 0 {
		p.MaxSize = 1 << 20
	}
	out, err := CropAnimation(context.Background(), data, k, p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func paletted(r image.Rectangle, c ...color.Color) *image.Paletted {
	return image.NewPaletted(r, c)
}

func TestCropAnimationGIFDisposal(t *testing.T) {
	red, blue, green, yellow := color.RGBA{0xFF, 0, 0, 0xFF}, color.RGBA{0, 0, 0xFF, 0xFF}, color.RGBA{0, 0x80, 0, 0xFF}, color.RGBA{0xFF, 0xFF, 0, 0xFF}
	f3 := paletted(image.Rect(0, 0, 2, 2), yellow, color.RGBA{})
	f3.SetColorIndex(1, 1, 1)
	src := &gif.GIF{
		Image:     []*image.Paletted{paletted(image.Rect(0, 0, 16, 16), red), paletted(image.Rect(2, 2, 6, 6), blue), paletted(image.Rect(8, 8, 12, 12), green), f3},
		Delay:     []int{10, 20, 30, 40},
		Disposal:  []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious, gif.DisposalNone},
		LoopCount: 2,
		Config:    image.Config{Width: 16, Height: 16},
	}
	var in bytes.Buffer
	if err := gif.EncodeAll(&in, src); err != nil {
		t.Fatal(err)
	}
	out := crop(t, in.Bytes(), GIF, AnimatedPicture{Crop: image.Rect(0, 0, 16, 16), Width: 16, Height: 16})
	g, got := showGIF(t, out)
	_, want := showGIF(t, in.Bytes())
	if len(got) != 4 || g.LoopCount != 2 || !equalInts(g.Delay, []int{10, 20, 30, 40}) {
		t.Fatalf("%d frames, %v, looping %d", len(got), g.Delay, g.LoopCount)
	}
	for i := range got {
		samePicture(t, "frame", got[i], want[i], 0)
	}
	// The blue square is cleared to transparent, and the green one goes,
	// leaving the hole the blue left.
	if c := want[3].RGBAAt(3, 3); c.A != 0 {
		t.Fatalf("the reference didn't clear the frame: %v", c)
	}
	if c := got[3].RGBAAt(9, 9); c != red {
		t.Fatalf("the frame put back: %v", c)
	}
	if c := got[3].RGBAAt(1, 1); c != red {
		t.Fatalf("under a transparent pixel: %v", c)
	}
	res, err := Verify(new(bytes.Buffer), bytes.NewReader(out), GIF)
	if err != nil || !res.Animated || res.Width != 16 || res.Height != 16 {
		t.Fatalf("the den's check: %+v %v", res, err)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCropAnimationWebPBlending(t *testing.T) {
	green := filled(8, 8, color.NRGBA{0, 0xC0, 0, 0xFF})
	draw.Draw(green, image.Rect(0, 0, 4, 4), image.Transparent, image.Point{}, draw.Src)
	frames := []webpFrameSpec{
		{img: filled(16, 16, color.NRGBA{0, 0, 0xFF, 0xFF}), ms: 100},
		// Half see-through, over the blue, then cleared.
		{img: filled(8, 8, color.NRGBA{0xFF, 0, 0, 0x80}), x: 4, y: 4, ms: 200, blend: true, clear: true},
		// Not blended: its transparent corner takes the blue away.
		{img: green, ms: 300},
		{img: filled(8, 8, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}), x: 8, y: 8, ms: 400, blend: true},
	}
	in := animatedWebP(16, 16, 0, frames...)
	out := crop(t, in, WebP, AnimatedPicture{Crop: image.Rect(0, 0, 16, 16), Width: 16, Height: 16})
	g, got := showGIF(t, out)
	want := showWebP(16, 16, frames...)
	if len(got) != 4 || g.LoopCount != 0 || !equalInts(g.Delay, []int{10, 20, 30, 40}) {
		t.Fatalf("%d frames, %v, looping %d", len(got), g.Delay, g.LoopCount)
	}
	for i := range got {
		samePicture(t, "frame", got[i], want[i], 1)
	}
	if c := got[1].RGBAAt(6, 6); c.R < 0x7E || c.R > 0x81 || c.B < 0x7E || c.B > 0x81 || c.A != 0xFF {
		t.Fatalf("red half over blue: %v", c)
	}
	if c := got[2].RGBAAt(1, 1); c.A != 0 {
		t.Fatalf("a frame that doesn't blend left the blue: %v", c)
	}
	if c := got[2].RGBAAt(10, 10); c.A != 0 {
		t.Fatalf("the cleared frame: %v", c)
	}
}

func TestCropAnimationCropsAndScales(t *testing.T) {
	quarters := []color.Color{color.RGBA{0xE0, 0x20, 0x20, 0xFF}, color.RGBA{0x20, 0xE0, 0x20, 0xFF},
		color.RGBA{0x20, 0x20, 0xE0, 0xFF}, color.RGBA{0xE0, 0xE0, 0x20, 0xFF}, color.RGBA{0, 0, 0, 0xFF}}
	bg := paletted(image.Rect(0, 0, 128, 128), quarters...)
	for y := range 128 {
		for x := range 128 {
			bg.SetColorIndex(x, y, uint8(x/64+2*(y/64)))
		}
	}
	src := &gif.GIF{Image: []*image.Paletted{bg}, Delay: []int{10}, Disposal: []byte{gif.DisposalNone}}
	for i := range 3 {
		sq := paletted(image.Rect(8+8*i, 16, 16+8*i, 24), quarters...)
		for j := range sq.Pix {
			sq.Pix[j] = 4
		}
		src.Image, src.Delay, src.Disposal = append(src.Image, sq), append(src.Delay, 10), append(src.Disposal, gif.DisposalPrevious)
	}
	var in bytes.Buffer
	if err := gif.EncodeAll(&in, src); err != nil {
		t.Fatal(err)
	}
	_, frames := showGIF(t, in.Bytes())

	// Half the top left quarter's sides, scaled by the scaler alone.
	part := image.Rect(0, 0, 64, 64)
	out := crop(t, in.Bytes(), GIF, AnimatedPicture{Crop: part, Width: 32, Height: 32})
	_, got := showGIF(t, out)
	if len(got) != len(frames) {
		t.Fatalf("%d frames", len(got))
	}
	for i, f := range frames {
		want := image.NewRGBA(image.Rect(0, 0, 32, 32))
		xdraw.CatmullRom.Scale(want, want.Bounds(), f, part, xdraw.Src, nil)
		samePicture(t, "scaled", got[i], want, holdTolerance+1)
	}

	// The whole of it at an eighth, shrunk first: each quarter shows its
	// color away from the edges, and the square moves.
	out = crop(t, in.Bytes(), GIF, AnimatedPicture{Crop: image.Rect(0, 0, 128, 128), Width: 16, Height: 16})
	_, got = showGIF(t, out)
	for i, at := range [][2]int{{3, 3}, {12, 3}, {3, 12}, {12, 12}} {
		want := color.RGBAModel.Convert(quarters[i]).(color.RGBA)
		if c := got[0].RGBAAt(at[0], at[1]); c != want {
			t.Errorf("quarter %d: %v, not %v", i, c, want)
		}
	}
	if len(got) != 4 || got[1].RGBAAt(1, 2) == got[3].RGBAAt(1, 2) {
		t.Fatalf("the square doesn't move: %d frames", len(got))
	}

	// A part off the image isn't framed.
	_, err := CropAnimation(context.Background(), in.Bytes(), GIF, AnimatedPicture{Crop: image.Rect(100, 100, 200, 200), Width: 16, Height: 16, MaxSize: 1 << 20})
	if !errors.Is(err, ErrNotFramed) {
		t.Fatalf("a part off the image: %v", err)
	}
}

func TestCropAnimationTiming(t *testing.T) {
	still := paletted(image.Rect(0, 0, 8, 8), color.RGBA{0xFF, 0, 0, 0xFF}, color.RGBA{0, 0, 0xFF, 0xFF})
	outside := paletted(image.Rect(12, 12, 16, 16), color.RGBA{0, 0xFF, 0, 0xFF})
	moved := paletted(image.Rect(0, 0, 4, 4), color.RGBA{0, 0, 0xFF, 0xFF})
	for _, c := range []struct {
		name   string
		delays []int
		loops  int
		frames []*image.Paletted
		want   []int
	}{
		// Browsers show 0 and 1 for 10, and a frame that changes nothing
		// the picture shows adds its time to the frame before.
		{"fast", []int{0, 1, 5, 20}, -1, []*image.Paletted{still, moved, outside, still}, []int{10, 15, 20}},
		{"forever", []int{10, 10}, 0, []*image.Paletted{still, moved}, []int{10, 10}},
		{"three times", []int{10, 10}, 2, []*image.Paletted{still, moved}, []int{10, 10}},
	} {
		src := &gif.GIF{Image: c.frames, Delay: c.delays, Disposal: make([]byte, len(c.frames)), LoopCount: c.loops,
			Config: image.Config{Width: 16, Height: 16}}
		var in bytes.Buffer
		if err := gif.EncodeAll(&in, src); err != nil {
			t.Fatal(err)
		}
		g, _ := showGIF(t, crop(t, in.Bytes(), GIF, AnimatedPicture{Crop: image.Rect(0, 0, 8, 8), Width: 8, Height: 8}))
		if !equalInts(g.Delay, c.want) || g.LoopCount != c.loops {
			t.Errorf("%s: %v, looping %d", c.name, g.Delay, g.LoopCount)
		}
	}

	// A WebP's plays, and times in milliseconds.
	red, blue := filled(8, 8, color.NRGBA{0xFF, 0, 0, 0xFF}), filled(8, 8, color.NRGBA{0, 0, 0xFF, 0xFF})
	for _, c := range []struct{ plays, loops int }{{0, 0}, {1, -1}, {3, 2}} {
		in := animatedWebP(8, 8, c.plays, webpFrameSpec{img: red, ms: 15}, webpFrameSpec{img: blue, ms: 3}, webpFrameSpec{img: red, ms: 1234})
		g, _ := showGIF(t, crop(t, in, WebP, AnimatedPicture{Crop: image.Rect(0, 0, 8, 8), Width: 8, Height: 8}))
		if !equalInts(g.Delay, []int{2, 10, 123}) || g.LoopCount != c.loops {
			t.Errorf("playing %d times: %v, looping %d", c.plays, g.Delay, g.LoopCount)
		}
	}
}

func TestCropAnimationPalettes(t *testing.T) {
	// A frame of 1,024 colors gets 255 of them, which stay near.
	rainbow := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			rainbow.SetNRGBA(x, y, color.NRGBA{uint8(8 * x), uint8(8 * y), uint8(4*x + 4*y), 0xFF})
		}
	}
	dot := filled(4, 4, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	frames := []webpFrameSpec{{img: rainbow, ms: 100}, {img: dot, x: 12, y: 12, ms: 100, blend: true}}
	out := crop(t, animatedWebP(32, 32, 0, frames...), WebP, AnimatedPicture{Crop: image.Rect(0, 0, 32, 32), Width: 32, Height: 32})
	g, got := showGIF(t, out)
	want := showWebP(32, 32, frames...)
	if len(g.Image) != 2 || len(g.Image[0].Palette) > 256 {
		t.Fatalf("%d frames, the first of %d colors", len(g.Image), len(g.Image[0].Palette))
	}
	for i := range got {
		samePicture(t, "quantized", got[i], want[i], 16)
	}
	// The second frame draws its dot alone.
	if b := g.Image[1].Bounds(); b != image.Rect(12, 12, 16, 16) {
		t.Fatalf("the second frame covers %v", b)
	}

	// A gradient of more colors than a palette holds is dithered, so each
	// patch of it keeps its color on the whole, where bands would shift it.
	gradient := image.NewNRGBA(image.Rect(0, 0, 480, 160))
	for y := range 160 {
		for x := range 480 {
			gradient.SetNRGBA(x, y, color.NRGBA{uint8(x / 2), uint8(y + 40), 0x80, 0xFF})
		}
	}
	out = crop(t, animatedWebP(480, 160, 0, webpFrameSpec{img: gradient, ms: 100}), WebP, AnimatedPicture{Crop: gradient.Rect, Width: 480, Height: 160})
	_, got = showGIF(t, out)
	var off float64
	for y := 0; y < 160; y += 8 {
		for x := 0; x < 480; x += 8 {
			for ch := range 3 {
				sum := 0
				for i := range 64 {
					p, want := got[0].RGBAAt(x+i%8, y+i/8), gradient.NRGBAAt(x+i%8, y+i/8)
					sum += int([3]uint8{p.R, p.G, p.B}[ch]) - int([3]uint8{want.R, want.G, want.B}[ch])
				}
				off += math.Abs(float64(sum) / 64)
			}
		}
	}
	if mean := off / (60 * 20 * 3); mean > 1 {
		t.Fatalf("a gradient's patches are %.2f off on average", mean)
	}

	// A pixel mostly transparent is transparent, and one mostly opaque
	// keeps its color.
	edge := filled(8, 8, color.NRGBA{0x40, 0x80, 0xC0, 0xC8})
	draw.Draw(edge, image.Rect(0, 0, 4, 8), image.NewUniform(color.NRGBA{0x40, 0x80, 0xC0, 0x64}), image.Point{}, draw.Src)
	out = crop(t, animatedWebP(8, 8, 0, webpFrameSpec{img: edge, ms: 100}), WebP, AnimatedPicture{Crop: image.Rect(0, 0, 8, 8), Width: 8, Height: 8})
	_, got = showGIF(t, out)
	if c := got[0].RGBAAt(1, 1); c.A != 0 {
		t.Fatalf("mostly transparent: %v", c)
	}
	if c := got[0].RGBAAt(6, 6); c.A != 0xFF || c.R < 0x3F || c.R > 0x41 || c.B < 0xBF || c.B > 0xC1 {
		t.Fatalf("mostly opaque: %v", c)
	}
}

// rawFrame is a GIF frame as a test writes it by hand, for what image/gif
// doesn't write: interlaced rows, and color indexes past the frame's table,
// which holds two colors or four.
type rawFrame struct {
	rect       image.Rectangle
	colors     []byte
	pix        []byte // in the order the rows show
	interlaced bool
}

func rawGIF(w, h int, frames ...rawFrame) []byte {
	var b bytes.Buffer
	b.WriteString("GIF89a")
	b.Write(binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, uint16(w)), uint16(h)))
	b.Write([]byte{0, 0, 0})
	for _, f := range frames {
		b.Write([]byte{0x21, 0xF9, 4, 1 << 2, 10, 0, 0, 0})
		b.WriteByte(0x2C)
		for _, v := range []int{f.rect.Min.X, f.rect.Min.Y, f.rect.Dx(), f.rect.Dy()} {
			b.Write(binary.LittleEndian.AppendUint16(nil, uint16(v)))
		}
		flags, size := byte(0x80), 6
		if len(f.colors) > 6 {
			flags, size = 0x81, 12
		}
		if f.interlaced {
			flags |= 0x40
		}
		b.WriteByte(flags)
		b.Write(append(f.colors, make([]byte, size-len(f.colors))...))
		pix := f.pix
		if f.interlaced {
			fw, fh := f.rect.Dx(), f.rect.Dy()
			pix = make([]byte, len(f.pix))
			for r := range fh {
				copy(pix[gifRow(r, fh)*fw:], f.pix[r*fw:(r+1)*fw])
			}
		}
		b.WriteByte(2)
		bw := &blockWriter{out: &b}
		lw := lzw.NewWriter(bw, lzw.LSB, 2)
		lw.Write(pix)
		lw.Close()
		bw.close()
	}
	b.WriteByte(0x3B)
	return b.Bytes()
}

func TestCropAnimationGIFQuirks(t *testing.T) {
	// Rows 0 to 7 of an interlaced frame, each its own color: black on
	// the even rows, white on the odd ones.
	colors := []byte{0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0}
	rows := make([]byte, 4*8)
	for r := range 8 {
		for x := range 4 {
			rows[4*r+x] = byte(r % 2)
		}
	}
	// Then red, but at an index past a table of two.
	reds := bytes.Repeat([]byte{1}, 16)
	reds[0] = 3
	in := rawGIF(4, 8, rawFrame{rect: image.Rect(0, 0, 4, 8), colors: colors, pix: rows, interlaced: true},
		rawFrame{rect: image.Rect(0, 0, 4, 4), colors: []byte{0xFF, 0xFF, 0xFF, 0xFF, 0, 0}, pix: reds})
	_, got := showGIF(t, crop(t, in, GIF, AnimatedPicture{Crop: image.Rect(0, 0, 4, 8), Width: 4, Height: 8}))
	for r := range 8 {
		if c := got[0].RGBAAt(0, r); c.R != byte(0xFF*(r%2)) {
			t.Fatalf("interlaced row %d: %v", r, c)
		}
	}
	// An index past the frame's colors leaves the pixel as it was.
	if c, d := got[1].RGBAAt(0, 0), got[1].RGBAAt(1, 0); c.R != 0 || c.A != 0xFF || d.R != 0xFF || d.G != 0 {
		t.Fatalf("past the colors %v, red %v", c, d)
	}

	// A GIF cut off in its last frame keeps the frames before, as browsers
	// show it, and one cut off before any is damaged.
	cut := in[:len(in)-6]
	_, got = showGIF(t, crop(t, cut, GIF, AnimatedPicture{Crop: image.Rect(0, 0, 4, 8), Width: 4, Height: 8}))
	if len(got) != 1 {
		t.Fatalf("%d frames of a GIF cut short", len(got))
	}
	if _, err := CropAnimation(context.Background(), in[:40], GIF, AnimatedPicture{Crop: image.Rect(0, 0, 4, 8), Width: 4, Height: 8, MaxSize: 1 << 20}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("no whole frame: %v", err)
	}
}

func TestCropAnimationStills(t *testing.T) {
	var in bytes.Buffer
	gif.Encode(&in, paletted(image.Rect(0, 0, 8, 8), color.RGBA{0xFF, 0, 0, 0xFF}), nil)
	still := riffFile(riffChunk{id: "VP8L", data: vp8l(filled(8, 8, color.NRGBA{0, 0xFF, 0, 0xFF}))})
	for name, c := range map[string]struct {
		data []byte
		k    Kind
		want color.RGBA
	}{"gif": {in.Bytes(), GIF, color.RGBA{0xFF, 0, 0, 0xFF}}, "webp": {still, WebP, color.RGBA{0, 0xFF, 0, 0xFF}}} {
		out := crop(t, c.data, c.k, AnimatedPicture{Crop: image.Rect(0, 0, 8, 8), Width: 8, Height: 8})
		g, got := showGIF(t, out)
		res, err := Verify(new(bytes.Buffer), bytes.NewReader(out), GIF)
		if err != nil || res.Animated || len(got) != 1 || g.LoopCount != -1 || got[0].RGBAAt(4, 4) != c.want {
			t.Errorf("%s: %+v %v, %d frames looping %d", name, res, err, len(got), g.LoopCount)
		}
	}
}

func TestCropAnimationWebPFixtures(t *testing.T) {
	// ffmpeg's libwebp made these: a lossy animation whose second frame
	// blends a corner in, and a lossy still with alpha, here a frame.
	anim := readFixture(t, "anim.webp")
	g, got := showGIF(t, crop(t, anim, WebP, AnimatedPicture{Crop: image.Rect(0, 0, 32, 24), Width: 32, Height: 24}))
	if len(got) != 2 || !equalInts(g.Delay, []int{40, 20}) || g.LoopCount != 0 {
		t.Fatalf("%d frames, %v, looping %d", len(got), g.Delay, g.LoopCount)
	}
	first, err := decodeWebP(bytes.NewReader(anim), Result{Width: 32, Height: 24, Animated: true})
	if err != nil {
		t.Fatal(err)
	}
	samePicture(t, "the first frame", got[0], first, 24)

	alpha := readFixture(t, "alpha.webp")
	chunks, err := riffChunks(alpha[12:])
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 16)
	put24(frame[6:], 31)
	put24(frame[9:], 23)
	put24(frame[12:], 100)
	for _, c := range chunks[1:] { // ALPH and VP8
		frame = appendChunk(frame, c)
	}
	vp8x := append([]byte{webpAnimation | webpAlpha}, chunks[0].data[1:]...)
	in := riffFile(riffChunk{id: "VP8X", data: vp8x}, riffChunk{id: "ANIM", data: make([]byte, 6)}, riffChunk{id: "ANMF", data: frame})
	_, got = showGIF(t, crop(t, in, WebP, AnimatedPicture{Crop: image.Rect(0, 0, 32, 24), Width: 32, Height: 24}))
	stillImg, err := decodeWebP(bytes.NewReader(alpha), Result{})
	if err != nil {
		t.Fatal(err)
	}
	samePicture(t, "a lossy frame with alpha", got[0], stillImg, 24)
}

func TestCropAnimationBudget(t *testing.T) {
	ctx := context.Background()
	p := AnimatedPicture{Crop: image.Rect(0, 0, 1, 1), Width: 1, Height: 1, MaxSize: 1 << 20}
	// Frames are counted, sized and weighed before any is decoded: these
	// frames' data would decode to nothing.
	frames := func(w, h, n, fw, fh int) []byte {
		var b bytes.Buffer
		b.WriteString("GIF89a")
		b.Write(binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, uint16(w)), uint16(h)))
		b.Write([]byte{0x80, 0, 0, 0, 0, 0, 0xFF, 0xFF, 0xFF})
		for range n {
			b.Write([]byte{0x2C, 0, 0, 0, 0})
			b.Write(binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, uint16(fw)), uint16(fh)))
			b.Write([]byte{0, 2, 1, 0xFF, 0})
		}
		b.WriteByte(0x3B)
		return b.Bytes()
	}
	for name, c := range map[string]struct {
		data []byte
		crop image.Rectangle
	}{
		"too many frames":      {frames(1, 1, maxAnimationFrames+1, 1, 1), image.Rect(0, 0, 1, 1)},
		"too large a canvas":   {frames(4096, 2048, 1, 1, 1), image.Rect(0, 0, 1, 1)},
		"too large a frame":    {frames(16, 16, 2, 4096, 2048), image.Rect(0, 0, 1, 1)},
		"too much work":        {frames(2048, 2048, 70, 2048, 2048), image.Rect(0, 0, 1, 1)},
		"too much of the crop": {frames(2048, 2048, 70, 1, 1), image.Rect(0, 0, 2048, 2048)},
	} {
		p.Crop = c.crop
		if _, err := CropAnimation(ctx, c.data, GIF, p); !errors.Is(err, ErrAnimationTooLarge) {
			t.Errorf("%s: %v", name, err)
		}
	}
	p.Crop = image.Rect(0, 0, 1, 1)
	if _, err := CropAnimation(ctx, frames(1, 1, maxAnimationFrames, 1, 1), GIF, p); err != nil {
		t.Errorf("as many frames as allowed: %v", err)
	}
	if _, err := CropAnimation(ctx, make([]byte, MaxAnimationInput+1), GIF, p); !errors.Is(err, ErrAnimationTooLarge) {
		t.Errorf("too large a file: %v", err)
	}

	// A GIF larger than allowed stops as it passes it.
	var in bytes.Buffer
	src := &gif.GIF{Config: image.Config{Width: 64, Height: 64}}
	for i := range 64 {
		f := paletted(image.Rect(0, 0, 64, 64), color.Black, color.White)
		for j := range f.Pix {
			f.Pix[j] = uint8((j*7 + i*13) / 5 % 2)
		}
		src.Image, src.Delay, src.Disposal = append(src.Image, f), append(src.Delay, 10), append(src.Disposal, 0)
	}
	gif.EncodeAll(&in, src)
	_, err := CropAnimation(ctx, in.Bytes(), GIF, AnimatedPicture{Crop: image.Rect(0, 0, 64, 64), Width: 64, Height: 64, MaxSize: 16 << 10})
	if !errors.Is(err, ErrAnimationTooLong) {
		t.Fatalf("a GIF past its size: %v", err)
	}
}

// FuzzCropAnimation runs damaged GIFs and WebPs through the frames' readers
// and on to a picture, which must refuse what it can't read, and otherwise
// make a GIF the standard library reads, of the picture's size.
func FuzzCropAnimation(f *testing.F) {
	var in bytes.Buffer
	gif.EncodeAll(&in, &gif.GIF{Image: []*image.Paletted{paletted(image.Rect(0, 0, 16, 16), color.Black, color.White),
		paletted(image.Rect(4, 4, 8, 8), color.RGBA{0xFF, 0, 0, 0xFF}, color.RGBA{})}, Delay: []int{5, 5},
		Disposal: []byte{gif.DisposalNone, gif.DisposalPrevious}, LoopCount: 3})
	f.Add(in.Bytes())
	f.Add(rawGIF(8, 8, rawFrame{rect: image.Rect(0, 0, 8, 8), colors: []byte{1, 2, 3, 4, 5, 6}, pix: make([]byte, 64), interlaced: true}))
	f.Add(animatedWebP(16, 16, 2, webpFrameSpec{img: filled(16, 16, color.NRGBA{1, 2, 3, 0xFF}), ms: 50},
		webpFrameSpec{img: filled(4, 4, color.NRGBA{9, 8, 7, 0x80}), x: 2, y: 2, ms: 50, blend: true, clear: true}))
	f.Add(readFixture(f, "anim.webp"))
	f.Add(readFixture(f, "alpha.webp"))
	f.Fuzz(func(t *testing.T, data []byte) {
		k := Sniff(data)
		if k != GIF && k != WebP {
			return
		}
		out, err := CropAnimation(context.Background(), data, k, AnimatedPicture{Crop: image.Rect(0, 0, 8, 8), Width: 8, Height: 8, MaxSize: 1 << 20})
		if err != nil {
			return
		}
		g, err := gif.DecodeAll(bytes.NewReader(out))
		if err != nil || g.Config.Width != 8 || g.Config.Height != 8 {
			t.Fatalf("the picture: %v", err)
		}
	})
}
