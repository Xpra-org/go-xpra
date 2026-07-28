//go:build linux

package x11

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

// The keysym name is what the server resolves a key by when no keymap has been
// uploaded, so it has to be the real X11 name. xgbutil's KeysymToStr shortens
// punctuation and keypad keys to the character they produce, which is why
// keysymName overrides those ranges.
func TestKeysymName(t *testing.T) {
	cases := map[xproto.Keysym]string{
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
	}
	for keysym, want := range cases {
		if got := keysymName(keysym); got != want {
			t.Errorf("keysymName(%#x) = %q, want %q", keysym, got, want)
		}
	}
}

// An unmapped keysym must produce no name, so the client can drop the event
// rather than send the server something meaningless.
func TestKeysymNameUnmapped(t *testing.T) {
	if got := keysymName(0); got != "" {
		t.Errorf("keysymName(0) = %q, want an empty string", got)
	}
}

func TestKeyText(t *testing.T) {
	cases := map[xproto.Keysym]string{
		'a':    "a",
		'{':    "{",
		' ':    " ",
		0xff0d: "", // Return prints nothing
		0xffaa: "", // keypad multiply is outside the printable range
	}
	for keysym, want := range cases {
		if got := keyText(keysym); got != want {
			t.Errorf("keyText(%#x) = %q, want %q", keysym, got, want)
		}
	}
}

func TestModifierNames(t *testing.T) {
	cases := []struct {
		state uint16
		want  []string
	}{
		{0, []string{}},
		{xproto.ModMaskShift, []string{"shift"}},
		{xproto.ModMaskControl | xproto.ModMask1, []string{"control", "mod1"}},
		{xproto.ModMaskShift | xproto.ModMaskLock, []string{"shift", "lock"}},
		{xproto.ModMask4, []string{"mod4"}},
	}
	for _, tc := range cases {
		got := modifierNames(tc.state)
		if len(got) != len(tc.want) {
			t.Errorf("modifierNames(%#x) = %v, want %v", tc.state, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("modifierNames(%#x) = %v, want %v", tc.state, got, tc.want)
				break
			}
		}
	}
	// An empty result must still encode as a list, not as a null.
	if modifierNames(0) == nil {
		t.Error("modifierNames must never return nil")
	}
}

func TestIsAlphaKeysym(t *testing.T) {
	for _, keysym := range []xproto.Keysym{'a', 'z', 'A', 'Z'} {
		if !isAlphaKeysym(keysym) {
			t.Errorf("isAlphaKeysym(%q) = false, want true", rune(keysym))
		}
	}
	for _, keysym := range []xproto.Keysym{'0', '9', '{', 0xff0d} {
		if isAlphaKeysym(keysym) {
			t.Errorf("isAlphaKeysym(%#x) = true, want false", keysym)
		}
	}
}
