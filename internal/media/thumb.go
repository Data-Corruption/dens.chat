package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Previews fit in a ThumbSize square: sharp at up to twice the size the
// message list shows them.
const (
	ThumbSize    = 640
	thumbQuality = 80
	// maxDecodeBytes bounds the memory one decoded image may take. A
	// 50-megapixel phone photo fits; larger images are sent without a
	// preview.
	maxDecodeBytes = 256 << 20
)

// ErrTooBig is an image too large to decode for a preview. It is still
// sent, as a file to download.
var ErrTooBig = errors.New("the image is too large to preview")

// Thumb is a preview image.
type Thumb struct {
	Data []byte
	// Type is image/jpeg, or image/png for a preview with transparency.
	Type          string
	Width, Height int
}

// CanPreview reports whether an image Strip or Verify described is small
// enough to decode.
func CanPreview(res Result) bool {
	per := int64(4)
	if res.pixelBytes > 0 {
		per = int64(res.pixelBytes)
	}
	return res.Width > 0 && res.Height > 0 && int64(res.Width)*int64(res.Height)*per <= maxDecodeBytes
}

// Thumbnail decodes an image of kind k, which Strip or Verify described as
// res, and returns an upright preview that fits in ThumbSize. An animated
// image's preview is its first frame.
func Thumbnail(r io.Reader, k Kind, res Result) (Thumb, error) {
	if !k.Image() {
		return Thumb{}, fmt.Errorf("a %s has no preview", k)
	}
	if !CanPreview(res) {
		return Thumb{}, ErrTooBig
	}
	src, err := decode(r, k, res)
	if err != nil {
		return Thumb{}, fmt.Errorf("%w: decode the %s: %v", ErrMalformed, k, err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if res.Orientation >= 5 {
		w, h = h, w
	}
	if w != res.Width || h != res.Height {
		return Thumb{}, malformed(k, "a size that doesn't match its header")
	}
	tw, th := fit(b.Dx(), b.Dy(), ThumbSize)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	out := orient(dst, res.Orientation)
	var buf bytes.Buffer
	t := Thumb{Width: out.Bounds().Dx(), Height: out.Bounds().Dy()}
	if out.Opaque() {
		t.Type = "image/jpeg"
		err = jpeg.Encode(&buf, out, &jpeg.Options{Quality: thumbQuality})
	} else {
		t.Type = "image/png"
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, out)
	}
	t.Data = buf.Bytes()
	return t, err
}

func decode(r io.Reader, k Kind, res Result) (image.Image, error) {
	switch k {
	case JPEG:
		return jpeg.Decode(r)
	case PNG:
		return png.Decode(r)
	case GIF:
		frame, err := gif.Decode(r)
		if err != nil {
			return nil, err
		}
		return onCanvas(frame, res.Width, res.Height), nil
	case WebP:
		return decodeWebP(r, res)
	}
	return nil, fmt.Errorf("no decoder for %s", k)
}

// onCanvas draws a frame onto a transparent canvas the size of the image,
// unless it already covers it.
func onCanvas(frame image.Image, w, h int) image.Image {
	canvas := image.Rect(0, 0, w, h)
	if frame.Bounds() == canvas {
		return frame
	}
	out := image.NewRGBA(canvas)
	draw.Draw(out, frame.Bounds(), frame, frame.Bounds().Min, draw.Src)
	return out
}

// decodeWebP decodes a WebP, or an animated one's first frame, which the
// WebP decoder can't find on its own: it is rebuilt as a still image.
func decodeWebP(r io.Reader, res Result) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if !res.Animated {
		return webp.Decode(bytes.NewReader(data))
	}
	if len(data) < 12 {
		return nil, errors.New("cut short")
	}
	chunks, err := riffChunks(data[12:])
	if err != nil {
		return nil, err
	}
	for _, c := range chunks {
		if c.id != "ANMF" {
			continue
		}
		if len(c.data) < 16 {
			return nil, errors.New("a short animation frame")
		}
		d := c.data
		x := 2 * (int(d[0]) | int(d[1])<<8 | int(d[2])<<16)
		y := 2 * (int(d[3]) | int(d[4])<<8 | int(d[5])<<16)
		fw := 1 + (int(d[6]) | int(d[7])<<8 | int(d[8])<<16)
		fh := 1 + (int(d[9]) | int(d[10])<<8 | int(d[11])<<16)
		inner, err := riffChunks(d[16:])
		if err != nil {
			return nil, err
		}
		still := []byte("WEBP")
		alpha := false
		for _, fc := range inner {
			alpha = alpha || fc.id == "ALPH"
		}
		if alpha {
			vp8x := make([]byte, 10)
			vp8x[0] = webpAlpha
			put24(vp8x[4:], fw-1)
			put24(vp8x[7:], fh-1)
			still = appendChunk(still, riffChunk{id: "VP8X", data: vp8x})
		}
		for _, fc := range inner {
			still = appendChunk(still, fc)
		}
		file := binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(still)))
		frame, err := webp.Decode(bytes.NewReader(append(file, still...)))
		if err != nil {
			return nil, err
		}
		b := frame.Bounds()
		if b.Dx() != fw || b.Dy() != fh {
			return nil, errors.New("a frame's size doesn't match its header")
		}
		out := image.NewRGBA(image.Rect(0, 0, res.Width, res.Height))
		draw.Draw(out, image.Rect(x, y, x+fw, y+fh), frame, b.Min, draw.Src)
		return out, nil
	}
	return nil, errors.New("no animation frame")
}

func put24(b []byte, v int) {
	b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16)
}

// fit scales w×h to fit in a box×box square, never enlarging it.
func fit(w, h, box int) (int, int) {
	if w <= box && h <= box {
		return w, h
	}
	if w >= h {
		return box, max(1, (h*box+w/2)/w)
	}
	return max(1, (w*box+h/2)/h), box
}

// orient turns an image upright from its EXIF orientation: 2 to 4 mirror
// or turn it half way, 5 to 8 also swap its sides.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2: // mirrored
				dx, dy = w-1-x, y
			case 3: // turned half way
				dx, dy = w-1-x, h-1-y
			case 4: // mirrored top to bottom
				dx, dy = x, h-1-y
			case 5: // transposed
				dx, dy = y, x
			case 6: // needs a quarter turn clockwise
				dx, dy = h-1-y, x
			case 7: // transversed
				dx, dy = h-1-y, w-1-x
			case 8: // needs a quarter turn counterclockwise
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):][:4], src.Pix[src.PixOffset(x, y):][:4])
		}
	}
	return dst
}
