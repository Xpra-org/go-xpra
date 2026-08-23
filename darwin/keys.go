//go:build darwin

package darwin

import "github.com/Xpra-org/go-xpra/ui"

// macOS virtual keycodes this backend names by number, from Carbon's
// HIToolbox/Events.h (the kVK_* constants). They have been stable across
// macOS releases for decades and are reproduced here as plain numeric
// constants rather than linked, to avoid pulling in the Carbon framework for
// a couple of dozen numbers.
const (
	vkReturn         = 0x24
	vkTab            = 0x30
	vkDelete         = 0x33 // backspace
	vkEscape         = 0x35
	vkRightCommand   = 0x36
	vkCommand        = 0x37
	vkShift          = 0x38
	vkCapsLock       = 0x39
	vkOption         = 0x3A
	vkControl        = 0x3B
	vkRightShift     = 0x3C
	vkRightOption    = 0x3D
	vkRightControl   = 0x3E
	vkFunction       = 0x3F
	vkKeypadDecimal  = 0x41
	vkKeypadMultiply = 0x43
	vkKeypadPlus     = 0x45
	vkKeypadClear    = 0x47
	vkKeypadDivide   = 0x4B
	vkKeypadEnter    = 0x4C
	vkKeypadMinus    = 0x4E
	vkKeypadEquals   = 0x51
	vkKeypad0        = 0x52
	vkKeypad1        = 0x53
	vkKeypad2        = 0x54
	vkKeypad3        = 0x55
	vkKeypad4        = 0x56
	vkKeypad5        = 0x57
	vkKeypad6        = 0x58
	vkKeypad7        = 0x59
	vkKeypad8        = 0x5B
	vkKeypad9        = 0x5C
	vkF5             = 0x60
	vkF6             = 0x61
	vkF7             = 0x62
	vkF3             = 0x63
	vkF8             = 0x64
	vkF9             = 0x65
	vkF11            = 0x67
	vkF13            = 0x69
	vkF16            = 0x6A
	vkF14            = 0x6B
	vkF10            = 0x6D
	vkF12            = 0x6F
	vkF15            = 0x71
	vkHelp           = 0x72
	vkHome           = 0x73
	vkPageUp         = 0x74
	vkForwardDelete  = 0x75
	vkF4             = 0x76
	vkEnd            = 0x77
	vkF2             = 0x78
	vkPageDown       = 0x79
	vkF1             = 0x7A
	vkLeftArrow      = 0x7B
	vkRightArrow     = 0x7C
	vkDownArrow      = 0x7D
	vkUpArrow        = 0x7E
)

// The NSEventModifierFlags bits this backend reasons about.
const (
	nsEventModifierFlagCapsLock = 1 << 16
	nsEventModifierFlagShift    = 1 << 17
	nsEventModifierFlagControl  = 1 << 18
	nsEventModifierFlagOption   = 1 << 19
	nsEventModifierFlagCommand  = 1 << 20
)

type namedKeysym struct {
	name   string
	keysym int
}

// namedKeys names the keys that produce no text, or whose text is not what
// the server should match on, so they have to be recognised by virtual
// keycode. The keysyms are X11's, which the server takes as a hint when the
// name alone leaves it a choice.
var namedKeys = map[uint16]namedKeysym{
	vkReturn:        {"Return", 0xff0d},
	vkTab:           {"Tab", 0xff09},
	vkDelete:        {"BackSpace", 0xff08},
	vkEscape:        {"Escape", 0xff1b},
	vkForwardDelete: {"Delete", 0xffff},
	vkHome:          {"Home", 0xff50},
	vkEnd:           {"End", 0xff57},
	vkPageUp:        {"Page_Up", 0xff55},
	vkPageDown:      {"Page_Down", 0xff56},
	vkLeftArrow:     {"Left", 0xff51},
	vkUpArrow:       {"Up", 0xff52},
	vkRightArrow:    {"Right", 0xff53},
	vkDownArrow:     {"Down", 0xff54},
	vkHelp:          {"Help", 0xff6a},

	vkShift:        {"Shift_L", 0xffe1},
	vkRightShift:   {"Shift_R", 0xffe2},
	vkControl:      {"Control_L", 0xffe3},
	vkRightControl: {"Control_R", 0xffe4},
	// Option is the closest macOS has to X11's Alt, and Command to its Super
	// — the same split win32/keys.go makes between its own Alt and Windows
	// keys, so keep it consistent within this client rather than trying to
	// guess how other xpra clients on macOS name them.
	vkOption:       {"Alt_L", 0xffe9},
	vkRightOption:  {"Alt_R", 0xffea},
	vkCommand:      {"Super_L", 0xffeb},
	vkRightCommand: {"Super_R", 0xffec},
	vkCapsLock:     {"Caps_Lock", 0xffe5},
	vkFunction:     {"Menu", 0xff67}, // no X11 equivalent; the closest existing name

	vkF1: {"F1", 0xffbe}, vkF2: {"F2", 0xffbf}, vkF3: {"F3", 0xffc0},
	vkF4: {"F4", 0xffc1}, vkF5: {"F5", 0xffc2}, vkF6: {"F6", 0xffc3},
	vkF7: {"F7", 0xffc4}, vkF8: {"F8", 0xffc5}, vkF9: {"F9", 0xffc6},
	vkF10: {"F10", 0xffc7}, vkF11: {"F11", 0xffc8}, vkF12: {"F12", 0xffc9},
	vkF13: {"F13", 0xffca}, vkF14: {"F14", 0xffcb}, vkF15: {"F15", 0xffcc},
	vkF16: {"F16", 0xffcd},

	vkKeypad0: {"KP_0", 0xffb0}, vkKeypad1: {"KP_1", 0xffb1},
	vkKeypad2: {"KP_2", 0xffb2}, vkKeypad3: {"KP_3", 0xffb3},
	vkKeypad4: {"KP_4", 0xffb4}, vkKeypad5: {"KP_5", 0xffb5},
	vkKeypad6: {"KP_6", 0xffb6}, vkKeypad7: {"KP_7", 0xffb7},
	vkKeypad8: {"KP_8", 0xffb8}, vkKeypad9: {"KP_9", 0xffb9},
	vkKeypadDecimal:  {"KP_Decimal", 0xffae},
	vkKeypadMultiply: {"KP_Multiply", 0xffaa},
	vkKeypadPlus:     {"KP_Add", 0xffab},
	vkKeypadMinus:    {"KP_Subtract", 0xffad},
	vkKeypadDivide:   {"KP_Divide", 0xffaf},
	vkKeypadEnter:    {"KP_Enter", 0xff8d},
	vkKeypadEquals:   {"KP_Equal", 0xffbd},
	vkKeypadClear:    {"Clear", 0xff0b},
}

