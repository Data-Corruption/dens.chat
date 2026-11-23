package den

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// fakeClock has the den read the time from a clock the test moves.
func (f *fixture) fakeClock(start time.Time) *atomic.Int64 {
	var now atomic.Int64
	now.Store(start.UnixNano())
	f.d.SetClock(func() time.Time { return time.Unix(0, now.Load()) })
	return &now
}

// auditLines reads the whole log as token's member, page by page,
// newest first, and fails on a line the client would refuse.
func (f *fixture) auditLines(token denproto.Bytes) []denproto.AuditLine {
	f.t.Helper()
	var lines []denproto.AuditLine
	path := "/api/audit"
	for {
		var page denproto.AuditLog
		if status := f.call(http.MethodGet, path, token, nil, &page); status != http.StatusOK {
			f.t.Fatalf("GET %s: %d", path, status)
		}
		if len(page.Lines) > denproto.AuditPage {
			f.t.Fatalf("a page of %d lines", len(page.Lines))
		}
		for _, l := range page.Lines {
			if err := denproto.CheckAuditLine(l); err != nil {
				f.t.Fatalf("line %+v: %v", l, err)
			}
		}
		lines = append(lines, page.Lines...)
		if page.Next == "" {
			return lines
		}
		path = "/api/audit?before=" + page.Next
	}
}

