package denclient_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// readyDM waits until both members' Dens have a DM, have published their
// identities, and kept each other's, so either's first message makes its
// key, with nothing to type (M16.1).
func readyDM(t *testing.T, a, b *denclient.Manager, denID, dm string) {
	t.Helper()
	ida, idb := me(t, a, denID).ID, me(t, b, denID).ID
	for _, side := range []struct {
		m     *denclient.Manager
		other string
	}{{a, idb}, {b, ida}} {
		viewOf(t, side.m, denID, "the DM, with the other's identity kept", func(v denclient.View) bool {
			o, ok := memberIn(v, side.other)
			_, kept := v.Identities[side.other]
			return ok && o.Identity != "" && kept && v.Me.Identity != "" &&
				slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == dm })
		})
	}
}

// openDM opens a DM from a to b and waits until both Dens have it.
func openDM(t *testing.T, a, b *denclient.Manager, denID string) string {
	t.Helper()
	dm, err := a.OpenDM(context.Background(), denID, me(t, b, denID).ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []*denclient.Manager{a, b} {
		viewOf(t, m, denID, "the DM", func(v denclient.View) bool {
			return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == dm.ID })
		})
	}
	return dm.ID
}

func sendDM(t *testing.T, m *denclient.Manager, denID, dm, text string) denclient.PageMessage {
	t.Helper()
	msg, err := m.Send(context.Background(), denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func history(t *testing.T, m *denclient.Manager, denID, channel string) []denclient.PageMessage {
	t.Helper()
	page, err := m.History(context.Background(), denID, channel, denclient.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return page.Messages
}

// A DM's text and files leave each member's Dens sealed: the den stores
// what it can't open, and the other member reads it all, with replies,
// edits, ticks and photos, which the sender's Dens strips and previews.
func TestDMsStaySealed(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)

	first := sendDM(t, owner, denID, dm, "the secret list:\n[ ] milk\n[ ] eggs")
	if first.Text != "the secret list:\n[ ] milk\n[ ] eggs" || first.Locked != "" || first.KeyID == "" || first.Sealed != nil {
		t.Fatalf("sent: %+v", first)
	}
	var stored []byte
	if err := h.s.db.QueryRow(`SELECT text FROM den_messages WHERE id = ?`, first.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("secret")) || len(stored) < 40 {
		t.Fatalf("the den stores the DM's text as %q", stored)
	}
	// Not even the den's own data key opens it, as it does a channel's.
	if _, err := h.s.v.Open(stored, []byte("den_messages.text:"+first.ID)); err == nil {
		t.Fatal("the den's data key opens the DM's text")
	}
	got := history(t, member, denID, dm)
	if len(got) != 1 || got[0].Text != first.Text || got[0].Locked != "" || got[0].Nonce != nil {
		t.Fatalf("bob's history: %+v", got)
	}
	reply, err := member.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "on it", ReplyTo: first.ID})
	if err != nil || reply.Reply == nil || reply.Reply.Text != first.Text || reply.Reply.Locked || reply.Message.Reply != nil {
		t.Fatalf("a reply: %+v %v", reply, err)
	}

	edited, err := owner.Edit(ctx, denID, dm, first.ID, denproto.EditRequest{Revision: 1, Text: "the list:\n[ ] milk\n[ ] eggs"})
	if err != nil || edited.Revision != 2 || edited.EditedAt == 0 || edited.Text != "the list:\n[ ] milk\n[ ] eggs" {
		t.Fatalf("an edit: %+v %v", edited, err)
	}
	ticked, err := owner.SetTask(ctx, denID, dm, first.ID, 1, denproto.TaskRequest{Checked: true, Text: "eggs"})
	if err != nil || ticked.Revision != 3 || ticked.Text != "the list:\n[ ] milk\n[x] eggs" || ticked.EditedAt != edited.EditedAt {
		t.Fatalf("a tick: %+v %v", ticked, err)
	}
	_, err = owner.SetTask(ctx, denID, dm, first.ID, 1, denproto.TaskRequest{Checked: true, Text: "bread"})
	var conflict *denclient.ErrEditConflict
	if !errors.As(err, &conflict) || conflict.Current.Text != ticked.Text {
		t.Fatalf("a tick on a line that moved: %v", err)
	}
	if _, err := owner.Edit(ctx, denID, dm, first.ID, denproto.EditRequest{Revision: 1, Text: "stale"}); !errors.As(err, &conflict) {
		t.Fatalf("a stale edit: %v", err)
	}

	photo := gpsJPEG(t)
	up, err := owner.Upload(ctx, denID, dm, "IMG_2001.jpg", int64(len(photo)), bytes.NewReader(photo))
	if err != nil || !up.Stripped || up.Thumb == nil || up.Width == 0 || up.Type != "image/jpeg" || up.Sealed {
		t.Fatalf("a DM photo: %+v %v", up, err)
	}
	msg, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{up.ID}})
	if err != nil || len(msg.Attachments) != 1 || msg.Attachments[0].ID != up.ID {
		t.Fatalf("sending it: %+v %v", msg, err)
	}
	var seen denclient.PageMessage
	for _, m := range history(t, member, denID, dm) {
		if m.ID == msg.ID {
			seen = m
		}
	}
	if len(seen.Attachments) != 1 || seen.Attachments[0].Name != "IMG_2001.jpg" || seen.Attachments[0].Thumb == nil {
		t.Fatalf("bob sees %+v", seen)
	}
	data, kind, err := fetch(t, member, denID, up.ID, false)
	if err != nil || kind != media.JPEG || bytes.Contains(data, []byte("GPSLatitude")) || int64(len(data)) != up.Size {
		t.Fatalf("bob's copy of the photo: %d bytes, %v, %v", len(data), kind, err)
	}
	if _, kind, err := fetch(t, member, denID, up.ID, true); err != nil || !kind.Image() {
		t.Fatalf("its preview: %v %v", kind, err)
	}
	// All the den has of it is noise.
	bob, _ := denproto.ParseID(me(t, member, denID).ID)
	r, err := h.d.OpenFile(ctx, &den.Session{MemberID: bob}, up.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := io.ReadAll(r)
	r.Close()
	if media.Sniff(blob[:media.SniffLen]) != media.Other || bytes.Contains(blob, data[:64]) {
		t.Fatal("the den can read the photo")
	}
}

