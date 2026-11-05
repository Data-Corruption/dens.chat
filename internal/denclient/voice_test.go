package denclient_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
	"github.com/Data-Corruption/dens.chat/internal/sfu/sfutest"
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
	t        *testing.T
	m        *denclient.Manager
	id       string
	caller   *sfutest.Caller
	events   chan denclient.CallEvent
	ended    chan string
	resumed  chan struct{}
	refused  chan denclient.CallRefusal
	talking  chan []string
	sounding chan string
	stop     chan struct{}
	// The last offer answered, and its answer, which goes again if the den
	// sends that offer again after a resume, as the page does.
	version int
	answer  string
	// keys are the call's keys as the page last got them (M16.2).
	keysMu sync.Mutex
	keys   *denclient.CallKeys
}

func newPage(t *testing.T, m *denclient.Manager, id string) *page {
	t.Helper()
	caller, err := sfutest.NewCaller(sfutest.Options{Loopback: true})
	if err != nil {
		t.Fatal(err)
	}
	p := &page{t: t, m: m, id: id, caller: caller, events: make(chan denclient.CallEvent, 16),
		ended: make(chan string, 16), resumed: make(chan struct{}, 16), refused: make(chan denclient.CallRefusal, 16),
		talking: make(chan []string, 16), sounding: make(chan string, 16), stop: make(chan struct{})}
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
			case e.Refused != nil:
				p.refused <- *e.Refused
			case e.Talking != nil:
				p.talking <- *e.Talking
			case e.Sounding != nil:
				p.sounding <- *e.Sounding
			case e.Keys != nil:
				p.keysMu.Lock()
				p.keys = e.Keys
				p.keysMu.Unlock()
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
	if err := p.m.JoinCall(context.Background(), p.id, denID, channel, denclient.JoinOptions{Sealed: true}, p.deliver); err != nil {
		p.t.Fatal(err)
	}
}

