// Package media handles the files members share: it recognizes what a file
// is, takes metadata such as EXIF and GPS out of photos without re-encoding
// them, and makes the previews the message list shows.
//
// The sending client and the den run the same code. The client strips a
// photo before it leaves the member's machine, so the den never sees where
// it was taken; the den verifies that nothing is left and makes the
// preview.
package media

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// Kind is what a file is, as far as Dens cares.
type Kind uint8

const (
	// Other is any file Dens doesn't look inside. It is sent as it is,
	// with whatever details it carries, and only ever downloaded.
	Other Kind = iota
	JPEG
	PNG
	GIF
	WebP
	// Video and Audio carry metadata, often a location, that Dens can't
	// remove without ffmpeg, so they aren't sent yet.
	Video
	Audio
	// Photo is a photo format Dens can't clean yet: HEIF and AVIF, TIFF and
	// camera raw, JPEG XL and JPEG 2000. It isn't sent either.
	Photo
)

// Image reports whether Dens strips, previews and shows a kind inline.
func (k Kind) Image() bool { return k >= JPEG && k <= WebP }

// Refused reports whether a kind is refused rather than sent with
// metadata Dens can't take out.
func (k Kind) Refused() bool { return k >= Video }

// MIME is an image kind's media type, and "" for the others.
func (k Kind) MIME() string {
	switch k {
	case JPEG:
		return "image/jpeg"
	case PNG:
		return "image/png"
	case GIF:
		return "image/gif"
	case WebP:
		return "image/webp"
	}
	return ""
}

func (k Kind) String() string {
	switch k {
	case JPEG:
		return "JPEG"
	case PNG:
		return "PNG"
	case GIF:
		return "GIF"
	case WebP:
		return "WebP"
	case Video:
		return "video"
	case Audio:
		return "audio"
	case Photo:
		return "photo"
	}
	return "file"
}

// SniffLen is how much of a file Sniff wants to see.
const SniffLen = 512

// Sniff says what kind of file starts with head, which should be its
// first SniffLen bytes, or all of it if it's shorter. It goes by content,
// never by name.
func Sniff(head []byte) Kind {
	has := func(off int, s string) bool {
		return len(head) >= off+len(s) && string(head[off:off+len(s)]) == s
	}
	switch {
	case has(0, "\xFF\xD8\xFF"):
		return JPEG
	case has(0, "\x89PNG\r\n\x1a\n"):
		return PNG
	case has(0, "GIF87a"), has(0, "GIF89a"):
		return GIF
	case has(0, "RIFF") && has(8, "WEBP"):
		return WebP
	case has(0, "RIFF") && (has(8, "AVI ") || has(8, "AVIX")):
		return Video
	case has(0, "RIFF") && has(8, "WAVE"):
		return Audio
	case has(4, "ftyp"):
		return sniffISO(head)
	case has(4, "moov"), has(4, "mdat"), has(4, "wide"), has(4, "pnot"):
		return Video // QuickTime without a file type box
	case has(0, "\x1A\x45\xDF\xA3"): // Matroska and WebM
		return Video
	case has(0, "OggS"), has(0, "fLaC"), has(0, "ID3"), has(0, "#!AMR"), has(0, "MThd"):
		return Audio
	case has(0, "FORM") && (has(8, "AIFF") || has(8, "AIFC")):
		return Audio
	case has(0, "FLV\x01"), has(0, ".RMF"), has(0, "\x00\x00\x01\xBA"), has(0, "\x00\x00\x01\xB3"):
		return Video
	case has(0, "\x30\x26\xB2\x75\x8E\x66\xCF\x11"): // ASF: WMV and WMA
		return Video
	case len(head) > 188 && head[0] == 0x47 && head[188] == 0x47: // MPEG transport stream
		return Video
	// TIFF, which DNG and most camera raw formats are, then other raw
	// formats, JPEG XL, JPEG 2000, and Photoshop, which carries the EXIF
	// of the photos in it.
	case has(0, "II*\x00"), has(0, "MM\x00*"), has(0, "II+\x00"), has(0, "MM\x00+"),
		has(0, "IIRO"), has(0, "IIRS"), has(0, "MMOR"), has(0, "IIU\x00"), has(0, "FUJIFILMCCD-RAW"),
		has(0, "FOVb"), has(0, "\x00MRM"), has(6, "HEAPCCDR"),
		has(0, "\x00\x00\x00\x0CJXL \r\n\x87\n"), has(0, "\xFF\x0A"),
		has(0, "\x00\x00\x00\x0CjP  \r\n\x87\n"),
		has(0, "8BPS"):
		return Photo
	}
	return Other
}

