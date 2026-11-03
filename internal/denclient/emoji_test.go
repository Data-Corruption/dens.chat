package denclient_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// emojiPNG is a still emoji, as the page draws one.
func emojiPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// addEmoji adds a still emoji to a den, as staff's page does.
func addEmoji(t *testing.T, m *denclient.Manager, denID, name string, data []byte) denproto.Emoji {
	t.Helper()
	up := upload(t, m, denID, name+".png", data)
	e, err := m.AddEmoji(context.Background(), denID, name, up.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// emojiNames lists the den's emoji in a view, by name.
func emojiNames(v denclient.View) []string {
	var names []string
	for _, e := range v.Emoji {
		names = append(names, e.Name)
	}
	return names
}

// An animated GIF or WebP becomes an emoji as a GIF this service makes, at
// most 128 pixels on its longer side, its shape kept, never enlarged, and
// at most 512 KiB (M15.2).
func TestAnimatedEmoji(t *testing.T) {
	_, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	send := func(crop image.Rectangle, data []byte) (denclient.Uploaded, error) {
		return owner.UploadAnimated(ctx, denID, "emoji", crop, "dance.gif", int64(len(data)), bytes.NewReader(data), "")
	}
	for _, c := range []struct {
		crop          image.Rectangle
		width, height int
	}{
		{image.Rect(0, 0, 400, 200), 128, 64},
		{image.Rect(0, 0, 90, 300), 38, 128},
		// A small part, where the bar starts and leaves, isn't enlarged.
		{image.Rect(0, 100, 40, 120), 40, 20},
	} {
		up, err := send(c.crop, animatedGIF(t, 400, 300, 3))
		if err != nil || up.Type != "image/gif" || up.Width != c.width || up.Height != c.height || !up.Animated {
			t.Fatalf("%v: %+v %v", c.crop, up, err)
		}
		if _, err := owner.AddEmoji(ctx, denID, "dance_"+denproto.FormatID(int64(c.width)), up.ID); err != nil {
			t.Fatalf("%v: %v", c.crop, err)
		}
	}
	var input *denclient.InputError
	if _, err := send(image.Rect(0, 0, 128, 128), noisyGIF(t, 128, 128, 40)); !errors.As(err, &input) {
		t.Errorf("an emoji past its 512 KB: %v", err)
	}
	if _, err := send(image.Rect(0, 0, 0, 0), animatedGIF(t, 40, 40, 2)); !errors.As(err, &input) {
		t.Errorf("an emoji of nothing: %v", err)
	}
}

// TestTheDensEmojiComeAheadOfTheirUse checks that a member's service
// fetches the den's emoji into the cache as their list arrives and
// changes, so a page showing one fetches nothing from the den; that an
// emoji the cache let go of brings every one it lacks; and that a deleted
// emoji's image leaves the cache, while the member is there and while
// they're away.
func TestTheDensEmojiComeAheadOfTheirUse(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	s := newCachedStore(t)
	member, stop := cachedMember(t, h, owner, denID, s)
	defer func() { stop() }()
	den, err := denproto.ParseBytes(denID, denproto.IDSize)
	if err != nil {
		t.Fatal(err)
	}
	whole := func(e denproto.Emoji, thumb bool) bool {
		id, _ := denproto.ParseID(e.Image.ID)
		ok, err := s.cache.Whole(den, id, thumb)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	wave := addEmoji(t, owner, denID, "wave", emojiPNG(t, 96, 96))
	anim := animatedGIF(t, 200, 100, 3)
	up, err := owner.UploadAnimated(ctx, denID, "emoji", image.Rect(0, 0, 200, 100), "dance.gif", int64(len(anim)), bytes.NewReader(anim), "")
	if err != nil {
		t.Fatal(err)
	}
	dance, err := owner.AddEmoji(ctx, denID, "Dance", up.ID)
	if err != nil || dance.Name != "dance" || !dance.Image.Animated {
		t.Fatalf("%+v %v", dance, err)
	}
	v := viewOf(t, member, denID, "the den's emoji", func(v denclient.View) bool { return len(v.Emoji) == 2 })
	if !slices.Equal(emojiNames(v), []string{"dance", "wave"}) || v.EmojiLimits == nil || *v.EmojiLimits != denproto.DefaultEmojiLimits {
		t.Fatalf("the member sees the emoji %q and the limits %+v", emojiNames(v), v.EmojiLimits)
	}
	eventually(t, "the emoji's images, and the moving one's preview, in the cache", func() bool {
		return whole(wave, false) && whole(dance, false) && whole(dance, true)
	})

	// The page shows them from the cache alone.
	before := h.files.Load()
	for _, f := range []struct {
		e     denproto.Emoji
		thumb bool
	}{{wave, false}, {dance, false}, {dance, true}} {
		data, kind, err := fetch(t, member, denID, f.e.Image.ID, f.thumb)
		if err != nil || len(data) == 0 || !kind.Image() {
			t.Fatalf("%s: %v %s", f.e.Name, err, kind)
		}
	}
	if data, kind, _ := fetch(t, member, denID, dance.Image.ID, false); kind != media.GIF || len(data) == 0 {
		t.Errorf("the moving emoji is %s", kind)
	}
	if got := h.files.Load() - before; got != 0 {
		t.Errorf("showing the emoji fetched %d files from the den", got)
	}

	// One the cache let go of brings every one it lacks, so the den can't
	// tell which showed.
	if err := s.cache.Clear(); err != nil {
		t.Fatal(err)
	}
	before = h.files.Load()
	if _, _, err := fetch(t, member, denID, wave.Image.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := h.files.Load() - before; got != 3 || !whole(dance, false) || !whole(dance, true) {
		t.Errorf("an emoji the cache let go of fetched %d files", got)
	}

	// Deleted while the member is there, the image leaves the cache; a new
	// emoji comes as it's added.
	if err := owner.DeleteEmoji(ctx, denID, dance.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a deleted emoji's image to leave the cache", func() bool { return !whole(dance, false) && !whole(dance, true) })
	if _, _, err := fetch(t, member, denID, dance.Image.ID, false); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("a deleted emoji's image: %v", err)
	}
	party := addEmoji(t, owner, denID, "party", emojiPNG(t, 64, 32))
	eventually(t, "a new emoji's image in the cache", func() bool { return whole(party, false) })

	// Deleted while the member is away, the snapshot they come back to
	// says it went.
	stop()
	if err := owner.DeleteEmoji(ctx, denID, wave.ID); err != nil {
		t.Fatal(err)
	}
	h.restart()
	member, stop = restartMember(t, h, s, stop)
	viewOf(t, member, denID, "the emoji left", func(v denclient.View) bool { return slices.Equal(emojiNames(v), []string{"party"}) })
	eventually(t, "the emoji deleted while away to leave the cache", func() bool { return !whole(wave, false) })
	if !whole(party, false) {
		t.Error("the emoji left went from the cache")
	}

	// Turned off, emoji stay on the list and come as they show, where no
	// message shows them: in the list of them that staff keep.
	another := addEmoji(t, owner, denID, "another", emojiPNG(t, 16, 16))
	eventually(t, "another emoji's image in the cache", func() bool { return whole(another, false) })
	off := denproto.EmojiLimits{Emojis: 0}
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{EmojiLimits: &off}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "emoji off", func(v denclient.View) bool { return v.EmojiLimits != nil && v.EmojiLimits.Emojis == 0 })
	if err := s.cache.Clear(); err != nil {
		t.Fatal(err)
	}
	before = h.files.Load()
	if _, _, err := fetch(t, member, denID, party.Image.ID, false); err != nil {
		t.Errorf("an emoji's image while emoji are off: %v", err)
	}
	if got := h.files.Load() - before; got != 1 || whole(another, false) {
		t.Errorf("an emoji's image while emoji are off fetched %d files", got)
	}
}
