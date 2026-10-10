package denclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Managing files (M5): this member's files on a den, largest first, and
// taking one off its message or swapping it for another. A file's bytes
// never change under its ID, so both are edits of the message: the den
// changes a channel's, and this service seals a DM's again.

// PageFile is one of this member's files as the page lists it. A DM's
// comes opened, with its name and preview, or says why it can't be.
type PageFile struct {
	denproto.OwnFile
	Locked string `json:"locked,omitempty"`
}

// FilesPage is a page of this member's files, largest first; Next
// continues it.
type FilesPage struct {
	Files []PageFile `json:"files"`
	Next  string     `json:"next,omitempty"`
}

// Files lists a page of this member's files on a den. after is the Next
// of the page before, or empty for the first.
func (m *Manager) Files(ctx context.Context, denID, after string) (FilesPage, error) {
	c, err := m.find(denID)
	if err != nil {
		return FilesPage{}, err
	}
	if len(after) > 64 {
		return FilesPage{}, inputError(errors.New("that isn't a page of files"))
	}
	var page denproto.OwnFiles
	if err := c.call(ctx, http.MethodGet, "/api/me/files?after="+url.QueryEscape(after), nil, &page); err != nil {
		return FilesPage{}, err
	}
	if denproto.CheckOwnFiles(page) != nil {
		return FilesPage{}, errMalformed
	}
	// A DM's files are blobs, named by the messages they're on, which also
	// hold each file's preview as a blob of its own.
	opened := map[string]PageMessage{}
	previews := map[string]bool{}
	for _, msg := range page.Messages {
		pm := c.open(msg)
		opened[msg.ID] = pm
		for _, f := range pm.Attachments {
			if thumb := c.previewOf(f.ID); thumb != "" {
				previews[thumb] = true
			}
		}
	}
	out := FilesPage{Files: []PageFile{}, Next: page.Next}
	for _, f := range page.Files {
		pf := PageFile{OwnFile: f}
		if f.Sealed {
			if previews[f.ID] {
				continue // it goes with its file
			}
			pm, ok := opened[f.MessageID]
			switch {
			case !ok:
				pf.Locked = lockedBroken
			case pm.Locked != "":
				pf.Locked = pm.Locked
			default:
				i := slices.IndexFunc(pm.Attachments, func(a denproto.File) bool { return a.ID == f.ID })
				if i < 0 {
					pf.Locked = lockedBroken
					break
				}
				pf.File, pf.Excerpt = pm.Attachments[i], denproto.Excerpt(pm.Text)
			}
		}
		out.Files = append(out.Files, pf)
	}
	return out, nil
}

// previewOf is the blob of a DM file's preview, as far as this service
// knows, or empty.
func (c *conn) previewOf(fileID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dmFiles[fileID].thumb
}

