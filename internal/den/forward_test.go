package den

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A channel's forwarded message keeps its mark, in history and its
// event, and is its author's to delete, never to edit, tick or share: it
// comes with no poll, reply or editors. Its author takes a file off it,
// which frees their space, and puts none on it. A DM's mark goes inside
// its seal, so the den takes none beside one.
func TestForwardedMessages(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	if info, _ := f.d.Info(); !info.Forward || !info.ForwardLinks {
		t.Error("the den's info doesn't advertise forward and forward_links")
	}
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	first := f.send(owner, c.ID, "the plan")
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(member.MemberID)})
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	for name, req := range map[string]struct {
		channel string
		req     denproto.SendRequest
	}{
		"a poll": {c.ID, denproto.SendRequest{Text: "Lunch?", Poll: &denproto.PollRequest{Choices: []string{"a", "b"}, Hours: 1,
			ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive}}},
		"a reply":   {c.ID, denproto.SendRequest{Text: "x", ReplyTo: first.ID}},
		"editors":   {c.ID, denproto.SendRequest{Text: "x", Editors: []string{denproto.FormatID(owner.MemberID)}}},
		"in its DM": {dm.ID, f.sealedSend(member, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x"})},
	} {
		req.req.Forwarded = true
		if req.req.Nonce == nil {
			req.req.Nonce = denproto.Random(16)
		}
		if _, err := f.d.Send(ctx, member, req.channel, req.req); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("a forward with %s: %v", name, err)
		}
	}

	m, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] the plan", Forwarded: true})
	if err != nil || !m.Forwarded {
		t.Fatalf("a forward: %+v %v", m, err)
	}
	told := false
	for _, e := range drainEvents(sub) {
		var created denproto.Message
		if e.T == denproto.EventMessageCreated && json.Unmarshal(e.D, &created) == nil && created.ID == m.ID {
			told = created.Forwarded
		}
	}
	if !told {
		t.Error("the forward's event came unmarked")
	}
	page, _ := f.d.History(ctx, owner, c.ID, HistoryQuery{})
	if last := page.Messages[len(page.Messages)-1]; last.ID != m.ID || !last.Forwarded || page.Messages[0].Forwarded {
		t.Fatalf("history: %+v", page.Messages)
	}

	if _, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "[ ] my plan"}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Errorf("an edit of a forward: %v", err)
	}
	if _, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "[ ] the plan", Editors: &[]string{denproto.FormatID(owner.MemberID)}}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Errorf("a forward shared: %v", err)
	}
	if _, err := f.d.SetTask(ctx, member, m.ID, 0, denproto.TaskRequest{Checked: true, Text: "the plan"}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Errorf("a forward's task ticked: %v", err)
	}
	if err := f.d.Delete(ctx, member, m.ID); err != nil {
		t.Errorf("its author deleting a forward: %v", err)
	}

	var ups []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		up, err := f.upload(member, name, []byte(name))
		if err != nil {
			t.Fatal(err)
		}
		ups = append(ups, up.ID)
	}
	m, err = f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "two files", Attachments: ups[:2], Forwarded: true})
	if err != nil {
		t.Fatal(err)
	}
	own, err := f.d.OwnFiles(ctx, member, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range own.Files {
		if file.Forwarded != (file.MessageID == m.ID) {
			t.Errorf("the member's own files list %s, on message %q, as forwarded: %v", file.Name, file.MessageID, file.Forwarded)
		}
	}
	one, other := ups[:1], []string{ups[0], ups[2]}
	for name, req := range map[string]denproto.EditRequest{
		"another file in one's place": {Revision: 1, Text: "two files", Attachments: &other},
		"a file off and other words":  {Revision: 1, Text: "one file", Attachments: &one},
		"a file off and editors":      {Revision: 1, Text: "two files", Attachments: &one, Editors: &[]string{denproto.FormatID(owner.MemberID)}},
	} {
		if _, err := f.d.Edit(ctx, member, m.ID, req); !denproto.IsCode(err, denproto.CodeForbidden) {
			t.Errorf("a forward's edit with %s: %v", name, err)
		}
	}
	left, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "two files", Attachments: &one})
	if err != nil || !left.Forwarded || left.Text != "two files" || len(left.Attachments) != 1 || left.Attachments[0].ID != ups[0] {
		t.Errorf("a file taken off a forward: %+v %v", left, err)
	}
}

