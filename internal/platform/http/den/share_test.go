package den

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
)

func newShareCaller(t *testing.T) *sfu.TestCaller {
	t.Helper()
	c, err := sfu.NewTestCaller(sfu.CallerOptions{Loopback: true, UDP: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// callMember matches word of a call in which a member passes ok.
func callMember(channel, member, what string, ok func(denproto.CallMember) bool) (string, func(denproto.Event) bool) {
	return what, func(e denproto.Event) bool {
		var s denproto.VoiceState
		if e.T != denproto.EventVoiceState || json.Unmarshal(e.D, &s) != nil {
			return false
		}
		for _, c := range s.Calls {
			if c.ChannelID != channel {
				continue
			}
			for _, m := range c.Members {
				if m.ID == member {
					return ok(m)
				}
			}
		}
		return false
	}
}

// sharing matches word that a member shares, with sound or not, or that
// they don't.
func sharing(channel, member string, on, sound bool) (string, func(denproto.Event) bool) {
	return callMember(channel, member, "member "+member+" sharing: "+map[bool]string{true: "yes", false: "no"}[on],
		func(m denproto.CallMember) bool { return m.Sharing == on && m.Sound == (on && sound) })
}

// watching matches word that a member watches exactly these shares.
func watching(channel, member string, sharers ...string) (string, func(denproto.Event) bool) {
	return callMember(channel, member, "member "+member+" watching "+strings.Join(sharers, ", "),
		func(m denproto.CallMember) bool { return slices.Equal(m.Watching, sharers) })
}

// matcher is what a matching function returns, held as one value.
type matcher struct {
	what string
	ok   func(denproto.Event) bool
}

func m(what string, ok func(denproto.Event) bool) matcher { return matcher{what, ok} }

// all matches an event every matcher matches, as one voice.state can
// carry several changes together.
func all(ms ...matcher) (string, func(denproto.Event) bool) {
	var what []string
	for _, x := range ms {
		what = append(what, x.what)
	}
	return strings.Join(what, " and "), func(e denproto.Event) bool {
		for _, x := range ms {
			if !x.ok(e) {
				return false
			}
		}
		return true
	}
}

// refused matches the den refusing a share or a watch, for a reason.
func refused(channel, what, reason string) (string, func(denproto.Event) bool) {
	return what + " refused: " + reason, func(e denproto.Event) bool {
		var r denproto.VoiceRefused
		return e.T == denproto.EventVoiceRefused && json.Unmarshal(e.D, &r) == nil &&
			r.ChannelID == channel && r.What == what && r.Reason == reason
	}
}

func (v *voiceSocket) share(on, sound bool) {
	if v.caller != nil && on {
		v.caller.Share(sound)
	}
	v.send(denproto.EventVoiceShare, denproto.VoiceShare{On: on, Sound: sound})
}

func (v *voiceSocket) watch(member string, on bool) {
	v.send(denproto.EventVoiceWatch, denproto.VoiceWatch{MemberID: member, On: on})
}

// TestScreenShare checks who may share and watch (M4.2): a member of the
// call shares, with sound, and another watches and receives it; the den's
// limits refuse a second share and a viewer past the cap, and nobody
// watches a member who isn't sharing, or themselves. Stopping, and
// leaving, end a share and its viewers' watching.
func TestScreenShare(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	carolToken := f.member(aliceToken, "carol")
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	aliceCaller, bobCaller, carolCaller := newShareCaller(t), newShareCaller(t), newShareCaller(t)
	alice := f.voiceSocket(aliceToken, aliceCaller)
	bob := f.voiceSocket(bobToken, bobCaller)
	carol := f.voiceSocket(carolToken, carolCaller)
	for i, v := range []*voiceSocket{alice, bob, carol} {
		v.join(lounge.ID)
		alice.wait(inCall(lounge.ID, []string{alice.me, bob.me, carol.me}[:i+1]...))
	}

	bob.share(true, true)
	alice.wait(sharing(lounge.ID, bob.me, true, true))
	alice.watch(bob.me, true)
	carol.wait(watching(lounge.ID, alice.me, bob.me))
	if err := aliceCaller.WaitSaw(bob.me, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := aliceCaller.WaitHeardShare(bob.me, 20, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	// A new den lets one member share at a time.
	carol.share(true, false)
	carol.wait(refused(lounge.ID, denproto.RefusedShare, denproto.RefusedFull))
	// Nobody watches a member who isn't sharing, or themselves.
	carol.watch(alice.me, true)
	carol.wait(refused(lounge.ID, denproto.RefusedWatch, denproto.RefusedNotSharing))
	bob.watch(bob.me, true)
	bob.wait(refused(lounge.ID, denproto.RefusedWatch, denproto.RefusedNotSharing))
	// A share holds as many viewers as the owner allows.
	limits := denproto.DefaultCallLimits
	limits.ShareViewers = 1
	if _, err := f.d.Update(context.Background(), owner, denproto.DenUpdateRequest{CallLimits: &limits}); err != nil {
		t.Fatal(err)
	}
	carol.watch(bob.me, true)
	carol.wait(refused(lounge.ID, denproto.RefusedWatch, denproto.RefusedFull))

	// Stopping watching, and then the share, clear the marks.
	alice.watch(bob.me, false)
	carol.wait(watching(lounge.ID, alice.me))
	alice.watch(bob.me, true)
	carol.wait(watching(lounge.ID, alice.me, bob.me))
	bob.share(false, false)
	carol.wait(all(m(sharing(lounge.ID, bob.me, false, false)), m(watching(lounge.ID, alice.me))))

	// Leaving the call ends a share, and with it its viewers' watching.
	carol.share(true, false)
	alice.wait(sharing(lounge.ID, carol.me, true, false))
	alice.watch(carol.me, true)
	bob.wait(watching(lounge.ID, alice.me, carol.me))
	carol.send(denproto.EventVoiceLeave, struct{}{})
	bob.wait(all(m(inCall(lounge.ID, alice.me, bob.me)), m(watching(lounge.ID, alice.me))))

	// A den that allows no shares refuses them.
	limits.Shares = 0
	if _, err := f.d.Update(context.Background(), owner, denproto.DenUpdateRequest{CallLimits: &limits}); err != nil {
		t.Fatal(err)
	}
	bob.share(true, false)
	bob.wait(refused(lounge.ID, denproto.RefusedShare, denproto.RefusedOff))
}

// TestStaffMuteEndsAShare checks that a staff mute ends the member's share,
// and refuses one while it lasts.
func TestStaffMuteEndsAShare(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	alice := f.voiceSocket(aliceToken, newShareCaller(t))
	bob := f.voiceSocket(bobToken, newShareCaller(t))
	alice.join(lounge.ID)
	bob.wait(inCall(lounge.ID, alice.me))
	bob.join(lounge.ID)
	alice.wait(inCall(lounge.ID, alice.me, bob.me))
	bob.share(true, false)
	alice.wait(sharing(lounge.ID, bob.me, true, false))
	alice.watch(bob.me, true)
	bob.wait(watching(lounge.ID, alice.me, bob.me))

	if err := f.d.SetStaffMute(context.Background(), owner, bob.me, true); err != nil {
		t.Fatal(err)
	}
	alice.wait(all(m(sharing(lounge.ID, bob.me, false, false)), m(watching(lounge.ID, alice.me))))
	bob.share(true, false)
	bob.wait(refused(lounge.ID, denproto.RefusedShare, denproto.RefusedStaffMuted))
}

// TestChannelBitrate checks a voice channel's bitrate (M4.2): a new one's,
// one set at creation, the values staff may set, and a change reaching
// the call in progress in its next offer.
func TestChannelBitrate(t *testing.T) {
	f := newFixture(t)
	f.startCalls(sfu.Config{})
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	ctx := context.Background()
	lounge := f.channel(owner, "Lounge", denproto.KindVoice, false)
	if lounge.Bitrate != denproto.DefaultVoiceBitrate {
		t.Errorf("a new voice channel's bitrate is %d", lounge.Bitrate)
	}
	name, low := "Quiet", 32000
	quiet, err := f.d.CreateChannel(ctx, owner, denproto.ChannelRequest{Name: &name, Kind: denproto.KindVoice, Bitrate: &low})
	if err != nil || quiet.Bitrate != low {
		t.Fatalf("a channel made at 32 kbps: %+v, %v", quiet, err)
	}
	general := f.channel(owner, "general", denproto.KindText, false)
	if general.Bitrate != 0 {
		t.Errorf("a text channel has a bitrate of %d", general.Bitrate)
	}
	for _, b := range []int{8000, 15000, 136000, 60000} {
		if _, err := f.d.UpdateChannel(ctx, owner, lounge.ID, denproto.ChannelRequest{Bitrate: &b}); err == nil {
			t.Errorf("a voice channel took a bitrate of %d", b)
		}
	}
	if _, err := f.d.UpdateChannel(ctx, owner, general.ID, denproto.ChannelRequest{Bitrate: &low}); err == nil {
		t.Error("a text channel took a bitrate")
	}

	alice := f.voiceSocket(aliceToken, newShareCaller(t))
	alice.join(lounge.ID)
	alice.wait(offerWith("maxaveragebitrate=96000"))
	lower := 48000
	c, err := f.d.UpdateChannel(ctx, owner, lounge.ID, denproto.ChannelRequest{Bitrate: &lower})
	if err != nil || c.Bitrate != lower {
		t.Fatalf("lowering the bitrate: %+v, %v", c, err)
	}
	alice.wait(offerWith("maxaveragebitrate=48000"))
}

// offerWith matches an offer whose description holds a text.
func offerWith(text string) (string, func(denproto.Event) bool) {
	return "an offer with " + text, func(e denproto.Event) bool {
		var o denproto.VoiceOffer
		return e.T == denproto.EventVoiceOffer && json.Unmarshal(e.D, &o) == nil && strings.Contains(o.SDP, text)
	}
}

// TestCallLimitsAreTheOwners checks that only the owner sets the limits,
// within their ranges, and that members hear of them.
func TestCallLimitsAreTheOwners(t *testing.T) {
	f := newFixture(t)
	aliceToken, _ := f.owner()
	owner := f.session(aliceToken)
	bobToken := f.member(aliceToken, "bob")
	bob := f.voiceSocket(bobToken, nil)
	ctx := context.Background()
	limits := denproto.DefaultCallLimits
	if _, err := f.d.Update(ctx, f.session(bobToken), denproto.DenUpdateRequest{CallLimits: &limits}); err == nil {
		t.Error("a member set the den's limits")
	}
	for _, bad := range []func(*denproto.CallLimits){
		func(l *denproto.CallLimits) { l.Members = 31 },
		func(l *denproto.CallLimits) { l.Callers = 1 },
		func(l *denproto.CallLimits) { l.Shares = 11 },
		func(l *denproto.CallLimits) { l.ShareViewers = 0 },
		func(l *denproto.CallLimits) { l.ShareBitrate = 50_000_001 },
		func(l *denproto.CallLimits) { l.ShareHeight = 1000 },
		func(l *denproto.CallLimits) { l.ShareFPS = 61 },
	} {
		l := denproto.DefaultCallLimits
		bad(&l)
		if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{CallLimits: &l}); err == nil {
			t.Errorf("the den took limits %+v", l)
		}
	}
	limits.ShareBitrate, limits.ShareHeight, limits.ShareFPS = 50_000_000, 2160, 60
	if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{CallLimits: &limits}); err != nil {
		t.Fatal(err)
	}
	bob.wait("the den's new limits", func(e denproto.Event) bool {
		var d denproto.Den
		return e.T == denproto.EventDenUpdated && json.Unmarshal(e.D, &d) == nil && d.CallLimits == limits
	})
}
