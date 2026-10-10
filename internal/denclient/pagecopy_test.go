package denclient_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
)

// Copies the page makes (M5.5), with a page stood in for: it takes the
// offer, asks for the video's packets a window at a time and checks them,
// and sends back AV1 packets the media module encoded, as Chrome's
// WebCodecs would.

type frame struct {
	text bool
	data []byte
}

// memSocket is the page's socket, in memory.
type memSocket struct {
	toPage, toService chan frame
}

func newMemSocket() *memSocket {
	return &memSocket{toPage: make(chan frame, 64), toService: make(chan frame, 64)}
}

func (s *memSocket) Read(ctx context.Context) (bool, []byte, error) {
	select {
	case f, ok := <-s.toService:
		if !ok {
			return false, nil, io.EOF
		}
		return f.text, f.data, nil
	case <-ctx.Done():
		return false, nil, ctx.Err()
	}
}

func (s *memSocket) Write(ctx context.Context, text bool, data []byte) error {
	select {
	case s.toPage <- frame{text, slices.Clone(data)}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pageMsg is a message on the socket, as the page reads and writes them.
type pageMsg struct {
	T       string           `json:"t"`
	Stage   string           `json:"stage,omitempty"`
	Done    float64          `json:"done,omitempty"`
	Copy    *denclient.Offer `json:"copy,omitempty"`
	Offer   int              `json:"offer,omitempty"`
	Bytes   int64            `json:"bytes,omitempty"`
	Packets int              `json:"packets,omitempty"`
	Reason  string           `json:"reason,omitempty"`
}

// standIn is how the page stood in for answers: accept and make the copy,
// decline, fail partway, go quiet, send records that aren't a copy's, or
// have the member send the video full size while it makes the copy.
type standIn int

const (
	makes standIn = iota
	declines
	fails
	goesQuiet
	sendsJunk
	sendsFull
)

// fakePage plays the page on a socket: source is what it encodes the copy
// from, which is the video itself unless the module can't decode it. It
// keeps the offers and the stages it saw.
type fakePage struct {
	t      *testing.T
	m      *denclient.Manager
	sock   *memSocket
	how    standIn
	source []byte
	// over, for the first offer, encodes at this bitrate instead, so the
	// copy comes out over the den's limit.
	over int
	// full asks for the video full size, as the chip's button does.
	full   func()
	mu     sync.Mutex
	offers []denclient.Offer
	stages []string
	got    int // the video's packets it was handed, each checked
	ctx    context.Context
}

func (p *fakePage) post(f frame) {
	select {
	case p.sock.toService <- f:
	case <-p.ctx.Done():
	}
}

func (p *fakePage) send(v pageMsg) {
	data, _ := json.Marshal(v)
	p.post(frame{true, data})
}

func (p *fakePage) run(ctx context.Context) {
	window := int64(64 << 10)
	var offer *denclient.Offer
	for {
		var f frame
		select {
		case f = <-p.sock.toPage:
		case <-ctx.Done():
			return
		}
		if !f.text {
			for off := 0; off < len(f.data); {
				size := int(binary.LittleEndian.Uint32(f.data[off:]))
				key := binary.LittleEndian.Uint32(f.data[off+4:])&1 == 1
				if p.got == 0 && !key {
					p.t.Error("the video's packets don't start with a keyframe")
				}
				off += 24 + size
				p.got++
			}
			p.send(pageMsg{T: "more", Offer: offer.ID, Bytes: window})
			continue
		}
		var msg pageMsg
		if err := json.Unmarshal(f.data, &msg); err != nil {
			p.t.Error(err)
			return
		}
		switch msg.T {
		case "progress":
			p.mu.Lock()
			if n := len(p.stages); n == 0 || p.stages[n-1] != msg.Stage {
				p.stages = append(p.stages, msg.Stage)
			}
			p.mu.Unlock()
		case "offer":
			offer = msg.Copy
			p.mu.Lock()
			p.offers = append(p.offers, *offer)
			p.mu.Unlock()
			p.got = 0
			if p.how == declines {
				p.send(pageMsg{T: "decline", Offer: offer.ID, Reason: "this browser can't decode it"})
				continue
			}
			p.send(pageMsg{T: "accept", Offer: offer.ID})
			p.send(pageMsg{T: "more", Offer: offer.ID, Bytes: window})
		case "packets-end":
			if p.got != offer.Packets {
				p.t.Errorf("handed %d packets of %d", p.got, offer.Packets)
			}
			if p.how == sendsFull {
				p.full()
				continue
			}
			p.copy(ctx, offer)
		}
	}
}

// copy makes the offer's copy, as the page would, and sends it.
func (p *fakePage) copy(ctx context.Context, o *denclient.Offer) {
	switch p.how {
	case goesQuiet:
		return
	case sendsJunk:
		junk := make([]byte, 24+10)
		binary.LittleEndian.PutUint32(junk, 10)
		p.post(frame{false, junk})
		return
	}
	e := o.Encoding
	kbps := e.Bitrate / 1000
	if p.over > 0 && o.ID == 1 {
		kbps = p.over
	}
	out := &ffmpeg.Buffer{}
	in := &ffmpeg.Buffer{}
	_, _ = in.WriteAt(p.source, 0)
	if _, err := p.m.Media.Encode(ctx, in, out, ffmpeg.Chunk{StartUS: e.StartUS, EndUS: 1 << 40, MaxSide: max(e.Width, e.Height),
		FPS: e.FPS, KBPS: kbps}, nil); err != nil {
		p.t.Error(err)
		return
	}
	recs := out.Bytes()
	n := 0
	for off := 0; off < len(recs); {
		end := off
		for end < len(recs) && end-off < 256<<10 {
			end += 24 + int(binary.LittleEndian.Uint32(recs[end:]))
			n++
		}
		if p.how == fails {
			p.send(pageMsg{T: "fail", Offer: o.ID, Reason: "the encoder stopped"})
			return
		}
		p.post(frame{false, recs[off:end]})
		off = end
	}
	p.send(pageMsg{T: "copied", Offer: o.ID, Packets: n})
}

// uploadWithPage uploads a video for a message with the page standing in
// as how says, or with no page at all for how < 0.
func uploadWithPage(t *testing.T, m *denclient.Manager, denID, channel, name string, data []byte, how standIn, source []byte, over int) (denclient.Uploaded, *fakePage, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key := "page-copy-" + denproto.Random(4).String()
	p := &fakePage{t: t, m: m, sock: newMemSocket(), how: how, source: source, over: over, ctx: ctx}
	if source == nil {
		p.source = data
	}
	p.full = func() {
		if err := m.SendFullSize(denID, key); err != nil {
			t.Errorf("full size instead: %v", err)
		}
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	if how >= 0 {
		wg.Go(func() { p.run(ctx) })
		wg.Go(func() {
			// The page follows the upload once the service has it.
			for ctx.Err() == nil {
				if err := m.FollowUpload(ctx, denID, key, p.sock); err == nil {
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
	up, err := m.UploadVersions(ctx, denID, channel, name, int64(len(data)), bytes.NewReader(data),
		denclient.UploadOptions{Send: denclient.SendSmaller, Progress: key, PageCopies: true})
	return up, p, err
}

func TestVideoCopiedByThePage(t *testing.T) {
	if raceOn {
		t.Skip("encoding in the module under the race detector takes minutes")
	}
	_, owner, member, denID, channelID := chatDen(t)
	member.PageOfferWait = 300 * time.Millisecond
	video := mediaFile(t, "with-gps.mov")

	// The page makes the copy: offered at the phone video's size, as WebCodecs
	// decodes H.264, and encodes AV1 at 0.04 bits a pixel.
	up, p, err := uploadWithPage(t, member, denID, channelID, "IMG_0001.MOV", video, makes, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.offers) != 1 {
		t.Fatalf("offers: %+v", p.offers)
	}
	o := p.offers[0]
	if !strings.HasPrefix(o.Decoding.Codec, "avc1.") || len(o.Decoding.Description) == 0 || o.Decoding.CodedWidth != 568 || o.Packets != 120 ||
		o.Encoding.Codec != "av01.0.08M.08" || o.Encoding.Width != 568 || o.Encoding.Height != 320 || o.Encoding.FPS != 30 {
		t.Errorf("the offer: %+v", o)
	}
	if v := up.Versions; v == nil || v.Sent != denclient.SendSmaller || up.Type != "video/mp4" || up.Width != 320 || up.Height != 568 ||
		up.Thumb == nil || v.Smaller.FPS < 29 {
		t.Fatalf("the upload: %+v", up)
	}
	if !slices.Contains(p.stages, "copying") {
		t.Errorf("the page saw %v", p.stages)
	}
	small, plays := openVersion(t, member, denID, up.ID, denclient.SendSmaller)
	if plays != "video/mp4" || !bytes.Contains(small, []byte("av01")) {
		t.Fatalf("the copy plays as %q", plays)
	}
	attach(t, member, denID, channelID, up.ID)
	if got, kind, err := fetch(t, owner, denID, up.ID, false); err != nil || !bytes.Contains(got, []byte("av01")) {
		t.Fatalf("the owner fetching the copy: %v %s", err, kind)
	}

	// Declined, failed partway, gone quiet, sending what isn't a copy, or
	// not there at all, the page leaves the copy to the module.
	for _, how := range []standIn{declines, fails, goesQuiet, sendsJunk, -1} {
		// The page stood in for encodes the copy whole before it sends
		// any, where a page sends it as it goes; going quiet, it sends
		// nothing.
		member.PageQuiet = 30 * time.Second
		if how == goesQuiet {
			member.PageQuiet = 500 * time.Millisecond
		}
		up, _, err := uploadWithPage(t, member, denID, channelID, "IMG_0001.MOV", video, how, nil, 0)
		if err != nil {
			t.Fatalf("%d: %v", how, err)
		}
		if up.Versions == nil || up.Versions.Sent != denclient.SendSmaller || up.Type != "video/mp4" || up.Thumb == nil {
			t.Errorf("the module's copy, after %d: %+v", how, up)
		}
	}

	// Sent full size while the page makes the copy, the video goes as it
	// is, and the module makes no copy instead.
	member.PageQuiet = 30 * time.Second
	up, _, err = uploadWithPage(t, member, denID, channelID, "IMG_0001.MOV", video, sendsFull, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if up.Versions != nil || up.Type != "video/mp4" || up.Width != 320 || up.Thumb == nil {
		t.Fatalf("a video sent full size while the page made its copy: %+v", up)
	}
}

// A WebM's VP9, which the module doesn't decode, goes as the page's copy,
// and a copy over the den's limit is offered again, lower (M5.5).
func TestPageCopyRules(t *testing.T) {
	if raceOn {
		t.Skip("encoding in the module under the race detector takes minutes")
	}
	_, owner, member, denID, channelID := chatDen(t)
	member.PageOfferWait = 300 * time.Millisecond
	ctx := context.Background()

	// The page decodes VP9; the module, standing in, encodes another
	// video's frames at the WebM's size.
	webm := mediaFile(t, "vp9.webm")
	up, p, err := uploadWithPage(t, member, denID, channelID, "clip.webm", webm, makes, mediaFile(t, "grain-60fps.mp4"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.offers) != 1 || p.offers[0].Decoding.Codec != "vp09.00.41.08" || len(p.offers[0].Decoding.Description) != 0 ||
		p.offers[0].Encoding.Width != 640 || p.offers[0].Encoding.Height != 360 {
		t.Fatalf("the WebM's offer: %+v", p.offers)
	}
	if up.Versions == nil || up.Type != "video/mp4" || up.Width != 640 || up.Versions.Full.Type != "video/webm" {
		t.Fatalf("the WebM's copy: %+v", up)
	}
	// Without the page, the module makes no copy of VP9.
	up, _, err = uploadWithPage(t, member, denID, channelID, "clip.webm", webm, declines, nil, 0)
	if err != nil || up.Versions != nil || up.Type != "video/webm" {
		t.Fatalf("the WebM without the page: %+v, %v", up, err)
	}

	// Over a 1 MiB limit, the page's first copy comes out over, at 1.6
	// Mbps, and the second, offered lower, fits.
	limits := denproto.Limits{FileSize: denproto.MinFileSize, MemberStorage: 1 << 30, DenStorage: 1 << 31}
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{Limits: &limits}); err != nil {
		t.Fatal(err)
	}
	viewOf(t, member, denID, "the new limits", func(v denclient.View) bool { return v.Limits == limits })
	grain := mediaFile(t, "grain-60fps.mp4")
	up, p, err = uploadWithPage(t, member, denID, channelID, "grain.mp4", grain, makes, nil, 1600)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.offers) != 2 || p.offers[1].ID != 2 || p.offers[1].Encoding.Bitrate >= p.offers[0].Encoding.Bitrate {
		t.Fatalf("offers: %+v", p.offers)
	}
	if up.Versions == nil || up.Versions.Full.Fits || up.Size > denproto.MinFileSize {
		t.Fatalf("a video over the limit: %+v", up)
	}
}
