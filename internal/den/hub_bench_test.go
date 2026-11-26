package den

import (
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

const (
	benchMembers    = 500
	benchStaff      = 10
	benchRoles      = 50
	benchRingEvents = 10_000
)

// Deterministic role assignment for 500 members across 50 roles (IDs 1001..1050):
// 10 staff + 490 non-staff, each non-staff member holding 1 to 5 roles.
func benchMemberRoles(member int64) []int64 {
	if member <= benchStaff {
		return nil
	}
	n := int(member%5) + 1
	out := make([]int64, 0, n)
	for k := range n {
		id := int64(1001 + (int(member)*7+k*13)%benchRoles)
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func drainSubs(subs []*Sub) {
	for _, s := range subs {
		for {
			select {
			case <-s.Events:
			default:
				goto next
			}
		}
	next:
	}
}

func newBenchHub(maxLen, subSize int) *Hub {
	h := NewHub()
	h.maxLen = maxLen
	h.subSize = subSize
	h.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return h
}

// --- Benchmarks: Publish (500 members, 50 custom roles) ---

func BenchmarkHubPublish500(b *testing.B) {
	payload := denproto.Message{ID: "100", ChannelID: "10", Text: "hello"}

	b.Run("Everyone", func(b *testing.B) {
		h := newBenchHub(50_000, 2048)
		subs := make([]*Sub, 0, benchMembers)
		for m := int64(1); m <= benchMembers; m++ {
			s, _, _, _ := h.Subscribe(m, m <= benchStaff, "", 0)
			subs = append(subs, s)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			if i%1024 == 1023 {
				b.StopTimer()
				drainSubs(subs)
				b.StartTimer()
			}
			_ = h.Publish(denproto.EventMessageCreated, payload, Everyone)
		}
	})

	roleCases := []struct {
		name  string
		roles []int64
	}{
		{"1Role", []int64{1005}},
		{"3Roles", []int64{1005, 1019, 1041}},
		{"10Roles", []int64{1001, 1005, 1010, 1015, 1020, 1025, 1030, 1035, 1040, 1045}},
	}

	for _, tc := range roleCases {
		b.Run("Roles_"+tc.name, func(b *testing.B) {
			h := newBenchHub(50_000, 2048)
			for r := int64(1001); r <= 1000+benchRoles; r++ {
				h.EnsureRole(r)
			}
			subs := make([]*Sub, 0, benchMembers)
			for m := int64(1); m <= benchMembers; m++ {
				s, _, _, _ := h.SubscribeAt(m, m <= benchStaff, h.Mask(benchMemberRoles(m)), denproto.Version, "", 0)
				subs = append(subs, s)
			}
			aud := Roles(h.Mask(tc.roles))
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if i%1024 == 1023 {
					b.StopTimer()
					drainSubs(subs)
					b.StartTimer()
				}
				_ = h.Publish(denproto.EventMessageCreated, payload, aud)
			}
		})
	}
}

// --- Benchmarks: SubscribeAt (10k-event ring replay) ---

func BenchmarkHubSubscribeAt10k(b *testing.B) {
	payload := denproto.Message{ID: "100", ChannelID: "10", Text: "hello"}

	// Realistic mixed 10k ring: 40% Everyone, 10% Staff, 50% role-gated across 50 roles (1-3 roles each).
	h := newBenchHub(50_000, 1024)
	for r := int64(1001); r <= 1000+benchRoles; r++ {
		h.EnsureRole(r)
	}
	for i := range benchRingEvents {
		var aud Audience
		switch {
		case i%10 == 0:
			aud = Staff
		case i%2 == 0:
			aud = Everyone
		default:
			n := (i % 3) + 1
			rs := make([]int64, 0, n)
			for k := range n {
				id := int64(1001 + (i*7+k*11)%benchRoles)
				if !slices.Contains(rs, id) {
					rs = append(rs, id)
				}
			}
			aud = Roles(h.Mask(rs))
		}
		_ = h.Publish(denproto.EventMessageCreated, payload, aud)
	}

	targetMember := int64(42)
	mask := h.Mask(benchMemberRoles(targetMember))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sub, missed, resumed, _ := h.SubscribeAt(targetMember, false, mask, denproto.Version, h.Epoch(), 0)
		if !resumed || len(missed) == 0 {
			b.Fatal("expected resume")
		}
		h.Unsubscribe(sub)
	}
}

// --- Benchmarks: Pure audience-filter cost (no channel/JSON overhead) ---

func BenchmarkRoleAudienceIncludes10k(b *testing.B) {
	memberRoles := []int64{1003, 1011, 1024, 1037, 1049}
	var memberMask uint64
	for _, r := range memberRoles {
		memberMask |= uint64(1) << (r - 1001)
	}

	auds := make([]Audience, benchRingEvents)
	for i := range benchRingEvents {
		var mask uint64
		for _, off := range []int{0, 13, 29} {
			mask |= uint64(1) << ((i*7 + off) % benchRoles)
		}
		auds[i] = Roles(mask)
	}

	b.ReportAllocs()
	var hits int
	for range b.N {
		for i := range auds {
			if auds[i].includes(42, false, memberMask, denproto.Version) {
				hits++
			}
		}
	}
	if hits == 0 {
		b.Fatal("no hits")
	}
}

// --- Benchmarks: Visibility refreshes (500 subscribers) ---

func BenchmarkHubVisibilityRefresh500(b *testing.B) {
	b.Run("RefreshNonStaff", func(b *testing.B) {
		h := newBenchHub(50_000, 2048)
		subs := make([]*Sub, 0, benchMembers)
		for m := int64(1); m <= benchMembers; m++ {
			s, _, _, _ := h.Subscribe(m, m <= benchStaff, "", 0)
			subs = append(subs, s)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			if i%1024 == 1023 {
				b.StopTimer()
				drainSubs(subs)
				b.StartTimer()
			}
			h.RefreshNonStaff()
		}
	})

	b.Run("ChannelRoleChange_Targeted", func(b *testing.B) {
		h := newBenchHub(50_000, 2048)
		for r := int64(1001); r <= 1000+benchRoles; r++ {
			h.EnsureRole(r)
		}
		subs := make([]*Sub, 0, benchMembers)
		for m := int64(1); m <= benchMembers; m++ {
			s, _, _, _ := h.SubscribeAt(m, m <= benchStaff, h.Mask(benchMemberRoles(m)), denproto.Version, "", 0)
			subs = append(subs, s)
		}
		oldMask := h.Mask([]int64{1005})
		newMask := h.Mask([]int64{1005, 1022})
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			if i%1024 == 1023 {
				b.StopTimer()
				drainSubs(subs)
				b.StartTimer()
			}
			h.RefreshChannelVisibility(false, false, oldMask, newMask)
		}
	})

	b.Run("MemberRoleChange", func(b *testing.B) {
		h := newBenchHub(50_000, 2048)
		for r := int64(1001); r <= 1000+benchRoles; r++ {
			h.EnsureRole(r)
		}
		subs := make([]*Sub, 0, benchMembers)
		for m := int64(1); m <= benchMembers; m++ {
			s, _, _, _ := h.SubscribeAt(m, m <= benchStaff, h.Mask(benchMemberRoles(m)), denproto.Version, "", 0)
			subs = append(subs, s)
		}
		m1 := h.Mask([]int64{1001, 1002})
		m2 := h.Mask([]int64{1001, 1003})
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			if i%1024 == 1023 {
				b.StopTimer()
				drainSubs(subs)
				b.StartTimer()
			}
			if i%2 == 0 {
				h.SetMemberRoles(42, m1)
			} else {
				h.SetMemberRoles(42, m2)
			}
		}
	})

	b.Run("DeleteRole", func(b *testing.B) {
		h := newBenchHub(50_000, 2048)
		for r := int64(1001); r <= 1000+benchRoles; r++ {
			h.EnsureRole(r)
		}
		subs := make([]*Sub, 0, benchMembers)
		var affected []int64
		for m := int64(1); m <= benchMembers; m++ {
			rs := benchMemberRoles(m)
			if slices.Contains(rs, 1005) {
				affected = append(affected, m)
			}
			s, _, _, _ := h.SubscribeAt(m, m <= benchStaff, h.Mask(rs), denproto.Version, "", 0)
			subs = append(subs, s)
		}
		otherMask := h.Mask([]int64{1022})
		roleID := int64(2000)
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			b.StopTimer()
			if i%256 == 255 {
				drainSubs(subs)
			}
			roleID++
			bit, _ := h.EnsureRole(roleID)
			h.mu.Lock()
			for _, s := range subs {
				if slices.Contains(affected, s.member) {
					s.roles |= bit
				}
			}
			h.mu.Unlock()
			b.StartTimer()
			h.DeleteRole(roleID, affected, otherMask)
		}
	})
}

