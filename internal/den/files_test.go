package den

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// testJPEG is a photo as a client uploads it: already stripped.
func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withComment gives a JPEG metadata a client should have stripped.
func withComment(jpg []byte) []byte {
	seg := append([]byte{0xFF, 0xFE, 0, 15}, "SecretComment"...)
	return append(append(append([]byte{}, jpg[:2]...), seg...), jpg[2:]...)
}

func (f *fixture) upload(s *Session, name string, data []byte) (denproto.File, error) {
	return f.d.Upload(context.Background(), s, name, int64(len(data)), bytes.NewReader(data), false, "")
}

func (f *fixture) mustUpload(s *Session, name string, data []byte) denproto.File {
	f.t.Helper()
	file, err := f.upload(s, name, data)
	if err != nil {
		f.t.Fatal(err)
	}
	return file
}

func (f *fixture) read(s *Session, id string, thumb bool) ([]byte, error) {
	r, err := f.d.OpenFile(context.Background(), s, id, thumb)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err == nil && int64(len(data)) != r.Size() {
		f.t.Fatalf("file %s: %d bytes, stated %d", id, len(data), r.Size())
	}
	return data, err
}

// stored lists the files in the den's storage and temporary directories.
func (f *fixture) stored() (files, temps []string) {
	f.t.Helper()
	for _, dir := range []struct {
		path string
		out  *[]string
	}{{f.storage.Dir, &files}, {f.storage.Temp, &temps}} {
		entries, err := os.ReadDir(dir.path)
		if err != nil {
			f.t.Fatal(err)
		}
		for _, e := range entries {
			*dir.out = append(*dir.out, e.Name())
		}
	}
	return files, temps
}

func TestUploadStoresSealed(t *testing.T) {
	f, owner, member := chatFixture(t)
	photo := testJPEG(t, 1200, 900)
	file := f.mustUpload(member, "../holiday/beach.jpg", photo)
	if file.Name != "_holiday_beach.jpg" || file.Type != "image/jpeg" || file.Size != int64(len(photo)) ||
		file.Width != 1200 || file.Height != 900 || file.Thumb == nil || file.Thumb.Width != 640 || file.Thumb.Height != 480 {
		t.Fatalf("%+v", file)
	}
	files, temps := f.stored()
	if len(files) != 2 || len(temps) != 0 {
		t.Fatalf("stored %v, temporary %v", files, temps)
	}
	for _, name := range files {
		data, _ := os.ReadFile(filepath.Join(f.storage.Dir, name))
		if bytes.Contains(data, photo[100:200]) {
			t.Fatal("a file is stored in the clear")
		}
	}
	var sealedName []byte
	f.db.QueryRow(`SELECT name FROM den_files WHERE id = ?`, file.ID).Scan(&sealedName)
	if bytes.Contains(sealedName, []byte("beach")) {
		t.Fatal("a file's name is stored in the clear")
	}
	got, err := f.read(member, file.ID, false)
	if err != nil || !bytes.Equal(got, photo) {
		t.Fatalf("read back: %v", err)
	}
	thumb, err := f.read(member, file.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(thumb)); err != nil || cfg.Width != 640 {
		t.Fatalf("preview: %+v %v", cfg, err)
	}
	// Until a message uses it, only its uploader can fetch it.
	if _, err := f.read(owner, file.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("someone else read a pending upload: %v", err)
	}
	st, err := f.d.Storage(context.Background(), member)
	if err != nil || st.Used != file.Size+int64(len(thumb)) || st.DenUsed != st.Used {
		t.Fatalf("storage %+v, %v", st, err)
	}

	other := f.mustUpload(member, "notes.txt", []byte("just some notes\n"))
	if other.Type != "text/plain" || other.Thumb != nil || other.Width != 0 {
		t.Fatalf("%+v", other)
	}
}

