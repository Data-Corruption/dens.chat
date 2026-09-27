package host

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestAllocLockedHoldsAndClearsData(t *testing.T) {
	buf, free, err := AllocLocked(HostKeySize)
	if err != nil {
		t.Fatalf("AllocLocked: %v", err)
	}
	if len(buf) != HostKeySize || cap(buf) != HostKeySize {
		t.Fatalf("len/cap = %d/%d, want %d", len(buf), cap(buf), HostKeySize)
	}
	for i := range buf {
		buf[i] = 0xAA
	}
	if !bytes.Equal(buf, bytes.Repeat([]byte{0xAA}, HostKeySize)) {
		t.Fatal("locked memory did not hold data")
	}
	free()
}

func TestDevKeyIsCreatedOnceAndReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datakey.dev")
	first, err := LoadOrCreateDevKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateDevKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != HostKeySize || !bytes.Equal(first, second) {
		t.Fatal("development key changed between loads")
	}
}

func TestIdentityLookupRoundTrip(t *testing.T) {
	me, err := CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	if me.ID == "" {
		t.Fatal("current user has no ID")
	}
	byID, err := LookupUserID(me.ID)
	if err != nil {
		t.Fatalf("LookupUserID(%s): %v", me.ID, err)
	}
	if !byID.Equal(me) {
		t.Fatalf("LookupUserID = %v, want %v", byID, me)
	}
	if (Identity{}).Equal(Identity{}) {
		t.Fatal("empty identities must never be equal")
	}
}
