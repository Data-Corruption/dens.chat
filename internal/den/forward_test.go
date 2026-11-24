package den

import (
	"context"
	"encoding/json"
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
	if info, _ := f.d.Info(); !info.Forward {
		t.Error("the den's info doesn't say it keeps a forward's mark")
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
