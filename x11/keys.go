//go:build linux

package x11

import (
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"

	"github.com/Xpra-org/go-xpra/ui"
)

// effectiveKeysym resolves a keycode to the keysym it produces under the
// current modifier state.
//
// X keyboard mappings list keysyms per keycode by level: level 0 unshifted,
// level 1 shifted. This implements the common cases only — no Mode_switch and
// no layout groups — which is enough for the layouts a basic client meets.
func effectiveKeysym(X *xgbutil.XUtil, keycode xproto.Keycode, state uint16) xproto.Keysym {
	unshifted := keybind.KeysymGet(X, keycode, 0)
	shifted := keybind.KeysymGet(X, keycode, 1)

	shift := state&xproto.ModMaskShift != 0
	// Caps lock only applies to alphabetic keys, and it inverts shift there.
	if state&xproto.ModMaskLock != 0 && isAlphaKeysym(unshifted) {
		shift = !shift
	}
	if shift && shifted != 0 {
		return shifted
	}
	return unshifted
}

func isAlphaKeysym(keysym xproto.Keysym) bool {
	return (keysym >= 'a' && keysym <= 'z') || (keysym >= 'A' && keysym <= 'Z')
}

// modifierNames converts an X modifier mask into the names xpra uses.
//
// These are the X11 modifier names as-is: the server maps mod1..mod5 onto
// whatever its own keymap has bound there.
func modifierNames(state uint16) []string {
	var mods []string
	for _, m := range []struct {
		mask uint16
		name string
	}{
		{xproto.ModMaskShift, "shift"},
		{xproto.ModMaskLock, "lock"},
		{xproto.ModMaskControl, "control"},
		{xproto.ModMask1, "mod1"},
		{xproto.ModMask2, "mod2"},
		{xproto.ModMask3, "mod3"},
		{xproto.ModMask4, "mod4"},
		{xproto.ModMask5, "mod5"},
	} {
		if state&m.mask != 0 {
			mods = append(mods, m.name)
		}
	}
	if mods == nil {
		return []string{}
	}
	return mods
}

// keyText returns the text the key produces, or "" for non-printing keys.
// The server uses it to decide whether caps lock should apply.
func keyText(keysym xproto.Keysym) string {
	if keysym >= 0x20 && keysym <= 0x7e {
		return string(rune(keysym))
	}
	return ""
}

// keysymName returns the X11 name of a keysym.
//
// xgbutil's KeysymToStr cannot be used directly: it deliberately shortens
// punctuation and keypad names to the character they produce ("braceleft" to
// "{", "KP_Add" to "+"), and that mapping is both lossy and not what xpra
// wants. So printable ASCII and the keypad are named here from the keysym
// value, and everything else — function keys, modifiers, arrows, Return —
// falls through to KeysymToStr, which spells those correctly.
func keysymName(keysym xproto.Keysym) string {
	if keysym <= 0x7e {
		// In this range the keysym value is the character code.
		if name := ui.KeysymName(rune(keysym)); name != "" {
			return name
		}
	}
	if name, ok := keypadKeysyms[keysym]; ok {
		return name
	}
	return keybind.KeysymToStr(keysym)
}

// keypadKeysyms names the keypad keys that KeysymToStr would shorten.
var keypadKeysyms = map[xproto.Keysym]string{
	0xffaa: "KP_Multiply", 0xffab: "KP_Add", 0xffad: "KP_Subtract",
	0xffae: "KP_Decimal", 0xffaf: "KP_Divide",
	0xffb0: "KP_0", 0xffb1: "KP_1", 0xffb2: "KP_2", 0xffb3: "KP_3",
	0xffb4: "KP_4", 0xffb5: "KP_5", 0xffb6: "KP_6", 0xffb7: "KP_7",
	0xffb8: "KP_8", 0xffb9: "KP_9",
}
