package den

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"

	"github.com/coder/websocket"
)

// fakeKey is a member's key for a call as the den sees one: of the right
// shape, sealed to each member named, each copy filled with its own byte.
// The den checks no more, so nothing in it needs to open.
func fakeKey(channel string, copies map[string]byte) denproto.VoiceKey {
	k := denproto.VoiceKey{ChannelID: channel, KID: denproto.Random(denproto.KIDSize), Commit: denproto.Random(denproto.KeyCommitSize),
		Maker:     denproto.KeyMaker{KEMHash: denproto.Random(denproto.IdentityHashSize), Sig: denproto.Random(denproto.PublicKeySize)},
		Signature: denproto.Random(denproto.SignatureSize)}
	ids := make([]string, 0, len(copies))
	for id := range copies {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		x, _ := denproto.ParseID(a)
		y, _ := denproto.ParseID(b)
		return int(x - y)
	})
	for _, id := range ids {
		k.Members = append(k.Members, denproto.KeyMember{MemberID: id, Identity: denproto.Random(denproto.IdentityHashSize)})
		k.Copies = append(k.Copies, denproto.KeyCopy{MemberID: id, Copy: bytes.Repeat([]byte{copies[id]}, denproto.CallKeyCopySize)})
	}
	return k
}

// keyFrom matches a key that reached a member: from maker, with the copy
// filled with fill, and the key's own ID.
func keyFrom(maker string, kid []byte, fill byte) (string, func(denproto.Event) bool) {
	return "member " + maker + "'s key", func(e denproto.Event) bool {
		var k denproto.VoiceKeyEvent
		return e.T == denproto.EventVoiceKey && json.Unmarshal(e.D, &k) == nil && k.MemberID == maker && bytes.Equal(k.KID, kid) &&
			bytes.Equal(k.Copy, bytes.Repeat([]byte{fill}, denproto.CallKeyCopySize))
	}
}

// keysAmong returns the keys among what a socket has received so far: by,
// a member in the call, mutes and unmutes, and the den's word of it
// reaches the socket after every key passed to it before, as both go
// through its subscription.
func (v *voiceSocket) keysAmong(channel string, by *voiceSocket) []denproto.VoiceKeyEvent {
	v.t.Helper()
	by.send(denproto.EventVoiceMute, denproto.VoiceMute{Muted: true})
	by.send(denproto.EventVoiceMute, denproto.VoiceMute{})
	var got []denproto.VoiceKeyEvent
	muted := false
	v.wait("a round trip through the den", func(e denproto.Event) bool {
		var k denproto.VoiceKeyEvent
		if e.T == denproto.EventVoiceKey && json.Unmarshal(e.D, &k) == nil {
			got = append(got, k)
		}
		var s denproto.VoiceState
		if e.T != denproto.EventVoiceState || json.Unmarshal(e.D, &s) != nil {
			return false
		}
		for _, c := range s.Calls {
			i := slices.IndexFunc(c.Members, func(m denproto.CallMember) bool { return m.ID == by.me })
			if c.ChannelID != channel || i < 0 {
				continue
			}
			if c.Members[i].Muted {
				muted = true
			} else if muted {
				return true
			}
		}
		return false
	})
	return got
}

