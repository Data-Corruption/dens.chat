package den

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// boxesBefore reads a socket's events until fence matches one, and returns
// the boxes that came before it, in order.
func (v *voiceSocket) boxesBefore(what string, fence func(denproto.Event) bool) []denproto.RelaySignalEvent {
	v.t.Helper()
	var got []denproto.RelaySignalEvent
	v.wait(what, func(e denproto.Event) bool {
		if fence(e) {
			return true
		}
		var b denproto.RelaySignalEvent
		if e.T == denproto.EventRelaySignal && json.Unmarshal(e.D, &b) == nil {
			got = append(got, b)
		}
		return false
	})
	return got
}

// aBox matches a box that reached a device: from the one named, for the
// one named or every device, holding fill alone.
func aBox(from, to []byte, fill byte) func(denproto.Event) bool {
	return func(e denproto.Event) bool {
		var b denproto.RelaySignalEvent
		return e.T == denproto.EventRelaySignal && json.Unmarshal(e.D, &b) == nil && bytes.Equal(b.From, from) && bytes.Equal(b.To, to) &&
			bytes.Equal(b.Box, bytes.Repeat([]byte{fill}, denproto.RelayBoxSize))
	}
}

func filled(fill byte) denproto.Bytes { return bytes.Repeat([]byte{fill}, denproto.RelayBoxSize) }

// relayDevices is alice, the owner, on her desktop and her laptop, which
// her desktop approved, and bob, a member, each with a socket.
type relayDevices struct {
	desktop, laptop, bob          *voiceSocket
	desktopKey, laptopKey, bobKey denproto.Bytes
	general, fence                denproto.Channel
	aliceToken, laptopToken       denproto.Bytes
}

func (f *fixture) relayDevices() relayDevices {
	f.t.Helper()
	code, err := f.d.Create(f.t.Context(), "Den", "https://den.test")
	if err != nil {
		f.t.Fatal(err)
	}
	info, _ := f.d.Info()
	verifier := denproto.Verifier("password", info.ID, "alice")
	_, desktopKey, _ := ed25519.GenerateKey(nil)
	resp, err := f.d.Challenge(denproto.Random(denproto.NonceSize))
	if err != nil {
		f.t.Fatal(err)
	}
	joined, err := f.d.Join(f.t.Context(), denproto.JoinRequest{Invite: code, Username: "alice", DisplayName: "Alice", Verifier: verifier,
		PublicKey: denproto.Bytes(desktopKey.Public().(ed25519.PublicKey)), DeviceLabel: "desktop", Nonce: resp.Nonce,
		Proof: denproto.Prove(desktopKey, info.ID, resp.Nonce), SealCheck: denproto.Random(denproto.SealCheckSize)})
	if err != nil {
		f.t.Fatal(err)
	}
	_, laptopKey, _ := ed25519.GenerateKey(nil)
	laptopToken := f.approve(joined.Token, "alice", verifier, laptopKey)
	bobToken := f.member(joined.Token, "bob")
	bobSession := f.session(bobToken)
	r := relayDevices{
		desktop: f.voiceSocket(joined.Token, nil), laptop: f.voiceSocket(laptopToken, nil), bob: f.voiceSocket(bobToken, nil),
		desktopKey: denproto.ID(desktopKey.Public().(ed25519.PublicKey)), laptopKey: denproto.ID(laptopKey.Public().(ed25519.PublicKey)),
		bobKey: bobSession.KeyID, aliceToken: joined.Token, laptopToken: laptopToken,
	}
	// Typing has a limit of its own in each channel, so each fence types
	// in one of two.
	r.general = f.channel(f.session(joined.Token), "general", denproto.KindText, false)
	r.fence = f.channel(f.session(joined.Token), "fence", denproto.KindText, false)
	both := denproto.Focus{Channels: []string{r.general.ID, r.fence.ID}}
	r.bob.send(denproto.EventFocus, both)
	r.desktop.send(denproto.EventFocus, both)
	return r
}

// typingFrom is the fence for bob: alice typing in general, which reaches
// him after everything her desktop sent before.
func typingFrom(member string) func(denproto.Event) bool {
	return func(e denproto.Event) bool {
		var t denproto.Typing
		return e.T == denproto.EventTyping && json.Unmarshal(e.D, &t) == nil && t.MemberID == member
	}
}

