package denclient_test

import (
	"context"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
)

// startCalls runs the den's calls on loopback ports, as the service runs
// them on its media ports.
func (h *denHost) startCalls() {
	h.t.Helper()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		h.t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.d.StartCalls(udp, tcp, sfu.Config{Loopback: true}); err != nil {
		h.t.Fatal(err)
	}
}

// page stands in for a page that holds a call: a test caller is its
// browser, and it answers the offers its install relays, as the page does.
type page struct {
	t       *testing.T
	m       *denclient.Manager
	id      string
	caller  *sfu.TestCaller
	events  chan denclient.CallEvent
	ended   chan string
	resumed chan struct{}
	stop    chan struct{}
	// The last offer answered, and its answer, which goes again if the den
	// sends that offer again after a resume, as the page does.
	version int
	answer  string
}

func newPage(t *testing.T, m *denclient.Manager, id string) *page {
	t.Helper()
	caller, err := sfu.NewTestCaller(sfu.CallerOptions{Loopback: true})
	if err != nil {
		t.Fatal(err)
	}
	p := &page{t: t, m: m, id: id, caller: caller, events: make(chan denclient.CallEvent, 16),
		ended: make(chan string, 16), resumed: make(chan struct{}, 16), stop: make(chan struct{})}
	t.Cleanup(func() {
		close(p.stop)
		_ = caller.Close()
	})
	go p.run()
	return p
}

// deliver is what the install calls; like the page's stream, it doesn't
// block.
func (p *page) deliver(e denclient.CallEvent) {
	select {
	case p.events <- e:
	default:
	}
}

func (p *page) run() {
	for {
		select {
		case <-p.stop:
			return
		case e := <-p.events:
			switch {
			case e.Ended != "":
				p.ended <- e.Ended
			case e.Resumed:
				p.resumed <- struct{}{}
			case e.Offer != nil:
				if e.Offer.Version != p.version {
					answer, err := p.caller.Answer(e.Offer.SDP)
					if err != nil {
						continue
					}
					p.version, p.answer = e.Offer.Version, answer
				}
				p.m.AnswerCall(p.id, e.DenID, e.Offer.Version, p.answer)
			}
		}
	}
}

func (p *page) join(denID, channel string) {
	p.t.Helper()
	if err := p.m.JoinCall(context.Background(), p.id, denID, channel, false, false, p.deliver); err != nil {
		p.t.Fatal(err)
	}
}

// resume asks for the call the den holds, as the page does once its den
// is back.
func (p *page) resume(denID, channel string) {
	p.t.Helper()
	if err := p.m.JoinCall(context.Background(), p.id, denID, channel, false, true, p.deliver); err != nil {
		p.t.Fatal(err)
	}
	select {
	case <-p.resumed:
	case reason := <-p.ended:
		p.t.Fatalf("page %s's resume ended the call: %s", p.id, reason)
	case <-time.After(10 * time.Second):
		p.t.Fatalf("page %s's call didn't resume", p.id)
	}
}

func (p *page) waitEnded(reason string) {
	p.t.Helper()
	select {
	case got := <-p.ended:
		if got != reason {
			p.t.Fatalf("page %s's call ended %q, not %q", p.id, got, reason)
		}
	case <-time.After(10 * time.Second):
		p.t.Fatalf("page %s's call didn't end %q", p.id, reason)
	}
}

func (p *page) hears(member string) {
	p.t.Helper()
	if err := p.caller.WaitHeard(member, 20, 10*time.Second); err != nil {
		p.t.Fatal(err)
	}
}

// inCall reads who a view shows in a channel's call; a member ending in
// "*" is muted.
func inCall(v denclient.View, channel string) []string {
	got := []string{}
	for _, c := range v.Calls {
		if c.ChannelID == channel {
			for _, m := range c.Members {
				id := m.ID
				if m.Muted {
					id += "*"
				}
				got = append(got, id)
			}
		}
	}
	return got
}

func showsCall(channel string, want ...string) func(denclient.View) bool {
	return func(v denclient.View) bool { return slices.Equal(inCall(v, channel), append([]string{}, want...)) }
}

