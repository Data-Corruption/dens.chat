package denproto

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The page shows each kind of line in its own words, and reads the same
// lines.
//
//go:embed testdata/audit.json
var auditCases []byte

// Every kind the den writes has a case, and every case's line passes the
// check a client makes, one of a kind the den doesn't write yet among
// them.
func TestAuditCases(t *testing.T) {
	var file struct {
		Cases []struct {
			Line AuditLine `json:"line"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(auditCases, &file); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, c := range file.Cases {
		if err := CheckAuditLine(c.Line); err != nil {
			t.Errorf("line %+v: %v", c.Line, err)
		}
		kinds = append(kinds, c.Line.Kind)
	}
	for _, kind := range AuditKinds {
		if !slices.Contains(kinds, kind) {
			t.Errorf("no case of %s", kind)
		}
	}
	if !slices.ContainsFunc(kinds, func(k string) bool { return !slices.Contains(AuditKinds, k) }) {
		t.Error("no case of a kind the den doesn't write")
	}
}

// A line from the den is refused for a malformed ID, kind, name, change or
// emoji, so the page never draws one.
func TestCheckAuditLine(t *testing.T) {
	good := AuditLine{ID: "9", At: 1, Kind: AuditChannelUpdated, By: "1", MemberID: "2",
		AuditDetails: AuditDetails{ChannelID: "3", Channel: "general", Changes: []AuditChange{{Field: "name", From: "a", To: "b"}}}}
	if err := CheckAuditLine(good); err != nil {
		t.Fatal(err)
	}
	// An event's name and place keep each emoji whole, joiners and all.
	family := good
	family.Name = "Game night 👨\u200d👩\u200d👧"
	family.Changes = []AuditChange{{Field: "place", From: "🏳\ufe0f\u200d🌈 bar", To: "park"}}
	if err := CheckAuditLine(family); err != nil {
		t.Errorf("a line naming an event with a joined emoji: %v", err)
	}
	for what, change := range map[string]func(*AuditLine){
		"no ID":               func(l *AuditLine) { l.ID = "" },
		"no time":             func(l *AuditLine) { l.At = 0 },
		"a kind in capitals":  func(l *AuditLine) { l.Kind = "Channel.Updated" },
		"a kind with a space": func(l *AuditLine) { l.Kind = "channel updated" },
		"a kind's empty part": func(l *AuditLine) { l.Kind = "channel..updated" },
		"a long kind":         func(l *AuditLine) { l.Kind = strings.Repeat("a", 33) },
		"a member's bad ID":   func(l *AuditLine) { l.By = "01" },
		"a channel's bad ID":  func(l *AuditLine) { l.ChannelID = "x" },
		"a bidi override":     func(l *AuditLine) { l.Channel = "gen\u202eeral" },
		"a lone joiner":       func(l *AuditLine) { l.Name = "a\u200db" },
		"a long name":         func(l *AuditLine) { l.Name = strings.Repeat("n", MaxEventNameRunes+1) },
		"a field with a dot":  func(l *AuditLine) { l.Changes[0].Field = "a.b" },
		"a value's newline":   func(l *AuditLine) { l.Changes[0].To = "a\nb" },
		"too many changes":    func(l *AuditLine) { l.Changes = make([]AuditChange, maxAuditChanges+1) },
		"a negative window":   func(l *AuditLine) { l.DeleteMessages = -1 },
		"not an emoji":        func(l *AuditLine) { l.Emoji = "x" },
	} {
		l := good
		l.Changes = slices.Clone(good.Changes)
		change(&l)
		if CheckAuditLine(l) == nil {
			t.Errorf("a line with %s passed", what)
		}
	}
}
