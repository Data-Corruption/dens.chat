package denclient_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func texts(page denclient.PageHistory) []string {
	var out []string
	for _, m := range page.Messages {
		out = append(out, m.Text)
	}
	return out
}

func pageOf(t *testing.T, m *denclient.Manager, denID, channel string, q denclient.HistoryQuery) denclient.PageHistory {
	t.Helper()
	page, err := m.History(context.Background(), denID, channel, q)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestHistoryComesFromTheCache(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	for i := range 60 {
		send(t, owner, denID, channelID, fmt.Sprintf("message %d", i))
	}
	// The member joins after the channel's history, which the den sends
	// as they read it.
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	defer stop()
	before := h.history.Load()
	first := pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 50})
	if len(first.Messages) != 50 || !first.HasOlder || first.HasNewer || h.history.Load() != before+1 {
		t.Fatalf("the newest page: %d messages, %d requests", len(first.Messages), h.history.Load()-before)
	}
	older := denclient.HistoryQuery{Before: first.Messages[0].ID, Limit: 50}
	if p := pageOf(t, member, denID, channelID, older); len(p.Messages) != 10 || p.HasOlder || h.history.Load() != before+2 {
		t.Fatalf("the older page: %d messages, older %v, %d requests", len(p.Messages), p.HasOlder, h.history.Load()-before)
	}
	// Both come from the cache now, and so does the newest message as it
	// arrives.
	again := pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 50})
	if !slices.Equal(texts(again), texts(first)) {
		t.Fatalf("the newest page from the cache: %v", texts(again))
	}
	if p := pageOf(t, member, denID, channelID, older); len(p.Messages) != 10 || p.HasOlder {
		t.Fatalf("the older page from the cache: %d messages, older %v", len(p.Messages), p.HasOlder)
	}
	// What arrives while the member is connected goes into the cache as it
	// comes, in every channel, so even a channel they haven't opened reads
	// without asking the den.
	name := "random"
	if err := owner.Manage(context.Background(), denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	var random string
	viewOf(t, member, denID, "#random", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == name {
				random = c.ID
			}
		}
		return random != ""
	})
	stream, stopStream := member.Stream()
	defer stopStream()
	latest := send(t, owner, denID, channelID, "the latest")
	for i := range 3 {
		send(t, owner, denID, random, fmt.Sprintf("random %d", i))
	}
	collect(t, stream, 4)
	if p := pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 2}); p.Messages[1].ID != latest.ID {
		t.Fatalf("the newest after an event: %v", texts(p))
	}
	if p := pageOf(t, member, denID, random, denclient.HistoryQuery{Limit: 50}); !slices.Equal(texts(p), []string{"random 0", "random 1", "random 2"}) {
		t.Fatalf("a channel never opened: %v", texts(p))
	}
	if got := h.history.Load() - before; got != 2 {
		t.Fatalf("%d requests for history, not the 2 pages the cache didn't hold", got)
	}
}

