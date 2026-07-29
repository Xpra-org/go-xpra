//go:build windows

package win32

import "testing"

// Naming a key is the whole of what this backend has to get right about the
// keyboard: the server matches on the name, so a wrong one types the wrong
// character or nothing at all.
func TestNamedKey(t *testing.T) {
	cases := []struct {
		name     string
		vk       byte
		extended bool
		wantName string
		wantSym  int
	}{
		{"return", vkReturn, false, "Return", 0xff0d},
		// The keypad's enter is the extended twin of the ordinary one.
		{"keypad enter", vkReturn, true, "KP_Enter", 0xff8d},
		// The navigation cluster is extended; the keypad keys that share its
		// virtual keys while num lock is off are not.
		{"home", vkHome, true, "Home", 0xff50},
		{"keypad home", vkHome, false, "KP_Home", 0xff95},
		{"delete", vkDelete, true, "Delete", 0xffff},
		{"keypad delete", vkDelete, false, "KP_Delete", 0xff9f},
		{"page up", vkPrior, true, "Page_Up", 0xff55},
		{"escape", vkEscape, false, "Escape", 0xff1b},
		{"f1", 0x70, false, "F1", 0xffbe},
		{"f12", 0x7B, false, "F12", 0xffc9},
		{"left shift", vkLShift, false, "Shift_L", 0xffe1},
		{"right alt", vkRMenu, true, "Alt_R", 0xffea},
		{"keypad 5", 0x65, false, "KP_5", 0xffb5},
		{"keypad divide", 0x6F, true, "KP_Divide", 0xffaf},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, keysym, ok := namedKey(tc.vk, tc.extended)
			if !ok {
				t.Fatalf("namedKey(%#x, %v) found nothing", tc.vk, tc.extended)
			}
			if name != tc.wantName || keysym != tc.wantSym {
				t.Errorf("namedKey(%#x, %v) = %q/%#x, want %q/%#x",
					tc.vk, tc.extended, name, keysym, tc.wantName, tc.wantSym)
			}
		})
	}
}

// Keys that produce text must not be named here: their name comes from the
// keyboard layout, which is the only thing that knows what they type.
func TestNamedKeyLeavesCharacterKeys(t *testing.T) {
	for _, vk := range []byte{'A', 'Z', '0', '9', 0xBA /* ;: on a US layout */} {
		if name, _, ok := namedKey(vk, false); ok {
			t.Errorf("namedKey(%#x) named a character key %q", vk, name)
		}
	}
}

// X11 names the two shifts, controls and alts apart, and Windows does not
// report which was pressed except through the scan code and the extended flag.
func TestSide(t *testing.T) {
	const (
		scanLeftShift  = 0x2A
		scanRightShift = 0x36
	)
	cases := []struct {
		name     string
		vk, scan byte
		extended bool
		want     byte
	}{
		{"left shift", vkShift, scanLeftShift, false, vkLShift},
		{"right shift", vkShift, scanRightShift, false, vkRShift},
		{"left control", vkControl, 0x1D, false, vkLControl},
		{"right control", vkControl, 0x1D, true, vkRControl},
		{"left alt", vkMenu, 0x38, false, vkLMenu},
		{"right alt", vkMenu, 0x38, true, vkRMenu},
		{"anything else", 'A', 0x1E, false, 'A'},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := side(tc.vk, tc.scan, tc.extended); got != tc.want {
				t.Errorf("side(%#x, %#x, %v) = %#x, want %#x",
					tc.vk, tc.scan, tc.extended, got, tc.want)
			}
		})
	}
}

// The modifier list is sent with every key, and an absent one has to encode as
// an empty list rather than as a null.
func TestModifierNamesNeverNil(t *testing.T) {
	if modifierNames() == nil {
		t.Error("modifierNames returned nil")
	}
}

// The server places windows by their content area, so a decorated window has to
// be created larger than the geometry it was given and offset by its frame.
func TestFrame(t *testing.T) {
	style, exStyle := styles(false)
	outer, err := frame(100, 200, 640, 480, style, exStyle)
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	if outer.Left > 100 || outer.Top >= 200 {
		t.Errorf("frame did not make room for the border and title bar: %+v", outer)
	}
	if outer.Right-outer.Left < 640 || outer.Bottom-outer.Top < 480 {
		t.Errorf("frame %+v is smaller than the content it has to hold", outer)
	}

	// An override-redirect window has no frame, so it is positioned exactly
	// where the server put it.
	style, exStyle = styles(true)
	popup, err := frame(100, 200, 640, 480, style, exStyle)
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	want := rect{Left: 100, Top: 200, Right: 740, Bottom: 680}
	if popup != want {
		t.Errorf("popup frame = %+v, want %+v", popup, want)
	}
}

func TestInitialFrameAtOriginStaysAccessible(t *testing.T) {
	style, exStyle := styles(false)
	outer, err := frame(0, 0, 640, 480, style, exStyle)
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	width, height := outer.Right-outer.Left, outer.Bottom-outer.Top

	placed := placeInitialFrame(outer)
	if placed.Left != 0 || placed.Top != 0 {
		t.Errorf("initial frame starts at %d,%d, want 0,0", placed.Left, placed.Top)
	}
	if placed.Right-placed.Left != width || placed.Bottom-placed.Top != height {
		t.Errorf("initial frame changed size from %dx%d to %dx%d",
			width, height, placed.Right-placed.Left, placed.Bottom-placed.Top)
	}
}

func TestHiword(t *testing.T) {
	// The wheel delta arrives in the high word of wParam, one notch at a time
	// and signed.
	if got := int16(hiword(0x0078_0000)); got != wheelDelta {
		t.Errorf("hiword of a wheel-up wParam = %d, want %d", got, wheelDelta)
	}
	if got := int16(hiword(0xFF88_0000)); got != -wheelDelta {
		t.Errorf("hiword of a wheel-down wParam = %d, want %d", got, -wheelDelta)
	}
}