// A channel forward may carry its source (den, channel, message ID), from the
// same den or another; it persists in history, events, changes, and when a
// file is removed. The den refuses a source without forwarded, on a DM or
// notes outside the seal, or with malformed IDs.
func TestForwardSource(t *testing.T) {
	f, owner, member := notesFixture(t)
	ctx := context.Background()
	info, _ := f.d.Info()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	first := f.send(owner, c.ID, "from here")
	dm, _ := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(member.MemberID)})
	notes, err := f.d.OpenNotes(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	sameDen := &denproto.ForwardSource{DenID: info.ID, ChannelID: c.ID, MessageID: first.ID}
	otherDen := &denproto.ForwardSource{DenID: bytes.Repeat([]byte{0x5a}, denproto.IDSize), ChannelID: "41", MessageID: "90210"}

	// Invalid requests rejected with invalid_field.
	dmSealed := f.sealedSend(member, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x"})
	dmSealed.ForwardSource = sameDen
	noteNonce := denproto.Random(16)
	noteBody, err := denproto.SealDMMessage(f.notesKey(member.MemberID), f.notesAD(notes, member.MemberID, noteNonce, 1), denproto.DMPayload{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	noteSealed := denproto.SendRequest{Nonce: noteNonce, Sealed: noteBody, KeyID: notes.ID, ForwardSource: sameDen}

	for name, tc := range map[string]struct {
		channel string
		req     denproto.SendRequest
	}{
		"without forwarded": {c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", ForwardSource: sameDen}},
		"short den ID":      {c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Forwarded: true, ForwardSource: &denproto.ForwardSource{DenID: []byte{1, 2}, ChannelID: "1", MessageID: "2"}}},
		"bad channel ID":    {c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Forwarded: true, ForwardSource: &denproto.ForwardSource{DenID: info.ID, ChannelID: "0", MessageID: "2"}}},
		"bad message ID":    {c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Forwarded: true, ForwardSource: &denproto.ForwardSource{DenID: info.ID, ChannelID: "1", MessageID: "bad"}}},
		"on DM":             {dm.ID, dmSealed},
		"on notes":          {notes.ID, noteSealed},
	} {
		if _, err := f.d.Send(ctx, member, tc.channel, tc.req); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("%s: got %v, want invalid_field", name, err)
		}
	}

	// Same-den and cross-den channel forwards persist across send, event,
	// history, edit (dropping a file), and changes.
	for name, src := range map[string]*denproto.ForwardSource{"same den": sameDen, "cross den": otherDen} {
		up1, err := f.upload(member, "a.txt", []byte("a"))
		if err != nil {
			t.Fatal(err)
		}
		up2, err := f.upload(member, "b.txt", []byte("b"))
		if err != nil {
			t.Fatal(err)
		}
		drainEvents(sub)
		m, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{
			Nonce: denproto.Random(16), Text: name, Attachments: []string{up1.ID, up2.ID}, Forwarded: true, ForwardSource: src,
		})
		if err != nil || !m.Forwarded || !reflect.DeepEqual(m.ForwardSource, src) {
			t.Fatalf("%s send: %+v %v", name, m, err)
		}
		var ev denproto.Message
		for _, e := range drainEvents(sub) {
			var got denproto.Message
			if e.T == denproto.EventMessageCreated && json.Unmarshal(e.D, &got) == nil && got.ID == m.ID {
				ev = got
			}
		}
		if !reflect.DeepEqual(ev.ForwardSource, src) {
			t.Errorf("%s event forward_source = %+v, want %+v", name, ev.ForwardSource, src)
		}
		mid, _ := denproto.ParseID(m.ID)
		page, err := f.d.History(ctx, owner, c.ID, HistoryQuery{Around: mid, Limit: 1})
		if err != nil || len(page.Messages) != 1 || !reflect.DeepEqual(page.Messages[0].ForwardSource, src) {
			t.Fatalf("%s history: %+v %v", name, page.Messages, err)
		}
		one := []string{up1.ID}
		edited, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: name, Attachments: &one})
		if err != nil || !reflect.DeepEqual(edited.ForwardSource, src) {
			t.Fatalf("%s edit drop file: %+v %v", name, edited, err)
		}
		var updated denproto.MessageUpdated
		for _, e := range drainEvents(sub) {
			var u denproto.MessageUpdated
			if e.T == denproto.EventMessageUpdated && json.Unmarshal(e.D, &u) == nil && u.ID == m.ID {
				updated = u
			}
		}
		if !reflect.DeepEqual(updated.ForwardSource, src) {
			t.Errorf("%s updated event forward_source = %+v, want %+v", name, updated.ForwardSource, src)
		}
		ch, err := f.d.Changes(ctx, owner, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, cm := range ch.Messages {
			if cm.ID == m.ID {
				found = true
				if !reflect.DeepEqual(cm.ForwardSource, src) {
					t.Errorf("%s changes forward_source = %+v, want %+v", name, cm.ForwardSource, src)
				}
			}
		}
		if !found {
			t.Errorf("%s edited forward missing from changes", name)
		}
	}
}
