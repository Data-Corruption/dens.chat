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
	// MaxDuration is the longest a video or audio file may say it lasts,
	// in milliseconds: a week.
	MaxDuration = 7 * 24 * 60 * 60 * 1000
	// MaxPreviewUpload bounds the image a page draws for a video's preview,
	// at the video's size, as a JPEG or PNG.
	MaxPreviewUpload = 16 << 20
)

// PreviewShape reports whether an image of pw × ph has the shape of a video
// of vw × vh, give or take a pixel of rounding on each side where it was
// drawn smaller.
func PreviewShape(pw, ph, vw, vh int) bool {
	if pw < 1 || ph < 1 || vw < 1 || vh < 1 {
		return false
	}
	diff := int64(pw)*int64(vh) - int64(ph)*int64(vw)
	return max(diff, -diff) <= int64(vw)+int64(vh)
}

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

// HeaderReplaces names the file an upload is made to take the place of
// (M5). The den counts the upload against the space that file frees, and
// it can take only that file's place.
const HeaderReplaces = "Dens-Replaces"

// What a picture on a profile is, in a member's list of their files (M5).
const (
	ProfileAvatar = "avatar"
	ProfileBanner = "banner"
)

// OwnFile is one of a member's files on a den, as they manage them (M5):
// the file and what uses it. That's a message, with its channel and, in a
// channel, the start of its text; a picture on their profile; or nothing
// while it waits to be used. A DM's file is a sealed blob, whose message
// comes in the page's Messages for the member's client to open.
type OwnFile struct {
	File
	MessageID string `json:"message_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	Profile   string `json:"profile,omitempty"`
	Excerpt   string `json:"excerpt,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// OwnFiles is a page of a member's files, largest first. Messages are the
// DM messages its sealed files are on, and Next continues the list.
type OwnFiles struct {
	Files    []OwnFile `json:"files"`
	Messages []Message `json:"messages,omitempty"`
	Next     string    `json:"next,omitempty"`
}

// OwnFilesPage is the most files a page of them lists.
const OwnFilesPage = 50

// CheckOwnFiles checks a page of a member's files as a den sends it.
func CheckOwnFiles(p OwnFiles) error {
	if len(p.Files) > OwnFilesPage || len(p.Messages) > OwnFilesPage || len(p.Next) > 64 {
		return errors.New("the page is too long")
	}
	for _, f := range p.Files {
		if err := CheckFile(f.File); err != nil {
			return err
		}
		onMessage := f.MessageID != "" || f.ChannelID != ""
		if onMessage {
			if _, err := ParseID(f.MessageID); err != nil {
				return errors.New("a file names an invalid message")
			}
			if _, err := ParseID(f.ChannelID); err != nil {
				return errors.New("a file names an invalid channel")
			}
		}
		if f.Profile != "" && (onMessage || f.Profile != ProfileAvatar && f.Profile != ProfileBanner) {
			return errors.New("a file is used in two places")
		}
		if f.Excerpt != "" && (!onMessage || utf8.RuneCountInString(f.Excerpt) > ReplyExcerpt) {
			return errors.New("a file has an invalid excerpt")
		}
		if f.CreatedAt <= 0 {
			return errors.New("a file has no time")
		}
	}
	for _, m := range p.Messages {
		if err := CheckMessage(m); err != nil || m.Sealed == nil {
			return errors.New("the page holds an invalid message")
		}
	}
	return nil
}

// Error codes for uploads.
const (
	// CodeUnsupportedType (415) is a file Dens refuses rather than send
	// with metadata it can't remove: AVIF, JPEG XL and camera raw photos,
	// and video and audio in containers the media module doesn't read. A
	// den also refuses a photo clients turn into a JPEG or PNG first, and
	// video and audio when it has no media module.
	CodeUnsupportedType = "unsupported_type"
	// CodeQuotaExceeded (507) is a member who has used up their space.
	CodeQuotaExceeded = "quota_exceeded"
	// CodeDenFull (507) is a den out of space for uploads: its limit, or
	// its disk.
	CodeDenFull = "den_full"
)

// File is an uploaded file: an attachment, or a picture on a profile. Type
// is what the den found the file to be, never what the uploader said;
// Width and Height are set for images and videos, as they display,
// Duration for video and audio when it's known, and Thumb when there's a
// preview.
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Animated bool   `json:"animated,omitempty"`
	Duration int64  `json:"duration_ms,omitempty"`
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
		if f.Name != "" || f.Type != SealedType || f.Size < 0 || f.Size > MaxFileSize || f.Width != 0 || f.Height != 0 || f.Animated ||
			f.Duration != 0 || f.Thumb != nil {
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
	if f.Duration < 0 || f.Duration > MaxDuration {
		return errors.New("file has an invalid duration")
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
