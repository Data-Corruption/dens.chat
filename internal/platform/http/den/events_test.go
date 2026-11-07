package den

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestEventsOverHTTP checks the events' routes (M17.4): staff make one,
// answered with it and their own mark; a member reads it by its ID, as a
// link's card does (M17.5), marks Interested and takes it back, answered
// with the count and their mark; a member who isn't its maker or above
// them can't change, cancel or end it; and its maker changes and cancels
// it.
func TestEventsOverHTTP(t *testing.T) {
	f := newFixture(t)
	alice, _ := f.owner()
	bob := f.member(alice, "bob")
	a := f.session(alice)
	lounge := f.channel(a, "lounge", denproto.KindVoice, false)
	name := "Game night"
	starts, ends := time.Now().Add(time.Hour).UnixMilli(), time.Now().Add(2*time.Hour).UnixMilli()
	req := denproto.EventRequest{Name: &name, StartsAt: &starts, EndsAt: &ends, ChannelID: &lounge.ID}
	if status := f.call(http.MethodPost, "/api/events", bob, req, nil); status != http.StatusForbidden {
		t.Fatalf("bob making an event while they're staff's: %d", status)
	}
	var e denproto.DenEvent
	if status := f.call(http.MethodPost, "/api/events", alice, req, &e); status != http.StatusCreated ||
		denproto.CheckDenEvent(e) != nil || e.Mine == nil || *e.Mine {
		t.Fatalf("alice making an event: %d, %+v", status, e)
	}
	path := "/api/events/" + e.ID
	// A link's card reads it by its ID (M17.5), and nothing that isn't one.
	var read denproto.DenEvent
	if status := f.call(http.MethodGet, path, bob, nil, &read); status != http.StatusOK || read.ID != e.ID || read.Mine == nil || *read.Mine {
		t.Fatalf("bob reading the event: %d, %+v", status, read)
	}
	if status := f.call(http.MethodGet, "/api/events/999999", bob, nil, nil); status != http.StatusNotFound {
		t.Fatalf("bob reading no event: %d", status)
	}
	var in denproto.EventInterest
	if status := f.call(http.MethodPut, path+"/interest", bob, nil, &in); status != http.StatusOK || in.Interested != 1 || !*in.Mine {
		t.Fatalf("bob's mark: %d, %+v", status, in)
	}
	if status := f.call(http.MethodDelete, path+"/interest", bob, nil, &in); status != http.StatusOK || in.Interested != 0 || *in.Mine {
		t.Fatalf("bob taking it back: %d, %+v", status, in)
	}
	renamed := "Quiz night"
	for _, step := range []struct {
		method, path string
		token        denproto.Bytes
		body         any
		want         int
	}{
		{http.MethodPatch, path, bob, denproto.EventRequest{Name: &renamed}, http.StatusForbidden},
		{http.MethodDelete, path, bob, nil, http.StatusForbidden},
		{http.MethodPost, path + "/end", bob, nil, http.StatusForbidden},
		{http.MethodPost, path + "/end", alice, nil, http.StatusForbidden},
		{http.MethodPatch, path, alice, denproto.EventRequest{Name: &renamed}, http.StatusOK},
		{http.MethodPut, "/api/events/999999/interest", bob, nil, http.StatusNotFound},
		{http.MethodDelete, path, alice, nil, http.StatusNoContent},
		{http.MethodDelete, path, alice, nil, http.StatusNotFound},
	} {
		if status := f.call(step.method, step.path, step.token, step.body, nil); status != step.want {
			t.Fatalf("%s %s: %d, want %d", step.method, step.path, status, step.want)
		}
	}
}

// TestBadgeHiddenOverHTTP checks a member hides a badge they earned from
// their card and shows it again (M17.6), answered with 204: their own
// profile carries it marked, and another's leaves it out; and a badge they
// didn't earn is 404.
func TestBadgeHiddenOverHTTP(t *testing.T) {
	f := newFixture(t)
	alice, _ := f.owner()
	bob := f.member(alice, "bob")
	lounge := f.channel(f.session(alice), "lounge", denproto.KindVoice, false)
	pic := emojiPNG(t, 128, 128)
	resp := f.upload(alice, "badge.png", bytes.NewReader(pic), int64(len(pic)))
	var up denproto.File
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&up) != nil {
		t.Fatalf("upload a badge's picture: %d", resp.StatusCode)
	}
	resp.Body.Close()
	name, title := "Game night", "Was there"
	starts, ends := time.Now().Add(time.Hour).UnixMilli(), time.Now().Add(2*time.Hour).UnixMilli()
	var e denproto.DenEvent
	if status := f.call(http.MethodPost, "/api/events", alice, denproto.EventRequest{Name: &name, StartsAt: &starts, EndsAt: &ends,
		ChannelID: &lounge.ID, Badge: &up.ID, BadgeTitle: &title}, &e); status != http.StatusCreated || e.Badge == nil {
		t.Fatalf("alice making a badged event: %d, %+v", status, e)
	}
	// Bob earned it, as his call would have given it.
	bobID := f.session(bob).MemberID
	if _, err := f.db.Exec(`INSERT INTO den_badge_awards (event_id, member_id, earned_at) VALUES (?, ?, ?)`, mustID(e.ID), bobID, starts); err != nil {
		t.Fatal(err)
	}
	profile := func(token denproto.Bytes) []denproto.MemberBadge {
		t.Helper()
		var p denproto.MemberProfile
		if status := f.call(http.MethodGet, "/api/members/"+denproto.FormatID(bobID), token, nil, &p); status != http.StatusOK {
			t.Fatalf("reading bob's profile: %d", status)
		}
		return p.Badges
	}
	path := "/api/me/badges/" + e.ID
	if status := f.call(http.MethodPut, path, alice, denproto.BadgeHiddenRequest{Hidden: true}, nil); status != http.StatusNotFound {
		t.Fatalf("alice hiding a badge she didn't earn: %d", status)
	}
	if status := f.call(http.MethodPut, path, bob, denproto.BadgeHiddenRequest{Hidden: true}, nil); status != http.StatusNoContent {
		t.Fatalf("bob hiding his badge: %d", status)
	}
	if own, other := profile(bob), profile(alice); len(own) != 1 || !own[0].Hidden || len(other) != 0 {
		t.Fatalf("hidden, bob's badges read %+v to him and %+v to alice", own, other)
	}
	if status := f.call(http.MethodPut, path, bob, "hidden", nil); status != http.StatusBadRequest {
		t.Fatalf("a malformed body: %d", status)
	}
	if status := f.call(http.MethodPut, path, bob, denproto.BadgeHiddenRequest{}, nil); status != http.StatusNoContent {
		t.Fatalf("bob showing his badge: %d", status)
	}
	if own, other := profile(bob), profile(alice); len(own) != 1 || own[0].Hidden || len(other) != 1 || other[0].Hidden {
		t.Fatalf("shown again, bob's badges read %+v to him and %+v to alice", own, other)
	}
}
