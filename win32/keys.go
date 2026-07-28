//go:build windows

package win32

import (
	"unicode/utf16"

	"github.com/Xpra-org/go-xpra/ui"
)

// The virtual key codes this backend reasons about by name.
const (
	vkBack     = 0x08
	vkTab      = 0x09
	vkClear    = 0x0C
	vkReturn   = 0x0D
	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12 // the Alt keys
	vkPause    = 0x13
	vkCapital  = 0x14
	vkEscape   = 0x1B
	vkPrior    = 0x21
	vkNext     = 0x22
	vkEnd      = 0x23
	vkHome     = 0x24
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkSnapshot = 0x2C
	vkInsert   = 0x2D
	vkDelete   = 0x2E
	vkLWin     = 0x5B
	vkRWin     = 0x5C
	vkApps     = 0x5D
	vkNumLock  = 0x90
	vkScroll   = 0x91
	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4
	vkRMenu    = 0xA5
)

// key translates a keyboard message into the naming the xpra server resolves
// keys by.
//
// It runs on the window thread, which is what makes the keyboard state calls
// below meaningful: they answer for the thread whose queue the message came
// from, and that is this one.
func (d *Display) key(w *Window, wparam, lparam uintptr, pressed bool) ui.Key {
	vk := byte(wparam)
	scan := byte(lparam >> 16)
	// Bit 24 marks the extended keys: the navigation cluster, the right-hand
	// modifiers, keypad enter and keypad divide.
	extended := lparam&(1<<24) != 0

	event := ui.Key{
		Window:  w.ID(),
		Pressed: pressed,
		// No keycode: a Windows scan code means nothing to an X server, and
		// sending one would only invite the server to trust it over the name.
		Modifiers: modifierNames(),
	}
	if name, keysym, ok := namedKey(side(vk, scan, extended), extended); ok {
		event.Name, event.Keysym = name, keysym
		return event
	}

	// Everything else is a key that produces text, and only the active keyboard
	// layout knows what.
	c := character(vk, scan)
	if c == 0 {
		return event
	}
	event.Text = string(c)
	// For printable ASCII the keysym value is the character code itself; beyond
	// that this backend has no name to offer, and the client drops the key
	// rather than send the server one it cannot resolve.
	if name := ui.KeysymName(c); name != "" {
		event.Name, event.Keysym = name, int(c)
	}
	return event
}

// side resolves the modifier keys that Windows reports under one virtual key
// code for both ends of the keyboard, which X11 names apart.
func side(vk, scan byte, extended bool) byte {
	switch vk {
	case vkShift:
		// The two shifts differ in nothing but their scan code.
		if resolved := mapVirtualKey(uint32(scan), mapvkVSCToVKEx); resolved != 0 {
			return byte(resolved)
		}
	case vkControl:
		if extended {
			return vkRControl
		}
		return vkLControl
	case vkMenu:
		if extended {
			return vkRMenu
		}
		return vkLMenu
	}
	return vk
}

// namedKey returns the X11 name and keysym of a key that produces no text.
func namedKey(vk byte, extended bool) (string, int, bool) {
	// Keypad enter is the odd one out: it is the extended twin of the ordinary
	// return key rather than of a navigation key.
	if vk == vkReturn && extended {
		return "KP_Enter", 0xff8d, true
	}
	// With num lock off the keypad reports the navigation cluster's virtual
	// keys, and the extended bit — which only the real navigation keys carry —
	// is the one thing that tells them apart.
	if !extended {
		if key, ok := keypadKeys[vk]; ok {
			return key.name, key.keysym, true
		}
	}
	if key, ok := namedKeys[vk]; ok {
		return key.name, key.keysym, true
	}
	return "", 0, false
}

// character asks the keyboard layout what a key produces, with the modifiers
// that would hide the answer taken out of the state.
func character(vk, scan byte) rune {
	var state [256]byte
	if !getKeyboardState(&state) {
		return 0
	}
	// Control would turn the key into a control character and Alt into nothing
	// at all. The server wants the key's own character, and applies the
	// modifiers itself; shift and caps lock stay, since those choose which
	// character the key produces.
	for _, modifier := range []byte{vkControl, vkLControl, vkRControl, vkMenu, vkLMenu, vkRMenu} {
		state[modifier] = 0
	}

	var buf [8]uint16
	n := toUnicode(uint32(vk), uint32(scan), &state, buf[:], toUnicodeNoState)
	if n <= 0 {
		// Zero is a key with no character, and a negative result is a dead key
		// — an accent waiting for the letter it belongs on, which this client
		// has no way to describe to the server.
		return 0
	}
	runes := utf16.Decode(buf[:n])
	if len(runes) != 1 {
		return 0
	}
	return runes[0]
}

