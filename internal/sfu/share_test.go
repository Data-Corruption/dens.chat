package sfu

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// waitOffer waits for an offer the member had, or has yet, that passes ok.
func (m *member) waitOffer(what string, ok func(sdp string) bool) offer {
	m.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := m.offersSeen()
		for i := len(got) - 1; i >= 0; i-- {
			if ok(got[i].sdp) {
				return got[i]
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.t.Fatalf("member %s had no offer %s", m.id, what)
	return offer{}
}

// eventually reports whether ok comes true within a few seconds.
func eventually(ok func() bool) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return true
		}
	}
	return ok()
}

// sections returns an offer's media sections, without their "m=".
func sections(sdp string) []string { return strings.Split(sdp, "\r\nm=")[1:] }

// sectionOf returns the section of an offer that sends a stream, or "".
func sectionOf(sdp, kind, stream string) string {
	for _, s := range sections(sdp) {
		if strings.HasPrefix(s, kind+" ") && strings.Contains(s, "\r\na=msid:"+stream+" ") {
			return s
		}
	}
	return ""
}

// count counts the sections of a kind with a direction.
func count(sdp, kind, direction string) int {
	n := 0
	for _, s := range sections(sdp) {
		if strings.HasPrefix(s, kind+" ") && strings.Contains(s, "\r\na="+direction) {
			n++
		}
	}
	return n
}

func (m *member) share(sound bool, bitrate int) {
	m.t.Helper()
	m.caller.Share(sound)
	if err := m.peer.Share(sound, bitrate); err != nil {
		m.t.Fatal(err)
	}
}

func (m *member) watch(sharer *member) {
	m.t.Helper()
	if err := m.peer.Watch(sharer.peer); err != nil {
		m.t.Fatal(err)
	}
}

func (m *member) sees(sharer *member) {
	m.t.Helper()
	if err := m.caller.WaitSaw(sharer.id, 20, 10*time.Second); err != nil {
		m.t.Fatal(err)
	}
}

// seenOver reports how many packets of a member's screen reach the caller
// over a while, once those already on their way have landed.
func (m *member) seenOver(sharer *member, d time.Duration) int {
	time.Sleep(300 * time.Millisecond)
	from := m.caller.Saw(sharer.id)
	time.Sleep(d)
	return m.caller.Saw(sharer.id) - from
}

