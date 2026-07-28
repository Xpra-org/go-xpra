//go:build linux

package x11

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

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
