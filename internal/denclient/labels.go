package denclient

import (
	"context"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Label is this device's label, which it signs in to dens under and tells
// those it's in.
func (m *Manager) Label() string {
	m.labelMu.Lock()
	defer m.labelMu.Unlock()
	return m.label
}

// SetLabel names this device anew, and tells each den it's connected to
// that keeps device names; the rest hear as they connect.
func (m *Manager) SetLabel(label string) {
	m.labelMu.Lock()
	m.label = label
	m.labelMu.Unlock()
	m.mu.Lock()
	conns := append([]*conn{}, m.conns...)
	m.mu.Unlock()
	for _, c := range conns {
		c.wakeLabel()
	}
}

// labelWork tells the den this device's label as each connection comes
// up and as the device is renamed. It reads the label as it sends, one
// request at a time, so the den keeps the latest; the den takes an
// unchanged label as nothing.
func (c *conn) labelWork(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.labelDue:
		}
		c.mu.Lock()
		ready := c.ws != nil && c.den != nil && c.den.deviceNames
		c.mu.Unlock()
		if !ready {
			continue
		}
		key := denproto.ID(c.j.key.Public()).String()
		err := c.call(ctx, http.MethodPatch, "/api/me/devices/"+key, denproto.DeviceLabelRequest{Label: c.m.Label()}, nil)
		if err != nil && ctx.Err() == nil {
			c.m.log.Warnf("tell a den this device's label: %v", logged(err))
		}
	}
}

// wakeLabel has labelWork tell the den this device's label.
func (c *conn) wakeLabel() {
	select {
	case c.labelDue <- struct{}{}:
	default:
	}
}
