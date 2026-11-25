package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// olderGroups is what a v1.15.0 den sent carol, a member of a group DM, 3,
// beside a text channel, 1, and her DM with bob, 2: her snapshot, the
// events as the group was made, written in and reacted to, and the change
// log; and the state her v1.15.0 Dens kept in its cache on another den,
// where the group is 2.
type olderGroups struct {
	Ready   denproto.Ready   `json:"ready"`
	Events  []denproto.Event `json:"events"`
	Changes denproto.Changes `json:"changes"`
	Kept    keptState        `json:"kept"`
}

func readOlderGroups(t *testing.T) olderGroups {
	t.Helper()
	b, err := os.ReadFile("testdata/v1.15.0-groups.json")
	if err != nil {
		t.Fatal(err)
	}
	var g olderGroups
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// An older den's group DM, in its snapshot and its events, is dropped
// unread, with its keys and read state, and the rest of the den reads on;
// its ID keeps its kind, so the den can't hand it out again as a channel.
func TestAnOlderDensGroupDropped(t *testing.T) {
	g := readOlderGroups(t)
	denID := g.Ready.Den.ID
	me := g.Ready.Me
	s := newState()
	if err := s.load(denID, g.Ready, nil, nil, nil, time.Now()); err != nil {
		t.Fatalf("a v1.15.0 den's snapshot with a group DM: %v", err)
	}
	if _, ok := s.channels["3"]; ok || s.channels["1"].Kind != denproto.KindText || s.channels["2"].Kind != denproto.KindDM {
		t.Errorf("the snapshot's channels: %+v", s.channels)
	}
	if _, ok := s.reads["3"]; ok {
		t.Error("the group's read state stayed")
	}
	if len(s.signed) != 1 || s.signed["1"].ChannelID != "2" {
		t.Errorf("the signed keys: %+v", s.signed)
	}
	if s.kinds["3"] != denproto.KindGroupDM {
		t.Errorf("the group's ID is kept as %q", s.kinds["3"])
	}

	// And what the capture lacks: the group's time set, an edit, a read,
	// typing, a delete and the group gone.
	var events []denproto.Event
	for _, e := range g.Events {
		events = append(events, e)
		switch e.T {
		case denproto.EventChannelCreated:
			events = append(events, denproto.Event{T: denproto.EventChannelUpdated, Seq: 30, D: e.D})
		case denproto.EventMessageCreated:
			events = append(events, denproto.Event{T: denproto.EventMessageUpdated, Seq: 31, D: e.D})
		}
	}
	read, _ := denproto.NewEvent(denproto.EventReadStateUpdated, 32, denproto.ReadState{ChannelID: "3", ReadPosition: "3"})
	typing, _ := denproto.NewEvent(denproto.EventTyping, 33, denproto.Typing{ChannelID: "3", MemberID: "2"})
	deleted, _ := denproto.NewEvent(denproto.EventMessageDeleted, 34, denproto.MessageDeleted{ID: "3", ChannelID: "3"})
	gone, _ := denproto.NewEvent(denproto.EventChannelDeleted, 35, denproto.ChannelDeleted{ID: "3"})
	for _, e := range append(events, read, typing, deleted, gone) {
		if _, ok, err := s.applyEvent(denID, e, me, time.Now()); ok || err != nil {
			t.Errorf("a v1.15.0 den's %s about the group: passed %t, %v", e.T, ok, err)
		}
	}
	if _, ok := s.channels["3"]; ok {
		t.Error("the group came back with its events")
	}
	if len(s.signed) != 1 {
		t.Errorf("the signed keys after the events: %+v", s.signed)
	}
	if !s.dropped(g.Changes.Messages[0].ChannelID) {
		t.Error("the group's message in the change log is kept")
	}

	// Its ID taken for a channel is the den breaking the protocol.
	text, _ := denproto.NewEvent(denproto.EventChannelCreated, 23, denproto.Channel{ID: "3", Kind: denproto.KindText, Name: "general"})
	if _, _, err := s.applyEvent(denID, text, me, time.Now()); err != errMalformed {
		t.Errorf("a text channel of a group's ID: %v", err)
	}
	g.Ready.Channels[2] = denproto.Channel{ID: "3", Kind: denproto.KindText, Name: "general"}
	if err := newState().load(denID, g.Ready, nil, s.kinds, nil, time.Now()); err != errMalformed {
		t.Errorf("a snapshot with the group as a text channel: %v", err)
	}
}

// An older den's group message, as its event reaches the connection, goes
// nowhere: not to the page, nor into the cache.
func TestAnOlderDensGroupMessageGoesNowhere(t *testing.T) {
	g := readOlderGroups(t)
	m := testManager(t)
	m.Now = time.Now
	v, err := vault.New(denproto.Random(vault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache"), v, m.log, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m.Cache = store
	c := &conn{m: m, j: &joined{denID: g.Ready.Den.ID}, den: newState(), ids: newKept(), verdicts: map[string]string{},
		copies: map[string]denproto.Bytes{}, dmFiles: map[string]dmFile{}}
	c.profile.Member = g.Ready.Me
	if err := c.den.load(g.Ready.Den.ID, g.Ready, nil, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Events {
		if e.T != denproto.EventMessageCreated {
			continue
		}
		for _, t2 := range []string{denproto.EventMessageCreated, denproto.EventMessageUpdated} {
			e.T = t2
			if _, ok, err := c.apply(context.Background(), e); ok || err != nil {
				t.Errorf("the group's %s reached the page: %t, %v", t2, ok, err)
			}
		}
	}
	if newest, err := store.Newest(g.Ready.Den.ID); err != nil || newest != 0 {
		t.Errorf("the cache holds message %d, %v", newest, err)
	}
}

// Nothing goes to an ID kept as a group's, sent or forwarded, unsealed
// or sealed: there's no key to send under.
func TestNothingGoesToAGroupsID(t *testing.T) {
	c, _, _ := testConn(t)
	c.den.kinds["3"] = denproto.KindGroupDM
	ctx := context.Background()
	if _, err := c.sendMessage(ctx, "3", denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Text: "hi"}); !errors.Is(err, ErrUnknownChannel) {
		t.Errorf("a message sent to a group's ID: %v", err)
	}
	if _, err := c.takesForward(ctx, "3"); !errors.Is(err, ErrUnknownChannel) {
		t.Errorf("a forward to a group's ID: %v", err)
	}
}

// A kept state as v1.15.0 wrote it, with a group DM, its key and its
// state, reads without them; the group's ID keeps its kind.
func TestKeptStateWithAGroup(t *testing.T) {
	k := readOlderGroups(t).Kept
	s, err := restore(nil, k, nil)
	if err != nil {
		t.Fatalf("v1.15.0's kept state: %v", err)
	}
	if _, ok := s.channels["2"]; ok || s.channels["1"].Kind != denproto.KindText {
		t.Errorf("the kept channels: %+v", s.channels)
	}
	if len(s.signed) != 0 {
		t.Errorf("the kept signed keys: %+v", s.signed)
	}
	if _, ok := s.reads["2"]; ok {
		t.Error("the group's read state stayed")
	}
	if s.kinds["2"] != denproto.KindGroupDM {
		t.Errorf("the group's ID is kept as %q", s.kinds["2"])
	}
	kinds := k.forGood()
	if kinds["2"] != denproto.KindGroupDM {
		t.Errorf("kept for good: %v", kinds)
	}
	back, _ := s.kept()
	b, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"group_states", "group_marks", "group_dms"} {
		if _, ok := fields[f]; ok {
			t.Errorf("the state kept again holds %s", f)
		}
	}
}