// The den can't read a DM to take tracking out of its links, so the sender's
// Dens does before sealing it, on sends and edits.
func TestDMLinksLoseTheirTracking(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	const shared, clean = "https://youtu.be/dQw4w9WgXcQ?si=Xa1B2c3D4e5F6g7H&t=42", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42"
	sent := sendDM(t, owner, denID, dm, "look "+shared)
	if sent.Text != "look "+clean {
		t.Fatalf("sent %q", sent.Text)
	}
	if got := history(t, member, denID, dm); len(got) != 1 || got[0].Text != "look "+clean {
		t.Fatalf("bob reads %+v", got)
	}
	edited, err := owner.Edit(ctx, denID, dm, sent.ID, denproto.EditRequest{Revision: sent.Revision, Text: "look again " + shared})
	if err != nil || edited.Text != "look again "+clean {
		t.Fatalf("edited %q: %v", edited.Text, err)
	}
	if got := history(t, member, denID, dm); len(got) != 1 || got[0].Text != "look again "+clean {
		t.Fatalf("bob reads %+v", got)
	}
}

// A new device the member approves gets their seal, and with it their
// identity and DM history. Starting over there signs their other devices
// out, and leaves what was sealed to the old identity unreadable to them,
// though not to the other member, whose Dens sees the member's keys
// change, and whose next message makes the DM a new key.
func TestNewDevicesAndStartingOver(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	before := sendDM(t, member, denID, dm, "from before")
	bob := me(t, member, denID)

	laptop := h.client(t, noOwnDen)
	signInApproved(t, laptop, member, denID, denclient.SignInRequest{Den: "https://example.com", Username: "bob", Password: "correct horse"}, false)
	waitFor(t, laptop, "the laptop connected", connected)
	got := history(t, laptop, denID, dm)
	if len(got) != 1 || got[0].Text != "from before" || got[0].Locked != "" {
		t.Fatalf("the laptop's history: %+v", got)
	}
	// The seal it was handed makes the same identity, so nothing changed.
	if v := viewOf(t, laptop, denID, "the laptop's identity", func(v denclient.View) bool { return v.Me.Identity != "" }); v.Me.Identity != bob.Identity {
		t.Fatalf("the laptop's identity %q, the phone's %q", v.Me.Identity, bob.Identity)
	}

	if _, err := laptop.StartOver(ctx, denID, "wrong password"); !denproto.IsCode(err, denproto.CodeWrongPassword) {
		t.Fatalf("starting over with the wrong password: %v", err)
	}
	seal, err := laptop.StartOver(ctx, denID, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denproto.ParseSeal(seal); err != nil {
		t.Fatalf("the new seal %q: %v", seal, err)
	}
	waitFor(t, member, "the first device signed out", revoked("You reset your DM recovery key"))
	if shown, err := laptop.ShowSeal(denID); err != nil || shown != seal {
		t.Fatalf("the seal kept: %q %v", shown, err)
	}
	got = history(t, laptop, denID, dm)
	if len(got) != 1 || got[0].Locked != "lost" || got[0].Text != "" {
		t.Fatalf("the old message after starting over: %+v", got)
	}
	// The other member's Dens sees bob's new identity as a change, and
	// still reads what was sealed to the old one.
	v := viewOf(t, owner, denID, "bob's new identity", func(v denclient.View) bool {
		m, _ := memberIn(v, bob.ID)
		return m.Identity != "" && m.Identity != bob.Identity && len(v.Identities[bob.ID].Changes) == 1
	})
	if k := v.Identities[bob.ID]; k.Verified || k.Held {
		t.Fatalf("bob's identity as alice's Dens kept it: %+v", k)
	}
	if got := history(t, owner, denID, dm); len(got) != 1 || got[0].Text != "from before" {
		t.Fatalf("the other member lost their history: %+v", got)
	}
	after := sendDM(t, owner, denID, dm, "after")
	if after.KeyID == before.KeyID {
		t.Fatal("alice sent under the key sealed to bob's old identity")
	}
	again := sendDM(t, laptop, denID, dm, "and back")
	got = history(t, laptop, denID, dm)
	if len(got) != 3 || got[1].Text != "after" || got[2].Text != "and back" || got[1].KeyID != again.KeyID || got[0].ID != before.ID {
		t.Fatalf("history across the new key: %+v", got)
	}
	v, _ = laptop.View(denID)
	var retired denclient.PageKey
	for _, k := range v.DMKeys {
		if k.ID == before.KeyID {
			retired = k
		}
	}
	if retired.Retired != denproto.RetiredStartedOver || retired.RetiredBy != me(t, laptop, denID).ID || !retired.Signed {
		t.Fatalf("the old key as the page sees it: %+v", retired)
	}
}

// A device signed in with a recovery code has no seal, so DMs say so until
// the member types the one they saved.
func TestRecoveredDeviceTypesTheSeal(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	desk := h.client(t, noOwnDen)
	joined := joinWithCodes(t, owner, desk, denID, "dora")
	if !joined.SealNew || joined.Seal == "" {
		t.Fatalf("a first join makes a seal: %+v", joined)
	}
	dm := openDM(t, owner, desk, denID)
	readyDM(t, owner, desk, denID, dm)
	sendDM(t, owner, denID, dm, "for dora")

	fresh := h.client(t, noOwnDen)
	if _, err := signIn(t, fresh, denclient.SignInRequest{Den: "https://example.com", Username: "dora", Password: "next password", RecoveryCode: joined.RecoveryCodes[0]}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, fresh, "the fresh machine connected", connected)
	if got := history(t, fresh, denID, dm); len(got) != 1 || got[0].Locked != "no_seal" {
		t.Fatalf("before the seal: %+v", got)
	}
	if _, err := fresh.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x"}); !isInput(err) {
		t.Fatalf("sending without the seal: %v", err)
	}
	if _, err := fresh.Code(ctx, denID, me(t, owner, denID).ID); !isInput(err) {
		t.Fatalf("a code without the seal: %v", err)
	}
	if err := fresh.TypeSeal(ctx, denID, "not a seal"); !isInput(err) {
		t.Fatalf("a mistyped seal: %v", err)
	}
	if err := fresh.TypeSeal(ctx, denID, joined.Seal); err != nil {
		t.Fatal(err)
	}
	if got := history(t, fresh, denID, dm); len(got) != 1 || got[0].Text != "for dora" {
		t.Fatalf("after the seal: %+v", got)
	}
	// The seal makes the identity dora had, so alice sees no change.
	if reply := sendDM(t, fresh, denID, dm, "got it"); reply.Locked != "" {
		t.Fatalf("dora's reply: %+v", reply)
	}
	v := viewOf(t, owner, denID, "dora's reply", func(v denclient.View) bool { return v.Identities[me(t, fresh, denID).ID].Changes == nil })
	if len(v.Identities[me(t, fresh, denID).ID].Changes) != 0 {
		t.Fatal("dora's identity changed as she typed her seal")
	}
}
