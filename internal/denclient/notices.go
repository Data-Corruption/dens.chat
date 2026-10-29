package denclient

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/notify"
)

// Notices hears what the dens' events say of the member's notifications
// (M12.3): each message another member sends, as it arrives, and what
// takes a notification back, the member reading the channel, from any of
// their devices, or the message, its channel or its den going. It's
// notify.Hub, or nil, as in most tests.
type Notices interface {
	Message(notify.Message)
	Cleared(den, channel, through string)
	Deleted(den, channel, message string)
	ChannelGone(den, channel string)
	Forgot(den string)
}

// noticeLocked is what a message that just arrived may notify the member
// of, or nil for one of their own, or one already read, as when the den
// sends it again after the connection drops. locked is a DM's message this
// device couldn't open. The caller holds c.mu.
func (c *conn) noticeLocked(m denproto.Message, locked bool) *notify.Message {
	me := c.profile.Member
	if c.m.Notices == nil || c.den == nil || m.AuthorID == me.ID {
		return nil
	}
	ch, ok := c.den.channels[m.ChannelID]
	if !ok {
		return nil
	}
	if r, ok := c.den.reads[m.ChannelID]; ok && r.ReadPosition != "" && compareIDs(m.ID, r.ReadPosition) <= 0 {
		return nil
	}
	author := c.den.members[m.AuthorID].DisplayName
	if author == "" {
		author = "A member"
	}
	n := &notify.Message{Den: c.j.denID.String(), DenName: c.profile.Name, Channel: m.ChannelID, ChannelName: ch.Name,
		DM: ch.Kind == denproto.KindDM, ID: m.ID, Author: author, Text: m.Text, Files: len(m.Attachments)}
	if locked {
		n.Text = ""
	} else {
		n.Mention = slices.Contains(denproto.Mentions(m.Text), strings.ToLower(me.Username))
	}
	return n
}

// tellNotices passes on what an event the den sent, now applied, says of
// the member's notifications: notice, a message that may notify them, and
// what takes one back.
func (c *conn) tellNotices(e denproto.Event, out denproto.Event, notice *notify.Message, own *denproto.Message) {
	n := c.m.Notices
	if n == nil {
		return
	}
	den := c.j.denID.String()
	if notice != nil {
		n.Message(*notice)
	}
	if own != nil {
		// The member's own message, from another of their devices, reads
		// the channel up to it.
		n.Cleared(den, own.ChannelID, own.ID)
	}
	switch e.T {
	case denproto.EventReadStateUpdated:
		var r denproto.ReadState
		if json.Unmarshal(out.D, &r) == nil && r.ReadPosition != "" {
			n.Cleared(den, r.ChannelID, r.ReadPosition)
		}
	case denproto.EventMessageDeleted:
		var d denproto.MessageDeleted
		if json.Unmarshal(out.D, &d) == nil {
			n.Deleted(den, d.ChannelID, d.ID)
		}
	case denproto.EventChannelDeleted:
		var d denproto.ChannelDeleted
		if json.Unmarshal(out.D, &d) == nil {
			n.ChannelGone(den, d.ID)
		}
	case denproto.EventMessagesExpired:
		var x denproto.MessagesExpired
		if json.Unmarshal(out.D, &x) == nil {
			n.Cleared(den, "", x.Through)
		}
	}
}

// tellReadNotices takes back what the member read while this install was
// away, as a new snapshot says.
func (c *conn) tellReadNotices(reads []denproto.ReadState) {
	n := c.m.Notices
	if n == nil {
		return
	}
	for _, r := range reads {
		if r.ReadPosition != "" {
			n.Cleared(c.j.denID.String(), r.ChannelID, r.ReadPosition)
		}
	}
}
