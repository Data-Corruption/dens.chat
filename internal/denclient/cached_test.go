package denclient_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// cachedMember joins a den as carol from an install whose client keeps a
// cache, and returns how to stop that client, which starts again on the
// same install with h.clientFrom.
func cachedMember(t *testing.T, h *denHost, owner *denclient.Manager, denID string, s store) (*denclient.Manager, func()) {
	t.Helper()
	invite, _, err := owner.CreateInvite(context.Background(), denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	m, stop := h.clientFrom(t, s, noOwnDen)
	join(t, m, invite, "carol")
	return m, stop
}

// restartMember stops a member's client and starts it again on the same
// install, as an update does, and waits for it to connect.
func restartMember(t *testing.T, h *denHost, s store, stop func()) (*denclient.Manager, func()) {
	t.Helper()
	stop()
	m, stop := h.clientFrom(t, s, noOwnDen)
	waitFor(t, m, "the member connected again", connected)
	return m, stop
}

// postFile sends a file in a message, and returns the file and message.
func postFile(t *testing.T, m *denclient.Manager, denID, channelID, name string, data []byte) (denclient.Uploaded, denproto.Message) {
	t.Helper()
	up := upload(t, m, denID, name, data)
	msg, err := m.Send(context.Background(), denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{up.ID}})
	if err != nil {
		t.Fatal(err)
	}
	return up, msg.Message
}

