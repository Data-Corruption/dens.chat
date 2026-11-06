package denclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Notes to self (M17.3). A member's notes in a den are a channel only they
// see, which the den makes as they first open it. Its messages and files
// are sealed here as a DM's are, under a key this service makes from the
// member's seal, so every device holding the seal reads them and the den
// can't: see denproto's notes.go. The den sends them only on a socket
// speaking version 3 of the protocol.

var errNotesOff = inputError(errors.New("this den runs an older Dens, which has no notes: its owner can update it"))

// OpenNotes returns the member's notes in a den, which the den makes the
// first time.
func (m *Manager) OpenNotes(ctx context.Context, denID string) (denproto.Channel, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Channel{}, err
	}
	c.mu.Lock()
	started, on, me := c.den != nil, c.den != nil && c.den.notes, c.meLocked()
	c.mu.Unlock()
	if !started {
		return denproto.Channel{}, ErrNotStarted
	}
	if !on {
		return denproto.Channel{}, errNotesOff
	}
	var ch denproto.Channel
	if err := c.call(ctx, http.MethodPost, "/api/notes", nil, &ch); err != nil {
		return denproto.Channel{}, err
	}
	ch, ok := cleanChannel(ch)
	if !ok || ch.Kind != denproto.KindNotes || ch.Members[0] != me {
		return denproto.Channel{}, errors.New("the den's answer is malformed")
	}
	return ch, nil
}
