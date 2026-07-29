package client

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

type outboundDisplay struct {
	ui.Display
	window ui.Window
}

func (d *outboundDisplay) NewWindow(_, _, _, _ int, _ bool) (ui.Window, error) {
	return d.window, nil
}

type outboundWindow struct {
	ui.Window
	id                  ui.WindowID
	x, y, width, height int
}

func (w *outboundWindow) ID() ui.WindowID {
	return w.id
}

func (w *outboundWindow) Geometry() (int, int, int, int) {
	return w.x, w.y, w.width, w.height
}

func (w *outboundWindow) SetTitle(string) {}

func (w *outboundWindow) Map() {}

func (w *outboundWindow) Resized(x, y, width, height int) error {
	w.x, w.y, w.width, w.height = x, y, width, height
	return nil
}

func (w *outboundWindow) Paint(_, _, _, _ int, _ []byte, _ int, _ string) error {
	return nil
}

func outboundHarness(t *testing.T) (*Client, *protocol.Conn, *outboundWindow) {
	t.Helper()
	clientStream, serverStream := net.Pipe()
	clientConn := protocol.New(clientStream)
	serverConn := protocol.New(serverStream)
	window := &outboundWindow{id: 99}
	client := New(clientConn, &outboundDisplay{window: window}, false, "", "")
	t.Cleanup(func() {
		clientConn.Close()
		serverConn.Close()
	})
	return client, serverConn, window
}

func receiveOutbound(t *testing.T, conn *protocol.Conn, wantType string) protocol.Packet {
	t.Helper()
	select {
	case packet, ok := <-conn.Packets():
		if !ok {
			t.Fatalf("connection closed while waiting for %q: %v", wantType, conn.Err())
		}
		if packet.Type() != wantType {
			t.Fatalf("packet type = %q, want %q; packet: %v", packet.Type(), wantType, packet)
		}
		return packet
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q", wantType)
		return nil
	}
}

func TestOutboundPacketsUseModernTypesAndShapes(t *testing.T) {
	setBackwardsCompatible(t, true)
	client, server, window := outboundHarness(t)

	if err := client.sendHello("", nil); err != nil {
		t.Fatalf("sendHello: %v", err)
	}
	receiveOutbound(t, server, "hello")

	client.handleNewWindow(protocol.Packet{
		"window-create", int64(7), int64(10), int64(20), int64(2), int64(1),
		map[string]any{"title": "test"}, map[string]any{},
	}, false)
	windowMap := receiveOutbound(t, server, "window-map")
	if windowMap.Int(1) != 7 || windowMap.Int(2) != 10 || windowMap.Int(3) != 20 ||
		windowMap.Int(4) != 2 || windowMap.Int(5) != 1 {
		t.Errorf("window-map geometry is wrong: %v", windowMap)
	}

	client.handleConfigure(ui.Configure{Window: window.id, X: 11, Y: 21, Width: 3, Height: 2})
	windowConfigure := receiveOutbound(t, server, "window-configure")
	if len(windowConfigure) != 3 {
		t.Fatalf("window-configure has %d elements, want 3: %v", len(windowConfigure), windowConfigure)
	}
	if got := windowConfigure.Dict(2)["geometry"]; !reflect.DeepEqual(got, []any{
		int64(11), int64(21), int64(3), int64(2),
	}) {
		t.Errorf("window-configure geometry = %#v", got)
	}

	client.handleMotion(ui.Motion{Window: window.id, X: 12, Y: 22})
	receiveOutbound(t, server, "pointer-motion")

	client.handleButton(ui.Button{Window: window.id, Button: 1, Pressed: true, X: 12, Y: 22})
	receiveOutbound(t, server, "pointer-button")

	client.handleKey(ui.Key{
		Window: window.id, Name: "a", Pressed: true, Modifiers: []string{"shift"},
		Keysym: 0x61, Text: "a", Keycode: 38,
	})
	keyboardEvent := receiveOutbound(t, server, "keyboard-event")
	if len(keyboardEvent) != 5 {
		t.Fatalf("keyboard-event has %d elements, want 5: %v", len(keyboardEvent), keyboardEvent)
	}
	attrs := keyboardEvent.Dict(4)
	if attrs.Int("keyval") != 0x61 || attrs.Str("string") != "a" ||
		attrs.Int("keycode") != 38 || attrs.Int("group") != 0 {
		t.Errorf("keyboard-event attributes are wrong: %v", attrs)
	}
	if got := attrs["modifiers"]; !reflect.DeepEqual(got, []any{"shift"}) {
		t.Errorf("keyboard-event modifiers = %#v", got)
	}

	client.handleFocus(ui.Focus{Window: window.id})
	receiveOutbound(t, server, "window-focus")

	client.handleDraw(protocol.Packet{
		"window-draw", int64(7), int64(0), int64(0), int64(1), int64(1),
		"rgb32", []byte{0, 0, 0, 0}, int64(42), int64(4),
		map[string]any{"rgb_format": "BGRX"},
	})
	drawAck := receiveOutbound(t, server, "window-draw-ack")
	if drawAck.Int(1) != 42 || drawAck.Int(2) != 7 ||
		drawAck.Int(3) != 1 || drawAck.Int(4) != 1 || drawAck.Int(5) <= 0 {
		t.Errorf("window-draw-ack is wrong: %v", drawAck)
	}

	client.handleCloseRequest(ui.CloseRequest{Window: window.id})
	receiveOutbound(t, server, "window-close")

	client.handlePing(protocol.Packet{"ping", int64(123), int64(0), "session"})
	receiveOutbound(t, server, "ping_echo")
}

func TestWindowDrawAckUsesTrunkShapeWithoutCompatibility(t *testing.T) {
	setBackwardsCompatible(t, false)
	client, server, window := outboundHarness(t)
	client.windows[7] = window

	client.handleDraw(protocol.Packet{
		"window-draw", int64(7), int64(0), int64(0), int64(3), int64(2),
		"rgb32", make([]byte, 24), int64(42), int64(12),
		map[string]any{"rgb_format": "BGRX"},
	})

	drawAck := receiveOutbound(t, server, "window-draw-ack")
	if drawAck.Int(1) != 7 || drawAck.Int(2) != 3 || drawAck.Int(3) != 2 ||
		drawAck.Int(4) != 42 || drawAck.Int(5) <= 0 || drawAck.Str(6) != "" {
		t.Errorf("strict window-draw-ack has the wrong shape: %v", drawAck)
	}
}