// modifierKeyMask maps the virtual keycode of a modifier key to the
// NSEventModifierFlags bit AppKit reports it under in a flagsChanged: event.
//
// The two shifts (and, less commonly, other modifier pairs) share a single
// bit: AppKit's public API has no per-side flag, only per-side keycodes. So
// holding both shifts and releasing just one is reported as that keycode's
// flagsChanged: event while the shared bit stays set — modifierKeyPressed
// reads that as still-pressed. This is a known, minor limitation shared by
// every framework built on this same public API.
var modifierKeyMask = map[uint16]uint64{
	vkShift:        nsEventModifierFlagShift,
	vkRightShift:   nsEventModifierFlagShift,
	vkControl:      nsEventModifierFlagControl,
	vkRightControl: nsEventModifierFlagControl,
	vkOption:       nsEventModifierFlagOption,
	vkRightOption:  nsEventModifierFlagOption,
	vkCommand:      nsEventModifierFlagCommand,
	vkRightCommand: nsEventModifierFlagCommand,
	vkCapsLock:     nsEventModifierFlagCapsLock,
}

// modifierKeyPressed reports whether a flagsChanged: event for keyCode
// represents that specific modifier key going down rather than up, given the
// flags mask now in effect and the one seen immediately before this event.
func modifierKeyPressed(keyCode uint16, flags, previousFlags uint64) bool {
	bit, ok := modifierKeyMask[keyCode]
	if !ok {
		return false
	}
	return flags&bit != 0 && previousFlags&bit == 0
}

// darwinModifierNames converts an NSEventModifierFlags mask into the X11
// modifier names xpra expects.
func darwinModifierNames(flags uint64) []string {
	modifiers := []string{}
	if flags&nsEventModifierFlagShift != 0 {
		modifiers = append(modifiers, "shift")
	}
	if flags&nsEventModifierFlagCapsLock != 0 {
		modifiers = append(modifiers, "lock")
	}
	if flags&nsEventModifierFlagControl != 0 {
		modifiers = append(modifiers, "control")
	}
	if flags&nsEventModifierFlagOption != 0 {
		modifiers = append(modifiers, "mod1")
	}
	if flags&nsEventModifierFlagCommand != 0 {
		modifiers = append(modifiers, "mod4")
	}
	return modifiers
}

// namedKey returns the X11 name and keysym of a key that produces no text.
func namedKey(keyCode uint16) (name string, keysym int, ok bool) {
	key, ok := namedKeys[keyCode]
	if !ok {
		return "", 0, false
	}
	return key.name, key.keysym, true
}

// keyEvent builds the ui.Key the server matches on, from a key's virtual
// keycode, the character its keyboard layout produces with Option, Control
// and Command left out (but Shift and Caps Lock honoured — the same
// distinction win32's character() draws by zeroing only the control/alt bits
// before asking Windows to resolve a character), and the modifiers in effect.
//
// keyCode alone names keys that produce no text; everything else is named
// from that character, matching the split win32/keys.go makes between
// namedKey and character.
func keyEvent(window ui.WindowID, keyCode uint16, charsIgnoringModifiers string, flags uint64, pressed bool) ui.Key {
	event := ui.Key{
		Window:    window,
		Pressed:   pressed,
		Modifiers: darwinModifierNames(flags),
	}
	if name, keysym, ok := namedKey(keyCode); ok {
		event.Name, event.Keysym = name, keysym
		return event
	}

	runes := []rune(charsIgnoringModifiers)
	if len(runes) != 1 {
		return event
	}
	c := runes[0]
	event.Text = string(c)
	// For printable ASCII the keysym value is the character code itself;
	// beyond that this backend has no name to offer, and the client drops the
	// key rather than send the server one it cannot resolve.
	if name := ui.KeysymName(c); name != "" {
		event.Name, event.Keysym = name, int(c)
	}
	return event
}
