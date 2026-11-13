package denclient_test

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// boxSink stands in for Relay: the notes each box held, opened, by the
// device each came from.
type boxSink struct {
	mu    sync.Mutex
	notes []string
	froms []denproto.Bytes
}

func (b *boxSink) Box(_ string, from denproto.Bytes, note []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.notes = append(b.notes, string(note))
	b.froms = append(b.froms, from)
}

func (b *boxSink) Changed() {}

// got waits for a note, and returns the device it came from.
func (b *boxSink) got(t *testing.T, note string) denproto.Bytes {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.mu.Lock()
		i := slices.Index(b.notes, note)
		var from denproto.Bytes
		if i >= 0 {
			from = b.froms[i]
		}
		b.mu.Unlock()
		if i >= 0 {
			return from
		}
		if time.Now().After(deadline) {
			t.Fatalf("the note %q never came", note)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (b *boxSink) all() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.notes)
}

// Notes go between a member's devices sealed with their seal, through a
// den that passes boxes: to every other device, or to one by its key ID;
// never back to the device that sent them, nor to another member.
func TestRelayNotesBetweenAMembersDevices(t *testing.T) {
	desk, laptopSink, carolSink := &boxSink{}, &boxSink{}, &boxSink{}
	h, owner, member, denID, _ := chatDen(t, func(m *denclient.Manager) { m.Relay = desk })
	invite, _, err := owner.CreateInvite(context.Background(), denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	join(t, h.client(t, noOwnDen, func(m *denclient.Manager) { m.Relay = carolSink }), invite, "carol")
	laptop := h.client(t, noOwnDen, func(m *denclient.Manager) { m.Relay = laptopSink })
	signInApproved(t, laptop, member, denID, denclient.SignInRequest{Den: "https://example.com", Username: "bob", Password: "correct horse"}, false)
	for _, m := range []*denclient.Manager{member, laptop} {
		deadline := time.Now().Add(10 * time.Second)
		for !slices.Contains(m.RelayDens(), denID) {
			if time.Now().After(deadline) {
				t.Fatal("a device of bob's never listed the den as passing boxes")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	if err := member.SendRelay(denID, nil, []byte("from the desk")); err != nil {
		t.Fatal(err)
	}
	deskKey := laptopSink.got(t, "from the desk")
	if err := laptop.SendRelay(denID, nil, []byte("from the laptop")); err != nil {
		t.Fatal(err)
	}
	laptopKey := desk.got(t, "from the laptop")
	if len(deskKey) != denproto.IDSize || bytes.Equal(deskKey, laptopKey) {
		t.Fatalf("the boxes came from %x and %x", deskKey, laptopKey)
	}
	if err := member.SendRelay(denID, laptopKey, []byte("for the laptop alone")); err != nil {
		t.Fatal(err)
	}
	if from := laptopSink.got(t, "for the laptop alone"); !bytes.Equal(from, deskKey) {
		t.Errorf("the laptop's own box came from %x, not the desk's %x", from, deskKey)
	}
	if err := laptop.SendRelay(denID, nil, []byte("last")); err != nil {
		t.Fatal(err)
	}
	desk.got(t, "last")
	if got := desk.all(); slices.Contains(got, "from the desk") || slices.Contains(got, "for the laptop alone") {
		t.Errorf("the desk got its own boxes back: %q", got)
	}
	if got := carolSink.all(); len(got) != 0 {
		t.Errorf("another member got bob's boxes: %q", got)
	}
	if err := member.SendRelay("not a den", nil, []byte("x")); err == nil {
		t.Error("a box for a den bob never joined went")
	}
}