// --- Correctness tests: targeted visibility refresh & resume semantics ---

func expectRefresh(t *testing.T, name string, sub *Sub, want bool) {
	t.Helper()
	select {
	case ev := <-sub.Events:
		if !want {
			t.Fatalf("%s unexpectedly got event %+v", name, ev)
		}
		if ev.T != eventRefresh {
			t.Fatalf("%s got %q, want %q", name, ev.T, eventRefresh)
		}
	default:
		if want {
			t.Fatalf("%s did not receive %q", name, eventRefresh)
		}
	}
}

func TestRoleHubTargetedRefreshAndResume(t *testing.T) {
	if got := unsafe.Sizeof(Audience{}); got != 48 {
		t.Fatalf("Audience size = %d, want 48", got)
	}
	t.Run("ChannelVisibilityTransitions", func(t *testing.T) {
		h := newBenchHub(1000, 64)
		r1, _ := h.EnsureRole(101)
		r2, _ := h.EnsureRole(102)
		r3, _ := h.EnsureRole(103)

		staff, _, _, _ := h.SubscribeAt(1, true, 0, denproto.Version, "", 0)
		hasR1, _, _, _ := h.SubscribeAt(2, false, r1, denproto.Version, "", 0)
		hasR2, _, _, _ := h.SubscribeAt(3, false, r2, denproto.Version, "", 0)
		hasR3, _, _, _ := h.SubscribeAt(4, false, r3, denproto.Version, "", 0)
		none, _, _, _ := h.SubscribeAt(5, false, 0, denproto.Version, "", 0)

		_ = h.Publish("before", nil, Everyone)
		for _, s := range []*Sub{staff, hasR1, hasR2, hasR3, none} {
			<-s.Events
		}
		before := h.seq

		// StaffOnly -> Roles(R1): only R1-holders refresh; R2/R3/none/staff resume cleanly.
		h.RefreshChannelVisibility(false, false, 0, r1)
		expectRefresh(t, "staff", staff, false)
		expectRefresh(t, "hasR1", hasR1, true)
		expectRefresh(t, "hasR2", hasR2, false)
		expectRefresh(t, "hasR3", hasR3, false)
		expectRefresh(t, "none", none, false)

		if _, _, resumed, _ := h.SubscribeAt(2, false, r1, denproto.Version, h.Epoch(), before); resumed {
			t.Fatal("R1 holder resumed across StaffOnly -> Roles(R1)")
		}
		if _, _, resumed, _ := h.SubscribeAt(2, false, r1, denproto.Version, h.Epoch(), h.seq); !resumed {
			t.Fatal("R1 holder could not resume after snapshot")
		}
		if _, _, resumed, _ := h.SubscribeAt(3, false, r2, denproto.Version, h.Epoch(), before); !resumed {
			t.Fatal("unrelated R2 holder unnecessarily forced to resync")
		}
		if _, _, resumed, _ := h.SubscribeAt(5, false, 0, denproto.Version, h.Epoch(), before); !resumed {
			t.Fatal("unrelated roleless member unnecessarily forced to resync")
		}
		if _, _, resumed, _ := h.SubscribeAt(1, true, 0, denproto.Version, h.Epoch(), before); !resumed {
			t.Fatal("staff unnecessarily forced to resync")
		}

		// Roles(R1) -> Roles(R2): both R1 (lost access) and R2 (gained access)
		// refresh and cannot resume; R3 and roleless still resume cleanly.
		mid := h.seq
		h.RefreshChannelVisibility(false, false, r1, r2)
		expectRefresh(t, "staff", staff, false)
		expectRefresh(t, "hasR1", hasR1, true)
		expectRefresh(t, "hasR2", hasR2, true)
		expectRefresh(t, "hasR3", hasR3, false)
		expectRefresh(t, "none", none, false)

		if _, _, resumed, _ := h.SubscribeAt(2, false, r1, denproto.Version, h.Epoch(), mid); resumed {
			t.Fatal("R1 holder resumed across Roles(R1) -> Roles(R2)")
		}
		if _, _, resumed, _ := h.SubscribeAt(3, false, r2, denproto.Version, h.Epoch(), mid); resumed {
			t.Fatal("R2 holder resumed across Roles(R1) -> Roles(R2)")
		}
		if _, _, resumed, _ := h.SubscribeAt(4, false, r3, denproto.Version, h.Epoch(), mid); !resumed {
			t.Fatal("unrelated R3 holder forced to resync")
		}

		// Everyone -> Roles(R1): all non-staff refresh and cannot resume; staff unaffected.
		prePub := h.seq
		h.RefreshChannelVisibility(true, false, 0, r1)
		expectRefresh(t, "staff", staff, false)
		expectRefresh(t, "hasR1", hasR1, true)
		expectRefresh(t, "hasR3", hasR3, true)
		expectRefresh(t, "none", none, true)
		if _, _, resumed, _ := h.SubscribeAt(4, false, r3, denproto.Version, h.Epoch(), prePub); resumed {
			t.Fatal("non-staff resumed across Everyone -> Roles(R1)")
		}
		if _, _, resumed, _ := h.SubscribeAt(1, true, 0, denproto.Version, h.Epoch(), prePub); !resumed {
			t.Fatal("staff failed to resume across Everyone -> Roles(R1)")
		}
	})

	t.Run("MemberRoleChangeAndOfflineLoss", func(t *testing.T) {
		h := newBenchHub(1000, 64)
		r1, _ := h.EnsureRole(101)

		alice, _, _, _ := h.SubscribeAt(10, false, r1, denproto.Version, "", 0)
		bob, _, _, _ := h.SubscribeAt(11, false, r1, denproto.Version, "", 0)
		_ = h.Publish("r1-msg", nil, Roles(r1))
		<-alice.Events
		<-bob.Events
		at := h.seq

		// Alice loses R1 (online) and offline member 12 also loses R1.
		h.SetMemberRoles(10, 0)
		h.SetMemberRoles(12, 0)
		expectRefresh(t, "alice", alice, true)
		expectRefresh(t, "bob", bob, false)

		// Alice's socket now has roles=0 so future R1 events skip her.
		_ = h.Publish("r1-secret", nil, Roles(r1))
		select {
		case ev := <-alice.Events:
			t.Fatalf("alice received secret after losing role: %+v", ev)
		default:
		}
		if ev := <-bob.Events; ev.T != "r1-secret" {
			t.Fatalf("bob got %+v, want r1-secret", ev)
		}

		// Both Alice and offline member 12 now reconnect with roles=0 and old seq:
		// neither may resume even though their current role mask is 0.
		if _, _, resumed, _ := h.SubscribeAt(10, false, 0, denproto.Version, h.Epoch(), at); resumed {
			t.Fatal("alice resumed across losing role")
		}
		if _, _, resumed, _ := h.SubscribeAt(12, false, 0, denproto.Version, h.Epoch(), at); resumed {
			t.Fatal("offline member resumed across losing role")
		}
		if _, _, resumed, _ := h.SubscribeAt(11, false, r1, denproto.Version, h.Epoch(), at); !resumed {
			t.Fatal("unaffected bob failed to resume")
		}
	})

	t.Run("RoleDeletionOnlineAndOffline", func(t *testing.T) {
		h := newBenchHub(1000, 64)
		r1, _ := h.EnsureRole(101)
		r2, _ := h.EnsureRole(102)
		r3, _ := h.EnsureRole(103)

		// Channel A had [R1] (flips to staff-only); Channel B had [R1, R2] (becomes [R2]).
		// Member 20 (online) and 21 (offline) held R1; member 22 holds R2; member 23 holds R3.
		m20, _, _, _ := h.SubscribeAt(20, false, r1, denproto.Version, "", 0)
		m22, _, _, _ := h.SubscribeAt(22, false, r2, denproto.Version, "", 0)
		m23, _, _, _ := h.SubscribeAt(23, false, r3, denproto.Version, "", 0)
		_ = h.Publish("init", nil, Everyone)
		<-m20.Events
		<-m22.Events
		<-m23.Events
		before := h.seq

		h.DeleteRole(101, []int64{20, 21}, r2)
		expectRefresh(t, "m20 (held R1)", m20, true)
		expectRefresh(t, "m22 (shared channel with R1 via R2)", m22, true)
		expectRefresh(t, "m23 (unrelated R3)", m23, false)
		if h.Roles(m20) != 0 {
			t.Fatalf("m20 still holds deleted role bit: %b", h.Roles(m20))
		}

		// Offline member 21 reconnects with roles=0: must NOT resume across R1 deletion.
		if _, _, resumed, _ := h.SubscribeAt(21, false, 0, denproto.Version, h.Epoch(), before); resumed {
			t.Fatal("offline R1 holder resumed across role deletion")
		}
		if _, _, resumed, _ := h.SubscribeAt(22, false, r2, denproto.Version, h.Epoch(), before); resumed {
			t.Fatal("R2 holder sharing channel with R1 resumed across role deletion")
		}
		if _, _, resumed, _ := h.SubscribeAt(23, false, r3, denproto.Version, h.Epoch(), before); !resumed {
			t.Fatal("unrelated R3 holder forced to resync on R1 deletion")
		}
	})

	t.Run("SlotRecyclingSafety", func(t *testing.T) {
		// Small ring to test both in-ring quarantine fallback and trimmed-slot free reuse.
		h := newBenchHub(50, 128)
		staff, _, _, _ := h.SubscribeAt(1, true, 0, denproto.Version, "", 0)
		member, _, _, _ := h.SubscribeAt(2, false, 0, denproto.Version, "", 0)

		// Allocate all 64 slots via create/delete churn within one ring window.
		// First 50 stay active; 14 more are created and deleted while in the ring.
		for id := int64(1); id <= 50; id++ {
			if _, refreshed := h.EnsureRole(id); refreshed {
				t.Fatalf("unexpected refresh allocating active slot %d", id)
			}
		}
		for id := int64(51); id <= 64; id++ {
			mask, refreshed := h.EnsureRole(id)
			if refreshed {
				t.Fatalf("unexpected refresh allocating free slot %d", id)
			}
			_ = h.Publish("old-role-secret", nil, Roles(mask))
			h.DeleteRole(id, nil, 0)
		}
		drainSubs([]*Sub{staff, member})
		beforeRecycle := h.seq

		// 65th role has no unretired slot (50 active + 14 retired still in ring):
		// recycling MUST refresh non-staff so no non-staff client can replay
		// old-role-secret using the recycled bit.
		recycledMask, refreshed := h.EnsureRole(65)
		if !refreshed {
			t.Fatal("expected non-staff refresh when recycling an in-ring retired slot")
		}
		expectRefresh(t, "member", member, true)
		expectRefresh(t, "staff", staff, false)
		if _, _, resumed, _ := h.SubscribeAt(2, false, recycledMask, denproto.Version, h.Epoch(), beforeRecycle); resumed {
			t.Fatal("non-staff resumed across recycled bit slot")
		}
		if _, _, resumed, _ := h.SubscribeAt(1, true, 0, denproto.Version, h.Epoch(), beforeRecycle); !resumed {
			t.Fatal("staff blocked by non-staff slot recycle refresh")
		}

		// Once enough events push a retired slot's deletion seq past h.first,
		// reusing that slot needs no refresh at all.
		h.DeleteRole(50, nil, 0)
		for range 60 {
			_ = h.Publish("pad", nil, Everyone)
		}
		drainSubs([]*Sub{staff, member})
		if _, refreshed := h.EnsureRole(66); refreshed {
			t.Fatal("unexpected refresh reusing a slot whose retirement fell out of the ring")
		}
	})
}
