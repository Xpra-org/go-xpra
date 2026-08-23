//go:build darwin

package darwin

import "testing"

func TestNamedKey(t *testing.T) {
	cases := []struct {
		name     string
		keyCode  uint16
		wantName string
		wantSym  int
	}{
		{"return", vkReturn, "Return", 0xff0d},
		{"backspace", vkDelete, "BackSpace", 0xff08},
		{"forward delete", vkForwardDelete, "Delete", 0xffff},
		{"escape", vkEscape, "Escape", 0xff1b},
		{"left arrow", vkLeftArrow, "Left", 0xff51},
		{"left shift", vkShift, "Shift_L", 0xffe1},
		{"right option", vkRightOption, "Alt_R", 0xffea},
		{"left command", vkCommand, "Super_L", 0xffeb},
		{"f1", vkF1, "F1", 0xffbe},
		{"f16", vkF16, "F16", 0xffcd},
		{"keypad 5", vkKeypad5, "KP_5", 0xffb5},
		{"keypad enter", vkKeypadEnter, "KP_Enter", 0xff8d},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, keysym, ok := namedKey(tc.keyCode)
			if !ok {
				t.Fatalf("namedKey(%#x) found nothing", tc.keyCode)
			}
			if name != tc.wantName || keysym != tc.wantSym {
				t.Errorf("namedKey(%#x) = %q/%#x, want %q/%#x", tc.keyCode, name, keysym, tc.wantName, tc.wantSym)
			}
		})
	}
}

// Keys that produce text must not be named here: their name comes from the
// character the layout produces, which is the only thing that knows what
// they type.
func TestNamedKeyLeavesCharacterKeysUnnamed(t *testing.T) {
	// kVK_ANSI_A .. kVK_ANSI_0 and friends are not in namedKeys at all.
	for _, keyCode := range []uint16{0x00, 0x01, 0x12, 0x1D} {
		if name, _, ok := namedKey(keyCode); ok {
			t.Errorf("namedKey(%#x) named a character key %q", keyCode, name)
		}
	}
}

func TestModifierKeyPressed(t *testing.T) {
	cases := []struct {
		name                 string
		keyCode              uint16
		flags, previousFlags uint64
		want                 bool
	}{
		{"shift goes down", vkShift, nsEventModifierFlagShift, 0, true},
		{"shift goes up", vkShift, 0, nsEventModifierFlagShift, false},
		{"unrelated key while shift held", vkShift, nsEventModifierFlagShift, nsEventModifierFlagShift, false},
		{"not a modifier key", 0x00, nsEventModifierFlagShift, 0, false},
		{"option goes down", vkOption, nsEventModifierFlagOption, 0, true},
		{"command goes down", vkRightCommand, nsEventModifierFlagCommand, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modifierKeyPressed(tc.keyCode, tc.flags, tc.previousFlags); got != tc.want {
				t.Errorf("modifierKeyPressed(%#x, %#x, %#x) = %v, want %v",
					tc.keyCode, tc.flags, tc.previousFlags, got, tc.want)
			}
		})
	}
}

func TestDarwinModifierNamesNeverNil(t *testing.T) {
	if darwinModifierNames(0) == nil {
		t.Error("darwinModifierNames(0) returned nil")
	}
}

func TestDarwinModifierNames(t *testing.T) {
	flags := uint64(nsEventModifierFlagShift | nsEventModifierFlagCommand)
	got := darwinModifierNames(flags)
	want := []string{"shift", "mod4"}
	if len(got) != len(want) {
		t.Fatalf("darwinModifierNames(%#x) = %v, want %v", flags, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("darwinModifierNames(%#x) = %v, want %v", flags, got, want)
		}
	}
}

func TestKeyEventNamedKey(t *testing.T) {
	key := keyEvent(1, vkReturn, "", 0, true)
	if key.Name != "Return" || key.Keysym != 0xff0d {
		t.Errorf("keyEvent(Return) = %+v", key)
	}
	if key.Text != "" {
		t.Errorf("keyEvent(Return) set Text = %q, want empty", key.Text)
	}
}

func TestKeyEventCharacterKey(t *testing.T) {
	key := keyEvent(1, 0x00 /* kVK_ANSI_A */, "A", nsEventModifierFlagShift, true)
	if key.Text != "A" {
		t.Errorf("keyEvent(shift-a).Text = %q, want %q", key.Text, "A")
	}
	if key.Name != "A" || key.Keysym != 'A' {
		t.Errorf("keyEvent(shift-a) = %+v, want Name A Keysym %d", key, int('A'))
	}
	if len(key.Modifiers) != 1 || key.Modifiers[0] != "shift" {
		t.Errorf("keyEvent(shift-a).Modifiers = %v, want [shift]", key.Modifiers)
	}
}
