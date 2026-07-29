package client

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

type cursorDisplay struct {
	ui.Display
	calls []*ui.Cursor
}

func (d *cursorDisplay) SetCursor(cursor *ui.Cursor) error {
	d.calls = append(d.calls, cursor)
	return nil
}

func cursorPNG(t *testing.T) []byte {
	t.Helper()
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 0x80, G: 0x40, B: 0x20, A: 0x80})
	src.SetNRGBA(1, 0, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestHandleCursor(t *testing.T) {
	setBackwardsCompatible(t, true)
	display := &cursorDisplay{}
	client := &Client{display: display}
	packet := protocol.Packet{
		"cursor", "default:png", int64(0), int64(0), int64(2), int64(1),
		int64(99), int64(-4), int64(7), cursorPNG(t), "pointer",
	}

	client.handlePacket(packet)

	if len(display.calls) != 1 || display.calls[0] == nil {
		t.Fatalf("SetCursor calls = %v, want one image", display.calls)
	}
	got := display.calls[0]
	if got.Width != 2 || got.Height != 1 {
		t.Errorf("cursor size = %dx%d, want 2x1", got.Width, got.Height)
	}
	if got.HotspotX != 1 || got.HotspotY != 0 {
		t.Errorf("cursor hotspot = %d,%d, want 1,0", got.HotspotX, got.HotspotY)
	}
	// The translucent first pixel is alpha-premultiplied BGRA.
	want := []byte{0x10, 0x20, 0x40, 0x80, 0x30, 0x20, 0x10, 0xff}
	if !bytes.Equal(got.Pixels, want) {
		t.Errorf("cursor pixels = %v, want %v", got.Pixels, want)
	}
}

func TestHandleCursorReset(t *testing.T) {
	setBackwardsCompatible(t, true)
	display := &cursorDisplay{}
	client := &Client{display: display}

	client.handlePacket(protocol.Packet{"cursor", ""})

	if len(display.calls) != 1 || display.calls[0] != nil {
		t.Errorf("SetCursor calls = %v, want [nil]", display.calls)
	}
}

func TestHandleCursorRejectsInvalidPackets(t *testing.T) {
	setBackwardsCompatible(t, true)
	display := &cursorDisplay{}
	client := &Client{display: display}

	client.handlePacket(protocol.Packet{"cursor", "raw", int64(0), int64(0), int64(1),
		int64(1), int64(0), int64(0), int64(1), []byte{0, 0, 0, 0}})
	client.handlePacket(protocol.Packet{"cursor", "png", int64(0)})
	client.handlePacket(protocol.Packet{"cursor", "png", int64(0), int64(0), int64(1),
		int64(1), int64(0), int64(0), int64(1), []byte("not png")})

	if len(display.calls) != 0 {
		t.Errorf("SetCursor called %d times for invalid packets", len(display.calls))
	}
}

func TestHandleModernCursorPackets(t *testing.T) {
	setBackwardsCompatible(t, false)
	display := &cursorDisplay{}
	client := &Client{display: display}

	client.handlePacket(protocol.Packet{
		"cursor", "png", int64(0), int64(0), int64(2), int64(1),
		int64(0), int64(0), int64(7), cursorPNG(t), "legacy",
	})
	if len(display.calls) != 0 {
		t.Fatal("legacy cursor packet was accepted with compatibility disabled")
	}

	client.handlePacket(protocol.Packet{
		"cursor-data", "png", int64(2), int64(1), int64(99), int64(-4),
		int64(7), cursorPNG(t), "pointer",
	})
	if len(display.calls) != 1 || display.calls[0] == nil {
		t.Fatalf("SetCursor calls = %v, want one image", display.calls)
	}
	got := display.calls[0]
	if got.HotspotX != 1 || got.HotspotY != 0 {
		t.Errorf("cursor hotspot = %d,%d, want 1,0", got.HotspotX, got.HotspotY)
	}

	client.handlePacket(protocol.Packet{"cursor-default"})
	if len(display.calls) != 2 || display.calls[1] != nil {
		t.Errorf("SetCursor calls = %v, want image then nil", display.calls)
	}
}
