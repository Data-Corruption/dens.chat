package vault

import (
	"bytes"
	"errors"
	"testing"
)

// The Flatpak's remembered copy opens with the keyring's secret and nothing
// else: another secret, as a reset keyring's or the random one KWallet
// answers a cancelled prompt with, fails rather than yielding a wrong key
// (M9.3).
func TestKeyringWrapOpensOnlyWithItsSecret(t *testing.T) {
	v, _ := testVault(t)
	secret := bytes.Repeat([]byte{7}, 64)
	wrap, err := v.WrapWithKeyring(secret)
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalKeyringWrap(wrap)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, v.key) {
		t.Fatal("the wrap holds the key in the clear")
	}
	decoded, err := UnmarshalKeyringWrap(data)
	if err != nil {
		t.Fatal(err)
	}
	key, err := UnwrapWithKeyring(decoded, secret)
	if err != nil || !v.Matches(key) {
		t.Fatalf("unwrap with its secret: %v", err)
	}
	other := bytes.Repeat([]byte{8}, 64)
	if _, err := UnwrapWithKeyring(decoded, other); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("unwrap with another secret = %v", err)
	}
	if _, err := v.WrapWithKeyring([]byte("short")); err == nil {
		t.Fatal("a short secret wrapped the key")
	}
	again, err := v.WrapWithKeyring(secret)
	if err != nil || bytes.Equal(again.Sealed, wrap.Sealed) || bytes.Equal(again.Salt, wrap.Salt) {
		t.Fatal("two wraps of one key under one secret are alike")
	}
}
