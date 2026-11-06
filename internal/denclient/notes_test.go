package denclient_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// openNotes opens a member's notes and waits until their Dens has them.
func openNotes(t *testing.T, m *denclient.Manager, denID string) denproto.Channel {
	t.Helper()
	viewOf(t, m, denID, "notes offered", func(v denclient.View) bool { return v.HasNotes })
	notes, err := m.OpenNotes(context.Background(), denID)
	if err != nil {
		t.Fatal(err)
	}
	viewOf(t, m, denID, "the notes", func(v denclient.View) bool {
		return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == notes.ID })
	})
	return notes
}

// A member's notes, written with a photo on one device, read on another of
// theirs, which ticks, replies, edits and deletes there too: the den holds
// neither the text nor the photo in the clear, another member sees nothing
// of them, and nothing in them is unread. Once the member starts over, the
// notes from before show as lost, for them to delete, and new ones open.
func TestNotesAcrossDevices(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	notes := openNotes(t, member, denID)
	bob := me(t, member, denID)
	if notes.Kind != denproto.KindNotes || !slices.Equal(notes.Members, []string{bob.ID}) {
		t.Fatalf("bob's notes: %+v", notes)
	}
	if again, err := member.OpenNotes(ctx, denID); err != nil || again.ID != notes.ID {
		t.Fatalf("bob's notes opened again: %+v, %v", again, err)
	}

	photo := gpsJPEG(t)
	up, err := member.Upload(ctx, denID, notes.ID, "IMG_3001.jpg", int64(len(photo)), bytes.NewReader(photo))
	if err != nil || !up.Stripped || up.Thumb == nil || up.Type != "image/jpeg" {
		t.Fatalf("a photo for the notes: %+v %v", up, err)
	}
	first, err := member.Send(ctx, denID, notes.ID, denproto.SendRequest{Nonce: denproto.Random(16),
		Text: "the receipt:\n[ ] return the lamp", Attachments: []string{up.ID}})
	if err != nil || first.Text != "the receipt:\n[ ] return the lamp" || first.Locked != "" || first.KeyID != notes.ID || len(first.Attachments) != 1 {
		t.Fatalf("a note: %+v %v", first, err)
	}
	var stored []byte
	if err := h.s.db.QueryRow(`SELECT text FROM den_messages WHERE id = ?`, first.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("receipt")) || len(stored) < 40 {
		t.Fatalf("the den stores the note as %q", stored)
	}

	laptop := h.client(t, noOwnDen)
	signInApproved(t, laptop, member, denID, denclient.SignInRequest{Den: "https://example.com", Username: "bob", Password: "correct horse"}, false)
	waitFor(t, laptop, "the laptop connected", connected)
	viewOf(t, laptop, denID, "the notes on the laptop", func(v denclient.View) bool {
		return v.HasNotes && slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == notes.ID })
	})
	got := history(t, laptop, denID, notes.ID)
	if len(got) != 1 || got[0].Text != first.Text || got[0].Locked != "" || len(got[0].Attachments) != 1 || got[0].Attachments[0].Name != "IMG_3001.jpg" {
		t.Fatalf("the notes on the laptop: %+v", got)
	}
	data, kind, err := fetch(t, laptop, denID, up.ID, false)
	if err != nil || kind != media.JPEG || bytes.Contains(data, []byte("GPSLatitude")) || int64(len(data)) != up.Size {
		t.Fatalf("the laptop's copy of the photo: %d bytes, %v, %v", len(data), kind, err)
	}

	ticked, err := laptop.SetTask(ctx, denID, notes.ID, first.ID, 0, denproto.TaskRequest{Checked: true, Text: "return the lamp"})
	if err != nil || ticked.Text != "the receipt:\n[x] return the lamp" || ticked.EditedAt != 0 {
		t.Fatalf("a tick on the laptop: %+v %v", ticked, err)
	}
	reply, err := laptop.Send(ctx, denID, notes.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "done", ReplyTo: first.ID})
	if err != nil || reply.Reply == nil || reply.Reply.Text != ticked.Text || reply.Reply.Locked {
		t.Fatalf("a reply on the laptop: %+v %v", reply, err)
	}
	edited, err := laptop.Edit(ctx, denID, notes.ID, reply.ID, denproto.EditRequest{Revision: 1, Text: "done, refunded"})
	if err != nil || edited.Text != "done, refunded" || edited.EditedAt == 0 {
		t.Fatalf("an edit on the laptop: %+v %v", edited, err)
	}
	typo, err := laptop.Send(ctx, denID, notes.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "tpyo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := laptop.DeleteMessage(ctx, denID, typo.ID); err != nil {
		t.Fatal(err)
	}
	got = history(t, member, denID, notes.ID)
	if len(got) != 2 || got[0].Text != ticked.Text || got[1].Text != "done, refunded" || got[1].Reply == nil || got[1].Reply.Text != ticked.Text {
		t.Fatalf("the notes back on the phone: %+v", got)
	}
	for _, s := range member.Statuses() {
		if s.DenID == denID && (s.Unread != 0 || s.Mentions != 0) {
			t.Errorf("the phone marks %d unread and %d mentions from the laptop's notes", s.Unread, s.Mentions)
		}
	}

	// What notes don't take.
	if _, err := member.React(ctx, denID, notes.ID, first.ID, denclient.PageReaction{Emoji: "👍", On: true}); !isInput(err) {
		t.Errorf("a reaction in the notes: %v", err)
	}
	if _, err := member.Send(ctx, denID, notes.ID, denproto.SendRequest{Nonce: denproto.Random(16),
		Poll: &denproto.PollRequest{Choices: []string{"a", "b"}, Hours: 1}}); err == nil {
		t.Error("the notes took a poll")
	}

	// Alice sees nothing of them.
	v, err := owner.View(denID)
	if err != nil || slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.Kind == denproto.KindNotes && c.ID == notes.ID }) {
		t.Errorf("alice's view holds bob's notes: %+v, %v", v.Channels, err)
	}
	if _, err := owner.History(ctx, denID, notes.ID, denclient.HistoryQuery{}); err == nil {
		t.Error("alice read bob's notes")
	}

	// Starting over leaves the notes from before shut.
	if _, err := laptop.StartOver(ctx, denID, "correct horse"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, member, "the first device signed out", revoked("You reset your DM recovery key"))
	got = history(t, laptop, denID, notes.ID)
	if len(got) != 2 || got[0].Locked != "lost" || got[1].Locked != "lost" || got[0].Text != "" || len(got[0].Attachments) != 0 {
		t.Fatalf("the notes after starting over: %+v", got)
	}
	if err := laptop.DeleteMessage(ctx, denID, got[0].ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := laptop.Send(ctx, denID, notes.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "a fresh start"})
	if err != nil || fresh.Locked != "" || fresh.Text != "a fresh start" {
		t.Fatalf("a note after starting over: %+v %v", fresh, err)
	}
	got = history(t, laptop, denID, notes.ID)
	if len(got) != 2 || got[0].Locked != "lost" || got[1].Text != "a fresh start" {
		t.Fatalf("the notes after a fresh one: %+v", got)
	}
}
