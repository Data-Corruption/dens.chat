package denclient_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/notify"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// notices keeps what a member's Dens tells its notifications.
type notices struct {
	mu       sync.Mutex
	messages []notify.Message
	starts   []notify.Start
	said     []string
}

func (n *notices) Message(m notify.Message) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, m)
}

func (n *notices) Started(st notify.Start) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.starts = append(n.starts, st)
}

func (n *notices) note(format string, args ...any) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.said = append(n.said, fmt.Sprintf(format, args...))
}

func (n *notices) Cleared(den, channel, through string) { n.note("cleared %s %s", channel, through) }
func (n *notices) Deleted(den, channel, message string) { n.note("deleted %s %s", channel, message) }
func (n *notices) EventOver(den, event string)          { n.note("over %s", event) }
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

// What a member's Dens tells its notifications: each message
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
	readyDM(t, owner, member, denID, dm)
	secret := sendDM(t, owner, denID, dm, "the secret is ||42||")
	if m := heard.waitMessage(t, secret.ID); !m.DM || m.Text != "the secret is ||42||" || m.Author != "Alice" || m.Channel != dm {
		t.Fatalf("the DM's notice is %+v", m)
	}

	if err := member.Leave(ctx, denID); err != nil {
		t.Fatal(err)
	}
	heard.waitSaid(t, "forgot")
}

// tee hands what a member's Dens tells its notifications to a real hub,
// then keeps it, so a test that waits for a message knows the hub heard
// it first.
type tee struct {
	*notify.Hub
	heard *notices
}

func (t tee) Message(m notify.Message) {
	t.Hub.Message(m)
	t.heard.Message(m)
}

func (t tee) Started(st notify.Start) {
	t.Hub.Started(st)
	t.heard.Started(st)
}

// newHub is the member's hub of notifications, with settings, and a page
// watching it off screen, which its browser lets notify, so what notifies
// shows there with its sound.
func newHub(t *testing.T, settings types.Notifications) (*notify.Hub, *hubPage) {
	t.Helper()
	log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	hub := notify.New(log, settings, func(types.Notifications) error { return nil }, nil, false)
	events, stop := hub.Watch("page")
	t.Cleanup(stop)
	hub.SetPage("page", false, true)
	return hub, &hubPage{events: events}
}

// hubPage is a page watching a hub.
type hubPage struct{ events <-chan notify.Event }

// took is what the page has been sent since last asked: the messages and
// events it showed, the messages whose sound it played alone, and the
// keys it took back.
func (p *hubPage) took() (shown, sounded, withdrawn []string) {
	for {
		select {
		case e, ok := <-p.events:
			if !ok {
				return
			}
			switch {
			case e.Show != nil && e.Show.Event != "":
				shown = append(shown, "event "+e.Show.Event)
			case e.Show != nil:
				shown = append(shown, e.Show.Message)
			case e.Sound != nil:
				sounded = append(sounded, e.Sound.Message)
			}
			withdrawn = append(withdrawn, e.Withdraw...)
		default:
			return
		}
	}
}

// A channel at All messages notifies of a message that mentions nobody,
// from the den's event to the page, though Mentions is off; nothing while
// its den is muted; and back at Mentions, it takes back what it showed and
// notifies of such a message no more. One at Nothing notifies of a mention
// no more.
func TestAllMessagesNotify(t *testing.T) {
	heard := &notices{}
	hub, page := newHub(t, types.Notifications{})
	_, owner, _, denID, general := chatDen(t, func(m *denclient.Manager) { m.Notices = tee{hub, heard} })
	sent := func(text string) string {
		t.Helper()
		m := send(t, owner, denID, general, text)
		heard.waitMessage(t, m.ID)
		return m.ID
	}

	sent("at Mentions")
	if shown, _, _ := page.took(); len(shown) != 0 {
		t.Fatalf("a message at Mentions, with Mentions off, showed %v", shown)
	}
	if err := hub.SetLevel(denID, general, notify.LevelAll); err != nil {
		t.Fatal(err)
	}
	plain := sent("no one in particular")
	if shown, _, _ := page.took(); !slices.Equal(shown, []string{plain}) {
		t.Fatalf("at All messages, the page showed %v, want %s", shown, plain)
	}

	if err := hub.Mute(denID, "", true); err != nil {
		t.Fatal(err)
	}
	sent("while the den is muted")
	if shown, _, _ := page.took(); len(shown) != 0 {
		t.Fatalf("in a muted den, the page showed %v", shown)
	}
	if err := hub.Mute(denID, "", false); err != nil {
		t.Fatal(err)
	}
	again := sent("and again")
	if shown, _, _ := page.took(); !slices.Equal(shown, []string{again}) {
		t.Fatalf("with the den unmuted, the page showed %v, want %s", shown, again)
	}

	if err := hub.SetLevel(denID, general, notify.LevelMentions); err != nil {
		t.Fatal(err)
	}
	if _, _, withdrawn := page.took(); !slices.Equal(withdrawn, []string{denID + "/" + general}) {
		t.Fatalf("back at Mentions, the page took back %v", withdrawn)
	}
	sent("no one, at Mentions")
	if shown, sounded, _ := page.took(); len(shown)+len(sounded) != 0 {
		t.Fatalf("back at Mentions, the page showed %v and sounded %v", shown, sounded)
	}

	on := true
	if err := hub.SetKinds(notify.Kinds{Mentions: &on}); err != nil {
		t.Fatal(err)
	}
	if err := hub.SetLevel(denID, general, notify.LevelNothing); err != nil {
		t.Fatal(err)
	}
	sent("hey @bob")
	if shown, sounded, _ := page.took(); len(shown)+len(sounded) != 0 {
		t.Fatalf("at Nothing, a mention showed %v and sounded %v", shown, sounded)
	}
}

