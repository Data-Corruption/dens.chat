package denclient

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// The den's emoji (M15.2): staff's page adds an emoji by an upload, a PNG
// its canvas drew or a GIF this service made (see UploadAnimated), and a
// name; every page draws the den's emoji, by their images, which this
// service fetches into the cache ahead of their use.

// AddEmoji adds an emoji to a den: an upload of this member's, waiting to
// be used, as its image, and its name.
func (m *Manager) AddEmoji(ctx context.Context, denID, name, uploadID string) (denproto.Emoji, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Emoji{}, err
	}
	clean, err := denproto.NormalizeEmojiName(name)
	if err != nil {
		return denproto.Emoji{}, inputError(errors.New("An emoji's name is 2 to 32 letters, digits and _."))
	}
	if err := checkID("upload", uploadID); err != nil {
		return denproto.Emoji{}, err
	}
	var emoji denproto.Emoji
	if err := c.call(ctx, http.MethodPost, "/api/emoji", denproto.EmojiRequest{Name: clean, FileID: uploadID}, &emoji); err != nil {
		return denproto.Emoji{}, err
	}
	if denproto.CheckEmoji(emoji) != nil {
		return denproto.Emoji{}, errMalformed
	}
	return emoji, nil
}

// RenameEmoji gives one of a den's emoji another name.
func (m *Manager) RenameEmoji(ctx context.Context, denID, emojiID, name string) (denproto.Emoji, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Emoji{}, err
	}
	if err := checkID("emoji", emojiID); err != nil {
		return denproto.Emoji{}, err
	}
	clean, err := denproto.NormalizeEmojiName(name)
	if err != nil {
		return denproto.Emoji{}, inputError(errors.New("An emoji's name is 2 to 32 letters, digits and _."))
	}
	var emoji denproto.Emoji
	if err := c.call(ctx, http.MethodPatch, "/api/emoji/"+emojiID, denproto.EmojiUpdate{Name: clean}, &emoji); err != nil {
		return denproto.Emoji{}, err
	}
	if denproto.CheckEmoji(emoji) != nil {
		return denproto.Emoji{}, errMalformed
	}
	return emoji, nil
}

// DeleteEmoji deletes one of a den's emoji, and its image.
func (m *Manager) DeleteEmoji(ctx context.Context, denID, emojiID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("emoji", emojiID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/emoji/"+emojiID, nil, nil)
}

// errFileNotKept answers a page asking for an emoji's file that the cache
// couldn't fetch with the rest, as it would for a file the den lacks.
var errFileNotKept error = &denproto.Error{Status: http.StatusNotFound, Code: denproto.CodeNotFound, Message: "the emoji isn't in the cache"}

// emojiFile is a file of one of the den's emoji to keep: its image, or the
// preview of one that moves, which a page shows while it doesn't.
type emojiFile struct {
	emoji, file int64
	thumb       bool
}

// emojiFiles lists the files of the den's emoji to keep, none while they're
// off or the den is older than emoji.
func (c *conn) emojiFiles() []emojiFile {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil || c.den.emojiLimits == nil || c.den.emojiLimits.Emojis == 0 {
		return nil
	}
	var out []emojiFile
	for _, e := range c.den.emoji {
		id, err1 := denproto.ParseID(e.ID)
		file, err2 := denproto.ParseID(e.Image.ID)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, emojiFile{emoji: id, file: file})
		if e.Image.Animated {
			out = append(out, emojiFile{emoji: id, file: file, thumb: true})
		}
	}
	return out
}

// kept reports whether a file is the image of one of the den's emoji, or
// its preview, while emoji are on: one the page takes from the cache alone
// (see fillEmoji). While they're off no message shows one, so Manage's
// list of them fetches each as it shows, as for any file.
func (c *conn) keptEmoji(fileID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil || c.den.emojiLimits == nil || c.den.emojiLimits.Emojis == 0 {
		return false
	}
	for _, e := range c.den.emoji {
		if e.Image.ID == fileID {
			return true
		}
	}
	return false
}

// fillEmoji fetches into the cache the files of the den's emoji it doesn't
// hold whole, so a page that shows one fetches nothing from the den then:
// what that would tell the den is which emoji showed, in a DM too. They
// come whenever the list arrives or changes, and whenever a page asks for
// one the cache let go of, all of those it lacks at once. The owner's count
// bounds what it takes. One fill runs at a time, and the next finds what
// the last fetched.
func (c *conn) fillEmoji(ctx context.Context) error {
	if !c.cached() {
		return nil
	}
	c.fillMu.Lock()
	defer c.fillMu.Unlock()
	for _, f := range c.emojiFiles() {
		whole, err := c.m.Cache.Whole(c.j.denID, f.file, f.thumb)
		if err != nil {
			return err
		}
		if whole {
			continue
		}
		fileID := denproto.FormatID(f.file)
		h, err := c.m.Cache.File(c.j.denID, f.file, f.thumb, cache.Owner{Named: f.emoji}, func() (cache.Source, error) {
			return c.fileSource(ctx, fileID, f.thumb)
		})
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, io.NewSectionReader(h, 0, h.Size()))
		h.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// kickFill wakes the cache's worker to fetch the den's emoji.
func (c *conn) kickFill() {
	select {
	case c.fillDue <- struct{}{}:
	default:
	}
}
