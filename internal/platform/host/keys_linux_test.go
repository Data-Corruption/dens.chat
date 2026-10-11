package host

import (
	"errors"
	"testing"
)

func TestKeyStateIsUnsupportedHere(t *testing.T) {
	if _, err := OpenKeyState(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("OpenKeyState = %v, want ErrUnsupported until a reader exists", err)
	}
}
