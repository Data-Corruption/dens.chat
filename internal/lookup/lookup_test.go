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
