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

// Every name KeysymName produces has to lead back to the character it came
// from, since the Wayland backend starts from the name and needs the character.
func TestKeysymForRoundTrips(t *testing.T) {
	for c := rune(0x20); c <= 0x7e; c++ {
		name := KeysymName(c)
		if name == "" {
			continue
		}
		if got := KeysymFor(name); got != c {
			t.Errorf("KeysymFor(%q) = %q, want %q", name, got, c)
		}
	}
}

// A name the keymap can hold but this table cannot represent gets nothing, so
// the backend leaves the numeric keysym and the text out rather than guessing.
func TestKeysymForUnnamed(t *testing.T) {
	for _, name := range []string{"", "Return", "F11", "KP_Add", "dead_acute", "eacute", "É"} {
		if got := KeysymFor(name); got != 0 {
			t.Errorf("KeysymFor(%q) = %q, want 0", name, got)
		}
	}
}

func TestModifierNames(t *testing.T) {
	cases := []struct {
		mask uint32
		want []string
	}{
		{0, []string{}},
		{1, []string{"shift"}},
		{4 | 8, []string{"control", "mod1"}},
		{1 | 2, []string{"shift", "lock"}},
		{64, []string{"mod4"}},
		{0xff, []string{"shift", "lock", "control", "mod1", "mod2", "mod3", "mod4", "mod5"}},
		// Anything above the eight real modifiers is not ours to name.
		{1 << 8, []string{}},
	}
	for _, tc := range cases {
		got := ModifierNames(tc.mask)
		if len(got) != len(tc.want) {
			t.Errorf("ModifierNames(%#x) = %v, want %v", tc.mask, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ModifierNames(%#x) = %v, want %v", tc.mask, got, tc.want)
				break
			}
		}
	}
	// An empty result must still encode as a list, not as a null.
	if ModifierNames(0) == nil {
		t.Error("ModifierNames must never return nil")
	}
}
