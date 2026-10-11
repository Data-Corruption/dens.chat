package host

import (
	"errors"
	"fmt"
)

// Key is a key the shortcut helper reads (M7).
type Key uint32

// KeyState reads whether keys are down, for the shortcut helper. Linux has
// no reader yet: the desktop's global shortcuts portal and X11 come with
// M7.2, and until then the page's own keys work while it has focus.
type KeyState struct{}

// OpenKeyState prepares to read keys in the desktop user's session.
func OpenKeyState() (*KeyState, error) {
	return nil, fmt.Errorf("this system offers the helper no way to read keys yet: %w", errors.ErrUnsupported)
}

// Close releases what OpenKeyState took.
func (*KeyState) Close() error { return nil }

// Note says what the system keeps from the reader, for the settings.
func (*KeyState) Note() string { return "" }

// Key resolves a key the page names; none can be read here yet.
func (*KeyState) Key(string) (Key, bool) { return 0, false }

// Down says whether k is held now.
func (*KeyState) Down(Key) bool { return false }

// Modifiers says which of Ctrl, Alt, Shift and Meta are held.
func (*KeyState) Modifiers() (ctrl, alt, shift, meta bool) { return false, false, false, false }