// The done-when of M6: a client restarted after a day offline downloads
// only what changed.
func TestADayAwayDownloadsOnlyWhatChanged(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	ctx := context.Background()
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	var sent []denproto.Message
	for i := range 30 {
		sent = append(sent, send(t, owner, denID, channelID, fmt.Sprintf("message %d", i)))
	}
	photo, _ := postFile(t, owner, denID, channelID, "photo.jpg", gpsJPEG(t))
	look(t, member, denID, channelID, photo)

	// A day away: the member's service is off, the den restarts and its
	// ring of events is gone.
	stop()
	edited := sent[10]
	if _, err := owner.Edit(ctx, denID, channelID, edited.ID, denproto.EditRequest{Revision: 1, Text: "edited while away"}); err != nil {
		t.Fatal(err)
	}
	if err := owner.DeleteMessage(ctx, denID, sent[20].ID); err != nil {
		t.Fatal(err)
	}
	var fresh []string
	for i := range 3 {
		fresh = append(fresh, send(t, owner, denID, channelID, fmt.Sprintf("new %d", i)).ID)
	}
	name := "Alice Away"
	if _, err := owner.UpdateProfile(ctx, denID, denproto.ProfileRequest{DisplayName: &name}); err != nil {
		t.Fatal(err)
	}
	h.restart()
	historyBefore, changesBefore, filesBefore := h.history.Load(), h.changes.Load(), h.files.Load()
	member, stop = restartMember(t, h, s, stop)
	defer stop()

	// The snapshot sent only the members that changed.
	h.upgradeMu.Lock()
	cursor := h.upgrades[len(h.upgrades)-1]
	h.upgradeMu.Unlock()
	if cursor == "" {
		t.Fatal("the member's upgrade didn't name its list of members")
	}
	viewOf(t, member, denID, "the owner's new name, and everyone", func(v denclient.View) bool {
		return len(v.Members) == 3 && slices.ContainsFunc(v.Members, func(m denproto.Member) bool { return m.DisplayName == name })
	})

	// The channel's newest page: the new messages, and the edit and delete
	// applied to what the cache held.
	var page denclient.PageHistory
	eventually(t, "the newest page with what changed", func() bool {
		page = pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 50})
		got := texts(page)
		return slices.Contains(got, "edited while away") && !slices.Contains(got, "message 20") &&
			strings.HasPrefix(got[len(got)-1], "new 2")
	})
	if got := h.history.Load() - historyBefore; got != 1 {
		t.Fatalf("%d requests for history, not the one page of what came while away", got)
	}
	if got := h.changes.Load() - changesBefore; got != 1 {
		t.Fatalf("%d requests for changes", got)
	}
	look2, _, err := fetch(t, member, denID, photo.ID, false)
	if err != nil || len(look2) == 0 || h.files.Load() != filesBefore {
		t.Fatalf("the photo after the day away: %v, %d requests for files", err, h.files.Load()-filesBefore)
	}
	_ = fresh
}

func TestOfflineReading(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	for i := range 5 {
		send(t, owner, denID, channelID, fmt.Sprintf("message %d", i))
	}
	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	sendDM(t, owner, denID, dm, "a private word")
	online := pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 50})
	if p := pageOf(t, member, denID, dm, denclient.HistoryQuery{Limit: 50}); len(p.Messages) != 1 || p.Messages[0].Text != "a private word" {
		t.Fatalf("the DM online: %+v", p.Messages)
	}

	// The member's service starts where it can't reach the den: example.com
	// leads to a server that hangs up at once.
	stop()
	member, stop = h.clientVia(t, s, noOwnDen, hangUp(t))
	defer stop()
	v := viewOf(t, member, denID, "the den as it last was", func(v denclient.View) bool {
		return len(v.Channels) == 2 && len(v.Members) == 3
	})
	if v.State == denclient.StateConnected {
		t.Fatal("a den that's away says it's connected")
	}
	if p := pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 50}); !slices.Equal(texts(p), texts(online)) || p.HasNewer {
		t.Fatalf("the channel offline: %v", texts(p))
	}
	if p := pageOf(t, member, denID, dm, denclient.HistoryQuery{Limit: 50}); len(p.Messages) != 1 || p.Messages[0].Text != "a private word" {
		t.Fatalf("the DM offline: %+v", p.Messages)
	}
	// What the cache doesn't hold needs the den, and says so at once.
	waitFor(t, member, "the den offline", func(s denclient.Status) bool { return s.State == denclient.StateOffline })
	start := time.Now()
	if _, err := member.History(context.Background(), denID, channelID, denclient.HistoryQuery{Before: online.Messages[0].ID}); err != denclient.ErrOffline {
		t.Fatalf("history the cache doesn't hold, without the den: %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("saying the den is offline took %v", took)
	}
}

func TestRetentionReachesTheCache(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	ctx := context.Background()
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	defer stop()
	old := send(t, owner, denID, channelID, "old news")
	young := send(t, owner, denID, channelID, "young")
	pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 50})
	if _, err := h.s.db.Exec(`UPDATE den_messages SET created_at = 1 WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}
	days := 1
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Retention: &days}); err != nil {
		t.Fatal(err)
	}
	h.d.StartRetention()
	eventually(t, "the old message out of the member's cache", func() bool {
		p := pageOf(t, member, denID, channelID, denclient.HistoryQuery{Limit: 50})
		return len(p.Messages) == 1 && p.Messages[0].ID == young.ID && !p.HasOlder
	})
}

// hangUp listens on loopback and closes every connection as it comes, and
// returns its address: a den that can't be reached, without waiting out a
// connection's time limit.
func hangUp(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return ln.Addr().String()
}
