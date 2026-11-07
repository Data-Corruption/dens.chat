package den

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/sfu"
)

// badged is a request for an event in a voice channel with a badge whose
// picture is an upload of s's.
func (f *fixture) badged(s *Session, name, channel string, ahead, length time.Duration) denproto.EventRequest {
	f.t.Helper()
	req := f.eventAt(name, channel, ahead, length)
	pic := f.mustUpload(s, "badge.png", testPNG(f.t, 128, 128))
	req.Badge, req.BadgeTitle = &pic.ID, ptr("Was there")
	return req
}

// startCalls runs calls on the loopback for a test.
func (f *fixture) startCalls() {
	f.t.Helper()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		f.t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.d.StartCalls(udp, tcp, sfu.Config{Loopback: true}); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = f.d.StopCalls() })
}

// inCall joins s's call in a voice channel on a socket of its own, at the
// fixture's clock, and returns the socket.
func (f *fixture) inCall(s *Session, channel string) *socket {
	f.t.Helper()
	sub, _, _, _ := f.d.Hub.Subscribe(s.MemberID, IsStaff(s.Role), "", 0)
	f.t.Cleanup(func() { f.d.Hub.Unsubscribe(sub) })
	sock := &socket{member: s.MemberID, keyID: s.KeyID, closeReq: make(chan closeRequest, 1), voiceOut: make(chan denproto.Event, 64)}
	f.d.joinCall(context.Background(), s, sock, sub, denproto.VoiceJoin{ChannelID: channel, Sealed: true})
	if c := f.d.socketCall(sock); c == nil {
		f.t.Fatalf("member %d isn't in the call", s.MemberID)
	}
	return sock
}

// awarded lists who earned an event's badge, and when, as member@ms past
// base.
func (f *fixture) awarded(event string, base time.Time) string {
	f.t.Helper()
	rows, err := f.db.Query(`SELECT member_id, earned_at FROM den_badge_awards WHERE event_id = ? ORDER BY member_id`, mustID(event))
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var member, at int64
		if err := rows.Scan(&member, &at); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, denproto.FormatID(member)+"@"+time.Duration((at-base.UnixMilli())*int64(time.Millisecond)).String())
	}
	return strings.Join(out, " ")
}

// dumpDen lists every table's rows but those named, as text, to compare.
func (f *fixture) dumpDen(except ...string) string {
	f.t.Helper()
	tables, err := database.Tables(context.Background(), f.db)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, table := range tables {
		if slices.Contains(except, table) {
			continue
		}
		rows, err := f.db.Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			f.t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			f.t.Fatal(err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				f.t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("%s: %q", table, vals))
		}
		rows.Close()
	}
	slices.Sort(out)
	return strings.Join(out, "\n")
}