func TestCallThroughTheClient(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	h.startCalls()
	name := "Lounge"
	if err := owner.Manage(context.Background(), denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, Kind: denproto.KindVoice}); err != nil {
		t.Fatal(err)
	}
	var lounge string
	viewOf(t, member, denID, "the voice channel", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == name {
				lounge = c.ID
			}
		}
		return lounge != ""
	})
	alice, bob := me(t, owner, denID).ID, me(t, member, denID).ID

	// The owner's install reaches its own den over loopback, and the
	// member's by name: both put media through the den's ports.
	a, b := newPage(t, owner, "page-a"), newPage(t, member, "page-b")
	a.join(denID, lounge)
	// A call lists members in the order the den heard them join.
	viewOf(t, member, denID, "alice in the call", showsCall(lounge, alice))
	b.join(denID, lounge)
	a.hears(bob)
	b.hears(alice)
	viewOf(t, member, denID, "both in the call", showsCall(lounge, alice, bob))

	member.MuteCall("page-b", denID, true)
	viewOf(t, owner, denID, "bob muted", showsCall(lounge, alice, bob+"*"))

	// Another page on the member's install takes the call over.
	c := newPage(t, member, "page-c")
	c.join(denID, lounge)
	b.waitEnded(denproto.VoiceMoved)
	c.hears(alice)

	// Closing the page that holds the call leaves it.
	member.DropPage("page-c")
	viewOf(t, owner, denID, "bob gone", showsCall(lounge, alice))

	// A den restart ends calls, which their pages hear as the dropped
	// connection it is.
	b.join(denID, lounge)
	viewOf(t, owner, denID, "bob back", showsCall(lounge, alice, bob))
	h.restart()
	a.waitEnded(denclient.CallDisconnected)
	b.waitEnded(denclient.CallDisconnected)
}

// TestCallRidesOutADroppedConnection checks that a call goes on when its
// install's connection to the den drops, and that its page takes it back
// once the connection returns, with its media flowing throughout.
func TestCallRidesOutADroppedConnection(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	h.startCalls()
	name := "Lounge"
	if err := owner.Manage(context.Background(), denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, Kind: denproto.KindVoice}); err != nil {
		t.Fatal(err)
	}
	var lounge string
	viewOf(t, member, denID, "the voice channel", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == name {
				lounge = c.ID
			}
		}
		return lounge != ""
	})
	alice, bob := me(t, owner, denID).ID, me(t, member, denID).ID
	a, b := newPage(t, owner, "page-a"), newPage(t, member, "page-b")
	a.join(denID, lounge)
	// A call lists members in the order the den heard them join.
	viewOf(t, member, denID, "alice in the call", showsCall(lounge, alice))
	b.join(denID, lounge)
	a.hears(bob)
	b.hears(alice)

	// The den makes the member's install reconnect, as a dropped socket does.
	bobID, _ := denproto.ParseID(bob)
	h.d.CloseMemberSockets(bobID, denproto.CloseTooSlow, "too slow")
	b.waitEnded(denclient.CallDisconnected)
	a.hears(bob)
	b.hears(alice)
	waitFor(t, member, "the den back", connected)
	b.resume(denID, lounge)
	viewOf(t, owner, denID, "bob still in the call", showsCall(lounge, alice, bob))

	// Another member's joining reaches the resumed call as an offer.
	carol := newPage(t, owner, "page-c")
	carol.join(denID, lounge)
	a.waitEnded(denproto.VoiceMoved)
	b.hears(alice)
	carol.hears(bob)
}

func TestCallRefusalsReachThePage(t *testing.T) {
	_, owner, _, denID, general := chatDen(t)
	a := newPage(t, owner, "page-a")
	// The den has no calls running: no media ports in this test.
	a.join(denID, general)
	a.waitEnded(denproto.VoiceNotFound)
	if err := owner.JoinCall(context.Background(), "page-a", "not a den", general, false, false, a.deliver); err == nil {
		t.Error("joined a call in a den this install hasn't joined")
	}
}
