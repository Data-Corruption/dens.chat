package denclient

import (
	"context"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

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