// resume asks for the call the den holds, as the page does once its den
// is back.
func (p *page) resume(denID, channel string) {
	p.t.Helper()
	if err := p.m.JoinCall(context.Background(), p.id, denID, channel, denclient.JoinOptions{Resume: true, Sealed: true}, p.deliver); err != nil {
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

// waitKeys waits until the call's keys, as the page last got them, are as
// want says, and returns them.
func (p *page) waitKeys(what string, want func(denclient.CallKeys) bool) denclient.CallKeys {
	p.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		p.keysMu.Lock()
		k := p.keys
		p.keysMu.Unlock()
		if k != nil && want(*k) {
			return *k
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("page %s never got %s: %+v", p.id, what, k)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *page) hears(member string) {
	p.t.Helper()
	if err := p.caller.WaitHeard(member, 20, 10*time.Second); err != nil {
		p.t.Fatal(err)
	}
}

// inCall reads who a view shows in a channel's call; a member ending in
// "*" is muted, and "~" after it deafened.
func inCall(v denclient.View, channel string) []string {
	got := []string{}
	for _, c := range v.Calls {
		if c.ChannelID == channel {
			for _, m := range c.Members {
				id := m.ID
				if m.Muted {
					id += "*"
				}
				if m.Deafened {
					id += "~"
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
	// The shortcut helper reads keys only while the install holds a call
	// (M7), which OnCall says as it changes.
	var hookMu sync.Mutex
	var holds []bool
	member.OnCall = func(active bool) {
		hookMu.Lock()
		holds = append(holds, active)
		hookMu.Unlock()
	}
	holding := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			hookMu.Lock()
			got := len(holds) > 0 && holds[len(holds)-1] == want
			hookMu.Unlock()
			if got {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("OnCall said %v, want %v last", holds, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// The owner's install reaches its own den over loopback, and the
	// member's by name: both put media through the den's ports.
	a, b := newPage(t, owner, "page-a"), newPage(t, member, "page-b")
	a.join(denID, lounge)
	// A call lists members in the order the den heard them join.
	viewOf(t, member, denID, "alice in the call", showsCall(lounge, alice))
	b.join(denID, lounge)
	holding(true)
	a.hears(bob)
	b.hears(alice)
	viewOf(t, member, denID, "both in the call", showsCall(lounge, alice, bob))

	member.MuteCall("page-b", denID, denproto.VoiceMute{Muted: true})
	viewOf(t, owner, denID, "bob muted", showsCall(lounge, alice, bob+"*"))
	member.MuteCall("page-b", denID, denproto.VoiceMute{Muted: true, Deafened: true})
	viewOf(t, owner, denID, "bob deafened", showsCall(lounge, alice, bob+"*~"))

	// Another page on the member's install takes the call over.
	c := newPage(t, member, "page-c")
	c.join(denID, lounge)
	b.waitEnded(denproto.VoiceMoved)
	c.hears(alice)

	holding(true)

	// Closing the page that holds the call leaves it.
	member.DropPage("page-c")
	holding(false)
	viewOf(t, owner, denID, "bob gone", showsCall(lounge, alice))

	// A den restart ends calls, which their pages hear as the dropped
	// connection it is.
	b.join(denID, lounge)
	viewOf(t, owner, denID, "bob back", showsCall(lounge, alice, bob))
	holding(true)
	h.restart()
	a.waitEnded(denclient.CallDisconnected)
	b.waitEnded(denclient.CallDisconnected)
	holding(false)
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
	if err := owner.JoinCall(context.Background(), "page-a", "not a den", general, denclient.JoinOptions{Sealed: true}, a.deliver); err == nil {
		t.Error("joined a call in a den this install hasn't joined")
	}
}

// TestShareThroughTheClient checks a share relayed by both installs
// (M4.2): the den's limits and a voice channel's bitrate in the page's
// view, an offer with VP9 sections passing the client's check, the
// marks reaching the views, the share reaching its viewer, and a refusal
// reaching the page that asked.
func TestShareThroughTheClient(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	h.startCalls()
	name := "Lounge"
	if err := owner.Manage(context.Background(), denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, Kind: denproto.KindVoice}); err != nil {
		t.Fatal(err)
	}
	var lounge string
	v := viewOf(t, member, denID, "the voice channel", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == name && c.Bitrate == denproto.DefaultVoiceBitrate {
				lounge = c.ID
			}
		}
		return lounge != ""
	})
	if v.CallLimits != denproto.DefaultCallLimits {
		t.Errorf("the page's view has limits %+v", v.CallLimits)
	}
	alice, bob := me(t, owner, denID).ID, me(t, member, denID).ID
	a, b := newPage(t, owner, "page-a"), newPage(t, member, "page-b")
	a.join(denID, lounge)
	viewOf(t, member, denID, "alice in the call", showsCall(lounge, alice))
	b.join(denID, lounge)
	a.hears(bob)

	b.caller.Share(true)
	member.ShareCall("page-b", denID, true, true)
	viewOf(t, owner, denID, "bob sharing", func(v denclient.View) bool {
		return slices.ContainsFunc(v.Calls, func(c denproto.Call) bool {
			return slices.ContainsFunc(c.Members, func(m denproto.CallMember) bool { return m.ID == bob && m.Sharing && m.Sound })
		})
	})
	owner.WatchCall("page-a", denID, bob, true)
	if err := a.caller.WaitSaw(bob, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.caller.WaitHeardShare(bob, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "alice watching bob", func(v denclient.View) bool {
		return slices.ContainsFunc(v.Calls, func(c denproto.Call) bool {
			return slices.ContainsFunc(c.Members, func(m denproto.CallMember) bool { return m.ID == alice && slices.Equal(m.Watching, []string{bob}) })
		})
	})

	// A new den lets one member share at a time.
	owner.ShareCall("page-a", denID, true, false)
	select {
	case r := <-a.refused:
		if r.What != denproto.RefusedShare || r.Reason != denproto.RefusedFull {
			t.Errorf("the refusal reached the page as %+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the refusal didn't reach the page")
	}
}

// waitTalking waits for word that these members hold push to talk.
func (p *page) waitTalking(want ...string) {
	p.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case got := <-p.talking:
			if slices.Equal(got, want) {
				return
			}
		case <-timeout:
			p.t.Fatalf("page %s didn't hear push to talk held by %v", p.id, want)
		}
	}
}

// TestPushToTalkThroughTheClient checks that the push to talk a page holds
// reaches the den through its install, and the den's word of it the other
// page in the call (M11).
func TestPushToTalkThroughTheClient(t *testing.T) {
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
	viewOf(t, member, denID, "alice in the call", showsCall(lounge, alice))
	b.join(denID, lounge)
	viewOf(t, owner, denID, "both in the call", showsCall(lounge, alice, bob))
	member.TalkCall("page-b", denID, true)
	a.waitTalking(bob)
	b.waitTalking(bob)
	// Only the page that holds the call speaks for it: what another page
	// says would have reached the den well before Alice's press.
	member.TalkCall("page-x", denID, false)
	time.Sleep(300 * time.Millisecond)
	owner.TalkCall("page-a", denID, true)
	a.waitTalking(alice, bob)
	member.TalkCall("page-b", denID, false)
	a.waitTalking(alice)
}

// waitSounding waits for word that a member's sound plays, or with "" that
// none does.
func (p *page) waitSounding(want string) {
	p.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case got := <-p.sounding:
			if got == want {
				return
			}
		case <-timeout:
			p.t.Fatalf("page %s didn't hear that %q's sound plays", p.id, want)
		}
	}
}

// TestTheSoundboardThroughTheClient checks the soundboard through both
// installs (M14.3): staff add a sound from an upload their page prepared,
// rename it and delete it, which every page's view follows, with the den's
// limits for it; a member plays it in a call, every page holding the call
// hears whose sound plays and receives it, and a refusal reaches the page
// that asked.
func TestTheSoundboardThroughTheClient(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	h.startCalls()
	ctx := context.Background()
	name := "Lounge"
	if err := owner.Manage(ctx, denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, Kind: denproto.KindVoice}); err != nil {
		t.Fatal(err)
	}
	var lounge string
	v := viewOf(t, member, denID, "the voice channel", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == name {
				lounge = c.ID
			}
		}
		return lounge != ""
	})
	if v.SoundLimits != denproto.DefaultSoundLimits || len(v.Sounds) != 0 {
		t.Fatalf("the page's view has the soundboard %+v, %+v", v.SoundLimits, v.Sounds)
	}
	limits := denproto.SoundLimits{Sounds: 5, Longest: 10, Wait: 0}
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{SoundLimits: &limits}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the soundboard's limits", func(v denclient.View) bool { return v.SoundLimits == limits })

	file := sfutest.Sound(sfutest.SoundPackets(100, 160))
	up, err := owner.Upload(ctx, denID, "", "horn.ogg", int64(len(file)), bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.AddSound(ctx, denID, "Mine", up.ID); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Errorf("a member adding a sound: %v", err)
	}
	horn, err := owner.AddSound(ctx, denID, "Horn", up.ID)
	if err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the sound", func(v denclient.View) bool { return slices.Contains(v.Sounds, horn) })

	alice, bob := me(t, owner, denID).ID, me(t, member, denID).ID
	a, b := newPage(t, owner, "page-a"), newPage(t, member, "page-b")
	a.join(denID, lounge)
	viewOf(t, member, denID, "alice in the call", showsCall(lounge, alice))
	b.join(denID, lounge)
	a.hears(bob)
	member.PlaySound("page-b", denID, horn.ID)
	a.waitSounding(bob)
	b.waitSounding(bob)
	if err := a.caller.WaitBoard(20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	owner.PlaySound("page-a", denID, horn.ID)
	select {
	case r := <-a.refused:
		if r.What != denproto.RefusedSound || r.SoundID != horn.ID || r.Reason != denproto.RefusedPlaying {
			t.Errorf("the refusal reached the page as %+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the refusal didn't reach the page")
	}
	a.waitSounding("")
	got := a.caller.Board()
	if len(got) < 90 || sfutest.PlaceOf(got[0].Payload) != 0 {
		t.Errorf("alice's page received %d packets of the sound's 100", len(got))
	}

	renamed, err := owner.RenameSound(ctx, denID, horn.ID, "Big horn")
	if err != nil || renamed.Name != "Big horn" {
		t.Fatalf("renamed: %+v, %v", renamed, err)
	}
	viewOf(t, member, denID, "the sound renamed", func(v denclient.View) bool { return slices.Contains(v.Sounds, renamed) })
	if err := owner.DeleteSound(ctx, denID, horn.ID); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the sound deleted", func(v denclient.View) bool { return len(v.Sounds) == 0 })
}

// TestCallKeysThroughTheClient checks a sealed call's keys between three
// installs (M16.2): each member's Dens makes a key as they join, and a new
// one as anyone joins or leaves, sealed to the others' identities, which
// the den passes on; each opens the others' and gives its page the call's
// keys whole, without those of a member who left; a page asks for them
// again; and a development build withholds a member's from its pages.
func TestCallKeysThroughTheClient(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	h.startCalls()
	name := "Lounge"
	if err := owner.Manage(context.Background(), denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, Kind: denproto.KindVoice}); err != nil {
		t.Fatal(err)
	}
	invite, _, err := owner.CreateInvite(context.Background(), denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	third := h.client(t, noOwnDen)
	join(t, third, invite, "carol")
	var lounge string
	v := viewOf(t, third, denID, "the voice channel, and a den that seals calls", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == name {
				lounge = c.ID
			}
		}
		return lounge != "" && v.SealedCalls
	})
	if !v.SealedCalls {
		t.Fatal("the den's view doesn't say it seals calls")
	}
	alice, bob, carol := me(t, owner, denID).ID, me(t, member, denID).ID, me(t, third, denID).ID
	// Each member's Dens publishes their identity as it signs in.
	viewOf(t, owner, denID, "everyone's identities", func(v denclient.View) bool {
		n := 0
		for _, m := range v.Members {
			if m.Identity != "" {
				n++
			}
		}
		return n == 3
	})
	a, b := newPage(t, owner, "page-a"), newPage(t, member, "page-b")
	a.join(denID, lounge)
	viewOf(t, member, denID, "alice in the call", showsCall(lounge, alice))
	b.join(denID, lounge)

	// settled waits until each page holds every other's latest key, and
	// no member's but theirs, and returns what each holds, by member.
	type side struct {
		p  *page
		id string
	}
	settled := func(what string, sides ...side) map[string]denclient.CallKeys {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			got := map[string]denclient.CallKeys{}
			for _, s := range sides {
				s.p.keysMu.Lock()
				if s.p.keys != nil {
					got[s.id] = *s.p.keys
				}
				s.p.keysMu.Unlock()
			}
			ok := len(got) == len(sides)
			for _, s := range sides {
				k := got[s.id]
				if !ok || k.Own == nil || len(k.Members) != len(sides)-1 {
					ok = false
					break
				}
				for _, o := range sides {
					if o.id != s.id && (got[o.id].Own == nil || !bytes.Equal(k.Members[o.id].Key, got[o.id].Own.Key) || k.Members[o.id].N != got[o.id].Own.N) {
						ok = false
					}
				}
			}
			if ok {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("the pages never settled on %s: %+v", what, got)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	first := settled("alice's and bob's keys", side{a, alice}, side{b, bob})
	if len(first[alice].Own.Key) != denproto.CallKeySize {
		t.Fatalf("a key of %d bytes", len(first[alice].Own.Key))
	}

	// Carol joins: alice's and bob's keys change, and each page holds all
	// three members' latest.
	c := newPage(t, third, "page-c")
	c.join(denID, lounge)
	second := settled("three members' keys", side{a, alice}, side{b, bob}, side{c, carol})
	for _, id := range []string{alice, bob} {
		if bytes.Equal(second[id].Own.Key, first[id].Own.Key) {
			t.Errorf("member %s's key didn't change as carol joined", id)
		}
	}
	c.hears(alice)

	// A page asks again, and gets them whole.
	a.keysMu.Lock()
	a.keys = nil
	a.keysMu.Unlock()
	owner.CallKeysAgain("page-a", denID)
	a.waitKeys("its keys again", func(k denclient.CallKeys) bool { return k.Own != nil && len(k.Members) == 2 })

	// A development build withholds bob's key from alice's pages, and
	// gives it again.
	if err := owner.Withhold(denID, bob, true); err != nil {
		t.Fatal(err)
	}
	a.waitKeys("its keys without bob's", func(k denclient.CallKeys) bool { _, ok := k.Members[bob]; return !ok && len(k.Members) == 1 })
	if err := owner.Withhold(denID, bob, false); err != nil {
		t.Fatal(err)
	}
	a.waitKeys("bob's key again", func(k denclient.CallKeys) bool { _, ok := k.Members[bob]; return ok })

	// Carol leaves: alice's and bob's keys change again, without hers.
	third.LeaveCall("page-c", denID)
	viewOf(t, owner, denID, "carol gone", showsCall(lounge, alice, bob))
	third3 := settled("alice's and bob's keys after carol left", side{a, alice}, side{b, bob})
	for _, id := range []string{alice, bob} {
		if bytes.Equal(third3[id].Own.Key, second[id].Own.Key) {
			t.Errorf("member %s's key didn't change as carol left", id)
		}
	}
}
