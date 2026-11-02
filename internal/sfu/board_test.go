package sfu

import (
	"errors"
	"io"
	"math/big"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu/sfutest"
)

// testSound plays n packets of 20 ms, each naming its place in its last
// two bytes.
type testSound struct {
	n, at  int
	closed atomic.Bool
}

func (s *testSound) Next() ([]byte, int, error) {
	if s.at >= s.n {
		return nil, 0, io.EOF
	}
	s.at++
	return []byte{0xfc, byte((s.at - 1) >> 8), byte(s.at - 1)}, 960, nil
}

func (s *testSound) Close() error {
	s.closed.Store(true)
	return nil
}

// placeOf is the place in its sound a soundboard packet names.
func placeOf(p sfutest.BoardPacket) int { return int(p.Payload[1])<<8 | int(p.Payload[2]) }

// play plays a sound of n packets in a room, and returns a channel that
// closes once it's over.
func play(t *testing.T, s *SFU, room string, sound Sound) (func(), <-chan struct{}) {
	t.Helper()
	done := make(chan struct{})
	stop, err := s.Play(room, sound, func() { close(done) })
	if err != nil {
		t.Fatal(err)
	}
	return stop, done
}

func over(t *testing.T, done <-chan struct{}, within time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("the sound wasn't over within %s", within)
	}
}

// heardInOrder checks what reached a member on the soundboard: the
// sound's packets, in order, from first to last, with nearly none lost on
// loopback, each timestamp 960 on from the one before by its place.
func heardInOrder(t *testing.T, who string, got []sfutest.BoardPacket, first, last int) {
	t.Helper()
	if len(got) == 0 {
		t.Fatalf("%s heard nothing on the soundboard", who)
	}
	if a, b := placeOf(got[0]), placeOf(got[len(got)-1]); a > first || b < last {
		t.Errorf("%s heard the sound from packet %d to %d, not %d to %d", who, a, b, first, last)
	}
	for i := 1; i < len(got); i++ {
		gap := placeOf(got[i]) - placeOf(got[i-1])
		if gap < 1 {
			t.Fatalf("%s heard packet %d after %d", who, placeOf(got[i]), placeOf(got[i-1]))
		}
		if d := got[i].Timestamp - got[i-1].Timestamp; d != uint32(960*gap) {
			t.Errorf("%s's packet %d is %d samples after the one before it, not %d", who, placeOf(got[i]), d, 960*gap)
		}
	}
	if lost := last - first + 1 - len(got); lost > (last-first)/10 {
		t.Errorf("%s lost %d of the sound's packets", who, lost)
	}
}

// TestASoundReachesTheCall checks that every member's offer carries the
// call's soundboard, in stereo, and that a sound played there reaches
// everyone in the call, the member who played it among them, in order and
// at its pace.
func TestASoundReachesTheCall(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", sfutest.Options{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", sfutest.Options{UDP: true}, true)
	carol := join(t, s, udpPort, tcpPort, "41", "1003", sfutest.Options{UDP: true}, true)
	carol.hears(alice)
	alice.hears(carol)
	for _, m := range []*member{alice, bob, carol} {
		first := m.waitOffers(1)[0].sdp
		if board := sectionOf(first, "audio", denproto.SoundboardStream); !strings.Contains(board, "a=sendonly") ||
			!strings.Contains(board, "a=fmtp:111 "+soundParams) {
			t.Errorf("member %s's first offer has no soundboard in stereo:\n%s", m.id, first)
		}
	}

	sound := &testSound{n: 50}
	start := time.Now()
	_, done := play(t, s, "41", sound)
	over(t, done, 10*time.Second)
	if took := time.Since(start); took < 900*time.Millisecond {
		t.Errorf("a second's sound played in %s", took)
	}
	if !sound.closed.Load() {
		t.Error("the sound wasn't closed once it was over")
	}
	time.Sleep(300 * time.Millisecond)
	for _, m := range []*member{alice, bob, carol} {
		got := m.caller.Board()
		heardInOrder(t, m.id, got, 0, 49)
		if len(got) > 1 {
			if span := got[len(got)-1].At.Sub(got[0].At); span < 800*time.Millisecond {
				t.Errorf("member %s heard a second's sound in %s", m.id, span)
			}
		}
	}
}

// TestAMemberJoiningPartwayHearsTheRest checks that a member who joins as
// a sound plays hears the rest of it.
func TestAMemberJoiningPartwayHearsTheRest(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", sfutest.Options{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", sfutest.Options{UDP: true}, true)
	bob.hears(alice)
	_, done := play(t, s, "41", &testSound{n: 150})
	time.Sleep(700 * time.Millisecond)
	carol := join(t, s, udpPort, tcpPort, "41", "1003", sfutest.Options{UDP: true}, true)
	if err := carol.caller.WaitBoard(10, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	over(t, done, 10*time.Second)
	time.Sleep(300 * time.Millisecond)
	got := carol.caller.Board()
	if first := placeOf(got[0]); first < 30 {
		t.Errorf("carol, who joined 700 ms in, heard the sound from packet %d", first)
	}
	heardInOrder(t, "carol", got, placeOf(got[0]), 149)
	heardInOrder(t, "bob", bob.caller.Board(), 0, 149)
}

