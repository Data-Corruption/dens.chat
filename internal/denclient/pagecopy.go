package denclient

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Copies the page makes (M5.5). In Chrome and Edge, the page makes a
// video's copy itself with WebCodecs, often on the graphics card. It says
// so as it uploads the video, and follows the upload over a socket of its
// own, which carries the upload's progress either way. Once this service
// has stripped the video, it offers the page the copy: what decoding the
// video's packets takes, which the media module hands out, and how to
// encode the copy. The page answers, asks for the packets as it decodes
// them, and sends back the copy's AV1 packets, which the module puts
// together with the video's sound. A page that declines, fails or goes
// quiet leaves the copy to the module.
//
// The socket carries JSON in its text messages, and packets in its binary
// ones, as records (internal/media/ffmpeg's driver, Packets), whole in
// each message. A socket, unlike a request, doesn't wait behind the
// browser's few connections to this service, which the uploads themselves
// hold while their copies are made.

// PageSocket is the page's end of an upload it follows.
type PageSocket interface {
	Read(ctx context.Context) (text bool, data []byte, err error)
	Write(ctx context.Context, text bool, data []byte) error
}

// Offer is what the page needs to make a video's copy: what it decodes the
// video's packets by, how it encodes the copy, how many packets there are,
// and where the video ends, for its progress.
type Offer struct {
	ID       int                `json:"id"`
	Decoding media.Decoding     `json:"decoding"`
	Encoding media.PageEncoding `json:"encoding"`
	Packets  int                `json:"packets"`
	EndUS    int64              `json:"end_us"`
}

// pageMsg is a message on the socket, either way.
type pageMsg struct {
	T string `json:"t"`
	// From this service: the upload's progress, an offer, the end of an
	// offer's packets, or an offer withdrawn.
	Stage string  `json:"stage,omitempty"`
	Done  float64 `json:"done,omitempty"`
	Copy  *Offer  `json:"copy,omitempty"`
	// From the page: an offer accepted or declined, more of its packets,
	// its copy sent whole, or failed.
	Offer   int    `json:"offer,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Packets int    `json:"packets,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// How long the page has to answer an offer, and to go between the messages
// of a copy it makes, before the module makes the copy instead. Tests
// shorten them.
const (
	pageOfferWait = 10 * time.Second
	pageQuiet     = 30 * time.Second
)

// Records, as the driver writes and reads them.
const (
	recordHeader = 24
	maxRecord    = 16 << 20
	maxTime      = int64(1) << 52
	// sendFrame bounds the binary messages this service sends.
	sendFrame = 1 << 20
)

// MaxPageMessage bounds the messages the page sends on its socket, which
// the socket must take: the page sends a copy's records a megabyte or so at
// a time.
const MaxPageMessage = 4 << 20

var (
	errPageGone  = errors.New("the page went before it made the copy")
	errPageQuiet = errors.New("the page went quiet while it made the copy")
	errNoAnswer  = errors.New("the page didn't answer the offer")
)

// pageCopy is a copy on offer to the page, or being made by it.
type pageCopy struct {
	offer  Offer
	follow *following
	// packets are the video's, for the page; received the copy's, from
	// it. The uploader owns both.
	packets, received *vault.Scratch
	answer, result    chan error
	cancel            chan struct{}
	cancelOnce        sync.Once
	activity          atomic.Int64

	// The socket's side, which only it touches.
	answered, finished bool
	ended              bool // the end of the packets went to the page
	sent, got          int64
	gotPackets         int
	lastPTS            int64
	cancelSent         bool
}

func (pc *pageCopy) touch() { pc.activity.Store(time.Now().UnixNano()) }

func (pc *pageCopy) stop() { pc.cancelOnce.Do(func() { close(pc.cancel) }) }

func (pc *pageCopy) stopped() bool {
	select {
	case <-pc.cancel:
		return true
	default:
		return false
	}
}

// pageMakes offers the page a copy and waits for it to make it, and
// returns the copy's packets, which received holds: the page's answer
// within pageOfferWait, and its messages no further apart than pageQuiet.
func (m *Manager) pageMakes(ctx context.Context, f *following, offer Offer, packets *vault.Scratch) (*vault.Scratch, error) {
	received, err := vault.NewScratch(m.TempDir, scratchPattern)
	if err != nil {
		return nil, err
	}
	pc := &pageCopy{offer: offer, follow: f, packets: packets, received: received, answer: make(chan error, 1),
		result: make(chan error, 1), cancel: make(chan struct{}), lastPTS: -maxTime}
	pc.touch()
	f.offer(pc)
	defer f.withdraw(pc)
	fail := func(err error) (*vault.Scratch, error) {
		received.Close()
		return nil, err
	}
	f.set("offered", 0)
	wait := time.NewTimer(m.PageOfferWait)
	defer wait.Stop()
	select {
	case err := <-pc.answer:
		if err != nil {
			return fail(err)
		}
	case <-wait.C:
		return fail(errNoAnswer)
	case <-ctx.Done():
		return fail(ctx.Err())
	}
	f.set("copying", 0)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case err := <-pc.result:
			if err != nil {
				return fail(err)
			}
			return received, nil
		case <-tick.C:
			if time.Since(time.Unix(0, pc.activity.Load())) > m.PageQuiet {
				return fail(errPageQuiet)
			}
		case <-ctx.Done():
			return fail(ctx.Err())
		}
	}
}

