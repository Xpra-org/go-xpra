package keysym

import "testing"

// The keysym name is what the server resolves a key by when no keymap has been
// uploaded, so it has to be the real X11 name. xgbutil's KeysymToStr shortens
// punctuation and keypad keys to the character they produce, which is why Name
// overrides those ranges.
func TestName(t *testing.T) {
	cases := map[uint32]string{
		// Punctuation, where KeysymToStr would give "{", "-", "/" and so on.
		0x7b: "braceleft",
		0x7d: "braceright",
		0x2d: "minus",
		0x2f: "slash",
		0x2e: "period",
		0x20: "space",
		0x5f: "underscore",
		0x40: "at",
		0x7e: "asciitilde",
		0x5c: "backslash",
		// Letters and digits are named after themselves.
		'a': "a",
		'Z': "Z",
		'7': "7",
		// The keypad, where KeysymToStr would give "*" and "5".
		0xffaa: "KP_Multiply",
		0xffb5: "KP_5",
		0xffaf: "KP_Divide",
		// Named keys, which KeysymToStr already spells correctly.
		0xff0d: "Return",
		0xff08: "BackSpace",
		0xff1b: "Escape",
		0xffbe: "F1",
		0xff51: "Left",
		0xffe1: "Shift_L",
		0xff09: "Tab",
		// A layout beyond ASCII still names its keys, which is what lets a
		// non-Latin keyboard type through the Wayland backend.
		0x0e9: "eacute",
		0x0fc: "udiaeresis",
	}
	for keysym, want := range cases {
		if got := Name(keysym); got != want {
			t.Errorf("Name(%#x) = %q, want %q", keysym, got, want)
		}
	}
}

// An unmapped keysym must produce no name, so the client can drop the event
// rather than send the server something meaningless.
func TestNameUnmapped(t *testing.T) {
	for _, keysym := range []uint32{0, 0xdeadbeef} {
		if got := Name(keysym); got != "" {
			t.Errorf("Name(%#x) = %q, want an empty string", keysym, got)
		}
	}
}
