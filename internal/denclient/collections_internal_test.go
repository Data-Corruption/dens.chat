package denclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func TestCleanCollectionsAndHostileInputs(t *testing.T) {
	denID := denproto.Random(denproto.IDSize)
	baseReady := func() denproto.Ready {
		return denproto.Ready{
			Epoch: "e1",
			Den: denproto.Den{
				ID:          denID,
				Name:        "Den",
				URL:         "https://den.example",
				Limits:      denproto.Limits{FileSize: denproto.MinFileSize, MemberStorage: denproto.MinFileSize, DenStorage: denproto.MinFileSize},
				CallLimits:  denproto.DefaultCallLimits,
				Collections: true,
			},
			Me:      denproto.Member{ID: "1", Username: "alice", DisplayName: "Alice", Role: denproto.RoleOwner, JoinedAt: 1},
			Members: []denproto.Member{{ID: "1", Username: "alice", DisplayName: "Alice", Role: denproto.RoleOwner, JoinedAt: 1}},
			Channels: []denproto.Channel{
				{ID: "10", Kind: denproto.KindText, Name: "general"},
				{ID: "11", Kind: denproto.KindVoice, Name: "Lounge", Bitrate: denproto.DefaultVoiceBitrate},
			},
			Collections: []denproto.Collection{
				{ID: "1", ChannelID: "10", Name: "Guides", WhoAdds: denproto.CollectionAddEveryone, Count: 2, CreatedBy: "1", CreatedAt: 100},
				{ID: "2", Name: "Saved", Count: 1, CreatedAt: 200},
			},
			Sync:      denproto.Sync{Epoch: denproto.Random(denproto.SyncEpochSize), Version: 1},
			SealCheck: denproto.Random(denproto.SealCheckSize),
		}
	}

	s := newState()
	if err := s.load(denID, baseReady(), nil, nil, nil, time.Now()); err != nil {
		t.Fatalf("valid ready failed: %v", err)
	}
	if !s.hasCollections || len(s.collections) != 2 {
		t.Fatalf("loaded collections: has=%v cols=%+v", s.hasCollections, s.collections)
	}

	// Cache round-trip preserves HasCollections and Collections.
	kept, members := s.kept()
	restored, err := restore(denID, kept, members)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if !restored.hasCollections || len(restored.collections) != 2 || restored.collections["1"].Name != "Guides" {
		t.Fatalf("restored collections: has=%v cols=%+v", restored.hasCollections, restored.collections)
	}

	// Hostile Ready cases rejected.
	for name, mod := range map[string]func(*denproto.Ready){
		"unknown channel": func(r *denproto.Ready) {
			r.Collections[0].ChannelID = "999"
		},
		"voice channel": func(r *denproto.Ready) {
			r.Collections[0].ChannelID = "11"
		},
		"personal with who_adds": func(r *denproto.Ready) {
			r.Collections[1].WhoAdds = denproto.CollectionAddStaff
		},
		"duplicate ID": func(r *denproto.Ready) {
			r.Collections = append(r.Collections, denproto.Collection{ID: "2", Name: "Other", CreatedAt: 300})
		},
		"duplicate name same scope case-insensitive": func(r *denproto.Ready) {
			r.Collections = append(r.Collections, denproto.Collection{ID: "3", Name: "saved", CreatedAt: 300})
		},
		"duplicate collection name FoldName İ/i": func(r *denproto.Ready) {
			r.Collections = append(r.Collections,
				denproto.Collection{ID: "3", Name: "İ", CreatedAt: 300},
				denproto.Collection{ID: "4", Name: "i", CreatedAt: 400},
			)
		},
		"duplicate role name FoldName École/école": func(r *denproto.Ready) {
			r.Roles = []denproto.Role{{ID: "1", Name: "École"}, {ID: "2", Name: "école"}}
		},
		"duplicate role name FoldName İ/i": func(r *denproto.Ready) {
			r.Roles = []denproto.Role{{ID: "1", Name: "İ"}, {ID: "2", Name: "i"}}
		},
		"over 20 personal": func(r *denproto.Ready) {
			r.Collections = nil
			for i := range denproto.MaxPersonalCollections + 1 {
				r.Collections = append(r.Collections, denproto.Collection{
					ID: denproto.FormatID(int64(i + 1)), Name: "P" + denproto.FormatID(int64(i+1)), CreatedAt: int64(i + 1),
				})
			}
		},
		"count over 200": func(r *denproto.Ready) {
			r.Collections[0].Count = denproto.MaxCollectionItems + 1
		},
	} {
		r := baseReady()
		mod(&r)
		if err := newState().load(denID, r, nil, nil, nil, time.Now()); err == nil {
			t.Errorf("%s: load unexpectedly succeeded", name)
		}
	}

	// Live events: item_added updates count; scope change rejected; channel.deleted drops shared collections.
	me := baseReady().Me
	ev, _ := denproto.NewEvent(denproto.EventCollectionItemAdded, 1, denproto.CollectionItemEvent{
		CollectionID: "1", ChannelID: "10", MessageID: "55", AddedBy: "1", AddedAt: 500, Count: 3,
	})
	if _, ok, err := s.applyEvent(denID, ev, me, time.Now()); err != nil || !ok || s.collections["1"].Count != 3 {
		t.Fatalf("item_added: ok=%v err=%v count=%d", ok, err, s.collections["1"].Count)
	}

	badScope, _ := denproto.NewEvent(denproto.EventCollectionUpdated, 2, denproto.Collection{
		ID: "1", Name: "Became Personal", Count: 3, CreatedAt: 100,
	})
	if _, _, err := s.applyEvent(denID, badScope, me, time.Now()); err == nil {
		t.Error("applyEvent allowed collection to switch scope")
	}

	chDel, _ := denproto.NewEvent(denproto.EventChannelDeleted, 3, denproto.ChannelDeleted{ID: "10"})
	if _, ok, err := s.applyEvent(denID, chDel, me, time.Now()); err != nil || !ok {
		t.Fatalf("channel.deleted: ok=%v err=%v", ok, err)
	}
	if _, still := s.collections["1"]; still {
		t.Error("shared collection survived channel.deleted")
	}
	if _, keptPersonal := s.collections["2"]; !keptPersonal {
		t.Error("personal collection was dropped by channel.deleted")
	}

	// FoldName agrees with den: İ and combining i\u0307 coexist in both roles and collections.
	okFold := baseReady()
	okFold.Roles = []denproto.Role{{ID: "1", Name: "İ"}, {ID: "2", Name: "i\u0307"}}
	okFold.Collections = append(okFold.Collections,
		denproto.Collection{ID: "3", Name: "İ", CreatedAt: 300},
		denproto.Collection{ID: "4", Name: "i\u0307", CreatedAt: 400},
	)
	if err := newState().load(denID, okFold, nil, nil, nil, time.Now()); err != nil {
		t.Fatalf("distinct İ and i\\u0307 rejected by client: %v", err)
	}
}

