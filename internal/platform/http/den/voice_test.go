package den

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"

	"github.com/coder/websocket"
)

// startCalls runs the den's calls on loopback ports.
func (f *fixture) startCalls(cfg sfu.Config) {
	f.t.Helper()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		f.t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		f.t.Fatal(err)
	}
	cfg.Loopback = true
	if err := f.d.StartCalls(udp, tcp, cfg); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) session(token denproto.Bytes) *dens.Session {
	f.t.Helper()
	s, err := f.d.Authenticate(context.Background(), token)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) channel(s *dens.Session, name, kind string, staffOnly bool) denproto.Channel {
	f.t.Helper()
	c, err := f.d.CreateChannel(context.Background(), s, denproto.ChannelRequest{Name: &name, Kind: kind, StaffOnly: &staffOnly})
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

// voiceSocket is a member's socket with what their client and browser do
// for a call: offers get the den's loopback candidates written in, and
// the caller answers them. Without a caller, offers go unanswered.
type voiceSocket struct {
	t      *testing.T
	c      *websocket.Conn
	me     string
	caller *sfu.TestCaller
	events chan denproto.Event
}

func (f *fixture) voiceSocket(token denproto.Bytes, caller *sfu.TestCaller) *voiceSocket {
	f.t.Helper()
	c, first := f.dial(token, "")
	var ready denproto.Ready
	if err := json.Unmarshal(first[0].D, &ready); err != nil {
		f.t.Fatal(err)
	}
	v := &voiceSocket{t: f.t, c: c, me: ready.Me.ID, caller: caller, events: make(chan denproto.Event, 1024)}
	f.t.Cleanup(func() { c.CloseNow() })
	go v.run()
	return v
}

func newCaller(t *testing.T) *sfu.TestCaller {
	t.Helper()
	c, err := sfu.NewTestCaller(sfu.CallerOptions{Loopback: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func (v *voiceSocket) run() {
	defer close(v.events)
	for {
		_, data, err := v.c.Read(context.Background())
		if err != nil {
			return
		}
		events, err := denproto.DecodeFrame(data)
		if err != nil {
			return
		}
		for _, e := range events {
			if e.T == denproto.EventVoiceOffer && v.caller != nil {
				var o denproto.VoiceOffer
				if json.Unmarshal(e.D, &o) != nil {
					return
				}
				sdp, err := denproto.AddCandidates(o.SDP, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, o.UDPPort, o.TCPPort)
				if err != nil {
					return
				}
				answer, err := v.caller.Answer(sdp)
				if err != nil {
					return
				}
				v.send(denproto.EventVoiceAnswer, denproto.VoiceAnswer{Version: o.Version, SDP: answer})
			}
			select {
			case v.events <- e:
			default:
			}
		}
	}
}

func (v *voiceSocket) send(t string, data any) {
	e, err := denproto.NewEvent(t, 0, data)
	if err != nil {
		v.t.Error(err)
		return
	}
	frame, _ := denproto.EncodeFrame(e)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = v.c.Write(ctx, websocket.MessageText, frame)
}

func (v *voiceSocket) join(channel string) {
	v.send(denproto.EventVoiceJoin, denproto.VoiceJoin{ChannelID: channel})
}

// wait reads events until one matches.
func (v *voiceSocket) wait(what string, match func(denproto.Event) bool) {
	v.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-v.events:
			if !ok {
				v.t.Fatalf("the socket closed waiting for %s", what)
			}
			if match(e) {
				return
			}
		case <-timeout:
			v.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// ended matches the end of a call, for a reason.
func ended(channel, reason string) (string, func(denproto.Event) bool) {
	return "the call in " + channel + " to end: " + reason, func(e denproto.Event) bool {
		var x denproto.VoiceEnded
		return e.T == denproto.EventVoiceEnded && json.Unmarshal(e.D, &x) == nil && x.ChannelID == channel && x.Reason == reason
	}
}

// inCall matches word of a call with exactly these members, in order; a
// member ending in "*" is muted.
func inCall(channel string, members ...string) (string, func(denproto.Event) bool) {
	return "the call in " + channel + " to hold " + strings.Join(members, ", "), func(e denproto.Event) bool {
		var s denproto.VoiceState
		if e.T != denproto.EventVoiceState || json.Unmarshal(e.D, &s) != nil {
			return false
		}
		for _, c := range s.Calls {
			if c.ChannelID == channel {
				return slices.Equal(callMembers(c), members)
			}
		}
		return false
	}
}

func callMembers(c denproto.Call) []string {
	var got []string
	for _, m := range c.Members {
		id := m.ID
		if m.Muted {
			id += "*"
		}
		got = append(got, id)
	}
	return got
}

func TestCallThroughTheDen(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	bobToken := f.member(aliceToken, "bob")
	lounge := f.channel(f.session(aliceToken), "Lounge", denproto.KindVoice, false)

	aliceCaller, bobCaller := newCaller(t), newCaller(t)
	alice := f.voiceSocket(aliceToken, aliceCaller)
	bob := f.voiceSocket(bobToken, bobCaller)
	alice.join(lounge.ID)
	bob.wait(inCall(lounge.ID, alice.me))
	bob.join(lounge.ID)
	alice.wait(inCall(lounge.ID, alice.me, bob.me))
	if err := aliceCaller.WaitHeard(bob.me, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := bobCaller.WaitHeard(alice.me, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	// A new socket's snapshot names who is in the call.
	c, first := f.dial(bobToken, "")
	var ready denproto.Ready
	if json.Unmarshal(first[0].D, &ready); len(ready.Calls) != 1 || !slices.Equal(callMembers(ready.Calls[0]), []string{alice.me, bob.me}) {
		t.Fatalf("ready's calls %+v", ready.Calls)
	}
	c.CloseNow()

	bob.send(denproto.EventVoiceMute, denproto.VoiceMute{Muted: true})
	alice.wait(inCall(lounge.ID, alice.me, bob.me+"*"))

	alice.send(denproto.EventVoiceLeave, struct{}{})
	bob.wait(inCall(lounge.ID, bob.me+"*"))

	// A call ends with the socket that joined it.
	bob.c.Close(websocket.StatusNormalClosure, "")
	alice.wait(inCall(lounge.ID))
}

func TestCallRefusals(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	general := f.channel(owner, "general", denproto.KindText, false)
	staff := f.channel(owner, "Staff", denproto.KindVoice, true)
	bob := f.voiceSocket(bobToken, nil)
	for _, id := range []string{general.ID, staff.ID, "999999", "not an id"} {
		bob.join(id)
		bob.wait(ended(id, denproto.VoiceNotFound))
	}
	// Joining counts against a limit of its own: four refusals above, and
	// six more joins, leave none.
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	for range 6 {
		bob.join(lounge.ID)
		bob.wait(inCall(lounge.ID, bob.me))
	}
	bob.join(lounge.ID)
	bob.wait(ended(lounge.ID, denproto.VoiceRateLimited))
}

func TestCallsEndWhenAccessDoes(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	staff := f.channel(owner, "Staff", denproto.KindVoice, true)
	alice := f.voiceSocket(aliceToken, nil)

	// Joining from another device moves the call there.
	first := f.voiceSocket(bobToken, nil)
	first.join(lounge.ID)
	alice.wait(inCall(lounge.ID, first.me))
	second := f.voiceSocket(bobToken, nil)
	second.join(lounge.ID)
	first.wait(ended(lounge.ID, denproto.VoiceMoved))
	alice.wait(inCall(lounge.ID, second.me))

	// A channel that becomes staff-only ends members' calls in it.
	yes := true
	if _, err := f.d.UpdateChannel(context.Background(), owner, lounge.ID, denproto.ChannelRequest{StaffOnly: &yes}); err != nil {
		t.Fatal(err)
	}
	second.wait(ended(lounge.ID, denproto.VoiceForbidden))
	alice.wait(inCall(lounge.ID))

	// A moderator joins a staff-only call, and loses it with the role.
	if _, err := f.d.SetRole(context.Background(), owner, second.me, denproto.RoleRequest{Role: denproto.RoleModerator}); err != nil {
		t.Fatal(err)
	}
	second.join(staff.ID)
	alice.wait(inCall(staff.ID, second.me))
	if _, err := f.d.SetRole(context.Background(), owner, second.me, denproto.RoleRequest{Role: denproto.RoleMember}); err != nil {
		t.Fatal(err)
	}
	second.wait(ended(staff.ID, denproto.VoiceForbidden))
	alice.wait(inCall(staff.ID))

	// Deleting a channel ends its call.
	alice.join(staff.ID)
	alice.wait(inCall(staff.ID, alice.me))
	if err := f.d.DeleteChannel(context.Background(), owner, staff.ID); err != nil {
		t.Fatal(err)
	}
	alice.wait(ended(staff.ID, denproto.VoiceDeleted))
}

func TestUnansweredCallFails(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{AnswerTimeout: 300 * time.Millisecond})
	aliceToken, _ := f.owner()
	lounge := f.channel(f.session(aliceToken), "Lounge", denproto.KindVoice, false)
	alice := f.voiceSocket(aliceToken, nil)
	watcher := f.voiceSocket(aliceToken, nil)
	alice.join(lounge.ID)
	alice.wait(ended(lounge.ID, denproto.VoiceFailed))
	watcher.wait(inCall(lounge.ID))
}

func TestCallHoldsFifteen(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	lounge := f.channel(f.session(aliceToken), "Lounge", denproto.KindVoice, false)
	var in []string
	for i := range denproto.MaxCallMembers {
		m := f.voiceSocket(f.member(aliceToken, "m"+strconv.Itoa(i)), nil)
		m.join(lounge.ID)
		in = append(in, m.me)
		m.wait(inCall(lounge.ID, in...))
	}
	late := f.voiceSocket(aliceToken, nil)
	late.join(lounge.ID)
	late.wait(ended(lounge.ID, denproto.VoiceFull))
}
