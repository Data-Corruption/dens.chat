package den

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// notesFixture is a den whose members' Dens speak version 3 of the
// protocol: its owner, alice, and bob.
func notesFixture(t *testing.T) (f *fixture, owner, bob *Session) {
	t.Helper()
	f, owner, bob = chatFixture(t)
	owner.Version, bob.Version = denproto.NotesVersion, denproto.NotesVersion
	return f, owner, bob
}

// notesKey is the key a member's notes are sealed with, as their Dens makes
// it from their seal.
func (f *fixture) notesKey(member int64) denproto.Bytes {
	f.t.Helper()
	info, _ := f.d.Info()
	key, err := denproto.NotesKey(f.seals[member], info.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return key
}

// notesAD binds a note as its member's Dens does: as a DM message, under
// the key its channel's ID names.
func (f *fixture) notesAD(notes denproto.Channel, member int64, nonce denproto.Bytes, revision int) denproto.MessageAD {
	f.t.Helper()
	info, _ := f.d.Info()
	cid, _ := denproto.ParseID(notes.ID)
	return denproto.MessageAD{DenID: info.ID, Channel: cid, Author: member, Nonce: nonce, KeyID: cid, Revision: revision}
}

// note sends a note, sealed as its member's Dens seals it, replying to
// another when reply is set.
func (f *fixture) note(s *Session, notes denproto.Channel, text, reply string, files ...string) (denproto.Message, error) {
	f.t.Helper()
	nonce := denproto.Random(16)
	sealed, err := denproto.SealDMMessage(f.notesKey(s.MemberID), f.notesAD(notes, s.MemberID, nonce, 1), denproto.DMPayload{Text: text})
	if err != nil {
		f.t.Fatal(err)
	}
	return f.d.Send(context.Background(), s, notes.ID, denproto.SendRequest{Nonce: nonce, Sealed: sealed, KeyID: notes.ID,
		ReplyTo: reply, Attachments: files})
}

// openNote opens a note as its member's Dens does.
func (f *fixture) openNote(notes denproto.Channel, m denproto.Message) string {
	f.t.Helper()
	author, _ := denproto.ParseID(m.AuthorID)
	p, err := denproto.OpenDMMessage(f.notesKey(author), f.notesAD(notes, author, m.Nonce, m.Revision), m.Sealed)
	if err != nil {
		f.t.Fatalf("open note %s: %v", m.ID, err)
	}
	return p.Text
}

// A member's notes are one channel, made as they first open it, which they
// alone see: they write, reply, edit and delete there as in a DM, sealed
// under their channel's ID, and nothing there counts as unread. Reactions,
// polls, editors, plain text, other keys and disappearing times are
// refused, and so is closing them.
func TestNotesAreTheMembersAlone(t *testing.T) {
	f, owner, bob := notesFixture(t)
	ctx := context.Background()
	theirs, err := f.d.OpenNotes(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	bobSub, _, _, _ := f.d.Hub.Subscribe(bob.MemberID, false, "", 0)
	aliceSub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	notes, err := f.d.OpenNotes(ctx, bob)
	if err != nil || notes.Kind != denproto.KindNotes || !slices.Equal(notes.Members, []string{denproto.FormatID(bob.MemberID)}) ||
		notes.Name != "" || notes.GroupID != nil || notes.ID == theirs.ID {
		t.Fatalf("bob's notes: %+v, %v", notes, err)
	}
	if again, err := f.d.OpenNotes(ctx, bob); err != nil || again.ID != notes.ID {
		t.Fatalf("bob's notes opened again: %+v, %v", again, err)
	}
	if got := drain(bobSub); !slices.Equal(got, []string{denproto.EventChannelCreated}) {
		t.Errorf("bob heard %v as his notes were made, then opened again", got)
	}

	first, err := f.note(bob, notes, "milk, eggs", "")
	if err != nil || first.KeyID != notes.ID || first.Sealed == nil || first.Text != "" {
		t.Fatalf("a note: %+v, %v", first, err)
	}
	reply, err := f.note(bob, notes, "and bread", first.ID)
	if err != nil || reply.Reply == nil || reply.Reply.KeyID != notes.ID || !bytes.Equal(reply.Reply.Sealed, first.Sealed) {
		t.Fatalf("a reply to a note: %+v, %v", reply, err)
	}
	edited, err := f.d.Edit(ctx, bob, first.ID, denproto.EditRequest{Revision: 1, KeyID: notes.ID, Sealed: func() denproto.Bytes {
		sealed, err := denproto.SealDMMessage(f.notesKey(bob.MemberID), f.notesAD(notes, bob.MemberID, first.Nonce, 2), denproto.DMPayload{Text: "milk, eggs, flour"})
		if err != nil {
			t.Fatal(err)
		}
		return sealed
	}()})
	if err != nil || edited.Revision != 2 || f.openNote(notes, edited) != "milk, eggs, flour" {
		t.Fatalf("an edited note: %+v, %v", edited, err)
	}
	if err := f.d.Delete(ctx, bob, reply.ID); err != nil {
		t.Fatal(err)
	}
	h, err := f.d.History(ctx, bob, notes.ID, HistoryQuery{})
	if err != nil || len(h.Messages) != 1 || f.openNote(notes, h.Messages[0]) != "milk, eggs, flour" {
		t.Fatalf("bob's notes' history: %+v, %v", h, err)
	}
	// What the den keeps is sealed.
	var kept []byte
	if err := f.db.QueryRow(`SELECT text FROM den_messages WHERE id = ?`, first.ID).Scan(&kept); err != nil || bytes.Contains(kept, []byte("milk")) {
		t.Errorf("the den keeps the note as %q, %v", kept, err)
	}

	// Nobody else sees them, staff included.
	if _, err := f.d.History(ctx, owner, notes.ID, HistoryQuery{}); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("alice read bob's notes: %v", err)
	}
	if _, err := f.note(owner, notes, "mine now", ""); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("alice wrote in bob's notes: %v", err)
	}
	wantCode(t, f.d.Delete(ctx, owner, first.ID), denproto.CodeNotFound)
	if got := drain(aliceSub); len(got) != 0 {
		t.Errorf("alice heard %v of bob's notes", got)
	}
	snap, err := f.d.snapshot(ctx, owner.MemberID, true, 0, nil)
	if err != nil || slices.ContainsFunc(snap.Channels, func(c denproto.Channel) bool { return c.ID == notes.ID }) {
		t.Errorf("alice's snapshot holds bob's notes: %+v, %v", snap.Channels, err)
	}

	// Bob's snapshot has them, and nothing in them unread.
	snap, err = f.d.snapshot(ctx, bob.MemberID, false, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(snap.Channels, func(c denproto.Channel) bool { return c.ID == notes.ID && c.Kind == denproto.KindNotes }) {
		t.Errorf("bob's snapshot lacks his notes: %+v", snap.Channels)
	}
	i := slices.IndexFunc(snap.ReadStates, func(r denproto.ReadState) bool { return r.ChannelID == notes.ID })
	if i < 0 || snap.ReadStates[i].LastMessage != first.ID || snap.ReadStates[i].ReadPosition != reply.ID || snap.ReadStates[i].MentionCount != 0 {
		t.Errorf("bob's notes' read state: %+v", snap.ReadStates)
	}

	// What notes don't take.
	nonce := denproto.Random(16)
	sealed, err := denproto.SealDMMessage(f.notesKey(bob.MemberID), f.notesAD(notes, bob.MemberID, nonce, 1), denproto.DMPayload{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for what, req := range map[string]denproto.SendRequest{
		"plain text":     {Nonce: nonce, Text: "x"},
		"another key":    {Nonce: nonce, Sealed: sealed, KeyID: theirs.ID},
		"no key":         {Nonce: nonce, Sealed: sealed},
		"a poll":         {Nonce: nonce, Sealed: sealed, KeyID: notes.ID, Poll: &denproto.PollRequest{Choices: []string{"a", "b"}, Hours: 1}},
		"another editor": {Nonce: nonce, Sealed: sealed, KeyID: notes.ID, Editors: []string{denproto.FormatID(owner.MemberID)}},
	} {
		if _, err := f.d.Send(ctx, bob, notes.ID, req); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("notes took %s: %v", what, err)
		}
	}
	wantCode(t, f.d.React(ctx, bob, first.ID, denproto.ReactionRequest{Emoji: "👍"}), denproto.CodeInvalidField)
	_, err = f.d.SetDMReactions(ctx, bob, first.ID, denproto.DMReactionsRequest{KeyID: notes.ID, Sealed: make([]byte, denproto.SealedReactionsSize)})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.SetExpiry(ctx, bob, notes.ID, denproto.ExpiryRequest{ExpiresAfter: 3600})
	wantCode(t, err, denproto.CodeInvalidField)
	wantCode(t, f.d.CloseDM(ctx, bob, notes.ID), denproto.CodeInvalidField)
	_, err = f.d.SetTask(ctx, bob, first.ID, 0, denproto.TaskRequest{Text: "milk", Checked: true})
	wantCode(t, err, denproto.CodeInvalidField)
}

// A Dens speaking version 2 of the protocol, or 1, is sent nothing of
// notes: no channel, read position, event, message or change, and a
// request at those versions finds none and can't make them, so an older
// device of the member goes on without them, connected.
func TestNotesUnseenBeforeVersion3(t *testing.T) {
	f, _, bob := notesFixture(t)
	ctx := context.Background()
	for _, v := range []int{1, denproto.GroupsVersion} {
		old := *bob
		old.Version = v
		if _, err := f.d.OpenNotes(ctx, &old); !denproto.IsCode(err, denproto.CodeNotFound) {
			t.Errorf("a request at version %d opened notes: %v", v, err)
		}
	}
	oldSub, _, _, _ := f.d.Hub.SubscribeAt(bob.MemberID, false, denproto.GroupsVersion, "", 0)
	notes, err := f.d.OpenNotes(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte("sealed by bob's Dens")
	file, err := f.d.Upload(ctx, bob, "", int64(len(blob)), bytes.NewReader(blob), true, "")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := f.note(bob, notes, "a photo of the receipt", "", file.ID)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.note(bob, notes, "never mind", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.Delete(ctx, bob, gone.ID); err != nil {
		t.Fatal(err)
	}
	if got := drain(oldSub); len(got) != 0 {
		t.Errorf("an older Dens heard %v", got)
	}
	for _, v := range []int{1, denproto.GroupsVersion} {
		old := *bob
		old.Version = v
		snap, err := f.d.snapshotAt(ctx, bob.MemberID, false, v, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(snap.Channels, func(c denproto.Channel) bool { return c.Kind == denproto.KindNotes }) ||
			slices.ContainsFunc(snap.ReadStates, func(r denproto.ReadState) bool { return r.ChannelID == notes.ID }) {
			t.Errorf("a snapshot at version %d holds the notes: %+v", v, snap)
		}
		if _, err := f.d.History(ctx, &old, notes.ID, HistoryQuery{}); err == nil {
			t.Errorf("a request at version %d read the notes", v)
		}
		if _, err := f.note(&old, notes, "from an older Dens", ""); err == nil {
			t.Errorf("a request at version %d wrote in the notes", v)
		}
		if r, err := f.d.OpenFile(ctx, &old, file.ID, false); err == nil {
			r.Close()
			t.Errorf("a request at version %d opened a note's file", v)
		}
		ch, err := f.d.Changes(ctx, &old, 0, 0)
		if err != nil || slices.ContainsFunc(ch.Messages, func(m denproto.Message) bool { return m.ChannelID == notes.ID }) ||
			slices.ContainsFunc(ch.Deleted, func(d denproto.MessageDeleted) bool { return d.ChannelID == notes.ID }) {
			t.Errorf("changes at version %d hold the notes': %+v, %v", v, ch, err)
		}
	}
	current, err := f.d.snapshot(ctx, bob.MemberID, false, 0, nil)
	if err != nil || !slices.ContainsFunc(current.Channels, func(c denproto.Channel) bool { return c.ID == notes.ID }) ||
		!slices.ContainsFunc(current.ReadStates, func(r denproto.ReadState) bool { return r.ChannelID == notes.ID && r.LastMessage == kept.ID }) {
		t.Errorf("a Dens speaking version 3 lacks the notes: %+v, %v", current, err)
	}
	if ch, err := f.d.Changes(ctx, bob, 0, 0); err != nil || !slices.ContainsFunc(ch.Deleted, func(d denproto.MessageDeleted) bool { return d.ID == gone.ID }) {
		t.Errorf("changes at version 3 lack the note deleted: %+v, %v", ch, err)
	}
	// Closed before the test's directory goes, which Windows won't remove
	// while a file in it is open.
	r, err := f.d.OpenFile(ctx, bob, file.ID, false)
	if err != nil {
		t.Fatalf("bob's note's file at version 3: %v", err)
	}
	r.Close()
}

// A member who leaves the den takes their notes with them, files and all:
// nobody else could read them.
func TestNotesGoWithTheirMember(t *testing.T) {
	f, owner, bob := notesFixture(t)
	ctx := context.Background()
	notes, err := f.d.OpenNotes(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := f.d.OpenNotes(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte("sealed by bob's Dens")
	file, err := f.d.Upload(ctx, bob, "", int64(len(blob)), bytes.NewReader(blob), true, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.note(bob, notes, "the receipt", "", file.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.note(owner, mine, "alice's own", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := f.stored()
	if err := f.d.Leave(ctx, bob); err != nil {
		t.Fatal(err)
	}
	var channels, messages, files int
	if err := f.db.QueryRow(`SELECT (SELECT count(*) FROM den_channels WHERE kind = 'notes'),
		(SELECT count(*) FROM den_messages WHERE author_id = ?1), (SELECT count(*) FROM den_files WHERE uploader_id = ?1)`, bob.MemberID).
		Scan(&channels, &messages, &files); err != nil || channels != 1 || messages != 0 || files != 0 {
		t.Errorf("after bob left the den holds %d notes, %d of his messages and %d of his files, %v", channels, messages, files, err)
	}
	if after, _ := f.stored(); len(after) != len(before)-1 {
		t.Errorf("the den's storage held %v, and %v after bob left", before, after)
	}
	if h, err := f.d.History(ctx, owner, mine.ID, HistoryQuery{}); err != nil || len(h.Messages) != 1 {
		t.Errorf("alice's notes after bob left: %+v, %v", h, err)
	}
}
