package client

import (
	"bytes"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

type iconWindow struct {
	ui.Window
	icons []*ui.Icon
}

func (w *iconWindow) SetIcon(icon *ui.Icon) error {
	w.icons = append(w.icons, icon)
	return nil
}

func TestHandleWindowIcon(t *testing.T) {
	window := &iconWindow{}
	client := &Client{windows: map[int64]ui.Window{7: window}}

	client.handlePacket(protocol.Packet{
		"window-icon", int64(7), int64(2), int64(1), "png", cursorPNG(t),
	})

	if len(window.icons) != 1 {
		t.Fatalf("SetIcon called %d times, want once", len(window.icons))
	}
	got := window.icons[0]
	if got.Width != 2 || got.Height != 1 {
		t.Errorf("icon size = %dx%d, want 2x1", got.Width, got.Height)
	}
	// The translucent first pixel is alpha-premultiplied BGRA.
	want := []byte{0x10, 0x20, 0x40, 0x80, 0x30, 0x20, 0x10, 0xff}
	if !bytes.Equal(got.Pixels, want) {
		t.Errorf("icon pixels = %v, want %v", got.Pixels, want)
	}
}

func TestHandleWindowIconRejectsInvalidPackets(t *testing.T) {
	window := &iconWindow{}
	client := &Client{windows: map[int64]ui.Window{7: window}}
	valid := cursorPNG(t)

	for _, packet := range []protocol.Packet{
		{"window-icon", int64(7)},
		{"window-icon", int64(99), int64(2), int64(1), "png", valid},
		{"window-icon", int64(7), int64(2), int64(1), "BGRA", valid},
		{"window-icon", int64(7), int64(2), int64(1), "png", int64(1)},
		{"window-icon", int64(7), int64(0), int64(1), "png", valid},
		{"window-icon", int64(7), int64(maxWindowIconDimension + 1), int64(1), "png", valid},
		{"window-icon", int64(7), int64(1), int64(2), "png", valid},
		{"window-icon", int64(7), int64(2), int64(1), "png", []byte("not png")},
	} {
		client.handlePacket(packet)
	}

	if len(window.icons) != 0 {
		t.Errorf("SetIcon called %d times for invalid packets", len(window.icons))
	}
}
