package den

import (
	"context"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A line written while nothing else comes due sets the den's timer for
// when it goes, so a den that runs for 90 days sweeps it then, without a
// restart. The den says it keeps the log and last seen, reopened too.
func TestAuditLineSetsTheTimer(t *testing.T) {
	f, owner, _ := chatFixture(t)
	if _, err := f.db.Exec(`DELETE FROM den_audit`); err != nil {
		t.Fatal(err)
	}
	f.d.StartTimer()
	defer f.d.StopTimer()
	timer := func() (bool, int64) {
		tm := &f.d.timer
		tm.mu.Lock()
		defer tm.mu.Unlock()
		return tm.timer != nil, tm.at
	}
	if set, _ := timer(); set {
		t.Fatal("the timer is set with nothing to come due")
	}
	f.clock = f.clock.Add(time.Minute)
	if _, err := f.d.CreateInvite(context.Background(), owner, denproto.InviteCreateRequest{}); err != nil {
		t.Fatal(err)
	}
	if set, at := timer(); !set || at != f.clock.Add(denproto.AuditKept).UnixMilli() {
		t.Fatalf("the timer, set %v, goes at %d, not 90 days on at %d", set, at, f.clock.Add(denproto.AuditKept).UnixMilli())
	}
	f.d.StopTimer()
	f.open()
	if info, _ := f.d.Info(); !info.AuditLog || !info.MembersSeen {
		t.Fatalf("the reopened den says %+v", info)
	}
}
