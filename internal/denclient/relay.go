package denclient

import (
	"bytes"
	"errors"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Relay (M20) links this install with the member's others across their
// network, and the dens only introduce them: the boxes between the
// member's devices are sealed and opened here, with their seal, and what's
// in them is Relay's.

// Relay hears what Relay needs of the dens: a box from another of the
// member's devices, opened, by the den it came through and the device it
// came from; and that the dens, the member's calls in them or this
// install's own call changed. Neither may block.
type Relay interface {
	Box(den string, from denproto.Bytes, note []byte)
	Changed()
}

// ErrNoRelay is a box for a den that passes none, or one this device has
// no seal for.
var ErrNoRelay = errors.New("this den doesn't pass boxes between your devices here")

func (m *Manager) relayChanged() {
	if m.Relay != nil {
		m.Relay.Changed()
	}
}

// RelayDens lists the dens, connected now, that pass boxes between the
// member's devices, where this device holds their seal.
func (m *Manager) RelayDens() []string {
	m.mu.Lock()
	conns := slices.Clone(m.conns)
	m.mu.Unlock()
	var dens []string
	for _, c := range conns {
		c.mu.Lock()
		ok := c.state == StateConnected && c.ws != nil && c.den != nil && c.den.relay && c.j.seal != nil
		c.mu.Unlock()
		if ok {
			dens = append(dens, c.j.denID.String())
		}
	}
	return dens
}

// MemberCalls lists the dens where the member is in a call, as each last
// said, whichever of their devices holds it.
func (m *Manager) MemberCalls() []string {
	m.mu.Lock()
	conns := slices.Clone(m.conns)
	m.mu.Unlock()
	var dens []string
	for _, c := range conns {
		c.mu.Lock()
		in := false
		if c.den != nil && c.state == StateConnected {
			me := c.profile.Member.ID
			for _, call := range c.den.calls {
				in = in || slices.ContainsFunc(call.Members, func(x denproto.CallMember) bool { return x.ID == me })
			}
		}
		c.mu.Unlock()
		if in {
			dens = append(dens, c.j.denID.String())
		}
	}
	return dens
}

// CallDen is the den of the call this install holds, or "" for none.
func (m *Manager) CallDen() string {
	m.callMu.Lock()
	defer m.callMu.Unlock()
	if m.call == nil {
		return ""
	}
	return m.call.den
}

// SendRelay seals a note for another of the member's devices, by its key
// ID, or for every other where to is nil, and sends it through a den.
func (m *Manager) SendRelay(den string, to denproto.Bytes, note []byte) error {
	c, err := m.find(den)
	if err != nil {
		return err
	}
	from := denproto.ID(c.j.key.Public())
	c.mu.Lock()
	member, err := denproto.ParseID(c.profile.Member.ID)
	if err != nil || c.j.seal == nil || c.den == nil || !c.den.relay {
		c.mu.Unlock()
		return ErrNoRelay
	}
	var box denproto.Bytes
	err = c.j.seal.Use(func(seal []byte) error {
		key, err := denproto.RelayKey(seal, c.j.denID)
		if err != nil {
			return err
		}
		defer clear(key)
		box, err = denproto.SealRelayBox(key, denproto.RelayBoxAD(c.j.denID, member, from, to), note)
		return err
	})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return c.sendFrame(denproto.EventRelaySignal, denproto.RelaySignal{To: to, Box: box})
}

// relayBox opens a box from another of the member's devices, and hands
// its note to Relay. One for another device, from this one, or that
// doesn't open goes no further.
func (c *conn) relayBox(b denproto.RelaySignalEvent) {
	r := c.m.Relay
	if r == nil {
		return
	}
	own := denproto.ID(c.j.key.Public())
	if bytes.Equal(b.From, own) || (len(b.To) > 0 && !bytes.Equal(b.To, own)) {
		return
	}
	c.mu.Lock()
	member, err := denproto.ParseID(c.profile.Member.ID)
	if err != nil || c.j.seal == nil {
		c.mu.Unlock()
		return
	}
	var note []byte
	err = c.j.seal.Use(func(seal []byte) error {
		key, err := denproto.RelayKey(seal, c.j.denID)
		if err != nil {
			return err
		}
		defer clear(key)
		note, err = denproto.OpenRelayBox(key, denproto.RelayBoxAD(c.j.denID, member, b.From, b.To), b.Box)
		return err
	})
	c.mu.Unlock()
	if err != nil {
		c.logf(false, "a box from another of the member's devices didn't open")
		return
	}
	r.Box(c.j.denID.String(), b.From, note)
}
