package denclient

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A call's length reaches the page as when it began on this computer's
// clock, the frame's arrival less elapsed_ms (M21.2): a later frame moves
// the start only earlier, and one later by more than startSlack is a new
// call; a call that empties takes its start with it, a full state keeps
// the start of a call going on, and a den older than M21.2 gives none. A
// ready keeps the starts the state held before it, by the same rule.
func TestCallStartsOnThisClock(t *testing.T) {
	s := newState()
	t0 := int64(1_800_000_000_000)
	ms := func(v int64) *int64 { return &v }
	call := func(channel string, elapsed *int64, record int64) denproto.Call {
		return denproto.Call{ChannelID: channel, Members: []denproto.CallMember{{ID: "1"}}, ElapsedMS: elapsed, RecordMS: record}
	}
	apply := func(at int64, full bool, calls ...denproto.Call) (pageVoiceState, error) {
		t.Helper()
		e, _ := denproto.NewEvent(denproto.EventVoiceState, 0, denproto.VoiceState{Calls: calls, Full: full})
		out, ok, err := s.applyEvent(nil, e, denproto.Member{ID: "1"}, time.UnixMilli(at))
		if err != nil {
			return pageVoiceState{}, err
		}
		if !ok || strings.Contains(string(out.D), "elapsed_ms") {
			t.Fatalf("the page got %t %s", ok, out.D)
		}
		var p pageVoiceState
		if err := json.Unmarshal(out.D, &p); err != nil {
			t.Fatal(err)
		}
		return p, nil
	}
	starts := func(at int64, want int64, calls ...denproto.Call) {
		t.Helper()
		p, err := apply(at, false, calls...)
		if err != nil || len(p.Calls) != 1 || p.Calls[0].StartedAt != want || s.starts[calls[0].ChannelID] != want {
			t.Fatalf("at %d the call started at %+v, %v; want %d", at-t0, p, err, want-t0)
		}
	}

	starts(t0, t0-5000, call("10", ms(5000), 60000))
	starts(t0+1030, t0-5000, call("10", ms(6000), 60000))
	starts(t0+1980, t0-5020, call("10", ms(7000), 60000))
	starts(t0+20000, t0+19000, call("10", ms(1000), 0))
	if v := s.view(Status{}, denproto.Member{}); len(v.Calls) != 1 || v.Calls[0].StartedAt != t0+19000 || v.Calls[0].ElapsedMS != nil {
		t.Fatalf("the view's calls are %+v", v.Calls)
	}
	// From a den older than M21.2, a call has no length.
	starts(t0+21000, 0, call("11", nil, 0))

	// A full state keeps a call going on, and drops the rest.
	p, err := apply(t0+22000, true, call("10", ms(2990), 0))
	if err != nil || len(p.Calls) != 1 || p.Calls[0].StartedAt != t0+19000 || len(s.calls) != 1 || len(s.starts) != 1 {
		t.Fatalf("a full state gave %+v, %v; the state holds %v and %v", p, err, s.calls, s.starts)
	}
	empty := denproto.Call{ChannelID: "10", Members: []denproto.CallMember{}, ElapsedMS: ms(4000)}
	if _, err := apply(t0+23000, false, empty); err != nil || len(s.calls) != 0 || len(s.starts) != 0 {
		t.Fatalf("the emptied call left %v and %v, %v", s.calls, s.starts, err)
	}

	for _, c := range []denproto.Call{call("10", ms(-1), 0), call("10", ms(denproto.MaxCallMS+1), 0), call("10", ms(0), -1),
		call("10", ms(0), denproto.MaxCallMS+1)} {
		if _, err := apply(t0+24000, false, c); err != errMalformed {
			t.Errorf("a call %d ms long against %d: %v", *c.ElapsedMS, c.RecordMS, err)
		}
	}

	// A ready keeps the start of a call that goes on.
	ready := denproto.Ready{Den: denproto.Den{Limits: denproto.Limits{FileSize: denproto.DefaultFileSize, MemberStorage: denproto.DefaultMemberStorage,
		DenStorage: denproto.DefaultDenStorage}, CallLimits: denproto.DefaultCallLimits}, Sync: denproto.Sync{Epoch: denproto.Random(denproto.SyncEpochSize)},
		SealCheck: denproto.Random(denproto.SealCheckSize), Calls: []denproto.Call{call("20", ms(10000), 0), call("21", ms(10000), 0)}}
	next := newState()
	if err := next.load(nil, ready, nil, map[string]int64{"20": t0 - 10100, "21": t0 - 20000}, time.UnixMilli(t0)); err != nil {
		t.Fatal(err)
	}
	if next.starts["20"] != t0-10100 || next.starts["21"] != t0-10000 {
		t.Fatalf("the ready's calls started at %d and %d", next.starts["20"]-t0, next.starts["21"]-t0)
	}
}
