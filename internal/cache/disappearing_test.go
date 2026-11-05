package cache

import (
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// sealedMsg is a DM's message, which goes at a time when expires isn't 0.
func sealedMsg(id, channel, expires int64) denproto.Message {
	return denproto.Message{ID: denproto.FormatID(id), ChannelID: denproto.FormatID(channel), AuthorID: "1", Revision: 1,
		Sealed: []byte("sealed"), KeyID: "4", Nonce: []byte("n"), ExpiresAt: expires}
}

// A disappearing message leaves the cache's pages once its time passes,
// and goes with the file on it, the replies' quotes of it, and the bytes
// it took, which Disappear says; a page or an event that brings it again
// keeps it out, as one already past its time is.
func TestDisappearingMessagesGo(t *testing.T) {
	f := newFixture(t)
	now := time.UnixMilli(1_759_000_000_000)
	f.s.now = func() time.Time { return now }
	soon := now.Add(time.Hour).UnixMilli()
	gone := sealedMsg(11, 3, soon)
	reply := sealedMsg(12, 3, now.Add(2*time.Hour).UnixMilli())
	reply.ReplyTo, reply.Reply = gone.ID, &denproto.Reply{AuthorID: "1", Sealed: gone.Sealed, KeyID: "4", Nonce: gone.Nonce, Revision: 1}
	f.addRun(3, 10, 12, true, 1, []denproto.Message{sealedMsg(10, 3, 0), gone, reply})
	used := f.s.Used()
	d := newDen(t, 3000)
	h := f.open(1, Owner{Message: 11, Channel: 3}, d)
	f.readAll(h, d, 1000)
	h.Close()

	if got, err := f.s.Disappear(); err != nil || len(got) != 0 {
		t.Fatalf("before its time: %v, %v", got, err)
	}
	now = time.UnixMilli(soon)
	// A page leaves it out at once, before the timer's pass.
	if p := f.page(3, Query{}, 1, true); !slices.Equal(ids(p.Messages), []int64{10, 12}) {
		t.Fatalf("the page at its time: %v", ids(p.Messages))
	}
	got, err := f.s.Disappear()
	if err != nil || len(got) != 1 || got[0].ID != 11 || got[0].Channel != 3 || string(got[0].DenID) != string(denA) {
		t.Fatalf("Disappear: %+v, %v", got, err)
	}
	var held, files int
	f.s.db.QueryRow(`SELECT count(*) FROM messages WHERE id = 11`).Scan(&held)
	f.s.db.QueryRow(`SELECT count(*) FROM files`).Scan(&files)
	if held != 0 || files != 0 {
		t.Fatalf("after it went: %d copies, %d files", held, files)
	}
	if m := f.held(3, 12); m.Reply != nil {
		t.Fatalf("the reply still quotes it: %+v", m.Reply)
	}
	if f.s.Used() >= used {
		t.Errorf("the cache holds %d bytes, %d before", f.s.Used(), used)
	}

	// Brought again, by a page fetched before the den deleted it, or one
	// past its time, it stays out.
	f.addRun(3, 10, 13, true, 1, []denproto.Message{gone, sealedMsg(13, 3, soon-1)})
	if err := f.s.AppendTail(denA, sealedMsg(14, 3, soon), 1); err != nil {
		t.Fatal(err)
	}
	f.s.db.QueryRow(`SELECT count(*) FROM messages WHERE id IN (11, 13, 14)`).Scan(&held)
	if held != 0 {
		t.Errorf("%d copies past their time came back", held)
	}
}

// Told whom to tell, the cache lets each message go as its time comes, the
// timer set for each it keeps that goes sooner, and once more as it opens
// again, for what passed while it was closed.
func TestDisappearingByTimer(t *testing.T) {
	f := newFixture(t)
	told := make(chan []Gone, 4)
	f.s.OnDisappear(func(g []Gone) { told <- g })
	later := time.Now().Add(time.Hour).UnixMilli()
	f.addRun(3, 10, 11, true, 1, []denproto.Message{sealedMsg(10, 3, later), sealedMsg(11, 3, time.Now().Add(100*time.Millisecond).UnixMilli())})
	select {
	case g := <-told:
		if len(g) != 1 || g[0].ID != 11 {
			t.Fatalf("the timer let go of %+v", g)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the timer let nothing go")
	}
	f.s.mu.Lock()
	at := f.s.dis.at
	f.s.mu.Unlock()
	if at != later {
		t.Errorf("the timer goes next at %d, not %d", at, later)
	}

	// Past its time while the cache was closed, a message goes as it opens.
	if err := f.s.AppendTail(denA, sealedMsg(12, 3, time.Now().Add(50*time.Millisecond).UnixMilli()), 1); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	time.Sleep(100 * time.Millisecond)
	f.s.OnDisappear(func(g []Gone) { told <- g })
	select {
	case g := <-told:
		if len(g) != 1 || g[0].ID != 12 {
			t.Fatalf("the timer let go of %+v as the cache opened", g)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the timer let nothing go as the cache opened")
	}
}