// sniffISO tells ISO media files apart by their brands: HEIF and AVIF
// photos, Canon raw, and otherwise audio or video.
func sniffISO(head []byte) Kind {
	if len(head) < 16 {
		return Video
	}
	size := int(head[0])<<24 | int(head[1])<<16 | int(head[2])<<8 | int(head[3])
	if size < 16 || size > len(head) {
		size = min(len(head), 64)
	}
	brands := [][]byte{head[8:12]}
	for off := 16; off+4 <= size; off += 4 {
		brands = append(brands, head[off:off+4])
	}
	audio := false
	for _, b := range brands {
		switch string(b) {
		case "heic", "heix", "heim", "heis", "hevc", "hevx", "hevm", "hevs", "mif1", "mif2", "msf1", "avif", "avis", "avci", "crx ":
			return Photo
		case "M4A ", "M4B ", "M4P ", "F4A ", "F4B ":
			audio = true
		}
	}
	if audio {
		return Audio
	}
	return Video
}

// Errors from Strip and Verify.
var (
	// ErrMetadata is metadata in a file that should have none.
	ErrMetadata = errors.New("the file still has metadata")
	// ErrMalformed is a file that isn't what it starts out as.
	ErrMalformed = errors.New("the file is damaged or not what it claims to be")
)

func malformed(k Kind, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrMalformed, k, fmt.Sprintf(format, args...))
}

// Result describes an image Strip or Verify copied.
type Result struct {
	// Width and Height are the size the image shows at: after the
	// orientation a JPEG keeps is applied.
	Width, Height int
	Animated      bool
	// Orientation is the EXIF orientation a JPEG keeps, from 1 (upright)
	// to 8. Images of other kinds are always 1.
	Orientation int
	// Removed says Strip took something out.
	Removed bool

	// pixelBytes is roughly what one pixel costs decoded, for the
	// thumbnailer's memory budget.
	pixelBytes int
}

// Strip copies a file of kind k from r to w, taking out everything an
// image doesn't need to show: EXIF, GPS, XMP, comments, embedded
// thumbnails, text chunks and anything after the image's end. A JPEG keeps
// only its orientation, rewritten as the smallest EXIF block that holds
// it. Pixels are never re-encoded. Files of kinds other than images pass
// through unchanged.
//
// A WebP is held in memory while it's rewritten, since its header states
// its size; callers bound r.
func Strip(w io.Writer, r io.Reader, k Kind) (Result, error) {
	return filter(w, r, k, false)
}

// Verify copies a file of kind k from r to w like Strip, but fails with
// ErrMetadata instead of taking anything out, and holds nothing in memory.
// The den runs it on uploads, which clients strip before sending.
func Verify(w io.Writer, r io.Reader, k Kind) (Result, error) {
	return filter(w, r, k, true)
}

func filter(w io.Writer, r io.Reader, k Kind, verify bool) (Result, error) {
	if !k.Image() {
		_, err := io.Copy(w, r)
		return Result{Orientation: 1}, err
	}
	bw := bufio.NewWriterSize(w, 64<<10)
	f := &filterState{w: bw, verify: verify, kind: k, res: Result{Orientation: 1}}
	br := bufio.NewReaderSize(r, 64<<10)
	var err error
	switch k {
	case JPEG:
		err = f.jpeg(br)
	case PNG:
		err = f.png(br)
	case GIF:
		err = f.gif(br)
	case WebP:
		if verify {
			err = f.webpVerify(br)
		} else {
			err = f.webpStrip(br)
		}
	}
	if err == nil {
		err = bw.Flush()
	}
	return f.res, err
}

type filterState struct {
	w      *bufio.Writer
	verify bool
	kind   Kind
	res    Result
}

// drop leaves something out of the copy; when verifying, it's an error.
func (f *filterState) drop(what string) error {
	if f.verify {
		return fmt.Errorf("%w: %s %s", ErrMetadata, f.kind, what)
	}
	f.res.Removed = true
	return nil
}

func (f *filterState) write(p []byte) error {
	_, err := f.w.Write(p)
	return err
}

// trailing handles whatever follows an image's end marker: nothing, or
// data to leave out.
func (f *filterState) trailing(r *bufio.Reader) error {
	if _, err := r.Peek(1); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	if err := f.drop("data after the image's end"); err != nil {
		return err
	}
	_, err := io.Copy(io.Discard, r)
	return err
}

// readFull reads exactly len(p) bytes, reporting a short read as a damaged
// file.
func (f *filterState) readFull(r io.Reader, p []byte) error {
	if _, err := io.ReadFull(r, p); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return malformed(f.kind, "cut short")
		}
		return err
	}
	return nil
}

// skip discards n bytes.
func (f *filterState) skip(r io.Reader, n int64) error {
	if _, err := io.CopyN(io.Discard, r, n); err != nil {
		if errors.Is(err, io.EOF) {
			return malformed(f.kind, "cut short")
		}
		return err
	}
	return nil
}

// copyN copies n bytes to the output.
func (f *filterState) copyN(r io.Reader, n int64) error {
	if _, err := io.CopyN(f.w, r, n); err != nil {
		if errors.Is(err, io.EOF) {
			return malformed(f.kind, "cut short")
		}
		return err
	}
	return nil
}
