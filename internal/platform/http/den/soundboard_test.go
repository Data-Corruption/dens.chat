package den

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	dens "github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
	"github.com/Data-Corruption/dens.chat/internal/sfu/sfutest"

	"github.com/coder/websocket"
)

// addSound adds a sound of n packets of 20 ms to the den's soundboard, as
// staff's client does: an upload, then the sound naming it.
func (f *fixture) addSound(token denproto.Bytes, name string, n int) denproto.Sound {
	f.t.Helper()
	file := sfutest.Sound(sfutest.SoundPackets(n, 160))
	resp := f.upload(token, name+".ogg", bytes.NewReader(file), int64(len(file)))
	var up denproto.File
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&up) != nil {
		f.t.Fatalf("upload a sound: %d %v", resp.StatusCode, denproto.ReadError(resp))
	}
	resp.Body.Close()
	resp = f.post("/api/soundboard", denproto.SoundRequest{Name: name, FileID: up.ID}, bearer(token))
	defer resp.Body.Close()
	var sound denproto.Sound
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&sound) != nil || denproto.CheckSound(sound) != nil {
		f.t.Fatalf("add a sound: %d %+v %v", resp.StatusCode, sound, denproto.ReadError(resp))
	}
	return sound
}

func bearer(token denproto.Bytes) http.Header {
	return http.Header{"Authorization": {"Bearer " + token.String()}}
}

func (f *fixture) soundLimits(owner *dens.Session, l denproto.SoundLimits) {
	f.t.Helper()
	if _, err := f.d.Update(context.Background(), owner, denproto.DenUpdateRequest{SoundLimits: &l}); err != nil {
		f.t.Fatal(err)
	}
}

func (v *voiceSocket) play(sound string) {
	v.send(denproto.EventVoiceSound, denproto.VoiceSound{SoundID: sound})
}

// sounding matches word of whose sound plays in a call, "" for none.
func sounding(channel, member string) (string, func(denproto.Event) bool) {
	return "member " + member + "'s sound to play in " + channel, func(e denproto.Event) bool {
		var s denproto.VoiceSounding
		return e.T == denproto.EventVoiceSounding && json.Unmarshal(e.D, &s) == nil && s.ChannelID == channel && s.MemberID == member
	}
}

// heardSound checks what reached a caller on the call's soundboard: the
// sound's packets in order, from its first to its last, nearly none lost
// on loopback, at their pace.
func heardSound(t *testing.T, who string, c *sfutest.Caller, from, to int) {
	t.Helper()
	got := c.Board()
	if len(got) == 0 {
		t.Fatalf("%s heard nothing on the soundboard", who)
	}
	if a, b := sfutest.PlaceOf(got[0].Payload), sfutest.PlaceOf(got[len(got)-1].Payload); a > from || b < to {
		t.Errorf("%s heard the sound from packet %d to %d, not %d to %d", who, a, b, from, to)
	}
	for i := 1; i < len(got); i++ {
		if sfutest.PlaceOf(got[i].Payload) <= sfutest.PlaceOf(got[i-1].Payload) {
			t.Fatalf("%s heard packet %d after %d", who, sfutest.PlaceOf(got[i].Payload), sfutest.PlaceOf(got[i-1].Payload))
		}
	}
	if lost := to - from + 1 - len(got); lost > (to-from)/10 {
		t.Errorf("%s lost %d of the sound's packets", who, lost)
	}
	if span, want := got[len(got)-1].At.Sub(got[0].At), time.Duration(to-from)*20*time.Millisecond*8/10; span < want {
		t.Errorf("%s heard %d packets of 20 ms in %s", who, to-from+1, span)
	}
}

// TestTheSoundboardOverHTTP checks the soundboard's routes: staff add,
// rename and delete sounds, and a member can't.
func TestTheSoundboardOverHTTP(t *testing.T) {
	f := newFixture(t)
	ownerToken, _ := f.owner()
	bobToken := f.member(ownerToken, "bob")
	horn := f.addSound(ownerToken, "Horn", 10)
	file := sfutest.Sound(sfutest.SoundPackets(10, 160))
	resp := f.upload(bobToken, "mine.ogg", bytes.NewReader(file), int64(len(file)))
	var up denproto.File
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	wantError(t, f.post("/api/soundboard", denproto.SoundRequest{Name: "Mine", FileID: up.ID}, bearer(bobToken)), http.StatusForbidden, denproto.CodeForbidden)

	send := func(method, path string, body any, token denproto.Bytes) *http.Response {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, f.srv.URL+path, bytes.NewReader(data))
		req.Header.Set(denproto.HeaderVersion, "1")
		req.Header.Set("Authorization", "Bearer "+token.String())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	wantError(t, send(http.MethodPatch, "/api/soundboard/"+horn.ID, denproto.SoundUpdate{Name: "Mine"}, bobToken), http.StatusForbidden, denproto.CodeForbidden)
	resp = send(http.MethodPatch, "/api/soundboard/"+horn.ID, denproto.SoundUpdate{Name: "Big horn"}, ownerToken)
	var renamed denproto.Sound
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&renamed) != nil || renamed.Name != "Big horn" {
		t.Fatalf("rename: %d %+v", resp.StatusCode, renamed)
	}
	resp.Body.Close()
	// A name another sound has, in any case, is refused as taken.
	bell := f.addSound(ownerToken, "Bell", 10)
	wantError(t, send(http.MethodPatch, "/api/soundboard/"+bell.ID, denproto.SoundUpdate{Name: "big HORN"}, ownerToken), http.StatusConflict, denproto.CodeSoundNameTaken)
	wantError(t, send(http.MethodDelete, "/api/soundboard/"+horn.ID, nil, bobToken), http.StatusForbidden, denproto.CodeForbidden)
	if resp := send(http.MethodDelete, "/api/soundboard/"+horn.ID, nil, ownerToken); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	wantError(t, send(http.MethodDelete, "/api/soundboard/"+horn.ID, nil, ownerToken), http.StatusNotFound, denproto.CodeNotFound)
}

