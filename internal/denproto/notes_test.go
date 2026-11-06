package denproto

import (
	"bytes"
	"testing"
)

// A member's notes key is the same on every device holding their seal,
// and another in each den, and under another seal: what one den keeps
// opens nowhere else, and starting over leaves the old notes shut.
func TestNotesKey(t *testing.T) {
	seal, other := NewSeal(), NewSeal()
	den, elsewhere := Random(IDSize), Random(IDSize)
	key, err := NotesKey(seal, den)
	if err != nil || len(key) != DMKeySize {
		t.Fatalf("a notes key: %x, %v", key, err)
	}
	again, err := NotesKey(append(Bytes{}, seal...), append(Bytes{}, den...))
	if err != nil || !bytes.Equal(again, key) {
		t.Errorf("the same seal at the same den made %x, then %x", key, again)
	}
	for what, k := range map[string]func() (Bytes, error){
		"another den":  func() (Bytes, error) { return NotesKey(seal, elsewhere) },
		"another seal": func() (Bytes, error) { return NotesKey(other, den) },
		"the seal's own key": func() (Bytes, error) {
			b, err := sealKey(seal, den)
			return b, err
		},
	} {
		if b, err := k(); err != nil || bytes.Equal(b, key) {
			t.Errorf("%s made the notes key %x, %v", what, b, err)
		}
	}
	if _, err := NotesKey(seal[:16], den); err == nil {
		t.Error("a short seal made a notes key")
	}
	if _, err := NotesKey(seal, den[:8]); err == nil {
		t.Error("a short den ID made a notes key")
	}

	// A note sealed under it opens under it alone, bound to its channel and
	// author, as a DM's message is.
	ad := MessageAD{DenID: den, Channel: 7, Author: 3, Nonce: Random(16), KeyID: 7, Revision: 1}
	sealed, err := SealDMMessage(key, ad, DMPayload{Text: "milk, eggs"})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := OpenDMMessage(key, ad, sealed); err != nil || p.Text != "milk, eggs" {
		t.Fatalf("the note opened as %+v, %v", p, err)
	}
	restarted, _ := NotesKey(other, den)
	moved := ad
	moved.Channel, moved.KeyID = 8, 8
	for what, open := range map[string]func() error{
		"after starting over": func() error { _, err := OpenDMMessage(restarted, ad, sealed); return err },
		"in another channel":  func() error { _, err := OpenDMMessage(key, moved, sealed); return err },
	} {
		if err := open(); err == nil {
			t.Errorf("a note opened %s", what)
		}
	}
}
