package den

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A member's status is a line of their record, cleaned as a name is with
// emoji whole, sealed at rest, and sent to everyone until its end, past
// which the den sends none and its hourly pass removes it.
func TestStatus(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	if info, _ := f.d.Info(); !info.Status || !info.DeviceNames {
		t.Fatalf("the den's info doesn't say it keeps statuses and device names: %+v", info)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	for what, bad := range map[string]denproto.StatusRequest{
		"a status too long":    {Status: strings.Repeat("a", denproto.MaxStatusRunes+1)},
		"a status of nothing":  {Status: "​"},
		"an end gone by":       {Status: "out", Until: f.clock.Add(-time.Second).UnixMilli()},
		"an end at this time":  {Status: "out", Until: f.clock.UnixMilli()},
		"an end with no line":  {Until: f.clock.Add(time.Hour).UnixMilli()},
		"an end before 1970":   {Status: "out", Until: -1},
		"a status of controls": {Status: "\x00\x01"},
	} {
		if _, err := f.d.SetStatus(ctx, member, bad); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("%s: %v", what, err)
		}
	}

	until := f.clock.Add(30 * time.Minute).UnixMilli()
	m, err := f.d.SetStatus(ctx, member, denproto.StatusRequest{Status: "  👩‍💻 at‮ work  ", Until: until})
	if err != nil || m.Status != "👩‍💻 at work" || m.StatusUntil != until {
		t.Fatalf("setting a status: %+v %v", m, err)
	}
	e := <-sub.Events
	var event denproto.Member
	if e.T != denproto.EventMemberUpdated || json.Unmarshal(e.D, &event) != nil || event.Status != m.Status || event.StatusUntil != until {
		t.Fatalf("event %+v", e)
	}
	var raw []byte
	if err := f.db.QueryRow(`SELECT status FROM den_members WHERE id = ?`, member.MemberID).Scan(&raw); err != nil || bytes.Contains(raw, []byte("work")) {
		t.Fatalf("the status is kept as %q, %v", raw, err)
	}
	statusOf := func(at time.Time) denproto.Member {
		t.Helper()
		snap, err := f.d.snapshot(ctx, owner.MemberID, true, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(snap.Members, func(m denproto.Member) bool { return m.ID == denproto.FormatID(member.MemberID) })
		if i < 0 {
			t.Fatalf("ready's members: %+v", snap.Members)
		}
		got, err := f.d.Member(ctx, member.MemberID)
		if err != nil {
			t.Fatal(err)
		}
		profile, err := f.d.Profile(ctx, owner, denproto.FormatID(member.MemberID))
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != snap.Members[i].Status || profile.Status != got.Status || got.StatusUntil != snap.Members[i].StatusUntil {
			t.Fatalf("at %v the record says %q, ready %q, the profile %q", at, got.Status, snap.Members[i].Status, profile.Status)
		}
		return got
	}
	if got := statusOf(f.clock); got.Status != m.Status || got.StatusUntil != until {
		t.Fatalf("the record carries %q until %d", got.Status, got.StatusUntil)
	}

	// Past its end the den sends none, before its pass has run.
	f.clock = time.UnixMilli(until)
	if got := statusOf(f.clock); got.Status != "" || got.StatusUntil != 0 {
		t.Fatalf("at its end the record carries %q until %d", got.Status, got.StatusUntil)
	}
	var floor int64
	if err := f.db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'den_changes'`).Scan(&floor); err != nil {
		t.Fatal(err)
	}
	f.d.prune()
	var kept []byte
	var keptUntil sql.NullInt64
	if err := f.db.QueryRow(`SELECT status, status_until FROM den_members WHERE id = ?`, member.MemberID).Scan(&kept, &keptUntil); err != nil ||
		kept != nil || keptUntil.Valid {
		t.Fatalf("after the den's pass it keeps %q until %v, %v", kept, keptUntil, err)
	}
	// The pass tells nobody, since everyone holds the end.
	var after int64
	if err := f.db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'den_changes'`).Scan(&after); err != nil || after != floor {
		t.Fatalf("the pass logged changes %d to %d, %v", floor, after, err)
	}
	select {
	case e := <-sub.Events:
		t.Fatalf("the pass published %s", e.T)
	default:
	}

	// One with no end stays, past any pass, until it's cleared.
	if m, err = f.d.SetStatus(ctx, member, denproto.StatusRequest{Status: "on holiday"}); err != nil || m.Status != "on holiday" || m.StatusUntil != 0 {
		t.Fatalf("a status with no end: %+v %v", m, err)
	}
	<-sub.Events
	f.clock = f.clock.Add(1000 * time.Hour)
	f.d.prune()
	if got := statusOf(f.clock); got.Status != "on holiday" {
		t.Fatalf("a status with no end went: %+v", got)
	}
	if m, err = f.d.SetStatus(ctx, member, denproto.StatusRequest{}); err != nil || m.Status != "" {
		t.Fatalf("clearing the status: %+v %v", m, err)
	}
	var cleared denproto.Member
	if e := <-sub.Events; e.T != denproto.EventMemberUpdated || json.Unmarshal(e.D, &cleared) != nil || cleared.Status != "" || cleared.ID != m.ID {
		t.Fatalf("clearing told %+v", e)
	}
	if err := f.db.QueryRow(`SELECT status, status_until FROM den_members WHERE id = ?`, member.MemberID).Scan(&kept, &keptUntil); err != nil ||
		kept != nil || keptUntil.Valid {
		t.Fatalf("a cleared status is kept as %q until %v, %v", kept, keptUntil, err)
	}

	// Leaving takes it.
	if _, err := f.d.SetStatus(ctx, member, denproto.StatusRequest{Status: "leaving"}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Leave(ctx, member); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT status, status_until FROM den_members WHERE id = ?`, member.MemberID).Scan(&kept, &keptUntil); err != nil ||
		kept != nil || keptUntil.Valid {
		t.Fatalf("a member who left keeps %q until %v, %v", kept, keptUntil, err)
	}
}
