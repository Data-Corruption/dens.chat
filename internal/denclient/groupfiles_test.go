package denclient_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"

	"golang.org/x/crypto/chacha20poly1305"
)

// openIn opens a file as the page shows it, under the message it's on, in
// its channel.
func openIn(m *denclient.Manager, denID, channel, message, id string, thumb bool) (*denclient.OpenedFile, error) {
	return m.OpenFile(context.Background(), denID, id, denclient.FileAt{Channel: channel, Message: message}, thumb)
}

// readFile reads a file as the page shows it, under the message it's on:
// all of it, or n bytes at off when n isn't 0.
func readFile(m *denclient.Manager, denID, channel, message, id string, thumb bool, off, n int64) ([]byte, error) {
	f, err := openIn(m, denID, channel, message, id, thumb)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if n == 0 {
		return io.ReadAll(f)
	}
	b := make([]byte, n)
	if _, err := f.ReadAt(b, off); err != nil {
		return nil, err
	}
	return b, nil
}

// sealBlob seals plain under a file's key, as the sender's Dens does, or a
// member who holds the key.
func sealBlob(t *testing.T, key, ad, plain []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := vault.SealStreamWith(key, &b, ad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// resealChunk changes one chunk of a sealed blob and seals it again under
// the blob's key and nonces, as a member who holds the key can: the blob
// still opens whole, with other bytes in that chunk.
func resealChunk(t *testing.T, key, ad, blob []byte, chunk int) []byte {
	t.Helper()
	const header = 17
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		t.Fatal(err)
	}
	at := header + chunk*vault.SealedChunk
	end := min(at+vault.SealedChunk, len(blob))
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	copy(nonce, blob[1:header])
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(chunk))
	copy(nonce[16:23], c[1:])
	if end == len(blob) {
		nonce[23] = 1
	}
	plain, err := aead.Open(nil, nonce, blob[at:end], ad)
	if err != nil {
		t.Fatal(err)
	}
	for i := range plain {
		plain[i] ^= 0x5a
	}
	out := bytes.Clone(blob)
	copy(out[at:], aead.Seal(nil, nonce, plain, ad))
	return out
}

// uploadSealedAs uploads a sealed blob as a member's own, which the den
// stores without reading.
func uploadSealedAs(t *testing.T, h *denHost, member string, blob []byte) string {
	t.Helper()
	id, _ := denproto.ParseID(member)
	f, err := h.d.Upload(context.Background(), &den.Session{MemberID: id, Version: denproto.Version}, "", int64(len(blob)),
		bytes.NewReader(blob), true, "")
	if err != nil {
		t.Fatal(err)
	}
	return f.ID
}

