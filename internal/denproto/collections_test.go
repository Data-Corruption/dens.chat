package denproto

import (
	"strings"
	"testing"
)

func TestCleanCollectionName(t *testing.T) {
	got, err := CleanCollectionName("  Raid Guides 🏆  ")
	if err != nil || got != "Raid Guides 🏆" {
		t.Fatalf("got %q, %v", got, err)
	}
	// Keeps joined emoji sequences whole.
	family := "Team 👨\u200d👩\u200d👧"
	if got, err := CleanCollectionName(family); err != nil || got != family {
		t.Fatalf("joined emoji: got %q, %v", got, err)
	}
	// Exactly 50 runes passes; 51 fails.
	exact := strings.Repeat("a", MaxCollectionNameRunes)
	if got, err := CleanCollectionName(exact); err != nil || got != exact {
		t.Fatalf("50 runes: got %q, %v", got, err)
	}
	// Strips control characters and bidi overrides, rejecting strings with nothing visible left.
	for in, want := range map[string]string{
		"bad\nname":   "badname",
		"bad\x00name": "badname",
		"a\u202eb":    "ab",
	} {
		if got, err := CleanCollectionName(in); err != nil || got != want {
			t.Errorf("CleanCollectionName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"",
		"   ",
		"\n\x00\u202e",
		strings.Repeat("a", MaxCollectionNameRunes+1),
		strings.Repeat("x", maxCollectionNameBytes+1),
		"\xff\xfe",
	} {
		if _, err := CleanCollectionName(bad); err == nil {
			t.Errorf("CleanCollectionName(%q) unexpectedly passed", bad)
		}
	}
}

func TestCheckCollection(t *testing.T) {
	shared := Collection{
		ID: "1", ChannelID: "10", Name: "Pinned Guides", WhoAdds: CollectionAddEveryone,
		Count: 3, CreatedBy: "2", CreatedAt: 1700000000000,
	}
	if err := CheckCollection(shared); err != nil {
		t.Fatalf("valid shared collection: %v", err)
	}
	personal := Collection{
		ID: "2", Name: "Saved", Count: 0, CreatedAt: 1700000000000,
	}
	if err := CheckCollection(personal); err != nil {
		t.Fatalf("valid personal collection: %v", err)
	}
	for name, mod := range map[string]func(*Collection){
		"bad id":            func(c *Collection) { c.ID = "0" },
		"empty name":        func(c *Collection) { c.Name = "" },
		"untrimmed name":    func(c *Collection) { c.Name = " Guides" },
		"control char name": func(c *Collection) { c.Name = "bad\nname" },
		"bidi override":     func(c *Collection) { c.Name = "a\u202eb" },
		"bad channel id":    func(c *Collection) { c.ChannelID = "abc" },
		"shared no who":     func(c *Collection) { c.WhoAdds = "" },
		"shared bad who":    func(c *Collection) { c.WhoAdds = "mods" },
		"personal with who": func(c *Collection) { c.ChannelID = ""; c.WhoAdds = CollectionAddStaff },
		"bad creator":       func(c *Collection) { c.CreatedBy = "01" },
		"negative count":    func(c *Collection) { c.Count = -1 },
		"count over cap":    func(c *Collection) { c.Count = MaxCollectionItems + 1 },
		"negative created":  func(c *Collection) { c.CreatedAt = -1 },
	} {
		c := shared
		mod(&c)
		if CheckCollection(c) == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestCheckCollectionItemAndEvent(t *testing.T) {
	msg := Message{ID: "5", ChannelID: "10", AuthorID: "2", CreatedAt: 1000, Revision: 1, Text: "hello"}
	item := CollectionItem{Message: msg, AddedBy: "2", AddedAt: 2000}
	if err := CheckCollectionItem(item); err != nil {
		t.Fatalf("valid item: %v", err)
	}
	badItem := item
	badItem.AddedBy = "0"
	if CheckCollectionItem(badItem) == nil {
		t.Error("expected error for bad added_by")
	}
	badItem = item
	badItem.AddedAt = -1
	if CheckCollectionItem(badItem) == nil {
		t.Error("expected error for negative added_at")
	}
	badItem = item
	badItem.Message.Text = ""
	if CheckCollectionItem(badItem) == nil {
		t.Error("expected error for invalid message")
	}

	ev := CollectionItemEvent{CollectionID: "1", ChannelID: "10", MessageID: "5", AddedBy: "2", AddedAt: 2000, Count: 1}
	if err := CheckCollectionItemEvent(ev); err != nil {
		t.Fatalf("valid item event: %v", err)
	}
	badEv := ev
	badEv.Count = MaxCollectionItems + 1
	if CheckCollectionItemEvent(badEv) == nil {
		t.Error("expected error for count over cap")
	}
	if !KnownCode(CodeCollectionNameTaken) {
		t.Error("CodeCollectionNameTaken must be registered in KnownCode")
	}
}
