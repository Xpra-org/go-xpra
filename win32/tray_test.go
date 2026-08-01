//go:build windows

package win32

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSetFixedUTF16TruncatesAndTerminates(t *testing.T) {
	tests := []struct {
		name string
		size int
		text string
		want string
	}{
		{"fits", 8, "xpra", "xpra"},
		{"leaves terminator", 5, "xpra!", "xpra"},
		{"keeps surrogate pair", 6, "ab😀cd", "ab😀c"},
		{"drops split surrogate", 4, "ab😀", "ab"},
		{"empty field", 0, "xpra", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			field := make([]uint16, test.size)
			for i := range field {
				field[i] = 0xffff
			}
			setFixedUTF16(field, test.text)
			end := 0
			for end < len(field) && field[end] != 0 {
				end++
			}
			got := string(utf16.Decode(field[:end]))
			if got != test.want {
				t.Errorf("decoded field = %q, want %q", got, test.want)
			}
			if len(field) > 0 && field[len(field)-1] != 0 {
				t.Errorf("fixed field is not NUL-terminated: %#v", field)
			}
		})
	}
}

func TestTooltipUTF16UsesItsFixedCapacity(t *testing.T) {
	var tooltip [128]uint16
	setFixedUTF16(tooltip[:], strings.Repeat("x", 200))
	if got := strings.IndexRune(string(utf16.Decode(tooltip[:])), '\x00'); got != 127 {
		t.Errorf("tooltip terminator index = %d, want 127", got)
	}
}

func TestTrayActivated(t *testing.T) {
	for _, notification := range []uint32{
		wmLButtonUp, wmRButtonUp, wmContextMenu, ninSelect, ninKeySelect,
		0x12340000 | wmContextMenu,
	} {
		if !trayActivated(notification) {
			t.Errorf("trayActivated(%#x) = false", notification)
		}
	}
	for _, notification := range []uint32{wmMouseMove, wmLButtonDown, wmRButtonDown, wmKeyDown} {
		if trayActivated(notification) {
			t.Errorf("trayActivated(%#x) = true", notification)
		}
	}
}
