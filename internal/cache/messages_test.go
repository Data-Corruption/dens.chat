package cache

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func msg(id, channel int64, text string) denproto.Message {
	return denproto.Message{ID: denproto.FormatID(id), ChannelID: denproto.FormatID(channel), AuthorID: "1", Revision: 1, Text: text}
}

func msgs(channel int64, ids ...int64) []denproto.Message {
	var out []denproto.Message
	for _, id := range ids {
		out = append(out, msg(id, channel, fmt.Sprintf("message %d", id)))
	}
	return out
}

func span(from, to int64) []int64 {
	var out []int64
	for id := from; id <= to; id++ {
		out = append(out, id)
	}
	return out
}

func ids(ms []denproto.Message) []int64 {
	var out []int64
	for _, m := range ms {
		n, _ := denproto.ParseID(m.ID)
		out = append(out, n)
	}
	return out
}

// page asks the cache for a page, failing the test unless it answers as
// want says.
func (f *fixture) page(channel int64, q Query, gen int64, want bool) Page {
	f.t.Helper()
	p, ok, err := f.s.Page(denA, channel, q, gen, false, false)
	if err != nil {
		f.t.Fatal(err)
	}
	if ok != want {
		f.t.Fatalf("query %+v: answered %v, want %v (%v)", q, ok, want, ids(p.Messages))
	}
	return p
}

