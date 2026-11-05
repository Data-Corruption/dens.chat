package denclient

import (
	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Disappearing messages (M16.4): the den deletes a DM's or a group's
// message at the time its members set, and tells those connected, as it
// tells them of any delete. The cache lets its copy go at that time too,
// by this computer's clock, whether the den can be reached or not, so a
// den offline shows none of it; this takes it off the pages and out of the
// notifications, with the keys of the files on it, as a delete would. A
// message whose time passed doesn't notify, and history leaves it out.

// disappeared hears of the messages the cache let go of as their time
// came.
func (m *Manager) disappeared(gone []cache.Gone) {
	byDen := map[string][]cache.Gone{}
	var dens []string
	for _, g := range gone {
		den := denproto.Bytes(g.DenID).String()
		if _, ok := byDen[den]; !ok {
			dens = append(dens, den)
		}
		byDen[den] = append(byDen[den], g)
	}
	for _, den := range dens {
		var events []denproto.Event
		ids := map[int64]bool{}
		for _, g := range byDen[den] {
			id, channel := denproto.FormatID(g.ID), denproto.FormatID(g.Channel)
			if e, err := denproto.NewEvent(denproto.EventMessageDeleted, 0, denproto.MessageDeleted{ID: id, ChannelID: channel}); err == nil {
				events = append(events, e)
			}
			if m.Notices != nil {
				m.Notices.Deleted(den, channel, id)
			}
			ids[g.ID] = true
		}
		if c, err := m.find(den); err == nil {
			c.forgetFiles(ids)
		}
		m.publish(PageEvent{DenID: den, Events: events})
	}
}

// forgetFiles forgets the keys of the files on messages that went.
func (c *conn) forgetFiles(messages map[int64]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, f := range c.dmFiles {
		if messages[f.message] {
			delete(c.dmFiles, id)
		}
	}
}

// pastTime reports whether a message's time to disappear came, by this
// computer's clock.
func (m *Manager) pastTime(msg denproto.Message) bool {
	return msg.ExpiresAt != 0 && msg.ExpiresAt <= m.wall().UnixMilli()
}