// DropUpload deletes one of this member's uploads waiting to be sent, so
// its space is free at once rather than within the hour. A DM's goes with
// its preview.
func (m *Manager) DropUpload(ctx context.Context, denID, uploadID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("upload", uploadID); err != nil {
		return err
	}
	blobs := []string{uploadID}
	c.mu.Lock()
	if u, ok := c.uploads[uploadID]; ok {
		if u.file.Thumb != nil {
			blobs = append(blobs, u.file.Thumb.ID)
		}
		delete(c.uploads, uploadID)
		delete(c.dmFiles, uploadID)
	}
	c.mu.Unlock()
	c.dropKept(blobs[:1])
	for _, id := range blobs {
		if err := c.call(ctx, http.MethodDelete, "/api/uploads/"+id, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// RemoveFile takes one of this member's files off the message it's on,
// which deletes it. A message that would be left with neither text nor
// files is deleted instead.
func (m *Manager) RemoveFile(ctx context.Context, denID, channelID, messageID, fileID string) error {
	return m.changeFile(ctx, denID, channelID, messageID, fileID, "")
}

// SwapFile puts an upload made with Replace in the place of the file it
// replaces, on that file's message, which deletes the file.
func (m *Manager) SwapFile(ctx context.Context, denID, channelID, messageID, fileID, uploadID string) error {
	if err := checkID("upload", uploadID); err != nil {
		return err
	}
	return m.changeFile(ctx, denID, channelID, messageID, fileID, uploadID)
}

// errEmpty is a change of files that would leave a message with nothing.
var errEmpty = errors.New("the message would be empty")

// changeFile takes a file off its message, or with upload set, puts that
// in its place. An edit that lands meanwhile doesn't stop it: it tries
// again on the message as that left it.
func (m *Manager) changeFile(ctx context.Context, denID, channelID, messageID, fileID, upload string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	for _, id := range [][2]string{{"channel", channelID}, {"message", messageID}, {"file", fileID}} {
		if err := checkID(id[0], id[1]); err != nil {
			return err
		}
	}
	var with *denproto.DMFile
	_, dm := c.isDM(channelID)
	if dm && upload != "" {
		c.mu.Lock()
		u, ok := c.uploads[upload]
		c.mu.Unlock()
		if !ok || u.channel != channelID {
			return inputError(errors.New("the new file isn't ready to go in this DM any more; add it again"))
		}
		with = &u.file
	}
	for attempt := 0; ; attempt++ {
		if dm {
			err = c.changeDMFile(ctx, channelID, messageID, fileID, with)
		} else {
			err = c.changeChannelFile(ctx, channelID, messageID, fileID, upload)
		}
		if errors.Is(err, errEmpty) {
			return c.call(ctx, http.MethodDelete, "/api/messages/"+messageID, nil, nil)
		}
		var conflict *ErrEditConflict
		if !errors.As(err, &conflict) || attempt == 2 {
			if err == nil && with != nil {
				c.sentUploads(blobsOf(*with))
			} else if err == nil && upload != "" {
				c.sentUploads([]string{upload})
			}
			return err
		}
	}
}

// changeChannelFile changes a file of a channel's message: the den keeps
// the message's files apart from its text, so the edit names them.
func (c *conn) changeChannelFile(ctx context.Context, channelID, messageID, fileID, upload string) error {
	current, err := c.message(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	files, err := swapIn(fileIDs(current.Attachments), fileID, upload)
	if err != nil {
		return err
	}
	if len(files) == 0 && denproto.CheckMessageText(current.Text, false) != nil {
		return errEmpty
	}
	_, err = change(ctx, c, http.MethodPatch, "/api/messages/"+messageID,
		denproto.EditRequest{Revision: current.Revision, Text: current.Text, Attachments: &files})
	return err
}

// changeDMFile changes a file of a DM's message, which lists its files in
// its sealed text: the message is sealed again with the new list, and the
// edit names every blob it keeps.
func (c *conn) changeDMFile(ctx context.Context, channelID, messageID, fileID string, with *denproto.DMFile) error {
	_, err := c.editDM(ctx, channelID, messageID, 0, nil, func(p *denproto.DMPayload) (bool, error) {
		i := slices.IndexFunc(p.Files, func(f denproto.DMFile) bool { return f.ID == fileID })
		if i < 0 {
			return false, inputError(errors.New("that file isn't on that message any more"))
		}
		if with != nil {
			p.Files[i] = *with
		} else {
			p.Files = slices.Delete(p.Files, i, i+1)
		}
		if len(p.Files) == 0 && denproto.CheckMessageText(p.Text, false) != nil {
			return false, errEmpty
		}
		return true, nil
	}, true)
	return err
}

// swapIn takes id out of a list of files, or puts with in its place.
func swapIn(files []string, id, with string) ([]string, error) {
	i := slices.Index(files, id)
	if i < 0 {
		return nil, inputError(errors.New("that file isn't on that message any more"))
	}
	if with != "" {
		files[i] = with
		return files, nil
	}
	return slices.Delete(files, i, i+1), nil
}

func fileIDs(fs []denproto.File) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.ID)
	}
	return out
}

// blobsOf is every blob a DM's file takes: its own and its preview's.
func blobsOf(f denproto.DMFile) []string {
	if f.Thumb != nil {
		return []string{f.ID, f.Thumb.ID}
	}
	return []string{f.ID}
}