// sendSealedAs sends a payload as a member, past their Dens's checks, with
// the blobs given on it at the den.
func sendSealedAs(t *testing.T, h *denHost, m *denclient.Manager, denID, channel, member string, p denproto.DMPayload, blobs []string) denproto.Message {
	t.Helper()
	keyID, nonce, sealed, err := denclient.SealPayloadAs(m, denID, channel, member, p)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := denproto.ParseID(member)
	msg, err := h.d.Send(context.Background(), &den.Session{MemberID: id, Version: denproto.Version}, channel,
		denproto.SendRequest{Nonce: nonce, Sealed: sealed, KeyID: keyID, Attachments: blobs})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// rawBlob is a blob as the den stores it, which any member of its place
// can fetch.
func rawBlob(t *testing.T, h *denHost, member, id string) []byte {
	t.Helper()
	who, _ := denproto.ParseID(member)
	r, err := h.d.OpenFile(context.Background(), &den.Session{MemberID: who, Version: denproto.Version}, id, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// groupFiles is a group of alice, bob, carol and erin, erin's install
// keeping a cache, where alice sent a photo and a file of nine megabytes
// in one message, which carol's and erin's Dens opened.
type groupFiles struct {
	h                       *denHost
	alice, bob, carol, erin *denclient.Manager
	ids                     []string
	denID, g                string
	msg                     denproto.Message
	photo, data             denclient.Uploaded
	dataPlain               []byte
}

func newGroupFiles(t *testing.T) *groupFiles {
	t.Helper()
	h, ms, ids, denID := groupDen(t)
	alice, bob, carol := ms[0], ms[1], ms[2]
	erin, _ := cachedGroupMember(t, h, alice, denID, newCachedStore(t), "erin", ids)
	ids = append(ids[:3:3], me(t, erin, denID).ID)
	ctx := context.Background()
	g, err := alice.MakeGroup(ctx, denID, ids[1:], "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []*denclient.Manager{alice, bob, carol, erin} {
		groupOf(t, m, denID, g, ids)
	}
	gf := &groupFiles{h: h, alice: alice, bob: bob, carol: carol, erin: erin, ids: ids, denID: denID, g: g}
	pic := gpsJPEG(t)
	if gf.photo, err = alice.Upload(ctx, denID, g, "IMG_2001.jpg", int64(len(pic)), bytes.NewReader(pic)); err != nil || gf.photo.Thumb == nil {
		t.Fatalf("the photo: %+v %v", gf.photo, err)
	}
	gf.dataPlain = randomBytes(9<<20 + 12345)
	if gf.data, err = alice.Upload(ctx, denID, g, "data.bin", int64(len(gf.dataPlain)), bytes.NewReader(gf.dataPlain)); err != nil {
		t.Fatal(err)
	}
	sent, err := alice.Send(ctx, denID, g, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{gf.photo.ID, gf.data.ID}})
	if err != nil {
		t.Fatal(err)
	}
	gf.msg = sent.Message
	for _, m := range []*denclient.Manager{carol, erin} {
		messageOn(t, m, denID, g, gf.msg.ID, "alice's files", func(pm denclient.PageMessage) bool { return len(pm.Attachments) == 2 })
	}
	return gf
}

// forged is what bob seals under the keys of alice's files: another photo
// and preview, her file with a chunk of its second run changed, and her
// file a chunk short, on a message of his, which the den then serves.
type forged struct {
	photo, thumb, run, short string
	photoPlain, thumbPlain   []byte
	thumbOf                  string // the blob of alice's photo's preview
}

func (gf *groupFiles) forge(t *testing.T) forged {
	t.Helper()
	p, err := denclient.PayloadOf(gf.bob, gf.denID, gf.g, gf.msg.ID)
	if err != nil || len(p.Files) != 2 || p.Files[0].Thumb == nil {
		t.Fatalf("alice's message as bob opens it: %+v %v", p, err)
	}
	fileAD, err := denclient.FileAD(gf.bob, gf.denID, gf.g, false)
	if err != nil {
		t.Fatal(err)
	}
	thumbAD, _ := denclient.FileAD(gf.bob, gf.denID, gf.g, true)
	f := forged{photoPlain: photo(t, 320, 240, 80), thumbPlain: photo(t, 64, 48, 70), thumbOf: p.Files[0].Thumb.ID}
	bob := gf.ids[1]
	f.photo = uploadSealedAs(t, gf.h, bob, sealBlob(t, p.Files[0].Key, fileAD, f.photoPlain))
	f.thumb = uploadSealedAs(t, gf.h, bob, sealBlob(t, p.Files[0].Key, thumbAD, f.thumbPlain))
	f.run = uploadSealedAs(t, gf.h, bob, resealChunk(t, p.Files[1].Key, fileAD, rawBlob(t, gf.h, bob, gf.data.ID), 20))
	f.short = uploadSealedAs(t, gf.h, bob, sealBlob(t, p.Files[1].Key, fileAD, gf.dataPlain[:len(gf.dataPlain)-vault.StreamChunk]))
	sendSealedAs(t, gf.h, gf.bob, gf.denID, gf.g, bob, denproto.DMPayload{Text: "mine"}, []string{f.photo, f.thumb, f.run, f.short})
	return f
}

// A group's file whose blob bob, working with the den, sealed other bytes
// for under its key doesn't open under alice's message for carol, nor for
// erin through her cache: not the photo, not its preview, not her file's
// second run, though its first opens, and not her file a chunk short. None
// of it was kept: once the den serves her blobs again, each opens as she
// sent it.
func TestGroupFileOthersSealed(t *testing.T) {
	gf := newGroupFiles(t)
	wantPhoto, err := readFile(gf.alice, gf.denID, gf.g, gf.msg.ID, gf.photo.ID, false, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantThumb, err := readFile(gf.alice, gf.denID, gf.g, gf.msg.ID, gf.photo.ID, true, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := gf.forge(t)
	gf.h.swap(gf.photo.ID, f.photo)
	gf.h.swap(f.thumbOf, f.thumb)
	gf.h.swap(gf.data.ID, f.run)
	second := int64(20*vault.StreamChunk + 10)
	for name, m := range map[string]*denclient.Manager{"carol": gf.carol, "erin": gf.erin} {
		if got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, gf.photo.ID, false, 0, 0); err == nil {
			t.Errorf("%s opened the photo the den swapped: bob's %v", name, bytes.Equal(got, f.photoPlain))
		}
		if got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, gf.photo.ID, true, 0, 0); err == nil {
			t.Errorf("%s opened the preview the den swapped: bob's %v", name, bytes.Equal(got, f.thumbPlain))
		}
		if got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 100<<10, 1000); err != nil || !bytes.Equal(got, gf.dataPlain[100<<10:][:1000]) {
			t.Errorf("%s's read in the file's first run: %v", name, err)
		}
		if got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, second, 1000); err == nil {
			t.Errorf("%s read the run the den swapped: hers %v", name, bytes.Equal(got, gf.dataPlain[second:][:1000]))
		}
	}
	// In a run neither has read, which erin's cache doesn't hold.
	gf.h.restore()
	gf.h.swap(gf.data.ID, f.short)
	for name, m := range map[string]*denclient.Manager{"carol": gf.carol, "erin": gf.erin} {
		if _, err := readFile(m, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 7<<20, 1000); err == nil {
			t.Errorf("%s opened her file a chunk short", name)
		}
	}
	gf.h.restore()
	for name, m := range map[string]*denclient.Manager{"carol": gf.carol, "erin": gf.erin} {
		got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, gf.photo.ID, false, 0, 0)
		if err != nil || !bytes.Equal(got, wantPhoto) {
			t.Errorf("%s's photo once the den serves hers: %v", name, err)
		}
		got, err = readFile(m, gf.denID, gf.g, gf.msg.ID, gf.photo.ID, true, 0, 0)
		if err != nil || !bytes.Equal(got, wantThumb) {
			t.Errorf("%s's preview once the den serves hers: %v", name, err)
		}
		got, err = readFile(m, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 0, 0)
		if err != nil || !bytes.Equal(got, gf.dataPlain) {
			t.Errorf("%s's file once the den serves hers: %v", name, err)
		}
	}
}

