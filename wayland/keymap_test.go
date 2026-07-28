//go:build linux

package wayland

import "testing"

// A keymap in the shape compositors send one: the short one-line form, the
// labelled multi-line form, a key whose actions list must not be mistaken for
// its symbols, and a level the key does not produce.
const testKeymap = `xkb_keymap {
xkb_keycodes "evdev+aliases(qwerty)" {
    minimum = 8;
    maximum = 255;
     <ESC> = 9;
    <AE01> = 10;
    <AD11> = 34;
    <CAPS> = 66;
    <LCTL> = 37;
    alias <MENU> = <COMPS>;
    indicator 1 = "Caps Lock";
};
xkb_types "complete" {
    virtual_modifiers NumLock,Alt,LevelThree;
    type "ONE_LEVEL" {
        modifiers= none;
        level_name[Level1]= "Any";
    };
};
xkb_symbols "pc+gb+inet(evdev)" {
    name[group1]="English (UK)";

    key  <ESC> {         [          Escape ] };
    key <AE01> {
        type= "FOUR_LEVEL",
        symbols[Group1]= [               1,          exclam,     onesuperior,      exclamdown ]
    };
    key <AD11> {
        type= "FOUR_LEVEL",
        symbols[Group1]= [     bracketleft,       braceleft,  dead_diaeresis,  dead_abovering ]
    };
    key <CAPS> {
        type= "ALPHABETIC",
        symbols[Group1]= [       Caps_Lock,         NoSymbol ],
        actions[Group1]= [ LockMods(modifiers=Lock),  NoAction() ]
    };
    modifier_map Lock { <CAPS> };
    modifier_map Control { <LCTL> };
};
};
`

func TestParseKeymapNamesKeys(t *testing.T) {
	k := parseKeymap(testKeymap)

	cases := []struct {
		name    string
		keycode uint32
		mods    uint32
		want    string
	}{
		// The one-line form, whose bracket group follows the brace directly.
		{"escape", 9 - keycodeOffset, 0, "Escape"},
		// The labelled form, whose symbols[Group1] brings a bracket group of
		// its own that must not be taken for the keysym list.
		{"digit", 10 - keycodeOffset, 0, "1"},
		{"digit shifted", 10 - keycodeOffset, modShift, "exclam"},
		// Punctuation has to keep its X11 name rather than become the
		// character, which is the whole reason for reading the keymap.
		{"bracket", 34 - keycodeOffset, 0, "bracketleft"},
		{"bracket shifted", 34 - keycodeOffset, modShift, "braceleft"},
		// A key whose second level is NoSymbol falls back to the first.
		{"caps shifted", 66 - keycodeOffset, modShift, "Caps_Lock"},
		// Caps lock inverts shift for letters only, and these are not letters.
		{"digit locked", 10 - keycodeOffset, modLock, "1"},
		// A keycode the keymap says nothing about has no name at all.
		{"unmapped", 200, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := k.name(tc.keycode, tc.mods); got != tc.want {
				t.Errorf("name(%d, %#x) = %q, want %q", tc.keycode, tc.mods, got, tc.want)
			}
		})
	}
}

// Caps lock is not a second shift: it applies to letters and inverts shift
// there, and leaves everything else alone.
func TestKeymapCapsLock(t *testing.T) {
	k := &keymap{levels: map[uint32][]string{
		1: {"a", "A"},
		2: {"1", "exclam"},
	}}
	cases := []struct {
		keycode uint32
		mods    uint32
		want    string
	}{
		{1, 0, "a"},
		{1, modShift, "A"},
		{1, modLock, "A"},
		{1, modLock | modShift, "a"},
		{2, modLock, "1"},
		{2, modLock | modShift, "exclam"},
	}
	for _, tc := range cases {
		if got := k.name(tc.keycode, tc.mods); got != tc.want {
			t.Errorf("name(%d, %#x) = %q, want %q", tc.keycode, tc.mods, got, tc.want)
		}
	}
}

// libxkbcommon 1.11 and later write the keysym values rather than their names,
// so the same keymap has to come out the same either way round.
func TestParseKeymapNumericKeysyms(t *testing.T) {
	const numeric = `xkb_keymap {
xkb_keycodes "(unnamed)" {
	 <ESC> = 9;
	<AE01> = 10;
	<AD11> = 34;
	<LFSH> = 50;
};
xkb_symbols "(unnamed)" {
	key  <ESC> {	[ 0xff1b ] };
	key <AE01> {	[ 0x31, 0x21 ] };
	key <AD11> {	[ 0x5b, 0x7b ] };
	key <LFSH> {	[ 0xffe1 ] };
};
};
`
	k := parseKeymap(numeric)
	cases := []struct {
		keycode uint32
		mods    uint32
		want    string
	}{
		{9 - keycodeOffset, 0, "Escape"},
		{10 - keycodeOffset, 0, "1"},
		{10 - keycodeOffset, modShift, "exclam"},
		{34 - keycodeOffset, 0, "bracketleft"},
		{34 - keycodeOffset, modShift, "braceleft"},
		{50 - keycodeOffset, 0, "Shift_L"},
	}
	for _, tc := range cases {
		if got := k.name(tc.keycode, tc.mods); got != tc.want {
			t.Errorf("name(%d, %#x) = %q, want %q", tc.keycode, tc.mods, got, tc.want)
		}
	}
}

// A keymap that makes no sense must leave an empty table rather than a wrong
// one: an unnamed key is dropped by the client, a misnamed one is not.
func TestParseKeymapRejectsRubbish(t *testing.T) {
	for _, text := range []string{"", "{}", "xkb_keymap { xkb_symbols {", "not a keymap at all"} {
		if k := parseKeymap(text); len(k.levels) != 0 {
			t.Errorf("parseKeymap(%q) found %d keys, want none", text, len(k.levels))
		}
	}
}

func TestKeyDetails(t *testing.T) {
	cases := []struct {
		name   string
		keysym int
		text   string
	}{
		{"braceleft", '{', "{"},
		{"a", 'a', "a"},
		{"1", '1', "1"},
		{"space", ' ', " "},
		// Outside printable ASCII the name is all the server gets, and it is
		// enough — the hints stay empty rather than being guessed at.
		{"Escape", 0, ""},
		{"Caps_Lock", 0, ""},
		{"dead_diaeresis", 0, ""},
		{"", 0, ""},
	}
	for _, tc := range cases {
		keysym, text := keyDetails(tc.name)
		if keysym != tc.keysym || text != tc.text {
			t.Errorf("keyDetails(%q) = %d, %q, want %d, %q", tc.name, keysym, text, tc.keysym, tc.text)
		}
	}
}