// audit reads the log's lines whose kinds start with prefix, as text:
// the kind, who acted, on whom, where and what, with members by username.
func (f *fixture) audit(token denproto.Bytes, prefix string) []string {
	f.t.Helper()
	name := func(id string) string {
		m, err := f.d.Member(context.Background(), mustID(id))
		if err != nil {
			f.t.Fatal(err)
		}
		return m.Username
	}
	var out []string
	for _, l := range f.auditLines(token) {
		if !strings.HasPrefix(l.Kind, prefix) {
			continue
		}
		parts := []string{l.Kind}
		if l.By != "" {
			parts = append(parts, "by "+name(l.By))
		}
		if l.MemberID != "" {
			parts = append(parts, "on "+name(l.MemberID))
		}
		if l.ChannelID != "" {
			parts = append(parts, "#"+l.Channel+"="+l.ChannelID)
		}
		if l.Ref != "" {
			parts = append(parts, "ref "+l.Ref)
		}
		if l.Name != "" {
			parts = append(parts, "name "+l.Name)
		}
		for _, c := range l.Changes {
			parts = append(parts, c.Field+":"+c.From+">"+c.To)
		}
		if l.Ban {
			parts = append(parts, "ban")
		}
		if l.DeleteMessages != 0 {
			parts = append(parts, fmt.Sprint("delete ", l.DeleteMessages))
		}
		if l.RevokeInvite {
			parts = append(parts, "invite")
		}
		if l.Emoji != "" {
			parts = append(parts, "emoji "+l.Emoji)
		}
		if l.Owner != "" {
			parts = append(parts, "owner "+name(l.Owner))
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

func wantLines(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("the log reads, newest first:\n  %s\nnot\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestStaffActsWriteTheLog checks that each of staff's acts in the den
// writes its line, which says who, what, on whom or where, with names as
// they were then and values before and after, and never a message's
// text; what changes nothing writes none. A line names a member who
// left, and reads after the channel it was about goes. A name as it was
// is sealed: the table's bytes hold none.
func TestStaffActsWriteTheLog(t *testing.T) {
	f := newFixture(t)
	f.d.RelaxLimits()
	f.d.TokenLifetime = 24 * time.Hour
	clock := f.fakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	now := func() time.Time { return time.Unix(0, clock.Load()) }
	alice, _ := f.owner()
	bob, carol, dave := f.member(alice, "bob"), f.member(alice, "carol"), f.member(alice, "dave")
	id := func(token denproto.Bytes) string { return denproto.FormatID(f.session(token).MemberID) }
	do := func(token denproto.Bytes, method, path string, body any, want int, out any) {
		t.Helper()
		if status := f.call(method, path, token, body, out); status != want {
			t.Fatalf("%s %s: %d, not %d", method, path, status, want)
		}
	}
	str := func(s string) *string { return &s }
	yes, no := true, false

	do(alice, http.MethodPatch, "/api/members/"+id(carol), denproto.RoleRequest{Role: denproto.RoleModerator}, http.StatusOK, nil)
	var inv denproto.Invite
	do(carol, http.MethodPost, "/api/invites", denproto.InviteCreateRequest{}, http.StatusCreated, &inv)
	do(carol, http.MethodDelete, "/api/invites/"+inv.ID, nil, http.StatusNoContent, nil)

	var games denproto.Group
	do(carol, http.MethodPost, "/api/groups", denproto.GroupRequest{Name: str("Games")}, http.StatusCreated, &games)
	var general, lounge denproto.Channel
	do(carol, http.MethodPost, "/api/channels", denproto.ChannelRequest{Name: str("general-marker")}, http.StatusCreated, &general)
	do(alice, http.MethodPost, "/api/channels", denproto.ChannelRequest{Name: str("Lounge"), Kind: denproto.KindVoice}, http.StatusCreated, &lounge)
	do(carol, http.MethodPatch, "/api/channels/"+general.ID, denproto.ChannelRequest{Name: str("chat"), Description: str("Talk here"),
		StaffOnly: &yes, GroupID: &games.ID}, http.StatusOK, nil)
	zero := 0
	do(carol, http.MethodPatch, "/api/channels/"+general.ID, denproto.ChannelRequest{Position: &zero}, http.StatusOK, nil)
	do(carol, http.MethodPatch, "/api/channels/"+general.ID, denproto.ChannelRequest{StaffOnly: &no, Name: str("chat")}, http.StatusOK, nil)
	bitrate := 64_000
	do(alice, http.MethodPatch, "/api/channels/"+lounge.ID, denproto.ChannelRequest{Bitrate: &bitrate}, http.StatusOK, nil)
	do(carol, http.MethodPatch, "/api/groups/"+games.ID, denproto.GroupRequest{Name: str("Play")}, http.StatusOK, nil)
	do(carol, http.MethodPatch, "/api/groups/"+games.ID, denproto.GroupRequest{Position: &zero}, http.StatusOK, nil)

	send := func(token denproto.Bytes, text string) denproto.Message {
		t.Helper()
		var m denproto.Message
		do(token, http.MethodPost, "/api/channels/"+general.ID+"/messages", denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Text: text},
			http.StatusCreated, &m)
		return m
	}
	secret := send(bob, "words-to-keep-out-of-the-log")
	own := send(dave, "dave's own words")
	do(dave, http.MethodDelete, "/api/messages/"+own.ID, nil, http.StatusNoContent, nil)
	do(carol, http.MethodDelete, "/api/messages/"+secret.ID, nil, http.StatusNoContent, nil)
	staffs := send(carol, "carol's own words")
	do(carol, http.MethodDelete, "/api/messages/"+staffs.ID, nil, http.StatusNoContent, nil)
	reacted := send(bob, "react to this")
	do(dave, http.MethodPost, "/api/messages/"+reacted.ID+"/reactions", denproto.ReactionRequest{Emoji: "👍"}, http.StatusNoContent, nil)
	do(carol, http.MethodPost, "/api/messages/"+reacted.ID+"/reactions/clear", denproto.ReactionRequest{Emoji: "👍"}, http.StatusNoContent, nil)
	do(carol, http.MethodPost, "/api/messages/"+reacted.ID+"/reactions/clear", denproto.ReactionRequest{Emoji: "🎉"}, http.StatusNoContent, nil)

	png := emojiPNG(t, 64, 64)
	resp := f.upload(alice, "emoji.png", bytes.NewReader(png), int64(len(png)))
	var up denproto.File
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&up) != nil {
		t.Fatalf("upload an emoji: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var wave denproto.Emoji
	do(alice, http.MethodPost, "/api/emoji", denproto.EmojiRequest{Name: "wave", FileID: up.ID}, http.StatusCreated, &wave)
	do(carol, http.MethodPatch, "/api/emoji/"+wave.ID, denproto.EmojiUpdate{Name: "hello"}, http.StatusOK, nil)
	do(carol, http.MethodDelete, "/api/emoji/"+wave.ID, nil, http.StatusNoContent, nil)
	horn := f.addSound(alice, "Horn", 5)
	do(carol, http.MethodPatch, "/api/soundboard/"+horn.ID, denproto.SoundUpdate{Name: "Honk"}, http.StatusOK, nil)
	do(carol, http.MethodDelete, "/api/soundboard/"+horn.ID, nil, http.StatusNoContent, nil)

	retention := 30
	update := denproto.DenUpdateRequest{Name: str("Den 2"), Retention: &retention, EventLimits: &denproto.EventLimits{MemberEvents: 2}}
	do(alice, http.MethodPatch, "/api/den", update, http.StatusOK, nil)
	do(alice, http.MethodPatch, "/api/den", update, http.StatusOK, nil)

	// A member's own events write nothing, and staff's changes to them do.
	at := func(d time.Duration) *int64 { v := now().Add(d).UnixMilli(); return &v }
	var night, late denproto.DenEvent
	do(bob, http.MethodPost, "/api/events", denproto.EventRequest{Name: str("Bob's night"), StartsAt: at(time.Hour), EndsAt: at(2 * time.Hour),
		Place: str("The park")}, http.StatusCreated, &night)
	do(bob, http.MethodPatch, "/api/events/"+night.ID, denproto.EventRequest{Place: str("The pier")}, http.StatusOK, nil)
	do(carol, http.MethodPatch, "/api/events/"+night.ID, denproto.EventRequest{Name: str("Quiz"), StartsAt: at(90 * time.Minute),
		EndsAt: at(3 * time.Hour), Place: str("The beach")}, http.StatusOK, nil)
	do(carol, http.MethodDelete, "/api/events/"+night.ID, nil, http.StatusNoContent, nil)
	do(bob, http.MethodPost, "/api/events", denproto.EventRequest{Name: str("Late show 👨\u200d👩\u200d👧"), StartsAt: at(time.Hour), EndsAt: at(3 * time.Hour),
		Place: str("The park")}, http.StatusCreated, &late)
	clock.Add(int64(time.Hour))
	f.d.StartTimer()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var e denproto.DenEvent
		if do(bob, http.MethodGet, "/api/events/"+late.ID, nil, http.StatusOK, &e); e.Live {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the late show never started")
		}
	}
	f.d.StopTimer()
	do(carol, http.MethodPost, "/api/events/"+late.ID+"/end", nil, http.StatusNoContent, nil)

	// A record reset writes its line where there was one.
	if _, err := f.db.Exec(`UPDATE den_channels SET call_record = 5000 WHERE id = ?`, mustID(lounge.ID)); err != nil {
		t.Fatal(err)
	}
	do(carol, http.MethodDelete, "/api/channels/"+lounge.ID+"/record", nil, http.StatusNoContent, nil)
	do(carol, http.MethodDelete, "/api/channels/"+lounge.ID+"/record", nil, http.StatusNoContent, nil)

	daveID, bobID := id(dave), id(bob)
	do(alice, http.MethodPost, "/api/members/"+daveID+"/remove", denproto.RemoveRequest{Ban: true, DeleteMessages: 3600, RevokeInvite: true},
		http.StatusNoContent, nil)
	do(carol, http.MethodDelete, "/api/bans/"+daveID, nil, http.StatusForbidden, nil)
	do(alice, http.MethodDelete, "/api/bans/"+daveID, nil, http.StatusNoContent, nil)
	do(carol, http.MethodPost, "/api/members/"+bobID+"/remove", denproto.RemoveRequest{}, http.StatusNoContent, nil)
	// Removing him again changes nothing, and banning him does.
	do(alice, http.MethodPost, "/api/members/"+bobID+"/remove", denproto.RemoveRequest{}, http.StatusNoContent, nil)
	do(alice, http.MethodPost, "/api/members/"+bobID+"/remove", denproto.RemoveRequest{Ban: true}, http.StatusNoContent, nil)
	do(carol, http.MethodDelete, "/api/channels/"+general.ID, nil, http.StatusNoContent, nil)
	do(carol, http.MethodDelete, "/api/groups/"+games.ID, nil, http.StatusNoContent, nil)

	// The den changes hands by an owner invite, which its machine made.
	code, err := f.d.OwnerInvite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	info, _ := f.d.Info()
	ch, err := f.d.Challenge(denproto.Random(denproto.NonceSize))
	if err != nil {
		t.Fatal(err)
	}
	erin, err := f.d.Join(context.Background(), denproto.JoinRequest{
		Invite: code, Username: "erin", DisplayName: "Erin", Verifier: denproto.Random(32),
		PublicKey: denproto.Bytes(key.Public().(ed25519.PublicKey)), DeviceLabel: "test",
		Nonce: ch.Nonce, Proof: denproto.Prove(key, info.ID, ch.Nonce), SealCheck: denproto.Random(denproto.SealCheckSize),
	})
	if err != nil {
		t.Fatal(err)
	}

	gc, lc := "#chat="+general.ID, "#Lounge="+lounge.ID
	wantLines(t, f.audit(erin.Token, ""), []string{
		"role.changed on alice role:owner>moderator owner erin",
		"group.deleted by carol ref " + games.ID + " name Play",
		"channel.deleted by carol " + gc,
		"member.removed by alice on bob ban",
		"member.removed by carol on bob",
		"ban.lifted by alice on dave",
		"member.removed by alice on dave ban delete 3600 invite",
		"record.reset by carol " + lc,
		"event.ended by carol on bob ref " + late.ID + " name Late show 👨\u200d👩\u200d👧",
		"event.canceled by carol on bob ref " + night.ID + " name Quiz",
		fmt.Sprintf("event.updated by carol on bob ref %s name Quiz name:Bob's night>Quiz place:The pier>The beach starts_at:%d>%d ends_at:%d>%d",
			night.ID, night.StartsAt, night.StartsAt+30*60_000, night.EndsAt, night.EndsAt+3600_000),
		"den.updated by alice name:Den>Den 2 retention:0>30 member_events:0>2",
		"sound.deleted by carol ref " + horn.ID + " name Honk",
		"sound.renamed by carol ref " + horn.ID + " name Honk name:Horn>Honk",
		"sound.added by alice ref " + horn.ID + " name Horn",
		"emoji.deleted by carol ref " + wave.ID + " name hello",
		"emoji.renamed by carol ref " + wave.ID + " name hello name:wave>hello",
		"emoji.added by alice ref " + wave.ID + " name wave",
		"reactions.cleared by carol on bob " + gc + " ref " + reacted.ID + " emoji 👍",
		"message.deleted by carol on bob " + gc,
		"group.updated by carol ref " + games.ID + " name Play name:Games>Play",
		"channel.updated by alice " + lc + " bitrate:96000>64000",
		"channel.updated by carol " + gc + " staff_only:true>false",
		"channel.updated by carol " + gc + " name:general-marker>chat description:> staff_only:false>true group:>Games",
		"channel.created by alice " + lc,
		"channel.created by carol #general-marker=" + general.ID,
		"group.created by carol ref " + games.ID + " name Games",
		"invite.revoked by carol ref " + inv.ID,
		"invite.created by carol ref " + inv.ID,
		"role.changed by alice on carol role:member>moderator",
		// The den's own invite, which made alice its owner, is the first.
		"invite.created by alice ref 4",
		"invite.created by alice ref 3",
		"invite.created by alice ref 2",
	})

	// What the lines say of names is sealed, and no line holds a
	// message's text.
	rows, err := f.db.Query(`SELECT kind, details FROM den_audit`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var details []byte
		if err := rows.Scan(&kind, &details); err != nil {
			t.Fatal(err)
		}
		for _, plain := range []string{"general-marker", "Games", "Horn", "wave", "Quiz", "Den 2"} {
			if bytes.Contains(details, []byte(plain)) {
				t.Errorf("a %s line keeps %q unsealed", kind, plain)
			}
		}
	}
	for _, l := range f.auditLines(erin.Token) {
		if !slices.Contains(denproto.AuditKinds, l.Kind) {
			t.Errorf("a line of kind %s, which denproto doesn't list", l.Kind)
		}
	}
	raw, _ := json.Marshal(f.auditLines(erin.Token))
	if bytes.Contains(raw, []byte("words-to-keep-out-of-the-log")) {
		t.Error("a line holds a message's text")
	}
}

// TestNoLineForMembersOrDMs checks that what members do write no line, a
// member's own deletion among them, and nothing in a DM does: staff have
// no powers there, and a member's DM is theirs.
func TestNoLineForMembersOrDMs(t *testing.T) {
	f := newFixture(t)
	alice, _ := f.owner()
	bob := f.member(alice, "bob")
	general := f.channel(f.session(alice), "general", denproto.KindText, false)
	before := f.audit(alice, "")
	var m denproto.Message
	if status := f.call(http.MethodPost, "/api/channels/"+general.ID+"/messages", bob,
		denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Text: "mine"}, &m); status != http.StatusCreated {
		t.Fatalf("bob's message: %d", status)
	}
	if status := f.call(http.MethodDelete, "/api/messages/"+m.ID, bob, nil, nil); status != http.StatusNoContent {
		t.Fatalf("bob deleting his own: %d", status)
	}
	var dm denproto.Channel
	if status := f.call(http.MethodPost, "/api/dms", bob, denproto.DMRequest{MemberID: denproto.FormatID(f.session(alice).MemberID)}, &dm); status != http.StatusOK {
		t.Fatalf("open a DM: %d", status)
	}
	k := f.signedKey(dm, bob, alice)
	sealed := func(token denproto.Bytes) denproto.Message {
		var m denproto.Message
		if status := f.call(http.MethodPost, "/api/channels/"+dm.ID+"/messages", token,
			denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Sealed: denproto.Random(64), KeyID: k.ID}, &m); status != http.StatusCreated {
			t.Fatalf("a DM's message: %d", status)
		}
		return m
	}
	bobs := sealed(bob)
	if status := f.call(http.MethodDelete, "/api/messages/"+bobs.ID, alice, nil, nil); status != http.StatusForbidden {
		t.Fatalf("the owner deleting bob's DM message: %d", status)
	}
	if status := f.call(http.MethodPost, "/api/messages/"+bobs.ID+"/reactions/clear", alice, denproto.ReactionRequest{Emoji: "👍"}, nil); status != http.StatusForbidden {
		t.Fatalf("the owner clearing reactions in a DM: %d", status)
	}
	for _, token := range []denproto.Bytes{bob, alice} {
		m := sealed(token)
		if status := f.call(http.MethodDelete, "/api/messages/"+m.ID, token, nil, nil); status != http.StatusNoContent {
			t.Fatalf("deleting one's own DM message: %d", status)
		}
	}
	if status := f.call(http.MethodPost, "/api/me/leave", bob, nil, nil); status != http.StatusNoContent {
		t.Fatalf("bob leaving: %d", status)
	}
	wantLines(t, f.audit(alice, ""), before)
}

// TestOnlyStaffReadTheLog checks that moderators and the owner read the
// log a page at a time, newest first, and a member is refused.
func TestOnlyStaffReadTheLog(t *testing.T) {
	f := newFixture(t)
	f.d.RelaxLimits()
	alice, _ := f.owner()
	bob, carol := f.member(alice, "bob"), f.member(alice, "carol")
	if status := f.call(http.MethodPatch, "/api/members/"+denproto.FormatID(f.session(carol).MemberID), alice,
		denproto.RoleRequest{Role: denproto.RoleModerator}, nil); status != http.StatusOK {
		t.Fatalf("making carol a moderator: %d", status)
	}
	for range denproto.AuditPage + 5 {
		if status := f.call(http.MethodPost, "/api/invites", carol, denproto.InviteCreateRequest{}, nil); status != http.StatusCreated {
			t.Fatalf("an invite: %d", status)
		}
	}
	wantError(t, f.withToken(http.MethodGet, "/api/audit", bob), http.StatusForbidden, denproto.CodeForbidden)
	for _, token := range []denproto.Bytes{alice, carol} {
		var first denproto.AuditLog
		if status := f.call(http.MethodGet, "/api/audit", token, nil, &first); status != http.StatusOK || len(first.Lines) != denproto.AuditPage ||
			first.Next != first.Lines[len(first.Lines)-1].ID {
			t.Fatalf("the first page: %d, %d lines, next %q", status, len(first.Lines), first.Next)
		}
		all := f.auditLines(token)
		// Two invites made by the fixture, a role, and carol's invites.
		if len(all) != denproto.AuditPage+8 || all[0].Kind != denproto.AuditInviteCreated || all[len(all)-1].Kind != denproto.AuditInviteCreated {
			t.Fatalf("the log holds %d lines", len(all))
		}
		for i := 1; i < len(all); i++ {
			if mustID(all[i].ID) >= mustID(all[i-1].ID) {
				t.Fatalf("line %s comes after line %s", all[i].ID, all[i-1].ID)
			}
		}
	}
	wantError(t, f.withToken(http.MethodGet, "/api/audit?before=x", alice), http.StatusBadRequest, denproto.CodeInvalidField)
}

// withToken sends a request with no body as token's member.
func (f *fixture) withToken(method, path string, token denproto.Bytes) *http.Response {
	f.t.Helper()
	req, _ := http.NewRequest(method, f.srv.URL+path, nil)
	req.Header.Set(denproto.HeaderVersion, "1")
	req.Header.Set("Authorization", "Bearer "+token.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	return resp
}

// TestLinesGoAfter90Days checks that the den's timer sweeps each line
// as its 90 days pass, against the den's clock: a line a day younger
// stays a day longer.
func TestLinesGoAfter90Days(t *testing.T) {
	f := newFixture(t)
	f.d.TokenLifetime = 2 * denproto.AuditKept
	start := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	clock := f.fakeClock(start)
	alice, _ := f.owner()
	invite := func() {
		if status := f.call(http.MethodPost, "/api/invites", alice, denproto.InviteCreateRequest{}, nil); status != http.StatusCreated {
			t.Fatalf("an invite: %d", status)
		}
	}
	invite()
	clock.Add(int64(24 * time.Hour))
	invite()
	kept := func() (n int) {
		t.Helper()
		if err := f.db.QueryRow(`SELECT count(*) FROM den_audit`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// count restarts the timer and waits until the den keeps want lines,
	// and staff read as many.
	count := func(want int) {
		t.Helper()
		f.d.StopTimer()
		f.d.StartTimer()
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			n, read := kept(), len(f.auditLines(alice))
			if n == want && read == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the den keeps %d lines and staff read %d, not %d", n, read, want)
			}
		}
	}
	clock.Store(start.Add(denproto.AuditKept - time.Millisecond).UnixNano())
	count(2)
	// Past its time, a line no timer swept yet reads as gone, and the
	// timer then sweeps it.
	f.d.StopTimer()
	clock.Store(start.Add(denproto.AuditKept).UnixNano())
	if n, read := kept(), len(f.auditLines(alice)); n != 2 || read != 1 {
		t.Fatalf("unswept past its time, the den keeps %d lines and staff read %d", n, read)
	}
	count(1)
	clock.Store(start.Add(denproto.AuditKept + 24*time.Hour - time.Millisecond).UnixNano())
	count(1)
	clock.Store(start.Add(denproto.AuditKept + 24*time.Hour).UnixNano())
	count(0)
	f.d.StopTimer()
}
