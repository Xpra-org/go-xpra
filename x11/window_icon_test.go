//go:build linux

package x11

import (
	"testing"

	"github.com/Xpra-org/go-xpra/ui"
)

func TestNetWMIconUnpremultipliesARGB(t *testing.T) {
	icon, err := netWMIcon(&ui.Icon{
		Width: 2, Height: 1,
		Pixels: []byte{
			0x10, 0x20, 0x40, 0x80,
			0x03, 0x02, 0x01, 0xff,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint{0x807f3f1f, 0xff010203}
	if icon.Width != 2 || icon.Height != 1 || len(icon.Data) != len(want) {
		t.Fatalf("EWMH icon = %#v, want a 2x1 icon with two pixels", icon)
	}
	for i := range want {
		if icon.Data[i] != want[i] {
			t.Errorf("pixel %d = %#08x, want %#08x", i, icon.Data[i], want[i])
		}
	}
}
