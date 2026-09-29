package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func sealStream(t *testing.T, v *Vault, plain, ad []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := v.SealStream(&out, ad)
	if err != nil {
		t.Fatal(err)
	}
	// Uneven writes, so chunks don't line up with them.
	for p := plain; len(p) > 0; {
		n := min(len(p), 7777)
		if _, err := w.Write(p[:n]); err != nil {
			t.Fatal(err)
		}
		p = p[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func openStream(v *Vault, sealed, ad []byte) ([]byte, error) {
	return io.ReadAll(v.OpenStream(bytes.NewReader(sealed), ad))
}

func TestStreamRoundTrips(t *testing.T) {
	v, _ := testVault(t)
	for _, size := range []int{0, 1, StreamChunk - 1, StreamChunk, StreamChunk + 1, 3*StreamChunk + 17, 2 * StreamChunk} {
		plain := make([]byte, size)
		_, _ = rand.Read(plain)
		sealed := sealStream(t, v, plain, []byte("file:1"))
		got, err := openStream(v, sealed, []byte("file:1"))
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("%d bytes: %v", size, err)
		}
		if bytes.Contains(sealed, plain[:min(size, 64)]) && size >= 16 {
			t.Errorf("%d bytes: plaintext shows in the sealed file", size)
		}
	}
}

func TestStreamRefusesTampering(t *testing.T) {
	v, _ := testVault(t)
	plain := make([]byte, 2*StreamChunk+100)
	_, _ = rand.Read(plain)
	sealed := sealStream(t, v, plain, []byte("file:1"))
	chunk := StreamChunk + 16
	flipped := append([]byte{}, sealed...)
	flipped[streamHeader+chunk+5] ^= 1
	// The first two chunks without the third: cut at a chunk boundary.
	cut := sealed[:streamHeader+2*chunk]
	swapped := append([]byte{}, sealed[:streamHeader]...)
	swapped = append(swapped, sealed[streamHeader+chunk:streamHeader+2*chunk]...)
	swapped = append(swapped, sealed[streamHeader:streamHeader+chunk]...)
	swapped = append(swapped, sealed[streamHeader+2*chunk:]...)
	for name, c := range map[string]struct {
		data []byte
		ad   string
	}{
		"other data":     {sealed, "file:2"},
		"a flipped bit":  {flipped, "file:1"},
		"cut at a chunk": {cut, "file:1"},
		"cut mid-chunk":  {sealed[:len(sealed)-10], "file:1"},
		"swapped chunks": {swapped, "file:1"},
		"extra data":     {append(append([]byte{}, sealed...), 0), "file:1"},
		"no header":      {sealed[:5], "file:1"},
		"empty":          {nil, "file:1"},
	} {
		if _, err := openStream(v, c.data, []byte(c.ad)); !errors.Is(err, ErrSealed) {
			t.Errorf("%s: %v, want ErrSealed", name, err)
		}
	}
	other, _ := testVault(t)
	if _, err := openStream(other, sealed, []byte("file:1")); !errors.Is(err, ErrSealed) {
		t.Errorf("another key: %v, want ErrSealed", err)
	}
}

func TestStreamUnclosedDoesNotOpen(t *testing.T) {
	v, _ := testVault(t)
	var out bytes.Buffer
	w, err := v.SealStream(&out, []byte("file:1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, 3*StreamChunk)); err != nil {
		t.Fatal(err)
	}
	if _, err := openStream(v, out.Bytes(), []byte("file:1")); !errors.Is(err, ErrSealed) {
		t.Errorf("%v, want ErrSealed", err)
	}
}
