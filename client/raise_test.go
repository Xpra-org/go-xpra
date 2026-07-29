package client

import (
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

type raisingWindow struct {
	ui.Window
	raised int
}

func (w *raisingWindow) Raise() {
	w.raised++
}

func TestHandleRaiseWindow(t *testing.T) {
	setBackwardsCompatible(t, true)
	for _, packetType := range []string{"raise-window", "window-raise"} {
		t.Run(packetType, func(t *testing.T) {
			window := &raisingWindow{}
			client := &Client{windows: map[int64]ui.Window{7: window}}

			client.handlePacket(protocol.Packet{packetType, int64(7)})

			if window.raised != 1 {
				t.Errorf("Raise called %d times, want 1", window.raised)
			}
		})
	}
}

func TestHandleRaiseWindowIgnoresUnknownWindow(t *testing.T) {
	setBackwardsCompatible(t, true)
	client := &Client{windows: map[int64]ui.Window{}}
	client.handlePacket(protocol.Packet{"raise-window", int64(99)})
}
