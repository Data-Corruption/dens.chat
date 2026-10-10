package den

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func fileIDs(fs []denproto.File) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.ID)
	}
	return out
}

// replacing uploads data as a file made to take the place of the one
// replaces names.
func (f *fixture) replacing(s *Session, name string, data []byte, replaces string) (denproto.File, error) {
	return f.d.Upload(context.Background(), s, name, int64(len(data)), bytes.NewReader(data), false, replaces)
}

func TestEditChangesFiles(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	a := f.mustUpload(member, "a.txt", []byte("a"))
	b := f.mustUpload(member, "b.txt", []byte("bb"))
	m, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "two files", Attachments: []string{a.ID, b.ID},
		Editors: []string{denproto.FormatID(owner.MemberID)}})
	if err != nil {
		t.Fatal(err)
	}
	cc := f.mustUpload(member, "c.txt", []byte("ccc"))

	// Only the author changes a message's files, not the members they named.
	_, err = f.d.Edit(ctx, owner, m.ID, denproto.EditRequest{Revision: 1, Text: "two files", Attachments: &[]string{a.ID}})
	wantCode(t, err, denproto.CodeForbidden)
	// The files are ones it has, and the author's uploads waiting to be used.
	theirs := f.mustUpload(owner, "theirs.txt", []byte("t"))
	_, err = f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "two files", Attachments: &[]string{a.ID, theirs.ID}})
	wantCode(t, err, denproto.CodeInvalidField)

	sub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	got, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "two files", Attachments: &[]string{cc.ID, b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fileIDs(got.Attachments), []string{cc.ID, b.ID}) || got.EditedAt == 0 || got.Revision != 2 {
		t.Fatalf("after the edit: %+v", got)
	}
	updated := nextEvent[denproto.MessageUpdated](t, sub, denproto.EventMessageUpdated)
	if !slices.Equal(updated.Files, []string{a.ID}) || !slices.Equal(fileIDs(updated.Attachments), []string{cc.ID, b.ID}) {
		t.Fatalf("message.updated: files %v, attachments %v", updated.Files, fileIDs(updated.Attachments))
	}
	if _, err := f.read(owner, a.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("a file the edit dropped: %v", err)
	}
	if got, err := f.read(owner, cc.ID, false); err != nil || string(got) != "ccc" {
		t.Fatalf("the file the edit added: %q %v", got, err)
	}
	if files, _ := f.stored(); len(files) != 3 {
		t.Fatalf("stored: %v", files)
	}

	// A message keeps text or files; it can lose its files with text left.
	_, err = f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 2, Text: "", Attachments: &[]string{}})
	wantCode(t, err, denproto.CodeInvalidField)
	if got, err = f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 2, Text: "no files now", Attachments: &[]string{}}); err != nil || len(got.Attachments) != 0 {
		t.Fatalf("losing every file: %+v %v", got, err)
	}

	// The same files in the same order change nothing, and mark nothing.
	plain := f.send(member, c.ID, "plain")
	if got, err = f.d.Edit(ctx, member, plain.ID, denproto.EditRequest{Revision: 1, Text: "plain", Attachments: &[]string{}}); err != nil || got.EditedAt != 0 {
		t.Fatalf("an edit that changes nothing: %+v %v", got, err)
	}
}

