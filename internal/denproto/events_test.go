package denproto

import (
	"strings"
	"testing"
	"time"
)

func TestEventLimits(t *testing.T) {
	for _, l := range []EventLimits{DefaultEventLimits, {MemberEvents: MaxMemberEvents}} {
		if err := CheckEventLimits(l); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}
	for _, l := range []EventLimits{{MemberEvents: -1}, {MemberEvents: MaxMemberEvents + 1}} {
		if CheckEventLimits(l) == nil {
			t.Errorf("%+v passed", l)
		}
	}
	if DefaultEventLimits.MemberEvents != 0 {
		t.Error("a new den's events aren't staff's alone")
	}
}

// TestCleanEventName checks an event's name and place are cleaned as a
// poll's choice is: what can't be seen goes, emoji stay whole, and each is
// 1 to 100 characters.
func TestCleanEventName(t *testing.T) {
	if got, err := CleanEventName("  🧑‍🚀 Launch​  party\n"); err != nil || got != "🧑‍🚀 Launch party" {
		t.Errorf("CleanEventName = %q, %v", got, err)
	}
	if got, err := CleanEventPlace(" The‮ back room "); err != nil || got != "The back room" {
		t.Errorf("CleanEventPlace = %q, %v", got, err)
	}
	for _, bad := range []string{"", " ​ ", strings.Repeat("x", MaxEventNameRunes+1), strings.Repeat("é", 600), "\xff"} {
		if _, err := CleanEventName(bad); err == nil {
			t.Errorf("CleanEventName(%.20q) passed", bad)
		}
		if _, err := CleanEventPlace(bad); err == nil {
			t.Errorf("CleanEventPlace(%.20q) passed", bad)
		}
	}
	if CheckEventDescription(strings.Repeat("é", MaxEventDescriptionRunes)) != nil || CheckEventDescription("") != nil {
		t.Error("the longest description, or none, was refused")
	}
	if CheckEventDescription(strings.Repeat("x", MaxEventDescriptionRunes+1)) == nil || CheckEventDescription("\xff") == nil {
		t.Error("a description too long, or not UTF-8, passed")
	}
}

// TestCheckDenEvent checks an event as a den sends it.
func TestCheckDenEvent(t *testing.T) {
	good := DenEvent{ID: "4", Name: "Game night", Description: "Bring *snacks*", StartsAt: 1_700_000_000_000,
		EndsAt: 1_700_000_000_000 + time.Hour.Milliseconds(), ChannelID: "2", MadeBy: "1", Live: true, Interested: 3,
		Banner: Image{ID: "9", Width: 900, Height: 300}}
	if err := CheckDenEvent(good); err != nil {
		t.Fatalf("a good event: %v", err)
	}
	place := good
	place.ChannelID, place.Place = "", "The back room"
	if err := CheckDenEvent(place); err != nil {
		t.Fatalf("an event in a place written out: %v", err)
	}
	// End now leaves an event shorter than any a member makes.
	brief := good
	brief.EndsAt = brief.StartsAt + 1
	if err := CheckDenEvent(brief); err != nil {
		t.Fatalf("an event of a millisecond: %v", err)
	}
	for name, change := range map[string]func(*DenEvent){
		"no ID":             func(e *DenEvent) { e.ID = "" },
		"no maker":          func(e *DenEvent) { e.MadeBy = "x" },
		"an unclean name":   func(e *DenEvent) { e.Name = " Game night" },
		"no name":           func(e *DenEvent) { e.Name = "" },
		"a long name":       func(e *DenEvent) { e.Name = strings.Repeat("x", MaxEventNameRunes+1) },
		"a long text":       func(e *DenEvent) { e.Description = strings.Repeat("x", MaxEventDescriptionRunes+1) },
		"two places":        func(e *DenEvent) { e.Place = "here" },
		"no place":          func(e *DenEvent) { e.ChannelID = "" },
		"a bad channel":     func(e *DenEvent) { e.ChannelID = "0" },
		"an unclean place":  func(e *DenEvent) { e.ChannelID, e.Place = "", "here​" },
		"an end before":     func(e *DenEvent) { e.EndsAt = e.StartsAt },
		"no start":          func(e *DenEvent) { e.StartsAt = 0 },
		"too long":          func(e *DenEvent) { e.EndsAt = e.StartsAt + MaxEventLength.Milliseconds() + 1 },
		"a bad banner":      func(e *DenEvent) { e.Banner.Width = MaxBannerWide + 1 },
		"less than nobody":  func(e *DenEvent) { e.Interested = -1 },
		"a banner of no ID": func(e *DenEvent) { e.Banner.ID = "" },
	} {
		e := good
		change(&e)
		if CheckDenEvent(e) == nil {
			t.Errorf("%s: passed", name)
		}
	}
	if (EventInterest{EventID: "4", Interested: 2}).Check() != nil || (EventInterest{EventID: "x"}).Check() == nil ||
		(EventInterest{EventID: "4", Interested: -1}).Check() == nil {
		t.Error("an event's interest checked wrong")
	}
}
