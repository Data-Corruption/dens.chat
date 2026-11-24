package denclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// secondDen starts Second Den, made by carol's install, which m joins as
// bob, with a channel, lobby. m reaches it at example.com at its own
// port.
func secondDen(t *testing.T, m *denclient.Manager) (h *denHost, carol *denclient.Manager, denID, lobby string) {
	t.Helper()
	ctx := context.Background()
	h = startDen(t, newStore(t), time.Hour)
	h.d.RelaxLimits()
	_, port, _ := net.SplitHostPort(h.public.Listener.Addr().String())
	code, err := h.d.Create(ctx, "Second Den", "https://example.com:"+port)
	if err != nil {
		t.Fatal(err)
	}
	carol = h.client(t, h.own())
	var joined denclient.Status
	for range 100 {
		joined, err = status(carol.JoinOwn(ctx, code, denclient.JoinRequest{Username: "carol", DisplayName: "Carol", Password: "correct horse"}))
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	denID = joined.DenID
	invite, _, err := carol.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Join(ctx, invite, denclient.JoinRequest{Username: "bob", DisplayName: "Bob", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	name := "lobby"
	if err := carol.Manage(ctx, denID, "channels", "POST", "", denproto.ChannelRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	for _, who := range []*denclient.Manager{carol, m} {
		lobby = channelIn(t, who, denID, "lobby")
	}
	return h, carol, denID, lobby
}

// channelIn waits for a den's channel of that name to reach a member's
// Dens, and returns its ID.
func channelIn(t *testing.T, m *denclient.Manager, denID, name string) string {
	t.Helper()
	v := viewOf(t, m, denID, "#"+name, func(v denclient.View) bool {
		return v.Status.State == denclient.StateConnected && channelNamed(v, name)
	})
	return v.Channels[slices.IndexFunc(v.Channels, func(c denproto.Channel) bool { return c.Name == name })].ID
}

func forward(t *testing.T, m *denclient.Manager, denID, channel, message, toDen, place string) denclient.PageMessage {
	t.Helper()
	fwd, err := m.Forward(context.Background(), denID, channel, message, denclient.ForwardTo{Den: toDen, Channel: place, Nonce: denproto.Random(16)})
	if err != nil {
		t.Fatalf("forward to %s: %v", place, err)
	}
	return fwd
}

// lastSent is what the den was last sent as a message, by its fields, and
// as it came.
func lastSent(t *testing.T, h *denHost) (map[string]json.RawMessage, string) {
	t.Helper()
	h.upgradeMu.Lock()
	defer h.upgradeMu.Unlock()
	var body map[string]json.RawMessage
	if len(h.sends) == 0 || json.Unmarshal(h.sends[len(h.sends)-1], &body) != nil {
		t.Fatalf("no send to read: %d", len(h.sends))
	}
	return body, string(h.sends[len(h.sends)-1])
}

func keysOf(m map[string]json.RawMessage) []string {
	return slices.Sorted(maps.Keys(m))
}

// newest is a place's newest message as a member's Dens opens it.
func newest(t *testing.T, m *denclient.Manager, denID, channel string) denclient.PageMessage {
	t.Helper()
	got := history(t, m, denID, channel)
	if len(got) == 0 {
		t.Fatalf("nothing in %s", channel)
	}
	return got[len(got)-1]
}

// sameFile checks that a forward's file opens for its reader as the
// original does for its own, its preview too, under an ID of its own in
// the same den.
func sameFile(t *testing.T, what string, from *denclient.Manager, fromDen string, orig denproto.File, to *denclient.Manager, toDen string, got denproto.File) {
	t.Helper()
	if fromDen == toDen && got.ID == orig.ID || got.Name != orig.Name || got.Type != orig.Type || got.Spoiler != orig.Spoiler || (got.Thumb == nil) != (orig.Thumb == nil) {
		t.Errorf("%s: %+v, from %+v", what, got, orig)
		return
	}
	want, _, err := fetch(t, from, fromDen, orig.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := fetch(t, to, toDen, got.ID, false)
	if err != nil || !bytes.Equal(data, want) {
		t.Errorf("%s opens as %d bytes, %v, not the original's %d", what, len(data), err, len(want))
	}
	if got.Thumb != nil {
		if _, kind, err := fetch(t, to, toDen, got.ID, true); err != nil || !kind.Image() {
			t.Errorf("%s's preview: %v, %v", what, kind, err)
		}
	}
}

// A message's text and files forward from a channel to a DM, a group and
// the member's notes, and from the DM back to a channel, each as its
// sender's own, marked, with its spoiler, and nothing of where it came
// from, its reply, its author, its channel, to the den or in the seal.
// What reads it opens its photo; nobody edits or ticks it after. A DM's
// mark is its seal's, never the den's, and a page's send marks nothing.
func TestForwardWithItsFiles(t *testing.T) {
	h, ms, ids, denID := groupDen(t)
	alice, bob, carol := ms[0], ms[1], ms[2]
	ctx := context.Background()
	general := channelIn(t, alice, denID, "general")
	photo := upload(t, alice, denID, "IMG_1234.jpg", gpsJPEG(t))
	doc := upload(t, alice, denID, "list.txt", []byte("milk\neggs\n"))
	first := send(t, alice, denID, general, "first")
	src, err := alice.Send(ctx, denID, general, denproto.SendRequest{Nonce: denproto.Random(16), Text: "[ ] the beach ||at noon||",
		ReplyTo: first.ID, Attachments: []string{photo.ID, doc.ID}, Spoilers: []string{photo.ID}})
	if err != nil || len(src.Attachments) != 2 || !src.Attachments[0].Spoiler {
		t.Fatalf("the original: %+v %v", src, err)
	}

	dm := openDM(t, bob, alice, denID)
	readyDM(t, bob, alice, denID, dm)
	g, err := bob.MakeGroup(ctx, denID, []string{ids[2], ids[3]}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[1:] {
		groupOf(t, m, denID, g, ids[1:])
	}
	notes := openNotes(t, bob, denID)

	bobID := me(t, bob, denID).ID
	for _, place := range []struct {
		name, id string
		reader   *denclient.Manager
	}{{"the DM", dm, alice}, {"the group", g, carol}, {"bob's notes", notes.ID, bob}} {
		fwd := forward(t, bob, denID, general, src.ID, denID, place.id)
		if !fwd.Forwarded || fwd.AuthorID != bobID || fwd.Text != src.Text || fwd.ReplyTo != "" || len(fwd.Attachments) != 2 {
			t.Fatalf("forwarded to %s: %+v", place.name, fwd)
		}
		if body, _ := lastSent(t, h); !slices.Equal(keysOf(body), []string{"attachments", "key_id", "nonce", "sealed", "text"}) {
			t.Errorf("forwarded to %s, the den was sent %v", place.name, keysOf(body))
		}
		p, err := denclient.PayloadOf(place.reader, denID, place.id, fwd.ID)
		if err != nil || !p.Forwarded || p.Grant != nil || p.Text != src.Text || len(p.Files) != 2 {
			t.Errorf("forwarded to %s, its seal holds %+v, %v", place.name, p, err)
		}
		got := newest(t, place.reader, denID, place.id)
		if got.ID != fwd.ID || !got.Forwarded || got.Locked != "" || got.EditedBy != "" || len(got.Editors) != 0 || got.Text != src.Text {
			t.Fatalf("%s reads %+v", place.name, got)
		}
		for i := range src.Attachments {
			sameFile(t, place.name+"'s file", alice, denID, src.Attachments[i], place.reader, denID, got.Attachments[i])
		}
	}

	// From the DM to a channel, its spoiler kept by the den.
	inDM := newest(t, bob, denID, dm)
	back := forward(t, bob, denID, dm, inDM.ID, denID, general)
	if body, _ := lastSent(t, h); !slices.Equal(keysOf(body), []string{"attachments", "forwarded", "nonce", "spoilers", "text"}) {
		t.Errorf("forwarded to general, the den was sent %v", keysOf(body))
	}
	got := newest(t, alice, denID, general)
	if got.ID != back.ID || !got.Forwarded || got.Text != src.Text || len(got.Attachments) != 2 {
		t.Fatalf("general reads %+v", got)
	}
	for i := range inDM.Attachments {
		sameFile(t, "general's file", bob, denID, inDM.Attachments[i], alice, denID, got.Attachments[i])
	}

	// A sealed place's mark is its seal's alone, and only a forward
	// marks a message.
	plain := sendDM(t, bob, denID, dm, "plain")
	if _, err := h.s.db.Exec(`UPDATE den_messages SET forwarded = 1 WHERE id = ?`, plain.ID); err != nil {
		t.Fatal(err)
	}
	if got := newest(t, alice, denID, dm); got.ID != plain.ID || got.Forwarded {
		t.Errorf("a DM's message the den marked reads %+v", got)
	}
	if m, err := bob.Send(ctx, denID, general, denproto.SendRequest{Nonce: denproto.Random(16), Text: "mine", Forwarded: true}); err != nil || m.Forwarded {
		t.Errorf("a send the page marked: %+v %v", m, err)
	}

	// A forward is its sender's to delete, not to edit, share or tick.
	for _, m := range []denclient.PageMessage{back, inDM} {
		if _, err := bob.Edit(ctx, denID, m.ChannelID, m.ID, denproto.EditRequest{Revision: m.Revision, Text: "mine now"}); err == nil {
			t.Errorf("bob edited a forward in %s", m.ChannelID)
		}
		others := []string{ids[0]}
		if _, err := bob.Edit(ctx, denID, m.ChannelID, m.ID, denproto.EditRequest{Revision: m.Revision, Text: m.Text, Editors: &others}); err == nil {
			t.Errorf("bob shared a forward in %s", m.ChannelID)
		}
		if _, err := bob.SetTask(ctx, denID, m.ChannelID, m.ID, 0, denproto.TaskRequest{Checked: true, Text: "the beach ||at noon||"}); err == nil {
			t.Errorf("bob ticked a forward in %s", m.ChannelID)
		}
		if err := bob.DeleteMessage(ctx, denID, m.ID); err != nil {
			t.Errorf("bob deleting his forward in %s: %v", m.ChannelID, err)
		}
	}
}

// A video the media module can't decode forwards with the preview its
// original had, which the page drew.
func TestForwardKeepsAPagesPreview(t *testing.T) {
	_, alice, bob, denID, general := chatDen(t)
	ctx := context.Background()
	clip := upload(t, alice, denID, "clip.mp4", mediaFile(t, "meta.mp4"))
	img := testPreview(t, 64, 48)
	if _, err := alice.SetThumb(ctx, denID, clip.ID, int64(len(img)), bytes.NewReader(img)); err != nil {
		t.Fatal(err)
	}
	attach(t, alice, denID, general, clip.ID)
	src := newest(t, bob, denID, general)
	dm := openDM(t, bob, alice, denID)
	readyDM(t, bob, alice, denID, dm)
	fwd := forward(t, bob, denID, general, src.ID, denID, dm)
	got := newest(t, alice, denID, dm)
	if got.ID != fwd.ID || len(got.Attachments) != 1 || got.Attachments[0].Thumb == nil {
		t.Fatalf("the DM reads %+v", got)
	}
	sameFile(t, "the clip", bob, denID, src.Attachments[0], alice, denID, got.Attachments[0])
}

// A forward goes to another den, from a channel and from a DM, where
// another member reads it, marked, and opens its photo.
func TestForwardToAnotherDen(t *testing.T) {
	_, alice, bob, denID, general := chatDen(t)
	h2, carol, den2, lobby := secondDen(t, bob)
	src, err := alice.Send(context.Background(), denID, general, denproto.SendRequest{Nonce: denproto.Random(16), Text: "look",
		Attachments: []string{upload(t, alice, denID, "IMG_1234.jpg", gpsJPEG(t)).ID}})
	if err != nil {
		t.Fatal(err)
	}
	fwd := forward(t, bob, denID, general, src.ID, den2, lobby)
	got := newest(t, carol, den2, lobby)
	if got.ID != fwd.ID || !got.Forwarded || got.Text != "look" || len(got.Attachments) != 1 {
		t.Fatalf("lobby reads %+v", got)
	}
	sameFile(t, "lobby's photo", alice, denID, src.Attachments[0], carol, den2, got.Attachments[0])
	if body, raw := lastSent(t, h2); !slices.Equal(keysOf(body), []string{"attachments", "forwarded", "nonce", "text"}) || strings.Contains(raw, denID) {
		t.Errorf("Second Den was sent %s", raw)
	}

	dm := openDM(t, bob, alice, denID)
	readyDM(t, bob, alice, denID, dm)
	inDM := forward(t, bob, denID, general, src.ID, denID, dm)
	theirs := openDM(t, bob, carol, den2)
	readyDM(t, bob, carol, den2, theirs)
	across := forward(t, bob, denID, dm, inDM.ID, den2, theirs)
	got = newest(t, carol, den2, theirs)
	if got.ID != across.ID || !got.Forwarded || got.Text != "look" || len(got.Attachments) != 1 {
		t.Fatalf("carol's DM reads %+v", got)
	}
	sameFile(t, "the DM's photo", bob, denID, inDM.Attachments[0], carol, den2, got.Attachments[0])
}

// A member whose Dens keeps a cache forwards a DM's photo, read through
// the cache, to another den, where it opens. Started again while that den
// is down, its state from the cache says nothing of forwards: Forward says
// the den is offline, not that it's too old.
func TestForwardThroughTheCache(t *testing.T) {
	h, alice, _, denID, _ := chatDen(t)
	ctx := context.Background()
	s := newCachedStore(t)
	erin, stop := h.clientFrom(t, s, noOwnDen)
	invite, _, err := alice.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	join(t, erin, invite, "erin")
	h2, carol, den2, lobby := secondDen(t, erin)
	dm := openDM(t, alice, erin, denID)
	readyDM(t, alice, erin, denID, dm)
	photo := gpsJPEG(t)
	up, err := alice.Upload(ctx, denID, dm, "IMG_1234.jpg", int64(len(photo)), bytes.NewReader(photo))
	if err != nil {
		t.Fatal(err)
	}
	attach(t, alice, denID, dm, up.ID)
	var src denclient.PageMessage
	for deadline := time.Now().Add(5 * time.Second); src.ID == ""; time.Sleep(20 * time.Millisecond) {
		if got := history(t, erin, denID, dm); len(got) > 0 {
			src = got[len(got)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the photo never reached erin")
		}
	}
	fwd := forward(t, erin, denID, dm, src.ID, den2, lobby)
	got := newest(t, carol, den2, lobby)
	if got.ID != fwd.ID || !got.Forwarded || len(got.Attachments) != 1 {
		t.Fatalf("lobby reads %+v", got)
	}
	sameFile(t, "lobby's photo", erin, denID, src.Attachments[0], carol, den2, got.Attachments[0])

	h2.stop()
	stop()
	erin, _ = h.clientFrom(t, s, noOwnDen)
	viewOf(t, erin, den2, "Second Den from the cache", func(v denclient.View) bool { return channelNamed(v, "lobby") })
	viewOf(t, erin, denID, "Chat Den", func(v denclient.View) bool { return v.Status.State == denclient.StateConnected })
	_, err = erin.Forward(ctx, denID, dm, src.ID, denclient.ForwardTo{Den: den2, Channel: lobby, Nonce: denproto.Random(16)})
	if !isInput(err) || !strings.Contains(err.Error(), "offline") {
		t.Errorf("forwarded to a den that's down: %v", err)
	}
}

// A file larger than the place's den takes, or past the member's space
// there, sends nothing: nothing goes up, and no message goes.
func TestForwardWithinThePlacesLimits(t *testing.T) {
	_, alice, bob, denID, general := chatDen(t)
	h2, carol, den2, lobby := secondDen(t, bob)
	ctx := context.Background()
	limits := denproto.Limits{FileSize: 1 << 20, MemberStorage: 3 << 19, DenStorage: 1 << 30}
	if err := carol.UpdateDen(ctx, den2, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, bob, den2, "the new limits", func(v denclient.View) bool { return v.Limits == limits })
	theirs := openDM(t, bob, carol, den2)
	readyDM(t, bob, carol, den2, theirs)
	// post sends a message of files of those sizes, a small one first,
	// which a forward would upload before it reached the last.
	post := func(sizes ...int) denclient.PageMessage {
		var ids []string
		for _, n := range append([]int{1000}, sizes...) {
			ids = append(ids, upload(t, alice, denID, "data.bin", denproto.Random(n)).ID)
		}
		m, err := alice.Send(ctx, denID, general, denproto.SendRequest{Nonce: denproto.Random(16), Text: "data", Attachments: ids})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	stored := func() int64 { return h2.uploads.Load() }
	// refused forwards m to place, which refuses it with want, or as
	// input with none.
	refused := func(what string, m denclient.PageMessage, place string, want error) {
		t.Helper()
		before, sent := stored(), len(history(t, carol, den2, place))
		_, err := bob.Forward(ctx, denID, general, m.ID, denclient.ForwardTo{Den: den2, Channel: place, Nonce: denproto.Random(16)})
		if err == nil || want != nil && !errors.Is(err, want) || want == nil && !isInput(err) {
			t.Errorf("%s: %v", what, err)
		}
		if after := stored(); after != before || len(history(t, carol, den2, place)) != sent {
			t.Errorf("%s made %d uploads, and the place holds %d messages, %d before", what, after-before, len(history(t, carol, den2, place)), sent)
		}
	}
	big := post(1<<20 + 1)
	refused("too large for lobby", big, lobby, denclient.ErrTooLarge)
	// Sealed, a file a little under the limit takes more than it.
	edge := post(1<<20 - 16)
	refused("too large sealed", edge, theirs, denclient.ErrTooLarge)
	forward(t, bob, denID, general, edge.ID, den2, lobby)
	refused("past bob's space", post(1<<19+1024), lobby, nil)
}

// What can't go says so: a poll; a message to a channel whose den keeps
// no forward's mark, though a DM there takes it; and a DM that holds since
// the other member's keys changed.
func TestForwardRefusals(t *testing.T) {
	_, ms, ids, denID := groupDen(t)
	alice, bob := ms[0], ms[1]
	ctx := context.Background()
	general := channelIn(t, alice, denID, "general")
	poll, err := alice.Send(ctx, denID, general, denproto.SendRequest{Nonce: denproto.Random(16), Text: "Lunch?",
		Poll: &denproto.PollRequest{Choices: []string{"Tacos", "Pho"}, Hours: 1, ShowVoters: denproto.VotersNever, ShowTotals: denproto.TotalsLive}})
	if err != nil {
		t.Fatal(err)
	}
	dm := openDM(t, bob, alice, denID)
	readyDM(t, bob, alice, denID, dm)
	if _, err := bob.Forward(ctx, denID, general, poll.ID, denclient.ForwardTo{Den: denID, Channel: dm, Nonce: denproto.Random(16)}); !isInput(err) {
		t.Errorf("a poll forwarded: %v", err)
	}
	plain := send(t, alice, denID, general, "plain")
	if err := denclient.ForgetForward(bob, denID); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.Forward(ctx, denID, general, plain.ID, denclient.ForwardTo{Den: denID, Channel: general, Nonce: denproto.Random(16)}); !isInput(err) {
		t.Errorf("forwarded to a den that keeps no mark: %v", err)
	}
	forward(t, bob, denID, general, plain.ID, denID, dm)

	if _, err := alice.Compare(ctx, denID, ids[1], codeOf(t, bob, denID, ids[0]).Mine); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.StartOver(ctx, denID, "correct horse"); err != nil {
		t.Fatal(err)
	}
	theirs := sendDM(t, bob, denID, dm, "a new key")
	viewOf(t, alice, denID, "bob's keys changed, and his key", func(v denclient.View) bool {
		return v.Identities[ids[1]].Held && slices.ContainsFunc(v.DMKeys, func(k denclient.PageKey) bool { return k.ID == theirs.KeyID })
	})
	if _, err := alice.Forward(ctx, denID, general, plain.ID, denclient.ForwardTo{Den: denID, Channel: dm, Nonce: denproto.Random(16)}); !isInput(err) ||
		!strings.Contains(err.Error(), "keys changed") {
		t.Errorf("forwarded into a DM that holds: %v", err)
	}
}
