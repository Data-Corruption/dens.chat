package host

import "testing"

func TestKeyStateResolvesThePagesKeys(t *testing.T) {
	ks, err := OpenKeyState()
	if err != nil {
		t.Fatal(err)
	}
	defer ks.Close()
	for code, want := range map[string]Key{"F8": 0x77, "Mouse4": 0x05, "Mouse5": 0x06, "Space": 0x20, "ShiftLeft": 0xA0, "Numpad5": 0x65} {
		if got, ok := ks.Key(code); !ok || got != want {
			t.Errorf("Key(%q) = %#x, %v; want %#x", code, got, ok, want)
		}
	}
	// Letters, digits and punctuation take the layout's virtual key.
	for _, code := range []string{"KeyA", "KeyV", "Digit1", "Backquote", "Slash"} {
		if got, ok := ks.Key(code); !ok || got == 0 {
			t.Errorf("Key(%q) = %#x, %v; want the layout's key", code, got, ok)
		}
	}
	if _, ok := ks.Key("NoSuchKey"); ok {
		t.Error("an unknown code resolved")
	}
	// Reading state needs no hook and no focus; a key nobody has, held, can't be.
	if ks.Down(0x87) {
		t.Error("F24 reads as held")
	}
	ks.Modifiers()
}
