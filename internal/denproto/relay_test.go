package denproto

import (
	"bytes"
	"testing"
)

// A box opens on the member's other devices at the den it was sealed for,
// to its note, and on nothing else: not at another den, as another member's
// or another device's, under another seal, or cut short. Every box is one
// size, whatever its note.
func TestRelayBoxes(t *testing.T) {
	seal, other := NewSeal(), NewSeal()
	den, elsewhere := Random(IDSize), Random(IDSize)
	from, to, third := Random(IDSize), Random(IDSize), Random(IDSize)
	key, err := RelayKey(seal, den)
	if err != nil || len(key) != DMKeySize {
		t.Fatalf("a relay key: %x, %v", key, err)
	}
	if notes, _ := NotesKey(seal, den); bytes.Equal(notes, key) {
		t.Error("the relay key is the notes' key")
	}
	note := []byte(`{"kind":"who"}`)
	ad := RelayBoxAD(den, 7, from, to)
	box, err := SealRelayBox(key, ad, note)
	if err != nil {
		t.Fatal(err)
	}
	if len(box) != RelayBoxSize {
		t.Errorf("a box of %d bytes, not %d", len(box), RelayBoxSize)
	}
	if long, err := SealRelayBox(key, ad, bytes.Repeat([]byte{'x'}, MaxRelayNote)); err != nil || len(long) != RelayBoxSize {
		t.Errorf("the longest note sealed to %d bytes, %v", len(long), err)
	}
	if _, err := SealRelayBox(key, ad, make([]byte, MaxRelayNote+1)); err == nil {
		t.Error("a note too long for its box was sealed")
	}
	got, err := OpenRelayBox(key, ad, box)
	if err != nil || !bytes.Equal(got, note) {
		t.Fatalf("the box opened to %q, %v", got, err)
	}

	otherKey, _ := RelayKey(other, den)
	elsewhereKey, _ := RelayKey(seal, elsewhere)
	for what, open := range map[string]func() (Bytes, error){
		"at another den":         func() (Bytes, error) { return OpenRelayBox(elsewhereKey, RelayBoxAD(elsewhere, 7, from, to), box) },
		"as another member's":    func() (Bytes, error) { return OpenRelayBox(key, RelayBoxAD(den, 8, from, to), box) },
		"from another device":    func() (Bytes, error) { return OpenRelayBox(key, RelayBoxAD(den, 7, third, to), box) },
		"for another device":     func() (Bytes, error) { return OpenRelayBox(key, RelayBoxAD(den, 7, from, third), box) },
		"as for every device":    func() (Bytes, error) { return OpenRelayBox(key, RelayBoxAD(den, 7, from, nil), box) },
		"under another seal":     func() (Bytes, error) { return OpenRelayBox(otherKey, ad, box) },
		"cut short":              func() (Bytes, error) { return OpenRelayBox(key, ad, box[:RelayBoxSize-1]) },
		"with a byte changed":    func() (Bytes, error) { b := bytes.Clone(box); b[100] ^= 1; return OpenRelayBox(key, ad, b) },
		"with its nonce changed": func() (Bytes, error) { b := bytes.Clone(box); b[0] ^= 1; return OpenRelayBox(key, ad, b) },
	} {
		if got, err := open(); err == nil {
			t.Errorf("a box opened %s, to %q", what, got)
		}
	}

	if _, err := RelayKey(seal[:16], den); err == nil {
		t.Error("a short seal made a relay key")
	}
	if _, err := RelayKey(seal, den[:8]); err == nil {
		t.Error("a short den ID made a relay key")
	}
}

// The den checks a box's shape alone: its size, and a key ID or none.
func TestRelaySignalShape(t *testing.T) {
	box := Random(RelayBoxSize)
	for _, tc := range []struct {
		what string
		s    RelaySignal
		ok   bool
	}{
		{"a box for every device", RelaySignal{Box: box}, true},
		{"a box for one", RelaySignal{To: Random(IDSize), Box: box}, true},
		{"a box too large", RelaySignal{Box: Random(RelayBoxSize + 1)}, false},
		{"a box too small", RelaySignal{Box: Random(RelayBoxSize - 1)}, false},
		{"no box", RelaySignal{}, false},
		{"a short key ID", RelaySignal{To: Random(8), Box: box}, false},
	} {
		if err := CheckRelaySignal(tc.s); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.what, err)
		}
	}
	if err := CheckRelaySignalEvent(RelaySignalEvent{From: Random(IDSize), Box: box}); err != nil {
		t.Errorf("a passed box: %v", err)
	}
	if err := CheckRelaySignalEvent(RelaySignalEvent{Box: box}); err == nil {
		t.Error("a passed box from no device")
	}
}