// TestASoundPlaysInTheCall checks a sound played in a call: the call's
// members hear of its player as it starts and of none as it ends, and each
// receives its file's packets, as the page wrote them, in order and at
// their pace; and the den refuses one asked for outside a call, while
// another plays, before the player's wait has passed, while staff mute
// them, with the soundboard off, or that it no longer has.
func TestASoundPlaysInTheCall(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	f.soundLimits(owner, denproto.SoundLimits{Sounds: 10, Longest: 10, Wait: 1})
	horn := f.addSound(aliceToken, "Horn", 50)

	aliceCaller, bobCaller := newCaller(t), newCaller(t)
	alice := f.voiceSocket(aliceToken, aliceCaller)
	bob := f.voiceSocket(bobToken, bobCaller)
	// Outside a call, nothing plays.
	bob.play(horn.ID)
	alice.join(lounge.ID)
	bob.wait(inCall(lounge.ID, alice.me))
	bob.join(lounge.ID)
	// A socket's call hears whose sound plays as it starts: none yet.
	bob.wait(sounding(lounge.ID, ""))
	alice.wait(inCall(lounge.ID, alice.me, bob.me))
	if err := bobCaller.WaitHeard(alice.me, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if n := len(bobCaller.Board()); n != 0 {
		t.Fatalf("bob heard %d packets on the soundboard before anyone played", n)
	}

	alice.play(horn.ID)
	alice.wait(sounding(lounge.ID, alice.me))
	bob.wait(sounding(lounge.ID, alice.me))
	bob.play(horn.ID)
	bob.wait(refused(lounge.ID, denproto.RefusedSound, denproto.RefusedPlaying))
	bob.wait(sounding(lounge.ID, ""))
	alice.wait(sounding(lounge.ID, ""))
	time.Sleep(300 * time.Millisecond)
	heardSound(t, "bob", bobCaller, 0, 49)
	heardSound(t, "alice", aliceCaller, 0, 49)

	alice.play(horn.ID)
	alice.wait(refused(lounge.ID, denproto.RefusedSound, denproto.RefusedWait))
	time.Sleep(1100 * time.Millisecond)
	alice.play(horn.ID)
	alice.wait(sounding(lounge.ID, alice.me))
	alice.wait(sounding(lounge.ID, ""))

	if err := f.d.SetStaffMute(context.Background(), owner, bob.me, true); err != nil {
		t.Fatal(err)
	}
	bob.play(horn.ID)
	bob.wait(refused(lounge.ID, denproto.RefusedSound, denproto.RefusedStaffMuted))
	if err := f.d.SetStaffMute(context.Background(), owner, bob.me, false); err != nil {
		t.Fatal(err)
	}
	f.soundLimits(owner, denproto.SoundLimits{Sounds: 0, Longest: 10, Wait: 1})
	bob.play(horn.ID)
	bob.wait(refused(lounge.ID, denproto.RefusedSound, denproto.RefusedOff))
	f.soundLimits(owner, denproto.SoundLimits{Sounds: 10, Longest: 10, Wait: 1})
	bob.play("999")
	bob.wait(refused(lounge.ID, denproto.RefusedSound, denproto.VoiceNotFound))
	bob.play(horn.ID)
	alice.wait(sounding(lounge.ID, bob.me))
}

// TestASoundEndsWithItsPlayer checks that a sound ends early as its player
// leaves the call, and as staff mute or disconnect them, and plays on
// through anyone else leaving.
func TestASoundEndsWithItsPlayer(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	carolToken := f.member(aliceToken, "carol")
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	f.soundLimits(owner, denproto.SoundLimits{Sounds: 10, Longest: 10, Wait: 0})
	long := f.addSound(aliceToken, "Long", 500)

	aliceCaller := newCaller(t)
	alice := f.voiceSocket(aliceToken, aliceCaller)
	alice.join(lounge.ID)
	alice.wait(inCall(lounge.ID, alice.me))
	quiet := func() {
		t.Helper()
		time.Sleep(300 * time.Millisecond)
		before := len(aliceCaller.Board())
		time.Sleep(300 * time.Millisecond)
		if after := len(aliceCaller.Board()); after != before {
			t.Errorf("alice heard %d packets of a sound that ended", after-before)
		}
	}
	for _, end := range []struct {
		what string
		do   func(bob *voiceSocket)
	}{
		{"leaving", func(bob *voiceSocket) { bob.send(denproto.EventVoiceLeave, struct{}{}) }},
		{"muted by staff", func(bob *voiceSocket) {
			if err := f.d.SetStaffMute(context.Background(), owner, bob.me, true); err != nil {
				t.Fatal(err)
			}
		}},
		{"disconnected by staff", func(bob *voiceSocket) {
			if err := f.d.DisconnectFromCall(context.Background(), owner, bob.me); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		bob := f.voiceSocket(bobToken, newCaller(t))
		carol := f.voiceSocket(carolToken, newCaller(t))
		bob.join(lounge.ID)
		alice.wait(inCall(lounge.ID, alice.me, bob.me))
		carol.join(lounge.ID)
		alice.wait(inCall(lounge.ID, alice.me, bob.me, carol.me))
		bob.play(long.ID)
		alice.wait(sounding(lounge.ID, bob.me))
		if err := aliceCaller.WaitBoard(10, 10*time.Second); err != nil {
			t.Fatal(err)
		}
		// Someone else leaving leaves the sound playing.
		carol.send(denproto.EventVoiceLeave, struct{}{})
		alice.wait(inCall(lounge.ID, alice.me, bob.me))
		if err := aliceCaller.WaitBoard(10, 5*time.Second); err != nil {
			t.Fatalf("after carol left: %v", err)
		}
		end.do(bob)
		alice.wait(sounding(lounge.ID, ""))
		quiet()
		if err := f.d.SetStaffMute(context.Background(), owner, bob.me, false); err != nil {
			t.Fatal(err)
		}
		// A call whose socket closes is held, so bob leaves it first.
		bob.send(denproto.EventVoiceLeave, struct{}{})
		bob.c.Close(websocket.StatusNormalClosure, "")
		carol.c.Close(websocket.StatusNormalClosure, "")
		alice.wait(inCall(lounge.ID, alice.me))
		t.Logf("a sound ended as its player was %s", end.what)
	}
}

// TestASoundReachesWhoJoinsPartway checks that a member joining as a sound
// plays hears of its player and receives the rest of it, and that a held
// call taken back hears again whose sound plays.
func TestASoundReachesWhoJoinsPartway(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	carolToken := f.member(aliceToken, "carol")
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	long := f.addSound(aliceToken, "Long", 150)

	aliceCaller, bobCaller, carolCaller := newCaller(t), newCaller(t), newCaller(t)
	alice := f.voiceSocket(aliceToken, aliceCaller)
	bob := f.voiceSocket(bobToken, bobCaller)
	alice.join(lounge.ID)
	bob.wait(inCall(lounge.ID, alice.me))
	bob.join(lounge.ID)
	alice.wait(inCall(lounge.ID, alice.me, bob.me))
	if err := bobCaller.WaitHeard(alice.me, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	alice.play(long.ID)
	bob.wait(sounding(lounge.ID, alice.me))

	// Bob's socket closes, and his call is held, its media flowing; the
	// socket that takes it back hears whose sound plays.
	bob.c.Close(websocket.StatusGoingAway, "")
	again := f.voiceSocket(bobToken, bobCaller)
	again.version, again.answer = bob.version, bob.answer
	again.send(denproto.EventVoiceJoin, denproto.VoiceJoin{ChannelID: lounge.ID, Resume: true})
	again.wait("the call to resume", func(e denproto.Event) bool { return e.T == denproto.EventVoiceResumed })
	again.wait(sounding(lounge.ID, alice.me))

	time.Sleep(500 * time.Millisecond)
	carol := f.voiceSocket(carolToken, carolCaller)
	carol.join(lounge.ID)
	carol.wait(sounding(lounge.ID, alice.me))
	if err := carolCaller.WaitBoard(10, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	carol.wait(sounding(lounge.ID, ""))
	again.wait(sounding(lounge.ID, ""))
	time.Sleep(300 * time.Millisecond)
	got := carolCaller.Board()
	if first := sfutest.PlaceOf(got[0].Payload); first < 25 {
		t.Errorf("carol, who joined partway, heard the sound from packet %d", first)
	}
	heardSound(t, "carol", carolCaller, sfutest.PlaceOf(got[0].Payload), 149)
	heardSound(t, "bob", bobCaller, 0, 149)
}