// TestRelayBoxesReachTheMembersDevices checks the den's part in Relay
// (M20): a box passed to the member's other devices, or to the one it
// names, never back to the device it came from nor to another member's,
// one of the wrong size or for another member's device passed to no one,
// and nothing of any kept for a device that comes back.
func TestRelayBoxesReachTheMembersDevices(t *testing.T) {
	f := newFixture(t)
	r := f.relayDevices()
	if info, _ := f.d.Info(); !info.Relay {
		t.Error("the den's info doesn't say it passes boxes")
	}
	aliceID := f.session(r.aliceToken).MemberID

	// A box for every other device reaches the laptop alone, and one for
	// the laptop reaches it naming it.
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: filled('a')})
	r.laptop.wait("the desktop's box for every device", aBox(r.desktopKey, nil, 'a'))
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{To: r.laptopKey, Box: filled('b')})
	r.laptop.wait("the desktop's box for the laptop", aBox(r.desktopKey, r.laptopKey, 'b'))

	// One for bob's device, one too large and one too small go nowhere:
	// the laptop's next box is the one sent after them, and bob, fenced by
	// alice's typing, which follows them through the den, gets none.
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{To: r.bobKey, Box: filled('c')})
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: bytes.Repeat([]byte{'d'}, denproto.RelayBoxSize+1)})
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: bytes.Repeat([]byte{'e'}, denproto.RelayBoxSize-1)})
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: filled('f')})
	r.desktop.send(denproto.EventTyping, denproto.Typing{ChannelID: r.general.ID})
	if got := r.laptop.boxesBefore("the box sent last", aBox(r.desktopKey, nil, 'f')); len(got) != 0 {
		t.Errorf("the laptop got %d boxes it shouldn't have", len(got))
	}
	if got := r.bob.boxesBefore("alice typing", typingFrom(denproto.FormatID(aliceID))); len(got) != 0 {
		t.Errorf("bob got %d of alice's boxes", len(got))
	}

	// The laptop's box reaches the desktop, the first box the desktop
	// hears: none of its own came back to it.
	r.laptop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: filled('g')})
	if got := r.desktop.boxesBefore("the laptop's box", aBox(r.laptopKey, nil, 'g')); len(got) != 0 {
		t.Errorf("the desktop got %d boxes back", len(got))
	}

	// A box sent while the laptop is away is kept for nobody: the laptop,
	// resuming, finds the channel made meanwhile among what it missed, and
	// no box.
	c, first := f.dial(r.laptopToken, "")
	var ready denproto.Ready
	if err := json.Unmarshal(first[0].D, &ready); err != nil {
		t.Fatal(err)
	}
	c.CloseNow()
	r.laptop.c.CloseNow()
	time.Sleep(100 * time.Millisecond)
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: filled('h')})
	later := f.channel(f.session(r.aliceToken), "later", denproto.KindText, false)
	r.desktop.send(denproto.EventTyping, denproto.Typing{ChannelID: r.fence.ID})
	r.bob.wait("alice typing again", typingFrom(denproto.FormatID(aliceID)))
	c, frames := f.dial(r.laptopToken, ready.Epoch+"."+strconv.FormatUint(ready.Seq, 10))
	defer c.CloseNow()
	missed := false
	for _, e := range frames {
		switch e.T {
		case denproto.EventRelaySignal:
			t.Error("the laptop got a box sent while it was away")
		case denproto.EventChannelCreated:
			missed = missed || bytes.Contains(e.D, []byte(later.ID))
		}
	}
	if len(frames) == 0 || frames[0].T != denproto.EventResumed || !missed {
		t.Errorf("the laptop's resume missed what it should have: %+v", frames)
	}
}

// TestRelayBoxesHaveALimit checks that a member's devices send at most 30
// boxes at once, together, and the rest go nowhere.
func TestRelayBoxesHaveALimit(t *testing.T) {
	f := newFixture(t)
	r := f.relayDevices()
	const sent = 60
	for range sent {
		r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: filled('x')})
	}
	r.desktop.send(denproto.EventRelaySignal, denproto.RelaySignal{Box: filled('y')})
	got := 0
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-r.laptop.events:
			if aBox(r.desktopKey, nil, 'x')(e) {
				got++
			}
			continue
		case <-deadline:
		}
		break
	}
	if got < denproto.RelaySignalsPer10s-1 || got >= sent {
		t.Errorf("%d of %d boxes sent at once went through, against a limit of %d", got, sent, denproto.RelaySignalsPer10s)
	}
}
