package client

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
)

func captureLogs(t *testing.T, f func()) string {
	t.Helper()
	var output bytes.Buffer
	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	}()
	f()
	return output.String()
}

func TestHandleServerEvent(t *testing.T) {
	client := &Client{verbose: true}
	output := captureLogs(t, func() {
		client.handlePacket(protocol.Packet{"server-event", "suspend", "idle", int64(30)})
	})
	for _, want := range []string{
		"server event: suspend",
		`server event "suspend" arguments: [idle 30]`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("log output %q does not contain %q", output, want)
		}
	}
}

func TestHandleServerEventArgumentsNeedVerbose(t *testing.T) {
	client := &Client{}
	output := captureLogs(t, func() {
		client.handlePacket(protocol.Packet{"server-event", "resume", "user"})
	})
	if !strings.Contains(output, "server event: resume") {
		t.Errorf("log output %q does not contain the event", output)
	}
	if strings.Contains(output, "arguments") {
		t.Errorf("non-verbose log output contains arguments: %q", output)
	}
}

func TestHandleServerEventRejectsMalformedPackets(t *testing.T) {
	client := &Client{}
	for _, packet := range []protocol.Packet{
		{"server-event"},
		{"server-event", int64(1)},
	} {
		output := captureLogs(t, func() {
			client.handlePacket(packet)
		})
		if !strings.Contains(output, "ignoring malformed server-event") {
			t.Errorf("log output %q does not report a malformed packet", output)
		}
	}
}
