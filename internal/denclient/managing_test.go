package denclient_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func ids(files []denproto.File) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.ID)
	}
	return out
}

// messageIn finds a message in a client's history of a channel.
func messageIn(t *testing.T, m *denclient.Manager, denID, channel, id string) (denclient.PageMessage, bool) {
	t.Helper()
	for _, msg := range history(t, m, denID, channel) {
		if msg.ID == id {
			return msg, true
		}
	}
	return denclient.PageMessage{}, false
}

// A member at their limit frees space by swapping and deleting files
// (M5): the list shows what uses each, and the others see the change.
func TestManagingFiles(t *testing.T) {
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	limits := denproto.Limits{FileSize: 1 << 20, MemberStorage: 1 << 20, DenStorage: 1 << 30}
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the new limits", func(v denclient.View) bool { return v.Limits == limits })
	big := upload(t, member, denID, "big.bin", bytes.Repeat([]byte("b"), 600<<10))
	small := upload(t, member, denID, "small.txt", []byte("small"))
	sent, err := member.Send(ctx, denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "two files", Attachments: []string{big.ID, small.ID}})
	if err != nil {
		t.Fatal(err)
	}

	page, err := member.Files(ctx, denID, "")
	if err != nil || len(page.Files) != 2 || page.Files[0].ID != big.ID || page.Files[0].MessageID != sent.ID ||
		page.Files[0].ChannelID != channelID || page.Files[0].Excerpt != "two files" {
		t.Fatalf("the list: %+v %v", page, err)
	}
	half := bytes.Repeat([]byte("h"), 500<<10)
	if _, err := member.Upload(ctx, denID, "", "half.bin", int64(len(half)), bytes.NewReader(half)); !denproto.IsCode(err, denproto.CodeQuotaExceeded) {
		t.Fatalf("an upload past the limit: %v", err)
	}

	// The swap counts against the space the big file frees.
	up, err := member.Replace(ctx, denID, "", big.ID, "half.bin", int64(len(half)), bytes.NewReader(half))
	if err != nil {
		t.Fatal(err)
	}
	if err := member.SwapFile(ctx, denID, channelID, sent.ID, big.ID, up.ID); err != nil {
		t.Fatal(err)
	}
	viewed := func(what string, want ...string) {
		t.Helper()
		got, _ := messageIn(t, owner, denID, channelID, sent.ID)
		if !slices.Equal(ids(got.Attachments), want) || got.EditedAt == 0 {
			t.Fatalf("the owner sees %s as %v", what, ids(got.Attachments))
		}
	}
	viewed("the swap", up.ID, small.ID)
	if _, _, err := fetch(t, owner, denID, big.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("the swapped file: %v", err)
	}

	// Deleting takes a file off its message, which keeps its text, and a
	// message left with nothing goes.
	if err := member.RemoveFile(ctx, denID, channelID, sent.ID, small.ID); err != nil {
		t.Fatal(err)
	}
	viewed("the delete", up.ID)
	alone := upload(t, member, denID, "alone.txt", []byte("alone"))
	lonely, err := member.Send(ctx, denID, channelID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{alone.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := member.RemoveFile(ctx, denID, channelID, lonely.ID, alone.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := messageIn(t, owner, denID, channelID, lonely.ID); ok {
		t.Fatal("a message left with nothing stayed")
	}

	// An upload taken off the composer frees its space at once.
	waiting := upload(t, member, denID, "waiting.bin", bytes.Repeat([]byte("w"), 100<<10))
	before, _ := member.Storage(ctx, denID)
	if err := member.DropUpload(ctx, denID, waiting.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := member.Storage(ctx, denID); before.Used-after.Used != 100<<10 {
		t.Fatalf("space freed by dropping an upload: %d", before.Used-after.Used)
	}
}

// A DM's files are listed opened, and swap and delete as a channel's do,
// with the message sealed again (M5).
func TestManagingDMFiles(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	photo := gpsJPEG(t)
	up, err := owner.Upload(ctx, denID, dm, "IMG_2001.jpg", int64(len(photo)), bytes.NewReader(photo))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "a photo for you", Attachments: []string{up.ID}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.Files(ctx, denID, "")
	if err != nil || len(page.Files) != 1 {
		t.Fatalf("the list: %+v %v", page, err)
	}
	if f := page.Files[0]; f.ID != up.ID || f.Name != "IMG_2001.jpg" || f.Thumb == nil || f.Excerpt != "a photo for you" || f.ChannelID != dm || f.Locked != "" {
		t.Fatalf("a DM's file in the list: %+v", f)
	}

	other := gpsJPEG(t)
	next, err := owner.Replace(ctx, denID, dm, up.ID, "IMG_2002.jpg", int64(len(other)), bytes.NewReader(other))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.SwapFile(ctx, denID, dm, sent.ID, up.ID, next.ID); err != nil {
		t.Fatal(err)
	}
	seen, _ := messageIn(t, member, denID, dm, sent.ID)
	if len(seen.Attachments) != 1 || seen.Attachments[0].ID != next.ID || seen.Attachments[0].Name != "IMG_2002.jpg" || seen.Text != "a photo for you" {
		t.Fatalf("the other member sees the swap as %+v", seen)
	}
	if _, _, err := fetch(t, member, denID, next.ID, true); err != nil {
		t.Fatalf("the new file's preview: %v", err)
	}
	if err := owner.RemoveFile(ctx, denID, dm, sent.ID, next.ID); err != nil {
		t.Fatal(err)
	}
	if seen, _ := messageIn(t, member, denID, dm, sent.ID); len(seen.Attachments) != 0 || seen.Text != "a photo for you" {
		t.Fatalf("the other member sees the delete as %+v", seen)
	}
	if page, err := owner.Files(ctx, denID, ""); err != nil || len(page.Files) != 0 {
		t.Fatalf("the list after: %+v %v", page, err)
	}
}
