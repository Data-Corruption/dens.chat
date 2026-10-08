package den

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
)

// TestResumeTakesOnlyTheDevice checks that only the device that held a call
// takes it back, that the offer it hadn't answered goes again, that a call
// ended while held tells its resume why, and that a socket the den closed
// for good ends its call instead of holding it.
func TestResumeTakesOnlyTheDevice(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.StartCalls(udp, tcp, sfu.Config{Loopback: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.StopCalls() })
	lounge := f.newChannel(owner, "Lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	sock := func(key []byte) *socket {
		return &socket{member: member.MemberID, keyID: key, closeReq: make(chan closeRequest, 1), voiceOut: make(chan denproto.Event, 16)}
	}
	next := func(s *socket, want string) denproto.Event {
		t.Helper()
		select {
		case e := <-s.voiceOut:
			if e.T != want {
				t.Fatalf("got %s %s, want %s", e.T, e.D, want)
			}
			return e
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s", want)
		}
		return denproto.Event{}
	}
	endedFor := func(s *socket, reason string) {
		t.Helper()
		var x denproto.VoiceEnded
		if e := next(s, denproto.EventVoiceEnded); json.Unmarshal(e.D, &x) != nil || x.Reason != reason {
			t.Fatalf("ended %s, want %s", e.D, reason)
		}
	}
	join := denproto.VoiceJoin{ChannelID: lounge.ID}
	resume := denproto.VoiceJoin{ChannelID: lounge.ID, Resume: true}

	first := sock(member.KeyID)
	f.d.joinCall(ctx, member, first, sub, join)
	next(first, denproto.EventVoiceOffer)
	f.d.socketClosed(first)

	other := sock(denproto.Random(32))
	f.d.joinCall(ctx, member, other, sub, resume)
	endedFor(other, denproto.VoiceFailed)

	again := sock(member.KeyID)
	f.d.joinCall(ctx, member, again, sub, resume)
	next(again, denproto.EventVoiceResumed)
	var o denproto.VoiceOffer
	if e := next(again, denproto.EventVoiceOffer); json.Unmarshal(e.D, &o) != nil || o.Version != 1 {
		t.Fatalf("the offer sent again: %s", e.D)
	}

	// Disconnected by staff while held, the call tells its resume why.
	f.d.socketClosed(again)
	if err := f.d.DisconnectFromCall(ctx, owner, denproto.FormatID(member.MemberID)); err != nil {
		t.Fatal(err)
	}
	later := sock(member.KeyID)
	f.d.joinCall(ctx, member, later, sub, resume)
	endedFor(later, denproto.VoiceDisconnectedByStaff)
	// Once.
	f.d.joinCall(ctx, member, later, sub, resume)
	endedFor(later, denproto.VoiceFailed)

	// A socket closed for good takes its call with it.
	f.d.joinCall(ctx, member, later, sub, join)
	next(later, denproto.EventVoiceOffer)
	later.requestClose(denproto.CloseRevoked, denproto.CloseReasonKeyRevoked)
	f.d.socketClosed(later)
	if c := f.d.socketCall(later); c != nil || len(f.d.visibleCalls(true)) != 0 {
		t.Fatal("a socket closed for good left its call held")
	}
}
