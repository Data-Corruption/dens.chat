package denproto

import (
	"strings"
	"testing"
)

func TestSoundLimits(t *testing.T) {
	if err := CheckSoundLimits(DefaultSoundLimits); err != nil {
		t.Fatalf("a new den's limits: %v", err)
	}
	for name, l := range map[string]SoundLimits{
		"off, with no wait": {Sounds: 0, Longest: 1, Wait: 0},
		"at the top":        {Sounds: MaxSounds, Longest: MaxSoundLongest, Wait: MaxSoundWait},
	} {
		if err := CheckSoundLimits(l); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, change := range map[string]func(*SoundLimits){
		"fewer than none":  func(l *SoundLimits) { l.Sounds = -1 },
		"too many":         func(l *SoundLimits) { l.Sounds = MaxSounds + 1 },
		"no length":        func(l *SoundLimits) { l.Longest = 0 },
		"too long":         func(l *SoundLimits) { l.Longest = MaxSoundLongest + 1 },
		"a wait backwards": func(l *SoundLimits) { l.Wait = -1 },
		"too long a wait":  func(l *SoundLimits) { l.Wait = MaxSoundWait + 1 },
		"none at all":      func(l *SoundLimits) { *l = SoundLimits{} },
	} {
		l := DefaultSoundLimits
		change(&l)
		if CheckSoundLimits(l) == nil {
			t.Errorf("%s: passed", name)
		}
	}
}

// A sound's size follows its length: 64 kbps, a quarter more, and its
// headers' room.
func TestMaxSoundSize(t *testing.T) {
	for ms, want := range map[int64]int64{0: 4096, 1000: 4096 + 10_000, 10_000: 4096 + 100_000, 300_000: 4096 + 3_000_000} {
		if got := MaxSoundSize(ms); got != want {
			t.Errorf("a sound of %d ms may be %d bytes, not %d", ms, got, want)
		}
	}
}

func TestCheckSound(t *testing.T) {
	good := Sound{ID: "7", Name: "Air horn", FileID: "12", Duration: 2400, AddedBy: "3", AddedAt: 1}
	if err := CheckSound(good); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Sound){
		"no ID":              func(s *Sound) { s.ID = "" },
		"no file":            func(s *Sound) { s.FileID = "x" },
		"nobody added it":    func(s *Sound) { s.AddedBy = "" },
		"no name":            func(s *Sound) { s.Name = "" },
		"a name too long":    func(s *Sound) { s.Name = strings.Repeat("a", MaxNameRunes+1) },
		"a name not cleaned": func(s *Sound) { s.Name = " Air horn" },
		"a hidden mark":      func(s *Sound) { s.Name = "Air‮horn" },
		"no length":          func(s *Sound) { s.Duration = 0 },
		"no time":            func(s *Sound) { s.AddedAt = 0 },
	} {
		s := good
		change(&s)
		if CheckSound(s) == nil {
			t.Errorf("%s: passed", name)
		}
	}
}
