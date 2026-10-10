package denclient_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// Smaller copies of videos (M5.4), from an iPhone's video of 568 × 320,
// turned a quarter, four seconds long; and a 60-frame video of 640 × 360,
// six seconds long and 1.3 MB, over a den's smallest limit.

// follower follows an upload by its key as the page does, and keeps the
// progress it saw.
type follower struct {
	mu   sync.Mutex
	seen []denclient.Progress
	stop chan struct{}
	done chan struct{}
}

func follow(m *denclient.Manager, denID, key string) *follower {
	f := &follower{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(f.done)
		for {
			if p, err := m.UploadProgress(denID, key); err == nil {
				f.mu.Lock()
				if n := len(f.seen); n == 0 || f.seen[n-1] != p {
					f.seen = append(f.seen, p)
				}
				f.mu.Unlock()
			}
			select {
			case <-f.stop:
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}()
	return f
}

// until waits for the upload to reach a stage, and says whether it did.
func (f *follower) until(stage string) bool {
	for range 5000 {
		f.mu.Lock()
		at := slices.ContainsFunc(f.seen, func(p denclient.Progress) bool { return p.Stage == stage })
		f.mu.Unlock()
		if at {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

func (f *follower) end() []denclient.Progress {
	close(f.stop)
	<-f.done
	return f.seen
}

// near says whether two sizes are within 2% of each other.
func near(a, b int64) bool {
	return a*50 >= b*49 && a*49 <= b*50
}

func uploadFollowed(m *denclient.Manager, denID, channel, name string, data []byte, send denclient.Send, key string) (denclient.Uploaded, error) {
	return m.UploadVersions(context.Background(), denID, channel, name, int64(len(data)), bytes.NewReader(data), send, "", key)
}

// openVersion reads a version kept of a video waiting to be sent, and
// what it plays as.
func openVersion(t *testing.T, m *denclient.Manager, denID, id string, which denclient.Send) ([]byte, string) {
	t.Helper()
	f, err := m.OpenVersion(denID, id, which)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return data, f.PlayType()
}

// A phone's video goes as a smaller copy, AV1 in an MP4 with a preview made
// from its full size, while the page follows how far it is; its sender
// compares the two and switches to full size and back; and the owner gets
// the copy, which the den strips again, with its preview (M5.4).
func TestVideoGoesSmaller(t *testing.T) {
	if raceOn {
		t.Skip("encoding in the module under the race detector takes minutes")
	}
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	data := mediaFile(t, "with-gps.mov")
	f := follow(member, denID, "video-copy-1")
	up, err := uploadFollowed(member, denID, channelID, "IMG_0001.MOV", data, denclient.SendSmaller, "video-copy-1")
	seen := f.end()
	if err != nil {
		t.Fatal(err)
	}
	v := up.Versions
	if v == nil || v.Sent != denclient.SendSmaller || up.Type != "video/mp4" || up.Width != 320 || up.Height != 568 ||
		up.Thumb == nil || !up.Stripped || up.Name != "IMG_0001.MOV" || up.Duration < 3900 {
		t.Fatalf("the upload: %+v", up)
	}
	// The den strips a video again, as it does any, so what it keeps is a
	// few bytes off what this service sent.
	if v.Smaller.Type != "video/mp4" || !near(v.Smaller.Size, up.Size) || v.Smaller.Width != 320 || v.Smaller.Height != 568 ||
		v.Smaller.FPS < 29 || v.Smaller.FPS > 31 || !v.Smaller.Fits {
		t.Fatalf("the copy: %+v", v.Smaller)
	}
	if v.Full.Type != "video/mp4" || v.Full.Width != 320 || v.Full.Height != 568 || v.Full.FPS < 29 || v.Full.FPS > 31 ||
		!v.Full.Fits || v.Full.Size*3 < v.Smaller.Size*4 {
		t.Fatalf("the full size: %+v", v.Full)
	}
	// The page saw the copy made, then sent, and the upload's progress
	// goes with it.
	copying, sending := -1, -1
	for i, p := range seen {
		switch {
		case p.Stage == "copying" && copying < 0:
			copying = i
		case p.Stage == "sending" && sending < 0:
			sending = i
		}
	}
	if copying < 0 || sending < copying || seen[len(seen)-1].Done != 1 {
		t.Errorf("the page saw %+v", seen)
	}
	if _, err := member.UploadProgress(denID, "video-copy-1"); err == nil {
		t.Error("the upload is still followed once it's done")
	}

	small, plays := openVersion(t, member, denID, up.ID, denclient.SendSmaller)
	if plays != "video/mp4" || int64(len(small)) != v.Smaller.Size || !bytes.Contains(small, []byte("av01")) {
		t.Fatalf("the copy plays as %q, %d bytes", plays, len(small))
	}
	full, plays := openVersion(t, member, denID, up.ID, denclient.SendFull)
	if plays != "video/mp4" || int64(len(full)) != v.Full.Size || !bytes.Contains(full, []byte("avc1")) ||
		bytes.Contains(full, []byte("com.apple.quicktime")) {
		t.Fatalf("the full size plays as %q, %d bytes", plays, len(full))
	}

	// Switching uploads the other version: the den makes the full size's
	// preview itself, and the copy takes its own again.
	big, err := member.SwitchVersion(ctx, denID, up.ID, denclient.SendFull)
	if err != nil {
		t.Fatal(err)
	}
	if !near(big.Size, v.Full.Size) || big.Thumb == nil || big.Versions.Sent != denclient.SendFull {
		t.Fatalf("switched to full size: %+v", big)
	}
	back, err := member.SwitchVersion(ctx, denID, big.ID, denclient.SendSmaller)
	if err != nil {
		t.Fatal(err)
	}
	if !near(back.Size, v.Smaller.Size) || back.Thumb == nil || back.Versions.Sent != denclient.SendSmaller {
		t.Fatalf("switched back: %+v", back)
	}

	attach(t, member, denID, channelID, back.ID)
	got, kind, err := fetch(t, owner, denID, back.ID, false)
	if err != nil || kind != media.Video || !bytes.Contains(got, []byte("av01")) {
		t.Fatalf("the owner fetching the copy: %v %s", err, kind)
	}
	if _, kind, err := fetch(t, owner, denID, back.ID, true); err != nil || kind != media.JPEG {
		t.Fatalf("the copy's preview: %v %s", err, kind)
	}
}

// A DM's video goes as a smaller copy too, sealed with the preview made
// from its full size, and opens for the other member (M5.4).
func TestDMVideoGoesSmaller(t *testing.T) {
	if raceOn {
		t.Skip("encoding in the module under the race detector takes minutes")
	}
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	up := uploadVersions(t, owner, denID, dm, "IMG_0001.MOV", mediaFile(t, "with-gps.mov"), denclient.SendSmaller)
	if up.Versions == nil || up.Versions.Sent != denclient.SendSmaller || up.Type != "video/mp4" || up.Thumb == nil ||
		up.Width != 320 || up.Height != 568 {
		t.Fatalf("a DM's video: %+v", up)
	}
	big, err := owner.SwitchVersion(ctx, denID, up.ID, denclient.SendFull)
	if err != nil || big.Thumb == nil {
		t.Fatalf("switched to full size: %+v %v", big, err)
	}
	back, err := owner.SwitchVersion(ctx, denID, big.ID, denclient.SendSmaller)
	if err != nil || back.Thumb == nil {
		t.Fatalf("switched back: %+v %v", back, err)
	}
	if _, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{back.ID}}); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for range 100 {
		if got, _, err = fetch(t, member, denID, back.ID, false); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || !bytes.Contains(got, []byte("av01")) {
		t.Fatalf("the member fetching the copy: %v", err)
	}
	if _, kind, err := fetch(t, member, denID, back.ID, true); err != nil || kind != media.JPEG {
		t.Fatalf("the copy's preview: %v %s", err, kind)
	}
}

// A video over the den's limit goes as a copy fitted to it, whatever the
// setting, and can't switch to full size, nor go full size while it's
// made. Within the limit, the member can have it go full size instead of
// waiting for its copy, and one sent full size by the setting gets none. A
// video the module can't copy, and audio, go full size (M5.4).
func TestVideoCopyRules(t *testing.T) {
	if raceOn {
		t.Skip("encoding in the module under the race detector takes minutes")
	}
	_, owner, member, denID, channelID := chatDen(t)
	ctx := context.Background()
	grain := mediaFile(t, "grain-60fps.mp4")
	setLimit := func(size int64) {
		t.Helper()
		limits := denproto.Limits{FileSize: size, MemberStorage: 1 << 30, DenStorage: 1 << 31}
		if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
			t.Fatal(err)
		}
		viewOf(t, member, denID, "the new limits", func(v denclient.View) bool { return v.Limits == limits })
	}

	setLimit(denproto.MinFileSize)
	f := follow(member, denID, "over-the-limit")
	asked := make(chan struct{})
	go func() {
		defer close(asked)
		if !f.until("copying") {
			t.Error("the copy never started")
		} else if err := member.SendFullSize(denID, "over-the-limit"); !errors.Is(err, denclient.ErrTooLarge) {
			t.Errorf("full size over the limit: %v", err)
		}
	}()
	up, err := uploadFollowed(member, denID, channelID, "grain.mp4", grain, denclient.SendFull, "over-the-limit")
	<-asked
	f.end()
	if err != nil {
		t.Fatal(err)
	}
	if up.Versions == nil || up.Versions.Sent != denclient.SendSmaller || up.Versions.Full.Fits || up.Size > denproto.MinFileSize ||
		up.Width != 640 || up.Height != 360 || up.Thumb == nil {
		t.Fatalf("a video over the limit: %+v", up)
	}
	if fps := up.Versions.Smaller.FPS; fps < 29 || fps > 31 {
		t.Errorf("the copy of a 60-frame video runs at %v frames a second", fps)
	}
	if _, err := member.SwitchVersion(ctx, denID, up.ID, denclient.SendFull); !errors.Is(err, denclient.ErrTooLarge) {
		t.Fatalf("switching to a full size over the limit: %v", err)
	}

	setLimit(denproto.DefaultFileSize)
	f = follow(member, denID, "send-it-full")
	asked = make(chan struct{})
	go func() {
		defer close(asked)
		if !f.until("copying") {
			t.Error("the copy never started")
		} else if err := member.SendFullSize(denID, "send-it-full"); err != nil {
			t.Errorf("full size instead: %v", err)
		}
	}()
	up, err = uploadFollowed(member, denID, channelID, "grain.mp4", grain, denclient.SendSmaller, "send-it-full")
	<-asked
	f.end()
	if err != nil {
		t.Fatal(err)
	}
	if up.Versions != nil || up.Type != "video/mp4" || up.Size < denproto.MinFileSize || up.Thumb == nil {
		t.Fatalf("a video sent full size while its copy was made: %+v", up)
	}

	// Sent full size by the setting, a video gets no copy, which would take
	// minutes for nothing.
	up = uploadVersions(t, member, denID, channelID, "IMG_0001.MOV", mediaFile(t, "with-gps.mov"), denclient.SendFull)
	if up.Versions != nil || up.Type != "video/mp4" || up.Width != 320 || up.Thumb == nil {
		t.Fatalf("a video sent full size: %+v", up)
	}
	// The module decodes no AV1, so a copy uploaded again has no copy of
	// its own, and no preview either, which the page draws.
	copied := uploadVersions(t, member, denID, channelID, "IMG_0001.MOV", mediaFile(t, "with-gps.mov"), denclient.SendSmaller)
	av1, _ := openVersion(t, member, denID, copied.ID, denclient.SendSmaller)
	again := uploadVersions(t, member, denID, channelID, "copy.mp4", av1, denclient.SendSmaller)
	if again.Versions != nil || again.Type != "video/mp4" || again.Thumb != nil || again.Width != 320 {
		t.Fatalf("a video the module can't copy: %+v", again)
	}
	// Audio in a video's container goes as audio does.
	sound := uploadVersions(t, member, denID, channelID, "song.webm", mediaFile(t, "meta.webm"), denclient.SendSmaller)
	if sound.Versions != nil || sound.Type != "audio/webm" {
		t.Fatalf("audio in WebM: %+v", sound)
	}
}
