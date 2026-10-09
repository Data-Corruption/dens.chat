package denclient

import (
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestDenCallsAreChecked checks what a den says of calls and shares
// (M4.2) before it reaches the page: who shares and watches, a voice
// channel's bitrate, the den's limits and its refusals.
func TestDenCallsAreChecked(t *testing.T) {
	call := func(members ...denproto.CallMember) denproto.Call {
		return denproto.Call{ChannelID: "41", Members: members}
	}
	good := call(denproto.CallMember{ID: "7", Sharing: true, Sound: true}, denproto.CallMember{ID: "8", Watching: []string{"7"}})
	if !cleanCall(good) {
		t.Error("a call with a share and a viewer didn't check")
	}
	for name, c := range map[string]denproto.Call{
		"sound without a share": call(denproto.CallMember{ID: "7", Sound: true}),
		"watching themselves":   call(denproto.CallMember{ID: "7", Sharing: true, Watching: []string{"7"}}),
		"watching twice":        call(denproto.CallMember{ID: "8", Watching: []string{"7", "7"}}),
		"watching a bad ID":     call(denproto.CallMember{ID: "8", Watching: []string{"07"}}),
		"watching five":         call(denproto.CallMember{ID: "8", Watching: []string{"1", "2", "3", "4", "5"}}),
	} {
		if cleanCall(c) {
			t.Errorf("%s: checked", name)
		}
	}

	group := "3"
	voice := denproto.Channel{ID: "41", GroupID: &group, Name: "Lounge", Kind: denproto.KindVoice, Bitrate: 64000}
	if _, ok := cleanChannel(voice); !ok {
		t.Error("a voice channel at 64 kbps didn't check")
	}
	for name, c := range map[string]denproto.Channel{
		"voice without a bitrate": {ID: "41", Name: "Lounge", Kind: denproto.KindVoice},
		"voice at 500 kbps":       {ID: "41", Name: "Lounge", Kind: denproto.KindVoice, Bitrate: 500000},
		"text with a bitrate":     {ID: "42", Name: "general", Kind: denproto.KindText, Bitrate: 64000},
		"a DM with a bitrate":     {ID: "43", Kind: denproto.KindDM, Members: []string{"7", "8"}, Bitrate: 64000},
	} {
		if _, ok := cleanChannel(c); ok {
			t.Errorf("%s: checked", name)
		}
	}

	den := denproto.Den{Limits: denproto.Limits{FileSize: denproto.DefaultFileSize, MemberStorage: denproto.DefaultMemberStorage,
		DenStorage: denproto.DefaultDenStorage}, CallLimits: denproto.DefaultCallLimits}
	if !cleanLimits(den) {
		t.Error("a new den's limits didn't check")
	}
	den.CallLimits.ShareBitrate = 1 << 40
	if cleanLimits(den) {
		t.Error("a share bitrate past the range checked")
	}
	den.CallLimits = denproto.CallLimits{}
	if cleanLimits(den) {
		t.Error("a den with no call limits checked")
	}

	for _, x := range []denproto.VoiceRefused{
		{What: denproto.RefusedShare, Reason: denproto.RefusedFull},
		{What: denproto.RefusedWatch, MemberID: "7", Reason: denproto.RefusedNotSharing},
	} {
		if r, ok := cleanRefusal(x); !ok || r.Reason != x.Reason {
			t.Errorf("%+v became %+v, %t", x, r, ok)
		}
	}
	if r, ok := cleanRefusal(denproto.VoiceRefused{What: denproto.RefusedShare, Reason: "<b>no</b>"}); !ok || r.Reason != denproto.VoiceFailed {
		t.Errorf("an unknown reason became %q, %t", r.Reason, ok)
	}
	for _, x := range []denproto.VoiceRefused{
		{What: "dance", Reason: denproto.RefusedFull},
		{What: denproto.RefusedWatch, Reason: denproto.RefusedFull},
		{What: denproto.RefusedShare, MemberID: "7", Reason: denproto.RefusedFull},
	} {
		if _, ok := cleanRefusal(x); ok {
			t.Errorf("%+v checked", x)
		}
	}
}
