package den

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/coder/websocket"
)

// renew renews a session over its socket, as its client does each time
// the token nears its end, and waits for the den's answer.
func (f *fixture) renew(token denproto.Bytes, key ed25519.PrivateKey) {
	f.t.Helper()
	c, _ := f.dial(token, "")
	defer c.CloseNow()
	var ch denproto.ChallengeResponse
	if status := f.call(http.MethodPost, "/api/auth/challenge", nil, denproto.ChallengeRequest{ClientNonce: denproto.Random(denproto.NonceSize)}, &ch); status != http.StatusOK {
		f.t.Fatalf("challenge: %d", status)
	}
	info, _ := f.d.Info()
	e, _ := denproto.NewEvent(denproto.EventAuthRenew, 0, denproto.Renew{Nonce: ch.Nonce, Proof: denproto.Prove(key, info.ID, ch.Nonce)})
	frame, _ := denproto.EncodeFrame(e)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, frame); err != nil {
		f.t.Fatal(err)
	}
	for {
		for _, e := range read(f.t, c) {
			if e.T == denproto.EventAuthRenewed {
				return
			}
		}
	}
}

// seen reads when each member was last seen, by their ID, as token's
// member.
func (f *fixture) seen(token denproto.Bytes) map[string]int64 {
	f.t.Helper()
	var list denproto.SeenList
	if status := f.call(http.MethodGet, "/api/members/seen", token, nil, &list); status != http.StatusOK {
		f.t.Fatalf("GET /api/members/seen: %d", status)
	}
	out := map[string]int64{}
	for _, s := range list.Seen {
		if _, dup := out[s.MemberID]; dup || s.Day%denproto.SeenDay.Milliseconds() != 0 {
			f.t.Fatalf("seen %+v", list.Seen)
		}
		out[s.MemberID] = s.Day
	}
	return out
}

// TestLastSeenByDay checks that a device's sign-in and each renewal of
// its session write when it was last seen, which staff read as each
// member's latest, by its day in UTC, and a member is refused. A member
// who left has no devices, and so no day.
func TestLastSeenByDay(t *testing.T) {
	f := newFixture(t)
	f.d.TokenLifetime = 1000 * time.Hour
	clock := f.fakeClock(time.Date(2026, 11, 20, 23, 30, 0, 0, time.UTC))
	day := func(y, m, d int) int64 { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC).UnixMilli() }
	alice, _ := f.owner()
	bob, bobKey := f.memberWithKey(alice, "bob")
	carol, carolKey := f.memberWithKey(alice, "carol")
	dave := f.member(alice, "dave")
	a, b, c, d := f.session(alice), f.session(bob), f.session(carol), f.session(dave)
	if status := f.call(http.MethodPatch, "/api/members/"+denproto.FormatID(c.MemberID), alice,
		denproto.RoleRequest{Role: denproto.RoleModerator}, nil); status != http.StatusOK {
		t.Fatalf("making carol a moderator: %d", status)
	}
	wantError(t, f.withToken(http.MethodGet, "/api/members/seen", bob), http.StatusForbidden, denproto.CodeForbidden)
	ids := func(s ...int64) []string {
		out := make([]string, len(s))
		for i, id := range s {
			out[i] = denproto.FormatID(id)
		}
		return out
	}
	member := ids(a.MemberID, b.MemberID, c.MemberID, d.MemberID)
	want := func(token denproto.Bytes, days ...int64) {
		t.Helper()
		got := f.seen(token)
		if len(got) != len(days) {
			t.Fatalf("seen %v, not %v", got, days)
		}
		for i, day := range days {
			if got[member[i]] != day {
				t.Fatalf("member %s seen %d, not %d, of %v", member[i], got[member[i]], day, got)
			}
		}
	}
	want(alice, day(2026, 11, 20), day(2026, 11, 20), day(2026, 11, 20), day(2026, 11, 20))

	// Two days on, bob's socket renews his session and carol's doesn't.
	clock.Add(int64(48 * time.Hour))
	f.renew(bob, bobKey)
	want(carol, day(2026, 11, 20), day(2026, 11, 22), day(2026, 11, 20), day(2026, 11, 20))
	clock.Add(int64(time.Hour))
	f.renew(carol, carolKey)
	want(alice, day(2026, 11, 20), day(2026, 11, 22), day(2026, 11, 23), day(2026, 11, 20))

	if status := f.call(http.MethodPost, "/api/me/leave", dave, nil, nil); status != http.StatusNoContent {
		t.Fatalf("dave leaving: %d", status)
	}
	member = member[:3]
	want(alice, day(2026, 11, 20), day(2026, 11, 22), day(2026, 11, 23))
	var raw json.RawMessage
	if status := f.call(http.MethodGet, "/api/members/seen", carol, nil, &raw); status != http.StatusOK {
		t.Fatalf("a moderator reading: %d", status)
	}
}
