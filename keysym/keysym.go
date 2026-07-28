// Package keysym names X11 keysyms the way the xpra protocol wants them.
//
// It exists because both Linux backends need the same answer from the same
// table. The X11 one gets a keysym straight from the X server; the Wayland one
// gets it out of the keymap the compositor hands over, which recent versions of
// libxkbcommon write as numbers rather than as names. Neither can send the
// server a key it cannot name: with no keymap uploaded, the server resolves
// every key through find_matching_keycode (xpra/x11/server/keyboard_config.py),
// which looks the name up in its own keymap.
//
// The table itself is xgbutil's, which is an X11 library — but the names are
// X11's whatever window system produced the key, so that is where they live.
package keysym

import (
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/keybind"

	"github.com/Xpra-org/go-xpra/ui"
)

// Name returns the X11 name of a keysym, and "" for one no table describes.
//
// xgbutil's KeysymToStr cannot be used on its own: it deliberately shortens
// punctuation and keypad names to the character they produce ("braceleft" to
// "{", "KP_Add" to "+"), and that mapping is both lossy and not what xpra
// wants. So printable ASCII and the keypad are named here from the keysym
// value, and everything else — function keys, modifiers, arrows, Return —
// falls through to KeysymToStr, which spells those correctly.
func Name(keysym uint32) string {
	if keysym <= 0x7e {
		// In this range the keysym value is the character code.
		if name := ui.KeysymName(rune(keysym)); name != "" {
			return name
		}
	}
	if name, ok := keypadKeysyms[keysym]; ok {
		return name
	}
	return keybind.KeysymToStr(xproto.Keysym(keysym))
}

// keypadKeysyms names the keypad keys that KeysymToStr would shorten.
var keypadKeysyms = map[uint32]string{
	0xffaa: "KP_Multiply", 0xffab: "KP_Add", 0xffad: "KP_Subtract",
	0xffae: "KP_Decimal", 0xffaf: "KP_Divide",
	0xffb0: "KP_0", 0xffb1: "KP_1", 0xffb2: "KP_2", 0xffb3: "KP_3",
	0xffb4: "KP_4", 0xffb5: "KP_5", 0xffb6: "KP_6", 0xffb7: "KP_7",
	0xffb8: "KP_8", 0xffb9: "KP_9",
}
