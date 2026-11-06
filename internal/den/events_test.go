package den

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// eventAt is a request for an event in a voice channel, starting in ahead
// of the fixture's clock and lasting length.
func (f *fixture) eventAt(name, channel string, ahead, length time.Duration) denproto.EventRequest {
	starts := f.clock.Add(ahead).UnixMilli()
	ends := f.clock.Add(ahead + length).UnixMilli()
	return denproto.EventRequest{Name: &name, StartsAt: &starts, EndsAt: &ends, ChannelID: &channel}
}

// makeEvent makes an event as s.
func (f *fixture) makeEvent(s *Session, req denproto.EventRequest) denproto.DenEvent {
	f.t.Helper()
	e, err := f.d.CreateEvent(context.Background(), s, req)
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

// eventsOf lists the events s's snapshot holds, by name, with their marks
// and counts, live ones marked.
func (f *fixture) eventsOf(s *Session) string {
	f.t.Helper()
	snap, err := f.d.snapshot(context.Background(), s.MemberID, IsStaff(s.Role), 0, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, e := range snap.Events {
		if err := denproto.CheckDenEvent(e); err != nil || e.Mine == nil {
			f.t.Fatalf("%q isn't an event fit to keep: %v", e.Name, err)
		}
		s := fmt.Sprintf("%s:%d", e.Name, e.Interested)
		if *e.Mine {
			s += "+mine"
		}
		if e.Live {
			s += "+live"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// eventsHeard drains the events about events a subscription holds now, as
// type:ID, with live and how many are interested where they say.
func eventsHeard(t *testing.T, sub *Sub) []string {
	t.Helper()
	var out []string
	for {
		select {
		case e := <-sub.Events:
			if !strings.HasPrefix(e.T, "event.") {
				continue
			}
			out = append(out, eventSaid(t, e))
		default:
			return out
		}
	}
}

func eventSaid(t *testing.T, e denproto.Event) string {
	t.Helper()
	switch e.T {
	case denproto.EventEventCanceled, denproto.EventEventEnded:
		return e.T + ":" + decode[denproto.EventID](t, e).ID
	case denproto.EventEventInterest:
		in := decode[denproto.EventInterest](t, e)
		s := fmt.Sprintf("%s:%s:%d", e.T, in.EventID, in.Interested)
		if in.Mine != nil {
			s += fmt.Sprintf(":mine=%v", *in.Mine)
		}
		return s
	}
	ev := decode[denproto.DenEvent](t, e)
	if err := denproto.CheckDenEvent(ev); err != nil {
		t.Fatalf("%s carries an event unfit to keep: %v", e.T, err)
	}
	s := e.T + ":" + ev.ID
	if ev.Live {
		s += ":live"
	}
	if ev.Mine != nil {
		t.Errorf("%s to everyone carries a member's mark", e.T)
	}
	return s
}

// TestEventsWhoMakesThem checks who makes events: staff, and members only
// as far as the owner's limit lets each, which lowering leaves their events
// as they were; the den holding 50 that haven't ended; and what an event
// takes: a name, a start from now to a year ahead, a length of 5 minutes
// to a week, and a place, a voice channel the maker sees or a line of
// text, which a development instance's den takes of seconds.
func TestEventsWhoMakesThem(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	general := f.newChannel(alice, "general", denproto.ChannelRequest{})
	hidden := f.newChannel(alice, "backstage", denproto.ChannelRequest{Kind: denproto.KindVoice, StaffOnly: ptr(true)})

	if info, _ := f.d.Info(); info.EventLimits == nil || info.EventLimits.MemberEvents != 0 {
		t.Fatalf("a new den's event limits are %+v, not 0 for members", info.EventLimits)
	}
	_, err := f.d.CreateEvent(ctx, bob, f.eventAt("Bob's night", lounge.ID, time.Hour, time.Hour))
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.Update(ctx, bob, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 2}})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 11}})
	wantCode(t, err, denproto.CodeInvalidField)
	if _, err := f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 2}}); err != nil {
		t.Fatal(err)
	}
	f.open() // the limit is kept
	if info, _ := f.d.Info(); info.EventLimits == nil || info.EventLimits.MemberEvents != 2 {
		t.Fatalf("the den reopened with event limits %+v", info.EventLimits)
	}

	place := "Bob's place"
	one := f.makeEvent(bob, f.eventAt("One", lounge.ID, time.Hour, time.Hour))
	textReq := f.eventAt("Two", "", 2*time.Hour, time.Hour)
	textReq.ChannelID, textReq.Place = nil, &place
	two := f.makeEvent(bob, textReq)
	if one.ChannelID != lounge.ID || one.Place != "" || two.Place != place || two.ChannelID != "" || one.MadeBy != sid(bob) ||
		one.Mine == nil || *one.Mine || one.Interested != 0 || one.Live {
		t.Fatalf("bob made %+v and %+v", one, two)
	}
	_, err = f.d.CreateEvent(ctx, bob, f.eventAt("Three", lounge.ID, time.Hour, time.Hour))
	wantCode(t, err, denproto.CodeForbidden)
	// Lowering the limit leaves bob's events, which he still changes.
	if _, err := f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.UpdateEvent(ctx, bob, one.ID, denproto.EventRequest{Name: ptr("One, renamed")}); err != nil {
		t.Fatal(err)
	}
	if got := f.eventsOf(bob); got != "One, renamed:0 Two:0" {
		t.Errorf("bob's events after the limit went down: %s", got)
	}

	// What an event takes.
	for name, change := range map[string]func(*denproto.EventRequest){
		"no name":     func(r *denproto.EventRequest) { r.Name = ptr(" ​ ") },
		"a long name": func(r *denproto.EventRequest) { r.Name = ptr(strings.Repeat("x", denproto.MaxEventNameRunes+1)) },
		"a long description": func(r *denproto.EventRequest) {
			r.Description = ptr(strings.Repeat("x", denproto.MaxEventDescriptionRunes+1))
		},
		"a start past":        func(r *denproto.EventRequest) { r.StartsAt = ptr(f.clock.Add(-2 * time.Minute).UnixMilli()) },
		"a start past a year": func(r *denproto.EventRequest) { *r = f.eventAt("Far", lounge.ID, 366*24*time.Hour, time.Hour) },
		"too short":           func(r *denproto.EventRequest) { *r = f.eventAt("Short", lounge.ID, time.Hour, 4*time.Minute) },
		"too long": func(r *denproto.EventRequest) {
			*r = f.eventAt("Long", lounge.ID, time.Hour, 7*24*time.Hour+time.Minute)
		},
		"a text channel": func(r *denproto.EventRequest) { r.ChannelID = &general.ID },
		"no channel":     func(r *denproto.EventRequest) { r.ChannelID = ptr("99999") },
		"no place":       func(r *denproto.EventRequest) { r.ChannelID = nil },
		"two places":     func(r *denproto.EventRequest) { r.Place = ptr("here") },
		"a blank place":  func(r *denproto.EventRequest) { r.ChannelID, r.Place = nil, ptr("  ") },
		"no end":         func(r *denproto.EventRequest) { r.EndsAt = nil },
		"no banner":      func(r *denproto.EventRequest) { r.Banner = ptr("12345") },
	} {
		req := f.eventAt("Fine", lounge.ID, time.Hour, time.Hour)
		change(&req)
		if _, err := f.d.CreateEvent(ctx, alice, req); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Bob can't see the staff's channel, so he can't put an event there.
	if _, err := f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 3}}); err != nil {
		t.Fatal(err)
	}
	_, err = f.d.CreateEvent(ctx, bob, f.eventAt("Sneaky", hidden.ID, time.Hour, time.Hour))
	wantCode(t, err, denproto.CodeInvalidField)
	// A start this minute, a moment past, starts at once; a name keeps its
	// emoji whole.
	now := f.makeEvent(alice, f.eventAt("🧑‍🚀 Now", lounge.ID, -30*time.Second, time.Hour))
	if now.Name != "🧑‍🚀 Now" {
		t.Errorf("the name came back as %q", now.Name)
	}

	// The den holds 50 that haven't ended, staff's among them.
	for i := len(f.eventsOf(alice)); i < 200; i++ {
		_, err := f.d.CreateEvent(ctx, alice, f.eventAt(fmt.Sprint("Filler ", i), lounge.ID, time.Hour, time.Hour))
		if err != nil {
			wantCode(t, err, denproto.CodeForbidden)
			break
		}
	}
	snap, err := f.d.snapshot(ctx, alice.MemberID, true, 0, nil)
	if err != nil || len(snap.Events) != denproto.MaxEvents {
		t.Fatalf("the den holds %d events, %v", len(snap.Events), err)
	}

	// Seconds, as a development instance's den takes.
	f.d.ShortTimes = true
	if err := f.d.CancelEvent(ctx, alice, snap.Events[len(snap.Events)-1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.CreateEvent(ctx, alice, f.eventAt("Brief", lounge.ID, time.Second, 5*time.Second)); err != nil {
		t.Errorf("a den with short times refused an event of 5 seconds: %v", err)
	}
}

// TestEventsChangeByRank checks who changes and cancels an event: its
// maker, and staff above them, by the rule calls and messages use; and
// that an event that started is ended, never canceled.
func TestEventsChangeByRank(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(alice, "carol")
	dave := f.member(alice, "dave")
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	if _, err := f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 5}}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Session{carol} {
		if _, err := f.d.SetRole(ctx, alice, sid(s), denproto.RoleRequest{Role: denproto.RoleModerator}); err != nil {
			t.Fatal(err)
		}
		s.Role = denproto.RoleModerator
	}
	bobs := f.makeEvent(bob, f.eventAt("Bob's", lounge.ID, time.Hour, time.Hour))
	carols := f.makeEvent(carol, f.eventAt("Carol's", lounge.ID, time.Hour, time.Hour))
	alices := f.makeEvent(alice, f.eventAt("Alice's", lounge.ID, time.Hour, time.Hour))

	rename := func(s *Session, id string) error {
		_, err := f.d.UpdateEvent(ctx, s, id, denproto.EventRequest{Name: ptr("Renamed by " + sid(s))})
		return err
	}
	for _, c := range []struct {
		who  *Session
		what denproto.DenEvent
		ok   bool
	}{
		{bob, bobs, true}, {dave, bobs, false}, {carol, bobs, true}, {alice, bobs, true},
		{bob, carols, false}, {carol, carols, true}, {alice, carols, true},
		{carol, alices, false}, {bob, alices, false}, {alice, alices, true},
	} {
		err := rename(c.who, c.what.ID)
		if c.ok && err != nil || !c.ok && !denproto.IsCode(err, denproto.CodeForbidden) {
			t.Errorf("member %s renaming %s: %v", sid(c.who), c.what.Name, err)
		}
	}
	wantCode(t, f.d.CancelEvent(ctx, dave, bobs.ID), denproto.CodeForbidden)
	wantCode(t, f.d.CancelEvent(ctx, carol, alices.ID), denproto.CodeForbidden)
	sub, _, _, _ := f.d.Hub.Subscribe(dave.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	if err := f.d.CancelEvent(ctx, carol, bobs.ID); err != nil {
		t.Fatalf("a moderator canceling a member's event: %v", err)
	}
	if got := eventsHeard(t, sub); !slices.Equal(got, []string{"event.canceled:" + bobs.ID}) {
		t.Errorf("dave heard %v", got)
	}
	wantCode(t, f.d.CancelEvent(ctx, carol, bobs.ID), denproto.CodeNotFound)

	// Started, it can only be ended.
	f.clock = f.clock.Add(90 * time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.d.CancelEvent(ctx, carol, carols.ID), denproto.CodeForbidden)
	wantCode(t, f.d.EndEvent(ctx, bob, carols.ID), denproto.CodeForbidden)
	if err := f.d.EndEvent(ctx, alice, carols.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.eventsOf(dave); got != "Renamed by "+sid(alice)+":0+live" {
		t.Errorf("dave's events: %s", got)
	}
}

// TestEventsStaffOnly checks an event in a staff-only voice channel: staff
// alone see it, in their snapshots, its events and its banner, and it
// follows its channel as that turns public and back, or as staff move it.
func TestEventsStaffOnly(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	backstage := f.newChannel(alice, "backstage", denproto.ChannelRequest{Kind: denproto.KindVoice, StaffOnly: ptr(true)})
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	bobs, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(bobs)
	alices, _, _, _ := f.d.Hub.Subscribe(alice.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(alices)

	banner := f.mustUpload(alice, "banner.png", testPNG(t, 600, 200))
	req := f.eventAt("Staff meeting", backstage.ID, time.Hour, time.Hour)
	req.Banner = &banner.ID
	e := f.makeEvent(alice, req)
	if e.Banner.ID != banner.ID || e.Banner.Width != 600 {
		t.Fatalf("the event's banner is %+v", e.Banner)
	}
	if got := eventsHeard(t, bobs); len(got) != 0 {
		t.Errorf("bob heard of a staff-only event: %v", got)
	}
	if got := eventsHeard(t, alices); !slices.Equal(got, []string{"event.created:" + e.ID}) {
		t.Errorf("alice heard %v", got)
	}
	if got := f.eventsOf(bob); got != "" {
		t.Errorf("bob's snapshot holds %s", got)
	}
	if got := f.eventsOf(alice); got != "Staff meeting:0" {
		t.Errorf("alice's snapshot holds %s", got)
	}
	if opens(f, bob, banner.ID) {
		t.Error("bob opened a staff-only event's banner")
	}
	r, err := f.d.OpenFile(ctx, alice, banner.ID, false)
	if err != nil {
		t.Fatalf("alice can't open the banner: %v", err)
	}
	r.Close()
	_, err = f.d.SetInterest(ctx, bob, e.ID, true)
	wantCode(t, err, denproto.CodeNotFound)

	// The channel turns public: bob's sockets start over, and his snapshot
	// holds the event, his own banner to open.
	if _, err := f.d.UpdateChannel(ctx, alice, backstage.ID, denproto.ChannelRequest{StaffOnly: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if got := f.eventsOf(bob); got != "Staff meeting:0" {
		t.Errorf("with the channel public, bob's snapshot holds %s", got)
	}
	r, err = f.d.OpenFile(ctx, bob, banner.ID, false)
	if err != nil {
		t.Fatalf("bob can't open the public event's banner: %v", err)
	}
	r.Close()
	if _, err := f.d.SetInterest(ctx, bob, e.ID, true); err != nil {
		t.Fatal(err)
	}
	// And back: bob's mark stays, but counts for nobody who sees it.
	if _, err := f.d.UpdateChannel(ctx, alice, backstage.ID, denproto.ChannelRequest{StaffOnly: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if got := f.eventsOf(bob); got != "" {
		t.Errorf("with the channel staff's again, bob's snapshot holds %s", got)
	}
	if got := f.eventsOf(alice); got != "Staff meeting:0" {
		t.Errorf("with the channel staff's again, alice's snapshot holds %s", got)
	}
	drain(bobs)
	drain(alices)

	// Staff move it out to the lounge, and back in: each time members who
	// aren't staff start over from a snapshot, which holds it or doesn't.
	if _, err := f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{ChannelID: &lounge.ID}); err != nil {
		t.Fatal(err)
	}
	if got := f.eventsOf(bob); got != "Staff meeting:1+mine" {
		t.Errorf("in the lounge, bob's snapshot holds %s", got)
	}
	if !refreshed(bobs) {
		t.Error("bob's socket didn't start over as the event came out")
	}
	if _, err := f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{ChannelID: &backstage.ID}); err != nil {
		t.Fatal(err)
	}
	if got := f.eventsOf(bob); got != "" {
		t.Errorf("back in the staff's channel, bob's snapshot holds %s", got)
	}
	if !refreshed(bobs) {
		t.Error("bob's socket didn't start over as the event went back")
	}
	// As it starts, bob, interested, hears nothing of it.
	f.clock = f.clock.Add(time.Hour)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, bobs); len(got) != 0 {
		t.Errorf("bob heard the staff-only event start: %v", got)
	}
	if got := eventsHeard(t, alices); !slices.Contains(got, "event.updated:"+e.ID+":live") {
		t.Errorf("alice heard %v as it started", got)
	}
}

// opens reports whether s opens a file, closing it if so: Windows can't
// remove a test's directory while a file in it is open.
func opens(f *fixture, s *Session, id string) bool {
	r, err := f.d.OpenFile(context.Background(), s, id, false)
	if err != nil {
		return false
	}
	r.Close()
	return true
}

// refreshed reports whether a subscription was told to start over, and
// drains it.
func refreshed(sub *Sub) bool { return slices.Contains(drain(sub), eventRefresh) }

// TestEventsInterest checks Interested: each member's mark, which the den
// counts for everyone who sees the event and tells the member's own
// sessions, and takes back; nobody sees who.
func TestEventsInterest(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(alice, "carol")
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	e := f.makeEvent(alice, f.eventAt("Game night", lounge.ID, time.Hour, 2*time.Hour))
	bobs, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(bobs)
	carols, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(carols)

	in, err := f.d.SetInterest(ctx, bob, e.ID, true)
	if err != nil || in.Interested != 1 || in.Mine == nil || !*in.Mine {
		t.Fatalf("bob's mark: %+v, %v", in, err)
	}
	if got := eventsHeard(t, bobs); !slices.Equal(got, []string{"event.interest:" + e.ID + ":1", "event.interest:" + e.ID + ":1:mine=true"}) {
		t.Errorf("bob heard %v", got)
	}
	if got := eventsHeard(t, carols); !slices.Equal(got, []string{"event.interest:" + e.ID + ":1"}) {
		t.Errorf("carol heard %v", got)
	}
	// Marking again changes nothing, and says nothing.
	if in, err := f.d.SetInterest(ctx, bob, e.ID, true); err != nil || in.Interested != 1 || !*in.Mine {
		t.Fatalf("bob's mark again: %+v, %v", in, err)
	}
	if got := eventsHeard(t, carols); len(got) != 0 {
		t.Errorf("a mark that changed nothing said %v", got)
	}
	if _, err := f.d.SetInterest(ctx, carol, e.ID, true); err != nil {
		t.Fatal(err)
	}
	for who, want := range map[*Session]string{alice: "Game night:2", bob: "Game night:2+mine", carol: "Game night:2+mine"} {
		if got := f.eventsOf(who); got != want {
			t.Errorf("member %s's snapshot: %s, want %s", sid(who), got, want)
		}
	}
	if in, err := f.d.SetInterest(ctx, bob, e.ID, false); err != nil || in.Interested != 1 || *in.Mine {
		t.Fatalf("bob taking it back: %+v, %v", in, err)
	}
	if got := f.eventsOf(bob); got != "Game night:1" {
		t.Errorf("bob's snapshot after taking it back: %s", got)
	}
	_, err = f.d.SetInterest(ctx, bob, "99999", true)
	wantCode(t, err, denproto.CodeNotFound)
}

// TestEventsByTimer checks the timer against a fixed clock: at its start
// an event goes live, event.started reaching its interested members alone,
// staff or not, and event.updated the rest, and a socket that resumes soon
// after hears it still; at its end it ends, for everyone, forgetting who
// was interested. An event that passed whole while the den was stopped
// ends without starting.
func TestEventsByTimer(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(alice, "carol")
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	e := f.makeEvent(alice, f.eventAt("Game night", lounge.ID, time.Hour, 2*time.Hour))
	missed := f.makeEvent(alice, f.eventAt("Missed", lounge.ID, 2*time.Hour, 30*time.Minute))
	if _, err := f.d.SetInterest(ctx, bob, e.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.SetInterest(ctx, alice, e.ID, true); err != nil {
		t.Fatal(err)
	}
	carols, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(carols)
	alices, _, _, _ := f.d.Hub.Subscribe(alice.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(alices)
	// Bob's socket is away as it starts, and resumes after.
	gone, _, _, at := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	f.d.Hub.Unsubscribe(gone)
	drain(alices)

	// Not yet.
	f.clock = f.clock.Add(time.Hour - time.Second)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, carols); len(got) != 0 {
		t.Fatalf("a second early, carol heard %v", got)
	}
	f.clock = f.clock.Add(time.Second)
	f.d.StartTimer()
	started := nextEvent[denproto.DenEvent](t, alices, denproto.EventEventStarted)
	f.d.StopTimer()
	if started.ID != e.ID || !started.Live || started.Interested != 2 || started.Mine != nil {
		t.Errorf("alice, interested, heard it start as %+v", started)
	}
	if got := eventsHeard(t, carols); !slices.Equal(got, []string{"event.updated:" + e.ID + ":live"}) {
		t.Errorf("carol, not interested, heard %v", got)
	}
	_, replay, resumed, _ := f.d.Hub.Subscribe(bob.MemberID, false, f.d.Hub.Epoch(), at)
	var said []string
	for _, ev := range replay {
		if strings.HasPrefix(ev.T, "event.") {
			said = append(said, eventSaid(t, ev))
		}
	}
	if !resumed || !slices.Equal(said, []string{"event.started:" + e.ID + ":live"}) {
		t.Errorf("bob resuming heard %v, resumed %v", said, resumed)
	}
	if got := f.eventsOf(carol); got != "Game night:2+live Missed:0" {
		t.Errorf("carol's snapshot once it started: %s", got)
	}

	// The den stops through all of Missed, and part of Game night: started
	// again, the timer ends Missed without starting it.
	f.clock = f.clock.Add(2 * time.Hour)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, carols); !slices.Equal(got, []string{"event.ended:" + missed.ID}) {
		t.Errorf("as Missed passed whole, carol heard %v", got)
	}
	if err := f.d.endDueEvents(ctx); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, carols); !slices.Equal(got, []string{"event.ended:" + e.ID}) {
		t.Errorf("at the end carol heard %v", got)
	}
	if got := f.eventsOf(bob); got != "" {
		t.Errorf("once it ended bob's snapshot holds %s", got)
	}
	var marks int
	var state string
	var ends int64
	if err := f.db.QueryRow(`SELECT (SELECT count(*) FROM den_event_interest), state, ends_at FROM den_events WHERE id = ?`, e.ID).
		Scan(&marks, &state, &ends); err != nil || marks != 0 || state != eventEnded || ends != e.EndsAt {
		t.Errorf("the ended event holds %d marks, %s, ending %d, %v", marks, state, ends, err)
	}
	_, err := f.d.SetInterest(ctx, bob, e.ID, true)
	wantCode(t, err, denproto.CodeNotFound)
}

// TestEventsEndNowAndMoved checks a live event's end: its maker or staff
// move it, never before now and within its length's limits, its start
// staying, and the timer ends it there; or End now ends it at once.
func TestEventsEndNowAndMoved(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	if _, err := f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 1}}); err != nil {
		t.Fatal(err)
	}
	e := f.makeEvent(bob, f.eventAt("Stream", lounge.ID, time.Minute, time.Hour))
	f.clock = f.clock.Add(31 * time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(alice.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	end := func(at time.Time) error {
		_, err := f.d.UpdateEvent(ctx, bob, e.ID, denproto.EventRequest{EndsAt: ptr(at.UnixMilli())})
		return err
	}
	starts := time.UnixMilli(e.StartsAt)
	wantCode(t, end(f.clock.Add(-time.Minute)), denproto.CodeInvalidField)
	wantCode(t, end(starts.Add(denproto.MaxEventLength+time.Minute)), denproto.CodeInvalidField)
	_, err := f.d.UpdateEvent(ctx, bob, e.ID, denproto.EventRequest{StartsAt: ptr(e.StartsAt + 1000)})
	wantCode(t, err, denproto.CodeInvalidField)
	// Brought forward, and put back.
	if err := end(f.clock.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := end(f.clock.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, sub); !slices.Equal(got, []string{"event.updated:" + e.ID + ":live", "event.updated:" + e.ID + ":live"}) {
		t.Errorf("alice heard %v", got)
	}
	// The same start, given again, is no change to it.
	if _, err := f.d.UpdateEvent(ctx, bob, e.ID, denproto.EventRequest{StartsAt: ptr(e.StartsAt), Name: ptr("Stream on")}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Hour)
	if err := f.d.endDueEvents(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.eventsOf(alice); got != "Stream on:0+live" {
		t.Errorf("before its moved end: %s", got)
	}
	// End now, which bob's event's maker can, as staff could.
	wantCode(t, f.d.CancelEvent(ctx, bob, e.ID), denproto.CodeForbidden)
	drain(sub)
	if err := f.d.EndEvent(ctx, bob, e.ID); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, sub); !slices.Equal(got, []string{"event.ended:" + e.ID}) {
		t.Errorf("alice heard %v", got)
	}
	var ends int64
	if err := f.db.QueryRow(`SELECT ends_at FROM den_events WHERE id = ?`, e.ID).Scan(&ends); err != nil || ends != f.clock.UnixMilli() {
		t.Errorf("End now left the end at %d, %v", ends, err)
	}
	if err := f.d.EndEvent(ctx, bob, e.ID); err != nil {
		t.Errorf("ending it again: %v", err)
	}
	if got := eventsHeard(t, sub); len(got) != 0 {
		t.Errorf("ending it again said %v", got)
	}
	// An event to come is canceled, not ended; and a member's events to
	// come count against their limit, their live ones too.
	next := f.makeEvent(bob, f.eventAt("Next", lounge.ID, time.Hour, time.Hour))
	wantCode(t, f.d.EndEvent(ctx, bob, next.ID), denproto.CodeForbidden)
}

// TestEventsGoWithTheirMaker checks a member leaving: their events to come
// go, with their banners, and their marks on the rest, whose counts the
// rest hear; their live event goes on to its end.
func TestEventsGoWithTheirMaker(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	if _, err := f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 3}}); err != nil {
		t.Fatal(err)
	}
	live := f.makeEvent(bob, f.eventAt("Live", lounge.ID, time.Minute, 3*time.Hour))
	banner := f.mustUpload(bob, "banner.png", testPNG(t, 900, 300))
	req := f.eventAt("Coming", lounge.ID, 2*time.Hour, time.Hour)
	req.Banner = &banner.ID
	coming := f.makeEvent(bob, req)
	alices := f.makeEvent(alice, f.eventAt("Alice's", lounge.ID, 2*time.Hour, time.Hour))
	if _, err := f.d.SetInterest(ctx, bob, alices.ID, true); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(alice.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	if err := f.d.Leave(ctx, bob); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, sub); !slices.Equal(got, []string{"event.canceled:" + coming.ID, "event.interest:" + alices.ID + ":0"}) {
		t.Errorf("as bob left alice heard %v", got)
	}
	if got := f.eventsOf(alice); got != "Live:0+live Alice's:0" {
		t.Errorf("alice's snapshot once bob left: %s", got)
	}
	var files int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_files WHERE id = ?`, banner.ID).Scan(&files); err != nil || files != 0 {
		t.Errorf("the canceled event's banner stayed: %d, %v", files, err)
	}
	_ = live
}

// TestEventsGoWithTheirChannel checks a voice channel deleted: its events
// to come go, with their banners, and its live one ends.
func TestEventsGoWithTheirChannel(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	other := f.newChannel(alice, "other", denproto.ChannelRequest{Kind: denproto.KindVoice})
	live := f.makeEvent(alice, f.eventAt("Live", lounge.ID, time.Minute, time.Hour))
	banner := f.mustUpload(alice, "banner.png", testPNG(t, 900, 300))
	req := f.eventAt("Coming", lounge.ID, 2*time.Hour, time.Hour)
	req.Banner = &banner.ID
	coming := f.makeEvent(alice, req)
	elsewhere := f.makeEvent(alice, f.eventAt("Elsewhere", other.ID, 2*time.Hour, time.Hour))
	f.clock = f.clock.Add(time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	if err := f.d.DeleteChannel(ctx, alice, lounge.ID); err != nil {
		t.Fatal(err)
	}
	if got := eventsHeard(t, sub); !slices.Equal(got, []string{"event.canceled:" + coming.ID, "event.ended:" + live.ID}) {
		t.Errorf("as the lounge went bob heard %v", got)
	}
	if got := f.eventsOf(bob); got != "Elsewhere:0" {
		t.Errorf("bob's snapshot once the lounge went: %s", got)
	}
	var channel *int64
	var state string
	if err := f.db.QueryRow(`SELECT channel_id, state FROM den_events WHERE id = ?`, live.ID).Scan(&channel, &state); err != nil ||
		channel != nil || state != eventEnded {
		t.Errorf("the lounge's live event ended as %v in %v, %v", state, channel, err)
	}
	var files int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_files WHERE id = ?`, banner.ID).Scan(&files); err != nil || files != 0 {
		t.Errorf("the canceled event's banner stayed: %d, %v", files, err)
	}
	_ = elsewhere
}

