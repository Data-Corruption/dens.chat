package lookup

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTheCacheStaysWithinItsBudget(t *testing.T) {
	c := NewCache(errors.New("not found"), time.Now)
	for i := range maxEntries + 10 {
		c.mu.Lock()
		c.store(&entry{key: string(rune('a'+i%26)) + strings.Repeat("x", i), until: time.Now().Add(time.Hour), size: 64})
		c.mu.Unlock()
	}
	if c.order.Len() != maxEntries || len(c.entries) != maxEntries {
		t.Errorf("%d entries kept, want %d", c.order.Len(), maxEntries)
	}
	big := make([]byte, 256<<10)
	for i := range maxBytes/len(big) + 5 {
		c.mu.Lock()
		c.store(&entry{key: "picture " + strings.Repeat("y", i), value: big, until: time.Now().Add(time.Hour), size: len(big)})
		c.mu.Unlock()
	}
	if c.bytes > maxBytes {
		t.Errorf("%d bytes kept, over %d", c.bytes, maxBytes)
	}
}

func TestCacheWithLimitEvictsLeastRecentlyUsed(t *testing.T) {
	c := NewCacheWithLimit(errors.New("not found"), time.Now, 1024)
	if c.maxBytes != 1024 {
		t.Fatalf("maxBytes = %d, want 1024", c.maxBytes)
	}
	c.mu.Lock()
	c.store(&entry{key: "a", until: time.Now().Add(time.Hour), size: 400})
	c.store(&entry{key: "b", until: time.Now().Add(time.Hour), size: 400})
	// Touch a so b becomes least recently used.
	c.order.MoveToFront(c.entries["a"])
	c.store(&entry{key: "c", until: time.Now().Add(time.Hour), size: 400})
	_, hasA := c.entries["a"]
	_, hasB := c.entries["b"]
	_, hasC := c.entries["c"]
	bytes := c.bytes
	c.mu.Unlock()
	if !hasA || hasB || !hasC || bytes != 800 {
		t.Errorf("got a=%v b=%v c=%v bytes=%d; want a=true b=false c=true bytes=800", hasA, hasB, hasC, bytes)
	}

	def := NewCacheWithLimit(errors.New("not found"), time.Now, 0)
	if def.maxBytes != maxBytes {
		t.Errorf("non-positive limit gave %d, want %d", def.maxBytes, maxBytes)
	}
}

func TestCleanTextIsANamesText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Never ‮gonna​  give", "Never gonna give"},
		{"Two\nlines\tand tabs", "Two lines and tabs"},
		{"\x00\x01", ""},
		{"Ünïcødé ✓", "Ünïcødé ✓"},
	} {
		if got := Clean(tc.in, 50); got != tc.want {
			t.Errorf("Clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := Clean(strings.Repeat("ab ", 50), 10); got != "ab ab ab…" {
		t.Errorf("cut to %q", got)
	}
}