func TestSwapAtTheLimit(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	limits := denproto.Limits{FileSize: 1 << 20, MemberStorage: 1 << 20, DenStorage: 1 << 30}
	if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	big := f.mustUpload(member, "big.bin", bytes.Repeat([]byte("b"), 600<<10))
	m := f.mustSendFiles(member, c.ID, big.ID)
	small := bytes.Repeat([]byte("s"), 500<<10)
	_, err := f.upload(member, "small.bin", small)
	wantCode(t, err, denproto.CodeQuotaExceeded)

	// Made to replace the big file, it counts against the space that frees.
	first, err := f.replacing(member, "small.bin", small, big.ID)
	if err != nil {
		t.Fatal(err)
	}
	// It takes only that file's place.
	_, err = f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Attachments: []string{first.ID}})
	wantCode(t, err, denproto.CodeInvalidField)
	// A newer one for the same file drops it.
	second, err := f.replacing(member, "smaller.bin", small[:400<<10], big.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.read(member, first.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("the replacement a newer one dropped: %v", err)
	}
	got, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "", Attachments: &[]string{second.ID}})
	if err != nil || !slices.Equal(fileIDs(got.Attachments), []string{second.ID}) {
		t.Fatalf("the swap: %+v %v", got, err)
	}
	if st, _ := f.d.Storage(ctx, member); st.Used != 400<<10 {
		t.Fatalf("space after the swap: %+v", st)
	}

	// A replacement is for one of the member's own files in use.
	waiting := f.mustUpload(member, "waiting.txt", []byte("w"))
	theirs := f.mustSendFiles(owner, c.ID, f.mustUpload(owner, "theirs.txt", []byte("t")).ID)
	for name, replaces := range map[string]string{
		"a waiting upload": waiting.ID, "someone else's file": theirs.Attachments[0].ID, "no file": "999999", "not an ID": "x",
	} {
		if _, err := f.replacing(member, "r.txt", []byte("r"), replaces); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("replacing %s: %v", name, err)
		}
	}
	// It takes the place only of the file it replaces, which the edit drops.
	other := f.mustSendFiles(member, c.ID, f.mustUpload(member, "other.txt", []byte("o")).ID)
	r, err := f.replacing(member, "r.txt", []byte("r"), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.d.Edit(ctx, member, other.ID, denproto.EditRequest{Revision: 1, Text: "", Attachments: &[]string{r.ID}})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 2, Text: "", Attachments: &[]string{second.ID, r.ID}})
	wantCode(t, err, denproto.CodeInvalidField)

	// A picture made to replace one goes only in its place.
	avatar := f.mustUpload(member, "avatar.png", testPNG(t, 64, 64))
	if _, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &avatar.ID}); err != nil {
		t.Fatal(err)
	}
	next, err := f.replacing(member, "next.png", testPNG(t, 128, 128), avatar.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Banner: &next.ID}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("an avatar's replacement as a banner: %v", err)
	}
	if _, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &next.ID}); err != nil {
		t.Fatalf("an avatar's replacement: %v", err)
	}
}

func TestDropUpload(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	waiting := f.mustUpload(member, "waiting.txt", []byte("waiting"))
	if err := f.d.DropUpload(ctx, owner, waiting.ID); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("dropping someone else's upload: %v", err)
	}
	if err := f.d.DropUpload(ctx, member, waiting.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.d.Storage(ctx, member); st.Used != 0 {
		t.Fatalf("space after dropping: %+v", st)
	}
	if files, _ := f.stored(); len(files) != 0 {
		t.Fatalf("stored: %v", files)
	}
	sent := f.mustSendFiles(member, c.ID, f.mustUpload(member, "sent.txt", []byte("sent")).ID)
	if err := f.d.DropUpload(ctx, member, sent.Attachments[0].ID); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("dropping a file in use: %v", err)
	}
}

