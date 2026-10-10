package denclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// The page's socket for a copy it makes (M5.5), without the module: an
// offer, the video's packets a window at a time, the copy's records back,
// and the offer declined or withdrawn. Quick enough for the race detector.

type testFrame struct {
	text bool
	data []byte
}

type chanSocket struct{ toPage, toService chan testFrame }

func (s *chanSocket) Read(ctx context.Context) (bool, []byte, error) {
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

func (s *chanSocket) Write(ctx context.Context, text bool, data []byte) error {
	select {
	case s.toPage <- testFrame{text, slices.Clone(data)}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// testRecords makes n records of size bytes each, a keyframe first, a
// thirtieth of a second apart.
func testRecords(n, size int) []byte {
	var out []byte
	for i := range n {
		h := make([]byte, recordHeader)
		binary.LittleEndian.PutUint32(h, uint32(size))
		if i == 0 {
			binary.LittleEndian.PutUint32(h[4:], 1)
		}
		binary.LittleEndian.PutUint64(h[8:], uint64(i*33333))
		binary.LittleEndian.PutUint64(h[16:], 33333)
		out = append(append(out, h...), bytes.Repeat([]byte{byte(i)}, size)...)
	}
	return out
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{log: log, follows: map[string]*following{}, TempDir: t.TempDir(), PageOfferWait: 2 * time.Second,
		PageQuiet: 2 * time.Second}
}

func TestPageSocket(t *testing.T) {
	m := testManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f, done, err := m.follow("den", "socket-test-1", true)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	sock := &chanSocket{toPage: make(chan testFrame, 16), toService: make(chan testFrame, 16)}
	served := make(chan error, 1)
	go func() { served <- m.FollowUpload(ctx, "den", "socket-test-1", sock) }()
	// A second page can't follow the same upload.
	time.Sleep(50 * time.Millisecond)
	if err := m.FollowUpload(ctx, "den", "socket-test-1", &chanSocket{}); err == nil {
		t.Error("a second page followed the upload")
	}

	packets, err := vault.NewScratch(t.TempDir(), "packets-*")
	if err != nil {
		t.Fatal(err)
	}
	defer packets.Close()
	video := testRecords(40, 10_000)
	if _, err := packets.WriteAt(video, 0); err != nil {
		t.Fatal(err)
	}
	copied := testRecords(20, 3_000)

	// The page: accept the offer, take the packets 32 KB at a time, and
	// send the copy in two messages.
	page := make(chan error, 1)
	go func() {
		page <- func() error {
			send := func(v pageMsg) {
				data, _ := json.Marshal(v)
				sock.toService <- testFrame{true, data}
			}
			var got []byte
			var stages []string
			for {
				var fr testFrame
				select {
				case fr = <-sock.toPage:
				case <-ctx.Done():
					return ctx.Err()
				}
				if !fr.text {
					got = append(got, fr.data...)
					send(pageMsg{T: "more", Offer: 1, Bytes: 32 << 10})
					continue
				}
				var msg pageMsg
				if err := json.Unmarshal(fr.data, &msg); err != nil {
					return err
				}
				switch msg.T {
				case "progress":
					stages = append(stages, msg.Stage)
				case "offer":
					if msg.Copy == nil || msg.Copy.ID != 1 || msg.Copy.Packets != 40 {
						return errors.New("a malformed offer")
					}
					send(pageMsg{T: "accept", Offer: 1})
					send(pageMsg{T: "more", Offer: 1, Bytes: 32 << 10})
				case "packets-end":
					if !bytes.Equal(got, video) {
						return errors.New("the packets came wrong")
					}
					sock.toService <- testFrame{false, copied[:len(copied)/2]}
					sock.toService <- testFrame{false, copied[len(copied)/2:]}
					send(pageMsg{T: "copied", Offer: 1, Packets: 20})
					if !slices.Contains(stages, "offered") {
						return errors.New("the page never heard it was offered")
					}
					return nil
				}
			}
		}()
	}()
	received, err := m.pageMakes(ctx, f, Offer{ID: 1, Packets: 40, EndUS: 40 * 33333}, packets)
	if err != nil {
		t.Fatal(err)
	}
	defer received.Close()
	if err := <-page; err != nil {
		t.Fatal(err)
	}
	back := make([]byte, received.Size())
	if _, err := received.ReadAt(back, 0); err != nil || !bytes.Equal(back, copied) {
		t.Fatalf("the copy came back wrong: %v", err)
	}

	// Declined.
	go func() {
		for fr := range sock.toPage {
			var msg pageMsg
			if fr.text && json.Unmarshal(fr.data, &msg) == nil && msg.T == "offer" {
				data, _ := json.Marshal(pageMsg{T: "decline", Offer: msg.Copy.ID, Reason: "no decoder"})
				sock.toService <- testFrame{true, data}
				return
			}
		}
	}()
	if _, err := m.pageMakes(ctx, f, Offer{ID: 2, Packets: 40}, packets); err == nil {
		t.Error("a declined offer was made")
	}

	// Withdrawn, as when the member sends the video full size instead: the
	// page hears it.
	accepted := make(chan struct{})
	heard := make(chan error, 1)
	go func() {
		heard <- func() error {
			for {
				var fr testFrame
				select {
				case fr = <-sock.toPage:
				case <-time.After(5 * time.Second):
					return errors.New("the page never heard the offer was withdrawn")
				}
				var msg pageMsg
				if !fr.text || json.Unmarshal(fr.data, &msg) != nil {
					continue
				}
				switch {
				case msg.T == "offer":
					data, _ := json.Marshal(pageMsg{T: "accept", Offer: msg.Copy.ID})
					sock.toService <- testFrame{true, data}
					close(accepted)
				case msg.T == "cancel" && msg.Offer == 3:
					return nil
				case msg.T == "cancel":
					return errors.New("another offer was withdrawn")
				}
			}
		}()
	}()
	withdrawn, withdraw := context.WithCancel(ctx)
	go func() {
		select {
		case <-accepted:
		case <-ctx.Done():
		}
		withdraw()
	}()
	if _, err := m.pageMakes(withdrawn, f, Offer{ID: 3, Packets: 40}, packets); !errors.Is(err, context.Canceled) {
		t.Errorf("a withdrawn offer: %v", err)
	}
	if err := <-heard; err != nil {
		t.Error(err)
	}

	// The upload's end ends the socket.
	done()
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the socket outlived the upload")
	}
}
