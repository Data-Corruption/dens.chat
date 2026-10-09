package denclient

import (
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A retention pass (M5) clears the unread marks of channels it emptied,
// and leaves the rest, and the den's period checks like its limits.
func TestMessagesExpired(t *testing.T) {
	s := newState()
	s.reads["1"] = denproto.ReadState{ChannelID: "1", LastMessage: "10", ReadPosition: "4", MentionCount: 2}
	s.reads["2"] = denproto.ReadState{ChannelID: "2", LastMessage: "30", ReadPosition: "4", MentionCount: 1}
	s.reads["3"] = denproto.ReadState{ChannelID: "3"}
	e, err := denproto.NewEvent(denproto.EventMessagesExpired, 1, denproto.MessagesExpired{Through: "20"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.applyEvent(e, denproto.Member{ID: "7"}); !ok || err != nil {
		t.Fatalf("applied: %t %v", ok, err)
	}
	if r := s.reads["1"]; r.LastMessage != "" || r.MentionCount != 0 || r.ReadPosition != "4" {
		t.Errorf("an emptied channel: %+v", r)
	}
	if r := s.reads["2"]; r.LastMessage != "30" || r.MentionCount != 1 {
		t.Errorf("a channel with newer messages: %+v", r)
	}
	if touched := s.takeTouched(); len(touched) != 1 || touched[0].ChannelID != "1" {
		t.Errorf("touched: %+v", touched)
	}
	bad, _ := denproto.NewEvent(denproto.EventMessagesExpired, 2, denproto.MessagesExpired{Through: "x"})
	if _, _, err := s.applyEvent(bad, denproto.Member{ID: "7"}); err != errMalformed {
		t.Errorf("a malformed event: %v", err)
	}

	den := denproto.Den{Limits: denproto.Limits{FileSize: denproto.DefaultFileSize, MemberStorage: denproto.DefaultMemberStorage,
		DenStorage: denproto.DefaultDenStorage}, CallLimits: denproto.DefaultCallLimits, Retention: 30}
	if !cleanLimits(den) {
		t.Error("a den keeping messages 30 days didn't check")
	}
	for _, days := range []int{-1, denproto.MaxRetention + 1} {
		den.Retention = days
		if cleanLimits(den) {
			t.Errorf("a retention of %d days checked", days)
		}
	}
}