func (f *fixture) addRun(channel, lo, hi int64, first bool, gen int64, ms []denproto.Message) {
	f.t.Helper()
	if err := f.s.AddRun(denA, channel, lo, hi, first, gen, ms); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) runs(channel int64) []Run {
	f.t.Helper()
	r, err := f.s.Runs(denA, channel)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestRunsJoinAndAnswerPages(t *testing.T) {
	f := newFixture(t)
	const gen = 7
	// The newest page, fetched in connection 7, reaches the newest.
	f.addRun(1, 10, 19, false, gen, msgs(1, span(10, 19)...))
	p := f.page(1, Query{}, gen, true)
	if !slices.Equal(ids(p.Messages), span(10, 19)) || !p.HasOlder || p.HasNewer {
		t.Fatalf("the newest page: %v older %v newer %v", ids(p.Messages), p.HasOlder, p.HasNewer)
	}
	f.page(1, Query{}, gen+1, false) // another connection may have missed messages

	// A message that arrives as an event joins the run.
	if err := f.s.AppendTail(denA, msg(25, 1, "live"), gen); err != nil {
		t.Fatal(err)
	}
	if r := f.runs(1); len(r) != 1 || r[0].Lo != 10 || r[0].Hi != 25 || r[0].Gen != gen {
		t.Fatalf("runs after an event: %+v", r)
	}

	// An older page, reaching the channel's start, joins the run below.
	f.addRun(1, 1, 9, true, 0, msgs(1, 3, 4, 5, 6, 7, 8, 9))
	if r := f.runs(1); len(r) != 1 || r[0].Lo != 1 || r[0].Hi != 25 || !r[0].First || r[0].Gen != gen {
		t.Fatalf("runs after the older page: %+v", r)
	}
	for _, c := range []struct {
		q            Query
		want         []int64
		older, newer bool
	}{
		{Query{Before: 10, Limit: 5}, span(5, 9), true, true},
		{Query{Before: 5, Limit: 5}, []int64{3, 4}, false, true},
		{Query{After: 4, Limit: 3}, []int64{5, 6, 7}, true, true},
		{Query{After: 19, Limit: 3}, []int64{25}, true, false},
		{Query{Around: 7, Limit: 4}, []int64{5, 6, 7, 8}, true, true},
		{Query{Limit: 3}, []int64{18, 19, 25}, true, false},
		{Query{Limit: 50}, append(span(3, 19), 25), false, false},
	} {
		p := f.page(1, c.q, gen, true)
		if !slices.Equal(ids(p.Messages), c.want) || p.HasOlder != c.older || p.HasNewer != c.newer {
			t.Fatalf("%+v: %v older %v newer %v, want %v older %v newer %v", c.q, ids(p.Messages), p.HasOlder, p.HasNewer,
				c.want, c.older, c.newer)
		}
	}

	// A channel with a gap: a page around an old message stands alone, and
	// asks the den for what lies beyond it.
	f.addRun(2, 100, 120, false, gen, msgs(2, 100, 110, 120))
	f.addRun(2, 40, 45, false, 0, msgs(2, span(40, 45)...))
	if r := f.runs(2); len(r) != 2 {
		t.Fatalf("a gap joined runs: %+v", r)
	}
	if p := f.page(2, Query{Around: 42, Limit: 4}, gen, true); !slices.Equal(ids(p.Messages), []int64{40, 41, 42, 43}) {
		t.Fatalf("around 42: %v", ids(p.Messages))
	}
	f.page(2, Query{Before: 40, Limit: 5}, gen, false)
	f.page(2, Query{After: 45, Limit: 5}, gen, false)
	f.page(2, Query{Before: 100, Limit: 5}, gen, false)
	// The page that fills the gap, reaching the newest, joins them all.
	f.addRun(2, 46, 120, false, gen, msgs(2, 50, 100, 110, 120))
	if r := f.runs(2); len(r) != 1 || r[0].Lo != 40 || r[0].Hi != 120 || r[0].Gen != gen {
		t.Fatalf("runs after the gap filled: %+v", r)
	}

	// An empty channel answers the newest page empty, once the client says
	// it's empty, and so does nothing else.
	if p, ok, _ := f.s.Page(denA, 3, Query{}, gen, false, true); !ok || len(p.Messages) != 0 || p.HasOlder || p.HasNewer {
		t.Fatalf("an empty channel: %+v %v", p, ok)
	}
	f.page(3, Query{}, gen, false)
	// Without the den, the highest run stands for the newest.
	if p, ok, _ := f.s.Page(denA, 1, Query{Limit: 2}, gen+1, true, false); !ok || !slices.Equal(ids(p.Messages), []int64{19, 25}) {
		t.Fatalf("offline: %v %v", ids(p.Messages), ok)
	}
}

func TestAnEmptyPageAtTheNewestReaches(t *testing.T) {
	f := newFixture(t)
	f.addRun(1, 1, 10, true, 0, msgs(1, span(1, 10)...))
	// A page after 10 that found nothing newer, in connection 3.
	f.addRun(1, 11, 10, false, 3, nil)
	if r := f.runs(1); len(r) != 1 || r[0].Gen != 3 {
		t.Fatalf("runs: %+v", r)
	}
	if p := f.page(1, Query{Limit: 2}, 3, true); !slices.Equal(ids(p.Messages), []int64{9, 10}) {
		t.Fatalf("the newest: %v", ids(p.Messages))
	}
	// Nothing after an ID past the run's end says nothing of what lies
	// between them.
	f.addRun(2, 1, 10, true, 0, msgs(2, span(1, 10)...))
	f.addRun(2, 16, 15, false, 4, nil)
	if r := f.runs(2); len(r) != 1 || r[0].Gen != 0 {
		t.Fatalf("runs after an empty page past the run: %+v", r)
	}
}

func TestReach(t *testing.T) {
	f := newFixture(t)
	f.addRun(1, 1, 10, true, 0, msgs(1, span(1, 10)...))
	f.addRun(2, 1, 20, true, 0, msgs(2, 15, 20))
	// A snapshot says channel 1's newest is 10 and 2's is 30: only 1's run
	// reaches it.
	if err := f.s.Reach(denA, map[int64]int64{1: 10, 2: 30}, 9); err != nil {
		t.Fatal(err)
	}
	f.page(1, Query{}, 9, true)
	f.page(2, Query{}, 9, false)
}

func TestDeletesAndRetentionStayGone(t *testing.T) {
	f := newFixture(t)
	f.addRun(1, 1, 20, true, 5, msgs(1, span(1, 20)...))
	if err := f.s.DeleteMessage(denA, 12); err != nil {
		t.Fatal(err)
	}
	// A page fetched before the delete, landing after it.
	f.addRun(1, 11, 13, false, 0, msgs(1, 11, 12, 13))
	if p := f.page(1, Query{Around: 12, Limit: 4}, 5, true); slices.Contains(ids(p.Messages), 12) {
		t.Fatalf("a deleted message came back: %v", ids(p.Messages))
	}
	// An older revision doesn't replace a newer one.
	newer := msg(5, 1, "edited")
	newer.Revision = 3
	if err := f.s.UpdateMessage(denA, newer); err != nil {
		t.Fatal(err)
	}
	f.addRun(1, 5, 5, false, 0, msgs(1, 5))
	if p := f.page(1, Query{Around: 5, Limit: 2}, 5, true); p.Messages[1].Text != "edited" {
		t.Fatalf("a page from before the edit undid it: %+v", p.Messages)
	}

	// Retention takes everything up to through, and the run starts there.
	if err := f.s.Expire(denA, 8); err != nil {
		t.Fatal(err)
	}
	if r := f.runs(1); len(r) != 1 || r[0].Lo != 9 || !r[0].First {
		t.Fatalf("runs after retention: %+v", r)
	}
	f.addRun(1, 1, 9, true, 0, msgs(1, 7, 8, 9))
	if p := f.page(1, Query{Before: 11, Limit: 10}, 5, true); !slices.Equal(ids(p.Messages), []int64{9, 10}) || p.HasOlder {
		t.Fatalf("before 11 after retention: %v older %v", ids(p.Messages), p.HasOlder)
	}
	// It outlasts a restart.
	f.reopen()
	f.addRun(1, 1, 9, true, 0, msgs(1, 8))
	if p := f.page(1, Query{Before: 10, Limit: 10}, 5, true); !slices.Equal(ids(p.Messages), []int64{9}) {
		t.Fatalf("after a restart, retention's reach: %v", ids(p.Messages))
	}
}

func TestQuotesFollowTheirOriginals(t *testing.T) {
	f := newFixture(t)
	original := msg(10, 1, "  the original")
	reply := msg(11, 1, "a reply")
	reply.ReplyTo, reply.Reply = original.ID, &denproto.Reply{AuthorID: "1", Text: "the original"}
	dmOriginal := denproto.Message{ID: "20", ChannelID: "2", AuthorID: "2", Revision: 1, Sealed: []byte("sealed v1"), KeyID: "4", Nonce: []byte("n")}
	dmReply := denproto.Message{ID: "21", ChannelID: "2", AuthorID: "1", Revision: 1, Sealed: []byte("sealed reply"), KeyID: "4", ReplyTo: "20"}
	f.addRun(1, 10, 11, false, 1, []denproto.Message{original, reply})
	f.addRun(2, 20, 21, false, 1, []denproto.Message{dmReply})
	quote := func(channel, id int64) *denproto.Reply {
		p := f.page(channel, Query{After: id - 1, Limit: 1}, 1, true)
		for _, m := range p.Messages {
			if m.ID == denproto.FormatID(id) {
				return m.Reply
			}
		}
		t.Fatalf("no message %d", id)
		return nil
	}
	edited := original
	edited.Text, edited.Revision = "the original, edited", 2
	if err := f.s.UpdateMessage(denA, edited); err != nil {
		t.Fatal(err)
	}
	if q := quote(1, 11); q == nil || q.Text != "the original, edited" {
		t.Fatalf("the quote after an edit: %+v", q)
	}
	// An original the cache doesn't hold still has its replies quote it.
	dmEdited := dmOriginal
	dmEdited.Sealed, dmEdited.Revision = []byte("sealed v2"), 2
	if err := f.s.UpdateMessage(denA, dmEdited); err != nil {
		t.Fatal(err)
	}
	if q := quote(2, 21); q == nil || !bytes.Equal(q.Sealed, []byte("sealed v2")) || q.Revision != 2 || q.KeyID != "4" {
		t.Fatalf("a DM reply's quote after an edit: %+v", q)
	}
	if err := f.s.DeleteMessage(denA, 10); err != nil {
		t.Fatal(err)
	}
	if q := quote(1, 11); q != nil {
		t.Fatalf("the quote of a deleted message: %+v", q)
	}
	if err := f.s.Expire(denA, 20); err != nil {
		t.Fatal(err)
	}
	if q := quote(2, 21); q != nil {
		t.Fatalf("the quote of an expired message: %+v", q)
	}
}

func TestAChannelKeepsItsNewest(t *testing.T) {
	old := channelCap
	channelCap = 10
	defer func() { channelCap = old }()
	f := newFixture(t)
	f.addRun(1, 1, 8, true, 2, msgs(1, span(1, 8)...))
	for id := int64(9); id <= 15; id++ {
		if err := f.s.AppendTail(denA, msg(id, 1, "live"), 2); err != nil {
			t.Fatal(err)
		}
	}
	if r := f.runs(1); len(r) != 1 || r[0].Lo != 6 || r[0].Hi != 15 || r[0].First {
		t.Fatalf("runs after passing the cap: %+v", r)
	}
	if p := f.page(1, Query{Limit: 50}, 2, true); !slices.Equal(ids(p.Messages), span(6, 15)) || !p.HasOlder {
		t.Fatalf("the newest after the cap: %v older %v", ids(p.Messages), p.HasOlder)
	}
}

func TestEvictionWeighsChannelsAgainstFiles(t *testing.T) {
	f := newFixture(t)
	clock := time.UnixMilli(1_759_000_000_000)
	f.s.now = func() time.Time { return clock }
	big := func(channel int64) []denproto.Message {
		var out []denproto.Message
		for id := int64(1); id <= 40; id++ {
			out = append(out, msg(channel*1000+id, channel, string(bytes.Repeat([]byte("x"), 4000))))
		}
		return out
	}
	f.addRun(1, 1001, 1040, true, 0, big(1))
	clock = clock.Add(time.Minute)
	d := newDen(t, 3*chunkSize)
	h := f.open(1, onMessage, d)
	f.readAll(h, d, chunkSize)
	h.Close()
	clock = clock.Add(time.Minute)
	f.addRun(2, 2001, 2040, true, 0, big(2))
	held := f.s.Used()
	// Room for the newest of the three only: the channel read first goes,
	// then the file, and the channel read last stays.
	f.s.mu.Lock()
	f.s.limit = held - 1
	err := f.s.evictLocked()
	first := f.s.used
	f.s.limit = held / 3
	err2 := f.s.evictLocked()
	f.s.mu.Unlock()
	if err != nil || err2 != nil {
		t.Fatal(err, err2)
	}
	if len(f.runs(1)) != 0 || first >= held || len(f.runs(2)) != 1 {
		t.Fatalf("after the first eviction: channel 1 %v, channel 2 %v", f.runs(1), f.runs(2))
	}
	var files int
	f.s.db.QueryRow(`SELECT count(*) FROM files`).Scan(&files)
	if files != 0 {
		t.Fatalf("%d files after the second eviction", files)
	}
}

func TestNewestCoversMessagesAndFiles(t *testing.T) {
	f := newFixture(t)
	f.addRun(1, 1, 30, true, 0, msgs(1, 30))
	d := newDen(t, 100)
	h := f.open(5, Owner{Message: 50, Channel: 2}, d)
	f.readAll(h, d, 100)
	h.Close()
	if n, err := f.s.Newest(denA); err != nil || n != 50 {
		t.Fatalf("newest %d %v", n, err)
	}
}

func TestStateAndMembersOutlastRestarts(t *testing.T) {
	f := newFixture(t)
	if st, err := f.s.State(denA); st != nil || err != nil {
		t.Fatalf("a den's state before any: %q %v", st, err)
	}
	if _, ok, _ := f.s.Members(denA); ok {
		t.Fatal("members before any")
	}
	if err := f.s.SaveState(denA, []byte(`{"channels":[]}`)); err != nil {
		t.Fatal(err)
	}
	want := Members{List: []byte(`[{"id":"1"}]`), Epoch: bytes.Repeat([]byte{3}, 16), Version: 12}
	if err := f.s.SaveMembers(denA, want); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	if st, err := f.s.State(denA); string(st) != `{"channels":[]}` || err != nil {
		t.Fatalf("state %q %v", st, err)
	}
	got, ok, err := f.s.Members(denA)
	if !ok || err != nil || !bytes.Equal(got.List, want.List) || !bytes.Equal(got.Epoch, want.Epoch) || got.Version != 12 {
		t.Fatalf("members %+v %v %v", got, ok, err)
	}
	// Emptying the cache keeps them; dropping the den doesn't.
	f.addRun(1, 1, 3, true, 0, msgs(1, 1, 2, 3))
	if err := f.s.Clear(); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.s.State(denA); st == nil || len(f.runs(1)) != 0 || f.s.Used() != 0 {
		t.Fatalf("after emptying: state %q, runs %v, used %d", st, f.runs(1), f.s.Used())
	}
	if err := f.s.DropDen(denA); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.s.State(denA); st != nil {
		t.Fatalf("a dropped den's state: %q", st)
	}
}
