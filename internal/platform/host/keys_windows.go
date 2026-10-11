package host

import "golang.org/x/sys/windows"

var (
	moduser32            = windows.NewLazySystemDLL("user32.dll")
	procGetAsyncKeyState = moduser32.NewProc("GetAsyncKeyState")
	procMapVirtualKeyW   = moduser32.NewProc("MapVirtualKeyW")
)

// Key is a key the shortcut helper reads (M7): a virtual key on Windows.
type Key uint32

// KeyState reads whether keys are down, for the shortcut helper. On Windows
// it asks for each key's state as the helper reads it (GetAsyncKeyState),
// with no keyboard hook, so it learns nothing of keys it isn't asked about.
// While a program run as administrator has focus, Windows reports every key
// as up.
type KeyState struct{}

// OpenKeyState prepares to read keys in the desktop user's session.
func OpenKeyState() (*KeyState, error) { return &KeyState{}, nil }

// Close releases what OpenKeyState took.
func (*KeyState) Close() error { return nil }

// Note says what Windows keeps from the reader, for the settings to show.
func (*KeyState) Note() string {
	return "While a program run as administrator is in front, Windows keeps its keys from Dens."
}

// Key resolves a key the page names by its place on the keyboard
// (KeyboardEvent.code), or a mouse's side button (Mouse4, Mouse5), to the
// virtual key it is under the current keyboard layout.
func (*KeyState) Key(code string) (Key, bool) {
	if vk, ok := fixedVirtualKeys[code]; ok {
		return Key(vk), true
	}
	scan, ok := layoutScanCodes[code]
	if !ok {
		return 0, false
	}
	const mapvkVscToVkEx = 3
	vk, _, _ := procMapVirtualKeyW.Call(uintptr(scan), mapvkVscToVkEx)
	if vk == 0 {
		return 0, false
	}
	return Key(vk), true
}

// Down says whether k is held now.
func (*KeyState) Down(k Key) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(k))
	return r&0x8000 != 0
}

// Modifiers says which of Ctrl, Alt, Shift and the Windows key are held.
func (s *KeyState) Modifiers() (ctrl, alt, shift, meta bool) {
	return s.Down(0x11), s.Down(0x12), s.Down(0x10), s.Down(0x5B) || s.Down(0x5C)
}

// fixedVirtualKeys are the keys whose virtual key no layout changes.
var fixedVirtualKeys = map[string]uint16{
	"Mouse4": 0x05, "Mouse5": 0x06,
	"Backspace": 0x08, "Tab": 0x09, "Enter": 0x0D, "NumpadEnter": 0x0D,
	"Pause": 0x13, "CapsLock": 0x14, "Escape": 0x1B, "Space": 0x20,
	"PageUp": 0x21, "PageDown": 0x22, "End": 0x23, "Home": 0x24,
	"ArrowLeft": 0x25, "ArrowUp": 0x26, "ArrowRight": 0x27, "ArrowDown": 0x28,
	"PrintScreen": 0x2C, "Insert": 0x2D, "Delete": 0x2E,
	"MetaLeft": 0x5B, "MetaRight": 0x5C, "ContextMenu": 0x5D,
	"Numpad0": 0x60, "Numpad1": 0x61, "Numpad2": 0x62, "Numpad3": 0x63, "Numpad4": 0x64,
	"Numpad5": 0x65, "Numpad6": 0x66, "Numpad7": 0x67, "Numpad8": 0x68, "Numpad9": 0x69,
	"NumpadMultiply": 0x6A, "NumpadAdd": 0x6B, "NumpadSubtract": 0x6D, "NumpadDecimal": 0x6E, "NumpadDivide": 0x6F,
	"F1": 0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73, "F5": 0x74, "F6": 0x75,
	"F7": 0x76, "F8": 0x77, "F9": 0x78, "F10": 0x79, "F11": 0x7A, "F12": 0x7B,
	"F13": 0x7C, "F14": 0x7D, "F15": 0x7E, "F16": 0x7F, "F17": 0x80, "F18": 0x81,
	"F19": 0x82, "F20": 0x83, "F21": 0x84, "F22": 0x85, "F23": 0x86, "F24": 0x87,
	"NumLock": 0x90, "ScrollLock": 0x91,
	"ShiftLeft": 0xA0, "ShiftRight": 0xA1, "ControlLeft": 0xA2, "ControlRight": 0xA3, "AltLeft": 0xA4, "AltRight": 0xA5,
}

// layoutScanCodes are the keys a layout gives meaning to, by scan code,
// which MapVirtualKey turns into the layout's virtual key: the letters,
// digits and punctuation.
var layoutScanCodes = map[string]uint16{
	"Backquote": 0x29, "Digit1": 0x02, "Digit2": 0x03, "Digit3": 0x04, "Digit4": 0x05, "Digit5": 0x06,
	"Digit6": 0x07, "Digit7": 0x08, "Digit8": 0x09, "Digit9": 0x0A, "Digit0": 0x0B, "Minus": 0x0C, "Equal": 0x0D,
	"KeyQ": 0x10, "KeyW": 0x11, "KeyE": 0x12, "KeyR": 0x13, "KeyT": 0x14, "KeyY": 0x15, "KeyU": 0x16,
	"KeyI": 0x17, "KeyO": 0x18, "KeyP": 0x19, "BracketLeft": 0x1A, "BracketRight": 0x1B,
	"KeyA": 0x1E, "KeyS": 0x1F, "KeyD": 0x20, "KeyF": 0x21, "KeyG": 0x22, "KeyH": 0x23, "KeyJ": 0x24,
	"KeyK": 0x25, "KeyL": 0x26, "Semicolon": 0x27, "Quote": 0x28, "Backslash": 0x2B,
	"KeyZ": 0x2C, "KeyX": 0x2D, "KeyC": 0x2E, "KeyV": 0x2F, "KeyB": 0x30, "KeyN": 0x31, "KeyM": 0x32,
	"Comma": 0x33, "Period": 0x34, "Slash": 0x35,
	"IntlBackslash": 0x56, "IntlRo": 0x73, "IntlYen": 0x7D,
}
