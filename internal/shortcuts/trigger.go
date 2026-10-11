package shortcuts

import (
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/types"
)

// desktopTrigger turns a key the page names (KeyboardEvent.code, with its
// modifiers) into the trigger the helper asks a desktop for, in the XDG
// shortcuts format, which names keys by their XKB symbols as a US layout
// has them, or "" for one it can't name, as a mouse's button. The desktop
// shows it to the member, who confirms or changes it.
func desktopTrigger(k types.VoiceKey) string {
	sym, ok := keySymbols[k.Code]
	if !ok {
		switch {
		case len(k.Code) == 4 && strings.HasPrefix(k.Code, "Key"):
			sym = strings.ToLower(k.Code[3:])
		case len(k.Code) == 6 && strings.HasPrefix(k.Code, "Digit"):
			sym = k.Code[5:]
		case isFunctionKey(k.Code):
			sym = k.Code
		default:
			return ""
		}
	}
	var parts []string
	for _, m := range []struct {
		on   bool
		name string
	}{{k.Ctrl, "CTRL"}, {k.Alt, "ALT"}, {k.Shift, "SHIFT"}, {k.Meta, "LOGO"}} {
		if m.on {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, sym), "+")
}

func isFunctionKey(code string) bool {
	if len(code) < 2 || len(code) > 3 || code[0] != 'F' {
		return false
	}
	for _, c := range code[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// keySymbols are the XKB symbols of the keys whose code doesn't spell them.
var keySymbols = map[string]string{
	"Backquote": "grave", "Minus": "minus", "Equal": "equal", "BracketLeft": "bracketleft", "BracketRight": "bracketright",
	"Backslash": "backslash", "Semicolon": "semicolon", "Quote": "apostrophe", "Comma": "comma", "Period": "period", "Slash": "slash",
	"Space": "space", "Tab": "Tab", "CapsLock": "Caps_Lock", "Enter": "Return", "Escape": "Escape", "Backspace": "BackSpace",
	"Insert": "Insert", "Delete": "Delete", "Home": "Home", "End": "End", "PageUp": "Page_Up", "PageDown": "Page_Down",
	"ArrowLeft": "Left", "ArrowUp": "Up", "ArrowRight": "Right", "ArrowDown": "Down",
	"Pause": "Pause", "ScrollLock": "Scroll_Lock", "PrintScreen": "Print", "ContextMenu": "Menu", "NumLock": "Num_Lock",
	"Numpad0": "KP_0", "Numpad1": "KP_1", "Numpad2": "KP_2", "Numpad3": "KP_3", "Numpad4": "KP_4",
	"Numpad5": "KP_5", "Numpad6": "KP_6", "Numpad7": "KP_7", "Numpad8": "KP_8", "Numpad9": "KP_9",
	"NumpadAdd": "KP_Add", "NumpadSubtract": "KP_Subtract", "NumpadMultiply": "KP_Multiply", "NumpadDivide": "KP_Divide",
	"NumpadDecimal": "KP_Decimal", "NumpadEnter": "KP_Enter", "IntlBackslash": "less",
}
