//go:build linux

package x11

import (
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
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

// keyText returns the text the key produces, or "" for non-printing keys.
// The server uses it to decide whether caps lock should apply.
func keyText(keysym xproto.Keysym) string {
	if keysym >= 0x20 && keysym <= 0x7e {
		return string(rune(keysym))
	}
	return ""
}