// TestCallKeysReachTheirMembersAlone checks the den's part in sealed calls
// (M16.2): a join that doesn't seal refused; a member's key passed to each
// other member of the call it holds a copy for, each their copy alone, and
// to no one outside the call; the latest from each kept while the call
// lasts, for a member whose call starts or is taken back; and a key for
// another call, a malformed one, and one past the member's limit, passed
// to no one.
func TestCallKeysReachTheirMembersAlone(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	bobToken := f.member(aliceToken, "bob")
	carolToken := f.member(aliceToken, "carol")
	lounge := f.channel(f.session(aliceToken), "Lounge", denproto.KindVoice, false)
	other := f.channel(f.session(aliceToken), "Other", denproto.KindVoice, false)
	alice := f.voiceSocket(aliceToken, newCaller(t))
	bob := f.voiceSocket(bobToken, newCaller(t))
	carol := f.voiceSocket(carolToken, newCaller(t))

	// An older Dens's page would send in the clear: its join is refused. A
	// resume takes back only a call the den holds, every one sealed, and
	// finds none.
	alice.send(denproto.EventVoiceJoin, denproto.VoiceJoin{ChannelID: lounge.ID})
	alice.wait(ended(lounge.ID, denproto.VoiceUpdateNeeded))
	alice.send(denproto.EventVoiceJoin, denproto.VoiceJoin{ChannelID: lounge.ID, Resume: true})
	alice.wait(ended(lounge.ID, denproto.VoiceFailed))
	if info, _ := f.d.Info(); !info.SealedCalls {
		t.Error("the den's info doesn't say it seals calls")
	}

	alice.join(lounge.ID)
	carol.wait(inCall(lounge.ID, alice.me))
	bob.join(lounge.ID)
	carol.wait(inCall(lounge.ID, alice.me, bob.me))

	// Alice's key reaches bob with his copy; carol, outside the call,
	// gets nothing.
	first := fakeKey(lounge.ID, map[string]byte{bob.me: 'b'})
	alice.send(denproto.EventVoiceKey, first)
	bob.wait(keyFrom(alice.me, first.KID, 'b'))
	if got := carol.keysAmong(lounge.ID, bob); len(got) != 0 {
		t.Errorf("carol, outside the call, got %d keys", len(got))
	}

	// Carol joins: alice's kept key holds no copy for her, so she gets
	// none; alice's next, sealed to both, reaches each with their own.
	carol.join(lounge.ID)
	bob.wait(inCall(lounge.ID, alice.me, bob.me, carol.me))
	if got := carol.keysAmong(lounge.ID, carol); len(got) != 0 {
		t.Errorf("carol got %d keys made before she joined", len(got))
	}
	second := fakeKey(lounge.ID, map[string]byte{bob.me: 'B', carol.me: 'C'})
	alice.send(denproto.EventVoiceKey, second)
	bob.wait(keyFrom(alice.me, second.KID, 'B'))
	carol.wait(keyFrom(alice.me, second.KID, 'C'))
	if got := alice.keysAmong(lounge.ID, alice); len(got) != 0 {
		t.Errorf("alice got %d keys of her own back", len(got))
	}

	// A key for a call alice isn't in, and one with a copy for nobody it
	// names, go nowhere.
	alice.send(denproto.EventVoiceKey, fakeKey(other.ID, map[string]byte{bob.me: 'x'}))
	bad := fakeKey(lounge.ID, map[string]byte{bob.me: 'y'})
	bad.Copies[0].MemberID = carol.me
	alice.send(denproto.EventVoiceKey, bad)
	if got := bob.keysAmong(lounge.ID, bob); len(got) != 0 {
		t.Errorf("bob got %d keys the den should have refused", len(got))
	}

	// Bob's socket closes, which holds his call: carol's key made
	// meanwhile, and alice's latest, reach him as he takes it back.
	bob.c.Close(websocket.StatusGoingAway, "")
	time.Sleep(100 * time.Millisecond)
	fromCarol := fakeKey(lounge.ID, map[string]byte{alice.me: 'a', bob.me: 'k'})
	carol.send(denproto.EventVoiceKey, fromCarol)
	alice.wait(keyFrom(carol.me, fromCarol.KID, 'a'))
	again := f.voiceSocket(bobToken, bob.caller)
	again.version, again.answer = bob.version, bob.answer
	again.send(denproto.EventVoiceJoin, denproto.VoiceJoin{ChannelID: lounge.ID, Resume: true, Sealed: true})
	again.wait("the call to resume", func(e denproto.Event) bool { return e.T == denproto.EventVoiceResumed })
	again.wait(keyFrom(alice.me, second.KID, 'B'))
	again.wait(keyFrom(carol.me, fromCarol.KID, 'k'))

	// Carol leaves and joins again: alice's latest key, still holding a
	// copy for her, reaches her as her call starts, as when a member moves
	// their call to another device.
	carol.send(denproto.EventVoiceLeave, struct{}{})
	alice.wait(inCall(lounge.ID, alice.me, bob.me))
	carol.join(lounge.ID)
	carol.wait(keyFrom(alice.me, second.KID, 'C'))

	// Past 30 keys in 10 seconds, alice's go nowhere: those she sent
	// above count too.
	sent := denproto.VoiceKeysPer10s + 5
	for range sent {
		alice.send(denproto.EventVoiceKey, fakeKey(lounge.ID, map[string]byte{bob.me: 'r', carol.me: 'r'}))
	}
	if got := again.keysAmong(lounge.ID, again); len(got) == 0 || len(got) >= sent-4 {
		t.Errorf("of %d keys sent at once, %d reached bob", sent, len(got))
	}
}