// TestBadgeAttach checks staff attach a badge to an event in a voice
// channel, a picture checked as an emoji's is and a title of up to 40
// characters, and change it or take it off until the event ends; that a
// member who isn't staff can't; that a badge goes nowhere but a voice
// channel; and that a badge's picture is the den's, opening to everyone.
func TestBadgeAttach(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	if _, err := f.d.Update(ctx, alice, denproto.DenUpdateRequest{EventLimits: &denproto.EventLimits{MemberEvents: 2}}); err != nil {
		t.Fatal(err)
	}

	// Bob isn't staff: his event takes no badge from him.
	_, err := f.d.CreateEvent(ctx, bob, f.badged(bob, "Bob's", lounge.ID, time.Hour, time.Hour))
	wantCode(t, err, denproto.CodeForbidden)

	// A picture too large for an emoji, one that isn't alice's, and a
	// title that's too long are refused; so is a badge with no title, or
	// in no voice channel.
	req := f.eventAt("Game night", lounge.ID, time.Hour, time.Hour)
	big := f.mustUpload(alice, "big.png", testPNG(t, 300, 300))
	req.Badge, req.BadgeTitle = &big.ID, ptr("Was there")
	_, err = f.d.CreateEvent(ctx, alice, req)
	wantCode(t, err, denproto.CodeInvalidField)
	bobs := f.mustUpload(bob, "bobs.png", testPNG(t, 64, 64))
	req.Badge = &bobs.ID
	_, err = f.d.CreateEvent(ctx, alice, req)
	wantCode(t, err, denproto.CodeInvalidField)
	pic := f.mustUpload(alice, "badge.png", testPNG(t, 128, 128))
	req.Badge, req.BadgeTitle = &pic.ID, ptr(strings.Repeat("x", 41))
	_, err = f.d.CreateEvent(ctx, alice, req)
	wantCode(t, err, denproto.CodeInvalidField)
	req.BadgeTitle = nil
	_, err = f.d.CreateEvent(ctx, alice, req)
	wantCode(t, err, denproto.CodeInvalidField)
	elsewhere := req
	elsewhere.ChannelID, elsewhere.Place, elsewhere.BadgeTitle = nil, ptr("The park"), ptr("Was there")
	_, err = f.d.CreateEvent(ctx, alice, elsewhere)
	wantCode(t, err, denproto.CodeInvalidField)

	req.BadgeTitle = ptr("  Night   owl 🦉 ")
	e := f.makeEvent(alice, req)
	if e.Badge == nil || e.Badge.Title != "Night owl 🦉" || e.Badge.Image.ID != pic.ID || e.Badge.Image.Width != 128 {
		t.Fatalf("the event's badge came back as %+v", e.Badge)
	}
	if err := denproto.CheckDenEvent(e); err != nil {
		t.Fatal(err)
	}
	if !opens(f, bob, pic.ID) {
		t.Error("bob can't open the badge's picture")
	}
	// The picture is the den's now: the sweep of unused uploads passes it
	// by, and alice's storage doesn't count it.
	f.clock = f.clock.Add(uploadExpiry + time.Minute)
	f.d.sweepUploads()
	if !opens(f, bob, pic.ID) {
		t.Error("the badge's picture went with the sweep")
	}
	storage, err := f.d.Storage(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if storage.Used != 0 {
		t.Errorf("alice's storage counts %d bytes, the badge's picture among them", storage.Used)
	}
	var sealed []byte
	if err := f.db.QueryRow(`SELECT title FROM den_badges WHERE event_id = ?`, mustID(e.ID)).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "owl") {
		t.Error("the den holds the badge's title in the clear")
	}

	// Bob, the maker of none of it, changes nothing of it; alice changes
	// its title, then its picture, which takes the old one with it.
	_, err = f.d.UpdateEvent(ctx, bob, e.ID, denproto.EventRequest{BadgeTitle: ptr("Mine now")})
	wantCode(t, err, denproto.CodeForbidden)
	got, err := f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{BadgeTitle: ptr("Early bird")})
	if err != nil || got.Badge == nil || got.Badge.Title != "Early bird" || got.Badge.Image.ID != pic.ID {
		t.Fatalf("a new title: %+v, %v", got.Badge, err)
	}
	moving := f.mustUpload(alice, "moving.gif", testGIF(t, 96, 96, 2, false))
	got, err = f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{Badge: &moving.ID})
	if err != nil || got.Badge == nil || got.Badge.Image.ID != moving.ID || !got.Badge.Image.Animated || got.Badge.Title != "Early bird" {
		t.Fatalf("a new picture: %+v, %v", got.Badge, err)
	}
	if opens(f, alice, pic.ID) {
		t.Error("the picture another took the place of stayed")
	}

	// An event with a badge stays in a voice channel, unless the badge
	// comes off with the move.
	_, err = f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{Place: ptr("The park")})
	wantCode(t, err, denproto.CodeInvalidField)
	got, err = f.d.UpdateEvent(ctx, alice, e.ID, denproto.EventRequest{Place: ptr("The park"), Badge: ptr("")})
	if err != nil || got.Badge != nil || got.Place != "The park" {
		t.Fatalf("taking the badge off as it moves: %+v, %v", got, err)
	}
	if opens(f, alice, moving.ID) {
		t.Error("the badge taken off kept its picture")
	}
	var named int
	if err := f.db.QueryRow(`SELECT count(*) FROM den_named_files WHERE kind = 'badge'`).Scan(&named); err != nil || named != 0 {
		t.Errorf("the den keeps %d badges' pictures, %v", named, err)
	}

	// Canceling an event takes its badge.
	other := f.makeEvent(alice, f.badged(alice, "Other", lounge.ID, 2*time.Hour, time.Hour))
	if err := f.d.CancelEvent(ctx, alice, other.ID); err != nil {
		t.Fatal(err)
	}
	if opens(f, alice, other.Badge.Image.ID) {
		t.Error("the canceled event's badge kept its picture")
	}
}

