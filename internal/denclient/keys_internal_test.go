package denclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// ForgetSignedKeys drops the signed keys a Dens holds for a den, as one
// whose socket hasn't brought them yet doesn't hold them, for the tests of
// package denclient_test.
func ForgetSignedKeys(m *Manager, denID string) {
	c, err := m.find(denID)
	if err != nil {
		panic(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.den.signed)
	clear(c.verdicts)
}

// SealAs seals a group's message as this member's Dens would at its first
// revision, but under another member's name, as a member of the group
// working with the den could, for the tests of package denclient_test.
func SealAs(m *Manager, denID, channel, author, text string) (keyID string, nonce, sealed denproto.Bytes, err error) {
	c, err := m.find(denID)
	if err != nil {
		return "", nil, nil, err
	}
	nonce = denproto.Random(16)
	keyID, sealed, err = c.sealMessage(context.Background(), channel, author, nonce, 1, denproto.DMPayload{Text: text})
	return keyID, nonce, sealed, err
}

// SealNameAs seals a name under a group's live key as this member's Dens
// holds it, which no change of theirs carries, as a member working with the
// den could, for the tests of package denclient_test.
func SealNameAs(m *Manager, denID, channel, name string) (denproto.SealedName, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.SealedName{}, err
	}
	keyID, key, err := c.sendKey(context.Background(), channel)
	if err != nil {
		return denproto.SealedName{}, err
	}
	defer clear(key)
	c.mu.Lock()
	kid := c.den.signed[keyID].KID
	c.mu.Unlock()
	cid, _ := denproto.ParseID(channel)
	return denproto.SealGroupName(key, c.j.denID, cid, kid, name)
}

// EditAs edits a group's message as this member's Dens would seal it at
// the next revision, past the checks its edits make: with the grant it
// opened, or with none, as v1.15.0 sealed every edit, for the tests of
// package denclient_test.
func EditAs(m *Manager, denID, channel, messageID, text string, carry bool) error {
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
	p.Text = text
	if !carry {
		p.Grant = nil
	}
	keyID, sealed, err := c.sealMessage(ctx, channel, current.AuthorID, current.Nonce, current.Revision+1, p)
	if err != nil {
		return err
	}
	_, err = change(ctx, c, http.MethodPatch, "/api/messages/"+messageID, denproto.EditRequest{Revision: current.Revision, Sealed: sealed, KeyID: keyID})
	return err
}

// SealRevisionAs seals text as a revision of a message, signed by this
// member with no grant, as a member working with the den could put other
// words in a revision's place, for the tests of package denclient_test.
func SealRevisionAs(m *Manager, denID, channel, author string, nonce denproto.Bytes, revision int, text string) (denproto.Bytes, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	_, sealed, err := c.sealMessage(context.Background(), channel, author, nonce, revision, denproto.DMPayload{Text: text})
	return sealed, err
}

// SignNameAs signs a sealed name by this member's identity, as the maker
// of a key that carries it does, for the tests of package denclient_test.
func SignNameAs(m *Manager, denID, channel string, n denproto.SealedName) (denproto.Bytes, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	cid, _ := denproto.ParseID(channel)
	var sig denproto.Bytes
	err = c.ownIdentity(func(ik *denproto.IdentityKeys) error {
		sig = ik.SignGroupName(c.j.denID, cid, n)
		return nil
	})
	return sig, err
}
