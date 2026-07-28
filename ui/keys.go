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

// modifierNames are the eight X11 modifier names in mask-bit order, which is
// the vocabulary xpra speaks: the server maps mod1..mod5 onto whatever its own
// keymap has bound there.
var modifierNames = [8]string{
	"shift", "lock", "control", "mod1", "mod2", "mod3", "mod4", "mod5",
}

// ModifierNames converts an X11 modifier mask into the names xpra uses.
//
// Both Linux backends need it, and both get the same mask: X11 in the state
// field of a key event, and Wayland in wl_keyboard.modifiers, whose bits are
// the indices of the compiled XKB keymap's real modifiers — which are these,
// in this order.
func ModifierNames(mask uint32) []string {
	mods := []string{}
	for bit, name := range modifierNames {
		if mask&(1<<uint(bit)) != 0 {
			mods = append(mods, name)
		}
	}
	return mods
}

// KeysymFor is the inverse of KeysymName: it returns the printable ASCII
// character an X11 keysym name stands for, and 0 for a name outside that range.
//
// Wayland is the caller that needs this. Its compositor hands over a keymap
// that already names keys the way the server wants, so the backend starts from
// the name and works backwards to fill in the numeric keysym and the text —
// which for this range are the character code and the character itself.
func KeysymFor(name string) rune {
	if c, ok := punctuationCharacters[name]; ok {
		return c
	}
	if runes := []rune(name); len(runes) == 1 {
		c := runes[0]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return c
		}
	}
	return 0
}

// punctuationCharacters inverts punctuationKeysyms, built once at startup so
// that neither table can drift from the other.
var punctuationCharacters = func() map[string]rune {
	characters := make(map[string]rune, len(punctuationKeysyms))
	for c, name := range punctuationKeysyms {
		characters[name] = c
	}
	return characters
}()

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
