package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// checkEditors checks the members named to edit a message; the den checks
// that each can.
func checkEditors(ids []string) error {
	if len(ids) > denproto.MaxEditors {
		return inputError(fmt.Errorf("at most %d others can edit a message", denproto.MaxEditors))
	}
	for _, id := range ids {
		if err := checkID("member", id); err != nil {
			return err
		}
	}
	return nil
}

// HistoryQuery selects a page of messages: the newest, or before, after or
// around a message ID.
type HistoryQuery struct {
	Before, After, Around string
	Limit                 int
}

// History fetches a page of a channel's messages from the den. A DM's
// come opened, or saying why they can't be.
func (m *Manager) History(ctx context.Context, denID, channelID string, q HistoryQuery) (PageHistory, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageHistory{}, err
	}
	h, err := c.history(ctx, channelID, q)
	if err != nil {
		return PageHistory{}, err
	}
	out := PageHistory{Messages: make([]PageMessage, 0, len(h.Messages)), HasOlder: h.HasOlder, HasNewer: h.HasNewer}
	for _, msg := range h.Messages {
		pm := c.open(msg)
		// The page matches a nonce only with its own sends, as it arrives.
		pm.Nonce = nil
		out.Messages = append(out.Messages, pm)
	}
	return out, nil
}

// history fetches a page of a channel's messages as the den sends them.
func (c *conn) history(ctx context.Context, channelID string, q HistoryQuery) (denproto.History, error) {
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

// message fetches one message of a channel as the den has it now.
func (c *conn) message(ctx context.Context, channelID, messageID string) (denproto.Message, error) {
	h, err := c.history(ctx, channelID, HistoryQuery{Around: messageID, Limit: 1})
	if err != nil {
		return denproto.Message{}, err
	}
	for _, msg := range h.Messages {
		if msg.ID == messageID {
			return msg, nil
		}
	}
	return denproto.Message{}, inputError(errors.New("that message is gone"))
}

// Send posts a message. The page picks the nonce, so a retry after a lost
// answer doesn't post twice. In a DM, its text and files go sealed.
func (m *Manager) Send(ctx context.Context, denID, channelID string, req denproto.SendRequest) (PageMessage, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageMessage{}, err
	}
	if err := checkID("channel", channelID); err != nil {
		return PageMessage{}, err
	}
	if req.ReplyTo != "" {
		if err := checkID("message", req.ReplyTo); err != nil {
			return PageMessage{}, err
		}
	}
	if len(req.Nonce) != denproto.NonceBytes {
		return PageMessage{}, inputError(errors.New("a message needs a 16-byte nonce"))
	}
	if len(req.Attachments) > denproto.MaxAttachments {
		return PageMessage{}, inputError(fmt.Errorf("a message has at most %d files", denproto.MaxAttachments))
	}
	for _, id := range req.Attachments {
		if err := checkID("upload", id); err != nil {
			return PageMessage{}, err
		}
	}
	if err := checkEditors(req.Editors); err != nil {
		return PageMessage{}, err
	}
	if err := denproto.CheckMessageText(req.Text, len(req.Attachments) > 0); err != nil {
		return PageMessage{}, inputError(err)
	}
	req.Sealed, req.KeyID = nil, ""
	if _, dm := c.isDM(channelID); dm {
		p := denproto.DMPayload{Text: req.Text}
		var blobs []string
		if p.Files, blobs, err = c.takeUploads(channelID, req.Attachments); err != nil {
			return PageMessage{}, err
		}
		if req.KeyID, req.Sealed, err = c.sealMessage(channelID, c.memberID(), req.Nonce, 1, p); err != nil {
			return PageMessage{}, err
		}
		req.Text, req.Attachments = "", blobs
	}
	var msg denproto.Message
	if err := c.call(ctx, http.MethodPost, "/api/channels/"+channelID+"/messages", req, &msg); err != nil {
		return PageMessage{}, err
	}
	if denproto.CheckMessage(msg) != nil || msg.ChannelID != channelID {
		return PageMessage{}, errMalformed
	}
	c.sentUploads(req.Attachments)
	return c.open(msg), nil
}

// memberID is this member's ID.
func (c *conn) memberID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.meLocked()
}

// ErrEditConflict reports an edit made against a stale revision.
type ErrEditConflict struct{ Current PageMessage }

func (e *ErrEditConflict) Error() string { return "the message changed since you started editing" }

// Edit replaces a message's text, and its editors when set. If someone
// changed it since revision, it returns *ErrEditConflict with the message
// as it is now. The page names the message's channel, which a DM's edit
// takes: the message is sealed again, with its files, at the next
// revision.
func (m *Manager) Edit(ctx context.Context, denID, channelID, messageID string, req denproto.EditRequest) (PageMessage, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageMessage{}, err
	}
	if err := checkID("message", messageID); err != nil {
		return PageMessage{}, err
	}
	// The den knows whether the message has files, which may leave it
	// without text.
	if err := denproto.CheckMessageText(req.Text, true); err != nil {
		return PageMessage{}, inputError(err)
	}
	if req.Editors != nil {
		if err := checkEditors(*req.Editors); err != nil {
			return PageMessage{}, err
		}
	}
	req.Sealed, req.KeyID, req.Unedited = nil, "", false
	if _, dm := c.isDM(channelID); dm {
		return c.editDM(ctx, channelID, messageID, req.Revision, req.Editors, func(p *denproto.DMPayload) (bool, error) {
			if err := denproto.CheckMessageText(req.Text, len(p.Files) > 0); err != nil {
				return false, inputError(err)
			}
			changed := p.Text != req.Text
			p.Text = req.Text
			return changed, nil
		})
	}
	return change(ctx, c, http.MethodPatch, "/api/messages/"+messageID, req)
}