// modifierNames reports the modifiers currently held, under the X11 names xpra
// expects. The numbered ones follow the usual X11 keymap, where mod1 is alt,
// mod2 num lock and mod4 the super (Windows) key.
func modifierNames() []string {
	modifiers := []string{}
	if held(vkShift) {
		modifiers = append(modifiers, "shift")
	}
	if toggled(vkCapital) {
		modifiers = append(modifiers, "lock")
	}
	if held(vkControl) {
		modifiers = append(modifiers, "control")
	}
	if held(vkMenu) {
		modifiers = append(modifiers, "mod1")
	}
	if toggled(vkNumLock) {
		modifiers = append(modifiers, "mod2")
	}
	if held(vkLWin) || held(vkRWin) {
		modifiers = append(modifiers, "mod4")
	}
	return modifiers
}

// held reports whether a key is down, which Windows marks with the high bit of
// the state — that is, by making it negative.
func held(vk byte) bool { return getKeyState(vk) < 0 }

// toggled reports whether a locking key is on, which is the low bit.
func toggled(vk byte) bool { return getKeyState(vk)&1 != 0 }

type namedKeysym struct {
	name   string
	keysym int
}

// namedKeys names the keys that produce no text, and so have to be recognised
// by their virtual key code. The keysyms are the X11 values, which the server
// takes as a hint when the name alone leaves it a choice.
var namedKeys = map[byte]namedKeysym{
	vkBack:     {"BackSpace", 0xff08},
	vkTab:      {"Tab", 0xff09},
	vkReturn:   {"Return", 0xff0d},
	vkPause:    {"Pause", 0xff13},
	vkCapital:  {"Caps_Lock", 0xffe5},
	vkEscape:   {"Escape", 0xff1b},
	vkPrior:    {"Page_Up", 0xff55},
	vkNext:     {"Page_Down", 0xff56},
	vkEnd:      {"End", 0xff57},
	vkHome:     {"Home", 0xff50},
	vkLeft:     {"Left", 0xff51},
	vkUp:       {"Up", 0xff52},
	vkRight:    {"Right", 0xff53},
	vkDown:     {"Down", 0xff54},
	vkSnapshot: {"Print", 0xff61},
	vkInsert:   {"Insert", 0xff63},
	vkDelete:   {"Delete", 0xffff},
	vkClear:    {"Clear", 0xff0b},
	vkLWin:     {"Super_L", 0xffeb},
	vkRWin:     {"Super_R", 0xffec},
	vkApps:     {"Menu", 0xff67},
	vkNumLock:  {"Num_Lock", 0xff7f},
	vkScroll:   {"Scroll_Lock", 0xff14},
	vkLShift:   {"Shift_L", 0xffe1},
	vkRShift:   {"Shift_R", 0xffe2},
	vkLControl: {"Control_L", 0xffe3},
	vkRControl: {"Control_R", 0xffe4},
	vkLMenu:    {"Alt_L", 0xffe9},
	vkRMenu:    {"Alt_R", 0xffea},

	// The keypad, which reports its own virtual keys while num lock is on. Its
	// digits are named apart from the ones on the top row so that applications
	// can tell them apart, so they cannot go through the character path.
	0x60: {"KP_0", 0xffb0},
	0x61: {"KP_1", 0xffb1},
	0x62: {"KP_2", 0xffb2},
	0x63: {"KP_3", 0xffb3},
	0x64: {"KP_4", 0xffb4},
	0x65: {"KP_5", 0xffb5},
	0x66: {"KP_6", 0xffb6},
	0x67: {"KP_7", 0xffb7},
	0x68: {"KP_8", 0xffb8},
	0x69: {"KP_9", 0xffb9},
	0x6A: {"KP_Multiply", 0xffaa},
	0x6B: {"KP_Add", 0xffab},
	0x6D: {"KP_Subtract", 0xffad},
	0x6E: {"KP_Decimal", 0xffae},
	0x6F: {"KP_Divide", 0xffaf},

	// The function keys, F1 to F24 running consecutively from both ends.
	0x70: {"F1", 0xffbe}, 0x71: {"F2", 0xffbf}, 0x72: {"F3", 0xffc0},
	0x73: {"F4", 0xffc1}, 0x74: {"F5", 0xffc2}, 0x75: {"F6", 0xffc3},
	0x76: {"F7", 0xffc4}, 0x77: {"F8", 0xffc5}, 0x78: {"F9", 0xffc6},
	0x79: {"F10", 0xffc7}, 0x7A: {"F11", 0xffc8}, 0x7B: {"F12", 0xffc9},
}

// keypadKeys names the keypad keys as they report themselves while num lock is
// off, when they share the navigation cluster's virtual key codes.
var keypadKeys = map[byte]namedKeysym{
	vkHome:   {"KP_Home", 0xff95},
	vkLeft:   {"KP_Left", 0xff96},
	vkUp:     {"KP_Up", 0xff97},
	vkRight:  {"KP_Right", 0xff98},
	vkDown:   {"KP_Down", 0xff99},
	vkPrior:  {"KP_Page_Up", 0xff9a},
	vkNext:   {"KP_Page_Down", 0xff9b},
	vkEnd:    {"KP_End", 0xff9c},
	vkClear:  {"KP_Begin", 0xff9d}, // the unlabelled 5 in the middle
	vkInsert: {"KP_Insert", 0xff9e},
	vkDelete: {"KP_Delete", 0xff9f},
}
