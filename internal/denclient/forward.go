package denclient

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Forwarding. A member forwards a message's text and files to a place they
// write in, in its den or another this install is in. This service reads
// each file, from the cache or the den, uploads it to the place as the
// member's own, sealed anew where the place is sealed, and sends the
// message marked forwarded. A forward from a text channel carries its
// source (den_id, channel_id, message_id — no URL or hostname), sealed
// inside DMPayload when sent to a DM or notes; a forward from a DM or
// notes carries no source. The place's limits are checked before anything
// goes.

// ForwardTo is where a forward goes, and the nonce the page picked for it,
// so a retry after a lost answer doesn't post twice.
type ForwardTo struct {
	Den     string         `json:"den"`
	Channel string         `json:"channel"`
	Nonce   denproto.Bytes `json:"nonce"`
}

// Forward sends a message of a den's channel to the place to names. A poll
// doesn't forward, nor what this device can't open; a reply's quote,
// reactions and editors stay behind, and a spoiler stays one.
func (m *Manager) Forward(ctx context.Context, denID, channelID, messageID string, to ForwardTo) (PageMessage, error) {
	src, err := m.find(denID)
	if err != nil {
		return PageMessage{}, err
	}
	dst, err := m.find(to.Den)
	if err != nil {
		return PageMessage{}, err
	}
	if err := checkID("message", messageID); err != nil {
		return PageMessage{}, err
	}
	if err := checkID("channel", to.Channel); err != nil {
		return PageMessage{}, err
	}
	if len(to.Nonce) != denproto.NonceBytes {
		return PageMessage{}, inputError(errors.New("a message needs a 16-byte nonce"))
	}
	// A den's state from the cache says nothing of forwards, so one that's
	// offline is said to be.
	if !dst.connected() {
		return PageMessage{}, inputError(errors.New("That den is offline, so nothing goes there now."))
	}
	msg, err := src.toForward(ctx, channelID, messageID)
	if err != nil {
		return PageMessage{}, err
	}
	sealed, err := dst.takesForward(ctx, to.Channel)
	if err != nil {
		return PageMessage{}, err
	}

	// Each file opens for its size here, and again as it goes up, so none
	// waits open on the den while the others upload.
	sizes := make([]int64, len(msg.Attachments))
	for i, a := range msg.Attachments {
		f, err := m.OpenFile(ctx, denID, a.ID, FileAt{Channel: channelID, Message: messageID}, false)
		if err != nil {
			return PageMessage{}, err
		}
		sizes[i] = f.Size()
		f.Close()
	}
	if err := dst.roomFor(ctx, sizes, sealed); err != nil {
		return PageMessage{}, err
	}

	req := denproto.SendRequest{Nonce: to.Nonce, Text: msg.Text, Forwarded: true}
	if src.isTextChannel(channelID) {
		req.ForwardSource = &denproto.ForwardSource{
			DenID:     src.denIDBytes(),
			ChannelID: channelID,
			MessageID: messageID,
		}
	}
	// What went up waits unsent within the hour if the forward goes no
	// further; it goes at once instead.
	sent := false
	defer func() {
		if sent || len(req.Attachments) == 0 {
			return
		}
		drop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for _, id := range req.Attachments {
			if err := m.DropUpload(drop, to.Den, id); err != nil {
				m.log.Infof("A forward's upload waits out its hour: %v", err)
			}
		}
	}()
	for _, a := range msg.Attachments {
		up, err := m.forwardFile(ctx, denID, FileAt{Channel: channelID, Message: messageID}, a, dst, to.Channel)
		if err != nil {
			return PageMessage{}, err
		}
		req.Attachments = append(req.Attachments, up.ID)
		if a.Spoiler {
			req.Spoilers = append(req.Spoilers, up.ID)
		}
		if strings.HasPrefix(up.Type, "video/") && up.Width > 0 && up.Thumb == nil && a.Thumb != nil {
			m.carryPreview(ctx, denID, FileAt{Channel: channelID, Message: messageID}, a.ID, dst, up.ID)
		}
	}
	out, err := dst.sendMessage(ctx, to.Channel, req)
	sent = err == nil
	return out, err
}

