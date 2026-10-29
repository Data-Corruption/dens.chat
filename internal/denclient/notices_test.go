package denclient_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/notify"
)

// notices keeps what a member's Dens tells its notifications.
type notices struct {
	mu       sync.Mutex
	messages []notify.Message
	said     []string
}

func (n *notices) Message(m notify.Message) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, m)
}

func (n *notices) note(format string, args ...any) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.said = append(n.said, fmt.Sprintf(format, args...))
}

func (n *notices) Cleared(den, channel, through string) { n.note("cleared %s %s", channel, through) }
func (n *notices) Deleted(den, channel, message string) { n.note("deleted %s %s", channel, message) }
func (n *notices) ChannelGone(den, channel string)      { n.note("gone %s", channel) }
func (n *notices) Forgot(den string)                    { n.note("forgot") }

// waitMessage waits for the notice of message id.
func (n *notices) waitMessage(t *testing.T, id string) notify.Message {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n.mu.Lock()
		i := slices.IndexFunc(n.messages, func(m notify.Message) bool { return m.ID == id })
		var m notify.Message
		if i >= 0 {
			m = n.messages[i]
		}
		n.mu.Unlock()
		if i >= 0 {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("no notice of message %s", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitSaid waits for the notices to hear what.
func (n *notices) waitSaid(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n.mu.Lock()
		ok := slices.Contains(n.said, what)
		said := slices.Clone(n.said)
		n.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the notices never heard %q, only %q", what, said)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// What a member's Dens tells its notifications (M12.3): each message
// another member sends, named and opened, a DM's too, and what takes one
// back: the member's own message, reading the channel, a delete, and
// leaving the den.
func TestNotices(t *testing.T) {
	heard := &notices{}
	_, owner, member, denID, general := chatDen(t, func(m *denclient.Manager) { m.Notices = heard })
	ctx := context.Background()

	mentioned := send(t, owner, denID, general, "hey @Bob, look")
	m := heard.waitMessage(t, mentioned.ID)
	want := notify.Message{Den: denID, DenName: "Chat Den", Channel: general, ChannelName: "general", ID: mentioned.ID,
		Author: "Alice", Mention: true, Text: "hey @Bob, look"}
	if m != want {
		t.Fatalf("the mention's notice is %+v, want %+v", m, want)
	}
	plain := send(t, owner, denID, general, "no one in particular")
	if m := heard.waitMessage(t, plain.ID); m.Mention || m.DM {
		t.Fatalf("a message mentioning no one: %+v", m)
	}

	own := send(t, member, denID, general, "mine")
	heard.waitSaid(t, "cleared "+general+" "+own.ID)
	heard.mu.Lock()
	for _, m := range heard.messages {
		if m.ID == own.ID {
			t.Errorf("the member's own message was a notice: %+v", m)
		}
	}
	heard.mu.Unlock()

	later := send(t, owner, denID, general, "later")
	heard.waitMessage(t, later.ID)
	if err := member.MarkRead(ctx, denID, general, later.ID); err != nil {
		t.Fatal(err)
	}
	heard.waitSaid(t, "cleared "+general+" "+later.ID)
	if err := owner.DeleteMessage(ctx, denID, later.ID); err != nil {
		t.Fatal(err)
	}
	heard.waitSaid(t, "deleted "+general+" "+later.ID)

	// A DM reaches the notices opened, which the den never could.
	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	secret := sendDM(t, owner, denID, dm, "the secret is ||42||")
	if m := heard.waitMessage(t, secret.ID); !m.DM || m.Text != "the secret is ||42||" || m.Author != "Alice" || m.Channel != dm {
		t.Fatalf("the DM's notice is %+v", m)
	}

	if err := member.Leave(ctx, denID); err != nil {
		t.Fatal(err)
	}
	heard.waitSaid(t, "forgot")
}
