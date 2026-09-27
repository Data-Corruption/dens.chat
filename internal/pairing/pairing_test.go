package pairing

import (
	"testing"
	"time"
)

func TestTokensRedeemOnceBeforeExpiry(t *testing.T) {
	s := New()
	now := time.Now()
	s.now = func() time.Time { return now }

	token, err := s.Issue()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Redeem(token) {
		t.Fatal("fresh token refused")
	}
	if s.Redeem(token) {
		t.Fatal("token redeemed twice")
	}

	late, _ := s.Issue()
	now = now.Add(TokenLifetime)
	if s.Redeem(late) {
		t.Fatal("expired token redeemed")
	}
	if s.Redeem("never issued") {
		t.Fatal("unknown token redeemed")
	}
}

func TestPendingTokensAreBounded(t *testing.T) {
	s := New()
	first, _ := s.Issue()
	for range maxPending {
		if _, err := s.Issue(); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.pending) != maxPending {
		t.Fatalf("%d pending tokens, want %d", len(s.pending), maxPending)
	}
	if s.Redeem(first) {
		t.Fatal("oldest token survived past the bound")
	}
}