// TestBadgeEarned checks a member earns a live event's badge by a minute
// at a stretch in its call, counting from its start for one there before,
// their call held or not; that one who left sooner, or whose minute comes
// only after the end, doesn't; that a badge is earned once; and that the
// den writes the award and nothing else of the call.
func TestBadgeEarned(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(alice, "carol")
	dave := f.member(alice, "dave")
	erin := f.member(alice, "erin")
	f.startCalls()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	e := f.makeEvent(alice, f.badged(alice, "Game night", lounge.ID, time.Minute, 10*time.Minute))
	start := f.clock.Add(time.Minute)

	// Alice is there before it starts, and counts from its start.
	f.inCall(alice, lounge.ID)
	f.clock = start
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	before := f.dumpDen("den_badge_awards")
	if at := f.d.nextAward(ctx); at != start.Add(time.Minute).UnixMilli() {
		t.Errorf("the next badge comes due at %d, not a minute past the start", at)
	}

	// Bob joins 10 seconds in and leaves 50 seconds later; carol joins 30
	// seconds in and her call is held at 50.
	f.clock = start.Add(10 * time.Second)
	bobs := f.inCall(bob, lounge.ID)
	f.clock = start.Add(30 * time.Second)
	carols := f.inCall(carol, lounge.ID)
	f.clock = start.Add(50 * time.Second)
	f.d.socketClosed(carols)
	f.clock = start.Add(59 * time.Second)
	f.d.leaveCall(bobs)
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.awarded(e.ID, start); got != "" {
		t.Errorf("before anyone's minute, the badge went to %s", got)
	}
	f.clock = start.Add(90 * time.Second)
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}
	want := alice.memberID() + "@1m0s " + carol.memberID() + "@1m30s"
	if got := f.awarded(e.ID, start); got != want {
		t.Errorf("at 1m30s the badge went to %s, want %s", got, want)
	}
	// Once each, though they stay.
	f.clock = start.Add(5 * time.Minute)
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.awarded(e.ID, start); got != want {
		t.Errorf("at 5m the badge went to %s, want %s", got, want)
	}
	if after := f.dumpDen("den_badge_awards"); after != before {
		t.Errorf("the calls left more than the awards behind: before\n%s\nafter\n%s", before, after)
	}

	// Dave joins 30 seconds before the end, erin a minute and a half
	// before; the timer comes late, after the end.
	f.clock = start.Add(9*time.Minute + 30*time.Second)
	f.inCall(dave, lounge.ID)
	f.clock = start.Add(8*time.Minute + 30*time.Second)
	f.inCall(erin, lounge.ID)
	f.clock = start.Add(11 * time.Minute)
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.d.endDueEvents(ctx); err != nil {
		t.Fatal(err)
	}
	want += " " + erin.memberID() + "@9m30s"
	if got := f.awarded(e.ID, start); got != want {
		t.Errorf("after the end the badge went to %s, want %s", got, want)
	}
	if at := f.d.nextAward(ctx); at != 0 {
		t.Errorf("after the end a badge comes due at %d", at)
	}
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.awarded(e.ID, start); got != want {
		t.Errorf("a pass after the end gave the badge to %s", got)
	}
}

func (s *Session) memberID() string { return denproto.FormatID(s.MemberID) }