// A read at an offset fetches the run it falls in, and no more of the
// file than its header first: the run whole, from its start.
func TestGroupFileReadAtItsRun(t *testing.T) {
	gf := newGroupFiles(t)
	f, err := openIn(gf.carol, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before := len(gf.h.fileGets(gf.data.ID, 0))
	off := int64(100*vault.StreamChunk + 77)
	got := make([]byte, 5000)
	if _, err := f.ReadAt(got, off); err != nil || !bytes.Equal(got, gf.dataPlain[off:][:5000]) {
		t.Fatalf("the read at %d: %v", off, err)
	}
	want := fmt.Sprintf("bytes=%d-", 17+96*vault.SealedChunk)
	if gets := gf.h.fileGets(gf.data.ID, before); len(gets) != 1 || gets[0] != want {
		t.Errorf("the read fetched %q, not its run from %q", gets, want)
	}
}

// sumsOf digests a sealed blob as the sender's Dens does as it uploads.
func sumsOf(t *testing.T, blob []byte) denproto.Bytes {
	t.Helper()
	s := vault.NewRunSums(int64(len(blob)))
	if _, err := s.Write(blob); err != nil {
		t.Fatal(err)
	}
	sums, err := s.Sums()
	if err != nil {
		t.Fatal(err)
	}
	return sums
}

// A message of bob's own names alice's photo's blob, under its key, with
// digests of the bytes he put there, which the den serves for it. Under
// his message the photo opens as his, which it is; under hers, for carol
// and for erin through her cache, it doesn't, whichever of the two their
// Dens opened last, whichever the page asked for first, and once their
// Dens forgot every file's key, until the page loads her message again.
func TestGroupFileUnderTheForgersMessage(t *testing.T) {
	gf := newGroupFiles(t)
	p, err := denclient.PayloadOf(gf.bob, gf.denID, gf.g, gf.msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	theirs := p.Files[0]
	fileAD, _ := denclient.FileAD(gf.bob, gf.denID, gf.g, false)
	thumbAD, _ := denclient.FileAD(gf.bob, gf.denID, gf.g, true)
	pic, small := photo(t, 320, 240, 80), photo(t, 64, 48, 70)
	picBlob, smallBlob := sealBlob(t, theirs.Key, fileAD, pic), sealBlob(t, theirs.Key, thumbAD, small)
	bob := gf.ids[1]
	blobs := []string{uploadSealedAs(t, gf.h, bob, picBlob), uploadSealedAs(t, gf.h, bob, smallBlob)}
	mine := sendSealedAs(t, gf.h, gf.bob, gf.denID, gf.g, bob, denproto.DMPayload{Files: []denproto.DMFile{{
		ID: theirs.ID, Key: theirs.Key, Name: "mine.jpg", Type: "image/jpeg", Size: int64(len(pic)), Width: 320, Height: 240,
		Sums: sumsOf(t, picBlob), Thumb: &denproto.DMThumb{ID: theirs.Thumb.ID, Width: 64, Height: 48, Size: int64(len(small)), Sums: sumsOf(t, smallBlob)},
	}}}, blobs)
	gf.h.swap(theirs.ID, blobs[0])
	gf.h.swap(theirs.Thumb.ID, blobs[1])

	readers := map[string]*denclient.Manager{"carol": gf.carol, "erin": gf.erin}
	check := func(when string, hisFirst bool) {
		t.Helper()
		for name, m := range readers {
			for range 2 {
				if hisFirst {
					got, err := readFile(m, gf.denID, gf.g, mine.ID, theirs.ID, false, 0, 0)
					if err != nil || !bytes.Equal(got, pic) {
						t.Errorf("%s, %s: the photo under bob's message: %v", when, name, err)
					}
					got, err = readFile(m, gf.denID, gf.g, mine.ID, theirs.ID, true, 0, 0)
					if err != nil || !bytes.Equal(got, small) {
						t.Errorf("%s, %s: its preview under bob's message: %v", when, name, err)
					}
				} else {
					if got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, theirs.ID, false, 0, 0); err == nil {
						t.Errorf("%s, %s: the photo opened under alice's message: bob's %v", when, name, bytes.Equal(got, pic))
					}
					if got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, theirs.ID, true, 0, 0); err == nil {
						t.Errorf("%s, %s: its preview opened under alice's message: bob's %v", when, name, bytes.Equal(got, small))
					}
				}
				hisFirst = !hisFirst
			}
		}
	}
	// Bob's opened last, his file asked for first, then hers.
	for _, m := range readers {
		messageOn(t, m, gf.denID, gf.g, mine.ID, "bob's message", func(denclient.PageMessage) bool { return true })
	}
	check("his opened last", true)
	// Hers opened last, her file asked for first, then his.
	for name, m := range readers {
		page, err := m.History(context.Background(), gf.denID, gf.g, denclient.HistoryQuery{Before: mine.ID})
		if err != nil || len(page.Messages) == 0 || page.Messages[len(page.Messages)-1].ID != gf.msg.ID {
			t.Fatalf("%s's page before bob's message: %+v %v", name, page, err)
		}
	}
	check("hers opened last", false)

	// The den restarts, and each Dens takes its state anew, forgetting
	// the files' keys.
	gf.h.restart()
	for _, m := range readers {
		waitFor(t, m, "connected again", connected)
		viewOf(t, m, gf.denID, "the group again", func(v denclient.View) bool { _, ok := v.GroupDMs[gf.g]; return ok })
	}
	gf.h.swap(theirs.ID, blobs[0])
	gf.h.swap(theirs.Thumb.ID, blobs[1])
	for name, m := range readers {
		if got, err := readFile(m, gf.denID, gf.g, gf.msg.ID, theirs.ID, false, 0, 0); err == nil && bytes.Equal(got, pic) {
			t.Errorf("%s: with no key kept, the photo opened under alice's message as bob's", name)
		}
	}
	for name, m := range readers {
		page, err := m.History(context.Background(), gf.denID, gf.g, denclient.HistoryQuery{After: gf.msg.ID})
		if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != mine.ID {
			t.Fatalf("%s's page after alice's message: %+v %v", name, page, err)
		}
		if _, err := readFile(m, gf.denID, gf.g, gf.msg.ID, theirs.ID, false, 0, 0); err == nil {
			t.Errorf("%s: with only bob's message opened, the photo opened under alice's", name)
		}
		if got, err := readFile(m, gf.denID, gf.g, mine.ID, theirs.ID, false, 0, 0); err != nil || !bytes.Equal(got, pic) {
			t.Errorf("%s: with only bob's message opened, the photo under his: %v", name, err)
		}
	}
}

