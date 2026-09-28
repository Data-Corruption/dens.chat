package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// View returns a joined den's state for the page.
func (m *Manager) View(denID string) (View, error) {
	c, err := m.find(denID)
	if err != nil {
		return View{}, err
	}
	status := c.status()
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.den
	if state == nil {
		state = newState()
	}
	return state.view(status, c.profile.Member), nil
}

func checkID(what, id string) error {
	if _, err := denproto.ParseID(id); err != nil {
		return inputError(errors.New("invalid " + what + " ID"))
	}
	return nil
}

// HistoryQuery selects a page of messages: the newest, or before, after or
// around a message ID.
type HistoryQuery struct {
	Before, After, Around string
	Limit                 int
}

// History fetches a page of a channel's messages from the den.
func (m *Manager) History(ctx context.Context, denID, channelID string, q HistoryQuery) (denproto.History, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.History{}, err
	}
	if err := checkID("channel", channelID); err != nil {
		return denproto.History{}, err
	}
	values := url.Values{}
	for name, v := range map[string]string{"before": q.Before, "after": q.After, "around": q.Around} {
		if v != "" {
			if err := checkID("message", v); err != nil {
				return denproto.History{}, err
			}
			values.Set(name, v)
		}
	}
	if q.Limit != 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	path := "/api/channels/" + channelID + "/messages"
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	var h denproto.History
	if err := c.call(ctx, http.MethodGet, path, nil, &h); err != nil {
		return h, err
	}
	if len(h.Messages) > denproto.MaxHistory {
		return h, errMalformed
	}
	for _, msg := range h.Messages {
		if denproto.CheckMessage(msg) != nil || msg.ChannelID != channelID {
			return h, errMalformed
		}
	}
	if h.Messages == nil {
		h.Messages = []denproto.Message{}
	}
	return h, nil
}

// Send posts a message. The page picks the nonce, so a retry after a lost
// answer doesn't post twice.
func (m *Manager) Send(ctx context.Context, denID, channelID string, req denproto.SendRequest) (denproto.Message, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Message{}, err
	}
	if err := checkID("channel", channelID); err != nil {
		return denproto.Message{}, err
	}
	if req.ReplyTo != "" {
		if err := checkID("message", req.ReplyTo); err != nil {
			return denproto.Message{}, err
		}
	}
	if len(req.Nonce) != denproto.NonceBytes {
		return denproto.Message{}, inputError(errors.New("a message needs a 16-byte nonce"))
	}
	if err := denproto.CheckText(req.Text); err != nil {
		return denproto.Message{}, inputError(err)
	}
	var msg denproto.Message
	if err := c.call(ctx, http.MethodPost, "/api/channels/"+channelID+"/messages", req, &msg); err != nil {
		return msg, err
	}
	if denproto.CheckMessage(msg) != nil {
		return msg, errMalformed
	}
	return msg, nil
}

// ErrEditConflict reports an edit made against a stale revision.
type ErrEditConflict struct{ Current denproto.Message }

func (e *ErrEditConflict) Error() string { return "the message changed since you started editing" }

// Edit replaces a message's text. If someone changed it since revision, it
// returns *ErrEditConflict with the message as it is now.
func (m *Manager) Edit(ctx context.Context, denID, messageID string, req denproto.EditRequest) (denproto.Message, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Message{}, err
	}
	if err := checkID("message", messageID); err != nil {
		return denproto.Message{}, err
	}
	if err := denproto.CheckText(req.Text); err != nil {
		return denproto.Message{}, inputError(err)
	}
	var msg denproto.Message
	err = c.call(ctx, http.MethodPatch, "/api/messages/"+messageID, req, &msg)
	var perr *denproto.Error
	if errors.As(err, &perr) && perr.Code == denproto.CodeEditConflict {
		var body struct {
			Message denproto.Message `json:"message"`
		}
		if json.Unmarshal(perr.Body, &body) != nil || denproto.CheckMessage(body.Message) != nil {
			return msg, errMalformed
		}
		return msg, &ErrEditConflict{Current: body.Message}
	}
	if err != nil {
		return msg, err
	}
	if denproto.CheckMessage(msg) != nil {
		return msg, errMalformed
	}
	return msg, nil
}

// DeleteMessage deletes a message for good.
func (m *Manager) DeleteMessage(ctx context.Context, denID, messageID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("message", messageID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/messages/"+messageID, nil, nil)
}

// MarkRead moves this member's read position in a channel forward.
func (m *Manager) MarkRead(ctx context.Context, denID, channelID, messageID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("channel", channelID); err != nil {
		return err
	}
	if err := checkID("message", messageID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodPut, "/api/channels/"+channelID+"/read", denproto.ReadRequest{MessageID: messageID}, nil)
}

// Manage creates (POST, empty id), changes (PATCH) or deletes (DELETE) a
// channel or group; kind is "channels" or "groups". The den announces the
// change as events, so the result comes back through the stream.
func (m *Manager) Manage(ctx context.Context, denID, kind, method, id string, req any) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if kind != "channels" && kind != "groups" {
		return inputError(errors.New("unknown kind"))
	}
	path := "/api/" + kind
	if method != http.MethodPost {
		if err := checkID("channel or group", id); err != nil {
			return err
		}
		path += "/" + id
	}
	if method == http.MethodDelete {
		req = nil
	}
	return c.call(ctx, method, path, req, nil)
}
