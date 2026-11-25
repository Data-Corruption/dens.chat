package denclient_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// messageOn returns a message as a member's Dens shows it, from the
// channel's history, once cond holds of it.
func messageOn(t *testing.T, m *denclient.Manager, denID, channel, id, what string, cond func(denclient.PageMessage) bool) denclient.PageMessage {
	t.Helper()
	var got denclient.PageMessage
	eventually(t, what, func() bool {
		for _, msg := range history(t, m, denID, channel) {
			if msg.ID == id {
				got = msg
				return cond(msg)
			}
		}
		return false
	})
	return got
}

// readUnder reads a file as the page shows it under a message, in its
// channel: all of it, or n bytes at off when n isn't 0.
func readUnder(m *denclient.Manager, denID, channel, message, id string, off, n int64) ([]byte, error) {
	f, err := m.OpenFile(context.Background(), denID, id, denclient.FileAt{Channel: channel, Message: message}, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if n == 0 {
		n = f.Size() - off
	}
	b := make([]byte, n)
	if _, err := f.ReadAt(b, off); err != nil {
		return nil, err
	}
	return b, nil
}

// sentFiles is a message with a photo and a file of three megabytes.
type sentFiles struct {
	channel, message string
	photo, data      denclient.Uploaded
	dataPlain        []byte
}

func sendFiles(t *testing.T, m *denclient.Manager, denID, channel string) sentFiles {
	t.Helper()
	ctx := context.Background()
	sf := sentFiles{channel: channel, dataPlain: randomBytes(3<<20 + 12345)}
	pic := gpsJPEG(t)
	var err error
	if sf.photo, err = m.Upload(ctx, denID, channel, "IMG_2001.jpg", int64(len(pic)), bytes.NewReader(pic)); err != nil || sf.photo.Thumb == nil {
		t.Fatalf("the photo: %+v %v", sf.photo, err)
	}
	if sf.data, err = m.Upload(ctx, denID, channel, "data.bin", int64(len(sf.dataPlain)), bytes.NewReader(sf.dataPlain)); err != nil {
		t.Fatal(err)
	}
	sent, err := m.Send(ctx, denID, channel, denproto.SendRequest{Nonce: denproto.Random(16),
		Attachments: []string{sf.photo.ID, sf.data.ID}})
	if err != nil {
		t.Fatal(err)
	}
	sf.message = sent.ID
	return sf
}

// Once a Dens holds no key for a file of a DM or of notes, after the den
// restarts and gives its state anew or after the Dens itself restarts, the
// den can't answer the file with plain bytes of its own, a photo or noise
// of the file's size, for the page to show or the cache to keep under its
// message: not for alice, whose Dens keeps no cache, and not for carol
// through hers. Once the message loads again, the file opens as it was
// sent, from the cache too while the den is away. Asked for in a channel
// under the message's ID, the file is the den's, and carol's copy stays.
func TestDMFileWithNoKeyHeld(t *testing.T) {
	h, alice, bob, denID, general := chatDen(t)
	s := newCachedStore(t)
	carol, stop := cachedMember(t, h, alice, denID, s)
	defer func() { stop() }()
	dm := openDM(t, alice, carol, denID)
	readyDM(t, alice, carol, denID, dm)
	notes := openNotes(t, carol, denID).ID
	inDM, inNotes := sendFiles(t, alice, denID, dm), sendFiles(t, carol, denID, notes)
	type reading struct {
		who string
		m   *denclient.Manager
		sf  sentFiles
	}
	readings := []reading{{"alice in the DM", alice, inDM}, {"carol in the DM", carol, inDM}, {"carol in her notes", carol, inNotes}}
	for _, r := range readings {
		messageOn(t, r.m, denID, r.sf.channel, r.sf.message, r.who+": the files' message", func(pm denclient.PageMessage) bool { return len(pm.Attachments) == 2 })
	}
	for _, sf := range []sentFiles{inDM, inNotes} {
		if _, err := readUnder(carol, denID, sf.channel, sf.message, sf.data.ID, 100<<10, 1000); err != nil {
			t.Fatal(err)
		}
	}
	plainPhoto := upload(t, bob, denID, "plain.jpg", photo(t, 320, 240, 80))
	plainData := upload(t, bob, denID, "plain.bin", randomBytes(len(inDM.dataPlain)))
	if _, err := bob.Send(context.Background(), denID, general, denproto.SendRequest{Nonce: denproto.Random(16),
		Attachments: []string{plainPhoto.ID, plainData.ID}}); err != nil {
		t.Fatal(err)
	}
	swapAll := func() {
		for _, sf := range []sentFiles{inDM, inNotes} {
			h.swap(sf.photo.ID, plainPhoto.ID)
			h.swap(sf.data.ID, plainData.ID)
		}
	}
	refused := func(when string, readings []reading) {
		t.Helper()
		for _, r := range readings {
			if got, err := readUnder(r.m, denID, r.sf.channel, r.sf.message, r.sf.photo.ID, 0, 0); err == nil {
				t.Errorf("%s, %s: the photo came as the den's %s", when, r.who, media.Sniff(got))
			}
			if r.m == carol {
				if _, err := readUnder(r.m, denID, r.sf.channel, r.sf.message, r.sf.data.ID, 2<<20, 1000); err == nil {
					t.Errorf("%s, %s: a part of the file came as the den's", when, r.who)
				}
			}
		}
	}
	opened := func(when string, readings []reading) {
		t.Helper()
		for _, r := range readings {
			messageOn(t, r.m, denID, r.sf.channel, r.sf.message, r.who+": the message again", func(pm denclient.PageMessage) bool { return len(pm.Attachments) == 2 })
			if got, err := readUnder(r.m, denID, r.sf.channel, r.sf.message, r.sf.photo.ID, 0, 0); err != nil || media.Sniff(got) != media.JPEG || int64(len(got)) != r.sf.photo.Size {
				t.Errorf("%s, %s: the photo once its message loads again: %v", when, r.who, err)
			}
			if r.m == carol {
				if got, err := readUnder(r.m, denID, r.sf.channel, r.sf.message, r.sf.data.ID, 0, 0); err != nil || !bytes.Equal(got, r.sf.dataPlain) {
					t.Errorf("%s, %s: the file once its message loads again: %v", when, r.who, err)
				}
			}
		}
	}

	h.restart()
	for _, m := range []*denclient.Manager{alice, carol} {
		waitFor(t, m, "connected again", connected)
	}
	swapAll()
	refused("after the den's restart", readings)
	h.restore()
	opened("after the den's restart", readings)

	h.swap(inDM.data.ID, plainData.ID)
	if got, err := readUnder(carol, denID, general, inDM.message, inDM.data.ID, 2<<20, 1000); err != nil || bytes.Equal(got, inDM.dataPlain[2<<20:][:1000]) {
		t.Errorf("carol's read of the file in a channel came from her copy of alice's: %v", err)
	}
	h.restore()

	carol, stop = restartMember(t, h, s, stop)
	readings = []reading{{"carol in the DM", carol, inDM}, {"carol in her notes", carol, inNotes}}
	swapAll()
	refused("after carol's restart", readings)
	h.restore()
	opened("after carol's restart", readings)

	h.stop()
	for _, r := range readings {
		if got, err := readUnder(carol, denID, r.sf.channel, r.sf.message, r.sf.data.ID, 0, 0); err != nil || !bytes.Equal(got, r.sf.dataPlain) {
			t.Errorf("%s: the file with the den away: %v", r.who, err)
		}
	}
}

// A sealed message names its files by ID, and can name any. One of
// carol's in her DM with alice that names bob's picture as a file of its
// own leaves his picture opening as the page asks for it, under no
// message, for alice and for carol through her cache.
func TestDMMessageNamingAPicture(t *testing.T) {
	h, alice, bob, denID, _ := chatDen(t)
	carol, stop := cachedMember(t, h, alice, denID, newCachedStore(t))
	defer stop()
	dm := openDM(t, alice, carol, denID)
	readyDM(t, alice, carol, denID, dm)
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}
	up := upload(t, bob, denID, "avatar.png", b.Bytes())
	if _, err := bob.UpdateProfile(context.Background(), denID, denproto.ProfileRequest{Avatar: &up.ID}); err != nil {
		t.Fatal(err)
	}
	bobID := me(t, bob, denID).ID
	readers := map[string]*denclient.Manager{"alice": alice, "carol": carol}
	for name, m := range readers {
		viewOf(t, m, denID, name+": bob's picture", func(v denclient.View) bool {
			bob, ok := memberIn(v, bobID)
			return ok && bob.Avatar.ID == up.ID
		})
	}
	carolID := me(t, carol, denID).ID
	keyID, nonce, sealed, err := denclient.SealPayloadAs(carol, denID, dm, carolID, denproto.DMPayload{Text: "mine", Files: []denproto.DMFile{{
		ID: up.ID, Key: denproto.Random(32), Name: "mine.png", Type: "image/png", Size: 100,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	who, _ := denproto.ParseID(carolID)
	mine, err := h.d.Send(context.Background(), &den.Session{MemberID: who, Version: denproto.Version}, dm,
		denproto.SendRequest{Nonce: nonce, Sealed: sealed, KeyID: keyID})
	if err != nil {
		t.Fatal(err)
	}
	for name, m := range readers {
		messageOn(t, m, denID, dm, mine.ID, name+": carol's message", func(pm denclient.PageMessage) bool { return len(pm.Attachments) == 1 })
		if _, kind, err := fetch(t, m, denID, up.ID, false); err != nil || kind != media.PNG {
			t.Errorf("%s: bob's picture, once a message of carol's names it: %v %s", name, err, kind)
		}
	}
}