// look has a member's page load a channel, as it does before it shows a
// message's files, and fetch a file and its preview, if it has one.
func look(t *testing.T, m *denclient.Manager, denID, channelID string, up denclient.Uploaded) (full, thumb []byte) {
	t.Helper()
	if _, err := m.History(context.Background(), denID, channelID, denclient.HistoryQuery{}); err != nil {
		t.Fatal(err)
	}
	full, _, err := fetch(t, m, denID, up.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if up.Thumb != nil {
		if thumb, _, err = fetch(t, m, denID, up.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	return full, thumb
}

// randomBytes is n bytes of noise that Dens takes for no kind of media.
// The client sends a photo, video or audio file through the media module,
// which can't read noise, and noise looks like one now and then: an MPEG
// audio frame's two-byte sync alone, in one of a few thousand.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	for {
		rand.Read(b)
		if media.Sniff(b[:min(n, media.SniffLen)]) == media.Other {
			return b
		}
	}
}

// eventually polls until cond holds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// gone reports whether a file no longer opens: not from the cache, and not
// from the den, which deleted it.
func gone(t *testing.T, m *denclient.Manager, denID, id string) bool {
	t.Helper()
	_, _, err := fetch(t, m, denID, id, false)
	return denproto.IsCode(err, denproto.CodeNotFound)
}

func TestCachedFilesOutlastRestarts(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	photo, _ := postFile(t, owner, denID, channelID, "photo.jpg", gpsJPEG(t))
	clip, _ := postFile(t, owner, denID, channelID, "clip.bin", randomBytes(3<<20+12345))
	wantPhoto, wantThumb := look(t, member, denID, channelID, photo)
	wantClip, _ := look(t, member, denID, channelID, clip)
	if member.CacheUsage().Used < int64(len(wantClip)) {
		t.Fatalf("the cache holds %d bytes", member.CacheUsage().Used)
	}

	check := func(when string) {
		t.Helper()
		before := h.files.Load()
		// Without the den, the page can't load the channel's history
		// until M6.2 keeps it, but what it showed still opens.
		full, _, err := fetch(t, member, denID, photo.ID, false)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		thumb, _, err := fetch(t, member, denID, photo.ID, true)
		if err != nil || !bytes.Equal(full, wantPhoto) || !bytes.Equal(thumb, wantThumb) {
			t.Fatalf("%s: the photo came back changed: %v", when, err)
		}
		f, err := member.OpenFile(context.Background(), denID, clip.ID, false)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		// A player seeking reads from the middle.
		mid := make([]byte, 100_000)
		_, err = f.ReadAt(mid, 2<<20)
		f.Close()
		if err != nil || !bytes.Equal(mid, wantClip[2<<20:2<<20+100_000]) {
			t.Fatalf("%s: the middle of the clip: %v", when, err)
		}
		if got := h.files.Load() - before; got != 0 {
			t.Fatalf("%s: %d requests for files reached the den", when, got)
		}
	}
	check("again")
	h.restart()
	member, stop = restartMember(t, h, s, stop)
	check("after the member's service and the den restarted")
	h.stop()
	check("with the den away")

	// Emptying the cache, as the settings do, leaves the files to the den.
	if err := member.ClearCache(); err != nil {
		t.Fatal(err)
	}
	if usage := member.CacheUsage(); usage.Used != 0 || usage.Limit != cache.DefaultLimit {
		t.Fatalf("after emptying: %+v", usage)
	}
	if _, _, err := fetch(t, member, denID, photo.ID, false); err == nil {
		t.Fatal("an emptied cache still served a file with the den away")
	}
	stop()
}

func TestFilesThatWentLeaveTheCache(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	ctx := context.Background()
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	name := "doomed"
	if err := owner.Manage(ctx, denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	var doomed string
	viewOf(t, member, denID, "the second channel", func(v denclient.View) bool {
		for _, c := range v.Channels {
			if c.Name == name {
				doomed = c.ID
			}
		}
		return doomed != ""
	})
	a, msgA := postFile(t, owner, denID, channelID, "a.bin", randomBytes(200_000))
	b, msgB := postFile(t, owner, denID, channelID, "b.bin", randomBytes(200_000))
	c, _ := postFile(t, owner, denID, doomed, "c.bin", randomBytes(200_000))
	live, msgLive := postFile(t, owner, denID, channelID, "live.bin", randomBytes(200_000))
	for _, f := range []struct {
		up      denclient.Uploaded
		channel string
	}{{a, channelID}, {b, channelID}, {c, doomed}, {live, channelID}} {
		look(t, member, denID, f.channel, f.up)
	}

	// While the member is connected, a delete takes its file out at once.
	if err := owner.DeleteMessage(ctx, denID, msgLive.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a deleted message's file to leave the cache", func() bool { return gone(t, member, denID, live.ID) })

	// While it's away, the changes since say what went.
	stop()
	if err := owner.DeleteMessage(ctx, denID, msgA.ID); err != nil {
		t.Fatal(err)
	}
	if err := owner.Manage(ctx, denID, "channels", http.MethodDelete, doomed, nil); err != nil {
		t.Fatal(err)
	}
	h.restart()
	member, stop = restartMember(t, h, s, stop)
	defer stop()
	eventually(t, "the deleted message's file to leave the cache", func() bool { return gone(t, member, denID, a.ID) })
	eventually(t, "the deleted channel's file to leave the cache", func() bool { return gone(t, member, denID, c.ID) })
	before := h.files.Load()
	look(t, member, denID, channelID, b)
	if h.files.Load() != before {
		t.Fatal("a file nothing deleted didn't come from the cache")
	}
	_ = msgB
}

func TestANewEpochStartsTheCacheAgain(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	a, _ := postFile(t, owner, denID, channelID, "a.bin", randomBytes(300_000))
	look(t, member, denID, channelID, a)
	if member.CacheUsage().Used == 0 {
		t.Fatal("nothing cached")
	}
	stop()
	// As a restore from a backup does.
	if err := h.d.NewSyncEpoch(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.restart()
	member, stop = restartMember(t, h, s, stop)
	defer stop()
	eventually(t, "the cache to empty", func() bool { return member.CacheUsage().Used == 0 })
	before := h.files.Load()
	look(t, member, denID, channelID, a)
	if h.files.Load() == before {
		t.Fatal("a file from another epoch came from the cache")
	}
}

func TestRemovalDropsTheCache(t *testing.T) {
	h, owner, _, denID, channelID := chatDen(t)
	ctx := context.Background()
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	defer stop()
	a, _ := postFile(t, owner, denID, channelID, "a.bin", randomBytes(300_000))
	look(t, member, denID, channelID, a)
	carol := waitFor(t, member, "connected", connected)
	v, err := owner.View(denID)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, m := range v.Members {
		if m.Username == carol.Username {
			id = m.ID
		}
	}
	if err := owner.RemoveMember(ctx, denID, id, denproto.RemoveRequest{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, member, "the removal", func(s denclient.Status) bool { return s.State == denclient.StateRemoved })
	if used := member.CacheUsage().Used; used != 0 {
		t.Fatalf("the cache holds %d bytes of a den the member was removed from", used)
	}
}

func TestTheOwnDenIsntCached(t *testing.T) {
	h := startDen(t, newStore(t), time.Hour)
	code, err := h.d.Create(context.Background(), "Own Den", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := newCachedStore(t)
	owner, _ := h.clientFrom(t, s, h.own())
	var joined denclient.Joined
	for range 100 {
		joined, err = owner.JoinOwn(context.Background(), code, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "correct horse"})
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	denID := joined.Status.DenID
	waitFor(t, owner, "connected", connected)
	name := "general"
	if err := owner.Manage(context.Background(), denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	v := viewOf(t, owner, denID, "the channel", func(v denclient.View) bool { return len(v.Channels) == 1 })
	a, _ := postFile(t, owner, denID, v.Channels[0].ID, "a.bin", randomBytes(300_000))
	look(t, owner, denID, v.Channels[0].ID, a)
	if used := owner.CacheUsage().Used; used != 0 {
		t.Fatalf("the cache holds %d bytes of the install's own den", used)
	}
}
