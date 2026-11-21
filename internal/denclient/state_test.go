package denclient

import (
	"strconv"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A retention pass clears the unread marks of channels it emptied,
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
	if _, ok, err := s.applyEvent(nil, e, denproto.Member{ID: "7"}, time.Now()); !ok || err != nil {
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
	if _, _, err := s.applyEvent(nil, bad, denproto.Member{ID: "7"}, time.Now()); err != errMalformed {
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

// A deleted DM message lists every sealed upload on it, a preview beside
// each file, which is twice as many as a channel's message holds; a client
// refusing that would miss the delete, and drop its connection over it.
func TestDeletedDMFiles(t *testing.T) {
	s := newState()
	files := make([]string, denproto.MaxDMFiles)
	for i := range files {
		files[i] = strconv.Itoa(100 + i)
	}
	e, err := denproto.NewEvent(denproto.EventMessageDeleted, 1, denproto.MessageDeleted{ID: "50", ChannelID: "9", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.applyEvent(nil, e, denproto.Member{ID: "7"}, time.Now()); !ok || err != nil {
		t.Fatalf("a DM message's delete: %t %v", ok, err)
	}
	if len(s.gone) != denproto.MaxDMFiles {
		t.Fatalf("%d files gone, want %d", len(s.gone), denproto.MaxDMFiles)
	}
	e, _ = denproto.NewEvent(denproto.EventMessageDeleted, 2, denproto.MessageDeleted{ID: "51", ChannelID: "9", Files: append(files, "999")})
	if _, _, err := s.applyEvent(nil, e, denproto.Member{ID: "7"}, time.Now()); err != errMalformed {
		t.Fatalf("a delete listing more files than a message holds: %v", err)
	}
}

// A channel's ID keeps its kind for good, so a den that names a DM a text
// channel breaks the protocol, whether it relabels it, in an event or a
// later snapshot, or deletes it and makes a channel of its ID, and across
// the cache: taken, the member's next message there would go unsealed.
func TestAChannelKeepsItsKind(t *testing.T) {
	dm := denproto.Channel{ID: "9", Kind: denproto.KindDM, Members: []string{"7", "8"}}
	text := denproto.Channel{ID: "9", Kind: denproto.KindText, Name: "general"}
	me := denproto.Member{ID: "7"}
	event := func(t *testing.T, s *denState, seq uint64, kind string, data any) error {
		t.Helper()
		e, err := denproto.NewEvent(kind, seq, data)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = s.applyEvent(nil, e, me, time.Now())
		return err
	}
	ready := denproto.Ready{Me: me, Den: denproto.Den{Limits: denproto.Limits{FileSize: denproto.DefaultFileSize,
		MemberStorage: denproto.DefaultMemberStorage, DenStorage: denproto.DefaultDenStorage}, CallLimits: denproto.DefaultCallLimits},
		Sync: denproto.Sync{Epoch: denproto.Random(denproto.SyncEpochSize)}, SealCheck: denproto.Random(denproto.SealCheckSize)}
	s := newState()
	if err := s.load(nil, ready, nil, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := event(t, s, 1, denproto.EventChannelCreated, dm); err != nil {
		t.Fatal(err)
	}
	if err := event(t, s, 2, denproto.EventChannelUpdated, text); err != errMalformed {
		t.Errorf("a DM updated into a text channel: %v", err)
	}
	if s.channels[dm.ID].Kind != denproto.KindDM {
		t.Errorf("the DM is now a %s channel", s.channels[dm.ID].Kind)
	}

	ready.Channels = []denproto.Channel{text}
	if err := newState().load(nil, ready, nil, s.kinds, nil, time.Now()); err != errMalformed {
		t.Errorf("a snapshot with the DM as a text channel: %v", err)
	}
	// A snapshot without the DM leaves its ID a DM's.
	ready.Channels = nil
	next := newState()
	if err := next.load(nil, ready, nil, s.kinds, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := event(t, next, 3, denproto.EventChannelCreated, text); err != errMalformed {
		t.Errorf("a text channel of a DM's ID, after a snapshot without the DM: %v", err)
	}

	// Deleted, its ID is still a DM's, here and once the cache has kept
	// the state.
	if err := event(t, s, 3, denproto.EventChannelDeleted, denproto.ChannelDeleted{ID: dm.ID}); err != nil {
		t.Fatal(err)
	}
	if err := event(t, s, 4, denproto.EventChannelCreated, text); err != errMalformed {
		t.Errorf("a text channel of a deleted DM's ID: %v", err)
	}
	k, members := s.kept()
	back, err := restore(nil, k, members)
	if err != nil {
		t.Fatal(err)
	}
	if err := event(t, back, 5, denproto.EventChannelCreated, text); err != errMalformed {
		t.Errorf("a text channel of a deleted DM's ID, after the cache: %v", err)
	}
	if err := event(t, back, 6, denproto.EventChannelCreated, dm); err != nil {
		t.Errorf("the DM again: %v", err)
	}

	// A cache from before kinds were kept holds its channels' all the same.
	k.Kinds, k.Channels = nil, []denproto.Channel{dm}
	if back, err = restore(nil, k, members); err != nil {
		t.Fatal(err)
	}
	if err := event(t, back, 7, denproto.EventChannelUpdated, text); err != errMalformed {
		t.Errorf("a DM from an older cache updated into a text channel: %v", err)
	}
}

// A den hands a member only their own notes, and one: another's, or a
// second, breaks the protocol.
func TestOwnNotesAlone(t *testing.T) {
	for what, channels := range map[string][]denproto.Channel{
		"another member's notes": {{ID: "9", Kind: denproto.KindNotes, Members: []string{"8"}}},
		"a second notes channel": {{ID: "9", Kind: denproto.KindNotes, Members: []string{"7"}}, {ID: "10", Kind: denproto.KindNotes, Members: []string{"7"}}},
	} {
		s := newState()
		var err error
		for i, c := range channels {
			e, _ := denproto.NewEvent(denproto.EventChannelCreated, uint64(i+1), c)
			if _, _, err = s.applyEvent(nil, e, denproto.Member{ID: "7"}, time.Now()); err != nil {
				break
			}
		}
		if err != errMalformed {
			t.Errorf("%s: %v", what, err)
		}
	}
}
