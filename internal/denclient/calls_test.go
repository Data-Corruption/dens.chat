package denclient

import (
	"strconv"
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

	// A den older than the soundboard sends none of its limits (M14.3).
	den.CallLimits = denproto.DefaultCallLimits
	for name, l := range map[string]denproto.SoundLimits{"none": {}, "a new den's": denproto.DefaultSoundLimits} {
		den.SoundLimits = l
		if !cleanLimits(den) {
			t.Errorf("%s soundboard limits didn't check", name)
		}
	}
	den.SoundLimits = denproto.SoundLimits{Sounds: 1}
	if cleanLimits(den) {
		t.Error("soundboard limits out of range checked")
	}

	for _, x := range []denproto.VoiceRefused{
		{What: denproto.RefusedShare, Reason: denproto.RefusedFull},
		{What: denproto.RefusedWatch, MemberID: "7", Reason: denproto.RefusedNotSharing},
		{What: denproto.RefusedSound, SoundID: "9", Reason: denproto.RefusedPlaying},
		{What: denproto.RefusedSound, SoundID: "9", Reason: denproto.RefusedWait},
		{What: denproto.RefusedSound, SoundID: "9", Reason: denproto.RefusedOff},
		{What: denproto.RefusedSound, SoundID: "9", Reason: denproto.VoiceNotFound},
	} {
		if r, ok := cleanRefusal(x); !ok || r.Reason != x.Reason || r.SoundID != x.SoundID {
			t.Errorf("%+v became %+v, %t", x, r, ok)
		}
	}
	// A sound's reasons are a sound's alone.
	if r, ok := cleanRefusal(denproto.VoiceRefused{What: denproto.RefusedShare, Reason: denproto.RefusedWait}); !ok || r.Reason != denproto.VoiceFailed {
		t.Errorf("a share refused for a sound's reason became %q, %t", r.Reason, ok)
	}
	if r, ok := cleanRefusal(denproto.VoiceRefused{What: denproto.RefusedShare, Reason: "<b>no</b>"}); !ok || r.Reason != denproto.VoiceFailed {
		t.Errorf("an unknown reason became %q, %t", r.Reason, ok)
	}
	for _, x := range []denproto.VoiceRefused{
		{What: "dance", Reason: denproto.RefusedFull},
		{What: denproto.RefusedWatch, Reason: denproto.RefusedFull},
		{What: denproto.RefusedShare, MemberID: "7", Reason: denproto.RefusedFull},
		{What: denproto.RefusedSound, Reason: denproto.RefusedPlaying},
		{What: denproto.RefusedSound, SoundID: "9", MemberID: "7", Reason: denproto.RefusedPlaying},
	} {
		if _, ok := cleanRefusal(x); ok {
			t.Errorf("%+v checked", x)
		}
	}
}

// TestDenSoundsAreChecked checks a den's sounds before they're kept or
// reach the page (M14.3): each checks out, once, and no more than a den
// keeps.
func TestDenSoundsAreChecked(t *testing.T) {
	sound := func(id, name string) denproto.Sound {
		return denproto.Sound{ID: id, Name: name, FileID: "5", Duration: 1000, AddedBy: "1", AddedAt: 1}
	}
	if got, ok := cleanSounds([]denproto.Sound{sound("1", "Horn"), sound("2", "Bell")}); !ok || len(got) != 2 {
		t.Errorf("two sounds came out as %v, %t", got, ok)
	}
	many := make([]denproto.Sound, denproto.MaxSounds+1)
	for i := range many {
		many[i] = sound(strconv.Itoa(i+1), "Sound "+strconv.Itoa(i))
	}
	for name, list := range map[string][]denproto.Sound{
		"one twice":             {sound("1", "Horn"), sound("1", "Horn")},
		"a bad name":            {sound("1", " Horn")},
		"a bad ID":              {sound("01", "Horn")},
		"more than a den keeps": many,
	} {
		if _, ok := cleanSounds(list); ok {
			t.Errorf("%s: checked", name)
		}
	}
	s := newState()
	s.limits = denproto.Limits{FileSize: denproto.DefaultFileSize, MemberStorage: denproto.DefaultMemberStorage, DenStorage: denproto.DefaultDenStorage}
	s.callLimits, s.soundLimits = denproto.DefaultCallLimits, denproto.DefaultSoundLimits
	for _, e := range []struct {
		t    string
		data any
		ok   bool
	}{
		{denproto.EventSoundCreated, sound("1", "Horn"), true},
		{denproto.EventSoundUpdated, sound("1", "Big horn"), true},
		{denproto.EventSoundCreated, sound("2", ""), false},
		{denproto.EventSoundDeleted, denproto.SoundDeleted{ID: "x"}, false},
		{denproto.EventSoundCreated, sound("3", "Bell"), true},
		{denproto.EventSoundDeleted, denproto.SoundDeleted{ID: "1"}, true},
	} {
		ev, _ := denproto.NewEvent(e.t, 1, e.data)
		if _, _, err := s.applyEvent(ev, denproto.Member{ID: "1"}); (err == nil) != e.ok {
			t.Errorf("%s %+v: %v", e.t, e.data, err)
		}
	}
	if v := s.view(Status{}, denproto.Member{}); len(v.Sounds) != 1 || v.Sounds[0].Name != "Bell" {
		t.Errorf("the view's sounds are %+v", v.Sounds)
	}
	k, _ := s.kept()
	if back, err := restore(k, nil); err != nil || len(back.sounds) != 1 || back.soundLimits != denproto.DefaultSoundLimits {
		t.Errorf("the kept state came back as %v, %v", back, err)
	}
}
