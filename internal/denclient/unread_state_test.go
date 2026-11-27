package denclient

import (
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func strPtr(s string) *string { return &s }

// A delete carrying last_message_id updates the channel's read state and
// clears unread marks live, ignores stale out-of-order deletes, stays
// compatible with older dens that omit it, and rejects malformed values.
func TestMessageDeletedUpdatesReadState(t *testing.T) {
	me := denproto.Member{ID: "7"}
	apply := func(t *testing.T, s *denState, seq uint64, d denproto.MessageDeleted) error {
		t.Helper()
		e, err := denproto.NewEvent(denproto.EventMessageDeleted, seq, d)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = s.applyEvent(nil, e, me, time.Now())
		return err
	}

	// Deleting the newest message clears unread on an unopened channel, both
	// when reverting to an already-read message and when emptying the channel.
	s := newState()
	s.channels["1"] = denproto.Channel{ID: "1", Kind: denproto.KindText, Name: "general"}
	s.channels["2"] = denproto.Channel{ID: "2", Kind: denproto.KindText, Name: "empty"}
	s.reads["1"] = denproto.ReadState{ChannelID: "1", LastMessage: "11", ReadPosition: "10"}
	s.reads["2"] = denproto.ReadState{ChannelID: "2", LastMessage: "1"}
	if m := s.marks(); m.unread != 2 {
		t.Fatalf("before deletes: %+v", m)
	}
	if err := apply(t, s, 1, denproto.MessageDeleted{ID: "11", ChannelID: "1", LastMessage: strPtr("10")}); err != nil {
		t.Fatal(err)
	}
	if r := s.reads["1"]; r.LastMessage != "10" || r.ReadPosition != "10" {
		t.Fatalf("channel 1 after delete: %+v", r)
	}
	if err := apply(t, s, 2, denproto.MessageDeleted{ID: "1", ChannelID: "2", LastMessage: strPtr("")}); err != nil {
		t.Fatal(err)
	}
	if r := s.reads["2"]; r.LastMessage != "" || r.ReadPosition != "" {
		t.Fatalf("channel 2 after delete: %+v", r)
	}
	if m := s.marks(); m.unread != 0 {
		t.Fatalf("after deletes: %+v", m)
	}
	if touched := s.takeTouched(); len(touched) != 2 || touched[0].ChannelID != "1" || touched[1].ChannelID != "2" {
		t.Fatalf("touched: %+v", touched)
	}

	// Deleting a non-newest message keeps unread intact without touching;
	// deleting the newest while another unread message remains updates
	// LastMessage and keeps unread intact.
	s.reads["1"] = denproto.ReadState{ChannelID: "1", LastMessage: "12", ReadPosition: "10"}
	if err := apply(t, s, 3, denproto.MessageDeleted{ID: "11", ChannelID: "1", LastMessage: strPtr("12")}); err != nil {
		t.Fatal(err)
	}
	if r := s.reads["1"]; r.LastMessage != "12" || len(s.takeTouched()) != 0 {
		t.Fatalf("non-newest delete changed state: %+v", r)
	}
	if err := apply(t, s, 4, denproto.MessageDeleted{ID: "12", ChannelID: "1", LastMessage: strPtr("11")}); err != nil {
		t.Fatal(err)
	}
	if r := s.reads["1"]; r.LastMessage != "11" || s.marks().unread != 1 || len(s.takeTouched()) != 1 {
		t.Fatalf("newest delete leaving unread 11: %+v %+v", r, s.marks())
	}

	// Out-of-order delete of an older message cannot regress LastMessage past
	// a newer message already received.
	s.reads["1"] = denproto.ReadState{ChannelID: "1", LastMessage: "15", ReadPosition: "10"}
	if err := apply(t, s, 5, denproto.MessageDeleted{ID: "14", ChannelID: "1", LastMessage: strPtr("13")}); err != nil {
		t.Fatal(err)
	}
	if r := s.reads["1"]; r.LastMessage != "15" || len(s.takeTouched()) != 0 {
		t.Fatalf("out-of-order delete regressed LastMessage: %+v", r)
	}

	// Older den omitting LastMessage leaves reads untouched and succeeds.
	if err := apply(t, s, 6, denproto.MessageDeleted{ID: "15", ChannelID: "1"}); err != nil {
		t.Fatalf("older den delete: %v", err)
	}
	if r := s.reads["1"]; r.LastMessage != "15" || len(s.takeTouched()) != 0 {
		t.Fatalf("older den delete modified reads: %+v", r)
	}

	// Hostile den sending a malformed LastMessage fails closed.
	for _, bad := range []string{"0", "01", "-1", "abc", "99999999999999999999"} {
		if err := apply(t, s, 7, denproto.MessageDeleted{ID: "15", ChannelID: "1", LastMessage: strPtr(bad)}); err != errMalformed {
			t.Errorf("malformed LastMessage %q: got %v, want errMalformed", bad, err)
		}
	}
	if r := s.reads["1"]; r.LastMessage != "15" {
		t.Fatalf("malformed events changed reads: %+v", r)
	}
}
