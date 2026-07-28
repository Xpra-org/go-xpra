package ui

// KeysymName returns the X11 keysym name of a printable ASCII character, and ""
// for anything else.
//
// It is the half of naming a key that both backends share: X11 gets a keysym
// and has to undo the shortening its own name table applies to punctuation,
// while Win32 gets a character out of the keyboard layout and has nothing but
// this table to name it with. For this range the keysym value is the character
// code itself, so the caller has no separate lookup to do.
func KeysymName(c rune) string {
	if name, ok := punctuationKeysyms[c]; ok {
		return name
	}
	// Letters and digits are named after themselves.
	if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		return string(c)
	}
	return ""
}

// punctuationKeysyms names the printable ASCII keysyms that are not letters or
// digits. The keys are the character codes, which for this range are also the
// keysym values.
var punctuationKeysyms = map[rune]string{
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
