package denclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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
	v := state.view(status, c.profile.Member)
	v.Identities = c.pageIdentitiesLocked()
	v.HasGroupDMs, v.GroupDMs = c.groupsOnLocked(), c.pageGroupsLocked()
	v.HasNotes = c.den != nil && c.den.notes
	return v, nil
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

// History fetches a page of a channel's messages: from the cache where it
// holds them, and from the den otherwise. A DM's come opened, or
// saying why they can't be.
func (m *Manager) History(ctx context.Context, denID, channelID string, q HistoryQuery) (PageHistory, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageHistory{}, err
	}
	var h denproto.History
	if c.cached() {
		h, err = c.cachedHistory(ctx, channelID, q)
	} else {
		h, err = c.history(ctx, channelID, q)
	}
	if err != nil {
		return PageHistory{}, err
	}
	c.fetchKeys(ctx, channelID, h.Messages)
	out := PageHistory{Messages: make([]PageMessage, 0, len(h.Messages)), HasOlder: h.HasOlder, HasNewer: h.HasNewer}
	for _, msg := range h.Messages {
		// One whose time to disappear came is about to go.
		if m.pastTime(msg) {
			continue
		}
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
// answer doesn't post twice. In a DM, its text and files go sealed. Only
// Forward marks a message forwarded.
func (m *Manager) Send(ctx context.Context, denID, channelID string, req denproto.SendRequest) (PageMessage, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageMessage{}, err
	}
	req.Forwarded = false
	return c.sendMessage(ctx, channelID, req)
}

