package denclient

import (
	"context"

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