// TestEventBanner checks an event's banner: an upload of its maker's,
// waiting to be used, three times as wide as high, as a profile's banner
// is; kept as the event uses it, which the member's own files say, past
// the hour unused uploads last; and deleted as another takes its place,
// or the event is canceled.
func TestEventBanner(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	square := f.mustUpload(alice, "square.png", testPNG(t, 300, 300))
	req := f.eventAt("Bannered", lounge.ID, 2*time.Hour, time.Hour)
	req.Banner = &square.ID
	_, err := f.d.CreateEvent(ctx, alice, req)
	wantCode(t, err, denproto.CodeInvalidField)
	bobs := f.mustUpload(bob, "bobs.png", testPNG(t, 900, 300))
	req.Banner = &bobs.ID
	_, err = f.d.CreateEvent(ctx, alice, req)
	wantCode(t, err, denproto.CodeInvalidField)

	first := f.mustUpload(alice, "first.png", testPNG(t, 900, 300))
	req.Banner = &first.ID
	e := f.makeEvent(alice, req)
	files, err := f.d.OwnFiles(ctx, alice, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := denproto.CheckOwnFiles(files); err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(files.Files, func(o denproto.OwnFile) bool { return o.ID == first.ID }); i < 0 || files.Files[i].Event != e.ID {
		t.Errorf("alice's files don't say the banner is the event's: %+v", files.Files)
	}
	// The sweep of uploads left unused passes it by.
	f.clock = f.clock.Add(uploadExpiry + time.Minute)
	f.d.sweepUploads()
	r, err := f.d.OpenFile(ctx, bob, first.ID, false)
	if err != nil {
		t.Fatalf("the banner went with the sweep: %v", err)
	}
	r.Close()

	second := f.mustUpload(alice, "second.gif", testGIF(t, 600, 200, 2, false))
	if _, err := f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{Banner: &second.ID}); err != nil {
		t.Fatal(err)
	}
	if opens(f, alice, first.ID) {
		t.Error("the banner another took the place of stayed")
	}
	if got, err := f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{Banner: ptr("")}); err != nil || got.Banner.ID != "" {
		t.Fatalf("taking the banner off: %+v, %v", got.Banner, err)
	}
	if opens(f, alice, second.ID) {
		t.Error("the banner taken off stayed")
	}
	third := f.mustUpload(alice, "third.png", testPNG(t, 900, 300))
	if _, err := f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{Banner: &third.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.CancelEvent(ctx, alice, e.ID); err != nil {
		t.Fatal(err)
	}
	if opens(f, alice, third.ID) {
		t.Error("the canceled event's banner stayed")
	}
}

// TestEventDetailsSealed checks the den keeps what members wrote of an
// event, its name, description and place, sealed with the data key.
func TestEventDetailsSealed(t *testing.T) {
	f, alice, _ := chatFixture(t)
	req := f.eventAt("Secret supper", "", time.Hour, time.Hour)
	req.ChannelID, req.Place, req.Description = nil, ptr("The back room"), ptr("Bring **cheese**")
	e := f.makeEvent(alice, req)
	if e.Description != "Bring **cheese**" || e.Place != "The back room" {
		t.Fatalf("the event came back as %+v", e)
	}
	var sealed []byte
	if err := f.db.QueryRow(`SELECT details FROM den_events WHERE id = ?`, e.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"Secret supper", "The back room", "cheese"} {
		if strings.Contains(string(sealed), plain) {
			t.Errorf("the den holds %q in the clear", plain)
		}
	}
	var d eventDetails
	plain, err := f.v.Open(sealed, eventAD(mustID(e.ID)))
	if err != nil || json.Unmarshal(plain, &d) != nil || d.Name != "Secret supper" {
		t.Errorf("the details open as %+v, %v", d, err)
	}
}
