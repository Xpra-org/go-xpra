package client

import (
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

func setBackwardsCompatible(t *testing.T, enabled bool) {
	t.Helper()
	old := protocol.BackwardsCompatible
	protocol.BackwardsCompatible = enabled
	t.Cleanup(func() { protocol.BackwardsCompatible = old })
}

func TestLegacySpecialPacketTypesDisabled(t *testing.T) {
	setBackwardsCompatible(t, false)

	client := &Client{
		windows: map[int64]ui.Window{},
		quit:    make(chan struct{}),
	}
	client.handlePacket(protocol.Packet{
		"new-override-redirect", int64(7), int64(0), int64(0),
		int64(10), int64(10), map[string]any{},
	})
	if len(client.windows) != 0 {
		t.Error("legacy new-override-redirect packet was accepted")
	}

	client.handlePacket(protocol.Packet{"disconnect", "legacy"})
	select {
	case <-client.quit:
		t.Error("legacy disconnect packet was accepted")
	default:
	}

	client.handlePacket(protocol.Packet{"connection-close", "modern"})
	select {
	case <-client.quit:
	default:
		t.Error("modern connection-close packet was not accepted")
	}
}

func TestLegacyAliasDisabled(t *testing.T) {
	setBackwardsCompatible(t, false)
	display := &ringingDisplay{}
	client := &Client{display: display}
	args := []any{
		int64(7), int64(2), int64(80), int64(440),
		int64(250), int64(1), int64(3), "TerminalBell",
	}

	client.handlePacket(protocol.Packet(append([]any{"bell"}, args...)))
	if len(display.calls) != 0 {
		t.Error("legacy bell packet was accepted")
	}

	client.handlePacket(protocol.Packet(append([]any{"window-bell"}, args...)))
	if len(display.calls) != 1 {
		t.Error("modern window-bell packet was not accepted")
	}
}
