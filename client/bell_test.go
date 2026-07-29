package client

import (
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

type bellCall struct {
	percent, pitch, duration int64
	name                     string
}

type ringingDisplay struct {
	ui.Display
	calls []bellCall
}

func (d *ringingDisplay) Bell(percent, pitch, duration int64, name string) {
	d.calls = append(d.calls, bellCall{percent, pitch, duration, name})
}

func TestHandleBell(t *testing.T) {
	setBackwardsCompatible(t, true)
	for _, packetType := range []string{"bell", "window-bell"} {
		t.Run(packetType, func(t *testing.T) {
			display := &ringingDisplay{}
			client := &Client{display: display}
			packet := protocol.Packet{
				packetType, int64(7), int64(2), int64(80), int64(440),
				int64(250), int64(1), int64(3), "TerminalBell",
			}

			client.handlePacket(packet)

			want := bellCall{80, 440, 250, "TerminalBell"}
			if len(display.calls) != 1 || display.calls[0] != want {
				t.Errorf("Bell calls = %+v, want [%+v]", display.calls, want)
			}
		})
	}
}

func TestHandleBellAcceptsShortPacket(t *testing.T) {
	setBackwardsCompatible(t, true)
	display := &ringingDisplay{}
	client := &Client{display: display}

	client.handlePacket(protocol.Packet{"bell"})

	want := bellCall{}
	if len(display.calls) != 1 || display.calls[0] != want {
		t.Errorf("Bell calls = %+v, want [%+v]", display.calls, want)
	}
}