// TestBadgesOnProfiles checks a profile carries the badges its member
// earned, latest first, each with its event's name and start, and only
// those whose events the asker sees: a staff-only event's badge, and its
// picture, are staff's alone, even once its channel is gone.
func TestBadgesOnProfiles(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	f.startCalls()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	office := f.newChannel(alice, "office", denproto.ChannelRequest{Kind: denproto.KindVoice, StaffOnly: ptr(true)})
	open := f.makeEvent(alice, f.badged(alice, "Game night", lounge.ID, time.Minute, 10*time.Minute))
	staff := f.makeEvent(alice, f.badged(alice, "Staff meeting", office.ID, 20*time.Minute, 10*time.Minute))

	f.clock = f.clock.Add(time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	f.inCall(alice, lounge.ID)
	f.clock = f.clock.Add(2 * time.Minute)
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(18 * time.Minute)
	if err := f.d.endDueEvents(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	f.inCall(alice, office.ID)
	f.clock = f.clock.Add(2 * time.Minute)
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}

	badges := func(viewer *Session) string {
		t.Helper()
		p, err := f.d.Profile(ctx, viewer, alice.memberID())
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, b := range p.Badges {
			if err := denproto.CheckMemberBadge(b); err != nil {
				t.Errorf("a badge unfit to show: %v", err)
			}
			out = append(out, b.Event+":"+b.Title)
		}
		return strings.Join(out, " ")
	}
	if got := badges(alice); got != "Staff meeting:Was there Game night:Was there" {
		t.Errorf("alice sees her badges as %s", got)
	}
	if got := badges(bob); got != "Game night:Was there" {
		t.Errorf("bob sees alice's badges as %s", got)
	}
	p, _ := f.d.Profile(ctx, bob, alice.memberID())
	if len(p.Badges) != 1 || p.Badges[0].StartsAt != open.StartsAt || p.Badges[0].EventID != open.ID {
		t.Errorf("bob's view of the badge: %+v", p.Badges)
	}
	if !opens(f, bob, open.Badge.Image.ID) || opens(f, bob, staff.Badge.Image.ID) || !opens(f, alice, staff.Badge.Image.ID) {
		t.Error("the badges' pictures open to others than those who see their events")
	}

	// The staff-only channel goes; its event, ended, stays staff's.
	f.clock = f.clock.Add(10 * time.Minute)
	if err := f.d.DeleteChannel(ctx, alice, office.ID); err != nil {
		t.Fatal(err)
	}
	if got := badges(bob); got != "Game night:Was there" {
		t.Errorf("once the office went bob sees alice's badges as %s", got)
	}
	if opens(f, bob, staff.Badge.Image.ID) {
		t.Error("once the office went bob opens its badge's picture")
	}
	if got := badges(alice); got != "Staff meeting:Was there Game night:Was there" {
		t.Errorf("once the office went alice sees her badges as %s", got)
	}
}

// TestBadgeHidden checks a member hides a badge they earned from their
// card and shows it again (M17.6): their own profile carries it marked,
// everyone else's copy leaves it out, the den keeps the mark alone and
// tells nobody, and a badge they didn't earn, or whose event they no
// longer see, is no such badge.
func TestBadgeHidden(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	carol := f.member(alice, "carol")
	if _, err := f.d.SetRole(ctx, alice, carol.memberID(), denproto.RoleRequest{Role: denproto.RoleModerator}); err != nil {
		t.Fatal(err)
	}
	carol.Role = denproto.RoleModerator
	f.startCalls()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	office := f.newChannel(alice, "office", denproto.ChannelRequest{Kind: denproto.KindVoice, StaffOnly: ptr(true)})
	night := f.makeEvent(alice, f.badged(alice, "Game night", lounge.ID, time.Minute, 10*time.Minute))
	meeting := f.makeEvent(alice, f.badged(alice, "Staff meeting", office.ID, time.Minute, 10*time.Minute))
	f.clock = f.clock.Add(time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	f.inCall(bob, lounge.ID)
	f.inCall(carol, office.ID)
	f.clock = f.clock.Add(2 * time.Minute)
	if err := f.d.awardDue(ctx); err != nil {
		t.Fatal(err)
	}

	badges := func(viewer, of *Session) string {
		t.Helper()
		p, err := f.d.Profile(ctx, viewer, of.memberID())
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, b := range p.Badges {
			mark := ""
			if b.Hidden {
				mark = " (hidden)"
			}
			out = append(out, b.Event+mark)
		}
		return strings.Join(out, ", ")
	}
	notFound := func(err error, what string) {
		t.Helper()
		var perr *denproto.Error
		if !errors.As(err, &perr) || perr.Status != http.StatusNotFound || perr.Code != denproto.CodeNotFound {
			t.Errorf("%s: %v, not 404 not_found", what, err)
		}
	}

	alices, _, _, _ := f.d.Hub.Subscribe(alice.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(alices)
	drain(alices)
	before := f.dumpDen("den_badge_awards")
	if err := f.d.SetBadgeHidden(ctx, bob, night.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := badges(bob, bob); got != "Game night (hidden)" {
		t.Errorf("bob's own profile carries his badges as %q", got)
	}
	for _, viewer := range []*Session{alice, carol} {
		if got := badges(viewer, bob); got != "" {
			t.Errorf("member %d sees bob's hidden badge: %q", viewer.MemberID, got)
		}
	}
	if err := f.d.SetBadgeHidden(ctx, bob, night.ID, true); err != nil {
		t.Errorf("hiding a hidden badge again: %v", err)
	}
	if after := f.dumpDen("den_badge_awards"); after != before {
		t.Errorf("hiding a badge changed more than its award: before\n%s\nafter\n%s", before, after)
	}
	if got := drain(alices); len(got) != 0 {
		t.Errorf("hiding a badge told alice %v", got)
	}
	if err := f.d.SetBadgeHidden(ctx, bob, night.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, other := badges(bob, bob), badges(alice, bob); got != "Game night" || other != "Game night" {
		t.Errorf("shown again, bob's badges read %q to him and %q to alice", got, other)
	}

	notFound(f.d.SetBadgeHidden(ctx, bob, meeting.ID, true), "bob hiding a badge he didn't earn")
	notFound(f.d.SetBadgeHidden(ctx, alice, night.ID, true), "alice hiding a badge she didn't earn")
	notFound(f.d.SetBadgeHidden(ctx, bob, "x", true), "a malformed event ID")
	notFound(f.d.SetBadgeHidden(ctx, bob, "99", true), "an event that isn't")

	// Carol hides the staff meeting's badge while she's staff; once she
	// isn't, it's no badge of hers to show or hide, and nobody sees it.
	if err := f.d.SetBadgeHidden(ctx, carol, meeting.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := badges(carol, carol); got != "Staff meeting (hidden)" {
		t.Errorf("carol's own profile carries her badges as %q", got)
	}
	if got := badges(alice, carol); got != "" {
		t.Errorf("alice sees carol's hidden badge: %q", got)
	}
	if _, err := f.d.SetRole(ctx, alice, carol.memberID(), denproto.RoleRequest{Role: denproto.RoleMember}); err != nil {
		t.Fatal(err)
	}
	carol.Role = denproto.RoleMember
	notFound(f.d.SetBadgeHidden(ctx, carol, meeting.ID, false), "carol showing a staff-only badge once she isn't staff")
	if got := badges(carol, carol); got != "" {
		t.Errorf("once she isn't staff carol's own profile carries %q", got)
	}
}

// TestEventByID checks a link's card reads an event by its ID: one to
// come or live with the member's own mark, an ended one saying so with
// nobody interested, and one the member can't see, or that's gone, not at
// all, a staff-only one even once its channel is gone, while one in a
// channel everyone saw stays everyone's.
func TestEventByID(t *testing.T) {
	f, alice, bob := chatFixture(t)
	ctx := context.Background()
	lounge := f.newChannel(alice, "lounge", denproto.ChannelRequest{Kind: denproto.KindVoice})
	office := f.newChannel(alice, "office", denproto.ChannelRequest{Kind: denproto.KindVoice, StaffOnly: ptr(true)})
	coming := f.makeEvent(alice, f.badged(alice, "Game night", lounge.ID, time.Hour, time.Hour))
	staff := f.makeEvent(alice, f.eventAt("Staff meeting", office.ID, time.Minute, 10*time.Minute))
	ended := f.makeEvent(alice, f.eventAt("Over", lounge.ID, time.Minute, 10*time.Minute))
	if _, err := f.d.SetInterest(ctx, bob, coming.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.SetInterest(ctx, bob, ended.ID, true); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Minute)
	if err := f.d.startDue(ctx); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(10 * time.Minute)
	if err := f.d.endDueEvents(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := f.d.Event(ctx, bob, coming.ID)
	if err != nil || got.Mine == nil || !*got.Mine || got.Interested != 1 || got.Ended || got.Badge == nil {
		t.Errorf("bob reads the event to come as %+v, %v", got, err)
	}
	got, err = f.d.Event(ctx, bob, ended.ID)
	if err != nil || !got.Ended || got.Live || got.Interested != 0 || got.Mine == nil || *got.Mine {
		t.Errorf("bob reads the ended event as %+v, %v", got, err)
	}
	if err := denproto.CheckDenEvent(got); err != nil {
		t.Errorf("the ended event is unfit to show: %v", err)
	}
	_, err = f.d.Event(ctx, bob, staff.ID)
	wantCode(t, err, denproto.CodeNotFound)
	if got, err := f.d.Event(ctx, alice, staff.ID); err != nil || !got.Ended {
		t.Errorf("alice reads the staff meeting as %+v, %v", got, err)
	}
	_, err = f.d.Event(ctx, bob, "9999")
	wantCode(t, err, denproto.CodeNotFound)
	_, err = f.d.Event(ctx, bob, "x")
	wantCode(t, err, denproto.CodeNotFound)

	// Both channels go: the ended events stay, each as it was seen.
	for _, c := range []denproto.Channel{office, lounge} {
		if err := f.d.DeleteChannel(ctx, alice, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.d.Event(ctx, bob, staff.ID)
	wantCode(t, err, denproto.CodeNotFound)
	got, err = f.d.Event(ctx, bob, ended.ID)
	if err != nil || !got.Ended || got.ChannelID != "" || got.Place != "" {
		t.Errorf("once its channel went bob reads the ended event as %+v, %v", got, err)
	}
	if err := denproto.CheckDenEvent(got); err != nil {
		t.Errorf("the ended event with no place is unfit to show: %v", err)
	}
	if got, err := f.d.Event(ctx, alice, staff.ID); err != nil || !got.Ended {
		t.Errorf("once its channel went alice reads the staff meeting as %+v, %v", got, err)
	}
	// The event to come went with its channel, badge and all.
	_, err = f.d.Event(ctx, bob, coming.ID)
	wantCode(t, err, denproto.CodeNotFound)
	if opens(f, alice, coming.Badge.Image.ID) {
		t.Error("the canceled event's badge kept its picture")
	}
}
