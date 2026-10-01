package denproto

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits for M1.4.
const (
	MaxAttachments   = 10  // files on one message
	MaxFilenameBytes = 255 // a file's name, cut without splitting a character
	maxMediaType     = 127
	// MaxThumb bounds a preview's sides; the den makes them at most 640.
	MaxThumb = 1024
	// MaxImageSide bounds an image's stated sides, which clients lay out
	// before it loads.
	MaxImageSide = 1 << 16
)

// Upload limits a den starts with; the owner changes them.
const (
	DefaultFileSize      = 25 << 20
	DefaultMemberStorage = 2 << 30
	DefaultDenStorage    = 20 << 30
	// MaxFileSize is the largest per-file limit an owner can set.
	MaxFileSize = 1 << 30
	// MinFileSize is the smallest, which still fits an avatar.
	MinFileSize = 1 << 20
)

// Profile images: a square avatar, and a banner three times as wide as it
// is high. Clients crop them to these shapes before uploading.
const (
	MinAvatarSide = 64
	MaxAvatarSide = 1024
	MinBannerWide = 300
	MaxBannerWide = 3000
)

// HeaderFilename carries an upload's name, percent-encoded, since headers
// are ASCII.
const HeaderFilename = "Dens-Filename"

// SealedType is the type a den gives a sealed upload, which it can't open.
const SealedType = "application/octet-stream"

// Error codes for uploads.
const (
	// CodeUnsupportedType (415) is a file Dens refuses rather than send
	// with metadata it can't remove: video, audio, and photos in formats
	// it can't clean yet.
	CodeUnsupportedType = "unsupported_type"
	// CodeQuotaExceeded (507) is a member who has used up their space.
	CodeQuotaExceeded = "quota_exceeded"
	// CodeDenFull (507) is a den out of space for uploads: its limit, or
	// its disk.
	CodeDenFull = "den_full"
)

// File is an uploaded file: an attachment, or a picture on a profile. Type
// is what the den found the file to be, never what the uploader said;
// Width and Height are set for images, as they display, and Thumb when the
// den made a preview.
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Animated bool   `json:"animated,omitempty"`
	Thumb    *Thumb `json:"thumb,omitempty"`
	// Sealed marks an upload sealed by the member's client for a DM
	// (M1.7), which the den can't open: it has no name or type of its own.
	Sealed bool `json:"sealed,omitempty"`
}

// Thumb is the size of a file's preview.
type Thumb struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Image is a picture on a profile: an uploaded image and its size.
type Image struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Limits are a den's upload limits, in bytes: per file, per member's
// files together, and for the whole den.
type Limits struct {
	FileSize      int64 `json:"file_size"`
	MemberStorage int64 `json:"member_storage"`
	DenStorage    int64 `json:"den_storage"`
}

// Storage is how much of the den's space a member's files take, and
// everyone's.
type Storage struct {
	Used    int64 `json:"used"`
	DenUsed int64 `json:"den_used"`
}

// CheckLimits checks limits an owner sets.
func CheckLimits(l Limits) error {
	if l.FileSize < MinFileSize || l.FileSize > MaxFileSize {
		return fmt.Errorf("the file size limit is %d to %d MiB", MinFileSize>>20, MaxFileSize>>20)
	}
	if l.MemberStorage < l.FileSize || l.DenStorage < l.MemberStorage || l.DenStorage > 1<<50 {
		return errors.New("each member's space is at least one file, and the den's at least one member's")
	}
	return nil
}

// CleanFilename makes a file's name safe to store and show: characters
// that hide or reorder text are dropped, path separators become _, spaces
// and dots are trimmed from the ends, and it's cut to MaxFilenameBytes
// without splitting a character. An empty result is "file".
func CleanFilename(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		switch {
		case hiddenRune(r):
			return -1
		case r == '/' || r == '\\':
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(strings.Join(strings.Fields(name), " "), " .")
	for len(name) > MaxFilenameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	name = strings.TrimRight(name, " .")
	if name == "" {
		return "file"
	}
	return name
}

// CheckMediaType checks a media type as a den states it: lowercase
// type/subtype, without parameters.
func CheckMediaType(t string) error {
	main, sub, ok := strings.Cut(t, "/")
	if !ok || main == "" || sub == "" || len(t) > maxMediaType {
		return errors.New("invalid media type")
	}
	for _, c := range []byte(main + sub) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&^_.+-", c) >= 0) {
			return errors.New("invalid media type")
		}
	}
	return nil
}

// CheckFile checks a file's description from a den before a client keeps
// or shows it.
func CheckFile(f File) error {
	if _, err := ParseID(f.ID); err != nil {
		return errors.New("file has an invalid ID")
	}
	if f.Sealed {
		if f.Name != "" || f.Type != SealedType || f.Size < 0 || f.Size > MaxFileSize || f.Width != 0 || f.Height != 0 || f.Animated || f.Thumb != nil {
			return errors.New("sealed file has details it can't have")
		}
		return nil
	}
	if f.Name == "" || CleanFilename(f.Name) != f.Name {
		return errors.New("file has an invalid name")
	}
	if err := CheckMediaType(f.Type); err != nil {
		return err
	}
	if f.Size < 0 || f.Size > MaxFileSize {
		return errors.New("file has an invalid size")
	}
	if (f.Width == 0) != (f.Height == 0) || f.Width < 0 || f.Height < 0 || f.Width > MaxImageSide || f.Height > MaxImageSide {
		return errors.New("file has an invalid image size")
	}
	if t := f.Thumb; t != nil {
		if f.Width == 0 || t.Width < 1 || t.Height < 1 || t.Width > MaxThumb || t.Height > MaxThumb {
			return errors.New("file has an invalid preview")
		}
	}
	return nil
}

// CheckImage checks a profile picture from a den.
func CheckImage(i Image) error {
	if i == (Image{}) {
		return nil
	}
	if _, err := ParseID(i.ID); err != nil {
		return errors.New("picture has an invalid ID")
	}
	if i.Width < 1 || i.Height < 1 || i.Width > MaxBannerWide || i.Height > MaxBannerWide {
		return errors.New("picture has an invalid size")
	}
	return nil
}