// toForward reads a message to forward, from the cache or the den, as
// History does, and opens it.
func (c *conn) toForward(ctx context.Context, channelID, messageID string) (PageMessage, error) {
	if err := checkID("channel", channelID); err != nil {
		return PageMessage{}, err
	}
	q := HistoryQuery{Around: messageID, Limit: 1}
	var h denproto.History
	var err error
	if c.cached() {
		h, err = c.cachedHistory(ctx, channelID, q)
	} else {
		h, err = c.history(ctx, channelID, q)
	}
	if err != nil {
		return PageMessage{}, err
	}
	i := slices.IndexFunc(h.Messages, func(m denproto.Message) bool { return m.ID == messageID })
	if i < 0 || c.m.pastTime(h.Messages[i]) {
		return PageMessage{}, inputError(errors.New("That message is gone."))
	}
	c.fetchKeys(ctx, channelID, h.Messages[i:i+1])
	msg := c.open(h.Messages[i])
	switch {
	case msg.Locked != "":
		return PageMessage{}, inputError(errors.New("This device can't open that message, so it can't forward it."))
	case msg.Poll != nil:
		return PageMessage{}, inputError(errors.New("A poll doesn't forward."))
	}
	return msg, nil
}

// forwardFile uploads one of a forward's files, where it is, to the place
// channel names, as the member's own.
func (m *Manager) forwardFile(ctx context.Context, denID string, at FileAt, a denproto.File, dst *conn, channel string) (Uploaded, error) {
	f, err := m.OpenFile(ctx, denID, a.ID, at, false)
	if err != nil {
		return Uploaded{}, err
	}
	defer f.Close()
	return m.uploadTo(ctx, dst, channel, a.Name, f.Size(), io.NewSectionReader(f, 0, f.Size()), "")
}

// takesForward says whether a place is sealed, or why nothing forwards to
// it, before a file goes up: this Dens holds it as neither a text channel
// nor a sealed place, so its files would go unsealed; its den keeps no
// channel's mark; or it has no key to send under, as a DM that holds.
func (c *conn) takesForward(ctx context.Context, channel string) (bool, error) {
	c.mu.Lock()
	if c.den == nil {
		c.mu.Unlock()
		return false, ErrNotStarted
	}
	sealed, kind, forwards := c.privateLocked(channel), c.den.channels[channel].Kind, c.den.forward
	c.mu.Unlock()
	switch {
	case sealed:
		_, key, err := c.sendKey(ctx, channel)
		clear(key)
		return true, err
	case kind != denproto.KindText:
		return false, inputError(errors.New("messages go in text channels, DMs and notes"))
	case !forwards:
		return false, inputError(errors.New("That den is too old to mark a forward in its channels."))
	}
	return false, nil
}

func (c *conn) denIDBytes() denproto.Bytes {
	return append(denproto.Bytes(nil), c.j.denID...)
}

// isTextChannel reports whether channel is a text channel (never a DM or
// notes), which determines whether forwards from it carry a ForwardSource.
func (c *conn) isTextChannel(channel string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil || c.privateLocked(channel) {
		return false
	}
	return c.den.channels[channel].Kind == denproto.KindText || c.den.kinds[channel] == denproto.KindText
}

func (c *conn) supportsForwardLinks() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.den != nil && c.den.forwardLinks
}

// roomFor checks that files of those sizes fit the den's limits as a
// message's files, sealed or not, and the member's space there and the
// den's.
func (c *conn) roomFor(ctx context.Context, sizes []int64, sealed bool) error {
	if len(sizes) == 0 {
		return nil
	}
	limits := c.limits()
	var need int64
	for _, size := range sizes {
		if sealed {
			size = vault.SealedSize(size)
		}
		if limits.FileSize > 0 && size > limits.FileSize {
			return ErrTooLarge
		}
		need += size
	}
	st, err := c.storage(ctx)
	if err != nil {
		return err
	}
	switch {
	case limits.MemberStorage > 0 && st.Used+need > limits.MemberStorage:
		return inputError(errors.New("These files would take more than your space in that den."))
	case limits.DenStorage > 0 && st.DenUsed+need > limits.DenStorage:
		return inputError(errors.New("That den has no room for these files."))
	}
	return nil
}

// carryPreview gives a forward's video the preview its original had, which
// a page drew where the media module couldn't, as SetThumb does. One that
// doesn't take leaves the video without, as a page that couldn't draw one
// sends it.
func (m *Manager) carryPreview(ctx context.Context, denID string, at FileAt, fileID string, dst *conn, uploadID string) {
	f, err := m.OpenFile(ctx, denID, fileID, at, true)
	if err == nil {
		defer f.Close()
		var data []byte
		if data, err = io.ReadAll(io.LimitReader(f, denproto.MaxPreviewUpload+1)); err == nil {
			_, err = dst.setThumb(ctx, uploadID, data)
		}
	}
	if err != nil && ctx.Err() == nil {
		m.log.Infof("A forward's video goes without its preview: %v", err)
	}
}