func TestUploadRefusals(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	_, err := f.upload(member, "gps.jpg", withComment(testJPEG(t, 20, 20)))
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.upload(member, "cut.jpg", testJPEG(t, 50, 50)[:300])
	wantCode(t, err, denproto.CodeInvalidField)

	limits := denproto.Limits{FileSize: 1 << 20, MemberStorage: 3 << 20, DenStorage: 4 << 20}
	if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, 1<<20+1)
	_, err = f.upload(member, "big.bin", big)
	wantCode(t, err, denproto.CodeTooLarge)
	// Without a stated length, the den stops reading past the limit.
	_, err = f.d.Upload(ctx, member, "big.bin", -1, bytes.NewReader(big), false, "")
	wantCode(t, err, denproto.CodeTooLarge)

	chunk := make([]byte, 1<<20)
	for range 3 {
		f.mustUpload(member, "chunk.bin", chunk)
	}
	_, err = f.upload(member, "chunk.bin", chunk)
	wantCode(t, err, denproto.CodeQuotaExceeded)
	f.mustUpload(owner, "chunk.bin", chunk)
	_, err = f.upload(owner, "chunk.bin", chunk)
	wantCode(t, err, denproto.CodeDenFull)

	limits = denproto.Limits{FileSize: 1 << 20, MemberStorage: 1 << 30, DenStorage: 1 << 31}
	if _, err := f.d.Update(ctx, owner, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	f.d.files.freeSpace = func(string) (uint64, error) { return freeSpaceFloor + 100, nil }
	_, err = f.upload(member, "chunk.bin", chunk)
	wantCode(t, err, denproto.CodeDenFull)

	files, temps := f.stored()
	if len(files) != 4 || len(temps) != 0 {
		t.Fatalf("refused uploads left files: stored %d, temporary %v", len(files), temps)
	}
	_, err = f.d.Update(ctx, member, denproto.DenUpdateRequest{Limits: &limits})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.Update(ctx, owner, denproto.DenUpdateRequest{Limits: &denproto.Limits{FileSize: 1}})
	wantCode(t, err, denproto.CodeInvalidField)
}

func TestAttachments(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	staff := f.newChannel(owner, "staff", denproto.ChannelRequest{StaffOnly: ptr(true)})
	carol := f.member(owner, "carol")
	sub, _, _, _ := f.d.Hub.Subscribe(carol.MemberID, false, "", 0)

	a := f.mustUpload(member, "a.jpg", testJPEG(t, 64, 48))
	b := f.mustUpload(member, "b.txt", []byte("hello"))
	nonce := denproto.Random(16)
	m, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: nonce, Attachments: []string{b.ID, a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Text != "" || len(m.Attachments) != 2 || m.Attachments[0].ID != b.ID || m.Attachments[1].Name != "a.jpg" {
		t.Fatalf("%+v", m)
	}
	// A retry returns the message, files and all, and doesn't need them
	// pending any more.
	again, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: nonce, Attachments: []string{b.ID, a.ID}})
	if err != nil || again.ID != m.ID || len(again.Attachments) != 2 {
		t.Fatalf("retry: %+v %v", again, err)
	}
	page, err := f.d.History(ctx, carol, c.ID, HistoryQuery{})
	if err != nil || len(page.Messages) != 1 || len(page.Messages[0].Attachments) != 2 {
		t.Fatalf("history: %+v %v", page, err)
	}
	if _, err := f.read(carol, a.ID, true); err != nil {
		t.Fatalf("carol reading a preview: %v", err)
	}
	reply, err := f.d.Send(ctx, owner, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "nice", ReplyTo: m.ID})
	if err != nil || reply.Reply == nil || reply.Reply.Text != "" {
		t.Fatalf("a reply to a message with only files: %+v %v", reply, err)
	}

	// Files go only once, only the uploader's, only with a message.
	_, err = f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{a.ID}})
	wantCode(t, err, denproto.CodeInvalidField)
	mine := f.mustUpload(owner, "mine.txt", []byte("x"))
	_, err = f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{mine.ID}})
	wantCode(t, err, denproto.CodeInvalidField)
	_, err = f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16)})
	wantCode(t, err, denproto.CodeInvalidField)

	// A file in a staff-only channel is staff's to see.
	secret := f.mustUpload(owner, "plan.txt", []byte("plans"))
	f.mustSendFiles(owner, staff.ID, secret.ID)
	if _, err := f.read(carol, secret.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("carol read a staff file: %v", err)
	}

	// An edit may clear the text of a message with files, not of one
	// without.
	if _, err := f.d.Edit(ctx, member, m.ID, denproto.EditRequest{Revision: 1, Text: "  "}); err != nil {
		t.Fatalf("clearing a caption: %v", err)
	}
	if _, err := f.d.Edit(ctx, owner, reply.ID, denproto.EditRequest{Revision: 1, Text: ""}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Fatalf("clearing a message without files: %v", err)
	}

	for len(sub.Events) > 0 {
		<-sub.Events
	}
	if err := f.d.Delete(ctx, member, m.ID); err != nil {
		t.Fatal(err)
	}
	var deleted denproto.MessageDeleted
	for e := range sub.Events {
		if e.T == denproto.EventMessageDeleted {
			json.Unmarshal(e.D, &deleted)
			break
		}
	}
	if len(deleted.Files) != 2 || deleted.Files[0] != b.ID || deleted.Files[1] != a.ID {
		t.Fatalf("message.deleted %+v", deleted)
	}
	if _, err := f.read(member, a.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("a deleted message's file: %v", err)
	}
	// The staff file and the owner's pending upload are what's left.
	files, _ := f.stored()
	if len(files) != 2 {
		t.Fatalf("stored after delete: %v", files)
	}
	if err := f.d.DeleteChannel(ctx, owner, staff.ID); err != nil {
		t.Fatal(err)
	}
	if files, _ := f.stored(); len(files) != 1 {
		t.Fatalf("stored after deleting the channel: %v", files)
	}
}

