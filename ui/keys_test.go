package ui

import "testing"

// The name is what the server resolves a key by, so punctuation has to come out
// as its X11 keysym name rather than as the character itself.
func TestKeysymName(t *testing.T) {
	cases := map[rune]string{
		'{':  "braceleft",
		'}':  "braceright",
		'-':  "minus",
		'/':  "slash",
		'.':  "period",
		' ':  "space",
		'_':  "underscore",
		'@':  "at",
		'~':  "asciitilde",
		'\\': "backslash",
		// Letters and digits are named after themselves.
		'a': "a",
		'Z': "Z",
		'7': "7",
	}
	for c, want := range cases {
		if got := KeysymName(c); got != want {
			t.Errorf("KeysymName(%q) = %q, want %q", c, got, want)
		}
	}
}

// Anything outside printable ASCII has no name here, and the caller has to
// decide what to do rather than be handed something meaningless.
func TestKeysymNameUnnamed(t *testing.T) {
	for _, c := range []rune{0, '\n', '\t', 0x7f, 'é', '€'} {
		if got := KeysymName(c); got != "" {
			t.Errorf("KeysymName(%q) = %q, want an empty string", c, got)
		}
	}
}
