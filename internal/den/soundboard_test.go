package den

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/sfu/sfutest"
)

// soundUpload uploads a sound of n packets of 20 ms, each of size bytes,
// as the page prepares one, and returns the upload.
func (f *fixture) soundUpload(s *Session, n, size int) denproto.File {
	f.t.Helper()
	return f.mustUpload(s, "sound.ogg", sfutest.Sound(sfutest.SoundPackets(n, size)))
}

func (f *fixture) addSound(s *Session, name string, n int) (denproto.Sound, error) {
	f.t.Helper()
	return f.d.AddSound(context.Background(), s, denproto.SoundRequest{Name: name, FileID: f.soundUpload(s, n, 160).ID})
}

func (f *fixture) mustAddSound(s *Session, name string, n int) denproto.Sound {
	f.t.Helper()
	sound, err := f.addSound(s, name, n)
	if err != nil {
		f.t.Fatal(err)
	}
	return sound
}

func (f *fixture) setSoundLimits(owner *Session, l denproto.SoundLimits) {
	f.t.Helper()
	if _, err := f.d.Update(context.Background(), owner, denproto.DenUpdateRequest{SoundLimits: &l}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) soundNames(s *Session) []string {
	f.t.Helper()
	snap, err := f.d.snapshot(context.Background(), s.MemberID, IsStaff(s.Role), 0, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, sound := range snap.Sounds {
		if err := denproto.CheckSound(sound); err != nil {
			f.t.Errorf("the snapshot's sound %+v: %v", sound, err)
		}
		names = append(names, sound.Name)
	}
	return names
}

// TestStaffKeepTheSoundboard checks that staff alone add, rename and
// delete the den's sounds, each by a name cleaned as a display name is and
// unique in any case, that every member hears of each change and finds the
// sounds in their snapshot, and that a deleted sound's file goes.
func TestStaffKeepTheSoundboard(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.moderator(owner, "carol")
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	defer f.d.Hub.Unsubscribe(sub)

	if info, _ := f.d.Info(); info.SoundLimits != denproto.DefaultSoundLimits {
		t.Fatalf("a new den's soundboard limits are %+v", info.SoundLimits)
	}
	_, err := f.addSound(member, "Mine", 10)
	wantCode(t, err, denproto.CodeForbidden)

	horn := f.mustAddSound(owner, "  Air‮ horn  ", 50)
	if horn.Name != "Air horn" || horn.AddedBy != denproto.FormatID(owner.MemberID) || horn.AddedAt != f.clock.UnixMilli() ||
		horn.Duration != (50*960-312)*1000/48000 {
		t.Fatalf("the sound added is %+v", horn)
	}
	if got := nextEvent[denproto.Sound](t, sub, denproto.EventSoundCreated); got != horn {
		t.Errorf("members heard of %+v, not %+v", got, horn)
	}
	_, err = f.addSound(carol, "AIR HORN", 10)
	wantCode(t, err, denproto.CodeSoundNameTaken)
	bell := f.mustAddSound(carol, "Bell", 10)
	if got := f.soundNames(member); !slices.Equal(got, []string{"Air horn", "Bell"}) {
		t.Fatalf("the snapshot's sounds are %q", got)
	}

	_, err = f.d.RenameSound(ctx, member, bell.ID, denproto.SoundUpdate{Name: "Gong"})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.RenameSound(ctx, carol, bell.ID, denproto.SoundUpdate{Name: "air horn"})
	wantCode(t, err, denproto.CodeSoundNameTaken)
	_, err = f.d.RenameSound(ctx, carol, "999", denproto.SoundUpdate{Name: "Gong"})
	wantCode(t, err, denproto.CodeNotFound)
	gong, err := f.d.RenameSound(ctx, owner, bell.ID, denproto.SoundUpdate{Name: "Gong"})
	if err != nil || gong.Name != "Gong" || gong.ID != bell.ID || gong.FileID != bell.FileID {
		t.Fatalf("renamed: %+v, %v", gong, err)
	}
	if got := nextEvent[denproto.Sound](t, sub, denproto.EventSoundUpdated); got != gong {
		t.Errorf("members heard of %+v, not %+v", got, gong)
	}
	// A sound keeps its own name, in another case.
	if _, err := f.d.RenameSound(ctx, carol, horn.ID, denproto.SoundUpdate{Name: "AIR HORN"}); err != nil {
		t.Errorf("a sound renamed in another case: %v", err)
	}

	before, _ := f.stored()
	wantCode(t, f.d.DeleteSound(ctx, member, gong.ID), denproto.CodeForbidden)
	if err := f.d.DeleteSound(ctx, carol, gong.ID); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.d.DeleteSound(ctx, carol, gong.ID), denproto.CodeNotFound)
	if got := nextEvent[denproto.SoundDeleted](t, sub, denproto.EventSoundDeleted); got.ID != gong.ID {
		t.Errorf("members heard of the deletion of %q", got.ID)
	}
	if after, _ := f.stored(); len(after) != len(before)-1 {
		t.Errorf("deleting a sound left %d stored files of %d", len(after), len(before))
	}
	if _, err := f.read(owner, gong.FileID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("a deleted sound's file: %v", err)
	}
	if got := f.soundNames(owner); !slices.Equal(got, []string{"AIR HORN"}) {
		t.Errorf("the sounds left are %q", got)
	}
}

// testWAV is a moment of silence as a WAV file.
func testWAV() []byte {
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint32(b, 8000)
	b = binary.LittleEndian.AppendUint32(b, 16000)
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, 1600)
	b = append(b, make([]byte, 1600)...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	return b
}

// TestTheDensSoundChecks checks what a sound the den takes is: Opus in
// Ogg, no longer than the owner allows, give or take the encoder's last
// packet, no larger than its length at the bitrate makes, an upload of the
// staff member's own waiting to be used, and within the den's count, while
// the soundboard is on; and that lowering the limits keeps the sounds
// already added.
func TestTheDensSoundChecks(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	add := func(file denproto.File, name string) error {
		_, err := f.d.AddSound(ctx, owner, denproto.SoundRequest{Name: name, FileID: file.ID})
		return err
	}

	wav := f.mustUpload(owner, "silence.wav", testWAV())
	if wav.Type != "audio/wav" {
		t.Fatalf("the WAV is %s", wav.Type)
	}
	wantCode(t, add(wav, "Silence"), denproto.CodeInvalidField)

	f.setSoundLimits(owner, denproto.SoundLimits{Sounds: 3, Longest: 2, Wait: 5})
	// 101 packets of 20 ms play 2.0136 s after the pre-skip, which fits
	// but for the last packet's padding; 102 don't.
	if err := add(f.soundUpload(owner, 101, 160), "Just fits"); err != nil {
		t.Errorf("a sound within its last packet of the longest: %v", err)
	}
	wantCode(t, add(f.soundUpload(owner, 102, 160), "Too long"), denproto.CodeInvalidField)
	// A second at 160 kbps is past what 64 kbps makes, with its room.
	wantCode(t, add(f.soundUpload(owner, 50, 400), "Too large"), denproto.CodeInvalidField)

	// The member's own upload, waiting to be used.
	theirs := f.soundUpload(member, 10, 160)
	wantCode(t, add(theirs, "Theirs"), denproto.CodeInvalidField)
	used := f.soundUpload(owner, 10, 160)
	if err := add(used, "Once"); err != nil {
		t.Fatal(err)
	}
	wantCode(t, add(used, "Twice"), denproto.CodeInvalidField)
	stale := f.soundUpload(owner, 10, 160)
	f.clock = f.clock.Add(uploadExpiry + time.Minute)
	wantCode(t, add(stale, "Stale"), denproto.CodeInvalidField)

	if err := add(f.soundUpload(owner, 10, 160), "Third"); err != nil {
		t.Fatal(err)
	}
	wantCode(t, add(f.soundUpload(owner, 10, 160), "Fourth"), denproto.CodeInvalidField)
	f.setSoundLimits(owner, denproto.SoundLimits{Sounds: 1, Longest: 1, Wait: 0})
	if got := f.soundNames(owner); len(got) != 3 {
		t.Errorf("lowering the limits left the sounds %q", got)
	}
	f.setSoundLimits(owner, denproto.SoundLimits{Sounds: 0, Longest: 10, Wait: 5})
	wantCode(t, add(f.soundUpload(owner, 10, 160), "Off"), denproto.CodeInvalidField)
	if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{SoundLimits: &denproto.SoundLimits{Sounds: 1, Longest: 0}}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Errorf("limits out of range: %v", err)
	}
	if _, err := f.d.Update(ctx, member, denproto.DenUpdateRequest{SoundLimits: &denproto.DefaultSoundLimits}); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Errorf("a member setting the limits: %v", err)
	}
	// They come back as they were set when the den opens again.
	f.open()
	if info, _ := f.d.Info(); info.SoundLimits != (denproto.SoundLimits{Sounds: 0, Longest: 10, Wait: 5}) {
		t.Errorf("the den opened with the soundboard's limits %+v", info.SoundLimits)
	}
	if preview, err := f.d.Preview(ctx, f.invite(owner, 1).Code); err != nil || preview.SoundLimits != (denproto.SoundLimits{}) {
		t.Errorf("a join preview shows the soundboard's limits %+v, %v", preview.SoundLimits, err)
	}
}

