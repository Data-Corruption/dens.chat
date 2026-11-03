package den

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func (f *fixture) addEmoji(s *Session, name string, data []byte) (denproto.Emoji, error) {
	f.t.Helper()
	up := f.mustUpload(s, "emoji.png", data)
	return f.d.AddEmoji(context.Background(), s, denproto.EmojiRequest{Name: name, FileID: up.ID})
}

func (f *fixture) mustAddEmoji(s *Session, name string, data []byte) denproto.Emoji {
	f.t.Helper()
	e, err := f.addEmoji(s, name, data)
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

func (f *fixture) setEmojiLimits(owner *Session, l denproto.EmojiLimits) {
	f.t.Helper()
	if _, err := f.d.Update(context.Background(), owner, denproto.DenUpdateRequest{EmojiLimits: &l}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) emojiNames(s *Session) []string {
	f.t.Helper()
	snap, err := f.d.snapshot(context.Background(), s.MemberID, IsStaff(s.Role), 0, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, e := range snap.Emoji {
		if err := denproto.CheckEmoji(e); err != nil {
			f.t.Errorf("the snapshot's emoji %+v: %v", e, err)
		}
		names = append(names, e.Name)
	}
	return names
}

// TestStaffKeepTheEmoji checks that staff alone add, rename and delete the
// den's emoji, each by a name of a-z, 0-9 and _, lowercased, and unique;
// that every member hears of each change and finds the emoji in their
// snapshot, with its image's size and whether it moves; and that a deleted
// emoji's file goes.
func TestStaffKeepTheEmoji(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.moderator(owner, "carol")
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	if info, _ := f.d.Info(); info.EmojiLimits == nil || *info.EmojiLimits != denproto.DefaultEmojiLimits {
		t.Fatalf("a new den's emoji limits are %+v", info.EmojiLimits)
	}
	_, err := f.addEmoji(member, "mine", testPNG(t, 64, 64))
	wantCode(t, err, denproto.CodeForbidden)

	party := f.mustAddEmoji(owner, "  Party_Parrot ", testPNG(t, 128, 96))
	if party.Name != "party_parrot" || party.Image.Width != 128 || party.Image.Height != 96 || party.Image.Animated ||
		party.AddedBy != denproto.FormatID(owner.MemberID) || party.AddedAt != f.clock.UnixMilli() {
		t.Fatalf("the emoji added is %+v", party)
	}
	if got := nextEvent[denproto.Emoji](t, sub, denproto.EventEmojiCreated); got != party {
		t.Errorf("members heard of %+v, not %+v", got, party)
	}
	for _, name := range []string{"PARTY_PARROT", "p", "party-parrot", ":wave:"} {
		_, err = f.addEmoji(carol, name, testPNG(t, 32, 32))
		if name == "PARTY_PARROT" {
			wantCode(t, err, denproto.CodeEmojiNameTaken)
		} else {
			wantCode(t, err, denproto.CodeInvalidField)
		}
	}
	dance := f.mustAddEmoji(carol, "dance", testGIF(t, 64, 64, 4, false))
	if !dance.Image.Animated {
		t.Errorf("an animated GIF's emoji is %+v", dance)
	}
	if got := f.emojiNames(member); !slices.Equal(got, []string{"dance", "party_parrot"}) {
		t.Fatalf("the snapshot's emoji are %q", got)
	}

	_, err = f.d.RenameEmoji(ctx, member, dance.ID, denproto.EmojiUpdate{Name: "boogie"})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.RenameEmoji(ctx, carol, dance.ID, denproto.EmojiUpdate{Name: "Party_Parrot"})
	wantCode(t, err, denproto.CodeEmojiNameTaken)
	_, err = f.d.RenameEmoji(ctx, carol, dance.ID, denproto.EmojiUpdate{Name: "boo gie"})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.RenameEmoji(ctx, carol, "999", denproto.EmojiUpdate{Name: "boogie"})
	wantCode(t, err, denproto.CodeNotFound)
	boogie, err := f.d.RenameEmoji(ctx, owner, dance.ID, denproto.EmojiUpdate{Name: "Boogie"})
	if err != nil || boogie.Name != "boogie" || boogie.ID != dance.ID || boogie.Image != dance.Image {
		t.Fatalf("renamed: %+v, %v", boogie, err)
	}
	if got := nextEvent[denproto.Emoji](t, sub, denproto.EventEmojiUpdated); got != boogie {
		t.Errorf("members heard of %+v, not %+v", got, boogie)
	}
	// An emoji keeps its own name.
	if _, err := f.d.RenameEmoji(ctx, carol, party.ID, denproto.EmojiUpdate{Name: "PARTY_PARROT"}); err != nil {
		t.Errorf("an emoji renamed to its own name: %v", err)
	}
	// A sound and an emoji may share a name: each kind has its own.
	if _, err := f.d.AddSound(ctx, owner, denproto.SoundRequest{Name: "boogie", FileID: f.soundUpload(owner, 10, 160).ID}); err != nil {
		t.Errorf("a sound named as an emoji is: %v", err)
	}

	before, _ := f.stored()
	wantCode(t, f.d.DeleteEmoji(ctx, member, boogie.ID), denproto.CodeForbidden)
	if err := f.d.DeleteEmoji(ctx, carol, boogie.ID); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.d.DeleteEmoji(ctx, carol, boogie.ID), denproto.CodeNotFound)
	// A sound isn't an emoji, by ID or otherwise.
	sounds, _ := f.d.sounds(ctx, f.d.db)
	wantCode(t, f.d.DeleteEmoji(ctx, carol, sounds[0].ID), denproto.CodeNotFound)
	if got := nextEvent[denproto.EmojiDeleted](t, sub, denproto.EventEmojiDeleted); got.ID != boogie.ID {
		t.Errorf("members heard of the deletion of %q", got.ID)
	}
	// Its image and the image's preview.
	if after, _ := f.stored(); len(after) != len(before)-2 {
		t.Errorf("deleting an emoji left %d stored files of %d", len(after), len(before))
	}
	if _, err := f.read(owner, boogie.Image.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("a deleted emoji's file: %v", err)
	}
	if got := f.emojiNames(owner); !slices.Equal(got, []string{"party_parrot"}) {
		t.Errorf("the emoji left are %q", got)
	}
}

// TestTheDensEmojiChecks checks what an emoji the den takes is: a still
// image at most 128 pixels on each side and 256 KiB, or an animated GIF of
// those sides and 512 KiB, an upload of the staff member's own waiting to
// be used, within the den's count while emoji are on; and that lowering the
// count keeps the emoji already added.
func TestTheDensEmojiChecks(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	add := func(file denproto.File, name string) error {
		_, err := f.d.AddEmoji(ctx, owner, denproto.EmojiRequest{Name: name, FileID: file.ID})
		return err
	}

	for name, data := range map[string][]byte{
		"too_wide": testPNG(t, 129, 64),
		"too_high": testPNG(t, 64, 129),
		"a_jpeg":   testJPEG(t, 200, 100),
		"a_wav":    testWAV(),
	} {
		up, err := f.upload(owner, name, data)
		if err != nil {
			t.Fatal(err)
		}
		wantCode(t, add(up, name), denproto.CodeInvalidField)
	}
	if err := add(f.mustUpload(owner, "small.jpg", testJPEG(t, 100, 50)), "a_small_jpeg"); err != nil {
		t.Errorf("a JPEG within an emoji's sides: %v", err)
	}
	// Frames of noise, too large for an emoji to move through, and fewer.
	big := f.mustUpload(owner, "big.gif", testGIF(t, 128, 128, 40, true))
	if !big.Animated || big.Size <= denproto.MaxAnimatedEmojiSize {
		t.Fatalf("the large GIF is %+v", big)
	}
	wantCode(t, add(big, "too_long"), denproto.CodeInvalidField)
	if err := add(f.mustUpload(owner, "small.gif", testGIF(t, 128, 128, 8, true)), "moves"); err != nil {
		t.Errorf("a GIF within an emoji's size: %v", err)
	}

	// The member's own upload, waiting to be used.
	theirs := f.mustUpload(member, "theirs.png", testPNG(t, 32, 32))
	wantCode(t, add(theirs, "theirs"), denproto.CodeInvalidField)
	used := f.mustUpload(owner, "used.png", testPNG(t, 32, 32))
	if err := add(used, "once"); err != nil {
		t.Fatal(err)
	}
	wantCode(t, add(used, "twice"), denproto.CodeInvalidField)
	stale := f.mustUpload(owner, "stale.png", testPNG(t, 32, 32))
	f.clock = f.clock.Add(uploadExpiry + time.Minute)
	wantCode(t, add(stale, "stale"), denproto.CodeInvalidField)

	f.setEmojiLimits(owner, denproto.EmojiLimits{Emojis: 4})
	if err := add(f.mustUpload(owner, "fourth.png", testPNG(t, 32, 32)), "fourth"); err != nil {
		t.Fatal(err)
	}
	wantCode(t, add(f.mustUpload(owner, "fifth.png", testPNG(t, 32, 32)), "fifth"), denproto.CodeInvalidField)
	f.setEmojiLimits(owner, denproto.EmojiLimits{Emojis: 1})
	if got := f.emojiNames(owner); len(got) != 4 {
		t.Errorf("lowering the count left the emoji %q", got)
	}
	// Off: none added, and those there stay.
	f.setEmojiLimits(owner, denproto.EmojiLimits{Emojis: 0})
	wantCode(t, add(f.mustUpload(owner, "off.png", testPNG(t, 32, 32)), "off"), denproto.CodeInvalidField)
	if got := f.emojiNames(owner); len(got) != 4 {
		t.Errorf("turning emoji off left %q", got)
	}
	if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{EmojiLimits: &denproto.EmojiLimits{Emojis: denproto.MaxEmojis + 1}}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Errorf("a count out of range: %v", err)
	}
	if _, err := f.d.Update(ctx, member, denproto.DenUpdateRequest{EmojiLimits: &denproto.DefaultEmojiLimits}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Errorf("a member setting the count: %v", err)
	}
	// The count comes back as it was set when the den opens again, and
	// goes to members as it is, 0 among the rest, but not in a preview.
	f.open()
	if info, _ := f.d.Info(); info.EmojiLimits == nil || info.EmojiLimits.Emojis != 0 {
		t.Errorf("the den opened with the emoji limits %+v", info.EmojiLimits)
	}
	snap, err := f.d.snapshot(ctx, member.MemberID, false, 0, nil)
	if err != nil || snap.Den.EmojiLimits == nil || snap.Den.EmojiLimits.Emojis != 0 {
		t.Errorf("a member's snapshot has the emoji limits %+v, %v", snap.Den.EmojiLimits, err)
	}
	if preview, err := f.d.Preview(ctx, f.invite(owner, 1).Code); err != nil || preview.EmojiLimits != nil {
		t.Errorf("a join preview shows the emoji limits %+v, %v", preview.EmojiLimits, err)
	}
}

// TestAnEmojisFileIsTheDens checks that an emoji's file counts toward the
// den's space and not its uploader's, isn't among their files, opens for
// every member, its preview too, and stays through retention, the sweep of
// waiting uploads and its uploader leaving; and that no message, picture
// or replacement takes it.
func TestAnEmojisFileIsTheDens(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.moderator(owner, "carol")
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	before, err := f.d.Storage(ctx, carol)
	if err != nil {
		t.Fatal(err)
	}
	upload := f.mustUpload(carol, "wave.png", testPNG(t, 96, 96))
	emoji, err := f.d.AddEmoji(ctx, carol, denproto.EmojiRequest{Name: "wave", FileID: upload.ID})
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.d.Storage(ctx, carol)
	if err != nil {
		t.Fatal(err)
	}
	// The den's space holds the image and its preview.
	if after.Used != before.Used || after.DenUsed < before.DenUsed+upload.Size {
		t.Errorf("the emoji took %d of its uploader's space and %d of the den's", after.Used-before.Used, after.DenUsed-before.DenUsed)
	}
	if page, err := f.d.OwnFiles(ctx, carol, ""); err != nil || len(page.Files) != 0 {
		t.Errorf("the uploader's files: %+v, %v", page.Files, err)
	}

	for _, s := range []*Session{owner, member} {
		stored, err := f.read(s, emoji.Image.ID, false)
		if err != nil || !bytes.HasPrefix(stored, []byte("\x89PNG")) {
			t.Errorf("member %d opens the emoji's file: %v", s.MemberID, err)
		}
		if _, err := f.read(s, emoji.Image.ID, true); err != nil {
			t.Errorf("member %d opens the emoji's preview: %v", s.MemberID, err)
		}
	}

	_, err = f.d.Send(ctx, carol, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{emoji.Image.ID}})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.UpdateProfile(ctx, carol, denproto.ProfileRequest{Avatar: &emoji.Image.ID})
	wantCode(t, err, denproto.CodeInvalidField)
	png := testPNG(t, 64, 64)
	_, err = f.d.Upload(ctx, carol, "swap.png", int64(len(png)), bytes.NewReader(png), false, emoji.Image.ID)
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.AddSound(ctx, carol, denproto.SoundRequest{Name: "Wave", FileID: emoji.Image.ID})
	wantCode(t, err, denproto.CodeInvalidField)

	f.send(carol, c.ID, "old news")
	f.setRetention(owner, 1)
	f.clock = f.clock.Add(48 * time.Hour)
	f.d.expire()
	f.d.sweepUploads()
	if err := f.d.Leave(ctx, carol); err != nil {
		t.Fatal(err)
	}
	if _, err := f.read(member, emoji.Image.ID, false); err != nil {
		t.Errorf("after retention, the sweep and its uploader leaving, the emoji's file: %v", err)
	}
	if got := f.emojiNames(owner); !slices.Equal(got, []string{"wave"}) {
		t.Errorf("the emoji left are %q", got)
	}
}