func TestOwnFiles(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	sent, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "  look at this one",
		Attachments: []string{f.mustUpload(member, "photo.jpg", testJPEG(t, 64, 48)).ID}})
	if err != nil {
		t.Fatal(err)
	}
	avatar := f.mustUpload(member, "avatar.png", testPNG(t, 64, 64))
	if _, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &avatar.ID}); err != nil {
		t.Fatal(err)
	}
	waiting := f.mustUpload(member, "waiting.txt", []byte("w"))
	carol := f.member(owner, "carol")
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: denproto.FormatID(carol.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := f.d.Upload(ctx, member, "", 9, bytes.NewReader([]byte("ciphertxt")), true, "")
	if err != nil {
		t.Fatal(err)
	}
	inDM := f.mustSendFiles(member, dm.ID, blob.ID)
	f.mustUpload(owner, "not mine.txt", []byte("n"))

	page, err := f.d.OwnFiles(ctx, member, "")
	if err != nil || denproto.CheckOwnFiles(page) != nil || page.Next != "" || len(page.Files) != 4 {
		t.Fatalf("the list: %+v %v", page, err)
	}
	byID := map[string]denproto.OwnFile{}
	for _, file := range page.Files {
		byID[file.ID] = file
	}
	if p := byID[sent.Attachments[0].ID]; p.MessageID != sent.ID || p.ChannelID != c.ID || p.Excerpt != "look at this one" || p.Name != "photo.jpg" {
		t.Errorf("a file on a message: %+v", p)
	}
	if p := byID[avatar.ID]; p.Profile != denproto.ProfileAvatar || p.MessageID != "" {
		t.Errorf("a picture on the profile: %+v", p)
	}
	if p := byID[waiting.ID]; p.Profile != "" || p.MessageID != "" || p.CreatedAt != f.clock.UnixMilli() {
		t.Errorf("a waiting upload: %+v", p)
	}
	if p := byID[blob.ID]; !p.Sealed || p.MessageID != inDM.ID || p.Excerpt != "" {
		t.Errorf("a DM's file: %+v", p)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != inDM.ID || page.Messages[0].Sealed == nil {
		t.Errorf("the DM message for its file: %+v", page.Messages)
	}

	// Pages follow one another, largest first by the space a file and its
	// preview take, which is its size for a file without one.
	for i := range 2*denproto.OwnFilesPage + 3 {
		f.mustUpload(member, fmt.Sprintf("%03d.txt", i), bytes.Repeat([]byte("x"), 1+i%7))
	}
	seen := map[string]bool{}
	var sizes []int64
	after := ""
	for pages := 0; ; pages++ {
		p, err := f.d.OwnFiles(ctx, member, after)
		if err != nil || denproto.CheckOwnFiles(p) != nil || pages > 5 {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, file := range p.Files {
			if seen[file.ID] {
				t.Fatalf("%s listed twice", file.ID)
			}
			seen[file.ID] = true
			if file.Thumb == nil {
				sizes = append(sizes, file.Size)
			}
		}
		if after = p.Next; after == "" {
			break
		}
	}
	if len(seen) != 4+2*denproto.OwnFilesPage+3 || !slices.IsSortedFunc(sizes, func(a, b int64) int { return int(b - a) }) {
		t.Fatalf("listed %d files, sizes %v", len(seen), sizes)
	}
	_, err = f.d.OwnFiles(ctx, member, "nonsense")
	wantCode(t, err, denproto.CodeInvalidField)
}

func TestDMEditChangesFiles(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: denproto.FormatID(carol.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	sealed := func(data string, replaces string) denproto.File {
		u, err := f.d.Upload(ctx, member, "", int64(len(data)), bytes.NewReader([]byte(data)), true, replaces)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	file, preview := sealed("a file", ""), sealed("its preview", "")
	m := f.mustSendFiles(member, dm.ID, file.ID, preview.ID)
	next, nextPreview := sealed("the next file", file.ID), sealed("its own preview", preview.ID)
	// A channel's upload can't go on a DM's message.
	plain := f.mustUpload(member, "plain.txt", []byte("p"))
	cid := mustInt(t, dm.ID)
	k := f.keyFor(member.MemberID, carol.MemberID, cid)
	edit := func(rev int, files ...string) (denproto.Message, error) {
		return f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: rev, KeyID: k.id,
			Sealed: f.sealText(k, cid, member.MemberID, m.Nonce, rev+1, "swapped"), Attachments: &files})
	}
	if _, err := edit(1, next.ID, plain.ID); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("a channel's upload on a DM's message: %v", err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)
	got, err := edit(1, next.ID, nextPreview.ID)
	if err != nil || got.Attachments != nil || got.EditedAt == 0 {
		t.Fatalf("the swap: %+v %v", got, err)
	}
	updated := nextEvent[denproto.MessageUpdated](t, sub, denproto.EventMessageUpdated)
	if !slices.Equal(updated.Files, []string{file.ID, preview.ID}) {
		t.Fatalf("message.updated names %v", updated.Files)
	}
	for _, id := range []string{file.ID, preview.ID} {
		if _, err := f.read(carol, id, false); !denproto.IsCode(err, denproto.CodeNotFound) {
			t.Fatalf("a blob the swap dropped: %v", err)
		}
	}
	if b, err := f.read(carol, next.ID, false); err != nil || string(b) != "the next file" {
		t.Fatalf("the blob the swap put in: %q %v", b, err)
	}
}
