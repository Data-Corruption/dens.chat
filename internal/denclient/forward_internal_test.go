package denclient

import (
	"context"
	"errors"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// PayloadOf opens a sealed message as this member's Dens has it at the den
// now, for the tests of package denclient_test: what its author sealed.
func PayloadOf(m *Manager, denID, channelID, messageID string) (denproto.DMPayload, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.DMPayload{}, err
	}
	msg, err := c.message(context.Background(), channelID, messageID)
	if err != nil {
		return denproto.DMPayload{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p, _, locked := c.openPayloadLocked(msg.ChannelID, msg.AuthorID, msg.KeyID, msg.Nonce, msg.Revision, msg.Sealed)
	if locked != "" {
		return p, errors.New(locked)
	}
	return p, nil
}

// ForgetForward has a den's state say the den keeps no forward's mark, as
// one older than forwards does, until its next snapshot or update, for the
// tests of package denclient_test.
func ForgetForward(m *Manager, denID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return ErrNotStarted
	}
	c.den.forward = false
	return nil
}
