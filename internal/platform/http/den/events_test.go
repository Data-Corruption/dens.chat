package den

import (
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
