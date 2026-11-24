package denclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// FileAD is what a file's seal in a channel of a den binds, or its
// preview's, as any member who holds the file's key can seal under it,
// for the tests of package denclient_test.
func FileAD(m *Manager, denID, channel string, thumb bool) ([]byte, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	ch, err := denproto.ParseID(channel)
	if err != nil {
		return nil, err
	}
	return denproto.DMFileAD(c.j.denID, ch, thumb), nil
}

// SealPayloadAs seals a message's payload as this member's Dens would at
// its first revision, under author's name, past the checks a send makes,
// for the tests of package denclient_test.
func SealPayloadAs(m *Manager, denID, channel, author string, p denproto.DMPayload) (keyID string, nonce, sealed denproto.Bytes, err error) {
	c, err := m.find(denID)
	if err != nil {
		return "", nil, nil, err
	}
	nonce = denproto.Random(16)
	keyID, sealed, err = c.sealMessage(context.Background(), channel, author, nonce, 1, p)
	return keyID, nonce, sealed, err
}

// UploadedFile is what a message that sends one of this member's uploads
// waiting to be sent seals of it, for the tests of package denclient_test.
func UploadedFile(m *Manager, denID, uploadID string) (denproto.DMFile, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.DMFile{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.uploads[uploadID]
	if !ok {
		return denproto.DMFile{}, errors.New("no such upload")
	}
	return u.file, nil
}

// EditPayloadAs seals a message again at its next revision as this
// member's Dens opens it, changed by edit, past the checks its edits make,
// as an older Dens edits, for the tests of package denclient_test.
func EditPayloadAs(m *Manager, denID, channel, messageID string, edit func(*denproto.DMPayload)) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	ctx := context.Background()
	current, err := c.message(ctx, channel, messageID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	p, _, locked := c.openPayloadLocked(current.ChannelID, current.AuthorID, current.KeyID, current.Nonce, current.Revision, current.Sealed)
	c.mu.Unlock()
	if locked != "" {
		return errors.New("it doesn't open: " + locked)
	}
	edit(&p)
	keyID, sealed, err := c.sealMessage(ctx, channel, current.AuthorID, current.Nonce, current.Revision+1, p)
	if err != nil {
		return err
	}
	_, err = change(ctx, c, http.MethodPatch, "/api/messages/"+messageID, denproto.EditRequest{Revision: current.Revision, Sealed: sealed, KeyID: keyID})
	return err
}

// ShownAs is the tag this member's Dens gave the page with a message's
// file as it last opened that message, or "" if it holds none, for the
// tests of package denclient_test.
func ShownAs(m *Manager, denID, messageID, fileID string) string {
	c, err := m.find(denID)
	if err != nil {
		return ""
	}
	message, _ := denproto.ParseID(messageID)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dmFiles[fileID][message].as
}