// sendMessage posts a message, as Send does. A sealed place's forwarded
// mark goes inside its seal, never to the den.
func (c *conn) sendMessage(ctx context.Context, channelID string, req denproto.SendRequest) (PageMessage, error) {
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
	if err := checkSpoilers(req.Spoilers, req.Attachments); err != nil {
		return PageMessage{}, err
	}
	if err := checkEditors(req.Editors); err != nil {
		return PageMessage{}, err
	}
	// The den takes tracking out of a channel's links itself, but can't
	// read a DM's, so the sender does, before sealing; doing it for every
	// message checks the limits on the text the den will keep.
	req.Text = denproto.CleanLinks(req.Text)
	if err := denproto.CheckMessageText(req.Text, len(req.Attachments) > 0); err != nil {
		return PageMessage{}, inputError(err)
	}
	req.Sealed, req.KeyID = nil, ""
	dm := c.private(channelID)
	var err error
	if req.Poll, err = checkPollSend(req, dm); err != nil {
		return PageMessage{}, err
	}
	if dm {
		p := denproto.DMPayload{Text: req.Text, Forwarded: req.Forwarded}
		var blobs []string
		if p.Files, blobs, err = c.takeUploads(channelID, req.Attachments); err != nil {
			return PageMessage{}, err
		}
		// A DM's spoilers go inside its seal, where the den can't read
		// them.
		for i := range p.Files {
			p.Files[i].Spoiler = slices.Contains(req.Spoilers, p.Files[i].ID)
		}
		if c.isGroup(channelID) {
			if p.Grant, err = c.grant(channelID, req.Nonce, 1, req.Editors); err != nil {
				return PageMessage{}, err
			}
		}
		if req.KeyID, req.Sealed, err = c.sealMessage(ctx, channelID, c.memberID(), req.Nonce, 1, p); err != nil {
			return PageMessage{}, err
		}
		req.Text, req.Attachments, req.Spoilers, req.Forwarded = "", blobs, nil, false
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

// checkSpoilers checks a send's spoilers: each one of its uploads,
// once.
func checkSpoilers(spoilers, uploads []string) error {
	for i, id := range spoilers {
		if !slices.Contains(uploads, id) || slices.Contains(spoilers[:i], id) {
			return inputError(errors.New("a spoiler is each one of the message's files, once"))
		}
	}
	return nil
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
	req.Text = denproto.CleanLinks(req.Text)
	if err := denproto.CheckMessageText(req.Text, true); err != nil {
		return PageMessage{}, inputError(err)
	}
	if req.Editors != nil {
		if err := checkEditors(*req.Editors); err != nil {
			return PageMessage{}, err
		}
	}
	req.Sealed, req.KeyID, req.Unedited = nil, "", false
	if c.private(channelID) {
		return c.editDM(ctx, channelID, messageID, req.Revision, req.Editors, func(p *denproto.DMPayload) (bool, error) {
			if err := denproto.CheckMessageText(req.Text, len(p.Files) > 0); err != nil {
				return false, inputError(err)
			}
			changed := p.Text != req.Text
			p.Text = req.Text
			return changed, nil
		}, false, nil)
	}
	return change(ctx, c, http.MethodPatch, "/api/messages/"+messageID, req)
}

// Keep signs a group's message that waits for its author, changed by
// someone they didn't sign for, as their own: the revision their page
// showed, by its digest, its text and files as they are, and no others to
// edit it.
func (m *Manager) Keep(ctx context.Context, denID, channelID, messageID string, shown denproto.Bytes) (PageMessage, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageMessage{}, err
	}
	if err := checkID("message", messageID); err != nil {
		return PageMessage{}, err
	}
	if len(shown) != sha256.Size || !c.isGroup(channelID) {
		return PageMessage{}, inputError(errors.New("there's nothing to keep"))
	}
	return c.editDM(ctx, channelID, messageID, 0, &[]string{}, func(*denproto.DMPayload) (bool, error) { return false, nil }, false, shown)
}

// grant signs the editors of a group's message as its author's, at the
// revision they're named at; nil for none. A grant naming its author
// checks for nobody, so the author goes from the list.
func (c *conn) grant(channel string, nonce []byte, revision int, editors []string) (*denproto.EditGrant, error) {
	author := c.memberID()
	editors = slices.DeleteFunc(slices.Clone(editors), func(e string) bool { return e == author })
	if len(editors) == 0 {
		return nil, nil
	}
	ch, _ := denproto.ParseID(channel)
	me, _ := denproto.ParseID(author)
	var g denproto.EditGrant
	err := c.ownIdentity(func(ik *denproto.IdentityKeys) error {
		var err error
		g, err = ik.SignGrant(c.j.denID, ch, me, nonce, revision, editors)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// editDM changes a DM message: it opens the message as it is now, lets
// edit change what it seals, which reports whether that changed the text,
// and seals it again at the next revision. A revision other than the
// current one is a conflict, as on the den, and so is errConflict from
// edit; revision 0 takes the current one. With files set, the edit changes
// the message's files, so it names every blob they take. In a group, an
// editor's revision carries the grant that names them, and only the
// author's names others; keep, the digest of the revision their page
// showed, signs one that waits for its author as theirs, which nothing
// else changes.
func (c *conn) editDM(ctx context.Context, channelID, messageID string, revision int, editors *[]string,
	edit func(*denproto.DMPayload) (bool, error), files bool, keep denproto.Bytes) (PageMessage, error) {
	current, err := c.message(ctx, channelID, messageID)
	if err != nil {
		return PageMessage{}, err
	}
	c.mu.Lock()
	if c.den == nil {
		c.mu.Unlock()
		return PageMessage{}, ErrNotStarted
	}
	p, _, locked := c.openPayloadLocked(current.ChannelID, current.AuthorID, current.KeyID, current.Nonce, current.Revision, current.Sealed)
	opened := c.openLocked(current)
	me := c.meLocked()
	group := c.den.channels[channelID].Kind == denproto.KindGroupDM
	c.mu.Unlock()
	if revision == 0 {
		revision = current.Revision
	}
	if current.Revision != revision {
		return PageMessage{}, &ErrEditConflict{Current: opened}
	}
	waiting := locked == lockedAuthor && current.AuthorID == me
	switch {
	case keep != nil && !bytes.Equal(keep, shownSum(current)):
		return PageMessage{}, &ErrEditConflict{Current: opened}
	case keep != nil && !waiting:
		return PageMessage{}, inputError(errors.New("there's nothing to keep"))
	case waiting && keep == nil:
		return PageMessage{}, inputError(errors.New("this message waits for you: keep it, or delete it"))
	case locked != "" && !waiting:
		return PageMessage{}, inputError(errors.New("this device can't open that message, so it can't change it"))
	case keep == nil && p.Forwarded:
		return PageMessage{}, inputError(errors.New("a forward is its sender's to delete, not to edit"))
	case !group:
	case current.AuthorID != me && (editors != nil || p.Grant == nil || !slices.Contains(p.Grant.Editors, me)):
		return PageMessage{}, inputError(errors.New("its author didn't let you edit this message"))
	case keep != nil:
		p.Grant = nil
	case editors != nil:
		if p.Grant, err = c.grant(channelID, current.Nonce, revision+1, *editors); err != nil {
			return PageMessage{}, err
		}
	}
	changed, err := edit(&p)
	if errors.Is(err, errConflict) {
		return PageMessage{}, &ErrEditConflict{Current: opened}
	}
	if err != nil {
		return PageMessage{}, err
	}
	keyID, sealed, err := c.sealMessage(ctx, channelID, current.AuthorID, current.Nonce, revision+1, p)
	if err != nil {
		return PageMessage{}, err
	}
	req := denproto.EditRequest{Revision: revision, Editors: editors, Sealed: sealed, KeyID: keyID, Unedited: !changed}
	if files {
		blobs := []string{}
		for _, f := range p.Files {
			blobs = append(blobs, blobsOf(f)...)
		}
		req.Attachments = &blobs
	}
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
	if !c.private(channelID) {
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
		}, false, nil)
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

// MarkAllRead moves this member's read position in every channel and DM
// they see in a den to its newest message.
func (m *Manager) MarkAllRead(ctx context.Context, denID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodPost, "/api/read", nil, nil)
}

// CallRecord reads a voice channel's longest call, as staff.
func (m *Manager) CallRecord(ctx context.Context, denID, channelID string) (denproto.CallRecord, error) {
	var rec denproto.CallRecord
	c, err := m.find(denID)
	if err != nil {
		return rec, err
	}
	if err := checkID("channel", channelID); err != nil {
		return rec, err
	}
	if err := c.call(ctx, http.MethodGet, "/api/channels/"+channelID+"/record", nil, &rec); err != nil {
		return rec, err
	}
	if rec.RecordMS < 0 || rec.RecordMS > denproto.MaxCallMS {
		return denproto.CallRecord{}, errMalformed
	}
	return rec, nil
}

// ResetCallRecord clears a voice channel's longest call, as staff.
func (m *Manager) ResetCallRecord(ctx context.Context, denID, channelID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("channel", channelID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/channels/"+channelID+"/record", nil, nil)
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
