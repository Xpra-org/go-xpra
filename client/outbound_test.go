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
	painted             []byte
	paintStride         int
	paintFormat         string
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

func (w *outboundWindow) Paint(_, _, _, _ int, pixels []byte, rowstride int, format string) error {
	w.painted = append(w.painted[:0], pixels...)
	w.paintStride = rowstride
	w.paintFormat = format
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
	if len(windowMap) != 8 {
		t.Errorf("window-map without monitors has %d elements, want 8: %v", len(windowMap), windowMap)
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
	if windowConfigure.Dict(2).Has("monitor") {
		t.Errorf("window-configure invented a monitor descriptor: %v", windowConfigure)
	}

	client.handleMotion(ui.Motion{Window: window.id, X: 12, Y: 22})
	motion := receiveOutbound(t, server, "pointer-motion")
	if len(motion.Dict(5)) != 0 {
		t.Errorf("pointer-motion invented monitor properties: %v", motion)
	}

	client.handleButton(ui.Button{Window: window.id, Button: 1, Pressed: true, X: 12, Y: 22})
	button := receiveOutbound(t, server, "pointer-button")
	if len(button.Dict(7)) != 0 {
		t.Errorf("pointer-button invented monitor properties: %v", button)
	}

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

func TestPingEchoUsesTheNegotiatedPacketName(t *testing.T) {
	tests := []struct {
		name                string
		backwardsCompatible bool
		packetType          string
	}{
		{"compatible", true, "ping_echo"},
		{"modern", false, "ping-echo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setBackwardsCompatible(t, test.backwardsCompatible)
			client, server, _ := outboundHarness(t)
			client.handlePing(protocol.Packet{"ping", int64(123), int64(0), "session"})
			echo := receiveOutbound(t, server, test.packetType)
			if echo.Int(1) != 123 || echo.Str(6) != "session" {
				t.Errorf("%s = %v", test.packetType, echo)
			}
		})
	}
}

func TestExitRequestDisconnectsAndStopsCleanly(t *testing.T) {
	tests := []struct {
		name                string
		backwardsCompatible bool
		packetType          string
	}{
		{"compatible", true, "disconnect"},
		{"modern", false, "connection-close"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setBackwardsCompatible(t, test.backwardsCompatible)
			client, server, _ := outboundHarness(t)
			events := make(chan ui.Event, 1)
			client.display = &exitRequestDisplay{events: events}
			runResult := make(chan error, 1)
			go func() { runResult <- client.Run() }()

			receiveOutbound(t, server, "hello")
			events <- ui.ExitRequest{}
			packet := receiveOutbound(t, server, test.packetType)
			if len(packet) != 2 || packet.Str(1) != "client exit" {
				t.Errorf("close packet = %v, want [%s client exit]", packet, test.packetType)
			}
			select {
			case err := <-runResult:
				if err != nil {
					t.Errorf("Run returned %v, want a clean exit", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Run did not stop after ExitRequest")
			}
		})
	}
}

// Disconnect is what the interrupt handler calls: the packet has to be sent by
// the run loop, from whatever goroutine asked for the shutdown.
func TestDisconnectSendsTheReasonAndStopsCleanly(t *testing.T) {
	tests := []struct {
		name                string
		backwardsCompatible bool
		packetType          string
	}{
		{"compatible", true, "disconnect"},
		{"modern", false, "connection-close"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setBackwardsCompatible(t, test.backwardsCompatible)
			client, server, _ := outboundHarness(t)
			client.display = &exitRequestDisplay{events: make(chan ui.Event)}
			runResult := make(chan error, 1)
			go func() { runResult <- client.Run() }()

			receiveOutbound(t, server, "hello")
			client.Disconnect("client interrupted")
			packet := receiveOutbound(t, server, test.packetType)
			if len(packet) != 2 || packet.Str(1) != "client interrupted" {
				t.Errorf("close packet = %v, want [%s client interrupted]", packet, test.packetType)
			}
			select {
			case err := <-runResult:
				if err != nil {
					t.Errorf("Run returned %v, want a clean exit", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Run did not stop after Disconnect")
			}
		})
	}
}

// Disconnect must be harmless when there is no run loop left to hear it: the
// interrupt handler cannot know whether Run has already returned.
func TestDisconnectAfterRunIsANoOp(t *testing.T) {
	client, _, _ := outboundHarness(t)
	client.Disconnect("client interrupted")
	client.Disconnect("client interrupted")
}

type exitRequestDisplay struct {
	ui.Display
	events <-chan ui.Event
}

func (d *exitRequestDisplay) Events() <-chan ui.Event { return d.events }

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

func TestWheelPacketTypeAndShape(t *testing.T) {
	tests := []struct {
		name                string
		backwardsCompatible bool
		button              int
		distance            int64
		packetType          string
	}{
		{"compatible", true, 4, 1000, "wheel-motion"},
		{"modern", false, 7, -1000, "pointer-wheel"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setBackwardsCompatible(t, test.backwardsCompatible)
			client, server, window := outboundHarness(t)
			client.windows[7] = window
			client.byLocal[window.id] = 7

			event := ui.Button{
				Window: window.id, Button: test.button, Pressed: true,
				X: 12, Y: 22,
			}
			client.handleButton(event)
			event.Pressed = false
			client.handleButton(event)

			wheel := receiveOutbound(t, server, test.packetType)
			if len(wheel) != 8 {
				t.Fatalf("%s has %d elements, want 8: %v", test.packetType, len(wheel), wheel)
			}
			if wheel.Int(1) != 7 || wheel.Int(2) != int64(test.button) || wheel.Int(3) != test.distance {
				t.Errorf("%s header fields are wrong: %v", test.packetType, wheel)
			}
			if got := wheel[4]; !reflect.DeepEqual(got, []any{int64(12), int64(22)}) {
				t.Errorf("%s pointer = %#v", test.packetType, got)
			}
			if got := wheel[5]; !reflect.DeepEqual(got, []any{}) {
				t.Errorf("%s modifiers = %#v", test.packetType, got)
			}
			if got := wheel[6]; !reflect.DeepEqual(got, []any{}) {
				t.Errorf("%s buttons = %#v", test.packetType, got)
			}
			if got := wheel.Dict(7); len(got) != 0 {
				t.Errorf("%s properties = %#v", test.packetType, got)
			}

			// The wheel release was consumed, so the next packet must be this
			// ordinary pointer button rather than a second wheel packet.
			client.handleButton(ui.Button{
				Window: window.id, Button: 1, Pressed: true, X: 12, Y: 22,
			})
			receiveOutbound(t, server, "pointer-button")
		})
	}
}

func TestPositionPacketsIncludeMonitorRelativeCoordinates(t *testing.T) {
	setBackwardsCompatible(t, false)
	client, server, window := outboundHarness(t)
	client.monitors = usableMonitors([]ui.Monitor{
		{}, // invalid monitors must not consume an advertised index
		{Geometry: ui.Rectangle{X: -1920, Width: 1920, Height: 1080}},
		{Geometry: ui.Rectangle{Width: 2560, Height: 1440}},
	})

	assertMonitor := func(packet protocol.Packet, field int, key string, index, x, y int64) {
		t.Helper()
		descriptor := packet.Dict(field)
		if key != "" {
			descriptor = descriptor.Dict(key)
		}
		if descriptor == nil {
			t.Fatalf("%s monitor descriptor is missing: %v", packet.Type(), packet)
		}
		if descriptor.Int("index") != index {
			t.Errorf("%s monitor index = %d, want %d", packet.Type(), descriptor.Int("index"), index)
		}
		if got := descriptor["position"]; !reflect.DeepEqual(got, []any{x, y}) {
			t.Errorf("%s monitor position = %#v, want [%d %d]", packet.Type(), got, x, y)
		}
	}

	client.handleNewWindow(protocol.Packet{
		"window-create", int64(7), int64(-1800), int64(100), int64(640), int64(480),
		map[string]any{}, map[string]any{},
	}, false)
	windowMap := receiveOutbound(t, server, "window-map")
	if len(windowMap) != 9 {
		t.Fatalf("window-map has %d elements, want monitor field at index 8: %v", len(windowMap), windowMap)
	}
	assertMonitor(windowMap, 8, "", 0, 120, 100)

	// A window origin may be outside every monitor while the rest of the
	// window remains visible. It still gets a stable monitor anchor and a
	// deliberately negative relative position.
	client.handleConfigure(ui.Configure{
		Window: window.id, X: -1930, Y: -10, Width: 640, Height: 480,
	})
	windowConfigure := receiveOutbound(t, server, "window-configure")
	assertMonitor(windowConfigure, 2, "monitor", 0, -10, -10)

	client.handleMotion(ui.Motion{Window: window.id, X: 100, Y: 50})
	assertMonitor(receiveOutbound(t, server, "pointer-motion"), 5, "monitor", 1, 100, 50)

	client.handleButton(ui.Button{
		Window: window.id, Button: 1, Pressed: true, X: -10, Y: 500,
	})
	assertMonitor(receiveOutbound(t, server, "pointer-button"), 7, "monitor", 0, 1910, 500)

	client.handleButton(ui.Button{
		Window: window.id, Button: 4, Pressed: true, X: 200, Y: 60,
	})
	assertMonitor(receiveOutbound(t, server, "pointer-wheel"), 7, "monitor", 1, 200, 60)
}