// offer puts a copy on offer to the page, and withdraw takes it back.
func (f *following) offer(pc *pageCopy) {
	f.mu.Lock()
	f.copy = pc
	f.mu.Unlock()
}

func (f *following) withdraw(pc *pageCopy) {
	pc.stop()
	f.mu.Lock()
	if f.copy == pc {
		f.copy = nil
	}
	f.mu.Unlock()
}

// followWait is how long a page's socket waits for the upload it follows
// to reach this service: the page opens it as it starts the upload, which
// can wait behind the browser's other requests to the service.
const followWait = 30 * time.Second

// FollowUpload serves the socket of the page following an upload by its
// key: it says how far the upload has come, offers the page the video's
// copy to make when it said it makes copies, hands it the video's packets
// as it asks for them, and takes the copy's. It returns when the upload
// ends, the page goes, or ctx ends. One page follows an upload at a time.
func (m *Manager) FollowUpload(ctx context.Context, denID, key string, s PageSocket) error {
	f, err := m.followed(denID, key)
	for wait := time.Now().Add(followWait); err != nil && time.Now().Before(wait); {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		f, err = m.followed(denID, key)
	}
	if err != nil {
		return err
	}
	f.mu.Lock()
	if f.socket {
		f.mu.Unlock()
		return inputError(errors.New("another page follows this upload"))
	}
	f.socket = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.socket = false
		f.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type incoming struct {
		text bool
		data []byte
	}
	msgs := make(chan incoming, 8)
	go func() {
		defer close(msgs)
		for {
			text, data, err := s.Read(ctx)
			if err != nil {
				return
			}
			select {
			case msgs <- incoming{text, data}:
			case <-ctx.Done():
				return
			}
		}
	}()
	write := func(v pageMsg) error {
		data, _ := json.Marshal(v)
		return s.Write(ctx, true, data)
	}
	var shown Progress
	// offered is the copy the page was offered and hasn't finished, and
	// last the latest offered, which isn't offered again.
	var offered, last *pageCopy
	// gone ends an offer the page answered, or will never answer now.
	gone := func(err error) {
		if offered == nil {
			return
		}
		switch {
		case !offered.answered:
			offered.answer <- err
		case !offered.finished:
			offered.result <- err
		}
		offered.answered, offered.finished = true, true
		offered = nil
	}
	defer gone(errPageGone)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-f.closed:
			return nil
		case in, ok := <-msgs:
			if !ok {
				return nil
			}
			if offered != nil {
				offered.touch()
			}
			if !in.text {
				if offered == nil || !offered.answered || offered.finished {
					continue
				}
				if err := m.takeRecords(offered, in.data); err != nil {
					m.log.Infof("A page's copy of a video failed: %v", err)
					gone(err)
				}
				continue
			}
			var msg pageMsg
			if json.Unmarshal(in.data, &msg) != nil || offered == nil || msg.Offer != offered.offer.ID {
				continue
			}
			switch msg.T {
			case "accept":
				if !offered.answered {
					offered.answered = true
					offered.answer <- nil
				}
			case "decline", "fail":
				reason := msg.Reason
				if len(reason) > 200 {
					reason = reason[:200]
				}
				m.log.Infof("The page doesn't make a video's copy: %s", reason)
				gone(fmt.Errorf("the page didn't make the copy: %s", reason))
			case "more":
				if offered.answered && !offered.finished {
					if err := m.sendPackets(ctx, s, offered, msg.Bytes); err != nil {
						return err
					}
				}
			case "copied":
				if offered.answered && !offered.finished {
					offered.finished = true
					if msg.Packets != offered.gotPackets || offered.gotPackets == 0 {
						offered.result <- fmt.Errorf("the page said it sent %d packets of %d", msg.Packets, offered.gotPackets)
					} else {
						offered.result <- nil
					}
					offered = nil
				}
			}
		case <-f.changed:
			// The progress changed; it goes out below, at once.
			tick.Reset(time.Millisecond)
		case <-tick.C:
			tick.Reset(200 * time.Millisecond)
			f.mu.Lock()
			p, current := f.p, f.copy
			f.mu.Unlock()
			if p != shown {
				if err := write(pageMsg{T: "progress", Stage: p.Stage, Done: p.Done}); err != nil {
					return err
				}
				shown = p
			}
			if offered != nil && offered.stopped() {
				if err := write(pageMsg{T: "cancel", Offer: offered.offer.ID}); err != nil {
					return err
				}
				gone(errors.New("withdrawn"))
			}
			if current != nil && current != last && !current.stopped() && offered == nil {
				offer := current.offer
				if err := write(pageMsg{T: "offer", Copy: &offer}); err != nil {
					return err
				}
				offered, last = current, current
				offered.touch()
			}
		}
	}
}

