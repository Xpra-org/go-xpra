package client

import (
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

type minimizingWindow struct {
	ui.Window
	states []bool
}

func (w *minimizingWindow) Minimize(minimized bool) {
	w.states = append(w.states, minimized)
}

func TestHandleShowDesktop(t *testing.T) {
	first, second := &minimizingWindow{}, &minimizingWindow{}
	client := &Client{windows: map[int64]ui.Window{
		1: first,
		2: second,
	}}

	client.handlePacket(protocol.Packet{"show-desktop", true})
	client.handlePacket(protocol.Packet{"show-desktop", false})

	for name, window := range map[string]*minimizingWindow{
		"first":  first,
		"second": second,
	} {
		if len(window.states) != 2 || !window.states[0] || window.states[1] {
			t.Errorf("%s window minimize states = %v, want [true false]", name, window.states)
		}
	}
}

func TestHandleShowDesktopShortPacketRestores(t *testing.T) {
	window := &minimizingWindow{}
	client := &Client{windows: map[int64]ui.Window{1: window}}

	client.handlePacket(protocol.Packet{"show-desktop"})

	if len(window.states) != 1 || window.states[0] {
		t.Errorf("window minimize states = %v, want [false]", window.states)
	}
}
