package host

import (
	"errors"
	"fmt"
	"os"
)

// Key is a key the shortcut helper reads (M7): an X11 keycode on Linux.
type Key uint32

// KeyState reads whether keys are down, for the shortcut helper, in an X11
// session: it asks the X server for the keyboard's state (QueryKeymap) as
// the helper reads it, and looks at the bound keys alone. Under Wayland no
// program may read the keyboard, so the desktop's portal (DesktopKeys)
// reads the keys there.
type KeyState struct {
	x *x11Conn
}

// OpenKeyState connects to the X11 session's display. It fails with
// errors.ErrUnsupported under Wayland, or without a display.
func OpenKeyState() (*KeyState, error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return nil, fmt.Errorf("under Wayland the desktop reads the keys: %w", errors.ErrUnsupported)
	}
	display := os.Getenv("DISPLAY")
	if display == "" {
		return nil, fmt.Errorf("%w: %w", errNoX11, errors.ErrUnsupported)
	}
	x, err := dialX11(display)
	if err != nil {
		return nil, fmt.Errorf("connect to the X server at %s: %w", display, err)
	}
	return &KeyState{x: x}, nil
}

// Close closes the connection to the X server.
func (s *KeyState) Close() error { return s.x.close() }

// Note says what the system keeps from the reader, for the settings.
func (*KeyState) Note() string { return "" }

// Key resolves a key the page names by its place on the keyboard
// (KeyboardEvent.code) to the keycode the X server gives it: Linux's input
// code, plus 8. A mouse's buttons aren't keys to the X server.
func (*KeyState) Key(code string) (Key, bool) {
	c, ok := inputCodes[code]
	if !ok {
		return 0, false
	}
	return Key(c + 8), true
}

// Down says whether k is held now.
func (s *KeyState) Down(k Key) bool {
	keys, err := s.x.keymap()
	return err == nil && k < 256 && keys[k/8]&(1<<(k%8)) != 0
}

// Modifiers says which of Ctrl, Alt, Shift and Super are held.
func (s *KeyState) Modifiers() (ctrl, alt, shift, meta bool) {
	keys, err := s.x.keymap()
	if err != nil {
		return false, false, false, false
	}
	down := func(codes ...uint16) bool {
		for _, c := range codes {
			k := c + 8
			if keys[k/8]&(1<<(k%8)) != 0 {
				return true
			}
		}
		return false
	}
	return down(29, 97), down(56, 100), down(42, 54), down(125, 126)
}

// inputCodes are Linux's input codes (linux/input-event-codes.h) of the
// keys the page names.
var inputCodes = map[string]uint16{
	"Escape": 1, "Digit1": 2, "Digit2": 3, "Digit3": 4, "Digit4": 5, "Digit5": 6, "Digit6": 7, "Digit7": 8,
	"Digit8": 9, "Digit9": 10, "Digit0": 11, "Minus": 12, "Equal": 13, "Backspace": 14, "Tab": 15,
	"KeyQ": 16, "KeyW": 17, "KeyE": 18, "KeyR": 19, "KeyT": 20, "KeyY": 21, "KeyU": 22, "KeyI": 23, "KeyO": 24,
	"KeyP": 25, "BracketLeft": 26, "BracketRight": 27, "Enter": 28, "ControlLeft": 29,
	"KeyA": 30, "KeyS": 31, "KeyD": 32, "KeyF": 33, "KeyG": 34, "KeyH": 35, "KeyJ": 36, "KeyK": 37, "KeyL": 38,
	"Semicolon": 39, "Quote": 40, "Backquote": 41, "ShiftLeft": 42, "Backslash": 43,
	"KeyZ": 44, "KeyX": 45, "KeyC": 46, "KeyV": 47, "KeyB": 48, "KeyN": 49, "KeyM": 50,
	"Comma": 51, "Period": 52, "Slash": 53, "ShiftRight": 54, "NumpadMultiply": 55, "AltLeft": 56, "Space": 57,
	"CapsLock": 58, "F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65, "F8": 66, "F9": 67,
	"F10": 68, "NumLock": 69, "ScrollLock": 70, "Numpad7": 71, "Numpad8": 72, "Numpad9": 73,
	"NumpadSubtract": 74, "Numpad4": 75, "Numpad5": 76, "Numpad6": 77, "NumpadAdd": 78,
	"Numpad1": 79, "Numpad2": 80, "Numpad3": 81, "Numpad0": 82, "NumpadDecimal": 83,
	"IntlBackslash": 86, "F11": 87, "F12": 88, "IntlRo": 89, "NumpadEnter": 96, "ControlRight": 97,
	"NumpadDivide": 98, "PrintScreen": 99, "AltRight": 100, "Home": 102, "ArrowUp": 103, "PageUp": 104,
	"ArrowLeft": 105, "ArrowRight": 106, "End": 107, "ArrowDown": 108, "PageDown": 109, "Insert": 110,
	"Delete": 111, "Pause": 119, "IntlYen": 124, "MetaLeft": 125, "MetaRight": 126, "ContextMenu": 127,
	"F13": 183, "F14": 184, "F15": 185, "F16": 186, "F17": 187, "F18": 188, "F19": 189, "F20": 190,
	"F21": 191, "F22": 192, "F23": 193, "F24": 194,
}
