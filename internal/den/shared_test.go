package den

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func (f *fixture) sendShared(s *Session, channel, text string, editors ...*Session) (denproto.Message, error) {
	ids := make([]string, len(editors))
	for i, e := range editors {
		ids[i] = denproto.FormatID(e.MemberID)
	}
	return f.d.Send(context.Background(), s, channel, denproto.SendRequest{Nonce: denproto.Random(16), Text: text, Editors: ids})
}

func TestSharedMessages(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	staff := f.newChannel(owner, "staff", denproto.ChannelRequest{StaffOnly: ptr(true)})
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(bob.MemberID)})
	if err != nil {
		t.Fatal(err)
	}

	// Editors must be other members who can see the channel, each once.
	for name, try := range map[string]func() error{
		"the author":           func() error { _, err := f.sendShared(owner, general.ID, "x", owner); return err },
		"a member twice":       func() error { _, err := f.sendShared(owner, general.ID, "x", bob, bob); return err },
		"staff-only, a member": func() error { _, err := f.sendShared(owner, staff.ID, "x", bob); return err },
		"a DM, someone else":   func() error { _, err := f.sendShared(owner, dm.ID, "x", carol); return err },
		"nobody": func() error {
			_, err := f.d.Send(ctx, owner, general.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Editors: []string{"999999"}})
			return err
		},
		"too many": func() error {
			many := make([]string, denproto.MaxEditors+1)
			for i := range many {
				many[i] = denproto.FormatID(int64(1000 + i))
			}
			_, err := f.d.Send(ctx, owner, general.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Editors: many})
			return err
		},
	} {
		if err := try(); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("%s as an editor: %v", name, err)
		}
	}
	if m, err := f.sendShared(owner, dm.ID, "ours", bob); err != nil || !slices.Equal(m.Editors, []string{denproto.FormatID(bob.MemberID)}) {
		t.Fatalf("a DM shared with its other member: %+v %v", m, err)
	}

	m, err := f.sendShared(owner, general.ID, "plans", carol, bob)
	want := []string{denproto.FormatID(bob.MemberID), denproto.FormatID(carol.MemberID)}
	if err != nil || !slices.Equal(m.Editors, want) {
		t.Fatalf("send: %+v %v", m, err)
	}
	h, err := f.d.History(ctx, bob, general.ID, HistoryQuery{})
	if err != nil || len(h.Messages) != 1 || !slices.Equal(h.Messages[0].Editors, want) {
		t.Fatalf("history: %+v %v", h, err)
	}

	// Only the author changes who may edit, and that alone doesn't mark
	// the message as edited.
	onlyBob := []string{denproto.FormatID(bob.MemberID)}
	_, err = f.d.Edit(ctx, bob, m.ID, denproto.EditRequest{Revision: 1, Text: "plans", Editors: &onlyBob})
	wantCode(t, err, denproto.CodeForbidden)
	m, err = f.d.Edit(ctx, owner, m.ID, denproto.EditRequest{Revision: 1, Text: "plans", Editors: &onlyBob})
	if err != nil || m.Revision != 2 || m.EditedAt != 0 || !slices.Equal(m.Editors, onlyBob) {
		t.Fatalf("changing editors: %+v %v", m, err)
	}

	// An editor edits; anyone else can't, and editors can't delete.
	_, err = f.d.Edit(ctx, carol, m.ID, denproto.EditRequest{Revision: 2, Text: "carol's plans"})
	wantCode(t, err, denproto.CodeForbidden)
	m, err = f.d.Edit(ctx, bob, m.ID, denproto.EditRequest{Revision: 2, Text: "better plans"})
	if err != nil || m.Text != "better plans" || m.EditedBy != denproto.FormatID(bob.MemberID) || m.EditedAt == 0 || !slices.Equal(m.Editors, onlyBob) {
		t.Fatalf("an editor's edit: %+v %v", m, err)
	}
	wantCode(t, f.d.Delete(ctx, bob, m.ID), denproto.CodeForbidden)
	if err := f.d.Delete(ctx, owner, m.ID); err != nil {
		t.Fatal(err)
	}
}

func TestTicks(t *testing.T) {
	f, owner, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	m, err := f.sendShared(owner, general.ID, "[ ] milk\n[ ] eggs\n[ ] bread", bob)
	if err != nil {
		t.Fatal(err)
	}
	tick := func(s *Session, n int, checked bool, text string) (denproto.Message, error) {
		return f.d.SetTask(ctx, s, m.ID, n, denproto.TaskRequest{Checked: checked, Text: text})
	}

	_, err = tick(carol, 0, true, "milk")
	wantCode(t, err, denproto.CodeForbidden)
	got, err := tick(bob, 0, true, "milk")
	if err != nil || got.Text != "[x] milk\n[ ] eggs\n[ ] bread" || got.Revision != 2 || got.EditedAt != 0 {
		t.Fatalf("an editor's tick: %+v %v", got, err)
	}
	// Ticking a box that's already ticked changes nothing.
	if got, err = tick(owner, 0, true, "milk"); err != nil || got.Revision != 2 {
		t.Fatalf("ticking again: %+v %v", got, err)
	}
	// A tick names its task's text, so one against a list that changed is
	// a conflict that carries the message as it is.
	for _, stale := range []struct {
		n    int
		text string
	}{{1, "milk"}, {3, "butter"}, {-1, "milk"}} {
		_, err = tick(bob, stale.n, true, stale.text)
		current, ok := Conflict(err)
		if !ok || current.Revision != 2 {
			t.Errorf("a stale tick of %d %q: %v", stale.n, stale.text, err)
		}
	}

	// Ticks on different boxes at the same moment all stay, and their
	// events go out in the order of the revisions.
	sub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	var list strings.Builder
	for i := range 20 {
		fmt.Fprintf(&list, "[ ] item %d\n", i)
	}
	m, err = f.sendShared(owner, general.ID, list.String(), bob)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 20 {
		s := owner
		if i%2 == 1 {
			s = bob
		}
		wg.Go(func() {
			<-start
			if _, err := tick(s, i, true, fmt.Sprintf("item %d", i)); err != nil {
				t.Errorf("tick %d: %v", i, err)
			}
		})
	}
	close(start)
	wg.Wait()
	final, _, err := f.d.message(ctx, m.ID)
	if err != nil || strings.Contains(final.Text, "[ ]") || final.Revision != 21 {
		t.Fatalf("after the ticks: %+v %v", final, err)
	}
	last := 0
	for len(sub.Events) > 0 {
		e := <-sub.Events
		if e.T != denproto.EventMessageUpdated {
			continue
		}
		var u denproto.Message
		json.Unmarshal(e.D, &u)
		if u.Revision <= last {
			t.Fatalf("revision %d came after %d", u.Revision, last)
		}
		last = u.Revision
	}
	if last != 21 {
		t.Fatalf("the last update was revision %d", last)
	}
}
