package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func testVault(t *testing.T) (*Vault, []byte) {
	t.Helper()
	// Keep the Argon2id cost low so the suite stays fast; the real cost is
	// exercised by the stored parameters, not by these values.
	oldTime, oldMemory := argonTime, argonMemory
	argonTime, argonMemory = 1, 8*1024
	t.Cleanup(func() { argonTime, argonMemory = oldTime, oldMemory })

	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	v, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	return v, key
}

func TestSealOpenBindsAssociatedData(t *testing.T) {
	v, _ := testVault(t)
	sealed, err := v.Seal([]byte("secret"), []byte("messages.body:1"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := v.Open(sealed, []byte("messages.body:1"))
	if err != nil || string(plain) != "secret" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	if _, err := v.Open(sealed, []byte("messages.body:2")); !errors.Is(err, ErrSealed) {
		t.Fatalf("value moved to another row opened: %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := v.Open(sealed, []byte("messages.body:1")); !errors.Is(err, ErrSealed) {
		t.Fatalf("tampered value opened: %v", err)
	}
	if _, err := v.Open([]byte{1, 2, 3}, nil); !errors.Is(err, ErrSealed) {
		t.Fatalf("short value opened: %v", err)
	}
}

func TestSealUsesFreshNonces(t *testing.T) {
	v, _ := testVault(t)
	a, _ := v.Seal([]byte("same"), nil)
	b, _ := v.Seal([]byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext are identical")
	}
}

func TestPasswordWrapRoundTrip(t *testing.T) {
	v, key := testVault(t)
	wrap, err := v.WrapWithPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalWrap(wrap)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalWrap(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapWithPassword(decoded, "correct horse")
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatal("unwrapped key differs")
	}
	if err := v.VerifyPassword(decoded, "correct horse"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if _, err := UnwrapWithPassword(decoded, "wrong horse!"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestPasswordWrapRejectsOutOfRangeCosts(t *testing.T) {
	v, _ := testVault(t)
	wrap, err := v.WrapWithPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	wrap.MemoryKiB = 1 << 30
	if _, err := UnwrapWithPassword(wrap, "correct horse"); err == nil {
		t.Fatal("unbounded memory cost accepted")
	}
}

func TestPasswordLengthLimits(t *testing.T) {
	v, _ := testVault(t)
	if _, err := v.WrapWithPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := ValidatePassword("long enough"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckValueDetectsAnotherKey(t *testing.T) {
	v, _ := testVault(t)
	other, _ := testVault(t)
	if err := v.VerifyCheckValue(v.CheckValue()); err != nil {
		t.Fatal(err)
	}
	if err := v.VerifyCheckValue(other.CheckValue()); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("another key's check value accepted: %v", err)
	}
}