// editDM changes a DM message: it opens the message as it is now, lets
// edit change what it seals, which reports whether that changed the text,
// and seals it again at the next revision. A revision other than the
// current one is a conflict, as on the den, and so is errConflict from
// edit; revision 0 takes the current one.
func (c *conn) editDM(ctx context.Context, channelID, messageID string, revision int, editors *[]string,
	edit func(*denproto.DMPayload) (bool, error)) (PageMessage, error) {
	current, err := c.message(ctx, channelID, messageID)
	if err != nil {
		return PageMessage{}, err
	}
	c.mu.Lock()
	if c.den == nil {
		c.mu.Unlock()
		return PageMessage{}, ErrNotStarted
	}
	p, locked := c.openPayloadLocked(current.ChannelID, current.AuthorID, current.KeyID, current.Nonce, current.Revision, current.Sealed)
	opened := c.openLocked(current)
	c.mu.Unlock()
	if locked != "" {
		return PageMessage{}, inputError(errors.New("this device can't open that message, so it can't change it"))
	}
	if revision == 0 {
		revision = current.Revision
	}
	if current.Revision != revision {
		return PageMessage{}, &ErrEditConflict{Current: opened}
	}
	changed, err := edit(&p)
	if errors.Is(err, errConflict) {
		return PageMessage{}, &ErrEditConflict{Current: opened}
	}
	if err != nil {
		return PageMessage{}, err
	}
	keyID, sealed, err := c.sealMessage(channelID, current.AuthorID, current.Nonce, revision+1, p)
	if err != nil {
		return PageMessage{}, err
	}
	req := denproto.EditRequest{Revision: revision, Editors: editors, Sealed: sealed, KeyID: keyID, Unedited: !changed}
	return change(ctx, c, http.MethodPatch, "/api/messages/"+messageID, req)
}

// SetTask checks or unchecks task n of a message. If the message changed
// so that task n isn't the text the member ticked, it returns
// *ErrEditConflict with the message as it is now. The den can't read a
// DM's text, so a DM's tick is an edit this service makes.
func (m *Manager) SetTask(ctx context.Context, denID, channelID, messageID string, n int, req denproto.TaskRequest) (PageMessage, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageMessage{}, err
	}
	if err := checkID("message", messageID); err != nil {
		return PageMessage{}, err
	}
	if n < 0 || n > denproto.MaxTextRunes || len(req.Text) > denproto.MaxTextBytes {
		return PageMessage{}, inputError(errors.New("no such task"))
	}
	if _, dm := c.isDM(channelID); !dm {
		return change(ctx, c, http.MethodPost, "/api/messages/"+messageID+"/tasks/"+strconv.Itoa(n), req)
	}
	// Ticks on other tasks may land meanwhile; each is its own edit, so
	// this one tries again against what they left. A task whose text moved
	// is a conflict, as on the den.
	for attempt := 0; ; attempt++ {
		moved := false
		msg, err := c.editDM(ctx, channelID, messageID, 0, nil, func(p *denproto.DMPayload) (bool, error) {
			tasks := denproto.Tasks(p.Text)
			if n >= len(tasks) || tasks[n].Text != req.Text {
				moved = true
				return false, errConflict
			}
			p.Text, _ = denproto.SetTask(p.Text, n, req.Checked)
			return false, nil
		})
		var conflict *ErrEditConflict
		if moved || !errors.As(err, &conflict) || attempt == 2 {
			return msg, err
		}
	}
}

// errConflict is an edit that no longer fits the message.
var errConflict = errors.New("the message changed")

// change sends an edit or a tick, and reads the message it leaves, or the
// conflict that carries it.
func change(ctx context.Context, c *conn, method, path string, req any) (PageMessage, error) {
	var msg denproto.Message
	err := c.call(ctx, method, path, req, &msg)
	var perr *denproto.Error
	if errors.As(err, &perr) && perr.Code == denproto.CodeEditConflict {
		var body struct {
			Message denproto.Message `json:"message"`
		}
		if json.Unmarshal(perr.Body, &body) != nil || denproto.CheckMessage(body.Message) != nil {
			return PageMessage{}, errMalformed
		}
		return PageMessage{}, &ErrEditConflict{Current: c.open(body.Message)}
	}
	if err != nil {
		return PageMessage{}, err
	}
	if denproto.CheckMessage(msg) != nil {
		return PageMessage{}, errMalformed
	}
	return c.open(msg), nil
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