func (f *fixture) mustSendFiles(s *Session, channel string, files ...string) denproto.Message {
	f.t.Helper()
	m, err := f.d.Send(context.Background(), s, channel, f.sealedSend(s, channel, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: files}))
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func TestDMFiles(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	carol := f.member(owner, "carol")
	dm, err := f.d.OpenDM(ctx, member, denproto.DMRequest{MemberID: denproto.FormatID(carol.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	// A DM takes only files its members' clients sealed, which the den
	// keeps as they came, and channels take none.
	plain := f.mustUpload(member, "for carol.txt", []byte("hi"))
	_, err = f.d.Send(ctx, member, dm.ID, f.sealedSend(member, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{plain.ID}}))
	wantCode(t, err, denproto.CodeInvalidField)
	blob := []byte("sealed by the client, which the den can't tell from noise")
	file, err := f.d.Upload(ctx, member, "ignored.txt", int64(len(blob)), bytes.NewReader(blob), true, "")
	if err != nil || !file.Sealed || file.Name != "" || file.Type != denproto.SealedType || file.Size != int64(len(blob)) || file.Thumb != nil {
		t.Fatalf("a sealed upload: %+v %v", file, err)
	}
	if err := denproto.CheckFile(file); err != nil {
		t.Fatalf("a client checking it: %v", err)
	}
	general := f.newChannel(owner, "general", denproto.ChannelRequest{})
	_, err = f.d.Send(ctx, member, general.ID, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x", Attachments: []string{file.ID}})
	wantCode(t, err, denproto.CodeInvalidField)
	m := f.mustSendFiles(member, dm.ID, file.ID)
	if m.Attachments != nil {
		t.Fatalf("a DM message lists its files outside its sealed text: %+v", m.Attachments)
	}
	if got, err := f.read(carol, file.ID, false); err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("carol reading a DM file: %q %v", got, err)
	}
	if _, err := f.read(carol, file.ID, true); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("a sealed file has no preview of the den's: %v", err)
	}
	if _, err := f.read(owner, file.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("the owner read a DM file through the den: %v", err)
	}
	// A sealed upload isn't a profile picture either.
	another, err := f.d.Upload(ctx, member, "", 3, bytes.NewReader([]byte("abc")), true, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &another.ID})
	wantCode(t, err, denproto.CodeInvalidField)
	// A DM message holds each file and its preview.
	var ids []string
	for range denproto.MaxDMFiles + 1 {
		u, err := f.d.Upload(ctx, member, "", 1, bytes.NewReader([]byte("x")), true, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID)
	}
	_, err = f.d.Send(ctx, member, dm.ID, f.sealedSend(member, dm.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: ids}))
	wantCode(t, err, denproto.CodeInvalidField)
	f.mustSendFiles(member, dm.ID, ids[:denproto.MaxDMFiles]...)
}