// TestStoppingASoundEndsIt checks that a sound stopped partway ends at
// once, and is over.
func TestStoppingASoundEndsIt(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", sfutest.Options{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", sfutest.Options{UDP: true}, true)
	bob.hears(alice)
	sound := &testSound{n: 500}
	stop, done := play(t, s, "41", sound)
	if err := bob.caller.WaitBoard(10, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	stop()
	stop()
	over(t, done, time.Second)
	if !sound.closed.Load() {
		t.Error("the sound wasn't closed once it was stopped")
	}
	time.Sleep(200 * time.Millisecond)
	before := len(bob.caller.Board())
	time.Sleep(300 * time.Millisecond)
	if after := len(bob.caller.Board()); after != before || before > 100 {
		t.Errorf("bob heard %d packets of a 10 s sound stopped partway, and %d more after", before, after-before)
	}
}

// TestOneSoundAtATime checks that a call plays one sound at a time, that
// another can play once it's over, picking up its timestamps from the
// first's by the time between, and that a call nobody is in plays none.
func TestOneSoundAtATime(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", sfutest.Options{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", sfutest.Options{UDP: true}, true)
	bob.hears(alice)
	_, done := play(t, s, "41", &testSound{n: 10})
	second := &testSound{n: 10}
	if _, err := s.Play("41", second, func() {}); !errors.Is(err, ErrPlaying) || !second.closed.Load() {
		t.Errorf("a second sound at once gave %v, and closed: %t", err, second.closed.Load())
	}
	nowhere := &testSound{n: 10}
	if _, err := s.Play("42", nowhere, func() {}); err == nil || !nowhere.closed.Load() {
		t.Errorf("a sound in a call nobody is in gave %v, and closed: %t", err, nowhere.closed.Load())
	}
	over(t, done, 5*time.Second)
	time.Sleep(500 * time.Millisecond)
	_, done = play(t, s, "41", &testSound{n: 10})
	over(t, done, 5*time.Second)
	time.Sleep(200 * time.Millisecond)
	got := bob.caller.Board()
	if len(got) < 15 {
		t.Fatalf("bob heard %d packets of two sounds of 10", len(got))
	}
	// The second sound's first packet follows the first's last by the
	// half second and more between them, as samples.
	for i := 1; i < len(got); i++ {
		if placeOf(got[i]) < placeOf(got[i-1]) {
			gap := got[i].Timestamp - got[i-1].Timestamp
			if gap < 960+20000 || gap > 960+48000 {
				t.Errorf("the second sound starts %d samples after the first's last packet, over half a second later", gap)
			}
			return
		}
	}
	t.Error("bob heard no second sound")
}

// TestTheLargestCallsOfferFits checks that an offer in a call as large as
// a den lets one be, to a member who shares and watches as many shares as
// a member may, with the soundboard and the den's candidates for four
// addresses, still fits MaxSDP and checks out.
// The track's timestamps run on through the silence between sounds by its
// length, which counts exactly however long a call goes quiet.
func TestTheSilenceBetweenSoundsCountsAtAnyLength(t *testing.T) {
	for _, d := range []time.Duration{0, 20 * time.Millisecond, 1500*time.Millisecond + 7, time.Hour, 60 * time.Hour, 400 * 24 * time.Hour} {
		want := new(big.Int).Mul(big.NewInt(int64(d)), big.NewInt(opusRate))
		want.Div(want, big.NewInt(int64(time.Second))).Mod(want, big.NewInt(1<<32))
		if got := samplesIn(d); uint64(got) != want.Uint64() {
			t.Errorf("%v: %d samples, want %d", d, got, want.Uint64())
		}
	}
}

func TestTheLargestCallsOfferFits(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{AnswerTimeout: time.Minute, ConnectTimeout: time.Minute})
	quiet := sfutest.Options{UDP: true, Rate: 5, ScreenRate: 10, ScreenBytes: 100}
	var all []*member
	for i := range denproto.MaxCallMembers {
		all = append(all, join(t, s, udpPort, tcpPort, "41", strconv.Itoa(3000+i), quiet, true))
	}
	me := all[0]
	me.share(true, 2_000_000)
	for _, sharer := range all[1 : 1+denproto.MaxWatching] {
		sharer.share(true, 2_000_000)
		me.watch(sharer)
	}
	// Its microphone, its share's screen and sound, everyone else's voice,
	// the shares it watches, and the soundboard.
	sections := 1 + 2 + (denproto.MaxCallMembers - 1) + 2*denproto.MaxWatching + 1
	o := me.waitOffer("with every section", func(sdp string) bool {
		return strings.Count(sdp, "\r\nm=") == sections && count(sdp, "video", "sendonly") == denproto.MaxWatching
	})
	addrs := []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("198.51.100.20"),
		netip.MustParseAddr("2001:db8::10"), netip.MustParseAddr("2001:db8:ffff:ffff:ffff:ffff:ffff:ffff")}
	sdp, err := denproto.AddCandidates(o.sdp, addrs, udpPort, tcpPort)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the largest call's offer is %d bytes, of %d", len(sdp), denproto.MaxSDP)
	if err := denproto.CheckOffer(sdp); err != nil {
		t.Errorf("the largest call's offer: %v", err)
	}
}
