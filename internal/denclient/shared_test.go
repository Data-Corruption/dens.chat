package denclient_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A member shares a checklist with another, who ticks it through their own
// Dens; a tick against a list that changed meanwhile comes back as a
// conflict with the message as it is.
func TestSharedChecklist(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	bob := me(t, member, denID).ID
	m, err := owner.Send(ctx, denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] milk\n[ ] eggs", Editors: []string{bob}})
	if err != nil || !slices.Equal(m.Editors, []string{bob}) {
		t.Fatalf("send: %+v %v", m, err)
	}
	ticked, err := member.SetTask(ctx, denID, m.ID, 1, denproto.TaskRequest{Checked: true, Text: "eggs"})
	if err != nil || ticked.Text != "[ ] milk\n[x] eggs" || ticked.Revision != 2 {
		t.Fatalf("tick: %+v %v", ticked, err)
	}
	edited, err := member.Edit(ctx, denID, m.ID, denproto.EditRequest{Revision: 2, Text: "[ ] bread\n[ ] milk\n[x] eggs"})
	if err != nil || edited.EditedBy != bob {
		t.Fatalf("an editor's edit: %+v %v", edited, err)
	}
	_, err = owner.SetTask(ctx, denID, m.ID, 1, denproto.TaskRequest{Checked: true, Text: "eggs"})
	var conflict *denclient.ErrEditConflict
	if !errors.As(err, &conflict) || conflict.Current.Revision != 3 {
		t.Fatalf("a stale tick: %v", err)
	}
	if _, err := member.Edit(ctx, denID, m.ID, denproto.EditRequest{Revision: 3, Text: "mine now", Editors: &[]string{}}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("an editor changing the editors: %v", err)
	}
	if _, err := owner.SetTask(ctx, denID, m.ID, -1, denproto.TaskRequest{Checked: true}); err == nil {
		t.Fatal("a negative task number was sent")
	}
}