// TestASoundsFileIsTheDens checks that a sound's file counts toward the
// den's space and not its uploader's, isn't among their files, opens for
// staff alone, and stays through retention, the sweep of waiting uploads
// and its uploader leaving; and that no message, picture or replacement
// takes it.
func TestASoundsFileIsTheDens(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.moderator(owner, "carol")
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	before, err := f.d.Storage(ctx, carol)
	if err != nil {
		t.Fatal(err)
	}
	upload := f.soundUpload(carol, 50, 160)
	sound, err := f.d.AddSound(ctx, carol, denproto.SoundRequest{Name: "Horn", FileID: upload.ID})
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.d.Storage(ctx, carol)
	if err != nil {
		t.Fatal(err)
	}
	if after.Used != before.Used || after.DenUsed != before.DenUsed+upload.Size {
		t.Errorf("the sound took %d of its uploader's space and %d of the den's, not 0 and %d",
			after.Used-before.Used, after.DenUsed-before.DenUsed, upload.Size)
	}
	if page, err := f.d.OwnFiles(ctx, carol, ""); err != nil || len(page.Files) != 0 {
		t.Errorf("the uploader's files: %+v, %v", page.Files, err)
	}

	stored, err := f.read(owner, sound.FileID, false)
	if err != nil || !bytes.HasPrefix(stored, []byte("OggS")) {
		t.Fatalf("staff open the sound's file: %v", err)
	}
	if _, err := f.read(member, sound.FileID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("a member opening a sound's file: %v", err)
	}

	_, err = f.d.Send(ctx, carol, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{sound.FileID}})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.UpdateProfile(ctx, carol, denproto.ProfileRequest{Avatar: &sound.FileID})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.Upload(ctx, carol, "swap.ogg", -1, bytes.NewReader(sfutest.Sound(sfutest.SoundPackets(5, 160))), false, sound.FileID)
	wantCode(t, err, denproto.CodeInvalidField)

	f.send(carol, c.ID, "old news")
	f.setRetention(owner, 1)
	f.clock = f.clock.Add(48 * time.Hour)
	f.d.expire()
	f.d.sweepUploads()
	if err := f.d.Leave(ctx, carol); err != nil {
		t.Fatal(err)
	}
	if _, err := f.read(owner, sound.FileID, false); err != nil {
		t.Errorf("after retention, the sweep and its uploader leaving, the sound's file: %v", err)
	}
	if got := f.soundNames(owner); !slices.Equal(got, []string{"Horn"}) {
		t.Errorf("the sounds left are %q", got)
	}
}
