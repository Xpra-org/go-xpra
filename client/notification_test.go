package client

import (
	"strings"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
)

func TestHandleNotificationShow(t *testing.T) {
	for _, packetType := range []string{"notify_show", "notification-show"} {
		t.Run(packetType, func(t *testing.T) {
			client := &Client{}
			packet := protocol.Packet{
				packetType, "org.example.Notifications", int64(42), "Terminal",
				int64(0), "terminal", "Build complete", "Passed\nNo warnings",
				int64(5000),
			}

			output := captureLogs(t, func() {
				client.handlePacket(packet)
			})

			for _, want := range []string{
				"notification from Terminal: Build complete",
				"  Passed",
				"  No warnings",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("log output %q does not contain %q", output, want)
				}
			}
		})
	}
}

func TestHandleNotificationShowWithoutApplication(t *testing.T) {
	client := &Client{}
	packet := protocol.Packet{
		"notify_show", "", int64(42), "", int64(0), "", "Session ready", "",
	}

	output := captureLogs(t, func() {
		client.handlePacket(packet)
	})
	if !strings.Contains(output, "notification: Session ready") {
		t.Errorf("log output %q does not contain the notification summary", output)
	}
}

func TestHandleNotificationClose(t *testing.T) {
	for _, packetType := range []string{"notify_close", "notification-close"} {
		t.Run(packetType, func(t *testing.T) {
			client := &Client{verbose: true}
			output := captureLogs(t, func() {
				client.handlePacket(protocol.Packet{packetType, int64(42)})
			})
			if !strings.Contains(output, "notification 42 closed") {
				t.Errorf("log output %q does not contain the close event", output)
			}
		})
	}
}

func TestHandleNotificationRejectsMalformedPackets(t *testing.T) {
	client := &Client{}
	for _, packet := range []protocol.Packet{
		{"notify_show"},
		{"notify_close"},
	} {
		output := captureLogs(t, func() {
			client.handlePacket(packet)
		})
		if !strings.Contains(output, "ignoring malformed notification-") {
			t.Errorf("log output %q does not report a malformed packet", output)
		}
	}
}
