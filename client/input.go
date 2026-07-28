package client

import (
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"

	"github.com/Xpra-org/go-xpra/rencodeplus"
)

// deviceID identifies the input device a pointer event came from. A negative
// value tells the server not to track per-device event sequence numbers
// (xpra/server/subsystem/pointer.py:590), which is what we want since we do not
// number our events.
const deviceID = -1

// handleMotion forwards pointer movement.
//
// Coordinates are absolute (root-relative), matching the geometry we reported
// in map-window, so the server can place the pointer in its own screen space.
func (c *Client) handleMotion(e xproto.MotionNotifyEvent) {
	wid, _, ok := c.lookup(e.Event)
	if !ok {
		return
	}
	c.send("pointer", deviceID, 0, wid,
		[]any{int(e.RootX), int(e.RootY)}, rencodeplus.Dict{})
}

// handleButton forwards a button press or release.
//
// Buttons 4 to 7 are the scroll wheel in X11 and need no special handling: xpra
// forwards them as buttons and the remote X server interprets them the same way.
func (c *Client) handleButton(xid xproto.Window, button xproto.Button, pressed bool, rootX, rootY int16) {
	wid, _, ok := c.lookup(xid)
	if !ok {
		return
	}
	c.send("pointer-button", deviceID, 0, wid, int(button), pressed,
		[]any{int(rootX), int(rootY)}, rencodeplus.Dict{})
}

// handleKey forwards a key press or release.
//
// We upload no keymap, so the server resolves the key by name through
// find_matching_keycode (xpra/x11/server/keyboard_config.py:551). That makes
// the keysym name the load-bearing field: it has to be a real X11 keysym name
// such as "braceleft", not the character it produces. The raw keycode and
// keysym value are sent alongside as additional hints.
func (c *Client) handleKey(xid xproto.Window, keycode xproto.Keycode, state uint16, pressed bool) {
	wid, _, ok := c.lookup(xid)
	if !ok {
		return
	}
	keysym := effectiveKeysym(c.display.X, keycode, state)
	name := keysymName(keysym)
	if name == "" {
		c.debugf("ignoring key with unmapped keysym %#x (keycode %d)", keysym, keycode)
		return
	}
	// group 0: we do not track keyboard layout groups.
	c.send("key-action", wid, name, pressed, modifierNames(state),
		int(keysym), keyString(keysym), int(keycode), 0)
}

// handleFocusIn tells the server which window should receive keyboard input.
func (c *Client) handleFocusIn(e xproto.FocusInEvent) {
	wid, _, ok := c.lookup(e.Event)
	if !ok {
		return
	}
	if c.focused == wid {
		return
	}
	c.focused = wid
	c.send("focus", wid, []string{})
}

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

// keyString returns the text the key produces, or "" for non-printing keys.
// The server uses it to decide whether caps lock should apply.
func keyString(keysym xproto.Keysym) string {
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
// wants. So punctuation and the keypad are named here from the keysym value,
// and everything else — function keys, modifiers, arrows, Return — falls
// through to KeysymToStr, which spells those correctly.
func keysymName(keysym xproto.Keysym) string {
	if name, ok := punctuationKeysyms[keysym]; ok {
		return name
	}
	if name, ok := keypadKeysyms[keysym]; ok {
		return name
	}
	// Letters and digits are named after themselves.
	if (keysym >= '0' && keysym <= '9') ||
		(keysym >= 'A' && keysym <= 'Z') ||
		(keysym >= 'a' && keysym <= 'z') {
		return string(rune(keysym))
	}
	return keybind.KeysymToStr(keysym)
}

// punctuationKeysyms names the printable ASCII keysyms that are not letters or
// digits. The values are the keysym codes, which for this range are the ASCII
// codes themselves.
var punctuationKeysyms = map[xproto.Keysym]string{
	0x20: "space", 0x21: "exclam", 0x22: "quotedbl", 0x23: "numbersign",
	0x24: "dollar", 0x25: "percent", 0x26: "ampersand", 0x27: "apostrophe",
	0x28: "parenleft", 0x29: "parenright", 0x2a: "asterisk", 0x2b: "plus",
	0x2c: "comma", 0x2d: "minus", 0x2e: "period", 0x2f: "slash",
	0x3a: "colon", 0x3b: "semicolon", 0x3c: "less", 0x3d: "equal",
	0x3e: "greater", 0x3f: "question", 0x40: "at",
	0x5b: "bracketleft", 0x5c: "backslash", 0x5d: "bracketright",
	0x5e: "asciicircum", 0x5f: "underscore", 0x60: "grave",
	0x7b: "braceleft", 0x7c: "bar", 0x7d: "braceright", 0x7e: "asciitilde",
}

// keypadKeysyms names the keypad keys that KeysymToStr would shorten.
var keypadKeysyms = map[xproto.Keysym]string{
	0xffaa: "KP_Multiply", 0xffab: "KP_Add", 0xffad: "KP_Subtract",
	0xffae: "KP_Decimal", 0xffaf: "KP_Divide",
	0xffb0: "KP_0", 0xffb1: "KP_1", 0xffb2: "KP_2", 0xffb3: "KP_3",
	0xffb4: "KP_4", 0xffb5: "KP_5", 0xffb6: "KP_6", 0xffb7: "KP_7",
	0xffb8: "KP_8", 0xffb9: "KP_9",
}