// digestsHold checks that each file of a sealed message, and its preview,
// carries the digests of the blob the den holds for it, as the reader's
// Dens opens the message.
func digestsHold(t *testing.T, h *denHost, reader *denclient.Manager, denID, channel, message string) {
	t.Helper()
	p, err := denclient.PayloadOf(reader, denID, channel, message)
	if err != nil || len(p.Files) == 0 {
		t.Fatalf("the message's seal: %+v %v", p, err)
	}
	member := me(t, reader, denID).ID
	for _, f := range p.Files {
		blob := rawBlob(t, h, member, f.ID)
		if !bytes.Equal(f.Sums, sumsOf(t, blob)) || int64(len(blob)) != vault.SealedSize(f.Size) {
			t.Errorf("%s: its digests aren't its blob's", f.Name)
		}
		if f.Thumb != nil {
			blob := rawBlob(t, h, member, f.Thumb.ID)
			if !bytes.Equal(f.Thumb.Sums, sumsOf(t, blob)) || int64(len(blob)) != vault.SealedSize(f.Thumb.Size) {
				t.Errorf("%s: its preview's digests aren't its blob's", f.Name)
			}
		}
	}
}

// A file sent with no digests, as an older Dens sends one, opens
// unchecked; so does one an older Dens's edit sealed again without them.
func TestGroupFileWithoutDigests(t *testing.T) {
	gf := newGroupFiles(t)
	ctx := context.Background()
	pic := gpsJPEG(t)
	up, err := gf.alice.Upload(ctx, gf.denID, gf.g, "old.jpg", int64(len(pic)), bytes.NewReader(pic))
	if err != nil {
		t.Fatal(err)
	}
	f, err := denclient.UploadedFile(gf.alice, gf.denID, up.ID)
	if err != nil || f.Sums == nil || f.Thumb.Sums == nil || f.Thumb.Size == 0 {
		t.Fatalf("the upload: %+v %v", f, err)
	}
	f.Sums, f.Thumb.Sums, f.Thumb.Size = nil, nil, 0
	old := sendSealedAs(t, gf.h, gf.alice, gf.denID, gf.g, gf.ids[0], denproto.DMPayload{Files: []denproto.DMFile{f}}, []string{f.ID, f.Thumb.ID})
	messageOn(t, gf.carol, gf.denID, gf.g, old.ID, "the old file", func(pm denclient.PageMessage) bool { return len(pm.Attachments) == 1 })
	want, err := readFile(gf.alice, gf.denID, "", "", up.ID, false, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readFile(gf.carol, gf.denID, gf.g, old.ID, up.ID, false, 0, 0); err != nil || !bytes.Equal(got, want) {
		t.Errorf("a file without digests: %v", err)
	}
	if _, err := readFile(gf.carol, gf.denID, gf.g, old.ID, up.ID, true, 0, 0); err != nil {
		t.Errorf("its preview: %v", err)
	}

	if err := denclient.EditPayloadAs(gf.alice, gf.denID, gf.g, gf.msg.ID, func(p *denproto.DMPayload) {
		p.Text = "edited on an older Dens"
		for i := range p.Files {
			p.Files[i].Sums = nil
		}
	}); err != nil {
		t.Fatal(err)
	}
	messageOn(t, gf.erin, gf.denID, gf.g, gf.msg.ID, "the edit", func(pm denclient.PageMessage) bool { return pm.Text == "edited on an older Dens" })
	if got, err := readFile(gf.erin, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 0, 0); err != nil || !bytes.Equal(got, gf.dataPlain) {
		t.Errorf("a file an older Dens's edit left without digests: %v", err)
	}
}

// Once each Dens took its state anew, holding no file's key, the den can't
// answer a file of alice's message with plain bytes of its own, a photo
// or her file's size of noise, for the page to show as hers or erin's
// cache to keep under her message; once her message loads again, the file
// opens as she sent it.
func TestGroupFileWithNoKeyHeld(t *testing.T) {
	gf := newGroupFiles(t)
	ctx := context.Background()
	if _, err := readFile(gf.erin, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 100<<10, 1000); err != nil {
		t.Fatal(err)
	}
	v, err := gf.bob.View(gf.denID)
	if err != nil || len(v.Channels) == 0 {
		t.Fatalf("bob's view: %v", err)
	}
	general := ""
	for _, c := range v.Channels {
		if c.Kind == denproto.KindText {
			general = c.ID
		}
	}
	pic := photo(t, 320, 240, 80)
	plainPhoto := upload(t, gf.bob, gf.denID, "plain.jpg", pic)
	plainData := upload(t, gf.bob, gf.denID, "plain.bin", randomBytes(len(gf.dataPlain)))
	if _, err := gf.bob.Send(ctx, gf.denID, general, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{plainPhoto.ID, plainData.ID}}); err != nil {
		t.Fatal(err)
	}
	gf.h.restart()
	for _, m := range []*denclient.Manager{gf.carol, gf.erin} {
		waitFor(t, m, "connected again", connected)
	}
	gf.h.swap(gf.photo.ID, plainPhoto.ID)
	gf.h.swap(gf.data.ID, plainData.ID)
	for name, m := range map[string]*denclient.Manager{"carol": gf.carol, "erin": gf.erin} {
		if f, err := openIn(m, gf.denID, gf.g, gf.msg.ID, gf.photo.ID, false); err == nil {
			t.Errorf("%s: with no key held, alice's photo came as the den's %v", name, f.Kind)
			f.Close()
		}
	}
	if _, err := readFile(gf.erin, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 7<<20, 1000); err == nil {
		t.Error("erin: with no key held, a run of alice's file came as the den's")
	}
	gf.h.restore()
	messageOn(t, gf.erin, gf.denID, gf.g, gf.msg.ID, "alice's message again", func(pm denclient.PageMessage) bool { return len(pm.Attachments) == 2 })
	if got, err := readFile(gf.erin, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 0, 0); err != nil || !bytes.Equal(got, gf.dataPlain) {
		t.Errorf("erin's copy of alice's file, once her message loads again: %v", err)
	}
	// Asked for in a channel under her message's ID, as a den can list a
	// message of its own, the file is the den's there, and her copy stays.
	gf.h.swap(gf.data.ID, plainData.ID)
	if got, err := readFile(gf.erin, gf.denID, general, gf.msg.ID, gf.data.ID, false, 7<<20, 1000); err != nil || bytes.Equal(got, gf.dataPlain[7<<20:][:1000]) {
		t.Errorf("erin's read of the file in a channel came from her copy of alice's: %v", err)
	}
	gf.h.stop()
	if got, err := readFile(gf.erin, gf.denID, gf.g, gf.msg.ID, gf.data.ID, false, 0, 0); err != nil || !bytes.Equal(got, gf.dataPlain) {
		t.Errorf("erin's copy of alice's file, with the den away: %v", err)
	}
}