// TestShareReachesItsViewers checks a share's way through the den: the
// sections it adds to the sharer's offers, which ask for its bitrate, its
// screen and sound reaching the member who watches and no one else, under
// the den's headers, and a keyframe request as the viewer's section opens.
func TestShareReachesItsViewers(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	carol := join(t, s, udpPort, tcpPort, "41", "1003", CallerOptions{UDP: true}, true)
	alice.hears(carol)

	alice.share(true, 2_000_000)
	o := alice.waitOffer("receiving a screen and its sound", func(sdp string) bool {
		return count(sdp, "video", "recvonly") == 1 && count(sdp, "audio", "recvonly") == 2
	})
	for _, want := range []string{"b=AS:2000\r\n", "b=TIAS:2000000\r\n", "VP9/90000"} {
		if !strings.Contains(o.sdp, want) {
			t.Errorf("the sharer's offer lacks %q:\n%s", strings.TrimSpace(want), o.sdp)
		}
	}

	bob.watch(alice)
	bob.sees(alice)
	if err := bob.caller.WaitHeardShare(alice.id, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if !eventually(func() bool { return alice.caller.KeyframeRequests() > 0 }) {
		t.Error("the sharer got no keyframe request as its viewer's section opened")
	}
	if n := bob.caller.Leaked(); n != 0 {
		t.Errorf("%d packets kept the sharer's header extension", n)
	}
	if n, m := carol.caller.Saw(alice.id), carol.caller.HeardShare(alice.id); n+m != 0 {
		t.Errorf("carol, who doesn't watch, got %d packets of the screen and %d of its sound", n, m)
	}

	// Bob's offer sends the screen and its sound in the sharer's stream,
	// the sound in stereo and voice at the call's bitrate.
	o = bob.waitOffer("sending the share", func(sdp string) bool { return sectionOf(sdp, "video", "screen-1001") != "" })
	if sound := sectionOf(o.sdp, "audio", "screen-1001"); !strings.Contains(sound, "a=fmtp:111 "+soundParams) {
		t.Errorf("the share's sound doesn't ask for %q:\n%s", soundParams, sound)
	}
	if voice := sectionOf(o.sdp, "audio", "1001"); !strings.Contains(voice, "a=fmtp:111 "+voiceParams(testBitrate)) {
		t.Errorf("alice's voice doesn't ask for %q:\n%s", voiceParams(testBitrate), voice)
	}
}

// TestWatchingComesAndGoes checks that a member who stops watching gets an
// offer that retires the share's sections and then nothing more of it,
// and that watching again takes the retired section back.
func TestWatchingComesAndGoes(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	bob.hears(alice)
	alice.share(false, 2_000_000)
	bob.watch(alice)
	bob.sees(alice)

	bob.peer.Unwatch(alice.id)
	bob.waitOffer("retiring the screen", func(sdp string) bool {
		return sectionOf(sdp, "video", "screen-1001") == "" && count(sdp, "video", "inactive") == 1
	})
	if n := bob.seenOver(alice, 500*time.Millisecond); n != 0 {
		t.Errorf("bob got %d packets of a share he stopped watching", n)
	}

	bob.watch(alice)
	o := bob.waitOffer("sending the screen again", func(sdp string) bool { return sectionOf(sdp, "video", "screen-1001") != "" })
	if n := strings.Count(o.sdp, "m=video"); n != 1 {
		t.Errorf("watching again took a new section: %d video sections", n)
	}
	bob.sees(alice)
}

// TestShareEnds checks that a share's viewers get offers that retire its
// sections, and nothing more of it, once its member stops sharing, and
// again once they leave; and that a share that ended can't be watched.
func TestShareEnds(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	bob.hears(alice)
	alice.share(true, 2_000_000)
	bob.watch(alice)
	bob.sees(alice)

	alice.peer.Unshare()
	bob.waitOffer("retiring the share", func(sdp string) bool {
		return sectionOf(sdp, "video", "screen-1001") == "" && sectionOf(sdp, "audio", "screen-1001") == ""
	})
	if n := bob.seenOver(alice, 500*time.Millisecond); n != 0 {
		t.Errorf("bob got %d packets of a share that ended", n)
	}
	if err := bob.peer.Watch(alice.peer); !errors.Is(err, ErrNotSharing) {
		t.Errorf("watching a share that ended gave %v", err)
	}

	alice.share(true, 2_000_000)
	bob.watch(alice)
	bob.sees(alice)
	alice.peer.Close()
	bob.waitOffer("retiring alice's voice and share", func(sdp string) bool {
		return count(sdp, "video", "sendonly") == 0 && count(sdp, "audio", "sendonly") == 0
	})
}

// TestKeyframeRequests checks that a viewer's PLIs and FIRs reach the
// sharer as PLIs, at most one every keyframeGap, and that a burst of them
// gets one at once and one more once the gap has passed.
func TestKeyframeRequests(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	bob.hears(alice)
	alice.share(false, 2_000_000)
	bob.watch(alice)
	bob.sees(alice)
	time.Sleep(2 * keyframeGap)

	before := alice.caller.KeyframeRequests()
	for range 5 {
		if err := bob.caller.RequestKeyframe(alice.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := alice.caller.WaitKeyframeRequests(2, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * keyframeGap)
	if n := alice.caller.KeyframeRequests() - before; n != 2 {
		t.Errorf("five PLIs at once reached the sharer as %d, not 2", n)
	}

	// A FIR asks the same.
	time.Sleep(keyframeGap)
	before = alice.caller.KeyframeRequests()
	var ssrc uint32
	for _, r := range bob.caller.pc.GetReceivers() {
		for _, tr := range r.Tracks() {
			if tr.Kind() == webrtc.RTPCodecTypeVideo {
				ssrc = uint32(tr.SSRC())
			}
		}
	}
	fir := &rtcp.FullIntraRequest{MediaSSRC: ssrc, FIR: []rtcp.FIREntry{{SSRC: ssrc, SequenceNumber: 1}}}
	if err := bob.caller.pc.WriteRTCP([]rtcp.Packet{fir}); err != nil {
		t.Fatal(err)
	}
	if err := alice.caller.WaitKeyframeRequests(1, 5*time.Second); err != nil {
		t.Errorf("a FIR didn't reach the sharer: %v", err)
	}
}

// TestPausedShareAsksForAKeyframe checks that a share whose packets stop,
// as a page pauses its encoder while nobody watches, gets a keyframe
// request when they start again.
func TestPausedShareAsksForAKeyframe(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	bob.hears(alice)
	alice.share(false, 2_000_000)
	bob.watch(alice)
	bob.sees(alice)

	alice.caller.StopSharing()
	time.Sleep(3 * keyframeGap)
	before := alice.caller.KeyframeRequests()
	alice.caller.Share(false)
	if err := alice.caller.WaitKeyframeRequests(1, 5*time.Second); err != nil && alice.caller.KeyframeRequests() == before {
		t.Errorf("a share that started again got no keyframe request: %v", err)
	}
	bob.sees(alice)
}

// TestShareBitrate checks that a share sending well past its bitrate is
// held to it, once the burst that lets a keyframe through is spent.
func TestShareBitrate(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	// 400 packets of 1,000 bytes a second, about 3.2 Mbps, against 250 kbps.
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true, ScreenRate: 400, ScreenBytes: 1000}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	bob.hears(alice)
	const bitrate = 250_000
	alice.share(false, bitrate)
	bob.watch(alice)
	bob.sees(alice)
	// The burst, 512 KiB, is spent within a couple of seconds.
	time.Sleep(3 * time.Second)
	const over = 2 * time.Second
	got := bob.seenOver(alice, over)
	perSecond := bitrate / 8 * shareHeadroom / 1000 // packets of 1,000 bytes
	if most := perSecond*over.Seconds() + 20; float64(got) > most {
		t.Errorf("forwarded %d packets in %s, more than %.0f", got, over, most)
	}
	if got < int(perSecond*over.Seconds()/2) {
		t.Errorf("forwarded only %d packets in %s", got, over)
	}
}

// TestStaffMuteHoldsBackTheShare checks that the den forwards nothing of a
// muted member's share.
func TestStaffMuteHoldsBackTheShare(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	bob.hears(alice)
	alice.share(true, 2_000_000)
	bob.watch(alice)
	bob.sees(alice)
	alice.peer.SetMuted(true)
	if n := bob.seenOver(alice, 500*time.Millisecond); n != 0 {
		t.Errorf("bob got %d packets of a muted member's share", n)
	}
}

// TestVoiceBitrate checks that a new bitrate reaches the member in an
// offer of its own, in every voice section.
func TestVoiceBitrate(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{})
	alice := join(t, s, udpPort, tcpPort, "41", "1001", CallerOptions{UDP: true}, true)
	bob := join(t, s, udpPort, tcpPort, "41", "1002", CallerOptions{UDP: true}, true)
	alice.hears(bob)
	alice.peer.SetVoiceBitrate(48000)
	o := alice.waitOffer("at 48 kbps", func(sdp string) bool { return strings.Contains(sdp, "maxaveragebitrate=48000") })
	if n, m := strings.Count(o.sdp, "a=fmtp:111 "+voiceParams(48000)), strings.Count(o.sdp, "m=audio"); n != m {
		t.Errorf("%d of %d sections ask for the new bitrate:\n%s", n, m, o.sdp)
	}
	alice.hears(bob)
}

// TestTwoSharesTwentyViewers checks the den at the milestone's scale: two
// members share in a call of 22, and each of the other 20 sees both.
func TestTwoSharesTwentyViewers(t *testing.T) {
	s, udpPort, tcpPort := testSFU(t, Config{AnswerTimeout: time.Minute, ConnectTimeout: time.Minute})
	// Quiet voices and small screens, so the test measures the den's
	// handling, not the machine's.
	quiet := CallerOptions{UDP: true, Rate: 5, ScreenRate: 30, ScreenBytes: 300}
	var sharers, viewers []*member
	for i := range 22 {
		m := join(t, s, udpPort, tcpPort, "41", strconv.Itoa(2000+i), quiet, true)
		if i < 2 {
			sharers = append(sharers, m)
		} else {
			viewers = append(viewers, m)
		}
	}
	for _, sharer := range sharers {
		sharer.share(false, 2_000_000)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2*len(viewers))
	for _, v := range viewers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, sharer := range sharers {
				if err := v.peer.Watch(sharer.peer); err != nil {
					errs <- err
					return
				}
			}
			for _, sharer := range sharers {
				if err := v.caller.WaitSaw(sharer.id, 10, 60*time.Second); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