func TestCollectionItemsPagingHostileDenAndUnknownChannel(t *testing.T) {
	denID := denproto.Random(denproto.IDSize)
	mkItem := func(msgID, chID string, at int64) denproto.CollectionItem {
		return denproto.CollectionItem{
			Message: denproto.Message{
				ID: msgID, ChannelID: chID, AuthorID: "1", CreatedAt: at, Revision: 1, Text: "msg " + msgID,
			},
			AddedBy: "1",
			AddedAt: at,
		}
	}
	personalCol := denproto.Collection{ID: "7", Name: "Saved", Count: 3, CreatedAt: 100}

	runWithPages := func(t *testing.T, pages []denproto.CollectionItems) (PageCollectionItems, error) {
		t.Helper()
		idx := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(denproto.HeaderRange, denproto.RangeHeader())
			if idx >= len(pages) {
				http.Error(w, "too many page requests", http.StatusInternalServerError)
				return
			}
			p := pages[idx]
			idx++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(p)
		}))
		defer srv.Close()

		st := newState()
		st.channels["10"] = denproto.Channel{ID: "10", Kind: denproto.KindText, Name: "general"}
		mgr := &Manager{Now: time.Now}
		c := &conn{
			m:       mgr,
			j:       &joined{denID: denID},
			api:     &api{base: srv.URL, client: srv.Client()},
			den:     st,
			token:   denproto.Random(denproto.TokenSize),
			expires: time.Now().Add(time.Hour),
		}
		mgr.conns = []*conn{c}
		return mgr.CollectionItems(context.Background(), denID.String(), "7")
	}

	// 1. Multi-page response drops personal items whose channel is absent from c.den.channels.
	got, err := runWithPages(t, []denproto.CollectionItems{
		{
			Collection:   personalCol,
			Items:        []denproto.CollectionItem{mkItem("30", "10", 300), mkItem("20", "999", 200)},
			HasMore:      true,
			NextBeforeAt: 200,
			NextBeforeID: "20",
		},
		{
			Collection: personalCol,
			Items:      []denproto.CollectionItem{mkItem("10", "10", 100)},
		},
	})
	if err != nil {
		t.Fatalf("multi-page personal collection failed: %v", err)
	}
	gotIDs := make([]string, len(got.Items))
	for i, it := range got.Items {
		gotIDs[i] = it.Message.ID
	}
	if !slices.Equal(gotIDs, []string{"30", "10"}) || got.Collection.Count != 2 {
		t.Fatalf("got IDs=%v count=%d, want [30 10] count=2 (dropping unknown channel 999)", gotIDs, got.Collection.Count)
	}

	// 2. Hostile den: non-advancing cursor (same timestamp and same/greater ID) rejected.
	if _, err := runWithPages(t, []denproto.CollectionItems{
		{Collection: personalCol, Items: []denproto.CollectionItem{mkItem("30", "10", 300)}, HasMore: true, NextBeforeAt: 300, NextBeforeID: "30"},
		{Collection: personalCol, Items: []denproto.CollectionItem{mkItem("29", "10", 300)}, HasMore: true, NextBeforeAt: 300, NextBeforeID: "30"},
	}); err == nil {
		t.Error("expected error for non-advancing cursor")
	}
	if _, err := runWithPages(t, []denproto.CollectionItems{
		{Collection: personalCol, Items: []denproto.CollectionItem{mkItem("30", "10", 300)}, HasMore: true, NextBeforeAt: 300, NextBeforeID: "30"},
		{Collection: personalCol, Items: []denproto.CollectionItem{mkItem("29", "10", 300)}, HasMore: true, NextBeforeAt: 301, NextBeforeID: "1"},
	}); err == nil {
		t.Error("expected error for backward timestamp cursor")
	}

	// 3. Hostile den: empty page with HasMore=true rejected.
	if _, err := runWithPages(t, []denproto.CollectionItems{
		{Collection: personalCol, Items: []denproto.CollectionItem{}, HasMore: true, NextBeforeAt: 300, NextBeforeID: "30"},
	}); err == nil {
		t.Error("expected error for empty page with has_more")
	}

	// 4. Hostile den: >200 total items across pages rejected.
	makeBatch := func(startID int64, n int, at int64) []denproto.CollectionItem {
		out := make([]denproto.CollectionItem, n)
		for i := range n {
			out[i] = mkItem(denproto.FormatID(startID-int64(i)), "10", at)
		}
		return out
	}
	if _, err := runWithPages(t, []denproto.CollectionItems{
		{Collection: personalCol, Items: makeBatch(300, 120, 300), HasMore: true, NextBeforeAt: 300, NextBeforeID: "181"},
		{Collection: personalCol, Items: makeBatch(180, 81, 200)},
	}); err == nil {
		t.Error("expected error for >200 total items across pages")
	}

	// 5. Hostile den: HasMore=true after already receiving 200 items rejected.
	if _, err := runWithPages(t, []denproto.CollectionItems{
		{Collection: personalCol, Items: makeBatch(300, 200, 300), HasMore: true, NextBeforeAt: 300, NextBeforeID: "101"},
	}); err == nil {
		t.Error("expected error for has_more at 200 items")
	}
}