// Do not disturb holds a DM, a mention and an event's start, from the
// den's events to the page, neither showing nor sounding them, and takes
// back nothing that showed; once the wall clock passes its end, the next
// DM shows, and nothing it held does. Until the member turns it off, no
// clock ends it.
func TestDoNotDisturbHolds(t *testing.T) {
	heard := &notices{}
	hub, page := newHub(t, types.Notifications{DMs: true, Mentions: true})
	var mu sync.Mutex
	now := time.Date(2026, 11, 21, 22, 0, 0, 0, time.Local)
	hub.Now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	pass := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(d)
	}
	h, owner, member, denID, general := chatDen(t, func(m *denclient.Manager) { m.Notices = tee{hub, heard} })
	h.d.StartTimer()
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	sentDM := func(text string) string {
		t.Helper()
		m := sendDM(t, owner, denID, dm, text)
		heard.waitMessage(t, m.ID)
		return m.ID
	}

	first := sentDM("before")
	if shown, _, _ := page.took(); !slices.Equal(shown, []string{first}) {
		t.Fatalf("before Do not disturb, the page showed %v, want %s", shown, first)
	}
	if err := hub.SetDoNotDisturb(true, hub.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	sentDM("held")
	mention := send(t, owner, denID, general, "hey @bob")
	heard.waitMessage(t, mention.ID)
	starts, ends := time.Now().Add(time.Hour).UnixMilli(), time.Now().Add(2*time.Hour).UnixMilli()
	e, err := owner.CreateEvent(ctx, denID, denproto.EventRequest{Name: ptrTo("Picnic"), StartsAt: &starts, EndsAt: &ends, Place: ptrTo("the park")})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "bob's Dens to see the event", func() bool { return strings.HasPrefix(eventsIn(t, member, denID), "Picnic") })
	if _, err := member.SetInterest(ctx, denID, e.ID, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "bob's mark", func() bool { return eventsIn(t, member, denID) == "Picnic:1+mine" })
	startNow := time.Now().UnixMilli()
	if _, err := owner.UpdateEvent(ctx, denID, e.ID, denproto.EventRequest{StartsAt: &startNow}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "bob's Dens to hear the picnic start", func() bool {
		heard.mu.Lock()
		defer heard.mu.Unlock()
		return len(heard.starts) > 0
	})
	if shown, sounded, withdrawn := page.took(); len(shown)+len(sounded)+len(withdrawn) != 0 {
		t.Fatalf("under Do not disturb, the page showed %v, sounded %v and took back %v", shown, sounded, withdrawn)
	}

	pass(time.Hour + time.Second)
	after := sentDM("after")
	if shown, _, _ := page.took(); !slices.Equal(shown, []string{after}) {
		t.Fatalf("past its end, the page showed %v, want %s alone", shown, after)
	}

	if err := hub.SetDoNotDisturb(true, time.Time{}); err != nil {
		t.Fatal(err)
	}
	pass(30 * 24 * time.Hour)
	sentDM("still held")
	if shown, sounded, _ := page.took(); len(shown)+len(sounded) != 0 {
		t.Fatalf("until turned off, a month on, the page showed %v and sounded %v", shown, sounded)
	}
	if err := hub.SetDoNotDisturb(false, time.Time{}); err != nil {
		t.Fatal(err)
	}
	last := sentDM("turned off")
	if shown, _, _ := page.took(); !slices.Equal(shown, []string{last}) {
		t.Fatalf("turned off, the page showed %v, want %s", shown, last)
	}
}