func TestProfilePictures(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	avatar := f.mustUpload(member, "me.png", testPNG(t, 256, 256))
	m, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &avatar.ID})
	if err != nil || m.Avatar != (denproto.Image{ID: avatar.ID, Width: 256, Height: 256}) {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := f.read(owner, avatar.ID, false); err != nil {
		t.Fatalf("anyone sees an avatar: %v", err)
	}
	snap, err := f.d.snapshot(ctx, owner.MemberID, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, sm := range snap.Members {
		if sm.ID == m.ID && sm.Avatar.ID != avatar.ID {
			t.Fatalf("ready's member %+v", sm)
		}
	}

	for name, c := range map[string]struct {
		data   []byte
		banner bool
	}{
		"not square":    {testPNG(t, 300, 200), false},
		"too small":     {testPNG(t, 32, 32), false},
		"not a picture": {[]byte("text"), false},
		"banner shape":  {testPNG(t, 600, 300), true},
	} {
		up := f.mustUpload(member, "x", c.data)
		req := denproto.ProfileRequest{Avatar: &up.ID}
		if c.banner {
			req = denproto.ProfileRequest{Banner: &up.ID}
		}
		if _, err := f.d.UpdateProfile(ctx, member, req); !denproto.IsCode(err, denproto.CodeInvalidField) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var anim bytes.Buffer
	pal := color.Palette{color.Black, color.White}
	frames := []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 64, 64), pal), image.NewPaletted(image.Rect(0, 0, 64, 64), pal)}
	gif.EncodeAll(&anim, &gif.GIF{Image: frames, Delay: []int{5, 5}})
	up := f.mustUpload(member, "anim.gif", anim.Bytes())
	if _, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &up.ID}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Errorf("an animated avatar: %v", err)
	}
	theirs := f.mustUpload(owner, "theirs.png", testPNG(t, 128, 128))
	if _, err := f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &theirs.ID}); !denproto.IsCode(err, denproto.CodeInvalidField) {
		t.Errorf("someone else's upload: %v", err)
	}

	banner := f.mustUpload(member, "wide.jpg", testJPEG(t, 1200, 400))
	next := f.mustUpload(member, "me2.png", testPNG(t, 512, 512))
	m, err = f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Avatar: &next.ID, Banner: &banner.ID})
	if err != nil || m.Avatar.ID != next.ID || m.Banner.ID != banner.ID {
		t.Fatalf("%+v %v", m, err)
	}
	// The old avatar is gone.
	if _, err := f.read(member, avatar.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("the replaced avatar: %v", err)
	}
	// A picture in use isn't pending: it can't go on a message.
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	_, err = f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{next.ID}})
	wantCode(t, err, denproto.CodeInvalidField)

	empty := ""
	m, err = f.d.UpdateProfile(ctx, member, denproto.ProfileRequest{Banner: &empty})
	if err != nil || m.Banner != (denproto.Image{}) || m.Avatar.ID != next.ID {
		t.Fatalf("clearing the banner: %+v %v", m, err)
	}
	if _, err := f.read(member, banner.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("the cleared banner: %v", err)
	}

	// Leaving takes the pictures and anything unsent with it.
	f.mustUpload(member, "unsent.txt", []byte("x"))
	if err := f.d.Leave(ctx, member); err != nil {
		t.Fatal(err)
	}
	left, err := f.d.Member(ctx, member.MemberID)
	if err != nil || left.Avatar != (denproto.Image{}) {
		t.Fatalf("after leaving: %+v %v", left, err)
	}
	var n int
	f.db.QueryRow(`SELECT count(*) FROM den_files WHERE uploader_id = ?`, member.MemberID).Scan(&n)
	if n != 0 {
		t.Fatalf("%d of the member's files are left", n)
	}
}

func TestSweepAndTidy(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	kept := f.mustUpload(member, "kept.txt", []byte("kept"))
	f.mustSendFiles(member, c.ID, kept.ID)
	stale := f.mustUpload(member, "stale.txt", []byte("stale"))
	f.clock = f.clock.Add(uploadExpiry + time.Minute)
	_, err := f.d.Send(ctx, member, c.ID, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{stale.ID}})
	wantCode(t, err, denproto.CodeInvalidField)
	f.d.sweepUploads()
	if _, err := f.read(member, stale.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Fatalf("an expired upload: %v", err)
	}
	if files, _ := f.stored(); len(files) != 1 {
		t.Fatalf("stored after the sweep: %v", files)
	}

	// What a crash leaves behind goes when the den starts.
	orphan := filepath.Join(f.storage.Dir, "00112233445566778899aabbccddeeff")
	part := filepath.Join(f.storage.Temp, partPrefix+"123")
	mine := filepath.Join(f.storage.Dir, "notes.txt")
	for _, p := range []string{orphan, orphan + thumbSuffix, part, mine} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.open()
	files, temps := f.stored()
	if len(files) != 2 || len(temps) != 0 {
		t.Fatalf("after reopening: stored %v, temporary %v", files, temps)
	}
	if got, err := f.read(member, kept.ID, false); err != nil || string(got) != "kept" {
		t.Fatalf("a kept file after reopening: %q %v", got, err)
	}
}

func TestRemovalDeletesFiles(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	file := f.mustUpload(member, "x.txt", []byte("x"))
	f.mustSendFiles(member, c.ID, file.ID)
	if err := f.d.Remove(ctx, owner, denproto.FormatID(member.MemberID), denproto.RemoveRequest{DeleteMessages: 3600}); err != nil {
		t.Fatal(err)
	}
	if files, _ := f.stored(); len(files) != 0 {
		t.Fatalf("stored after removing with messages: %v", files)
	}
}