// sendPackets sends the page the video's next packets, as far as budget
// bytes go, in messages of whole records, and the end of them, once, when
// they're all sent. A record larger than the budget goes whole.
func (m *Manager) sendPackets(ctx context.Context, s PageSocket, pc *pageCopy, budget int64) error {
	if pc.ended {
		return nil
	}
	size := pc.packets.Size()
	for budget > 0 && pc.sent < size {
		frame := make([]byte, 0, sendFrame)
		for pc.sent < size && budget > 0 {
			var h [recordHeader]byte
			if _, err := pc.packets.ReadAt(h[:], pc.sent); err != nil {
				return err
			}
			n := int64(binary.LittleEndian.Uint32(h[:])) + recordHeader
			if len(frame) > 0 && int64(len(frame))+n > sendFrame {
				break
			}
			at := len(frame)
			frame = append(frame, make([]byte, n)...)
			if _, err := pc.packets.ReadAt(frame[at:], pc.sent); err != nil {
				return err
			}
			pc.sent += n
			budget -= n
		}
		if err := s.Write(ctx, false, frame); err != nil {
			return err
		}
	}
	if pc.sent >= size {
		pc.ended = true
		data, _ := json.Marshal(pageMsg{T: "packets-end", Offer: pc.offer.ID})
		return s.Write(ctx, true, data)
	}
	return nil
}

// takeRecords checks a message of the copy's records from the page, and
// keeps them: whole, each a packet of a size a copy has, with flags it
// knows, its times going forward, and the copy no larger than a den takes.
// The module checks them again as it puts the copy together.
func (m *Manager) takeRecords(pc *pageCopy, data []byte) error {
	for off := 0; off < len(data); {
		if len(data)-off < recordHeader {
			return errors.New("a record cut short")
		}
		size := int64(binary.LittleEndian.Uint32(data[off:]))
		flags := binary.LittleEndian.Uint32(data[off+4:])
		pts := int64(binary.LittleEndian.Uint64(data[off+8:]))
		dur := int64(binary.LittleEndian.Uint64(data[off+16:]))
		if size == 0 || size > maxRecord || flags&^1 != 0 || pts <= pc.lastPTS || pts <= -maxTime || pts >= maxTime ||
			dur < 0 || dur >= maxTime || int64(len(data)-off-recordHeader) < size {
			return errors.New("a malformed record")
		}
		if pc.gotPackets == 0 && flags&1 == 0 {
			return errors.New("a copy that doesn't start with a keyframe")
		}
		pc.lastPTS = pts
		pc.gotPackets++
		off += recordHeader + int(size)
	}
	if pc.got+int64(len(data)) > denproto.MaxFileSize {
		return ErrTooLarge
	}
	if _, err := pc.received.WriteAt(data, pc.got); err != nil {
		return err
	}
	pc.got += int64(len(data))
	if start, end := pc.offer.Encoding.StartUS, pc.offer.EndUS; end > start {
		pc.follow.set("copying", float64(pc.lastPTS-start)/float64(end-start))
	}
	return nil
}
